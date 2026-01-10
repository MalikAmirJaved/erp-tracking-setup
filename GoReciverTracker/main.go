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
)

// Base directory to save screenshots
const baseDir = `D:\UsersTrackingScreenShots`

// Production configuration
type Config struct {
	MaxUploadSize int64
	BaseDir       string
}

var config = Config{
	MaxUploadSize: 50 << 20, // 50MB
	BaseDir:       baseDir,
}

func main() {
	// Create base directory if it doesn't exist
	if err := os.MkdirAll(config.BaseDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create base directory: %v", err)
	}

	http.HandleFunc("/upload-screenshot", uploadScreenshotHandler)
	http.HandleFunc("/health", healthCheckHandler)

	port := ":3002"
	log.Printf("🚀 Screenshot server running on http://127.0.0.1%s", port)
	log.Printf("📁 Base directory: %s", config.BaseDir)
	log.Printf("📦 Max upload size: %d bytes", config.MaxUploadSize)
	
	log.Fatal(http.ListenAndServe(port, nil))
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status": "ok", "service": "screenshot-server"}`)
}

func uploadScreenshotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form with size limit
	err := r.ParseMultipartForm(config.MaxUploadSize)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
		return
	}

	// Extract required fields
	companyId := r.FormValue("companyId")
	userId := r.FormValue("userId")
	localIP := r.FormValue("localIP")
	mac := r.FormValue("mac")
	timestamp := r.FormValue("timestamp")
	fileType := r.FormValue("type")
	encrypted := r.FormValue("encrypted")
	fileHash := r.FormValue("fileHash")
	algorithm := r.FormValue("algorithm")

	// Validate required fields
	if companyId == "" || userId == "" || localIP == "" || mac == "" || timestamp == "" || fileType == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Get file from form
	file, header, err := r.FormFile("screenshot")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read the uploaded file data
	fileData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read file data: %v", err), http.StatusInternalServerError)
		return
	}

	// Verify file hash if provided
	if fileHash != "" {
		calculatedHash := calculateFileHash(fileData)
		if calculatedHash != fileHash {
			log.Printf("Hash mismatch for file: %s. Expected: %s, Got: %s", 
				header.Filename, fileHash, calculatedHash)
			http.Error(w, "File integrity check failed", http.StatusBadRequest)
			return
		}
		log.Printf("✓ File hash verified for: %s", header.Filename)
	}

	// Create folder: baseDir/companyId/userId
	saveDir := filepath.Join(config.BaseDir, sanitize(companyId), sanitize(userId))
	err = os.MkdirAll(saveDir, os.ModePerm)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Construct file name with encryption indicator
	fileExt := filepath.Ext(header.Filename)
	if fileExt == "" {
		if encrypted == "true" {
			fileExt = ".enc"
		} else {
			fileExt = ".png"
		}
	}

	// Build filename with metadata
	fileName := fmt.Sprintf("%s__%s__%s__%s__%s__%s%s",
		sanitize(companyId),
		sanitize(userId),
		sanitize(localIP),
		sanitize(mac),
		sanitize(timestamp),
		sanitize(fileType),
		fileExt,
	)

	savePath := filepath.Join(saveDir, fileName)

	// Save file without decryption
	dst, err := os.Create(savePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Write the encrypted data directly to file
	_, err = dst.Write(fileData)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to write file: %v", err), http.StatusInternalServerError)
		return
	}

	// Log successful upload with encryption info
	logMsg := fmt.Sprintf("Uploaded: %s (Size: %d bytes)", fileName, len(fileData))
	if encrypted == "true" {
		logMsg += " [ENCRYPTED]"
		if algorithm != "" {
			logMsg += fmt.Sprintf(" [Algorithm: %s]", algorithm)
		}
	}
	log.Println(logMsg)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"status":    "success",
		"fileName":  fileName,
		"fileSize":  len(fileData),
		"encrypted": encrypted == "true",
		"hash":      fileHash,
	}
	
	jsonResponse, _ := json.Marshal(response)
	w.Write(jsonResponse)
}

// calculateFileHash creates a SHA256 hash of the file data
func calculateFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

// sanitize removes illegal characters for filenames
func sanitize(input string) string {
	// Replace spaces and other problematic characters
	replacer := strings.NewReplacer(
		" ", "_",
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(input)
}