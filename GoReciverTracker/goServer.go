// Go server

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	listenAddr    = "192.168.88.33:3002"
	baseDir       = `D:\UsersTrackingScreenShots`
	maxUploadSize = 80 << 20 // 80 MB
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Config struct {
	MaxUploadSize int64
	BaseDir       string
}

var config = Config{
	MaxUploadSize: maxUploadSize,
	BaseDir:       baseDir,
}

// ────────────────────────────────────────────────
// Global state
// ────────────────────────────────────────────────

type Client struct {
	conn      *websocket.Conn
	peerID    string // format: "admin_user123" or "user_emp456"
	userID    string
	companyID string
	role      string // "admin" or "user"
}

type LiveSession struct {
	ID        string
	Admin     *Client
	User      *Client
	StartedAt time.Time
	Active    bool
}

var (
	connections = struct {
		sync.RWMutex
		clients map[string]*Client // key = peerID
	}{
		clients: make(map[string]*Client),
	}

	activeSessions = struct {
		sync.RWMutex
		sessions map[string]*LiveSession // key = session ID
	}{
		sessions: make(map[string]*LiveSession),
	}
)

// ────────────────────────────────────────────────
// Message structure used in WebSocket
// ────────────────────────────────────────────────

type Message struct {
	Type      string          `json:"type"`
	From      string          `json:"from,omitempty"`
	To        string          `json:"to,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	PeerID    string          `json:"peerId,omitempty"`
	UserID    string          `json:"userId,omitempty"`
	CompanyID string          `json:"companyId,omitempty"`
}

// ────────────────────────────────────────────────
// Main entry point
// ────────────────────────────────────────────────

func main() {
	if err := os.MkdirAll(config.BaseDir, 0755); err != nil {
		log.Fatal("Cannot create screenshot directory:", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/upload-screenshot", uploadScreenshotHandler)
	mux.HandleFunc("/health", healthCheckHandler)
	mux.HandleFunc("/ws/live", signalingWebSocketHandler)
	mux.HandleFunc("/check-user-status", checkUserStatusHandler)

	log.Printf("Server listening on %s", listenAddr)

	// ✅ Wrap with CORS
	handler := withCORS(mux)

	log.Fatal(http.ListenAndServe(listenAddr, handler))
}

// ────────────────────────────────────────────────
// Screenshot upload endpoint
// ────────────────────────────────────────────────

func uploadScreenshotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(config.MaxUploadSize); err != nil {
		http.Error(w, "File too large or bad form", http.StatusBadRequest)
		return
	}

	companyId := r.FormValue("companyId")
	userId := r.FormValue("userId")
	localIP := r.FormValue("localIP")
	mac := r.FormValue("mac")
	timestamp := r.FormValue("timestamp")
	fileType := r.FormValue("type")
	encrypted := r.FormValue("encrypted")
	fileHash := r.FormValue("fileHash")

	if companyId == "" || userId == "" || localIP == "" || mac == "" || timestamp == "" || fileType == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("screenshot")
	if err != nil {
		http.Error(w, "Cannot read file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Cannot read file content", http.StatusInternalServerError)
		return
	}

	if fileHash != "" {
		calcHash := sha256.Sum256(data)
		expected := base64.StdEncoding.EncodeToString(calcHash[:])
		if expected != fileHash {
			http.Error(w, "File hash mismatch", http.StatusBadRequest)
			return
		}
	}

	saveDir := filepath.Join(config.BaseDir, sanitize(companyId), sanitize(userId))
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		http.Error(w, "Cannot create user directory", http.StatusInternalServerError)
		return
	}

	ext := ".png"
	if encrypted == "true" {
		ext = ".enc"
	}
	if h := header.Filename; filepath.Ext(h) != "" {
		ext = filepath.Ext(h)
	}

	filename := fmt.Sprintf("%s__%s__%s__%s__%s__%s%s",
		sanitize(companyId), sanitize(userId), sanitize(localIP),
		sanitize(mac), sanitize(timestamp), sanitize(fileType), ext)

	savePath := filepath.Join(saveDir, filename)

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		http.Error(w, "Cannot save file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "success",
		"filename": filename,
		"size":     len(data),
	})
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(" /\\:*?\"<>|", r) {
			return '_'
		}
		return r
	}, s)
}

// ────────────────────────────────────────────────
// Health check
// ────────────────────────────────────────────────

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"screenshot+live-signaling"}`))
}

// ────────────────────────────────────────────────
// WebSocket signaling endpoint (/ws/live)
// ────────────────────────────────────────────────

