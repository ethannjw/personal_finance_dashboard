package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	SheetID, TokenFile, TransactionsTab, CategoriesTab, Host string
	Port                                                     int
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var workbookID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// readEnv accepts literal KEY=value entries, with optional matching quotes.
// It never executes shell code or expands variables.
func readEnv(path string) (map[string]string, error) {
	values := map[string]string{}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		entry := strings.TrimSpace(scanner.Text())
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || !envKey.MatchString(key) {
			return nil, fmt.Errorf("%s:%d: expected KEY=value", path, line)
		}
		if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'") {
			if len(value) < 2 || value[len(value)-1] != value[0] {
				return nil, fmt.Errorf("%s:%d: unmatched quotes", path, line)
			}
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values, scanner.Err()
}

func defaultEnvPath() string {
	if _, err := os.Stat(".env"); err == nil {
		return ".env"
	}
	executable, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(executable), ".env")
	}
	return ".env"
}

func loadConfig(path string) (Config, error) {
	values, err := readEnv(path)
	if err != nil {
		return Config{}, err
	}
	get := func(key, fallback string) string {
		if value, ok := os.LookupEnv(key); ok {
			return value
		}
		if value, ok := values[key]; ok {
			return value
		}
		return fallback
	}
	config := Config{
		SheetID: get("FINANCE_SHEET_ID", ""), TokenFile: get("GOOGLE_TOKEN_FILE", ""),
		TransactionsTab: get("FINANCE_TRANSACTIONS_TAB", "📋 Transactions"),
		CategoriesTab:   get("FINANCE_CATEGORIES_TAB", "🏷️ Categories"),
		Host:            get("FINANCE_HOST", "127.0.0.1"),
	}
	config.Port, err = strconv.Atoi(get("FINANCE_PORT", "8765"))
	if err != nil || config.Port < 1 || config.Port > 65535 {
		return Config{}, fmt.Errorf("FINANCE_PORT must be between 1 and 65535")
	}
	if !workbookID.MatchString(config.SheetID) {
		return Config{}, fmt.Errorf("set FINANCE_SHEET_ID to your workbook ID in .env or the environment")
	}
	if config.TokenFile == "" {
		return Config{}, fmt.Errorf("set GOOGLE_TOKEN_FILE to your authorized-user OAuth JSON file")
	}
	if strings.HasPrefix(config.TokenFile, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, err
		}
		config.TokenFile = filepath.Join(home, config.TokenFile[2:])
	} else if !filepath.IsAbs(config.TokenFile) {
		config.TokenFile = filepath.Join(filepath.Dir(path), config.TokenFile)
	}
	if config.TransactionsTab == "" || config.CategoriesTab == "" || config.Host == "" {
		return Config{}, fmt.Errorf("tab names and FINANCE_HOST must not be empty")
	}
	return config, nil
}

func (r *SheetRepository) tabRange(tab, cells string) string {
	return "'" + strings.ReplaceAll(tab, "'", "''") + "'!" + cells
}
