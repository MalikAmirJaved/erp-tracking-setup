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

const baseDir = `D:\UsersTrackingScreenShots`

type Config struct {
	MaxUploadSize int64
	BaseDir       string
}

var config = Config{
	MaxUploadSize: 50 << 20, // 50MB
	BaseDir:       baseDir,
}

func main() {
	if err := os.MkdirAll(config.BaseDir, os.ModePerm); err != nil {
		os.Exit(1)
	}

	http.HandleFunc("/upload-screenshot", uploadScreenshotHandler)
	http.HandleFunc("/health", healthCheckHandler)
	log.Printf("🚀 Screenshot server running on http://127.0.0.1:3002")
	http.ListenAndServe(":3002", nil)
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

	err := r.ParseMultipartForm(config.MaxUploadSize)
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	companyId := r.FormValue("companyId")
	userId := r.FormValue("userId")
	localIP := r.FormValue("localIP")
	mac := r.FormValue("mac")
	timestamp := r.FormValue("timestamp")
	fileType := r.FormValue("type")
	encrypted := r.FormValue("encrypted")
	fileHash := r.FormValue("fileHash")

	if companyId == "" || userId == "" || localIP == "" || mac == "" || timestamp == "" || fileType == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("screenshot")
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file data", http.StatusInternalServerError)
		return
	}

	if fileHash != "" {
		calculatedHash := calculateFileHash(fileData)
		if calculatedHash != fileHash {
			http.Error(w, "File integrity check failed", http.StatusBadRequest)
			return
		}
	}

	saveDir := filepath.Join(config.BaseDir, sanitize(companyId), sanitize(userId))
	if err := os.MkdirAll(saveDir, os.ModePerm); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	fileExt := filepath.Ext(header.Filename)
	if fileExt == "" {
		if encrypted == "true" {
			fileExt = ".enc"
		} else {
			fileExt = ".png"
		}
	}

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

	dst, err := os.Create(savePath)
	if err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = dst.Write(fileData)
	if err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"status":    "success",
		"fileName":  fileName,
		"fileSize":  len(fileData),
		"encrypted": encrypted == "true",
		"hash":      fileHash,
	}

	json.NewEncoder(w).Encode(response)
}

func calculateFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func sanitize(input string) string {
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