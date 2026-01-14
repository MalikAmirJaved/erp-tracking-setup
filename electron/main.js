// electron/main.js
const { app, BrowserWindow, Tray, Menu, ipcMain, screen } = require("electron");
const path = require("path");
const fs = require("fs");
const Store = require("electron-store");
const { autoUpdater } = require("electron-updater");
const log = require("electron-log");
log.transports.file.level = "info";
autoUpdater.logger = log;
autoUpdater.autoDownload = true;

const store = new Store();
let mainWindow;
let tray;
let trackerProcess = null;
let currentUser = store.get("user") || null;

app.commandLine.appendSwitch("ignore-certificate-errors");
app.commandLine.appendSwitch("allow-insecure-localhost");
app.setAsDefaultProtocolClient("erpmonitoring");

function initAutoUpdater() {
  if (!app.isPackaged) return;

  autoUpdater.checkForUpdatesAndNotify();

  autoUpdater.on("update-available", () => {
    log.info("Update available");
  });

  autoUpdater.on("update-downloaded", () => {
    log.info("Update downloaded, restarting...");
    autoUpdater.quitAndInstall();
  });

  autoUpdater.on("error", (err) => {
    log.error("Auto update error:", err);
  });
}


function startGoTracker() {
  if (trackerProcess || !currentUser) return;

  let goExePath;
  if (app.isPackaged) {
    goExePath = path.join(process.resourcesPath, "go", "erp-monitoring.exe");
  } else {
    goExePath = path.join(__dirname, "go", "erp-monitoring.exe");
  }

  if (!fs.existsSync(goExePath)) {
    return;
  }

  trackerProcess = require("child_process").spawn(goExePath, [], {
    windowsHide: true,
    cwd: path.dirname(goExePath),
    env: {
      ...process.env,
      PATH: `${path.dirname(goExePath)};${process.env.PATH || ""}`,
    },
  });

  trackerProcess.on("error", () => {
    trackerProcess = null;
  });

  trackerProcess.on("close", () => {
    trackerProcess = null;
  });
}

async function sendUserToGoAndAutoStart() {
  if (!currentUser) return;

  let attempts = 0;
  const MAX_SILENT_RETRIES = 3;

  const trySend = async () => {
    try {
      const res = await fetch("http://127.0.0.1:9090/set-user", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(currentUser),
      });

      if (!res.ok) throw new Error();

      const startRes = await fetch("http://127.0.0.1:9090/start", {
        method: "POST",
      });

      if (!startRes.ok) throw new Error();

      mainWindow?.webContents.send("auto-tracking-started");
    } catch {
      attempts++;
      if (attempts <= MAX_SILENT_RETRIES) {
        setTimeout(trySend, 1500);
      }
    }
  };

  trySend();
}

// Add this helper function
async function isTrackerActuallyRunning() {
  try {
    const res = await fetch("http://127.0.0.1:9090/status", { method: "GET" });
    if (!res.ok) return false;
    const text = await res.text();
    return text === "running";
  } catch {
    return false;
  }
}

// Updated deep link handler
function handleDeepLink(url) {
  if (!url?.startsWith("erpmonitoring://")) return;

  const urlWithoutProtocol = url.replace(/^erpmonitoring:\/\//, "");
  const [_, queryString] = urlWithoutProtocol.split("?");
  const params = new URLSearchParams(queryString);

  const newUser = {
    userId: params.get("userId"),
    companyId: params.get("companyId"),
    name: params.get("name") || "User",
  };

  // Quick validation
  if (!newUser.userId || !newUser.companyId) return;

  const isSameUser =
    currentUser &&
    currentUser.userId === newUser.userId &&
    currentUser.companyId === newUser.companyId;

  // ────────────────────────────────────────────────────────────────
  //   CORE LOGIC - When to skip everything
  // ────────────────────────────────────────────────────────────────
  if (isSameUser) {
    // Same user → check if tracker is already happily running
    isTrackerActuallyRunning().then((isRunning) => {
      if (isRunning) {
        // ★★★ Most important case ★★★
        // Same user + tracker is running → DO ALMOST NOTHING
        console.log("Same user & tracker already running → keeping current session");
        return;
      }

      // Same user but tracker is NOT running → we should probably restart it
      console.log("Same user but tracker stopped → restarting tracker...");
      proceedWithNewUser(newUser);
    });
    return;
  }

  // Different user → normal full login flow
  console.log("New/different user detected → full login procedure");
  proceedWithNewUser(newUser);
}

// Helper to handle the "new user" flow (extracted for clarity)
function proceedWithNewUser(newUserInfo) {
  currentUser = newUserInfo;
  store.set("user", currentUser);

  // Kill old tracker if exists
  if (trackerProcess) {
    trackerProcess.kill();
    trackerProcess = null;
  }

  // Notify renderer (will reset UI, timer, etc.)
  mainWindow?.webContents.send("deep-link-auth", currentUser);

  // Start fresh tracker
  startGoTracker();

  // Give it a moment to start listening
  setTimeout(sendUserToGoAndAutoStart, 800);
}

function createWindow() {
  const { width: screenWidth, height: screenHeight } =
    screen.getPrimaryDisplay().workAreaSize;

  const winWidth = Math.round(screenWidth * 0.2);
  const winHeight = Math.round(screenHeight * 0.15);

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

  mainWindow.webContents.on("before-input-event", (event, input) => {
    if (
      ((input.control || input.meta) && input.key.toLowerCase() === "r") ||
      input.key === "F5"
    ) {
      event.preventDefault();
    }
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

  if (currentUser) {
    setTimeout(() => {
      mainWindow.webContents.send("deep-link-auth", currentUser);
      startGoTracker();
      sendUserToGoAndAutoStart();
    }, 1000);
  }
}

function createTray() {
  const iconPath = path.join(__dirname, "icon.png");
  if (!fs.existsSync(iconPath)) return;

  tray = new Tray(iconPath);
  tray.setToolTip("ERP Monitoring");

  const contextMenu = Menu.buildFromTemplate([
    { label: "Show", click: () => mainWindow.show() },
    { label: "Quit", click: () => app.quit() },
  ]);

  tray.setContextMenu(contextMenu);
  tray.on("click", () => {
    mainWindow.isVisible() ? mainWindow.hide() : mainWindow.show();
  });
}

ipcMain.on("hide-window", () => mainWindow?.hide());
ipcMain.handle("get-user", () => {
  return store.get("user") || null;
});
ipcMain.handle("get-app-version", () => {
  return app.getVersion();
});
const gotTheLock = app.requestSingleInstanceLock();
if (!gotTheLock) {
  app.quit();
} else {
  app.on("second-instance", (event, commandLine) => {
    const url = commandLine.find((arg) => arg.startsWith("erpmonitoring://"));
    if (url) handleDeepLink(url);
    if (mainWindow) {
      if (mainWindow.isMinimized()) mainWindow.restore();
      mainWindow.show();
    }
  });
}

app.whenReady().then(() => {
  createWindow();
  createTray();
initAutoUpdater();
  const startupUrl = process.argv.find((arg) =>
    arg.startsWith("erpmonitoring://")
  );

  if (startupUrl) {
    handleDeepLink(startupUrl);
  } else if (currentUser) {
    // Existing user on app start
    startGoTracker();
    setTimeout(() => {
      sendUserToGoAndAutoStart();
      // We don't send deep-link-auth here anymore if already logged in
      // Only if you really want to force UI refresh — usually not needed
    }, 1000);
  }
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) createWindow();
});

app.on("before-quit", () => {
  if (trackerProcess) trackerProcess.kill();
});