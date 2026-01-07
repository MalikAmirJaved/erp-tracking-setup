// electron/main.js
const { app, BrowserWindow, Tray, Menu, ipcMain } = require("electron");
const path = require("path");
const { spawn } = require("child_process");
const fs = require("fs");

app.commandLine.appendSwitch("ignore-certificate-errors"); // ignore SSL errors
app.commandLine.appendSwitch("allow-insecure-localhost"); // allow localhost HTTP
app.setAsDefaultProtocolClient("erpmonitoring", process.execPath, [
  "--",
  "--allow-insecure-localhost"
]);

let mainWindow;
let tray;
let trackerProcess = null;
let currentUser = null;

// ---------- Reliable send user + auto-start ----------
function sendUserToGoAndAutoStart() {
  if (!currentUser) {
    return;
  }

  const trySend = () => {

    fetch("http://127.0.0.1:9090/set-user", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(currentUser),
      mode: "cors"

    })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);

        // Now start tracking
        return fetch("http://127.0.0.1:9090/start", { method: "POST" });
      })
      .then((startRes) => {
        if (!startRes.ok) throw new Error(`Start HTTP ${startRes.status}`);
        return startRes.text();
      })
      .then((text) => {
        if (text.trim() === "success") {
          if (mainWindow && mainWindow.webContents) {
            mainWindow.webContents.send("auto-tracking-started");
          }
        } else {
          throw new Error(`Unexpected response: ${text}`);
        }
      })
      .catch((err) => {
        log.warn("Go tracker not ready or error – retrying in 1s:", err.message || err);
        setTimeout(trySend, 1000);
      });
  };

  trySend();
}

function handleDeepLink(url) {

  try {
    // Strip the protocol and parse manually
    const urlWithoutProtocol = url.replace(/^erpmonitoring:\/\//, "");
    // Split path and query
    const [path, queryString] = urlWithoutProtocol.split("?");
    
    // Use URLSearchParams on the query string
    const params = new URLSearchParams(queryString);
    
    const userIdRaw = params.get("userId");
    const companyIdRaw = params.get("companyId");
    const name = params.get("name");

    if (userIdRaw && companyIdRaw && name) {
      const userInfo = {
        userId: userIdRaw,
        companyId: companyIdRaw,
        name: decodeURIComponent(name),
      };

      currentUser = userInfo;

      if (mainWindow && mainWindow.webContents) {
        mainWindow.webContents.send("deep-link-auth", userInfo);
      }

      sendUserToGoAndAutoStart();
    }
  } catch (e) {
    log.error("Invalid deep link:", e);
  }
}



// ---------- Go Tracker ----------
function startGoTracker() {
  const goExePath = app.isPackaged
    ? path.join(process.resourcesPath, "go", "erp-monitoring.exe")
    : path.join(__dirname, "go", "erp-monitoring.exe");

  if (!fs.existsSync(goExePath)) {
    log.error("Go executable not found at:", goExePath);
    return;
  }

  trackerProcess = spawn(goExePath, [], { windowsHide: true });
}

// ---------- Window ----------
function createWindow() {
  mainWindow = new BrowserWindow({
    width: 420,
    height: 700,
    resizable: false,
    frame: false,
    transparent: true,
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
      devTools: true,
    },
  });

  if (app.isPackaged) {
    mainWindow.loadFile(path.join(__dirname, "../dist/index.html"));
  } else {
    mainWindow.loadURL("http://127.0.0.1:4000");
    mainWindow.webContents.openDevTools({ mode: "detach" });
  }

  mainWindow.on("closed", () => {
    mainWindow = null;
  });
}

// ---------- Tray ----------
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

// ---------- IPC ----------
ipcMain.on("hide-window", () => {
  mainWindow?.hide();
});

// Single instance + protocol
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

app.setAsDefaultProtocolClient("erpmonitoring");

// ---------- App Ready ----------
app.whenReady().then(() => {
  startGoTracker();
  createWindow();
  createTray();

  // Handle startup deep link
  const startupUrl = process.argv.find((arg) => arg.startsWith("erpmonitoring://"));
  if (startupUrl) handleDeepLink(startupUrl);

  // Fallback: if somehow user is already set (rare), try once
  setTimeout(() => {
    if (currentUser) sendUserToGoAndAutoStart();
  }, 3000);
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) createWindow();
});

app.on("before-quit", () => {
  if (trackerProcess) trackerProcess.kill();
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});