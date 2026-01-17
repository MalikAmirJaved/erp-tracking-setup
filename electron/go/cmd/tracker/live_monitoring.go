// live_monitoring.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ────────────────────────────────────────────────
// WebRTC Signaling Server
// ────────────────────────────────────────────────

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // For development, allow all origins
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	// Active connections
	connections = struct {
		sync.RWMutex
		clients map[string]*ClientConnection
	}{clients: make(map[string]*ClientConnection)}
)

// ClientConnection represents a WebSocket connection
type ClientConnection struct {
	conn      *websocket.Conn
	userID    string
	companyID string
	role      string // "admin" or "user"
	peerID    string
}

// WebRTC Signaling Message
type SignalingMessage struct {
	Type      string          `json:"type"` // offer, answer, candidate, join, leave, error, status
	From      string          `json:"from"`
	To        string          `json:"to"`
	Data      json.RawMessage `json:"data"`
	PeerID    string          `json:"peerId"`
	UserID    string          `json:"userId"`
	CompanyID string          `json:"companyId"`
}

// LiveSession represents an active live monitoring session
type LiveSession struct {
	AdminConnection *ClientConnection
	UserConnection  *ClientConnection
	StartedAt       time.Time
	IsActive        bool
}

var (
	activeSessions = struct {
		sync.RWMutex
		sessions map[string]*LiveSession
	}{sessions: make(map[string]*LiveSession)}
)

// WebSocket handler for live monitoring signaling
func liveMonitoringWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Printf("i am heree:")
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Read initial connection info
	var initMsg SignalingMessage
	err = conn.ReadJSON(&initMsg)
	if err != nil {
		log.Printf("Failed to read initial message: %v", err)
		return
	}

	// Validate initial message
	if initMsg.Type != "join" || initMsg.UserID == "" || initMsg.CompanyID == "" {
		conn.WriteJSON(SignalingMessage{
			Type: "error",
			Data: json.RawMessage(`{"message": "Invalid join message"}`),
		})
		return
	}

	// Create client connection
	client := &ClientConnection{
		conn:      conn,
		userID:    initMsg.UserID,
		companyID: initMsg.CompanyID,
		role:      initMsg.From, // "admin" or "user"
		peerID:    initMsg.PeerID,
	}

	clientID := fmt.Sprintf("%s_%s_%s", initMsg.CompanyID, initMsg.UserID, initMsg.From)

	// Register client
	connections.Lock()
	connections.clients[clientID] = client
	connections.Unlock()

	log.Printf("Client connected: %s (Role: %s)", clientID, initMsg.From)

	// Send connection confirmation
	conn.WriteJSON(SignalingMessage{
		Type: "connected",
		From: "server",
		To:   initMsg.From,
		Data: json.RawMessage(fmt.Sprintf(`{"clientId": "%s"}`, clientID)),
	})

	// If admin joins, check if target user is online
	if initMsg.From == "admin" {
		targetUserID := string(initMsg.Data)
		userClientID := fmt.Sprintf("%s_%s_user", initMsg.CompanyID, targetUserID)

		connections.RLock()
		_, userExists := connections.clients[userClientID]
		connections.RUnlock()

		if userExists {
			// Notify admin that user is online
			conn.WriteJSON(SignalingMessage{
				Type: "user-status",
				From: "server",
				To:   "admin",
				Data: json.RawMessage(`{"online": true, "userId": "` + targetUserID + `"}`),
			})
		} else {
			conn.WriteJSON(SignalingMessage{
				Type: "user-status",
				From: "server",
				To:   "admin",
				Data: json.RawMessage(`{"online": false, "userId": "` + targetUserID + `"}`),
			})
		}
	}

	// Handle messages
	for {
		var msg SignalingMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Printf("Client disconnected: %v", err)
			break
		}

		handleSignalingMessage(client, msg)
	}

	// Cleanup on disconnect
	connections.Lock()
	delete(connections.clients, clientID)
	connections.Unlock()

	// Clean up any active sessions
	activeSessions.Lock()
	for sessionID, session := range activeSessions.sessions {
		if (session.AdminConnection != nil && session.AdminConnection.userID == client.userID) ||
			(session.UserConnection != nil && session.UserConnection.userID == client.userID) {
			session.IsActive = false

			// Notify other party
			if client.role == "admin" && session.UserConnection != nil {
				session.UserConnection.conn.WriteJSON(SignalingMessage{
					Type: "session-ended",
					From: "server",
					To:   "user",
					Data: json.RawMessage(`{"reason": "admin disconnected"}`),
				})
			} else if client.role == "user" && session.AdminConnection != nil {
				session.AdminConnection.conn.WriteJSON(SignalingMessage{
					Type: "session-ended",
					From: "server",
					To:   "admin",
					Data: json.RawMessage(`{"reason": "user disconnected"}`),
				})
			}

			delete(activeSessions.sessions, sessionID)
		}
	}
	activeSessions.Unlock()

	log.Printf("Client disconnected: %s", clientID)
}

