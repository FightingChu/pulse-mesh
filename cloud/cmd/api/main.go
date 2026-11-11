package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func main() {
	addr := ":8080"
	aiURL := os.Getenv("PULSE_AI_URL")
	if aiURL == "" {
		aiURL = "http://127.0.0.1:8000"
	}
	srv := &http.Server{
		Addr: addr,
		Handler: httpapi.New(
			ingest.NewStore(),
			alerts.New(5*time.Minute),
			diagnose.NewClient(aiURL),
			diagnose.NewStore(1000),
		),
	}
	log.Printf("pulse-mesh cloud listening %s", addr)
	log.Fatal(srv.ListenAndServe())
}
