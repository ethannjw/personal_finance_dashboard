package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigEnvAndPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# config\nFINANCE_SHEET_ID=example-workbook\nGOOGLE_TOKEN_FILE=secrets/token.json\nFINANCE_TRANSACTIONS_TAB=\"My Transactions\"\nFINANCE_PORT=9001\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FINANCE_HOST", "127.0.0.2")
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.SheetID != "example-workbook" || config.Port != 9001 || config.Host != "127.0.0.2" || config.TransactionsTab != "My Transactions" || config.TokenFile != filepath.Join(filepath.Dir(path), "secrets/token.json") {
		t.Fatalf("unexpected config: %#v", config)
	}
	t.Setenv("FINANCE_SHEET_ID", "environment-workbook")
	config, err = loadConfig(path)
	if err != nil || config.SheetID != "environment-workbook" {
		t.Fatalf("environment override failed: %v", err)
	}
}

func TestConfigRequiresWorkbookAndToken(t *testing.T) {
	t.Setenv("FINANCE_SHEET_ID", "")
	_, err := loadConfig(filepath.Join(t.TempDir(), ".env"))
	if err == nil || !strings.Contains(err.Error(), "FINANCE_SHEET_ID") {
		t.Fatalf("expected missing workbook error: %v", err)
	}
	t.Setenv("FINANCE_SHEET_ID", "test-workbook")
	t.Setenv("GOOGLE_TOKEN_FILE", "")
	_, err = loadConfig(filepath.Join(t.TempDir(), ".env"))
	if err == nil || !strings.Contains(err.Error(), "GOOGLE_TOKEN_FILE") {
		t.Fatalf("expected missing token error: %v", err)
	}
	t.Setenv("GOOGLE_TOKEN_FILE", "token.json")
	t.Setenv("FINANCE_PORT", "70000")
	_, err = loadConfig(filepath.Join(t.TempDir(), ".env"))
	if err == nil {
		t.Fatal("invalid port accepted")
	}
}

func TestEnvParserRejectsMalformedWithoutExposingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	for _, content := range []string{"bad-key=private-value", "KEY=\"private-value"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := readEnv(path)
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("unexpected parser error: %v", err)
		}
	}
}

func TestConfiguredWorkbookAndTabsUsedForReadsAndWrites(t *testing.T) {
	config := Config{SheetID: "another-workbook", TransactionsTab: "Owner's transactions", CategoriesTab: "Custom categories"}
	var seen []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(request.URL.Path, "/spreadsheets/another-workbook/values") {
			t.Errorf("wrong workbook: %s", request.URL.Path)
		}
		if request.Method == http.MethodPost {
			var body struct {
				Data []struct {
					Range string `json:"range"`
				} `json:"data"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Data) != 1 || body.Data[0].Range != "'Owner''s transactions'!O2" {
				t.Fatalf("wrong write range: %#v", body)
			}
			return testJSONResponse(map[string]int{"totalUpdatedCells": 1}), nil
		}
		seen = append(seen, request.URL.Path)
		if strings.Contains(request.URL.Path, "Custom categories") {
			return testJSONResponse(map[string]any{"values": [][]any{{"Category", "Type", "Description", "Include in Expenses"}, {"Other", "Expense", "", true}}}), nil
		}
		header := make([]any, len(headers))
		for i, h := range headers {
			header[i] = h
		}
		return testJSONResponse(map[string]any{"values": [][]any{header}}), nil
	})}
	r := &SheetRepository{config: config, client: client, apiBase: "https://sheets.test"}
	data, err := r.Load(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if data.SpreadsheetID != config.SheetID || !strings.Contains(data.SpreadsheetURL, config.SheetID) {
		t.Fatal("wrong workbook metadata")
	}
	if len(seen) != 2 || !strings.Contains(seen[0], "Owner''s transactions") || !strings.Contains(seen[1], "Custom categories") {
		t.Fatalf("wrong read ranges: %#v", seen)
	}
	if err := r.writeUpdates(context.Background(), 2, map[string]any{"O": "Note"}); err != nil {
		t.Fatal(err)
	}
}
