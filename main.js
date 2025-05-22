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
let requestSave = false;
const pollRate = 1;
const saveRate = 5;
const devEnv = app.isPackaged ? './' : './';
const __main = path.resolve(__dirname, devEnv);
const __data = path.join(app.getPath('documents'), 'DemeterData');
const frameCommandsList = [4108, 4101, 4103, 4105, 4208, 4201, 4203, 4205]

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

let frames = loadData('frames');
let groups = loadData('groups');

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
			frames[data.ip].done = true;
		} else {
			frames[data.ip] = {
				"number":data.number,
				"name": data.name,
				"ip": data.ip,
				"enabled": false,
				"group": data.group,
				"slots": {},
				"done": true
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
				'enabled': data.enabled,
				'type': data.type,
				'take': data.take
			};
		} catch (error) {			
			if (!frames[data.ip].slots[data.slot].prefered) frames[data.ip].slots[data.slot].prefered = {};
			frames[data.ip].slots[data.slot].prefered[data.command] = {
				'value': data.value,
				'enabled': data.enabled,
				'type': data.type,
				'take': data.take
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

	frontend.on('deleteFrame', (event, data) => {
		delete frames[data.ip];
		save();
	})

	frontend.on('enableSlot', (event, data) => {
		frames[data.ip].slots[data.slot].enabled = data.enabled;
		save();
	})

	frontend.on('cardReboot', (event, data) => {
		const rolltrak = new Shell(Logs, 'CHECK', 'D');
		let command = `rolltrak -a ${data.frameIP} 4114@0000:10:${data.slot}=1`;
		Logs.debug(`Rebboting frame: ${data.frameIP}, slot: ${data.slot}`)
		rolltrak.run(command, false);
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
				"increment": data.increment,
				'take': data.take
			};
		} catch (error) {			
			if (!groups[data.group].commands) groups[data.group].commands = {};
			groups[data.group].commands[data.command] = {
				'value': data.value,
				'enabled': data.enabled,
				"type": data.type,
				"dataType": data.dataType,
				"increment": data.increment,
				'take': data.take
			};
		}
		save();
	})

	frontend.on('enableGroup', (event, data) => {
		groups[data.name].enabled = data.enabled;
		save();
	})

	frontend.on('setGroups', (event ,data) => {
		groups = data;
		frontend.send('groups', groups);
		Logs.debug('Saving');
		writeData('frames', frames);
		writeData('groups', groups);
	})

	frontend.on('deleteGroup', (event, data) => {
		delete groups[data.name];
		save();
	})

	frontend.on('setFrames', (event ,data) => {
		frames = data;
		frontend.send('frames', frames);
		Logs.debug('Saving');
		writeData('frames', frames);
		writeData('groups', groups);
	})

	// frontend.on('enable')

	app.on('before-quit', function () {
		isQuiting = true;
	});

	app.on('activate', async () => {
		if (BrowserWindow.getAllWindows().length === 0) createWindow();
	});

	// Logs.on('logSend', message => {
	// 	if (!isQuiting) frontend.send('log', message);
	// });
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
		icon: path.join(__static, 'img/icon/demeter.ico'),
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
		// Logs.info(stdout);
	})

	const frame = frames[frameIP]
	const foundSlots = [];

	frame.done = false;

	frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Connecting to frame', 'offline':frame.offline});
	const [unitAddress, err] = await getInfo(17044, frameIP, '00', '00');
	if (err) {
		frame.offline = true;
		frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Cannot reach frame', 'offline':frame.offline});
		frame.done = true;
		save()
		Logs.warn(unitAddress);
		return
	}
	frame.offline = false;
	const address = unitAddress.split('= 0x')[1] || '10';

	let command = `rolltrak -a ${frameIP} 0@0000:${address}:10?`;
		
	for (let slot = 0; slot < 20; slot++) {
		command += ` ${16530 + slot}@0000:${address}:00?`;
	}

	Logs.info(command);
	frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Discovering cards within frame', 'offline':frame.offline});
	jobs++
	const {stdout} = await rolltrak.run(command, false);
	jobs--
	const slotsData = parseTrackData(stdout);

	frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Getting cards current config', 'offline':frame.offline});
	slotsData.forEach((slotInfo, index) => {
		const slot = String((1+index).toString(10)).padStart(2, '0')
		try {
			if (!slotInfo.includes('IQUCP25_SDI') && !slotInfo.includes('IQUCP_MADI')) {
				Logs.info(`Frame: ${frameIP} Slot: ${slot} is not a UCP`);
				if (frame.slots[slot]) frame.slots[slot].offline = true;
				return
			}
			if (frame.slots[slot]) frame.slots[slot].offline = false;
			foundSlots.push(slot);
		} catch (error) {
			return Logs.warn(`Issue with slot: ${slot} at IP: ${frameIP}`, error);
		}
	});

	const slotsPromises = [];

	Logs.info(`Frame: ${frameIP} found slots`, foundSlots);
	foundSlots.forEach(async slot => {
		slotsPromises.push(new Promise(async (resolve, reject) => {
			const slotHex = Number(slot).toString(16).padStart(2, '0');
			let checkNull = false;
			const [cardIPA, err1] = await getInfo(4101, frameIP, slotHex, address);
			const [cardIPB, err2] = await getInfo(4201, frameIP, slotHex, address);
			const [card1UP, err3] = await getInfo(4128, frameIP, slotHex, address);
			const [card2UP, err4] = await getInfo(4228, frameIP, slotHex, address);
			const [card1SFP, err5] = await getInfo(4129, frameIP, slotHex, address);
			const [card2SFP, err6] = await getInfo(4229, frameIP, slotHex, address);
			if (!frame.slots[slot]) frame.slots[slot] = {
				"enabled": true
			};
			frame.slots[slot].ipa = cardIPA == "StringVal" ? null : cardIPA;
			frame.slots[slot].ipb = cardIPB == "StringVal" ? null : cardIPB;
			frame.slots[slot].ipaup = err3 ? 'ERROR' : card1UP;
			frame.slots[slot].ipbup = err4 ? 'ERROR' : card2UP;
			frame.slots[slot].sfp1 = err5 ? 'ERROR' : card1SFP;
			frame.slots[slot].sfp2 = err6 ? 'ERROR' : card2SFP;
			
			if (err1 && err2) {
				Logs.warn('Error resolving IPs of card', [cardIPA, cardIPB]);
				frame.slots[slot].offline = true;
				return resolve();
			}

			frame.slots[slot].offline = false;

			// if (err3 && err4) {
			// 	Logs.warn('Failed to get status of card IPs', [card1UP, card2UP]);
			// 	// return resolve();
			// }
			
			let requestIP = '';
			if (card1UP == "UP" && cardIPA !== "StringVal" && cardIPA !== "No rollcall connection") {
				requestIP = cardIPA;
				Logs.debug(`Using primary IP: ${cardIPA}`)
			} else if (card2UP == "UP" && cardIPB !== "StringVal" && cardIPB !== "No rollcall connection") {
				requestIP = cardIPB;
				Logs.debug(`Using secondary IP: ${cardIPB}`)
			} else {
				Logs.warn(`IPs for slot: ${slot} not found or not online`);
			}

			if (frame.slots[slot].active === undefined) frame.slots[slot].active = {}
		

			if (requestIP) {
				const [slotInfo, err] = await checkCard(requestIP)
				frame.slots[slot].ins = slotInfo.ins;
				frame.slots[slot].outs = slotInfo.outs;
				if (slotInfo.active !== undefined) frame.slots[slot].active = slotInfo.active;
				if (err) {
					Logs.warn(`Frame: ${frameIP}, Slot: ${slot} issue`, slotInfo);
					checkNull = true;
				}
			}

			if (frame.slots[slot].active['4101'] == undefined && cardIPA !== "StringVal" && cardIPA !== "No rollcall connection") {
				frame.slots[slot].active['4101'] = cardIPA
			}

			if (frame.slots[slot].active['4201'] == undefined && cardIPB !== "StringVal" && cardIPB !== "No rollcall connection") {
				frame.slots[slot].active['4201'] = cardIPB
			}


			frame.slots[slot].group = computeGroupCommands(frame.group, frame.number, slot);
			if (!frame.slots[slot].prefered) frame.slots[slot].prefered = {}

			Logs.info('Sending slot info to front end');
			frontend.send('slotInfo', {
				"frame": frame,
				"slots": frame.slots
			});

			const cardCommands = {};
			const frameCommands = {};
			const cardTakes = {};
			const frameTakes = {};
			const cardSlot = '00';
			const cardAddress = '30';

			for (const command in frame.slots[slot].group) { // Working out groups commands
				const cmd = frame.slots[slot].group[command];
				if (!cmd.enabled) continue;
				if (cmd.value == null) continue;
				if (checkNull) {
					if (frame.slots[slot].active[command] != null) {
						if (cmd.value == frame.slots[slot].active[command]) continue;
					}
				} else {
					if (frame.slots[slot].active[command] == null) continue;
					if (cmd.value == frame.slots[slot].active[command]) continue;
				}

				if (frameCommandsList.includes(Number(command))) {
					frameCommands[command] = {
						"value": cmd.value,
						"type": cmd.type
					}
					if (cmd.take) frameTakes[cmd.take] = true;
				} else {
					cardCommands[command] = {
						"value": cmd.value,
						"type": cmd.type
					}
					if (cmd.take) cardTakes[cmd.take] = true;
				}
			}

			for (const command in frame.slots[slot].prefered) { // Working out card commands
				const cmd = frame.slots[slot].prefered[command];
				if (!cmd.enabled) continue;
				if (cmd.value == null) continue;
				if (cmd.value == frame.slots[slot].active[command]) continue;
				if (frameCommandsList.includes(Number(command))) {
					frameCommands[command] = {
						"value": cmd.value,
						"type": cmd.type
					}
					if (cmd.take) frameTakes[cmd.take] = true;
				} else {
					cardCommands[command] = {
						"value": cmd.value,
						"type": cmd.type
					}
					if (cmd.take) cardTakes[cmd.take] = true;
				}
			}

			save();

			if (!frame.enabled || !frame.slots[slot].enabled) return resolve();
			Logs.debug(`Frame: ${frameIP} Commands that need sending`, frameCommands);
			
			doCommands(rolltrak, frameCommands, frameTakes, frameIP, "10", slotHex).then(()=>{
				if (card1UP == "UP" || card2UP == "UP") doCommands(rolltrak, cardCommands, cardTakes, requestIP, cardAddress, cardSlot);
			});
			save();
			resolve();
		}))
	})
	await Promise.all(slotsPromises);
	save();
	frame.done = true;
	frontend.send('frameStatus', {'frameIP': frameIP, 'status': 'Done', 'offline':frame.offline});
}

