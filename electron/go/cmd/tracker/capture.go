// Go client

package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kbinani/screenshot"
)

const SERVER_URL = "http://127.0.0.1:3002/upload-screenshot"
const WS_URL = "ws://127.0.0.1:3002/ws/live"

var ENCRYPTION_KEY = []byte("this-is-32-byte-long-key-for-aes")

type UserInfo struct {
	UserID    string `json:"userId"`
	CompanyID string `json:"companyId"`
	Name      string `json:"name"`
}

type CaptureType string

const (
	CaptureStart   CaptureType = "start"
	CaptureRegular CaptureType = "regular"
	CaptureBreak   CaptureType = "break"
	CaptureResume  CaptureType = "resume"
	CaptureStop    CaptureType = "stop"
)

var (
	mu            sync.Mutex
	isRunning     bool
	isOnBreak     bool
	ticker        *time.Ticker
	stopChan      chan struct{}
	currentUser   *UserInfo
	localIP       string
	macAddress    string
	wsConn        *websocket.Conn
	wsMutex       sync.Mutex
)

// ────────────────────────────────────────────────
// WebSocket management
// ────────────────────────────────────────────────

func connectWebSocket() bool {
	mu.Lock()
	user := currentUser
	mu.Unlock()

	if user == nil {
		return false
	}

	wsMutex.Lock()
	defer wsMutex.Unlock()

	if wsConn != nil {
		wsConn.Close()
		wsConn = nil
	}

	conn, _, err := websocket.DefaultDialer.Dial(WS_URL, nil)
	if err != nil {
		log.Printf("WebSocket connect failed: %v", err)
		return false
	}

	peerID := "user_" + user.UserID
	joinMsg := map[string]interface{}{
		"type": "join-live",
		"data": map[string]string{
			"From":      "user",
			"UserID":    user.UserID,
			"CompanyID": user.CompanyID,
			"PeerID":    peerID,
		},
	}
	if err := conn.WriteJSON(joinMsg); err != nil {
		conn.Close()
		return false
	}

	var resp map[string]string
	if err := conn.ReadJSON(&resp); err != nil || resp["type"] != "joined" {
		conn.Close()
		return false
	}

	wsConn = conn
	log.Println("WebSocket connected to server")
	go wsKeepAlive(conn)
	return true
}

func wsKeepAlive(conn *websocket.Conn) {
	for {
		time.Sleep(30 * time.Second)
		wsMutex.Lock()
		if conn == wsConn {
			conn.WriteMessage(websocket.PingMessage, nil)
		}
		wsMutex.Unlock()
	}
}

func closeWebSocket() {
	wsMutex.Lock()
	if wsConn != nil {
		wsConn.Close()
		wsConn = nil
		log.Println("WebSocket disconnected")
	}
	wsMutex.Unlock()
}

// ────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "unknown-ip"
	}
	defer conn.Close()
	ip := conn.LocalAddr().(*net.UDPAddr).IP.String()
	return strings.Replace(ip, ".", "-", -1)
}

func getMACAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "unknown-mac"
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0 && len(i.HardwareAddr) > 0 {
			return strings.Replace(i.HardwareAddr.String(), ":", "-", -1)
		}
	}
	return "unknown-mac"
}

func encryptData(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(ENCRYPTION_KEY)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, data, nil), nil
}

func generateFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

// ────────────────────────────────────────────────
// Core capture logic
// ────────────────────────────────────────────────

