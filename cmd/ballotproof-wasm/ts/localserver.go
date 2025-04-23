package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	// Define your static files directory.
	staticDir := "./"
	// Create a file server for your directory.
	fs := http.FileServer(http.Dir(staticDir))
	// Custom handler to check for gzip support.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Construct the file path from the requested URL.
		filePath := staticDir + r.URL.Path
		// Check if client accepts gzip.
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			gzPath := filePath + ".gz"
			if _, err := os.Stat(gzPath); err == nil {
				// If the gzipped file exists, set the gzip encoding header.
				w.Header().Set("Content-Encoding", "gzip")
				// For wasm files, set the proper MIME type.
				if strings.HasSuffix(r.URL.Path, ".wasm") {
					w.Header().Set("Content-Type", "application/wasm")
				}
				http.ServeFile(w, r, gzPath)
				return
			}
		}
		// Otherwise, serve the file normally.
		fs.ServeHTTP(w, r)
	})

	log.Println("Server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
