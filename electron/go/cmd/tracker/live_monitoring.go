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
		log.Println("WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	var joinMsg SignalingMessage
	if err := conn.ReadJSON(&joinMsg); err != nil {
		log.Println("Failed to read join message:", err)
		return
	}

	if joinMsg.Type != "join-live" ||
		joinMsg.UserID == "" ||
		joinMsg.CompanyID == "" ||
		joinMsg.From == "" {
		conn.WriteJSON(SignalingMessage{
			Type: "error",
			Data: json.RawMessage(`{"msg":"invalid join message"}`),
		})
		return
	}

	peerID := fmt.Sprintf("%s_%s", joinMsg.From, joinMsg.UserID)

	client := &ClientConnection{
		conn:      conn,
		userID:    joinMsg.UserID,
		companyID: joinMsg.CompanyID,
		role:      joinMsg.From, // admin | user
		peerID:    peerID,
	}

	connections.Lock()
	connections.clients[peerID] = client
	connections.Unlock()

	log.Printf("🟢 Live client connected: %s (%s)", peerID, client.role)

	conn.WriteJSON(SignalingMessage{
		Type: "connected",
		Data: json.RawMessage(fmt.Sprintf(`{"peerId":"%s"}`, peerID)),
	})

	for {
		var msg SignalingMessage
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("🔴 WebSocket closed: %s (%v)", peerID, err)
			break
		}
		handleSignalingMessage(client, msg)
	}

	connections.Lock()
	delete(connections.clients, peerID)
	connections.Unlock()

	cleanupSessionsForClient(client)

	log.Printf("⚫ Live client disconnected: %s", peerID)
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
	var payload struct {
		To string `json:"to"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil || payload.To == "" {
		return
	}

	connections.RLock()
	target, exists := connections.clients[payload.To]
	connections.RUnlock()

	if !exists {
		log.Printf("⚠️ Target not found: %s", payload.To)
		return
	}

	msg.From = sender.peerID

	if err := target.conn.WriteJSON(msg); err != nil {
		log.Printf("❌ Forward failed (%s → %s): %v", sender.peerID, payload.To, err)
	}
}


func handleLiveRequest(admin *ClientConnection, msg SignalingMessage) {
	var payload struct {
		UserID string `json:"userId"`
	}

	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Println("Invalid request-live payload:", err)
		return
	}

	targetPeer := "user_" + payload.UserID

	connections.RLock()
	userConn, exists := connections.clients[targetPeer]
	connections.RUnlock()

	if !exists {
		admin.conn.WriteJSON(SignalingMessage{
			Type: "error",
			Data: json.RawMessage(`{"msg":"User offline"}`),
		})
		return
	}

	log.Printf("📡 Live request: admin=%s → user=%s", admin.userID, payload.UserID)

	userConn.conn.WriteJSON(SignalingMessage{
		Type:      "live-request",
		From:      admin.peerID,
		To:        targetPeer,
		CompanyID: admin.companyID,
		Data:      json.RawMessage(fmt.Sprintf(`{"adminId":"%s"}`, admin.userID)),
	})
}



func handleLiveAccept(user *ClientConnection, msg SignalingMessage) {
	var adminID string
	if err := json.Unmarshal(msg.Data, &adminID); err != nil {
		return
	}

	adminPeer := "admin_" + adminID

	connections.RLock()
	adminConn, ok := connections.clients[adminPeer]
	connections.RUnlock()

	if !ok {
		return
	}

	sessionID := fmt.Sprintf("sess_%s_%s_%s",
		user.companyID,
		adminConn.userID,
		user.userID,
	)

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
