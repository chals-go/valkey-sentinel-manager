package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type record struct {
	Body   string `json:"body"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Time   string `json:"time"`
}

var (
	mu      sync.Mutex
	history []record
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	mux.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(history)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec := record{
			Body:   string(body),
			Method: r.Method,
			Path:   r.URL.RequestURI(),
			Time:   time.Now().Format(time.RFC3339),
		}
		mu.Lock()
		history = append(history, rec)
		if len(history) > 100 {
			history = history[len(history)-100:]
		}
		mu.Unlock()
		log.Printf("%s %s %s", r.Method, r.URL.RequestURI(), string(body))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	log.Println("Mock DNS API listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
