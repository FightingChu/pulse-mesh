package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
	"pulse-mesh/cloud/internal/journal"
)

func main() {
	addr := envOr("PULSE_ADDR", ":8080")
	aiURL := os.Getenv("PULSE_AI_URL")
	if aiURL == "" {
		aiURL = "http://127.0.0.1:8000"
	}
	alertWindow := 5 * time.Minute
	if raw := os.Getenv("PULSE_ALERT_WINDOW"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("invalid PULSE_ALERT_WINDOW %q: %v", raw, err)
		}
		alertWindow = parsed
	}
	dataDir := os.Getenv("PULSE_DATA_DIR")
	dataLock, err := journal.AcquireDirectoryLock(dataDir)
	if err != nil {
		log.Fatalf("lock data directory: %v", err)
	}
	sampleStore := ingest.NewStore()
	diagnosisStore := diagnose.NewStore(1000)
	alertEngine := alerts.New(alertWindow)
	if dataDir != "" {
		sampleStore, err = ingest.OpenStore(filepath.Join(dataDir, "samples.jsonl"))
		if err != nil {
			_ = dataLock.Close()
			log.Fatalf("open sample store: %v", err)
		}
		diagnosisStore, err = diagnose.OpenStore(filepath.Join(dataDir, "diagnoses.jsonl"), 1000)
		if err != nil {
			_ = sampleStore.Close()
			_ = dataLock.Close()
			log.Fatalf("open diagnosis store: %v", err)
		}
		alertEngine, err = alerts.OpenStore(filepath.Join(dataDir, "alerts.jsonl"), alertWindow)
		if err != nil {
			_ = sampleStore.Close()
			_ = diagnosisStore.Close()
			_ = dataLock.Close()
			log.Fatalf("open alert store: %v", err)
		}
	}
	srv := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		Handler: httpapi.New(
			sampleStore,
			alertEngine,
			diagnose.NewClient(aiURL),
			diagnosisStore,
		),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("pulse-mesh cloud listening %s ai=%s", addr, aiURL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	if err := sampleStore.Close(); err != nil {
		log.Printf("close sample store: %v", err)
	}
	if err := diagnosisStore.Close(); err != nil {
		log.Printf("close diagnosis store: %v", err)
	}
	if err := alertEngine.Close(); err != nil {
		log.Printf("close alert store: %v", err)
	}
	if err := dataLock.Close(); err != nil {
		log.Printf("release data directory lock: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
