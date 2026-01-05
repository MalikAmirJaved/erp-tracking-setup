// electron/preload.js
const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("electronAPI", {
  hideWindow: () => ipcRenderer.send("hide-window"),
  openSettings: () => require("electron").shell.openExternal("ms-settings:privacy-graphicscaptureprogrammatic"),
  onTrackingStarted: (callback) => ipcRenderer.on("tracking-started", () => callback())
});