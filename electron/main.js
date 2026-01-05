// electron/main.js
const { app, BrowserWindow, Tray, Menu, ipcMain, shell } = require("electron");
const path = require("path");
const { spawn } = require("child_process");
const fs = require("fs");

let mainWindow;
let tray;
let trackerProcess = null;

function startGoTracker() {
  let trackerPath;

  if (app.isPackaged) {
    // In packaged app: extraResources are next to app.asar
    trackerPath = path.join(process.resourcesPath, "go", "tracker.exe");
  } else {
    // In development
    trackerPath = path.join(__dirname, "go", "tracker.exe");
  }

  if (!fs.existsSync(trackerPath)) {
    console.error(`Go tracker binary not found at: ${trackerPath}`);
    // Optional: show toast or dialog to user
    return;
  }

  console.log(`Starting Go tracker: ${trackerPath}`);

  trackerProcess = spawn(trackerPath, [], {
    detached: false,
    stdio: "ignore",
    windowsHide: true
  });

  trackerProcess.unref();
}

async function autoStartTracking() {
  let attempts = 0;
  const maxAttempts = 15;

  while (attempts < maxAttempts) {
    try {
      const res = await fetch("http://localhost:9090/start", { method: "POST" });
      if (res.ok && (await res.text()) === "success") {
        console.log("✅ Screenshot capture auto-started");
        mainWindow?.webContents.send("tracking-started");
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

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 400,
    height: 600,
    resizable: false,
    frame: false,                  // Removes title bar + close/minimize buttons
    transparent: true,
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      nodeIntegration: false,
      contextIsolation: true
    },
    icon: path.join(__dirname, "icon.ico") // Optional: add your icon
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