package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testJSONResponse(value any) *http.Response {
	payload, _ := json.Marshal(value)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(payload)),
	}
}

func TestAmountParsesSheetFormats(t *testing.T) {
	for input, expected := range map[any]float64{"$1,234.56": 1234.56, "($20.40)": -20.40, -3.5: -3.5} {
		if actual := asAmount(input); actual != expected {
			t.Fatalf("asAmount(%v) = %v, want %v", input, actual, expected)
		}
	}
}

func TestTransactionFromRow(t *testing.T) {
	row := []any{
		"txn-1", "2026-07-20", "2026-07", "DBS", "DBS CC", "Personal",
		"Dining Out", 12.5, "Lunch", "Card", "statement.pdf", true,
		"Expense", "", "note", "Other",
	}
	transaction := transactionFromRow(row, 42)
	if transaction.ID != "txn-1" || transaction.RowNumber != 42 || !transaction.Included {
		t.Fatalf("unexpected transaction: %#v", transaction)
	}
}

func TestRestoreCategoryOnlyUsesIncludedAutoCategory(t *testing.T) {
	included := map[string]bool{"Groceries": true, "Other": true, "Internal Transfer": false}
	if got := restoredCategory(Transaction{AutoCategory: "Groceries"}, included); got != "Groceries" {
		t.Fatalf("got %q", got)
	}
	if got := restoredCategory(Transaction{AutoCategory: "Internal Transfer"}, included); got != "Other" {
		t.Fatalf("got %q", got)
	}
}

func TestSheetRepositoryLoadsAndUpdatesByStableID(t *testing.T) {
	var updateBody struct {
		ValueInputOption string `json:"valueInputOption"`
		Data             []struct {
			Range  string  `json:"range"`
			Values [][]any `json:"values"`
		} `json:"data"`
	}
	row := []any{
		"txn-safe", "2026-07-20", "2026-07", "DBS", "DBS CC", "Personal",
		"Dining Out", 12.5, "Lunch", "Card", "statement.pdf", true,
		"Expense", "", "", "Other",
	}
	categories := [][]any{
		{"Category", "Type", "Description", "Include in Expenses"},
		{"Dining Out", "Expense", "", true},
		{"Internal Transfer", "Movement", "", false},
		{"Other", "Expense", "", true},
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			if err := json.NewDecoder(request.Body).Decode(&updateBody); err != nil {
				t.Errorf("decode update: %v", err)
			}
			return testJSONResponse(map[string]int{"totalUpdatedCells": len(updateBody.Data)}), nil
		}
		prefix := "/spreadsheets/" + sheetID + "/values/"
		escapedRange := strings.TrimPrefix(request.URL.EscapedPath(), prefix)
		rangeName, err := url.PathUnescape(escapedRange)
		if err != nil {
			t.Errorf("decode range: %v", err)
		}
		var values [][]any
		switch rangeName {
		case "'" + transactionsTab + "'!A1:P5000":
			headerRow := make([]any, len(headers))
			for index, header := range headers {
				headerRow[index] = header
			}
			values = [][]any{headerRow, row}
		case "'" + categoriesTab + "'!A1:D100":
			values = categories
		case "'" + transactionsTab + "'!A2:A5000":
			values = [][]any{{"txn-safe"}}
		case "'" + transactionsTab + "'!A2:P2":
			values = [][]any{row}
		default:
			t.Errorf("unexpected range %q", rangeName)
		}
		return testJSONResponse(map[string]any{"values": values}), nil
	})}

	repository := &SheetRepository{config: testConfig(), client: client, apiBase: "https://sheets.test", cacheFor: time.Minute}
	data, err := repository.Load(context.Background(), true)
	if err != nil || len(data.Transactions) != 1 || data.Transactions[0].ID != "txn-safe" {
		t.Fatalf("load data=%#v err=%v", data, err)
	}
	transfer := true
	updated, err := repository.UpdateTransaction(context.Background(), "txn-safe", TransactionPatch{IsTransfer: &transfer})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Category != "Internal Transfer" || updated.Included {
		t.Fatalf("unexpected update: %#v", updated)
	}
	if updateBody.ValueInputOption != "USER_ENTERED" || len(updateBody.Data) != 1 || updateBody.Data[0].Range != "'"+transactionsTab+"'!G2" {
		t.Fatalf("unsafe update body: %#v", updateBody)
	}
}

