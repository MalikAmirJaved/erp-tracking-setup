// electron/main.js
const { app, BrowserWindow, Tray, Menu, ipcMain, screen, dialog } = require("electron");
const path = require("path");
const { spawn } = require("child_process");
const fs = require("fs");

let mainWindow;
let tray;
let trackerProcess = null;

// ---------- Deep Link / Custom Protocol Handling ----------
function handleDeepLink(url) {
  console.log("Received deep link:", url);

  // Example: parse the URL and send to renderer if needed
  try {
    const parsed = new URL(url);
    console.log("Protocol path:", parsed.pathname);
    console.log("Search params:", parsed.searchParams.toString());

    // You can forward the URL or parsed data to the renderer process
    if (mainWindow && mainWindow.webContents) {
      mainWindow.webContents.send("deep-link", {
        url,
        path: parsed.pathname,
        params: Object.fromEntries(parsed.searchParams),
      });
    }
  } catch (err) {
    console.error("Invalid deep link URL:", err);
  }

  // Ensure window is visible
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.show();
  }
}

// When a second instance is launched with a protocol URL
app.on("second-instance", (event, commandLine, workingDirectory) => {
  const url = commandLine.find((arg) => arg.startsWith("erpmonitoring://"));
  if (url) {
    handleDeepLink(url);
  }

  // Focus existing window
  if (mainWindow) {
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.show();
  }
});

// ---------- Auto-start screenshot tracking ----------
async function autoStartTracking() {
  let attempts = 0;
  const maxAttempts = 15;

  while (attempts < maxAttempts) {
    try {
      const res = await fetch("http://localhost:9090/start", { method: "POST" });
      if (res.ok && (await res.text()) === "success") {
        console.log("✅ Screenshot capture auto-started");

        // Notify renderer
        mainWindow?.webContents.send("auto-tracking-started");
        return;
      }
    } catch (err) {
      console.log("Go tracker not ready, retrying in 1s...");
    }
    attempts++;
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  console.error("Failed to auto-start tracking");
}

// ---------- Start Go tracker binary ----------
function startGoTracker() {
  let trackerPath;

  if (app.isPackaged) {
    trackerPath = path.join(process.resourcesPath, "go", "erp-monitoring.exe");
  } else {
    trackerPath = path.join(__dirname, "go", "erp-monitoring.exe");
  }

  if (!fs.existsSync(trackerPath)) {
    console.error(`Go tracker binary not found at: ${trackerPath}`);

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
      windowsHide: true,
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

// ---------- Create main window ----------
function createWindow() {
  const { width: screenWidth, height: screenHeight } =
    screen.getPrimaryDisplay().workAreaSize;

  const winWidth = Math.round(screenWidth * 0.2); // 20%
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

// ---------- Create system tray ----------
function createTray() {
  const iconPath = path.join(__dirname, "icon.png"); // orange tray icon

  if (!fs.existsSync(iconPath)) {
    console.error("Tray icon not found:", iconPath);
    return;
  }

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
const gotTheLock = app.requestSingleInstanceLock();

if (!gotTheLock) {
  app.quit();
} else {
  app.on("second-instance", (event, commandLine) => {
    const url = commandLine.find(arg => arg.startsWith("erpmonitoring://"));
    if (url) handleDeepLink(url);
  });
}

app.setAsDefaultProtocolClient("erpmonitoring");
// ---------- App ready ----------
app.whenReady().then(() => {
  startGoTracker();
  createWindow();
  createTray();

  // Check if app was opened with a protocol URL on first launch
  const startupUrl = process.argv.find((arg) =>
    arg.startsWith("erpmonitoring://")
  );
  if (startupUrl) {
    handleDeepLink(startupUrl);
  }

  setTimeout(autoStartTracking, 1500); // Auto-start screenshots
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) createWindow();
});

app.on("before-quit", () => {
  if (trackerProcess) {
    trackerProcess.kill();
  }
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});