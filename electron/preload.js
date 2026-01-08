// electron/preload.js
const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("electronAPI", {
  hideWindow: () => ipcRenderer.send("hide-window"),
  openSettings: () =>
    require("electron").shell.openExternal(
      "ms-settings:privacy-graphicscaptureprogrammatic"
    ),
  onAutoTrackingStarted: (callback) =>
    ipcRenderer.on("auto-tracking-started", () => callback()),
  onDeepLinkAuth: (callback) =>
    ipcRenderer.on("deep-link-auth", (_event, userInfo) => callback(userInfo)),
  getUser: () => ipcRenderer.invoke("get-user"),
});
