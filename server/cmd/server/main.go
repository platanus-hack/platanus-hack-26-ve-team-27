// Command server is the MAMÁ backend HTTP server.
package main

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/config"
)

func main() {
	cfg := config.Load()
	if missing := cfg.Missing(); len(missing) > 0 {
		// Names only, never values.
		log.Printf("config: not set: %s", strings.Join(missing, ", "))
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("mama server listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