func handleSignalingMessage(client *ClientConnection, msg SignalingMessage) {
	switch msg.Type {
	case "offer":
		// Forward offer to target user
		forwardToTarget(client, msg)
	case "answer":
		// Forward answer to admin
		forwardToTarget(client, msg)
	case "candidate":
		// Forward ICE candidate
		forwardToTarget(client, msg)
	case "request-live":
		handleLiveRequest(client, msg)
	case "accept-live":
		handleLiveAccept(client, msg)
	case "reject-live":
		handleLiveReject(client, msg)
	case "end-live":
		handleLiveEnd(client, msg)
	}
}

func forwardToTarget(sender *ClientConnection, msg SignalingMessage) {
	targetID := fmt.Sprintf("%s_%s_%s", sender.companyID, msg.To, getOppositeRole(sender.role))

	connections.RLock()
	targetClient, exists := connections.clients[targetID]
	connections.RUnlock()

	if exists {
		// Update the message to include sender info
		msg.From = sender.userID
		err := targetClient.conn.WriteJSON(msg)
		if err != nil {
			log.Printf("Failed to forward message to %s: %v", targetID, err)
		}
	}
}

func handleLiveRequest(adminClient *ClientConnection, msg SignalingMessage) {
	userID := string(msg.Data)
	userClientID := fmt.Sprintf("%s_%s_user", adminClient.companyID, userID)

	connections.RLock()
	userClient, userExists := connections.clients[userClientID]
	connections.RUnlock()

	if !userExists {
		adminClient.conn.WriteJSON(SignalingMessage{
			Type: "error",
			From: "server",
			To:   "admin",
			Data: json.RawMessage(`{"message": "User not online"}`),
		})
		return
	}

	// Send live request to user
	requestMsg := SignalingMessage{
		Type:      "live-request",
		From:      adminClient.userID,
		To:        userID,
		UserID:    adminClient.userID,
		CompanyID: adminClient.companyID,
		Data:      json.RawMessage(fmt.Sprintf(`{"adminId": "%s", "companyId": "%s"}`, adminClient.userID, adminClient.companyID)),
	}

	err := userClient.conn.WriteJSON(requestMsg)
	if err != nil {
		log.Printf("Failed to send live request: %v", err)
		adminClient.conn.WriteJSON(SignalingMessage{
			Type: "error",
			From: "server",
			To:   "admin",
			Data: json.RawMessage(`{"message": "Failed to send request"}`),
		})
	}
}

