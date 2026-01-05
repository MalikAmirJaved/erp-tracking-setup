package main

import (
	"log"
	"net/http"
	"sync"
	"time"

	"tracker/internal/screenshot"
)

var (
	mu        sync.Mutex
	isRunning bool
	ticker    *time.Ticker
	stopChan  chan struct{}
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow your dev origin + any (for packaged app, origin is null or file://)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func startCaptureLoop() {
	mu.Lock()
	if isRunning {
		mu.Unlock()
		return
	}
	isRunning = true
	mu.Unlock()

	stopChan = make(chan struct{})
	ticker = time.NewTicker(1 * time.Minute)

	go func() {
		// Immediate first screenshot
		if err := screenshot.CaptureScreen(); err != nil {
			log.Printf("Initial screenshot failed: %v", err)
		}

		for {
			select {
			case <-ticker.C:
				if err := screenshot.CaptureScreen(); err != nil {
					log.Printf("Periodic screenshot failed: %v", err)
				}
			case <-stopChan:
				ticker.Stop()
				mu.Lock()
				isRunning = false
				mu.Unlock()
				return
			}
		}
	}()
}

func stopCaptureLoop() {
	mu.Lock()
	defer mu.Unlock()
	if !isRunning {
		return
	}
	if stopChan != nil {
		close(stopChan)
	}
	isRunning = false
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		err := screenshot.CaptureScreen()
		if err != nil {
			http.Error(w, "Failed to take initial screenshot", http.StatusInternalServerError)
			return
		}

		startCaptureLoop()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		stopCaptureLoop()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("stopped"))
	})

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	log.Println("Go tracker running on :9090")
	log.Fatal(http.ListenAndServe(":9090", handler))
}