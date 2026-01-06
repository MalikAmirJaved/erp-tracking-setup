// electron/preload.js - Add the new event listener
const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("electronAPI", {
  hideWindow: () => ipcRenderer.send("hide-window"),
  openSettings: () => require("electron").shell.openExternal("ms-settings:privacy-graphicscaptureprogrammatic"),
  onAutoTrackingStarted: (callback) => ipcRenderer.on("auto-tracking-started", () => callback()),
  onTrackingStarted: (callback) => ipcRenderer.on("tracking-started", () => callback())
});