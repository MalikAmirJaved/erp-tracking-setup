// electron/main.js
const { app, BrowserWindow, Tray, Menu, ipcMain, screen, shell } = require("electron");
const path = require("path");
const { spawn } = require("child_process");
const fs = require("fs");
require("dotenv").config();

let mainWindow;
let tray;
let trackerProcess = null;

function startGoTracker() {
  const trackerPath = path.join(__dirname, "go", "tracker.exe");

  // Check if the file exists first
  if (!fs.existsSync(trackerPath)) {
    console.error(`Go tracker binary not found at: ${trackerPath}`);
    // Optional: show a dialog or toast in renderer later
    return;
  }

  console.log(`Starting Go tracker: ${trackerPath}`);

  // Spawn the process (detached so it keeps running even if Electron closes)
  trackerProcess = spawn(trackerPath, [], {
    detached: true,
    stdio: "inherit",           // forward logs to Electron console
    windowsHide: true,          // hide console window on Windows
  });

  trackerProcess.on("error", (err) => {
    console.error("Failed to start tracker.exe:", err);
  });

  trackerProcess.on("close", (code) => {
    console.log(`tracker.exe exited with code ${code}`);
    trackerProcess = null;
  });

  // Optional: kill child process when app quits
  app.on("before-quit", () => {
    if (trackerProcess) {
      console.log("Killing tracker.exe on app quit...");
      trackerProcess.kill();
    }
  });
}

const createWindow = () => {
  const { width: screenWidth, height: screenHeight } =
    screen.getPrimaryDisplay().workAreaSize;

  const winWidth = Math.round(screenWidth * 0.2);   // 20%
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
    mainWindow.loadURL(process.env.VITE_REACT_APP_API_URL); // ← adjust if your Vite port is different
    mainWindow.webContents.openDevTools(); // helpful during dev
  }

  mainWindow.on("close", (e) => {
    if (!app.isQuitting) {
      e.preventDefault();
      mainWindow.hide();
    }
  });
};

const createTray = () => {
  const iconPath = path.join(__dirname, "icon.png");

  if (!fs.existsSync(iconPath)) {
    console.error("Tray icon not found:", iconPath);
    return;
  }

  tray = new Tray(iconPath);
  tray.setToolTip("Time Tracker");

  const contextMenu = Menu.buildFromTemplate([
    { label: "Show", click: () => mainWindow.show() },
    { label: "Quit", click: () => {
        app.isQuitting = true;
        app.quit();
      }
    },
  ]);

  tray.setContextMenu(contextMenu);

  tray.on("click", () => {
    mainWindow.isVisible() ? mainWindow.hide() : mainWindow.show();
  });
};

ipcMain.on("hide-window", () => {
  mainWindow?.hide();
});

app.whenReady().then(() => {
  // 1. Start the Go tracker first
  startGoTracker();

  // 2. Then create window & tray
  createWindow();
  createTray();
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow();
  }
});

app.on("before-quit", () => {
  app.isQuitting = true;
  if (trackerProcess) {
    trackerProcess.kill();
  }
});