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

var (
	mu          sync.Mutex
	isRunning   bool
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

func CaptureScreen() error {
	mu.Lock()
	user := currentUser
	mu.Unlock()

	if user == nil {
		return fmt.Errorf("no user authenticated")
	}

	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return fmt.Errorf("no displays")
	}

	dir := "D:\\trackingScreenShot"
	os.MkdirAll(dir, os.ModePerm)

	timestamp := time.Now().Unix()

	for i := 0; i < n; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			continue
		}

		filename := fmt.Sprintf("%s__%s__%s__%s__%d__%d.png",
			user.CompanyID, user.UserID, localIP, macAddress, timestamp, i)

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
	ticker = time.NewTicker(30 * time.Second) // adjust interval as needed
	stopChan = make(chan struct{})
	mu.Unlock()

	go func() {
		for {
			select {
			case <-ticker.C:
				CaptureScreen()
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

		if err := CaptureScreen(); err != nil {
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

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		running := isRunning
		mu.Unlock()
		if running {
			w.Write([]byte("running"))
		} else {
			w.Write([]byte("stopped"))
		}
	})

	handler := corsMiddleware(mux)
	log.Println("Go tracker listening on :9090")
	log.Fatal(http.ListenAndServe("127.0.0.1:9090", handler))
}