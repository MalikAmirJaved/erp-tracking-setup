package main

import (
    "fmt"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "strings"
)

// Base directory to save screenshots
const baseDir = `D:\UsersTrackingScreenShots`

func main() {
    http.HandleFunc("/upload-screenshot", uploadScreenshotHandler)

    port := ":4000"
    fmt.Printf("🚀 Screenshot server running on http://localhost%s\n", port)
    log.Fatal(http.ListenAndServe(port, nil))
}

func uploadScreenshotHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // Parse multipart form, limit to 50MB
    err := r.ParseMultipartForm(50 << 20)
    if err != nil {
        http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
        return
    }

    // Extract required fields
    companyId := r.FormValue("companyId")
    userId := r.FormValue("userId")
    localIP := r.FormValue("localIP")
    mac := r.FormValue("mac")
    timestamp := r.FormValue("timestamp")
    fileType := r.FormValue("type") // start, regular, stop, resume, break

    if companyId == "" || userId == "" || localIP == "" || mac == "" || timestamp == "" || fileType == "" {
        http.Error(w, "Missing required fields", http.StatusBadRequest)
        return
    }

    // Get file from form
    file, header, err := r.FormFile("screenshot")
    if err != nil {
        http.Error(w, "Failed to read file: "+err.Error(), http.StatusBadRequest)
        return
    }
    defer file.Close()

    // Create folder: baseDir/companyId/userId
    saveDir := filepath.Join(baseDir, sanitize(companyId), sanitize(userId))
    err = os.MkdirAll(saveDir, os.ModePerm)
    if err != nil {
        http.Error(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
        return
    }

    // Construct file name
    fileExt := filepath.Ext(header.Filename)
    if fileExt == "" {
        fileExt = ".png"
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

    // Save file
    dst, err := os.Create(savePath)
    if err != nil {
        http.Error(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
        return
    }
    defer dst.Close()

    _, err = dst.ReadFrom(file)
    if err != nil {
        http.Error(w, "Failed to write file: "+err.Error(), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, "File uploaded successfully: %s", fileName)
}

// sanitize removes illegal characters for filenames
func sanitize(input string) string {
    return strings.ReplaceAll(input, " ", "_")
}
