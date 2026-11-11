package main

import (
	"log"
	"net/http"

	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func main() {
	addr := ":8080"
	srv := &http.Server{
		Addr:    addr,
		Handler: httpapi.New(ingest.NewStore()),
	}
	log.Printf("pulse-mesh cloud listening %s", addr)
	log.Fatal(srv.ListenAndServe())
}
