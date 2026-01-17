// live_monitoring.go
// Contains all live monitoring WebSocket logic for the employee agent
// This file is part of the same binary as main.go

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

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	connections = struct {
		sync.RWMutex
		clients map[string]*ClientConnection // key = company_user_role
	}{
		clients: make(map[string]*ClientConnection),
	}

	activeSessions = struct {
		sync.RWMutex
		sessions map[string]*LiveSession
	}{
		sessions: make(map[string]*LiveSession),
	}
)

type ClientConnection struct {
	conn      *websocket.Conn
	userID    string
	companyID string
	role      string // "admin" or "user"
	peerID    string // "admin_xxx" or "user_xxx"
}

type LiveSession struct {
	AdminConnection *ClientConnection
	UserConnection  *ClientConnection
	StartedAt       time.Time
	IsActive        bool
}

type SignalingMessage struct {
	Type      string          `json:"type"`
	From      string          `json:"from,omitempty"`
	To        string          `json:"to,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	PeerID    string          `json:"peerId,omitempty"`
	UserID    string          `json:"userId,omitempty"`
	CompanyID string          `json:"companyId,omitempty"`
}

// WebSocket handler for live monitoring (employee agent side)
func liveMonitoringWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	var initMsg SignalingMessage
	if err := conn.ReadJSON(&initMsg); err != nil {
		log.Printf("Failed to read initial join message: %v", err)
		return
	}

	if initMsg.Type != "join-live" || initMsg.UserID == "" || initMsg.CompanyID == "" || initMsg.PeerID == "" {
		conn.WriteJSON(SignalingMessage{
			Type: "error",
			Data: json.RawMessage(`{"message":"invalid join message"}`),
		})
		return
	}

	client := &ClientConnection{
		conn:      conn,
		userID:    initMsg.UserID,
		companyID: initMsg.CompanyID,
		role:      initMsg.From, // "admin" or "user"
		peerID:    initMsg.PeerID,
	}

	clientKey := fmt.Sprintf("%s_%s_%s", initMsg.CompanyID, initMsg.UserID, initMsg.From)

	connections.Lock()
	connections.clients[clientKey] = client
	connections.Unlock()

	log.Printf("Live monitoring client connected: %s (role: %s)", clientKey, client.role)

	// Send confirmation
	conn.WriteJSON(SignalingMessage{
		Type: "connected",
		From: "server",
		Data: json.RawMessage(fmt.Sprintf(`{"clientId":"%s"}`, clientKey)),
	})

	// Message loop
	for {
		var msg SignalingMessage
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("WebSocket read error for %s: %v", clientKey, err)
			break
		}

		handleSignalingMessage(client, msg)
	}

	// Cleanup
	connections.Lock()
	delete(connections.clients, clientKey)
	connections.Unlock()

	cleanupSessionsForClient(client)

	log.Printf("Live monitoring client disconnected: %s", clientKey)
}

func handleSignalingMessage(client *ClientConnection, msg SignalingMessage) {
	switch msg.Type {
	case "request-live":
		// Only admins should send this — ignore if not admin
		if client.role != "admin" {
			return
		}
		handleLiveRequest(client, msg)

	case "accept-live":
		if client.role != "user" {
			return
		}
		handleLiveAccept(client, msg)

	case "reject-live":
		if client.role != "user" {
			return
		}
		handleLiveReject(client, msg)

	case "end-live":
		handleLiveEnd(client, msg)

	case "webrtc-offer", "webrtc-answer", "webrtc-candidate":
		forwardToTarget(client, msg)
	}
}

func forwardToTarget(sender *ClientConnection, msg SignalingMessage) {
	var targetPeer string
	switch msg.Type {
	case "webrtc-offer", "webrtc-answer", "webrtc-candidate":
		var payload struct {
			To string `json:"to"`
		}
		if err := json.Unmarshal(msg.Data, &payload); err == nil && payload.To != "" {
			targetPeer = payload.To
		}
	}

	if targetPeer == "" {
		return
	}

	connections.RLock()
	target, exists := connections.clients[targetPeer]
	connections.RUnlock()

	if exists && target.conn != nil {
		msg.From = sender.peerID
		if err := target.conn.WriteJSON(msg); err != nil {
			log.Printf("Failed to forward %s to %s: %v", msg.Type, targetPeer, err)
		}
	}
}

func handleLiveRequest(admin *ClientConnection, msg SignalingMessage) {
	var targetUserID string
	if err := json.Unmarshal(msg.Data, &targetUserID); err != nil {
		return
	}

	userKey := fmt.Sprintf("%s_%s_user", admin.companyID, targetUserID)

	connections.RLock()
	userConn, exists := connections.clients[userKey]
	connections.RUnlock()

	if !exists {
		admin.conn.WriteJSON(SignalingMessage{
			Type: "error",
			Data: json.RawMessage(`{"msg":"User offline"}`),
		})
		return
	}

	userConn.conn.WriteJSON(SignalingMessage{
		Type:      "live-request",
		From:      admin.userID,
		To:        targetUserID,
		CompanyID: admin.companyID,
		Data:      json.RawMessage(fmt.Sprintf(`{"adminId":"%s"}`, admin.userID)),
	})
}

func handleLiveAccept(user *ClientConnection, msg SignalingMessage) {
	var adminID string
	if err := json.Unmarshal(msg.Data, &adminID); err != nil {
		return
	}

	adminKey := fmt.Sprintf("%s_%s_admin", user.companyID, adminID)

	connections.RLock()
	adminConn, ok := connections.clients[adminKey]
	connections.RUnlock()

	if !ok {
		return
	}

	sessionID := fmt.Sprintf("sess_%s_%s_%s", user.companyID, adminID, user.userID)

	session := &LiveSession{
		AdminConnection: adminConn,
		UserConnection:  user,
		StartedAt:       time.Now(),
		IsActive:        true,
	}

	activeSessions.Lock()
	activeSessions.sessions[sessionID] = session
	activeSessions.Unlock()

	adminConn.conn.WriteJSON(SignalingMessage{
		Type: "live-accepted",
		From: user.userID,
		Data: json.RawMessage(fmt.Sprintf(`{"sessionId":"%s","userId":"%s"}`, sessionID, user.userID)),
	})
}

func handleLiveReject(user *ClientConnection, msg SignalingMessage) {
	var adminID string
	if err := json.Unmarshal(msg.Data, &adminID); err != nil {
		return
	}

	adminKey := fmt.Sprintf("%s_%s_admin", user.companyID, adminID)

	connections.RLock()
	admin, ok := connections.clients[adminKey]
	connections.RUnlock()

	if ok {
		admin.conn.WriteJSON(SignalingMessage{
			Type: "live-rejected",
			From: user.userID,
			Data: json.RawMessage(`{"msg":"rejected"}`),
		})
	}
}

func handleLiveEnd(client *ClientConnection, msg SignalingMessage) {
	var sessionID string
	if err := json.Unmarshal(msg.Data, &sessionID); err != nil {
		return
	}

	activeSessions.Lock()
	session, exists := activeSessions.sessions[sessionID]
	if exists {
		session.IsActive = false

		if client.role == "admin" && session.UserConnection != nil {
			session.UserConnection.conn.WriteJSON(SignalingMessage{
				Type: "session-ended",
				Data: json.RawMessage(`{"reason":"ended by admin"}`),
			})
		} else if client.role == "user" && session.AdminConnection != nil {
			session.AdminConnection.conn.WriteJSON(SignalingMessage{
				Type: "session-ended",
				Data: json.RawMessage(`{"reason":"ended by user"}`),
			})
		}

		delete(activeSessions.sessions, sessionID)
	}
	activeSessions.Unlock()
}

func cleanupSessionsForClient(client *ClientConnection) {
	activeSessions.Lock()
	for id, s := range activeSessions.sessions {
		if (s.AdminConnection != nil && s.AdminConnection.userID == client.userID) ||
			(s.UserConnection != nil && s.UserConnection.userID == client.userID) {
			s.IsActive = false
			delete(activeSessions.sessions, id)
		}
	}
	activeSessions.Unlock()
}

// ────────────────────────────────────────────────
// Optional HTTP endpoints (if needed by frontend)
// ────────────────────────────────────────────────

func checkUserStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CompanyID string `json:"companyId"`
		UserID    string `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("%s_%s_user", req.CompanyID, req.UserID)

	connections.RLock()
	_, online := connections.clients[key]
	connections.RUnlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"online": online,
		"userId": req.UserID,
	})
}

func getActiveSessionsHandler(w http.ResponseWriter, r *http.Request) {
	companyID := r.URL.Query().Get("companyId")
	adminID := r.URL.Query().Get("adminId")

	activeSessions.RLock()
	defer activeSessions.RUnlock()

	var sessions []map[string]interface{}
	for id, s := range activeSessions.sessions {
		if s.AdminConnection != nil &&
			s.AdminConnection.companyID == companyID &&
			s.AdminConnection.userID == adminID &&
			s.IsActive {
			sessions = append(sessions, map[string]interface{}{
				"sessionId": id,
				"userId":    s.UserConnection.userID,
				"startedAt": s.StartedAt.Format(time.RFC3339),
			})
		}
	}

	json.NewEncoder(w).Encode(sessions)
}