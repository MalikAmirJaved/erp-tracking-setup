// electron/main.js
const { app, BrowserWindow, Tray, Menu, ipcMain, shell, screen } = require("electron");
const path = require("path");
const { spawn } = require("child_process");
const fs = require("fs");

let mainWindow;
let tray;
let trackerProcess = null;

// electron/main.js - Update the autoStartTracking function
async function autoStartTracking() {
  let attempts = 0;
  const maxAttempts = 15;

  while (attempts < maxAttempts) {
    try {
      const res = await fetch("http://localhost:9090/start", { method: "POST" });
      if (res.ok && (await res.text()) === "success") {
        console.log("✅ Screenshot capture auto-started");
        
        // Send message to renderer to update UI state
        mainWindow?.webContents.send("auto-tracking-started");
        return;
      }
    } catch (err) {
      console.log("Go tracker not ready, retrying in 1s...");
    }
    attempts++;
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  console.error("Failed to auto-start tracking");
}

// Also update the startGoTracker function to handle edge cases better
function startGoTracker() {
  let trackerPath;

  if (app.isPackaged) {
    trackerPath = path.join(process.resourcesPath, "go", "tracker.exe");
  } else {
    trackerPath = path.join(__dirname, "go", "tracker.exe");
  }

  if (!fs.existsSync(trackerPath)) {
    console.error(`Go tracker binary not found at: ${trackerPath}`);
    
    // Show error dialog in production
    if (app.isPackaged) {
      dialog.showErrorBox(
        "Tracker Binary Missing",
        "The screenshot capture module could not be found. Please reinstall the application."
      );
    }
    return;
  }

  console.log(`Starting Go tracker: ${trackerPath}`);

  try {
    trackerProcess = spawn(trackerPath, [], {
      detached: false,
      stdio: "ignore",
      windowsHide: true
    });

    trackerProcess.on("error", (err) => {
      console.error("Failed to start tracker process:", err);
    });

    trackerProcess.on("exit", (code) => {
      console.log(`Tracker process exited with code ${code}`);
      trackerProcess = null;
    });

    trackerProcess.unref();
  } catch (error) {
    console.error("Error starting tracker:", error);
  }
}

function createWindow() {
  const { width: screenWidth, height: screenHeight } =
    screen.getPrimaryDisplay().workAreaSize;

  const winWidth = Math.round(screenWidth * 0.2);  // 20%
  const winHeight = Math.round(screenHeight * 0.15); // 15%

  mainWindow = new BrowserWindow({
    width: winWidth,
    height: winHeight,
    x: screenWidth - winWidth - 30,
    y: screenHeight - winHeight - 80,
    frame: false,
    resizable: false,
    alwaysOnTop: true,
    skipTaskbar: true,
    transparent: false,
    backgroundColor: "#FFFFFF",
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  });

  if (app.isPackaged) {
    mainWindow.loadFile(path.join(__dirname, "../dist/index.html"));
  } else {
    mainWindow.loadURL("http://localhost:4000");
    mainWindow.webContents.openDevTools({ mode: "detach" });
  }

  mainWindow.on("closed", () => {
    mainWindow = null;
  });
}

function createTray() {
  const iconPath = path.join(__dirname, "icon.png"); // Add your tray icon (orange)

  if (!fs.existsSync(iconPath)) {
    console.error("Tray icon not found:", iconPath);
    return;
  }

  tray = new Tray(iconPath);
  tray.setToolTip("Tracker Test");

  const contextMenu = Menu.buildFromTemplate([
    { label: "Show", click: () => mainWindow.show() },
    { label: "Quit", click: () => app.quit() }
  ]);

  tray.setContextMenu(contextMenu);
  tray.on("click", () => {
    mainWindow.isVisible() ? mainWindow.hide() : mainWindow.show();
  });
}

ipcMain.on("hide-window", () => {
  mainWindow?.hide();
});

app.whenReady().then(() => {
  startGoTracker();
  createWindow();
  createTray();
  setTimeout(autoStartTracking, 1500); // Auto-start screenshots
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) createWindow();
});

app.on("before-quit", () => {
  if (trackerProcess) {
    trackerProcess.kill(); // Kill Go process to avoid "cannot be closed" during reinstall
  }
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});