func TestCategoryUpdatesRunConcurrently(t *testing.T) {
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			arrived <- struct{}{}
			select {
			case <-release:
				return testJSONResponse(map[string]int{"totalUpdatedCells": 1}), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		rangeName, _ := url.PathUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/spreadsheets/"+sheetID+"/values/"))
		var values [][]any
		switch rangeName {
		case "'" + categoriesTab + "'!A1:D100":
			values = [][]any{{"Category", "Type", "Description", "Include in Expenses"}, {"Other", "Expense", "", true}}
		case "'" + transactionsTab + "'!A2:A5000":
			values = [][]any{{"txn-a"}, {"txn-b"}}
		default:
			id := "txn-a"
			if strings.HasSuffix(rangeName, "A3:P3") {
				id = "txn-b"
			}
			values = [][]any{{id, "2026-07-20", "2026-07", "DBS", "DBS CC", "Personal", "Other", 12.5, "Lunch", "Card", "statement.pdf", true, "Expense", "", "", "Other"}}
		}
		return testJSONResponse(map[string]any{"values": values}), nil
	})}
	repository := &SheetRepository{config: testConfig(), client: client, apiBase: "https://sheets.test"}
	results := make(chan error, 2)
	category := "Other"
	for _, id := range []string{"txn-a", "txn-b"} {
		go func(id string) {
			_, err := repository.UpdateTransaction(ctx, id, TransactionPatch{Category: &category})
			results <- err
		}(id)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-ctx.Done():
			close(release)
			t.Fatal("distinct category writes did not run concurrently")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCategoryBatchValidatesBeforeSingleWrite(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "duplicate-sheet-id", "duplicate-selection", "invalid-category", "write-failure"} {
		t.Run(scenario, func(t *testing.T) {
			posts := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method == http.MethodPost {
					posts++
					var payload struct {
						Data []struct {
							Range  string     `json:"range"`
							Values [][]string `json:"values"`
						} `json:"data"`
					}
					if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if len(payload.Data) != 2 || payload.Data[0].Range != "'"+transactionsTab+"'!G3" || payload.Data[1].Range != "'"+transactionsTab+"'!G2" {
						t.Fatalf("unsafe ranges: %#v", payload)
					}
					for _, item := range payload.Data {
						if len(item.Values) != 1 || len(item.Values[0]) != 1 || item.Values[0][0] != "Other" {
							t.Fatalf("unexpected values: %#v", item)
						}
					}
					response := testJSONResponse(map[string]int{"totalUpdatedCells": 2})
					if scenario == "write-failure" {
						response.StatusCode = http.StatusServiceUnavailable
					}
					return response, nil
				}
				if strings.Contains(request.URL.Path, categoriesTab) {
					return testJSONResponse(map[string]any{"values": [][]any{{"Category", "Type", "Description", "Include in Expenses"}, {"Other", "Expense", "", true}}}), nil
				}
				header := make([]any, len(headers))
				for i, name := range headers {
					header[i] = name
				}
				rows := [][]any{header, {"txn-a", "2026-07-20", "2026-07", "DBS", "DBS CC", "Personal", "Dining Out", 12.5}, {"txn-b", "2026-07-21", "2026-07", "DBS", "DBS CC", "Personal", "Dining Out", 20}}
				if scenario == "duplicate-sheet-id" {
					rows = append(rows, rows[1])
				}
				return testJSONResponse(map[string]any{"values": rows}), nil
			})}
			repository := &SheetRepository{config: testConfig(), client: client, apiBase: "https://sheets.test", cache: &cacheEntry{}}
			ids, category := []string{"txn-b", "txn-a"}, "Other"
			if scenario == "missing" {
				ids[1] = "gone"
			}
			if scenario == "duplicate-selection" {
				ids[1] = ids[0]
			}
			if scenario == "invalid-category" {
				category = "Unknown"
			}
			updated, err := repository.UpdateCategories(context.Background(), ids, category)
			if scenario == "valid" {
				if err != nil || posts != 1 || len(updated) != 2 || updated[0].ID != "txn-b" || updated[0].Category != "Other" || !updated[0].Included || repository.cache != nil {
					t.Fatalf("batch result: %#v error=%v posts=%d", updated, err, posts)
				}
			} else {
				if err == nil {
					t.Fatal("expected validation/write failure")
				}
				wantPosts := 0
				if scenario == "write-failure" {
					wantPosts = 1
					if repository.cache != nil {
						t.Fatal("failed write retained cache")
					}
				}
				if posts != wantPosts {
					t.Fatalf("writes=%d want=%d", posts, wantPosts)
				}
			}
		})
	}
}

const sheetID = "test-workbook"
const transactionsTab = "📋 Transactions"
const categoriesTab = "🏷️ Categories"

func testConfig() Config {
	return Config{SheetID: sheetID, TransactionsTab: transactionsTab, CategoriesTab: categoriesTab}
}