async function doCommands(rolltrak, cardCommands, takes, requestIP, cardAddress, cardSlot) {
	const commandsArray = []
	for (const command in cardCommands) {
		let val = '';
		switch (cardCommands[command].type) {
			case 'text':
			case 'smartip':
				val = `"${cardCommands[command].value}"`;
				break;
			default:
				val = cardCommands[command].value;
				break;
		}
		commandsArray.push({
			'command': `${command}@0000:${cardAddress}:${cardSlot}`,
			'value': val
		})
	}

	if (commandsArray.length > 0) {
		Logs.log('Changes required, pushing')
		try {
			const toRun = commandsArray.map(command => `${command.command}=${command.value}`).join(' ');
			Logs.debug(`Running: rolltrak -a ${requestIP} ${toRun}`);
			jobs++
			await rolltrak.run(`rolltrak -a ${requestIP} ${toRun}`, false);
			jobs--
			if (Object.keys(takes).length > 0) {
				const toTake = Object.keys(takes).map(take => `${take}@0000:${cardAddress}:${cardSlot}=1`).join(' ');
				Logs.debug(`Running: rolltrak -a ${requestIP} ${toTake}`);
				// jobs++
				// await rolltrak.run(`rolltrak -a ${requestIP} ${toTake}`, false);
				// jobs--
			}
		} catch (error) {
			Logs.error('Error sending changes', error)
		}
	}
}

