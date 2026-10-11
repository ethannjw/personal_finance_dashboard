package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const sheetsAPIBase = "https://sheets.googleapis.com/v4"

var headers = []string{
	"ID", "Date", "Month", "Bank", "Account", "Reporting Bucket",
	"Category", "Amount (SGD)", "Description", "Card / Source", "File",
	"Include in Expenses", "Transaction Type", "Transfer Link", "Notes",
	"Auto Category",
}

type Transaction struct {
	ID              string  `json:"id"`
	Date            string  `json:"date"`
	Month           string  `json:"month"`
	Bank            string  `json:"bank"`
	Account         string  `json:"account"`
	ReportingBucket string  `json:"reportingBucket"`
	Category        string  `json:"category"`
	Amount          float64 `json:"amount"`
	Description     string  `json:"description"`
	Source          string  `json:"source"`
	File            string  `json:"file"`
	Included        bool    `json:"included"`
	TransactionType string  `json:"transactionType"`
	TransferLink    string  `json:"transferLink"`
	Notes           string  `json:"notes"`
	AutoCategory    string  `json:"autoCategory"`
	RowNumber       int     `json:"rowNumber"`
}

type Category struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Included    bool   `json:"included"`
}

type DashboardData struct {
	Transactions   []Transaction `json:"transactions"`
	Categories     []Category    `json:"categories"`
	SpreadsheetID  string        `json:"spreadsheetId"`
	SpreadsheetURL string        `json:"spreadsheetUrl"`
	SyncedAt       string        `json:"syncedAt"`
}

type TransactionPatch struct {
	Category    *string `json:"category"`
	Description *string `json:"description"`
	Notes       *string `json:"notes"`
	IsTransfer  *bool   `json:"isTransfer"`
}

type FinanceRepository interface {
	Load(context.Context, bool) (DashboardData, error)
	UpdateTransaction(context.Context, string, TransactionPatch) (Transaction, error)
	UpdateCategories(context.Context, []string, string) ([]Transaction, error)
}

type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return e.Message }

type cacheEntry struct {
	created time.Time
	data    DashboardData
}

type SheetRepository struct {
	config      Config
	client      *http.Client
	apiBase     string
	cacheFor    time.Duration
	mu          sync.Mutex
	cache       *cacheEntry
	updateLocks [64]sync.Mutex
}

type authorizedUser struct {
	Token        string   `json:"token"`
	RefreshToken string   `json:"refresh_token"`
	TokenURI     string   `json:"token_uri"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Scopes       []string `json:"scopes"`
	Expiry       string   `json:"expiry"`
	TokenType    string   `json:"token_type"`
}

func NewSheetRepository(ctx context.Context, config Config) (*SheetRepository, error) {
	payload, err := os.ReadFile(config.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("read Google token: %w", err)
	}
	var stored authorizedUser
	if err := json.Unmarshal(payload, &stored); err != nil {
		return nil, fmt.Errorf("parse Google token: %w", err)
	}
	if stored.RefreshToken == "" || stored.ClientID == "" || stored.ClientSecret == "" {
		return nil, errors.New("Google token is missing refresh credentials")
	}
	expiry, _ := time.Parse(time.RFC3339Nano, stored.Expiry)
	token := &oauth2.Token{
		AccessToken:  stored.Token,
		RefreshToken: stored.RefreshToken,
		TokenType:    stored.TokenType,
		Expiry:       expiry,
	}
	oauthConfig := oauth2.Config{
		ClientID:     stored.ClientID,
		ClientSecret: stored.ClientSecret,
		Scopes:       stored.Scopes,
		Endpoint: oauth2.Endpoint{
			TokenURL: stored.TokenURI,
		},
	}
	return &SheetRepository{
		config:   config,
		client:   oauth2.NewClient(ctx, oauthConfig.TokenSource(ctx, token)),
		apiBase:  sheetsAPIBase,
		cacheFor: 20 * time.Second,
	}, nil
}

func (r *SheetRepository) getValues(ctx context.Context, rangeName string) ([][]any, error) {
	endpoint := fmt.Sprintf("%s/spreadsheets/%s/values/%s", r.apiBase, r.config.SheetID, url.PathEscape(rangeName))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	query := request.URL.Query()
	query.Set("valueRenderOption", "UNFORMATTED_VALUE")
	request.URL.RawQuery = query.Encode()
	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read Google Sheet: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return nil, googleResponseError(response)
	}
	var result struct {
		Values [][]any `json:"values"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Google Sheet: %w", err)
	}
	return result.Values, nil
}

