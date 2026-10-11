package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

type fakeRepository struct {
	data         DashboardData
	updatedID    string
	updatedPatch TransactionPatch
}

func (f *fakeRepository) Load(_ context.Context, _ bool) (DashboardData, error) {
	return f.data, nil
}

func (f *fakeRepository) UpdateTransaction(_ context.Context, id string, patch TransactionPatch) (Transaction, error) {
	f.updatedID, f.updatedPatch = id, patch
	return Transaction{ID: id, Category: "Groceries", Included: true}, nil
}

func (f *fakeRepository) UpdateCategories(_ context.Context, ids []string, category string) ([]Transaction, error) {
	transactions := make([]Transaction, 0, len(ids))
	for _, id := range ids {
		transactions = append(transactions, Transaction{ID: id, Category: category})
	}
	return transactions, nil
}

func testAssets() fs.FS {
	return fstest.MapFS{"index.html": {Data: []byte("<h1>Ledger</h1>")}}
}

func TestHealthAndData(t *testing.T) {
	repository := &fakeRepository{data: DashboardData{Transactions: []Transaction{{ID: "txn-1"}}}}
	handler := NewHandler(repository, testAssets())
	for _, path := range []string{"/api/health", "/api/data"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, response.Code)
		}
	}
}

func TestUnknownPatchFieldIsRejected(t *testing.T) {
	handler := NewHandler(&fakeRepository{}, testAssets())
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/transactions/txn-1", strings.NewReader(`{"amount": 20}`))
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("returned %d", response.Code)
	}
}

func TestPatchUsesTransactionID(t *testing.T) {
	repository := &fakeRepository{}
	handler := NewHandler(repository, testAssets())
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/transactions/txn-stable", strings.NewReader(`{"category":"Groceries"}`))
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || repository.updatedID != "txn-stable" {
		t.Fatalf("status=%d id=%q", response.Code, repository.updatedID)
	}
	var body map[string]Transaction
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["transaction"].ID != "txn-stable" {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestBulkCategoryRoute(t *testing.T) {
	handler := NewHandler(&fakeRepository{}, testAssets())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/api/transactions/categories", strings.NewReader(`{"ids":["txn-a","txn-b"],"category":"Other"}`)))
	var payload struct {
		Transactions []Transaction `json:"transactions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(payload.Transactions) != 2 || payload.Transactions[0].Category != "Other" {
		t.Fatalf("response: %d %s", response.Code, response.Body.String())
	}
}
