/* eslint-disable no-unused-vars */
const serverID = new Date().getTime();

const express = require('express');
const fs = require('fs');
const path = require('path');
const {Logs} = require('xeue-logs');
const {Config} = require('xeue-config');
const {Server} = require('xeue-webserver');
const {app, BrowserWindow, ipcMain, Tray, Menu} = require('electron');
const {version} = require('./package.json');
const electronEjs = require('electron-ejs');
const AutoLaunch = require('auto-launch');
const https = require('https');
const {MicaBrowserWindow, IS_WINDOWS_11} = require('mica-electron');

const background = IS_WINDOWS_11 ? 'micaActive' : 'bg-dark';

const httpsAgent = new https.Agent({
	rejectUnauthorized: false,
});

const __static = __dirname+'/static';

const ejs = new electronEjs({'static': __static, 'background': background}, {});

Array.prototype.symDiff = function(x) {
	return this.filter(y => !x.includes(y)).concat(x => !y.includes(x));
}

Array.prototype.diff = function(x) {
	return this.filter(y => !x.includes(y));
}

/* Data Defines */



/* Globals */

let isQuiting = false;
let mainWindow = null;
let configLoaded = false;
const devEnv = app.isPackaged ? './' : './';
const __main = path.resolve(__dirname, devEnv);

const logger = new Logs(
	false,
	'DemeterLogging',
	path.join(app.getPath('documents'), 'DemeterData'),
	'D',
	false
)
const config = new Config(
	logger
);
const webServer = new Server(
	expressRoutes,
	logger,
	version,
	config,
	doMessage
);

/* Start App */

(async () => {

	await app.whenReady();
	await setUpApp();
	await createWindow();

	{ /* Config */
		logger.printHeader('Demeter Monitoring');
		config.require('port', [], 'What port shall the server use');
		config.require('systemName', [], 'What is the name of the system');
		config.require('loggingLevel', {'A':'All', 'D':'Debug', 'W':'Warnings', 'E':'Errors'}, 'Set logging level');
		config.require('createLogFile', {true: 'Yes', false: 'No'}, 'Save logs to local file');
		config.require('advancedConfig', {true: 'Yes', false: 'No'}, 'Show advanced config settings');
		{
			config.require('debugLineNum', {true: 'Yes', false: 'No'}, 'Print line numbers', ['advancedConfig', true]);
			config.require('printPings', {true: 'Yes', false: 'No'}, 'Print pings', ['advancedConfig', true]);
			config.require('devMode', {true: 'Yes', false: 'No'}, 'Dev mode - Disables connections to devices', ['advancedConfig', true]);
		}

		config.default('port', 8080);
		config.default('systemName', 'Demeter');
		config.default('loggingLevel', 'W');
		config.default('createLogFile', true);
		config.default('advancedConfig', false);
		config.default('debugLineNum', false);
		config.default('printPings', false);
		config.default('devMode', false);

		if (!await config.fromFile(path.join(app.getPath('documents'), 'DemeterData', 'config.conf'))) {
			config.set('port', 8080);
			config.set('systemName', 'Demeter');
			config.set('loggingLevel', 'W');
			config.set('createLogFile', true);
			config.set('advancedConfig', false);
			config.set('debugLineNum', false);
			config.set('printPings', false);
			config.set('devMode', false);
		}

		if (config.get('loggingLevel') == 'D' || config.get('loggingLevel') == 'A') {
			config.set('debugLineNum', true);
		}

		logger.setConf({
			'createLogFile': config.get('createLogFile'),
			'logsFileName': 'DemeterLogging',
			'configLocation': path.join(app.getPath('documents'), 'DemeterData'),
			'loggingLevel': config.get('loggingLevel'),
			'debugLineNum': config.get('debugLineNum'),
		});

		logger.log('Running version: v'+version, ['H', 'SERVER', logger.g]);
		logger.log(`Logging to: ${path.join(app.getPath('documents'), 'DemeterData', 'logs')}`, ['H', 'SERVER', logger.g]);
		logger.log(`Config saved to: ${path.join(app.getPath('documents'), 'DemeterData', 'config.conf')}`, ['H', 'SERVER', logger.g]);
		config.print();
		config.userInput(async command => {
			switch (command) {
			case 'config':
				await config.fromCLI(path.join(app.getPath('documents'), 'DemeterData', 'config.conf'));
				if (config.get('loggingLevel') == 'D' || config.get('loggingLevel') == 'A') {
					config.set('debugLineNum', true);
				}
				logger.setConf({
					'createLogFile': config.get('createLogFile'),
					'logsFileName': 'DemeterLogging',
					'configLocation': path.join(app.getPath('documents'), 'DemeterData'),
					'loggingLevel': config.get('loggingLevel'),
					'debugLineNum': config.get('debugLineNum')
				});
				return true;
			}
		});
		configLoaded = true;
	}

	webServer.start(config.get('port'));

	logger.log(`Demeter can be accessed at http://localhost:${config.get('port')}`, 'C');
	mainWindow.webContents.send('loaded', `http://localhost:${config.get('port')}/inApp`);
	
})().catch(error => {
	console.log(error);
});


