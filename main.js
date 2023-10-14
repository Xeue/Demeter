/* eslint-disable no-unused-vars */
const fs = require('fs');
const fsPromises = require('fs').promises;
const temp = require('temp');
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
		config.require('advancedConfig', {true: 'Yes', false: 'No'}, 'Show advanced config settings');
		{
			config.require('debugLineNum', {true: 'Yes', false: 'No'}, 'Print line numbers', ['advancedConfig', true]);
			config.require('printPings', {true: 'Yes', false: 'No'}, 'Print pings', ['advancedConfig', true]);
			config.require('devMode', {true: 'Yes', false: 'No'}, 'Dev mode - Disables connections to devices', ['advancedConfig', true]);
		}

		config.default('systemName', 'Demeter');
		config.default('loggingLevel', 'W');
		config.default('createLogFile', true);
		config.default('advancedConfig', false);
		config.default('debugLineNum', false);
		config.default('printPings', false);
		config.default('devMode', false);

		if (!await config.fromFile(path.join(app.getPath('documents'), 'DemeterData', 'config.conf'))) {
			config.set('systemName', 'Demeter');
			config.set('loggingLevel', 'W');
			config.set('createLogFile', true);
			config.set('advancedConfig', false);
			config.set('debugLineNum', false);
			config.set('printPings', false);
			config.set('devMode', false);
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

async function createGatewayCard(driveLetter) {
	const shell = new Shell(logger, 'FORMAT', 'D', 'cmd.exe');

	const selectDiskCmd = temp.path({ suffix: '.txt' });
	await fsPromises.writeFile(selectDiskCmd, [`SELECT VOLUME ${driveLetter}`,`LIST DISK`, `EXIT`].join('\n'));
	const {stdout} = await shell.run(`diskpart /s "${selectDiskCmd}"`);
	let selectedDisk;
	stdout.forEach(row => {
		if (row.includes("* Disk")) {
			selectedDisk = String(row.match(/\* Disk (.*?) Online/g)).replace('* Disk ', '').replace(/\s*Online/g, '');
			return;
		}
	});
	logger.debug('Selected disk is: '+selectedDisk);

	const cleanDiskCommand = temp.path({ suffix: '.txt' });
	await fsPromises.writeFile(cleanDiskCommand, [
		`SELECT DISK ${selectedDisk}`,
		`CLEAN`,
		`EXIT`
	].join('\n'));
	logger.debug('Cleaning disk');
	await shell.run(`diskpart /s "${cleanDiskCommand}"`);

	logger.debug('Creating new partition');

	const partition32 = temp.path({ suffix: '.txt' });
	await fsPromises.writeFile(partition32, [
		`SELECT DISK ${selectedDisk}`,
		`CREATE PARTITION PRIMARY SIZE=27400 OFFSET=10240`,
		`EXIT`
	].join('\n'));
	const partition16 = temp.path({ suffix: '.txt' });
	await fsPromises.writeFile(partition16, [
		`SELECT DISK ${selectedDisk}`,
		`CREATE PARTITION PRIMARY SIZE=12560 OFFSET=10240`,
		`EXIT`
	].join('\n'));
	const partition8 = temp.path({ suffix: '.txt' });
	await fsPromises.writeFile(partition8, [
		`SELECT DISK ${selectedDisk}`,
		`CREATE PARTITION PRIMARY SIZE=5550 OFFSET=10240`,
		`EXIT`
	].join('\n'));
	{
		const errorMessage = "Virtual Disk Service error:\r\nThere is not enough usable space for this operation.";
		logger.debug('Trying to create 27GB partition');
		const {stdout} = await shell.run(`diskpart /s "${partition32}"`);
		if (stdout.includes(errorMessage)) {
			logger.debug('Trying to create 12GB partition');
			const {stdout} = await shell.run(`diskpart /s "${partition16}"`);
			if (stdout.includes(errorMessage)) {
				logger.debug('Trying to create 5.5GB partition');
				const {stdout} = await shell.run(`diskpart /s "${partition8}"`);
				if (stdout.includes(errorMessage)) {
					logger.error('Cannot create partition');
					return;
				}
			}
		}
	}

	logger.debug('Formating new partition');
	await shell.run(`ECHO Y | format ${driveLetter}: /FS:FAT32 /Q /X /V:UCP25_SDI`);
	logger.debug('Copying boot files');
	await shell.run(`${CFImager} -raw -offset 0x400 -skip 0x400 -f ipl.bin -d ${driveLetter}`);
	logger.debug('Disk prepared');
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