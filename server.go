package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type application struct {
	repository FinanceRepository
	assets     fs.FS
}

func NewHandler(repository FinanceRepository, assets fs.FS) http.Handler {
	app := &application{repository: repository, assets: assets}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(40 * time.Second))
	router.Get("/api/health", app.health)
	router.Get("/api/data", app.data)
	router.Patch("/api/transactions/categories", app.updateCategories)
	router.Patch("/api/transactions/{transactionID}", app.updateTransaction)
	router.NotFound(app.static)
	return router
}

func (a *application) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (a *application) data(response http.ResponseWriter, request *http.Request) {
	data, err := a.repository.Load(request.Context(), request.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, data)
}

func (a *application) updateCategories(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, 1_048_576)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload struct {
		IDs      []string `json:"ids"`
		Category string   `json:"category"`
	}
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "Invalid category batch."})
		return
	}
	transactions, err := a.repository.UpdateCategories(request.Context(), payload.IDs, payload.Category)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"transactions": transactions})
}

func (a *application) updateTransaction(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, 16_384)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var patch TransactionPatch
	if err := decoder.Decode(&patch); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "Invalid transaction update."})
		return
	}
	transaction, err := a.repository.UpdateTransaction(request.Context(), chi.URLParam(request, "transactionID"), patch)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]Transaction{"transaction": transaction})
}

func (a *application) static(response http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/api/") {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "Not found."})
		return
	}
	assetPath := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if assetPath == "." || assetPath == "" {
		assetPath = "index.html"
	}
	payload, err := fs.ReadFile(a.assets, assetPath)
	if err != nil {
		payload, err = fs.ReadFile(a.assets, "index.html")
	}
	if err != nil {
		http.Error(response, "Dashboard assets unavailable", http.StatusInternalServerError)
		return
	}
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
	if strings.HasSuffix(assetPath, ".css") {
		response.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else if strings.HasSuffix(assetPath, ".js") {
		response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	} else {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, _ = response.Write(payload)
}

func writeError(response http.ResponseWriter, err error) {
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		writeJSON(response, httpError.Status, map[string]string{"error": httpError.Message})
		return
	}
	writeJSON(response, http.StatusBadGateway, map[string]string{"error": "Google Sheets request failed. Check the server log."})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
