/* eslint-disable no-unused-vars */

const path = require('path');
const _Logs = require('xeue-logs').Logs;
const {Config} = require('xeue-config');
const {Shell} = require('xeue-shell');
const {app, BrowserWindow, ipcMain} = require('electron');
const {version} = require('./package.json');
const electronEjs = require('electron-ejs');
const {MicaBrowserWindow, IS_WINDOWS_11} = require('mica-electron');

const background = IS_WINDOWS_11 ? 'micaActive' : 'bg-dark';

const __static = __dirname+'/static';

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
const devEnv = app.isPackaged ? './' : './';
const __main = path.resolve(__dirname, devEnv);
const __data = path.join(app.getPath('documents'), 'DemeterData');

const Logs = new _Logs(
	false,
	'DemeterLogging',
	__data,
	'D',
	false
)
const config = new Config(
	Logs
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

		if (!await config.fromFile(path.join(__data, 'config.conf'))) {
			config.set('systemName', 'Demeter');
			config.set('loggingLevel', 'W');
			config.set('createLogFile', true);
			config.set('debugLineNum', false);
			config.write(path.join(__data, 'config.conf'));
		}

		if (config.get('loggingLevel') == 'D' || config.get('loggingLevel') == 'A') {
			config.set('debugLineNum', true);
		}

		Logs.setConf({
			'createLogFile': config.get('createLogFile'),
			'logsFileName': 'DemeterLogging',
			'configLocation': __data,
			'loggingLevel': config.get('loggingLevel'),
			'debugLineNum': config.get('debugLineNum'),
		});

		Logs.log('Running version: v'+version, ['H', 'SERVER', Logs.g]);
		Logs.log(`Logging to: ${path.join(__data, 'logs')}`, ['H', 'SERVER', Logs.g]);
		Logs.log(`Config saved to: ${path.join(__data, 'config.conf')}`, ['H', 'SERVER', Logs.g]);
		config.print();
		config.userInput(async command => {
			switch (command) {
			case 'config':
				await config.fromCLI(path.join(__data, 'config.conf'));
				if (config.get('loggingLevel') == 'D' || config.get('loggingLevel') == 'A') {
					config.set('debugLineNum', true);
				}
				Logs.setConf({
					'createLogFile': config.get('createLogFile'),
					'logsFileName': 'DemeterLogging',
					'configLocation': __data,
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

	ipcMain.on('recieve', (event, data) => {
		Logs.object(data);
		switch (data.command) {
			case 'frames':
				sendGUI('request')
				break;
		
			default:
				break;
		}
	})

	ipcMain.on('doRollTrak', (event, disks) => {
		doRollTrak();
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
		width: 1640,
		height: 1220,
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



//4101 - IP
//4103 - Gateway
//4105 - Netmask
//4108 - Mode 1 = static

//4201 - IP
//4203 - Gateway
//4205 - Netmask
//4208 - Mode 1 = static

//4501 - Reference - 3-Chassis A, 4-Chassis B, 1-Network

//4052 NMOS mode 0 = OFF 2 = ON
//4056 NMOS Registry 1=AUTO 2=Statis
//4054 NMOS Int 0=auto 1=GB 2=Eth1 3=Eth2
//4051 NMOS Take 1

//21000 PTP 0=Freerun, 1=Multicast, 2=Unicast, 3=NTP
//21046 - Enable PTP on Eth 1
//21047 - Enable PTP on Eth 2
//21048 - Enable PTP on Control

//21010 - PTP Domain
//21013 - PTP Multicast Address

//21074 - PTP Preference 0=Eth1, 1=Eth2, 2=None, 3=Best

//8500 - Select audio spigot 0 indexed
//8501 - Set mode 1=??? 0=Pass-through 2=mute 3=tone 4=custom

//48729 - 2110-31 interop
//48730 - 2110-31 interop

//58645 - Packet timing 1=125 increments by 14
//58644 - channel count increments by 14

async function doRollTrak() {
	const rolltrak = new Shell(Logs, 'DDS', 'D');
	for (let index = 0; index < 70000; index++) {		
		const command = `rolltrak -a 10.40.42.161 ${index}@0000:30:00?`;
		await rolltrak.run(command, true);
	}
	//Logs.debug(`Running: ${command}`);
}

async function doRollTrak2() {
	const rolltrak = new Shell(Logs, 'DDS', 'D');

	const globalCommands = {
		// 'spigotCommands': {
		// 	'58645': 1,//Packet Timing
		// 	'58645': 16//Channel Count
		// },
		'cardCommands': {
			// '48729': 1,//Interop
			// '48730': 1//Interop
			'4501': 3,
			'21000': 1,
			'21046':1,
			'21047':1,
			'21074':3
		},
		// 'doShuffleFix': true
	}

	const framesSpecifics = {
		// '10.40.44.10':{},
		// '10.40.44.20':{},
		// '10.40.44.30':{},
		// '10.40.44.40':{},
		// '10.40.44.50':{},
		// '10.40.44.60':{},
		// '10.40.44.70':{},
		// '10.40.44.80':{},
		// '10.40.44.100':{},
		// '10.40.44.110':{},
		// '10.40.44.120':{},
		// '10.40.44.130':{},
		// '10.40.44.140':{},
		// '10.40.44.150':{},
		// '10.40.44.160':{},
		// '10.40.44.170':{},
		// '10.40.44.180':{},
		// '10.40.44.190':{},
		// '10.40.44.200':{},
		// '10.40.44.210':{},
		// '10.40.45.10':{},
		// '10.40.45.20':{},
		// '10.40.45.40':{},
		// '10.40.128.10':{},
		// '10.40.128.20':{},
		// '10.40.128.30':{},
		// '10.40.128.40':{},
		// '10.40.128.50':{},
		// '10.40.128.60':{},
		// '10.40.128.70':{},
		// '10.40.128.80':{},
		// '10.40.128.90':{},
		// '10.40.128.100':{},
		// '10.40.128.110':{},
		// '10.40.128.120':{},
		// '10.40.128.130':{},
		// '10.40.128.140':{},
		// '10.40.128.150':{},
		// '10.40.128.160':{},
		// '10.40.128.170':{},
		// '10.40.128.190':{},
		// '10.40.128.210':{},
		// '10.40.128.230':{},
		// '10.40.128.240':{},
		// '10.40.129.10':{},
		// '10.40.129.20':{}
	};

	const cardSpecifics = {
		// '10.40.42.11':{},
		// '10.40.42.12':{},
		// '10.40.42.13':{},
		// '10.40.42.14':{},
		// '10.40.42.21':{},
		// '10.40.42.22':{},
		// '10.40.42.23':{},
		// '10.40.42.24':{},
		// '10.40.42.31':{},
		// '10.40.42.32':{},
		// '10.40.42.33':{},
		// '10.40.42.34':{},
		// '10.40.42.41':{},
		// '10.40.42.42':{},
		// '10.40.42.43':{},
		// '10.40.42.44':{},
		// '10.40.42.51':{},
		// '10.40.42.52':{},
		// '10.40.42.53':{},
		// '10.40.42.54':{},
		// '10.40.42.61':{},
		// '10.40.42.62':{},
		// '10.40.42.63':{},
		// '10.40.42.64':{},
		// '10.40.42.71':{},
		// '10.40.42.72':{},
		// '10.40.42.73':{},
		// '10.40.42.74':{},
		// '10.40.42.81':{},
		// '10.40.42.82':{},
		// '10.40.42.83':{},
		// '10.40.42.84':{},
		// '10.40.42.91':{},
		// '10.40.42.92':{},
		// '10.40.42.93':{},
		// '10.40.42.94':{},
		// '10.40.42.101':{},
		// '10.40.42.102':{},
		// '10.40.42.103':{},
		// '10.40.42.104':{},
		// '10.40.42.111':{},
		// '10.40.42.112':{},
		// '10.40.42.113':{},
		// '10.40.42.114':{},
		// '10.40.42.121':{},
		// '10.40.42.122':{},
		// '10.40.42.123':{},
		// '10.40.42.124':{},
		// '10.40.42.131':{},
		// '10.40.42.132':{},
		// '10.40.42.133':{},
		// '10.40.42.134':{},
		// '10.40.42.141':{},
		// '10.40.42.142':{},
		// '10.40.42.143':{},
		// '10.40.42.144':{},
		// '10.40.42.151':{},
		// '10.40.42.152':{},
		// '10.40.42.153':{},
		// '10.40.42.154':{},
		// '10.40.42.161':{},
		// '10.40.42.162':{},
		// '10.40.42.163':{},
		// '10.40.42.164':{},
		// '10.40.42.171':{},
		// '10.40.42.172':{},
		// '10.40.42.173':{},
		// '10.40.42.174':{},
		// '10.40.42.181':{},
		// '10.40.42.182':{},
		// '10.40.42.183':{},
		// '10.40.42.184':{},
		// '10.40.42.191':{},
		// '10.40.42.192':{},
		// '10.40.42.193':{},
		// '10.40.42.194':{},
		// '10.40.42.201':{},
		// '10.40.42.202':{},
		// '10.40.42.203':{},
		// '10.40.42.204':{}
	}

	const frames = {};
	const cards = {};
	const framesPromises = [];

	for (const frameIP in framesSpecifics) {
		if (!Object.hasOwnProperty.call(framesSpecifics, frameIP)) return;
		frames[frameIP] = {...JSON.parse(JSON.stringify(globalCommands)), ...framesSpecifics[frameIP]}	
	}

	for (const cardIP in cardSpecifics) {
		if (!Object.hasOwnProperty.call(cardSpecifics, cardIP)) return;
		cards[cardIP] = {...JSON.parse(JSON.stringify(globalCommands)), ...cardSpecifics[cardIP]}	
	}
	
	for (const frameIP in frames) {
		if (!Object.hasOwnProperty.call(frames, frameIP)) return;
		framesPromises.push(doFrame(frameIP, frames));
	}

	for (const cardIP in cards) {
		if (!Object.hasOwnProperty.call(cards, cardIP)) return;
		doCard(cardIP, cards);
	}

	await Promise.all(framesPromises);
	Logs.log('All commands complete')

	async function doFrame(frameIP, frames) {
		const frame = frames[frameIP]
		const getSlotsPromises = [];
		const foundSlots = [];
		const checking = [];
	
		for (let slot = 0; slot < 20; slot++) {
			const command = `rolltrak -a ${frameIP} ${16530+slot}@0000:10:00?`;
			Logs.debug(`Running: ${command}`);
			checking.push({'slot':slot,'ip':frameIP});
			getSlotsPromises.push(rolltrak.run(command, false));
		}
	
		const slotsData = await Promise.all(getSlotsPromises);
	
		slotsData.forEach((slot, index) => {
			try {
				if (!slot.stdout[0].includes('IQUCP25_SDI')) return;
				foundSlots.push(String((1+index).toString(16)).padStart(2, '0'));
			} catch (error) {
				Logs.warn(`Issue with slot: ${checking[index].slot} at IP: ${checking[index].ip}`, error);
			}
		});
	
		const slotsPromises = [];

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
		await Promise.all(slotsPromises);
		Logs.log(`Done all pushes for ${frameIP}`);
	}
	
	async function doCard(cardIP, cards) {
		const card = cards[cardIP]
	
		for (const commandID in card.cardCommands) {
			if (!Object.hasOwnProperty.call(card.cardCommands, commandID)) return;
			const value = card.cardCommands[commandID];
			const command = `rolltrak -a ${cardIP} ${commandID}@0000:30:00=${value}`;
			Logs.debug(`Running: ${command}`);
			const newPromise = rolltrak.run(command);
			await newPromise;
		}
	
		// for (let spigot = 0; spigot < 16; spigot++) {
		// 	if (card.doShuffleFix) {
		// 		const commandAudioSelect = `rolltrak -a ${cardIP} 8500@0000:30:00=${spigot}`;
		// 		const newPromiseAudioSelect = rolltrak.run(commandAudioSelect);
		// 		await newPromiseAudioSelect;
	
		// 		const commandAudioSet = `rolltrak -a ${cardIP} 8501@0000:30:00=0`;
		// 		const newPromiseAudioSet = rolltrak.run(commandAudioSet);
		// 		await newPromiseAudioSet;
		// 	}
	
		// 	for (const commandID in frame.spigotCommands) {
		// 		if (!Object.hasOwnProperty.call(frame.spigotCommands, commandID)) return;
		// 		const value = frame.spigotCommands[commandID];
		// 		const command = `rolltrak -a ${frameIP} ${Number(commandID)+(14*spigot)}@0000:10:${slot}=${value}`;
		// 		Logs.debug(`Running: ${command}`);
		// 		const newPromise = rolltrak.run(command);
		// 		await newPromise;
		// 	}
		// }
		console.log(`Done all pushes for ${cardIP}`);
	}
}
