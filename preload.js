const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electronAPI', {
	log: (callback) => ipcRenderer.on('log', callback),
	receive: (callback) => ipcRenderer.on('receive', callback),
	request: (command, data) => ipcRenderer.send(command, data),
	doRollTrak: message => ipcRenderer.send('doRollTrak', message)
});

contextBridge.exposeInMainWorld('backend', {
	send: (command, data) => ipcRenderer.send(command, data),
	on: (command, callback) => ipcRenderer.on(command, (event, data) => callback(data))
});