func CaptureScreen(captureType CaptureType) error {
	mu.Lock()
	user := currentUser
	onBreak := isOnBreak
	mu.Unlock()

	if user == nil {
		return fmt.Errorf("no user")
	}

	if onBreak && captureType == CaptureRegular {
		return nil
	}

	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return fmt.Errorf("no displays")
	}

	timestamp := time.Now().UnixNano()
	action := string(captureType)

	for i := 0; i < n; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			continue
		}

		var pngBuf bytes.Buffer
		if err := png.Encode(&pngBuf, img); err != nil {
			continue
		}

		webpFileName := fmt.Sprintf("%s__%s__%s__%s__%d__%s.webp",
			user.CompanyID, user.UserID, localIP, macAddress, timestamp, action,
		)

		tmpPNG := webpFileName + ".png"

		if err := ioutil.WriteFile(tmpPNG, pngBuf.Bytes(), 0644); err != nil {
			continue
		}

		defer os.Remove(tmpPNG)
		defer os.Remove(webpFileName)

		cmd := exec.Command("cwebp", tmpPNG, "-q", "20", "-o", webpFileName)
		if err := cmd.Run(); err != nil {
			os.Remove(webpFileName)
			continue
		}

		defer os.Remove(webpFileName)

		webpData, err := ioutil.ReadFile(webpFileName)
		if err != nil {
			continue
		}

		encryptedData, err := encryptData(webpData)
		if err != nil {
			continue
		}

		fileHash := generateFileHash(encryptedData)

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		writer.WriteField("companyId", user.CompanyID)
		writer.WriteField("userId", user.UserID)
		writer.WriteField("localIP", localIP)
		writer.WriteField("mac", macAddress)
		writer.WriteField("timestamp", fmt.Sprintf("%d", timestamp))
		writer.WriteField("type", action)
		writer.WriteField("encrypted", "true")
		writer.WriteField("fileHash", fileHash)
		writer.WriteField("algorithm", "AES-256-GCM")

		fileName := fmt.Sprintf("%s__%s__%s__%s__%d__%s.enc",
			user.CompanyID, user.UserID, localIP, macAddress, timestamp, action,
		)

		part, err := writer.CreateFormFile("screenshot", fileName)
		if err != nil {
			writer.Close()
			continue
		}

		if _, err := part.Write(encryptedData); err != nil {
			writer.Close()
			continue
		}

		writer.Close()

		req, err := http.NewRequest("POST", SERVER_URL, body)
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}

		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	return nil
}

func startCaptureLoop() {
	mu.Lock()
	if isRunning {
		mu.Unlock()
		return
	}
	isRunning = true
	isOnBreak = false
	ticker = time.NewTicker(60 * time.Second)
	stopChan = make(chan struct{})
	mu.Unlock()

	CaptureScreen(CaptureStart)

	go func() {
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				onBreak := isOnBreak
				mu.Unlock()
				if !onBreak {
					CaptureScreen(CaptureRegular)
				}
			case <-stopChan:
				return
			}
		}
	}()
}

func stopCaptureLoop() {
	mu.Lock()
	if !isRunning {
		mu.Unlock()
		return
	}
	isRunning = false
	isOnBreak = false
	if ticker != nil {
		ticker.Stop()
	}
	if stopChan != nil {
		close(stopChan)
	}
	mu.Unlock()
}

// ────────────────────────────────────────────────
// HTTP Handlers (updated)
// ────────────────────────────────────────────────

func setUserHandler(w http.ResponseWriter, r *http.Request) {
	var user UserInfo
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	mu.Lock()
	currentUser = &user
	mu.Unlock()

	w.Write([]byte("user set"))
}

func startHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	user := currentUser
	mu.Unlock()

	if user == nil {
		http.Error(w, "no user", http.StatusForbidden)
		return
	}

	if !connectWebSocket() {
		http.Error(w, "cannot connect to server", http.StatusServiceUnavailable)
		return
	}

	if err := CaptureScreen(CaptureStart); err != nil {
		http.Error(w, "capture failed", http.StatusInternalServerError)
		return
	}

	startCaptureLoop()
	w.Write([]byte("success"))
}

func stopHandler(w http.ResponseWriter, r *http.Request) {
	wasRunning := isRunning
	if wasRunning {
		CaptureScreen(CaptureStop)
	}

	stopCaptureLoop()
	closeWebSocket()
	w.Write([]byte("stopped"))
}

func breakHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	isOnBreak = true
	mu.Unlock()

	CaptureScreen(CaptureBreak)
	closeWebSocket()
	w.Write([]byte("on break"))
}

func resumeHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	isOnBreak = false
	mu.Unlock()

	if !connectWebSocket() {
		http.Error(w, "cannot connect to server", http.StatusServiceUnavailable)
		return
	}

	CaptureScreen(CaptureResume)

	if !isRunning {
		startCaptureLoop()
	}
	w.Write([]byte("resumed"))
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	running := isRunning
	onBreak := isOnBreak
	mu.Unlock()

	if running && !onBreak {
		w.Write([]byte("running"))
	} else {
		w.Write([]byte("stopped"))
	}
}