function computeGroupCommands(group, frameNumber, slotNumber) {
	frameNumber = Number(frameNumber)
	slotNumber = Number(slotNumber)
	const commands = {}
	for (const commandID in groups[group].commands) {
		if (!Object.prototype.hasOwnProperty.call(groups[group].commands, commandID)) continue;
		const command = groups[group].commands[commandID];
		if (!command.enabled) continue;
		
		const value = command.value.replaceAll('FRAME', frameNumber).replaceAll('SLOT', slotNumber).replaceAll('CARD', Math.floor(slotNumber/2));

		if (command.type == "card") {
			commands[commandID] = parseCommand(value, command.dataType, command.take)
		} else {
			for (let spigot = 0; spigot < 16; spigot++) {
				const take = Number(command.take)+(Number(command.increment)*spigot);
				commands[Number(commandID)+(Number(command.increment)*spigot)] = parseCommand(value.replaceAll('SPIGOT', spigot+1), command.dataType, take);
			}
		}
	}
	return commands
}

function parseCommand(command, type, take) {
	switch (type) {
		case 'smartip':
			const octets = command.split('.');
			return {"value":octets.map(octet=>eval(octet)).join('.'), "type": type, "enabled": true, "take": take};;
		default:
			try {
				return {"value":eval(command), "type": type, "enabled": true, "take": take};
			} catch (error) {
				return {"value":command, "type": type, "enabled": true, "take": take};
			}
	}
}

