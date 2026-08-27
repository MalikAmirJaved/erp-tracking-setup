// live_monitoring.go
// Contains all live monitoring WebSocket logic for the employee agent
// This file is part of the same binary as main.go

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const CENTRAL_WS_URL = "ws://192.168.88.33:3002/ws/live"

var centralConn *websocket.Conn
var centralConnMu sync.Mutex
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

func connectToCentralServer() {
	centralConnMu.Lock()
	if centralConn != nil {
		centralConn.Close()
	}
	centralConnMu.Unlock()

	for {
		conn, _, err := websocket.DefaultDialer.Dial(CENTRAL_WS_URL, nil)
		if err != nil {
			// log.Printf("Cannot connect to central signaling server: %v — retry in 8s", err)
			time.Sleep(8 * time.Second)
			continue
		}

		// log.Println("Connected to central signaling server")

		centralConnMu.Lock()
		centralConn = conn
		centralConnMu.Unlock()

		// Join as user
		mu.Lock()
		u := currentUser
		mu.Unlock()

		if u == nil {
			conn.Close()
			continue
		}

		join := map[string]interface{}{
			"type": "join-live",
			"data": map[string]string{
				"From":      "user",
				"UserID":    u.UserID,
				"CompanyID": u.CompanyID,
				"PeerID":    "user_" + u.UserID,
			},
		}

		if err := conn.WriteJSON(join); err != nil {
			conn.Close()
			continue
		}

		// Forward messages from central → local popup
		go func() {
			for {
				var msg map[string]interface{}
				if err := conn.ReadJSON(&msg); err != nil {
					log.Printf("Central WS read error: %v", err)
					centralConnMu.Lock()
					if conn == centralConn {
						centralConn = nil
					}
					centralConnMu.Unlock()
					break
				}

				// Forward important messages to all connected local clients (popups)
				connections.RLock()
				for _, c := range connections.clients {
					_ = c.conn.WriteJSON(msg)
				}
				connections.RUnlock()

			}
		}()

		// Keep connection alive
		go func() {
			for {
				time.Sleep(25 * time.Second)
				centralConnMu.Lock()
				if conn != centralConn {
					centralConnMu.Unlock()
					return
				}
				conn.WriteMessage(websocket.PingMessage, nil)
				centralConnMu.Unlock()
			}
		}()

		// Wait for disconnect
		<-time.After(365 * 24 * time.Hour) // practically forever
	}
}

// WebSocket handler for local popup (localhost:9090/ws-live)
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
		role:      joinMsg.From,
		peerID:    peerID,
	}

	connections.Lock()
	connections.clients[peerID] = client
	connections.Unlock()

	log.Printf("🟢 Local popup connected: %s (%s)", peerID, client.role)

	conn.WriteJSON(SignalingMessage{
		Type: "connected",
		Data: json.RawMessage(fmt.Sprintf(`{"peerId":"%s"}`, peerID)),
	})

	for {
		var msg SignalingMessage
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("🔴 Local popup disconnected: %s (%v)", peerID, err)
			break
		}

		// Forward messages from popup to central (mainly accept/reject/offer/answer/candidate)
		if msg.Type == "accept-live" || msg.Type == "reject-live" ||
			strings.HasPrefix(msg.Type, "webrtc-") {
			wsMutex.Lock()
			if wsConn != nil {
				msg.From = peerID
				wsConn.WriteJSON(msg)
			}
			wsMutex.Unlock()
		}
	}

	connections.Lock()
	delete(connections.clients, peerID)
	connections.Unlock()

	cleanupSessionsForClient(client)
	log.Printf("⚫ Local popup disconnected: %s", peerID)
}

func handleSignalingMessage(client *ClientConnection, msg SignalingMessage) {
	switch msg.Type {

	case "live-request":
		if !liveMonitoringEnabled {
			client.conn.WriteJSON(SignalingMessage{
				Type: "live-rejected",
				Data: json.RawMessage(`{"reason":"monitoring-disabled"}`),
			})
			return
		}

		// auto-accept
		adminID := strings.TrimPrefix(msg.From, "admin_")

		client.conn.WriteJSON(SignalingMessage{
			Type: "accept-live",
			Data: json.RawMessage(fmt.Sprintf(`"%s"`, adminID)),
		})
		return

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

	if exists {
		msg.From = sender.peerID
		target.conn.WriteJSON(msg)
		return
	}

	// If target is remote (admin), forward to central server
	if strings.HasPrefix(payload.To, "admin_") {
		wsMutex.Lock()
		if wsConn != nil {
			msg.From = sender.peerID
			wsConn.WriteJSON(msg)
		}
		wsMutex.Unlock()
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
