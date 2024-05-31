/* eslint-disable no-unused-vars */
const fs = require('fs');
const files = require('fs').promises;
const temp = require('temp').track();
const path = require('path');
const _Logs = require('xeue-logs').Logs;
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

const Logs = new _Logs(
	false,
	'DemeterLogging',
	path.join(app.getPath('documents'), 'DemeterData'),
	'D',
	false
)
const config = new Config(
	Logs
);

const gateway = new Gateway(
	Logs,
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
		Logs.printHeader('Demeter');
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

		Logs.setConf({
			'createLogFile': config.get('createLogFile'),
			'logsFileName': 'DemeterLogging',
			'configLocation': path.join(app.getPath('documents'), 'DemeterData'),
			'loggingLevel': config.get('loggingLevel'),
			'debugLineNum': config.get('debugLineNum'),
		});

		Logs.log('Running version: v'+version, ['H', 'SERVER', Logs.g]);
		Logs.log(`Logging to: ${path.join(app.getPath('documents'), 'DemeterData', 'logs')}`, ['H', 'SERVER', Logs.g]);
		Logs.log(`Config saved to: ${path.join(app.getPath('documents'), 'DemeterData', 'config.conf')}`, ['H', 'SERVER', Logs.g]);
		config.print();
		config.userInput(async command => {
			switch (command) {
			case 'config':
				await config.fromCLI(path.join(app.getPath('documents'), 'DemeterData', 'config.conf'));
				if (config.get('loggingLevel') == 'D' || config.get('loggingLevel') == 'A') {
					config.set('debugLineNum', true);
				}
				Logs.setConf({
					'createLogFile': config.get('createLogFile'),
					'logsFileName': 'DemeterLogging',
					'configLocation': path.join(app.getPath('documents'), 'DemeterData'),
					'loggingLevel': config.get('loggingLevel'),
					'debugLineNum': config.get('debugLineNum')
				});
				return true;
			case 'go':
				doRollTrak();
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

	ipcMain.on('doRollTrak', (event, disks) => {
		doRollTrak();
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

	Logs.on('logSend', message => {
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
		Logs.warn("Exiting");
	});

	mainWindow.on('minimize', function (event) {
		Logs.info("Minimising");
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
	Logs.info("Checking disks");
	const disks = await getDiskInfo();
	Logs.info('Disks', disks);
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
			Logs.error(`Couldn't get disk info`);
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



//4101 - IP
//4103 - Gateway
//4105 - Netmask
//4108 - Mode 1 = static

//4201 - IP
//4203 - Gateway
//4205 - Netmask
//4208 - Mode 1 = static

//4052 NMOS mode 0 = OFF 2 = ON
//4056 NMOS Registry 1=AUTO 2=Statis
//4054 NMOS Int 0=auto 1=GB 2=Eth1 3=Eth2
//4051 NMOS Take 1

//8500 - Select audio spigot 0 indexed
//8501 - Set mode 1=??? 0=Pass-through 2=mute 3=tone 4=custom

//48729 - 2110-31 interop
//48730 - 2110-31 interop

//58645 - Packet timing 1=125 increments by 14
//58644 - channel count increments by 14

async function doRollTrak() {
	const rolltrak = new Shell(Logs, 'DDS', 'D');

	const globalCommands = {
		'spigotCommands': {
			'58645': 1,//Packet Timing
			'58645': 16//Channel Count
		},
		'cardCommands': {
			'48729': 1,//Interop
			'48730': 1//Interop
		},
		'doShuffleFix': true
	}

	const framesSpecifics = {
		'10.40.44.10':{},
		'10.40.44.20':{},
		'10.40.44.30':{},
		'10.40.44.40':{},
		'10.40.44.50':{},
		'10.40.44.60':{},
		'10.40.44.70':{},
		'10.40.44.80':{},
		'10.40.44.100':{},
		'10.40.44.110':{},
		'10.40.44.120':{},
		'10.40.44.130':{},
		'10.40.44.140':{},
		'10.40.44.150':{},
		'10.40.44.160':{},
		'10.40.44.170':{},
		'10.40.44.180':{},
		'10.40.44.190':{},
		'10.40.44.200':{},
		'10.40.44.210':{},
		'10.40.45.10':{},
		'10.40.45.20':{},
		'10.40.45.40':{},
		'10.40.128.10':{},
		'10.40.128.20':{},
		'10.40.128.30':{},
		'10.40.128.40':{},
		'10.40.128.50':{},
		'10.40.128.60':{},
		'10.40.128.70':{},
		'10.40.128.80':{},
		'10.40.128.90':{},
		'10.40.128.100':{},
		'10.40.128.110':{},
		'10.40.128.120':{},
		'10.40.128.130':{},
		'10.40.128.140':{},
		'10.40.128.150':{},
		'10.40.128.160':{},
		'10.40.128.170':{},
		'10.40.128.190':{},
		'10.40.128.210':{},
		'10.40.128.230':{},
		'10.40.128.240':{},
		'10.40.129.10':{},
		'10.40.129.20':{}
	};

	const frames = {};

	for (const frameIP in framesSpecifics) {
		if (!Object.hasOwnProperty.call(framesSpecifics, frameIP)) return;
		frames[frameIP] = {...JSON.parse(JSON.stringify(globalCommands)), ...framesSpecifics[frameIP]}	
	}

	
	for (const frameIP in frames) {
		if (!Object.hasOwnProperty.call(frames, frameIP)) return;
		doFrame(frameIP, frames);
	}

	async function doFrame(frameIP, frames) {
		const frame = frames[frameIP]
		const getSlotsPromises = [];
		const foundSlots = [];

		for (let slot = 0; slot < 20; slot++) {
			const command = `rolltrak -a ${frameIP} ${16530+slot}@0000:10:00?`;
			Logs.debug(`Running: ${command}`);
			getSlotsPromises.push(rolltrak.run(command, false));
		}

		const slotsData = await Promise.all(getSlotsPromises);

		slotsData.forEach((slot, index) => {
			if (!slot.stdout[0].includes('IQUCP25_SDI')) return;
			foundSlots.push(String((1+index).toString(16)).padStart(2, '0'));
		});

		foundSlots.forEach(async slot => {

			for (const commandID in frame.cardCommands) {
				if (!Object.hasOwnProperty.call(frame.cardCommands, commandID)) return;
				const value = frame.cardCommands[commandID];
				const command = `rolltrak -a ${frameIP} ${commandID}@0000:10:${slot}=${value}`;
				Logs.debug(`Running: ${command}`);
				const newPromise = rolltrak.run(command);
				await newPromise;
			}

			for (let spigot = 0; spigot < 16; spigot++) {
				if (frame.doShuffleFix) {
					const commandAudioSelect = `rolltrak -a ${frameIP} 8500@0000:10:${slot}=${spigot}`;
					const newPromiseAudioSelect = rolltrak.run(commandAudioSelect);
					await newPromiseAudioSelect;
	
					const commandAudioSet = `rolltrak -a ${frameIP} 8501@0000:10:${slot}=0`;
					const newPromiseAudioSet = rolltrak.run(commandAudioSet);
					await newPromiseAudioSet;
				}

				for (const commandID in frame.spigotCommands) {
					if (!Object.hasOwnProperty.call(frame.spigotCommands, commandID)) return;
					const value = frame.spigotCommands[commandID];
					const command = `rolltrak -a ${frameIP} ${Number(commandID)+(14*spigot)}@0000:10:${slot}=${value}`;
					Logs.debug(`Running: ${command}`);
					const newPromise = rolltrak.run(command);
					await newPromise;
				}
			}
		})
		console.log(`Done all pushes for ${frameIP}`);
	}
}