async function checkCard(cardIP) {
	const rolltrak = new Shell(Logs, 'CHECK', 'D');
	const requestAddress = '30';
	const requestSlot = '00';
	const slotInfo = {
		"active": {},
		"commands": {},
		"ins": 0,
		"outs": 0
	};
	const [IOString, err] = await getInfo(18000, cardIP, requestSlot, requestAddress);
	if (err) {
		Logs.warn(IOString);
		return [slotInfo, true];
	}
	try {		
		const [[string, ins, outs]] = IOString.matchAll(/([0-9]{1,2}) In.*?([0-9]{1,2}) Out/g);
		slotInfo.ins = Number(ins);
		slotInfo.outs = Number(outs);
	} catch (error) {
		return ['Unable to match on IO string', true]
	}

	let commands = [];

	commandsDB.card.forEach(group => {
		group.commands.forEach(command => {
			if (!command.shuffle) commands.push(command.command);
		})
	});

	for (let index = 0; index < slotInfo.ins; index++) {		
		commandsDB.spigot.forEach(group => {
			group.commands.forEach(command => {
				commands.push(command.command + (command.increment*index));
			})
		});
	}

	if (commands.length < 1) return [slotInfo, true];

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
	return [slotInfo, false];
}

async function getInfo(commandID, frameIP, slot, address = '10') {
	jobs++
	try {
		const rolltrak = new Shell(Logs, 'DDS', 'D');
		Logs.info(`rolltrak -a ${frameIP} ${commandID}@0000:${address}:${slot}?`);
		const output = await rolltrak.run(`rolltrak -a ${frameIP} ${commandID}@0000:${address}:${slot}?`, false); // Get's I/O arrangement
		const {stdout, stderr} = output;
		let outArr;
		try {
			outArr = stdout[0].split('\r\n');
		} catch (error) {
			jobs--
			return [`Frame: ${frameIP}, Slot: ${slot}, Failed to parse rollcall data`, true];
		}
		const checkOut = outArr.length > 1 ? 1 : 0;
		const returnString = outArr[checkOut];
		if (returnString.includes('No rollcall connection')) {
			jobs--
			return [`Frame: ${frameIP}, Slot: ${slot}, No rollcall connection`, true];
		}
		jobs--
		return [returnString.split('\t')[7], false];
	} catch (error) {
		Logs.warn(`Frame: ${frameIP}, Slot: ${slot}, General error connecting`, error);
		jobs--
		return ['', true];
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
	Logs.debug("Scanning all frames")
	for (const frameIP in frames) {
		if (!Object.prototype.hasOwnProperty.call(frames, frameIP)) return
		checkFrame(frameIP)
	}
	Logs.debug(`Current Jobs: ${jobs}`);
	setInterval(()=>{
		Logs.debug("Scanning all frames")
		for (const frameIP in frames) {
			if (!Object.prototype.hasOwnProperty.call(frames, frameIP)) return
			if (!frames[frameIP].done) return
			checkFrame(frameIP)
		}
		Logs.debug(`Current Jobs: ${jobs}`);
	}, pollRate*1000)

	setInterval(()=>{
		if (!requestSave) return;
		Logs.debug('Saving');
		writeData('frames', frames);
		writeData('groups', groups);
		requestSave = false;
	}, saveRate*1000)
}


function save() {
	requestSave = true;
}

function loadData(file) {
	try {
		if (!fs.existsSync(`${__data}/data/`)){
			fs.mkdirSync(`${__data}/data/`);
		}
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
		if (!fs.existsSync(`${__data}/data/`)){
			fs.mkdirSync(`${__data}/data/`);
		}
		fs.writeFileSync(`${__data}/data/${file}.json`, JSON.stringify(data, undefined, 2));
	} catch (error) {
		Logs.error(`Could not write the file ${file}.json, do we have permission to access the file?`, error);
	}
}