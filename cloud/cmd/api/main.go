package main

import (
	"log"
	"net/http"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func main() {
	addr := ":8080"
	engine := alerts.New(5 * time.Minute)
	srv := &http.Server{
		Addr:    addr,
		Handler: httpapi.New(ingest.NewStore(), engine),
	}
	log.Printf("pulse-mesh cloud listening %s", addr)
	log.Fatal(srv.ListenAndServe())
}