/* Electron */


async function setUpApp() {
	const tray = new Tray(path.join(__static, 'img/icon/network-96.png'));
	tray.setContextMenu(Menu.buildFromTemplate([
		{
			label: 'Show App', click: function () {
				mainWindow.show();
			}
		},
		{
			label: 'Exit', click: function () {
				isQuiting = true;
				app.quit();
			}
		}
	]));

	ipcMain.on('window', (event, message) => {
		switch (message) {
		case 'exit':
			app.quit();
			break;
		case 'minimise':
			mainWindow.hide();
			break;
		default:
			break;
		}
	});

	ipcMain.on('config', (event, message) => {
		switch (message) {
		case 'start':
			config.fromAPI(path.join(app.getPath('documents'), 'DemeterData','config.conf'), configQuestion, configDone);
			break;
		case 'stop':
			logger.log('Not implemeneted yet: Cancle config change');
			break;
		case 'show':
			config.print();
			break;
		default:
			break;
		}
	});

	const autoLaunch = new AutoLaunch({
		name: 'Demeter Monitoring',
		isHidden: true,
	});
	autoLaunch.isEnabled().then(isEnabled => {
		if (!isEnabled) autoLaunch.enable();
	});

	app.on('before-quit', function () {
		isQuiting = true;
	});

	app.on('activate', async () => {
		if (BrowserWindow.getAllWindows().length === 0) createWindow();
	});

	logger.on('logSend', message => {
		if (!isQuiting) mainWindow.webContents.send('log', message);
	});
}

async function createWindow() {
	const windowOptions = {
		width: 1440,
		height: 720,
		autoHideMenuBar: true,
		webPreferences: {
			contextIsolation: true,
			preload: path.resolve(__main, 'preload.js')
		},
		icon: path.join(__static, 'img/icon/icon.png'),
		show: false,
		frame: false,
		titleBarStyle: 'hidden',
		titleBarOverlay: {
			color: '#313d48',
			symbolColor: '#ffffff',
			height: 56
		}
	}
	
	if (IS_WINDOWS_11) {
		mainWindow = new MicaBrowserWindow(windowOptions);
		mainWindow.setDarkTheme();
		mainWindow.setMicaEffect();
	} else {
		mainWindow = new BrowserWindow(windowOptions);
	}

	if (!app.commandLine.hasSwitch('hidden')) {
		mainWindow.show();
	} else {
		mainWindow.hide();
	}

	mainWindow.on('close', function (event) {
		if (!isQuiting) {
			event.preventDefault();
			mainWindow.webContents.send('requestExit');
			event.returnValue = false;
		}
	});

	mainWindow.on('minimize', function (event) {
		event.preventDefault();
		mainWindow.hide();
	});

	mainWindow.loadURL(path.resolve(__main, 'views/app.ejs'));

	await new Promise(resolve => {
		ipcMain.on('ready', (event, ready) => {
			if (configLoaded) {
				mainWindow.webContents.send('loaded', `http://localhost:${config.get('port')}/inApp`);
			}
			resolve();
		});
	});
}


/* Express setup & Websocket Server */


