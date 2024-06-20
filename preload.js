const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electronAPI', {
	log: (callback) => ipcRenderer.on('log', callback),
	recieve: (callback) => ipcRenderer.on('receive', callback),
	request: data => ipcRenderer.send('request', data),
	doRollTrak: message => ipcRenderer.send('doRollTrak', message)
});