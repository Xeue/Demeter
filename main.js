/* eslint-disable no-unused-vars */
const fs = require('fs');
const files = require('fs').promises;
const temp = require('temp').track();
const path = require('path');
const {Logs} = require('xeue-logs');
const {Config} = require('xeue-config');
const {Shell} = require('xeue-shell');
const {app, BrowserWindow, ipcMain} = require('electron');
const {version} = require('./package.json');
const electronEjs = require('electron-ejs');
const {MicaBrowserWindow, IS_WINDOWS_11} = require('mica-electron');
const iconvLite = require("iconv-lite");
const childProcess = require("child_process");
const {Gateway} = require('./modules/gateway');

const background = IS_WINDOWS_11 ? 'micaActive' : 'bg-dark';

const __static = __dirname+'/static';

const CFImager = path.join(
	__dirname,
	"lib",
	"CFImager.exe"
).replace("app.asar", "app.asar.unpacked");

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
const __data = path.join(app.getPath('documents'), 'DemeterData');

if (!fs.existsSync(__data+'/firmware')) fs.mkdirSync(__data+'/firmware');
if (!fs.existsSync(__data+'/firmware/gateway')) fs.mkdirSync(__data+'/firmware/gateway');
if (!fs.existsSync(__data+'/firmware/mv')) fs.mkdirSync(__data+'/firmware/mv');
if (!fs.existsSync(__data+'/firmware/madi')) fs.mkdirSync(__data+'/firmware/madi');

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

const gateway = new Gateway(
	logger,
	sendGUI,
	CFImager,
	__data
);

/* Start App */

(async () => {

	await app.whenReady();
	await setUpApp();
	await createWindow();

	{ /* Config */
		logger.printHeader('Demeter');
		config.require('systemName', [], 'What is the name of the system');
		config.require('loggingLevel', {'A':'All', 'D':'Debug', 'W':'Warnings', 'E':'Errors'}, 'Set logging level');
		config.require('createLogFile', {true: 'Yes', false: 'No'}, 'Save logs to local file');
		config.require('debugLineNum', {true: 'Yes', false: 'No'}, 'Print line numbers', ['advancedConfig', true]);

		config.default('systemName', 'Demeter');
		config.default('loggingLevel', 'W');
		config.default('createLogFile', true);
		config.default('debugLineNum', false);

		if (!await config.fromFile(path.join(app.getPath('documents'), 'DemeterData', 'config.conf'))) {
			config.set('systemName', 'Demeter');
			config.set('loggingLevel', 'W');
			config.set('createLogFile', true);
			config.set('debugLineNum', false);
			config.write(path.join(app.getPath('documents'), 'DemeterData', 'config.conf'));
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

})().catch(error => {
	console.log(error);
});

const ejs = new electronEjs({
	'static': __static,
	'background': background,
	'version': version,
	'systemName': config.get('systemName')
}, {});



/* Electron */


async function setUpApp() {
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

	ipcMain.on('gateway', (event, disks) => {
		gateway.create(disks);
	})

	ipcMain.on('firmware', async () => {
		const firmware = await getFirmwareObject();
		mainWindow.webContents.send('firmware', firmware);
	})

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
		logger.warn("Exiting");
	});

	mainWindow.on('minimize', function (event) {
		logger.info("Minimising");
	});

	mainWindow.loadURL(path.resolve(__main, 'views/app.ejs'));
}

function sendGUI(channel, message) {
	mainWindow.webContents.send(channel, message);
}

async function sleep(seconds) {
	await new Promise (resolve => setTimeout(resolve, 1000*seconds));
}

setInterval(() => {checkDisks()}, 1*1000);

async function checkDisks() {
	logger.info("Checking disks");
	const disks = await getDiskInfo();
	logger.info('Disks', disks);
	mainWindow.webContents.send('disks', disks);
}

async function createMVCard(driveLetter) {

}

async function createMadiCard(driveLetter) {

}

function getDiskInfo() {
    return new Promise((resolve, reject) => {
		const drives = [];
		let buffer;
		let cp;
        try {
			buffer = childProcess.execSync(
				'wmic logicaldisk get Caption,FreeSpace,Size,VolumeSerialNumber,Description,VolumeName /format:list',
				{
					windowsHide: true,
					encoding: 'buffer'
				}
			);
			cp = childProcess.execSync('chcp').toString().split(':')[1].trim();
		} catch (error) {
			logger.error(`Couldn't get disk info`);
            reject(error);
        }
		let encoding = '';
		switch (cp) {
			case '65000': // UTF-7
				encoding = 'UTF-7';
				break;
			case '65001': // UTF-8
				encoding = 'UTF-8';
				break;
			default: // Other Encoding
				if (/^-?[\d.]+(?:e-?\d+)?$/.test(cp)) {
					encoding = 'cp' + cp;
				} else {
					encoding = cp;
				}
		}
		buffer = iconvLite.encode(iconvLite.decode(buffer, encoding), 'UTF-8');
		const lines = buffer.toString().split('\r\r\n');
		let newDiskIteration = false;
		let caption = '';
		let description = '';
		let freeSpace = 0;
		let size = 0;
		let name = '';
		lines.forEach(value => {
			if (value !== '') {
				const [section, data] = value.split('=');
				switch (section) {
					case 'Caption':
						caption = data;
						newDiskIteration = true;
						break;
					case 'Description':
						description = data;
						break;
					case 'FreeSpace':
						freeSpace = isNaN(parseFloat(data)) ? 0 : +data;
						break;
					case 'Size':
						size = isNaN(parseFloat(data)) ? 0 : +data;
						break;
					case 'VolumeName':
						name = data;
						break;
				}
			} else {
				if (!newDiskIteration) return;
				const used = (size - freeSpace);
				const percent = size > 0 ? Math.round((used / size) * 100) + '%' : '0%';
				drives.push({
					'filesystem': description,
					'blocks': size,
					'used': used,
					'available': freeSpace,
					'capacity': percent,
					'mounted': caption,
					'name': name
				});
				newDiskIteration = false;
				caption = '';
				description = '';
				freeSpace = 0;
				size = 0;
			}
		});
		resolve(drives);
    });
}

async function getFirmwareObject() {
	const folders = await Promise.allSettled([
		files.readdir(__data+'/firmware/gateway'),
		files.readdir(__data+'/firmware/mv'),
		files.readdir(__data+'/firmware/madi')
	]);
	return {
		'gateway': folders[0].value,
		'mv': folders[1].value,
		'madi': folders[2].value
	}
}