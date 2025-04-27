/* eslint-disable no-unused-vars */

const path = require('path');
const _Logs = require('xeue-logs').Logs;
const {Config} = require('xeue-config');
const {Shell} = require('xeue-shell');
const {app, BrowserWindow, ipcMain: frontend} = require('electron');
const {version} = require('./package.json');
const electronEjs = require('electron-ejs');
const {MicaBrowserWindow, IS_WINDOWS_11} = require('mica-electron');
const JSON5 = require('json5')
const fs = require('fs');

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
let jobs = 0;
const pollRate = 5;
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

const commandsJSON = fs.readFileSync(path.join(__main, 'commandsDB.json'));
const commandsDB = JSON5.parse(commandsJSON);

const frames = loadData('frames');
const groups = loadData('groups');

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
			}
		});
		configLoaded = true;
	}

	startLoops();
	doRollTrak();

})().catch(error => {
	console.log(error);
});

const ejs = new electronEjs({
	'static': __static,
	'background': background,
	'version': version,
	'systemName': config.get('systemName'),
	'commands': commandsDB
}, {});



/* Electron */


async function setUpApp() {

	frontend.send = (command, data) => mainWindow.webContents.send(command, data);

	frontend.on('window', (event, message) => {
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

	/* Frames */

	frontend.on('addFrame', (event, data) => {
		if (frames[data.ip]) {
			frames[data.ip].number = data.number;
			frames[data.ip].name = data.name;
			frames[data.ip].ip = data.ip;
			frames[data.ip].group = data.group;
		} else {
			frames[data.ip] = {
				"number":data.number,
				"name": data.name,
				"ip": data.ip,
				"enabled": false,
				"slots": {}
			};
		}
		save();
		frontend.send('frames', frames);
		Logs.info('Added/updated frames', frames);
	})

	frontend.on('getFrames', (event,data) => {
		frontend.send('frames', frames);
	})

	frontend.on('setCommand', (event, data) => {
		try {
			frames[data.ip].slots[data.slot].prefered[data.command] = {
				'value': data.value,
				'enabled': data.enabled
			};
		} catch (error) {			
			if (!frames[data.ip].slots[data.slot].prefered) frames[data.ip].slots[data.slot].prefered = {};
			frames[data.ip].slots[data.slot].prefered[data.command] = {
				'value': data.value,
				'enabled': data.enabled
			};
		}
		save();
	})

	frontend.on('setEnable', (event, data) => {
		try {
			frames[data.ip].slots[data.slot].prefered[data.command].enabled = data.enabled
		} catch (error) {			
			if (!frames[data.ip].slots[data.slot].prefered) frames[data.ip].slots[data.slot].prefered = {};
			frames[data.ip].slots[data.slot].prefered[data.command] = {
				'value': null,
				'enabled': data.enabled
			};
		}
		save();
	})

	frontend.on('enableFrame', (event, data) => {
		frames[data.ip].enabled = data.enabled;
		save();
	})

	frontend.on('enableSlot', (event, data) => {
		frames[data.ip].slots[data.slot].enabled = data.enabled;
		save();
	})

	/* Groups */

	frontend.on('addGroup', (event,data) => {
		if (groups[data.name]) {
			groups[data.name].name = data.name
			groups[data.name].enabled = data.enabled
		} else {
			groups[data.name] = {
				"enabled": false,
				"name": data.name,
				"commands": {}
			}
		}
		save();
		frontend.send('groups', groups);
		Logs.info('Added/updated groups', groups);
	})

	frontend.on('getGroups', (event,data) => {
		frontend.send('groups', groups);
	})


	frontend.on('setGroupCommand', (event, data) => {
		try {
			groups[data.group].commands[data.command] = {
				'value': data.value,
				'enabled': data.enabled,
				"type": data.type,
				"dataType": data.dataType,
				"increment": data.increment
			};
		} catch (error) {			
			if (!groups[data.group].commands) groups[data.group].commands = {};
			groups[data.group].commands[data.command] = {
				'value': data.value,
				'enabled': data.enabled,
				"type": data.type,
				"dataType": data.dataType,
				"increment": data.increment
			};
		}
		save();
	})

	frontend.on('enableGroup', (event, data) => {
		groups[data.name].enabled = data.enabled;
		save();
	})

	// frontend.on('enable')

	app.on('before-quit', function () {
		isQuiting = true;
	});

	app.on('activate', async () => {
		if (BrowserWindow.getAllWindows().length === 0) createWindow();
	});

	Logs.on('logSend', message => {
		if (!isQuiting) frontend.send('log', message);
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

async function sleep(seconds) {
	await new Promise (resolve => setTimeout(resolve, 1000*seconds));
}

async function checkFrame(frameIP) {
	const rolltrak = new Shell(Logs, 'CHECK', 'D');
	rolltrak.on('stdout', stdout=>{
		//Logs.log(stdout);
	})

	const frame = frames[frameIP]
	const foundSlots = [];
	const checking = [];

	frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Getting rollcall address of frame'});
	const unitAddress = await getInfo(17044, frameIP, '00', '00');
	const address = unitAddress.split('= 0x')[1] || '10';

	let command = `rolltrak -a ${frameIP} 0@0000:${address}:10?`;
		
	for (let slot = 0; slot < 20; slot++) {
		command += ` ${16530 + slot}@0000:${address}:00?`;
	}

	Logs.info(command);
	frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Discovering cards within frame'});
	jobs++
	const {stdout} = await rolltrak.run(command, false);
	jobs--
	const slotsData = parseTrackData(stdout);

	frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Checking cards current config'});
	slotsData.forEach((slot, index) => {
		try {
			if (!slot.includes('IQUCP25_SDI')) return;
			foundSlots.push(String((1+index).toString(16)).padStart(2, '0'));
		} catch (error) {
			Logs.warn(`Issue with slot: ${checking[index].slot} at IP: ${checking[index].ip}`, error);
		}
	});

	const slotsPromises = [];

	foundSlots.forEach(async slot => {
		slotsPromises.push(new Promise(async (resolve, reject) => {
			const cardIPA = await getInfo(4101, frameIP, slot, address);
			const cardIPB = await getInfo(4201, frameIP, slot, address);
			let requestIP = frameIP;
			if (cardIPB && cardIPB !== "StringVal") {
				requestIP = cardIPB
			}
			if (cardIPA && cardIPA !== "StringVal") {
				requestIP = cardIPA
			}

			if (cardIPA == "StringVal" && cardIPB == "StringVal") return;

			const slotInfo = await checkCard(requestIP)

			frame.slots[slot].ipa = cardIPA ? cardIPA : null;
			frame.slots[slot].ipb = cardIPB ? cardIPB : null;
			frame.slots[slot].ins = slotInfo.ins;
			frame.slots[slot].outs = slotInfo.outs;
			frame.slots[slot].active = slotInfo.active;
			frame.slots[slot].group = computeGroupCommands(frame.group, frame.number, slot);
			if (frame.enabled && frame.slots[slot].enabled) {
				const toSend = []
				for (const command in frame.slots[slot].prefered) {
					if (!Object.prototype.hasOwnProperty.call(frame.slots[slot].prefered, command)) continue;
					const cmd = frame.slots[slot].prefered[command];
					if (!cmd.enabled) continue;
					if (cmd.value == frame.slots[slot].active[command]) continue;
					const cardSlot = '00';
					const cardAddress = '30';
					if (cmd.value == null) continue;
					toSend.push({
						'command': `${command}@0000:${cardAddress}:${cardSlot}`,
						'value': cmd.value
					})
					// if (cmd.)
				}

				if (toSend.length > 0) {
					Logs.log('Changes required, pushing')
					try {						
						const toRun = toSend.map(command => `${command.command}=${command.value}`).join(' ');
						Logs.debug(`Running: rolltrak -a ${requestIP} ${toRun}`);
						jobs++
						rolltrak.run(`rolltrak -a ${requestIP} ${toRun}`, false).then(()=>{jobs--});
					} catch (error) {
						Logs.error('Error sending changes', error)
					}
				}
			}
			resolve();
		}))

	})
	await Promise.all(slotsPromises);

	frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Retrieved current cards and config'});
	frontend.send('slotInfo', {
		"frame": frame,
		"slots": frame.slots
	});
}

function computeGroupCommands(group, frameNumber, slotNumber) {
	const commands = {}
	for (const command in groups[group].commands) {
		if (!Object.prototype.hasOwnProperty.call(groups[group].commands, command)) return;
		const commandDef = groups[group].commands[command];
		if (!commandDef.enabled) return;
		commandDef.value = commandDef.value.replaceAll('FRAME', frameNumber).replaceAll('SLOT', slotNumber).replaceAll('CARD', Math.floor(slotNumber/2));
		if (commandDef.type == "card") {
			commands[command] = parseCommand(commandDef.value, commandDef.dataType)
		} else {
			for (let spigot = 0; spigot < 16; spigot++) {
				commands[command+(commandDef.increment*spigot)] = parseCommand(commandDef.value.replaceAll('SPIGOT', spigot), commandDef.dataType)
			}
		}
	}
	return commands
}

function parseCommand(command, type) {
	switch (type) {
		case 'smartip':
			const octets = command.split('.');
			return octets.map(octet=>exec(octet)).join();
		case 'text':
		case 'boolean':
		case 'select':
			return exec(command)
	}
}

async function checkCard(cardIP) {
	const rolltrak = new Shell(Logs, 'CHECK', 'D');
	const requestAddress = '30';
	const requestSlot = '00';
	const slotInfo = {};
	const IOString = await getInfo(18000, cardIP, requestSlot, requestAddress);
	const [[string, ins, outs]] = IOString.matchAll(/([0-9]{1,2}) In.*?([0-9]{1,2}) Out/g);
	slotInfo.ins = Number(ins);
	slotInfo.outs = Number(outs);
	slotInfo.active = {};
	slotInfo.commands = {};

	let commands = [];

	commandsDB.card.forEach(group => {
		group.commands.forEach(command => {
			if (command.command != "shuffle") commands.push(command.command);
		})
	});

	for (let index = 0; index < slotInfo.ins; index++) {		
		commandsDB.spigot.forEach(group => {
			group.commands.forEach(command => {
				if (command.command != "shuffle") commands.push(command.command + (command.increment*index));
			})
		});
	}

	const toCheck = commands.map(command => `${command}@0000:${requestAddress}:${requestSlot}?`).join(' ');

	Logs.info(`rolltrak -a ${cardIP} 0@0000:${requestAddress}:${requestSlot}? ${toCheck}`);
	jobs++
	const {stdout} = await rolltrak.run(`rolltrak -a ${cardIP} 0@0000:${requestAddress}:${requestSlot}? ${toCheck}`, false);
	jobs--
	stdout.shift();
	rows = stdout.join("\r\n").split("\r\n");
	rows.forEach(row => {
		const values = row.split('\t').filter(n=>n);
		slotInfo.active[values[5]] = values[6];
	})
	return slotInfo;
}

async function getInfo(commandID, frameIP, slot, address = '10') {
	jobs++
	Logs.debug(`Jobs: ${jobs}`);
	try {
		const rolltrak = new Shell(Logs, 'DDS', 'D');
		Logs.info(`rolltrak -a ${frameIP} ${commandID}@0000:${address}:${slot}?`);
		const output = await rolltrak.run(`rolltrak -a ${frameIP} ${commandID}@0000:${address}:${slot}?`, false); // Get's I/O arrangement
		const {stdout, stderr} = output;
		let outArr;
		try {
			outArr = stdout[0].split('\r\n');
		} catch (error) {
			Logs.object(outArr);
			Logs.warn(`Issue getting info from slot ${slot} at IP: ${frameIP}`, error);
			jobs--
			return '';
		}
		const checkOut = outArr.length > 1 ? 1 : 0;
		const returnString = outArr[checkOut];
		if (returnString.includes('No rollcall connection')) {
			Logs.warn(`Issue getting info from slot ${slot} at IP: ${frameIP}`);
			jobs--
			return '';
		}
		jobs--
		return returnString.split('\t')[7];
	} catch (error) {
		Logs.warn(`Issue getting info from slot ${slot} at IP: ${frameIP}`, error);
		jobs--
		return '';
	}
}

function parseTrackData(rows) {
	try {
		if (rows[0].includes('No rollcall connection')) {
			Logs.warn('Rollcall connection timeout');
			return [];
		}
		if (rows.length < 2) {
			Logs.warn('Not enough rows returned');
			return [];
		}
		rows.shift();
		return rows.map(row => row.split('\t')[7])
	} catch (error) {
		Logs.warn('Issue parsing data', error);
		return [];
	}
}


function startLoops() {
	setInterval(()=>{
		Logs.debug("Scanning all frames")
		for (const frameIP in frames) {
			if (!Object.prototype.hasOwnProperty.call(frames, frameIP)) return
			checkFrame(frameIP)
		}
	}, pollRate*1000)
}


function save() {
	writeData('frames', frames)
	writeData('groups', groups)
}

function loadData(file) {
	try {
		const dataRaw = fs.readFileSync(`${__data}/data/${file}.json`);
		try {
			return JSON.parse(dataRaw);
		} catch (error) {
			Logs.error(`There is an error with the syntax of the JSON in ${file}.json file`, error);
			return [];
		}
	} catch (error) {
		Logs.warn(`Could not read the file ${file}.json, attempting to create new file`);
		Logs.debug('File error:', error);
		const fileData = {};
		if (!fs.existsSync(`${__data}/data/`)){
			fs.mkdirSync(`${__data}/data/`);
		}
		fs.writeFileSync(`${__data}/data/${file}.json`, JSON.stringify(fileData, null, 4));
		return fileData;
	}
}

function writeData(file, data) {
	try {
		fs.writeFileSync(`${__data}/data/${file}.json`, JSON.stringify(data, undefined, 2));
	} catch (error) {
		Logs.error(`Could not write the file ${file}.json, do we have permission to access the file?`, error);
	}
}











































async function doRollTrak() {
	return;
	const rolltrak = new Shell(Logs, 'DDS', 'D');

	rolltrak.on('stdout', stdout=>{
		//Logs.log(stdout);
	})

	const globalCommands = {
		'spigotCommands': {
			'58644': 16,// Channel Count
			'58645': 1, // Packet Timing
			'50007': 1, // Extended Headers
			// 'take': true
		},

		'cardCommands': {
			'48729': 1,//Interop
			'48730': 1,//Interop
			// '4501': 3, // Ref Chassis
			// '21000': 1, // PTP Multicast
			// '21046':1, // PTP Eth 1
			// '21047':1, // PTP Eth 2
			// '21074':3  // PTP Best
			'48700': 0, // Audio extended headers
			'48703': 0  // Meta extended headers
		},
		'doShuffleFix': true
	}

	const framesSpecifics = {
		// '10.40.44.10':{},
		'10.40.44.20':{"number":2},
		// '10.40.44.30':{},
		// '10.40.44.40':{},
		// '10.40.44.50':{},
		// '10.40.44.60':{},
		// '10.40.44.70':{},
		// '10.40.44.80':{},
		// '10.40.44.140':{},
		// '10.40.44.150':{},
		// '10.40.44.160':{},
		// '10.40.44.170':{},
		// '10.40.44.180':{},
		// '10.40.44.190':{},
		// '10.40.44.210':{},
		// '10.40.46.130':{},
		// '10.40.46.140':{},
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
	const cardsPromises = [];

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
		cardsPromises.push(doCard(cardIP, cards));
	}

	await Promise.all([
		Promise.all(framesPromises).then(() => Logs.log('All frames commands complete', ['C','FINISH', Logs.r])),
		Promise.all(cardsPromises).then(() => Logs.log('All cards commands complete', ['c','FINISH', Logs.r]))
	]);

	Logs.log('All commands complete');


	async function doFrame(frameIP, frames) {
		const frame = frames[frameIP]
		const foundSlots = [];
		const checking = [];
		
		Logs.log(`Get slots for frame ${frameIP}`);

		const unitAddress = await getInfo(17044, frameIP, '00', '00');
		const address = unitAddress.split('= 0x')[1] || '10';

		let command = `rolltrak -a ${frameIP} 0@0000:${address}:10?`;
		
		for (let slot = 0; slot < 20; slot++) {
			command += ` ${16530 + slot}@0000:${address}:00?`;
		}

		Logs.debug(`Running: ${command}`);
		const {stdout} = await rolltrak.run(command, false);
		const slotsData = parseTrackData(stdout);

		Logs.log(`All slots found for frame ${frameIP}`);
	
		Logs.debug('Found slots', slotsData);

		slotsData.forEach((slot, index) => {
			try {
				if (!slot.includes('IQUCP25_SDI')) return;
				foundSlots.push(String((1+index).toString(16)).padStart(2, '0'));
			} catch (error) {
				Logs.warn(`Issue with slot: ${checking[index].slot} at IP: ${checking[index].ip}`, error);
			}
		});
	
		Logs.object(foundSlots);

		const slotsPromises = [];

		foundSlots.forEach(async slot => {
			slotsPromises.push(new Promise(async (resolve, reject) => {
				const cardIP = await getInfo(4101, frameIP, slot, address);
				if (!cardIP) return reject('No card IP found');
				const cardSlot = '00';
				const cardAddress = '30';

				const commands = [];

				for (const commandID in frame.cardCommands) {
					if (!Object.hasOwnProperty.call(frame.cardCommands, commandID)) return;
					commands.push({
						'command': `${commandID}@0000:${cardAddress}:${cardSlot}`,
						'value': frame.cardCommands[commandID]
					})
				}

				const IOString = await getInfo(18000, cardIP, cardSlot, cardAddress);
				const [[string, ins, outs]] = IOString.matchAll(/([0-9]{1,2}) In.*?([0-9]{1,2}) Out/g);

				Logs.log(`Processing ${ins} ins`);

				if (frame.doShuffleFix) {
					for (let spigot = 0; spigot < 16; spigot++) {	
						commands.push({'command': `8500@0000:${cardAddress}:${cardSlot}`, 'value': spigot, 'noCheck': true});
						commands.push({'command': `8501@0000:${cardAddress}:${cardSlot}`, 'value': 0, 'noCheck': true});
					}
				}

				for (let spigot = 0; spigot < ins; spigot++) {
					for (const commandID in frame.spigotCommands) {
						if (!Object.hasOwnProperty.call(frame.spigotCommands, commandID)) return;
						const value = frame.spigotCommands[commandID];
						let incrementor = 14;
						if (commandID == 'take') {
							incrementor = 300;
							commands.push({'command': `${55040+(incrementor*spigot)}@0000:${cardAddress}:${cardSlot}`, 'value': 1, 'noCheck': true});
							commands.push({'command': `${55040+(incrementor*spigot)}@0000:${cardAddress}:${cardSlot}`, 'value': 0, 'noCheck': true});
						} else {
							if (['50007'].includes(commandID)) incrementor = 300;
							commands.push({'command': `${Number(commandID)+(incrementor*spigot)}@0000:${cardAddress}:${cardSlot}`, 'value': value});
						}
					}
				}
				const toRun = commands.map(command => `${command.command}=${command.value}`).join(' ');
				Logs.debug(`Running: rolltrak -a ${cardIP} ${toRun}`);
				await rolltrak.run(`rolltrak -a ${cardIP} ${toRun}`, false);

				const toCheckArr = commands.filter(command => !command.noCheck);
				const toCheck = toCheckArr.map(command => `${command.command}?`).join(' ');

				Logs.debug(`Running: rolltrak -a ${cardIP} 0@0000:${cardAddress}:${cardSlot}? ${toCheck}`);
				const {stdout} = await rolltrak.run(`rolltrak -a ${cardIP} 0@0000:${cardAddress}:${cardSlot}? ${toCheck}`, false);
				stdout.shift();
				const results = stdout.map(row => row.split('\t')[6]);
				const reCheck = [];
				toCheckArr.forEach((command, index) => {
					if (results[index] != String(command.value)) reCheck.push(command);
				})

				if (reCheck.length > 0) {
					Logs.warn(`${reCheck.length} commands failed, attempting to retry`);
					const toRun = reCheck.map(command => `${command.command}=${command.value}`).join(' ');
					Logs.debug(`Re-Running: rolltrak -a ${cardIP} ${toRun}`);
					await rolltrak.run(`rolltrak -a ${cardIP} ${toRun}`, false);
				} else {
					Logs.debug('All commands sucessfull');
				}

				resolve();
			}))

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
			await rolltrak.run(command);
		}
	
		for (let spigot = 0; spigot < 16; spigot++) {
			if (card.doShuffleFix) {
				const commandAudioSelect = `rolltrak -a ${cardIP} 8500@0000:30:00=${spigot}`;
				await rolltrak.run(commandAudioSelect);
	
				const commandAudioSet = `rolltrak -a ${cardIP} 8501@0000:30:00=0`;
				await rolltrak.run(commandAudioSet);
			}
	
			for (const commandID in card.spigotCommands) {
				if (!Object.hasOwnProperty.call(card.spigotCommands, commandID)) return;
				const value = card.spigotCommands[commandID];
				let incrementor = 14;
				if (commandID == 'take') {
					incrementor = 300;
					const command1 = `rolltrak -a ${cardIP} ${55040+(incrementor*spigot)}@0000:30:${slot}=1`;
					Logs.debug(`Running: ${command1}`);
					await rolltrak.run(command1);
					const command2 = `rolltrak -a ${cardIP} ${55040+(incrementor*spigot)}@0000:30:${slot}=0`;
					Logs.debug(`Running: ${command2}`);
					await rolltrak.run(command2);
				} else {
					const command = `rolltrak -a ${cardIP} ${Number(commandID)+(incrementor*spigot)}@0000:30:${slot}=${value}`;
					Logs.debug(`Running: ${command}`);
					await rolltrak.run(command);
				}
			}
		}
		Logs.log(`Done all pushes for ${cardIP}`);
	}
}