func googleResponseError(response *http.Response) error {
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(payload, &body)
	message := body.Error.Message
	if message == "" {
		message = strings.TrimSpace(string(payload))
	}
	if message == "" {
		message = response.Status
	}
	return fmt.Errorf("Google Sheets API: %s", message)
}

func (r *SheetRepository) categories(ctx context.Context) ([]Category, error) {
	rows, err := r.getValues(ctx, r.tabRange(r.config.CategoriesTab, "A1:D100"))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, &HTTPError{Status: http.StatusBadGateway, Message: "The Categories tab is empty."}
	}
	categories := make([]Category, 0, len(rows)-1)
	for _, row := range rows[1:] {
		row = padded(row, 4)
		if strings.TrimSpace(asString(row[0])) == "" {
			continue
		}
		categories = append(categories, Category{
			Name: asString(row[0]), Type: asString(row[1]),
			Description: asString(row[2]), Included: asBool(row[3]),
		})
	}
	return categories, nil
}

func (r *SheetRepository) Load(ctx context.Context, force bool) (DashboardData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !force && r.cache != nil && time.Since(r.cache.created) < r.cacheFor {
		return r.cache.data, nil
	}
	rows, err := r.getValues(ctx, r.tabRange(r.config.TransactionsTab, "A1:P5000"))
	if err != nil {
		return DashboardData{}, err
	}
	if len(rows) == 0 || !sameHeaders(rows[0]) {
		return DashboardData{}, &HTTPError{
			Status:  http.StatusBadGateway,
			Message: "The Transactions tab schema has changed; no data was loaded.",
		}
	}
	transactions := make([]Transaction, 0, len(rows)-1)
	for index, row := range rows[1:] {
		if len(row) == 0 || strings.TrimSpace(asString(row[0])) == "" {
			continue
		}
		transactions = append(transactions, transactionFromRow(row, index+2))
	}
	categories, err := r.categories(ctx)
	if err != nil {
		return DashboardData{}, err
	}
	data := DashboardData{
		Transactions:   transactions,
		Categories:     categories,
		SpreadsheetID:  r.config.SheetID,
		SpreadsheetURL: "https://docs.google.com/spreadsheets/d/" + r.config.SheetID + "/edit",
		SyncedAt:       time.Now().Format(time.RFC3339),
	}
	r.cache = &cacheEntry{created: time.Now(), data: data}
	return data, nil
}

func (r *SheetRepository) findTransaction(ctx context.Context, id string) (Transaction, error) {
	ids, err := r.getValues(ctx, r.tabRange(r.config.TransactionsTab, "A2:A5000"))
	if err != nil {
		return Transaction{}, err
	}
	rowNumber := 0
	for index, row := range ids {
		if len(row) > 0 && asString(row[0]) == id {
			if rowNumber != 0 {
				return Transaction{}, &HTTPError{Status: http.StatusConflict, Message: "Transaction ID is duplicated; no update was made."}
			}
			rowNumber = index + 2
		}
	}
	if rowNumber == 0 {
		return Transaction{}, &HTTPError{Status: http.StatusNotFound, Message: "Transaction no longer exists. Refresh the dashboard."}
	}
	rows, err := r.getValues(ctx, r.tabRange(r.config.TransactionsTab, fmt.Sprintf("A%d:P%d", rowNumber, rowNumber)))
	if err != nil {
		return Transaction{}, err
	}
	if len(rows) != 1 {
		return Transaction{}, &HTTPError{Status: http.StatusConflict, Message: "Transaction row could not be read."}
	}
	return transactionFromRow(rows[0], rowNumber), nil
}

