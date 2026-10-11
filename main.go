package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed static/* static/vendor/*
var staticAssets embed.FS

func main() {
	envPath := flag.String("env", defaultEnvPath(), "configuration file path")
	host := flag.String("host", "", "address to bind (overrides FINANCE_HOST)")
	port := flag.Int("port", 0, "port to bind (overrides FINANCE_PORT)")
	flag.Parse()

	config, err := loadConfig(*envPath)
	if err != nil {
		log.Fatalf("configure app: %v", err)
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "host":
			config.Host = *host
		case "port":
			config.Port = *port
		}
	})
	if config.Host == "" || config.Port < 1 || config.Port > 65535 {
		log.Fatal("host must not be empty and port must be between 1 and 65535")
	}

	repository, err := NewSheetRepository(context.Background(), config)
	if err != nil {
		log.Fatalf("configure Google Sheets: %v", err)
	}
	assets, err := fs.Sub(staticAssets, "static")
	if err != nil {
		log.Fatalf("load embedded assets: %v", err)
	}

	address := fmt.Sprintf("%s:%d", config.Host, config.Port)
	server := &http.Server{
		Addr:              address,
		Handler:           NewHandler(repository, assets),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("Finance dashboard: http://%s", address)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve dashboard: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
