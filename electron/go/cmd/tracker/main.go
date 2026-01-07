package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kbinani/screenshot"
	"github.com/shirou/gopsutil/v3/host"
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
	userIP      string
)

func getLocalIP() string {
	info, _ := host.Info()
	return info.Hostname // fallback
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

func CaptureScreen() error {
	mu.Lock()
	user := currentUser
	mu.Unlock()

	if user == nil {
		return fmt.Errorf("no authenticated user")
	}

	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return fmt.Errorf("no display found")
	}

	dir := "D:\\trackingScreenShot"
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	ip := userIP
	if ip == "" {
		ip = getLocalIP()
	}

	for i := 0; i < n; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			return err
		}

		fileName := fmt.Sprintf("screenshot_%d_%d.png", i, time.Now().Unix())
		filePath := filepath.Join(dir, fileName)

		file, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer file.Close()

		if err := png.Encode(file, img); err != nil {
			return err
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
	stopChan = make(chan struct{})
	mu.Unlock()

	ticker = time.NewTicker(10 * time.Second) // adjust interval as needed
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := CaptureScreen(); err != nil {
					log.Printf("Capture error: %v", err)
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
	if ticker != nil {
		ticker.Stop()
	}
	if stopChan != nil {
		close(stopChan)
	}
	mu.Unlock()
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/set-user", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received /set-user request")

		var user UserInfo
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			http.Error(w, "Invalid user data", http.StatusBadRequest)
			return
		}

		mu.Lock()
		currentUser = &user
		mu.Unlock()

		log.Printf("User authenticated: %s (ID: %s, Company: %s)", user.Name, user.UserID, user.CompanyID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("user set"))
	})

	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received /start request")

		mu.Lock()
		user := currentUser
		mu.Unlock()

		if user == nil {
			http.Error(w, "No user authenticated", http.StatusForbidden)
			return
		}

		if err := CaptureScreen(); err != nil {
			http.Error(w, "Initial capture failed", http.StatusInternalServerError)
			return
		}

		startCaptureLoop()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received /stop request")
		stopCaptureLoop()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("stopped"))
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		log.Println("Received /status request")
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
	
	log.Println("Go tracker running on :9090")
	log.Fatal(http.ListenAndServe("127.0.0.1:9090", handler))
}
