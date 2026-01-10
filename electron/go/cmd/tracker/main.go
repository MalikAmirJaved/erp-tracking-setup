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
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"mime/multipart"

	"github.com/kbinani/screenshot"
)

const SERVER_URL = "http://localhost:3002/upload-screenshot"

// Encryption key - In production, this should come from secure storage
// For now, using a fixed key. In production, use environment variables or key management service
var ENCRYPTION_KEY = []byte("this-is-32-byte-long-key-for-aes") // 32 bytes

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
	mu          sync.Mutex
	isRunning   bool
	isOnBreak   bool // true when user is on break
	ticker      *time.Ticker
	stopChan    chan struct{}
	currentUser *UserInfo
	localIP     string
	macAddress  string
)

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

// encryptData encrypts the given data using AES-GCM
func encryptData(data []byte) ([]byte, error) {
	// Create a new AES cipher block
	block, err := aes.NewCipher(ENCRYPTION_KEY)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %v", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}

	// Create a nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to create nonce: %v", err)
	}

	// Encrypt the data
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// generateFileHash creates a SHA256 hash of the encrypted data for verification
func generateFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func CaptureScreen(captureType CaptureType) error {
	mu.Lock()
	user := currentUser
	onBreak := isOnBreak
	mu.Unlock()

	if user == nil {
		return fmt.Errorf("no user authenticated")
	}

	// Skip regular captures during break
	if onBreak && captureType == CaptureRegular {
		return nil
	}

	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return fmt.Errorf("no displays")
	}

	timestamp := time.Now().Unix()
	action := string(captureType)
	if captureType == CaptureRegular {
		action = "regular"
	}

	for i := 0; i < n; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			log.Println("Capture error:", err)
			continue
		}

		// Encode PNG in memory
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			log.Println("PNG encode error:", err)
			continue
		}

		// Encrypt the screenshot data
		encryptedData, err := encryptData(buf.Bytes())
		if err != nil {
			log.Println("Encryption error:", err)
			continue
		}

		// Generate hash for verification
		fileHash := generateFileHash(encryptedData)

		// Prepare multipart/form-data
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add metadata fields
		writer.WriteField("companyId", user.CompanyID)
		writer.WriteField("userId", user.UserID)
		writer.WriteField("localIP", localIP)
		writer.WriteField("mac", macAddress)
		writer.WriteField("timestamp", fmt.Sprintf("%d", timestamp))
		writer.WriteField("type", action)
		writer.WriteField("encrypted", "true") // Flag indicating file is encrypted
		writer.WriteField("fileHash", fileHash) // Hash for verification
		writer.WriteField("algorithm", "AES-256-GCM") // Encryption algorithm used

		// Add encrypted file
		fileName := fmt.Sprintf("%s__%s__%s__%s__%d__%s.enc",
			user.CompanyID, user.UserID, localIP, macAddress, timestamp, action)

		part, err := writer.CreateFormFile("screenshot", fileName)
		if err != nil {
			log.Println("CreateFormFile error:", err)
			writer.Close()
			continue
		}

		_, err = part.Write(encryptedData)
		if err != nil {
			log.Println("Write file error:", err)
			writer.Close()
			continue
		}

		writer.Close()

		// Send POST request
		req, err := http.NewRequest("POST", SERVER_URL, body)
		if err != nil {
			log.Println("HTTP request error:", err)
			continue
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Println("Upload failed:", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			log.Printf("Upload failed, status: %d, response: %s", resp.StatusCode, string(bodyBytes))
		} else {
			log.Println("Uploaded encrypted screenshot:", fileName)
		}
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
	ticker = time.NewTicker(30 * time.Second)
	stopChan = make(chan struct{})
	mu.Unlock()

	// Immediate capture on fresh start
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

func main() {
	localIP = getLocalIP()
	macAddress = getMACAddress()

	// Log encryption status
	log.Printf("Encryption enabled: Using AES-256-GCM")
	
	mux := http.NewServeMux()

	mux.HandleFunc("/set-user", func(w http.ResponseWriter, r *http.Request) {
		var user UserInfo
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		mu.Lock()
		currentUser = &user
		mu.Unlock()

		log.Printf("User set: %s (%s - %s)", user.Name, user.UserID, user.CompanyID)
		w.Write([]byte("user set"))
	})

	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		user := currentUser
		mu.Unlock()

		if user == nil {
			http.Error(w, "no user", http.StatusForbidden)
			return
		}

		mu.Lock()
		isOnBreak = false
		mu.Unlock()

		if err := CaptureScreen(CaptureStart); err != nil {
			http.Error(w, "capture failed", http.StatusInternalServerError)
			return
		}

		startCaptureLoop()
		w.Write([]byte("success"))
	})

	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		wasRunning := isRunning
		mu.Unlock()

		if wasRunning {
			CaptureScreen(CaptureStop) // ← Capture with "stop" action
		}

		stopCaptureLoop()
		w.Write([]byte("stopped"))
	})

	// Dedicated break endpoint
	mux.HandleFunc("/break", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		isOnBreak = true
		mu.Unlock()

		CaptureScreen(CaptureBreak)
		w.Write([]byte("on break"))
	})

	// Dedicated resume endpoint
	mux.HandleFunc("/resume", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		isOnBreak = false
		mu.Unlock()

		CaptureScreen(CaptureResume)

		// Ensure loop is running
		if !isRunning {
			startCaptureLoop()
		}
		w.Write([]byte("resumed"))
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		running := isRunning
		onBreak := isOnBreak
		mu.Unlock()

		if running && !onBreak {
			w.Write([]byte("running"))
		} else {
			w.Write([]byte("stopped"))
		}
	})

	handler := corsMiddleware(mux)
	log.Println("Go tracker listening on :9090")
	log.Fatal(http.ListenAndServe("127.0.0.1:9090", handler))
}