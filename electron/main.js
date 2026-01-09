// electron/main.js
const { app, BrowserWindow, Tray, Menu, ipcMain, screen } = require("electron");
const path = require("path");
const fs = require("fs");
const Store = require("electron-store");

const store = new Store();
let mainWindow;
let tray;
let trackerProcess = null;
let currentUser = store.get("user") || null; // Persistent user

app.commandLine.appendSwitch("ignore-certificate-errors");
app.commandLine.appendSwitch("allow-insecure-localhost");
app.setAsDefaultProtocolClient("erpmonitoring");

function startGoTracker() {
  if (trackerProcess || !currentUser) return;
  let goExePath;
  if (app.isPackaged) {
    goExePath = path.join(process.resourcesPath, "go", "erp-monitoring.exe");
  } else {
    goExePath = path.join(__dirname, "go", "erp-monitoring.exe");
  }
  if (!fs.existsSync(goExePath)) {
    console.error("Go tracker executable not found:", goExePath);
    return;
  }

  trackerProcess = require("child_process").spawn(goExePath, [], {
    windowsHide: true,
  });

  trackerProcess.on("error", (err) => {
    console.error("Failed to start Go tracker:", err);
    trackerProcess = null;
  });

  trackerProcess.on("close", (code) => {
    console.log("Go tracker exited with code:", code);
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

      if (!res.ok) throw new Error(`HTTP ${res.status}`);

      const startRes = await fetch("http://127.0.0.1:9090/start", {
        method: "POST",
      });

      if (!startRes.ok) throw new Error(`Start failed ${startRes.status}`);

      // ONLY AFTER SUCCESS → notify renderer that tracking auto-started
      mainWindow?.webContents.send("auto-tracking-started");
    } catch (err) {
      attempts++;

      if (attempts > MAX_SILENT_RETRIES) {
        console.warn("Go service not ready yet, retrying...");
      }

      setTimeout(trySend, 1500);
    }
  };

  trySend();
}

function handleDeepLink(url) {
  if (!url?.startsWith("erpmonitoring://")) return;

  const urlWithoutProtocol = url.replace(/^erpmonitoring:\/\//, "");
  const [_, queryString] = urlWithoutProtocol.split("?");
  const params = new URLSearchParams(queryString);

  const userInfo = {
    userId: params.get("userId"),
    companyId: params.get("companyId"),
    name: params.get("name") || "User",
  };

  if (userInfo.userId && userInfo.companyId) {
    currentUser = userInfo;
    store.set("user", currentUser);

    // Kill old tracker process if running
    if (trackerProcess) {
      trackerProcess.kill();
      trackerProcess = null;
    }

    mainWindow?.webContents.send("deep-link-auth", currentUser);

    startGoTracker();
    setTimeout(sendUserToGoAndAutoStart, 800);
  }
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
// ⬇️ ADD THIS BLOCK HERE
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

  const startupUrl = process.argv.find((arg) =>
    arg.startsWith("erpmonitoring://")
  );
  if (startupUrl) handleDeepLink(startupUrl);
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) createWindow();
});

app.on("before-quit", () => {
  if (trackerProcess) trackerProcess.kill();
});