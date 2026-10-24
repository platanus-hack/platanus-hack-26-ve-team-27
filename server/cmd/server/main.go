// Command server is the MAMÁ backend HTTP server.
package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("mama server listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
