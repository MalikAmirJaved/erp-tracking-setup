package main

import (
	"log"
	"net/http"
)

func main() {
	// Initialize global values (IP & MAC are used in capture.go)
	localIP = getLocalIP()
	macAddress = getMACAddress()

	mux := http.NewServeMux()

	// Register all routes (handlers are now in capture.go)
	mux.HandleFunc("/set-user", setUserHandler)
	mux.HandleFunc("/start", startHandler)
	mux.HandleFunc("/stop", stopHandler)
	mux.HandleFunc("/break", breakHandler)
	mux.HandleFunc("/resume", resumeHandler)
	mux.HandleFunc("/status", statusHandler)

	// Apply CORS middleware
	handler := corsMiddleware(mux)

	log.Println("Server starting on http://127.0.0.1:9090")
	if err := http.ListenAndServe("127.0.0.1:9090", handler); err != nil {
		log.Fatal(err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}