func signalingWebSocketHandler(w http.ResponseWriter, r *http.Request) {

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	var client *Client

	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}

		switch msg.Type {
		case "join-live":
			var join struct {
				From      string `json:"From"`
				UserID    string `json:"UserID"`
				CompanyID string `json:"CompanyID"`
				PeerID    string `json:"PeerID"`
			}
			if err := json.Unmarshal(msg.Data, &join); err != nil {
				conn.WriteJSON(Message{Type: "error", Data: json.RawMessage(`{"msg":"bad join format"}`)})
				return
			}

			client = &Client{
				conn:      conn,
				peerID:    join.PeerID,
				userID:    join.UserID,
				companyID: join.CompanyID,
				role:      join.From,
			}

			connections.Lock()
			connections.clients[join.PeerID] = client
			connections.Unlock()

			conn.WriteJSON(Message{Type: "joined"})

		case "request-live":
			if client.role != "admin" {
				continue
			}

			var targetUserID string
			_ = json.Unmarshal(msg.Data, &targetUserID)

			targetPeer := "user_" + targetUserID

			connections.RLock()
			target, exists := connections.clients[targetPeer]
			connections.RUnlock()

			if !exists {
				// User offline → tell admin immediately
				client.conn.WriteJSON(Message{
					Type: "live-not-available",
					Data: json.RawMessage(`{"reason":"user-offline","userId":"` + targetUserID + `"}`),
				})
				continue
			}

			// ── Auto accept ───────────────────────────────────────
			sessionID := fmt.Sprintf("sess-%s-%s-%s", client.companyID, client.userID, targetUserID)

			sess := &LiveSession{
				ID:        sessionID,
				Admin:     client,
				User:      target,
				StartedAt: time.Now(),
				Active:    true,
			}

			activeSessions.Lock()
			activeSessions.sessions[sessionID] = sess
			activeSessions.Unlock()

			// Tell admin: session created, waiting for WebRTC
			client.conn.WriteJSON(Message{
				Type: "live-auto-accepted",
				Data: json.RawMessage(fmt.Sprintf(`{"sessionId":"%s","userId":"%s"}`, sessionID, targetUserID)),
			})

			// Tell user: monitoring starting (optional – can be silent)
			target.conn.WriteJSON(Message{
				Type: "monitoring-started",
				From: client.userID, // admin id
				Data: json.RawMessage(`{"adminId":"` + client.userID + `","sessionId":"` + sessionID + `"}`),
			})

		case "accept-live":
			if client == nil || client.role != "user" {
				continue
			}
			var adminID string
			_ = json.Unmarshal(msg.Data, &adminID)

			adminPeer := "admin_" + adminID

			connections.RLock()
			admin, ok := connections.clients[adminPeer]
			connections.RUnlock()

			if !ok {
				continue
			}

			sessionID := fmt.Sprintf("sess-%s-%s-%s", client.companyID, adminID, client.userID)

			sess := &LiveSession{
				ID:        sessionID,
				Admin:     admin,
				User:      client,
				StartedAt: time.Now(),
				Active:    true,
			}

			activeSessions.Lock()
			activeSessions.sessions[sessionID] = sess
			activeSessions.Unlock()

			admin.conn.WriteJSON(Message{
				Type: "live-accepted",
				Data: json.RawMessage(fmt.Sprintf(`{"sessionId":"%s","userId":"%s"}`, sessionID, client.userID)),
			})

		case "reject-live":
			// forward reject (you can implement similarly to accept)

		case "end-live":
			// forward end (you can implement similarly)

		case "webrtc-offer", "webrtc-answer", "webrtc-candidate":
			var target string
			_ = json.Unmarshal(msg.Data, &struct{ To *string }{&target})
			if target == "" {
				continue
			}

			connections.RLock()
			dest, ok := connections.clients[target]
			connections.RUnlock()

			if ok {
				dest.conn.WriteJSON(msg)
			}
		}
	}

	// Cleanup on disconnect
	if client != nil {
		connections.Lock()
		delete(connections.clients, client.peerID)
		connections.Unlock()

		// End sessions this client was in
		activeSessions.Lock()
		for id, s := range activeSessions.sessions {
			if (s.Admin != nil && s.Admin.peerID == client.peerID) ||
				(s.User != nil && s.User.peerID == client.peerID) {
				s.Active = false
				delete(activeSessions.sessions, id)

				other := s.User
				if client.role == "user" {
					other = s.Admin
				}
				if other != nil && other.conn != nil {
					other.conn.WriteJSON(Message{
						Type: "session-ended",
						Data: json.RawMessage(`{"reason":"peer disconnected"}`),
					})
				}
			}
		}
		activeSessions.Unlock()
	}
}

// ────────────────────────────────────────────────
// Check if user is online
// ────────────────────────────────────────────────

func checkUserStatusHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("📡 Checking active user")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CompanyID string `json:"companyId"`
		UserID    string `json:"userId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	targetPeer := "user_" + req.UserID

	connections.RLock()
	_, exists := connections.clients[targetPeer]
	connections.RUnlock()

	log.Printf("🟢 Online check: %s → %v", targetPeer, exists)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{
		"online": exists,
	})
}

func logAllActiveUsers() {
	connections.RLock()
	defer connections.RUnlock()

	log.Println("🟢 Active WebSocket Clients:")

	if len(connections.clients) == 0 {
		log.Println("   (none)")
		return
	}

	for peerID, client := range connections.clients {
		log.Printf(
			"   peerID=%s | userID=%s | company=%s | role=%s\n",
			peerID,
			client.userID,
			client.companyID,
			client.role,
		)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Allow your frontend
		w.Header().Set("Access-Control-Allow-Origin", "http://192.168.88.33:3001")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle preflight
		if r.Method == http.MethodOptions {
			log.Println("✅ CORS preflight:", r.URL.Path)
			log.Println("✅ CORS rrrr   j:", r)
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