func (r *SheetRepository) UpdateTransaction(ctx context.Context, id string, patch TransactionPatch) (Transaction, error) {
	// Serialize updates to the same transaction while allowing unrelated rows to save concurrently.
	var lockIndex uint32
	for _, char := range id {
		lockIndex = lockIndex*31 + uint32(char)
	}
	lock := &r.updateLocks[lockIndex%uint32(len(r.updateLocks))]
	lock.Lock()
	defer lock.Unlock()
	current, err := r.findTransaction(ctx, id)
	if err != nil {
		return Transaction{}, err
	}
	categories, err := r.categories(ctx)
	if err != nil {
		return Transaction{}, err
	}
	categoryNames := make(map[string]bool, len(categories))
	includedCategories := make(map[string]bool, len(categories))
	for _, category := range categories {
		categoryNames[category.Name] = true
		includedCategories[category.Name] = category.Included
	}
	updates := map[string]any{}
	if patch.Category != nil {
		category := strings.TrimSpace(*patch.Category)
		if !categoryNames[category] {
			return Transaction{}, &HTTPError{Status: http.StatusBadRequest, Message: "Choose a category from the workbook's Categories tab."}
		}
		updates["G"] = category
	}
	if patch.IsTransfer != nil {
		if *patch.IsTransfer {
			updates["G"] = "Internal Transfer"
		} else if !includedCategories[current.Category] {
			if _, alreadySet := updates["G"]; !alreadySet {
				updates["G"] = restoredCategory(current, includedCategories)
			}
		}
	}
	for _, editable := range []struct {
		value  *string
		column string
		name   string
		limit  int
	}{
		{patch.Description, "I", "Description", 500},
		{patch.Notes, "O", "Notes", 2000},
	} {
		if editable.value == nil {
			continue
		}
		value := strings.TrimSpace(*editable.value)
		if len(value) > editable.limit {
			return Transaction{}, &HTTPError{Status: http.StatusBadRequest, Message: fmt.Sprintf("%s is limited to %d characters.", editable.name, editable.limit)}
		}
		updates[editable.column] = value
	}
	if len(updates) == 0 {
		return current, nil
	}
	if err := r.writeUpdates(ctx, current.RowNumber, updates); err != nil {
		return Transaction{}, err
	}
	r.mu.Lock()
	r.cache = nil
	r.mu.Unlock()
	if value, ok := updates["G"]; ok {
		current.Category = value.(string)
		current.Included = includedCategories[current.Category]
	}
	if value, ok := updates["I"]; ok {
		current.Description = value.(string)
	}
	if value, ok := updates["O"]; ok {
		current.Notes = value.(string)
	}
	return current, nil
}

