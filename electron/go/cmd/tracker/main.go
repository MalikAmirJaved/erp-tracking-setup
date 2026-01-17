// main.go (updated)
package main

import (
	"log"
	"net/http"
)

func main() {
	// Initialize global values
	localIP = getLocalIP()
	macAddress = getMACAddress()

	mux := http.NewServeMux()

	// Register existing routes
	mux.HandleFunc("/set-user", setUserHandler)
	mux.HandleFunc("/start", startHandler)
	mux.HandleFunc("/stop", stopHandler)
	mux.HandleFunc("/break", breakHandler)
	mux.HandleFunc("/resume", resumeHandler)
	mux.HandleFunc("/status", statusHandler)
	
	// Live monitoring routes
	mux.HandleFunc("/ws-live", liveMonitoringWebSocket)
	mux.HandleFunc("/check-user-status", checkUserStatusHandler)
	mux.HandleFunc("/active-sessions", getActiveSessionsHandler)

	// Apply CORS middleware
	handler := corsMiddleware(mux)

	log.Println("📡 Server starting on http://127.0.0.1:9090")
	log.Println("📡 Live monitoring WebSocket available at ws://127.0.0.1:9090/ws-live")
	if err := http.ListenAndServe("127.0.0.1:9090", handler); err != nil {
		log.Fatal(err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}