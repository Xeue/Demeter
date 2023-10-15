const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electronAPI', {
	log: (callback) => ipcRenderer.on('log', callback),
	disks: (callback) => ipcRenderer.on('disks', callback),
	diskLog: (callback) => ipcRenderer.on('diskLog', callback),
	gatewayTx: message => ipcRenderer.send('gateway', message),
	gotFirmware: (callback) => ipcRenderer.on('firmware', callback),
	getFirmware: message => ipcRenderer.send('firmware', message),
	progress: (callback) => ipcRenderer.on('progress', callback),
});