func (r *SheetRepository) UpdateCategories(ctx context.Context, ids []string, category string) ([]Transaction, error) {
	if len(ids) == 0 || len(ids) > 4999 {
		return nil, &HTTPError{Status: http.StatusBadRequest, Message: "Select between 1 and 4999 entries."}
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || wanted[id] {
			return nil, &HTTPError{Status: http.StatusBadRequest, Message: "Selection contains an empty or repeated transaction ID."}
		}
		wanted[id] = true
	}
	// Acquire in a fixed order so batches cannot deadlock with each other.
	for i := range r.updateLocks {
		r.updateLocks[i].Lock()
	}
	defer func() {
		for i := len(r.updateLocks) - 1; i >= 0; i-- {
			r.updateLocks[i].Unlock()
		}
	}()
	categories, err := r.categories(ctx)
	if err != nil {
		return nil, err
	}
	category = strings.TrimSpace(category)
	valid, included := false, false
	for _, item := range categories {
		if item.Name == category {
			valid, included = true, item.Included
			break
		}
	}
	if !valid {
		return nil, &HTTPError{Status: http.StatusBadRequest, Message: "Choose a category from the workbook's Categories tab."}
	}
	// Read fresh IDs and row data, never browser row numbers or the dashboard cache.
	rows, err := r.getValues(ctx, r.tabRange(r.config.TransactionsTab, "A1:P5000"))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || !sameHeaders(rows[0]) {
		return nil, &HTTPError{Status: http.StatusConflict, Message: "Transaction schema changed; no update was made."}
	}
	found := make(map[string]Transaction, len(ids))
	for index, row := range rows[1:] {
		if len(row) == 0 {
			continue
		}
		id := asString(row[0])
		if !wanted[id] {
			continue
		}
		if _, exists := found[id]; exists {
			return nil, &HTTPError{Status: http.StatusConflict, Message: "Transaction ID is duplicated; no update was made."}
		}
		found[id] = transactionFromRow(row, index+2)
	}
	type valueRange struct {
		Range  string  `json:"range"`
		Values [][]any `json:"values"`
	}
	data := make([]valueRange, 0, len(ids))
	updated := make([]Transaction, 0, len(ids))
	for _, id := range ids {
		transaction, exists := found[id]
		if !exists {
			return nil, &HTTPError{Status: http.StatusConflict, Message: "Selected transaction no longer exists. Refresh the dashboard; no update was made."}
		}
		data = append(data, valueRange{Range: r.tabRange(r.config.TransactionsTab, fmt.Sprintf("G%d", transaction.RowNumber)), Values: [][]any{{category}}})
		transaction.Category, transaction.Included = category, included
		updated = append(updated, transaction)
	}
	// Invalidate even if the response is lost after Google applies the write.
	defer func() { r.mu.Lock(); r.cache = nil; r.mu.Unlock() }()
	if err := r.writeValueBatch(ctx, data); err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *SheetRepository) writeUpdates(ctx context.Context, rowNumber int, updates map[string]any) error {
	type valueRange struct {
		Range  string  `json:"range"`
		Values [][]any `json:"values"`
	}
	data := make([]valueRange, 0, len(updates))
	for _, column := range []string{"G", "I", "O"} {
		if value, ok := updates[column]; ok {
			data = append(data, valueRange{
				Range:  r.tabRange(r.config.TransactionsTab, fmt.Sprintf("%s%d", column, rowNumber)),
				Values: [][]any{{value}},
			})
		}
	}
	return r.writeValueBatch(ctx, data)
}

func (r *SheetRepository) writeValueBatch(ctx context.Context, data any) error {
	body, err := json.Marshal(map[string]any{"valueInputOption": "USER_ENTERED", "data": data})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/spreadsheets/%s/values:batchUpdate", r.apiBase, r.config.SheetID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("update Google Sheet: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return googleResponseError(response)
	}
	return nil
}

func padded(row []any, width int) []any {
	result := make([]any, width)
	copy(result, row)
	return result
}

func sameHeaders(row []any) bool {
	if len(row) != len(headers) {
		return false
	}
	for index := range headers {
		if asString(row[index]) != headers[index] {
			return false
		}
	}
	return true
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func asBool(value any) bool {
	if boolean, ok := value.(bool); ok {
		return boolean
	}
	valueString := strings.ToLower(strings.TrimSpace(asString(value)))
	return valueString == "true" || valueString == "1" || valueString == "yes"
}

var amountCleanup = regexp.MustCompile(`[^0-9.()\-]`)

func asAmount(value any) float64 {
	if number, ok := value.(float64); ok {
		return number
	}
	cleaned := amountCleanup.ReplaceAllString(asString(value), "")
	if strings.HasPrefix(cleaned, "(") && strings.HasSuffix(cleaned, ")") {
		cleaned = "-" + strings.TrimSuffix(strings.TrimPrefix(cleaned, "("), ")")
	}
	number, _ := strconv.ParseFloat(cleaned, 64)
	return number
}

func transactionFromRow(row []any, rowNumber int) Transaction {
	row = padded(row, len(headers))
	return Transaction{
		ID: asString(row[0]), Date: asString(row[1]), Month: asString(row[2]),
		Bank: asString(row[3]), Account: asString(row[4]), ReportingBucket: asString(row[5]),
		Category: asString(row[6]), Amount: asAmount(row[7]), Description: asString(row[8]),
		Source: asString(row[9]), File: asString(row[10]), Included: asBool(row[11]),
		TransactionType: asString(row[12]), TransferLink: asString(row[13]),
		Notes: asString(row[14]), AutoCategory: asString(row[15]), RowNumber: rowNumber,
	}
}

func restoredCategory(transaction Transaction, included map[string]bool) string {
	if included[transaction.AutoCategory] {
		return transaction.AutoCategory
	}
	return "Other"
}