func handleLiveAccept(userClient *ClientConnection, msg SignalingMessage) {
	adminID := string(msg.Data)
	adminClientID := fmt.Sprintf("%s_%s_admin", userClient.companyID, adminID)

	connections.RLock()
	adminClient, adminExists := connections.clients[adminClientID]
	connections.RUnlock()

	if !adminExists {
		userClient.conn.WriteJSON(SignalingMessage{
			Type: "error",
			From: "server",
			To:   "user",
			Data: json.RawMessage(`{"message": "Admin not found"}`),
		})
		return
	}

	// Create session
	sessionID := fmt.Sprintf("%s_%s_%s", userClient.companyID, adminID, userClient.userID)
	session := &LiveSession{
		AdminConnection: adminClient,
		UserConnection:  userClient,
		StartedAt:       time.Now(),
		IsActive:        true,
	}

	activeSessions.Lock()
	activeSessions.sessions[sessionID] = session
	activeSessions.Unlock()

	// Notify admin
	adminClient.conn.WriteJSON(SignalingMessage{
		Type: "live-accepted",
		From: userClient.userID,
		To:   adminID,
		Data: json.RawMessage(fmt.Sprintf(`{"sessionId": "%s", "userId": "%s"}`, sessionID, userClient.userID)),
	})

	log.Printf("Live session started: %s", sessionID)
}

func handleLiveReject(userClient *ClientConnection, msg SignalingMessage) {
	adminID := string(msg.Data)
	adminClientID := fmt.Sprintf("%s_%s_admin", userClient.companyID, adminID)

	connections.RLock()
	adminClient, adminExists := connections.clients[adminClientID]
	connections.RUnlock()

	if adminExists {
		adminClient.conn.WriteJSON(SignalingMessage{
			Type: "live-rejected",
			From: userClient.userID,
			To:   adminID,
			Data: json.RawMessage(`{"message": "User rejected live monitoring request"}`),
		})
	}
}

func handleLiveEnd(client *ClientConnection, msg SignalingMessage) {
	sessionID := string(msg.Data)

	activeSessions.Lock()
	session, exists := activeSessions.sessions[sessionID]
	if exists {
		session.IsActive = false

		// Notify other party
		if client.role == "admin" && session.UserConnection != nil {
			session.UserConnection.conn.WriteJSON(SignalingMessage{
				Type: "session-ended",
				From: "server",
				To:   "user",
				Data: json.RawMessage(`{"reason": "ended by admin"}`),
			})
		} else if client.role == "user" && session.AdminConnection != nil {
			session.AdminConnection.conn.WriteJSON(SignalingMessage{
				Type: "session-ended",
				From: "server",
				To:   "admin",
				Data: json.RawMessage(`{"reason": "ended by user"}`),
			})
		}

		delete(activeSessions.sessions, sessionID)
		log.Printf("Live session ended: %s", sessionID)
	}
	activeSessions.Unlock()
}

func getOppositeRole(role string) string {
	if role == "admin" {
		return "user"
	}
	return "admin"
}

// HTTP endpoint to check if user is available for live monitoring
func checkUserStatusHandler(w http.ResponseWriter, r *http.Request) {

	// Handle CORS preflight (VERY important if frontend is separate)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()

	type StatusRequest struct {
		UserID    string `json:"userId"`
		CompanyID string `json:"companyId"`
	}

	var req StatusRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		log.Println("❌ Decode error:", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// ✅ NOW log it
	log.Println("✅ Decoded request:", req)

	if req.UserID == "" || req.CompanyID == "" {
		http.Error(w, "userId and companyId are required", http.StatusBadRequest)
		return
	}

	userClientID := fmt.Sprintf("%s_%s_user", req.CompanyID, req.UserID)

	connections.RLock()
	_, exists := connections.clients[userClientID]
	connections.RUnlock()

	response := map[string]interface{}{
		"online": exists,
		"userId": req.UserID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}


// HTTP endpoint to get active live sessions for admin
func getActiveSessionsHandler(w http.ResponseWriter, r *http.Request) {
	companyID := r.URL.Query().Get("companyId")
	adminID := r.URL.Query().Get("adminId")

	activeSessions.RLock()
	sessions := []map[string]interface{}{}
	for sessionID, session := range activeSessions.sessions {
		if session.AdminConnection != nil &&
			session.AdminConnection.companyID == companyID &&
			session.AdminConnection.userID == adminID &&
			session.IsActive {
			sessions = append(sessions, map[string]interface{}{
				"sessionId": sessionID,
				"userId":    session.UserConnection.userID,
				"startedAt": session.StartedAt,
				"duration":  time.Since(session.StartedAt).String(),
			})
		}
	}
	activeSessions.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}