function expressRoutes(expressApp) {
	expressApp.set('views', path.join(__main, 'views'));
	expressApp.set('view engine', 'ejs');
	expressApp.use(express.json());
	expressApp.use(express.static(__static));

	expressApp.get('/',  (req, res) =>  {
		logger.log('New client connected', 'A');
		res.header('Content-type', 'text/html');
		res.render('web', {
			switches:switches('Media'),
			controlSwitches:switches('Control'),
			systemName:config.get('systemName'),
			webSocketEndpoint:config.get('webSocketEndpoint'),
			secureWebSocketEndpoint:config.get('secureWebSocketEndpoint'),
			webEnabled:config.get('webEnabled'),
			version: version,
			pings:syslogSourceList(),
			background:'bg-dark'
		});
	});

	expressApp.get('/inApp',  (req, res) =>  {
		logger.log('New client connected', 'A');
		res.header('Content-type', 'text/html');
		res.render('web', {
			switches:switches('Media'),
			controlSwitches:switches('Control'),
			systemName:config.get('systemName'),
			webSocketEndpoint:config.get('webSocketEndpoint'),
			secureWebSocketEndpoint:config.get('secureWebSocketEndpoint'),
			webEnabled:config.get('webEnabled'),
			version: version,
			pings:syslogSourceList(),
			background:'micaActive'
		});
	});

	expressApp.get('/about', (req, res) => {
		logger.log('Collecting about information', 'A');
		res.header('Content-type', 'text/html');
		const aboutInfo = {
			'aboutInfo': {
				'Version': version,
				'Config': config.all(),
				'Switches':switches(),
				'IQ Frames':frames(),
				'UPS':ups(),
				'Devices':devices(),
				'Pings':pings(),
				'Port Monitoring':ports()
			},
			'systemName': config.get('systemName')
		}
		res.render('about', aboutInfo);
	})

	expressApp.get('/broken', (req, res) => {
		res.send('no');
	});

	expressApp.get('/fibre', (req, res) => {
		logger.log('Request for fibre data', 'D');
		res.send(JSON.stringify(data.fibre));
	});

	expressApp.get('/ups', (req, res) => {
		logger.log('Request for UPS data', 'D');
		res.send(JSON.stringify(data.ups));
	});

	expressApp.get('/phy', (req, res) => {
		logger.log('Request for PHY/FEC data', 'D');
		res.send(JSON.stringify(data.phy));
	});

	expressApp.get('/mac', (req, res) => {
		logger.log('Request for mac/flap data', 'D');
		res.send(JSON.stringify(data.mac));
	});

	expressApp.get('/devices', (req, res) => {
		logger.log('Request for devices data', 'D');
		res.send(JSON.stringify(data.devices.Media));
	});

	expressApp.get('/getConfig', (req, res) => {
		logger.log('Request for devices config', 'D');
		let catagory = req.query.catagory;
		let data;
		switch (catagory) {
		case 'switches':
			data = switches();
			break;
		case 'ports':
			data = ports();
			break;
		case 'frames':
			data = frames();
			break;
		case 'ups':
			data = ups();
			break;
		case 'devices':
			data = devices();
			break;
		case 'pings':
			data = pings();
			break;
		default:
			break;
		}
		res.send(JSON.stringify(data));
	});

	expressApp.get('/config', (req, res) => {
		logger.log('Requesting app config', 'A');
		res.send(JSON.stringify(config.all()));
	});

	expressApp.post('/setswitches', (req, res) => {
		logger.log('Request to set switches config data', 'D');
		writeData('Switches', req.body);
		res.send('Done');
	});
	expressApp.post('/setports', (req, res) => {
		logger.log('Request to set ports config data', 'D');
		writeData('Ports', req.body);
		res.send('Done');
	});
	expressApp.post('/setdevices', (req, res) => {
		logger.log('Request to set devices config data', 'D');
		writeData('Devices', req.body);
		res.send('Done');
	});
	expressApp.post('/setups', (req, res) => {
		logger.log('Request to set ups config data', 'D');
		writeData('Ups', req.body);
		res.send('Done');
	});
	expressApp.post('/setframes', (req, res) => {
		logger.log('Request to set frames config data', 'D');
		writeData('Frames', req.body);
		res.send('Done');
	});
	expressApp.post('/setpings', (req, res) => {
		logger.log('Request to set pings config data', 'D');
		writeData('Pings', req.body);
		res.send('Done');
	});
}


async function doMessage(msgObj, socket) {
	const payload = msgObj.payload;
	const header = msgObj.header;
	if (typeof payload.source == 'undefined') {
		payload.source = 'default';
	}
	switch (payload.command) {
	case 'meta':
		logger.object('Received', msgObj, 'D');
		socket.send('Received meta');
		break;
	case 'register':
		coreDoRegister(socket, msgObj);
		break;
	case 'get':
		switch (payload.data) {
			case 'temperature':
				getTemperature(header, payload).then(data => {
					webServer.sendTo(socket, data);
				});
				break;
			case 'syslog':
				getSyslog(header, payload).then(data => {
					webServer.sendTo(socket, data);
				})
			default:
				break;
		}
		break;
	default:
		logger.object('Unknown message', msgObj, 'W');
	}
}


function coreDoRegister(socket, msgObj) {
	const header = msgObj.header;
	if (typeof socket.type == 'undefined') {
		socket.type = header.type;
	}
	if (typeof socket.ID == 'undefined') {
		socket.ID = header.fromID;
	}
	if (typeof socket.version == 'undefined') {
		socket.version = header.version;
	}
	if (typeof socket.prodID == 'undefined') {
		socket.prodID = header.prodID;
	}
	if (header.version !== version) {
		if (header.version.substr(0, header.version.indexOf('.')) != version.substr(0, version.indexOf('.'))) {
			logger.log('Connected client has different major version, it will not work with this server!', 'E');
		} else {
			logger.log('Connected client has differnet version, support not guaranteed', 'W');
		}
	}
	logger.log(`${logger.g}${header.fromID}${logger.reset} Registered as new client`, 'D');
	socket.connected = true;
}

function makeHeader() {
	const header = {};
	header.fromID = serverID;
	header.timestamp = new Date().getTime();
	header.version = version;
	header.type = 'Server';
	header.active = true;
	header.messageID = header.timestamp;
	header.recipients = [];
	header.system = config.get('systemName');
	return header;
}

function distributeData(type, data) {
	sendCloudData({'command':'log', 'type':type, 'data':data});
	webServer.sendToAll({'command':'log', 'type':type, 'data':data});
}


function minutes(mins) {
	return parseInt(mins) * 60;
}

async function sleep(seconds) {
	await new Promise (resolve => setTimeout(resolve, 1000*seconds));
}