package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kbinani/screenshot"
)

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

	dir := "D:\\trackingScreenShot"
	os.MkdirAll(dir, os.ModePerm)

	timestamp := time.Now().Unix()

	action := string(captureType)
	if captureType == CaptureRegular {
		action = "regular"
	}

	for i := 0; i < n; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			continue
		}

		filename := fmt.Sprintf("%s__%s__%s__%s__%d__%s.png",
			user.CompanyID, user.UserID, localIP, macAddress, timestamp, action)

		filePath := filepath.Join(dir, filename)
		f, err := os.Create(filePath)
		if err != nil {
			continue
		}
		png.Encode(f, img)
		f.Close()
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