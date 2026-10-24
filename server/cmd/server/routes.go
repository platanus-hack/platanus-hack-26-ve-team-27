package main

import "net/http"

// newMux registers every HTTP route. Add new routes here so there is one
// place to see the whole API surface.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleRoot)
	return mux
}

func handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("mama server\n"))
}
