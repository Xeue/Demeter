/* eslint-disable no-unused-vars */

import path from 'path';
import {Logs as _Logs} from 'xeue-logs';
import { Config } from 'xeue-config';
import { Shell } from 'xeue-shell';
import { app, BrowserWindow, ipcMain as frontend } from 'electron';
import { version } from './package.json';
import electronEjs from 'electron-ejs';
import { MicaBrowserWindow, IS_WINDOWS_11 } from 'mica-electron';
import JSON5 from 'json5';
import fs from 'fs';
// import unhandled from 'electron-unhandled';

// unhandled();

const background = IS_WINDOWS_11 ? 'micaActive' : 'bg-dark';

const __internal = import.meta.dirname;
const __data = path.join(app.getPath("documents"), 'DemeterData');
const __static = `${app.isPackaged ? process.resourcesPath : __internal}/static`;
// const __views = `${app.isPackaged ? process.resourcesPath : __internal}/views`;
app.commandLine.appendSwitch('js-flags', '--max-old-space-size=4096');


/* Data Defines */

type SlotInfo = {
	ins: number,
	outs: number,
	active: any,
	commands: any,
}

type GroupsDB = {
	card: GroupDB[],
	spigot: GroupDB[]
}

type GroupDB = {
	name: string,
	commands: CommandDB[]
}

type CommandDB = {
	command: number,
	name: string,
	type: string,
	increment?: number,
	default?: any,
	options?: {[key: string|number]:string|number},
	depends?: {[key: string|number]:string|number},
	take?: 4051,
	restart?: boolean,
	shuffle?: boolean,
	inOnly: boolean
}

type Groups = {
	[key: string]:Group
}

type Group = {
	name: string,
	enabled: boolean,
	commands: {[key: string]:CommandDef}
}

type CommandDef = {
	value: string,
	enabled: boolean,
	type: string,
	dataType: string,
	increment: string,
	take: number
}

type Command = {
	value: string | number,
	type: string,
}

type RolltrakCommand = {
	command: string,
	value: string | number,
}

type Frames = {
	[key: string]:Frame
}

type Frame = {
	number: string,
    name: string,
    ip: string,
    enabled: boolean,
	scan: boolean,
    group: string,
    slots: {[key: string]:Slot},
    done: boolean,
    offline?: boolean,
	type: string
}

type Slot = {
	enabled: boolean,
	ipa?: string,
	ipb?: string,
	ipaup?: string,
	ipbup?: string,
	sfp1?: string,
	sfp2?: string,
	offline: boolean,
	prefered: {[key:string]:FramePrefered},
	active: {[key: string]:string|boolean|number},
	group: {[key:string]:FrameGroup},
	ins: number,
	outs: number
}

type FrameGroup = {
	value: number|string,
	type: string,
	enabled: boolean,
	take: number
}

type FramePrefered = {
	value: number|null|string,
	enabled: boolean,
	type: string,
	dataType?: string,
	take?: number
}

/* Globals */

let mainWindow:MicaBrowserWindow|BrowserWindow;
let jobs = 0;
let requestSave = false;
const pollRate = 3;
const saveRate = 5;
const frameCommandsList = [4108, 4101, 4103, 4105, 4208, 4201, 4203, 4205]
const shufflesList = [50265, 50565, 50865, 51165, 51465, 51765, 52065, 52365, 52665, 52965, 53265, 53565, 53865, 54165, 54465, 54765]

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

const commandsJSON = fs.readFileSync(path.join(__internal, 'commandsDB.json'));
const commandsDB:GroupsDB = JSON5.parse(commandsJSON.toString());

let frames:Frames = loadData('frames');
let groups:Groups = loadData('groups');

/* Start App */

(async () => {

	await app.whenReady();
	await setUpApp();
	await createWindow();

	{ /* Config */
		Logs.printHeader('Demeter');
		config.path(__data)
		config.require('systemName', [], 'What is the name of the system');
		config.require('loggingLevel', {'A':'All', 'D':'Debug', 'W':'Warnings', 'E':'Errors'}, 'Set logging level');
		config.require('createLogFile', {true: 'Yes', false: 'No'}, 'Save logs to local file');
		config.require('debugLineNum', {true: 'Yes', false: 'No'}, 'Print line numbers', ['advancedConfig', true]);

		config.default('systemName', 'Demeter');
		config.default('loggingLevel', 'W');
		config.default('createLogFile', true);
		config.default('debugLineNum', false);

		if (!await config.fromFile('config.conf')) {
			config.set('systemName', 'Demeter');
			config.set('loggingLevel', 'W');
			config.set('createLogFile', true);
			config.set('debugLineNum', false);
			config.write();
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
		config.userInput(async (command:string) => {
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

process.on('uncaughtException', error => {
	Logs.error('Uncaught error', error);
});

process.on('unhandledRejection', (error:any) => {
	Logs.error('Uncaught error', error);
});


/* Electron */

function frontendSend(command:string, data:any) {
	mainWindow.webContents.send(command, data);
}

async function setUpApp() {


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
			frames[data.ip].type = data.type;
		} else {
			frames[data.ip] = {
				"number":data.number,
				"name": data.name,
				"ip": data.ip,
				"enabled": false,
				"scan": true,
				"group": data.group,
				"slots": {},
				"done": true,
				"type": data.type
			};
		}
		save();
		frontendSend('frames', frames);
		Logs.info('Added/updated frames', frames);
	})

	frontend.on('getFrames', (event,data) => {
		frontendSend('frames', frames);
	})

	frontend.on('setCommand', (event, data) => {
		try {
			frames[data.ip].slots[data.slot].prefered[data.command] = {
				'value': data.value,
				'enabled': data.enabled,
				'dataType': data.dataType,
				'type': data.dataType,
				'take': data.take
			};
		} catch (error) {			
			if (!frames[data.ip].slots[data.slot].prefered) frames[data.ip].slots[data.slot].prefered = {};
			frames[data.ip].slots[data.slot].prefered[data.command] = {
				'value': data.value,
				'enabled': data.enabled,
				'dataType': data.dataType,
				'type': data.dataType,
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
				'enabled': data.enabled,
				'type': 'text',
			};
		}
		save();
	})

	frontend.on('enableFrame', (event, data) => {
		frames[data.ip].enabled = data.enabled;
		save();
	})

	frontend.on('scanFrame', (event, data) => {
		frames[data.ip].scan = data.scan;
		frames[data.ip].done = true;
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

	frontend.on('cardReboot', async (event, data) => {
		const rolltrak = new Shell(Logs, 'CHECK', 'D');
		const slot = String(Number(data.slot).toString(16)).padStart(2, '0')
		const [address, err] = await getFrameAddress(data.frameIP);
		if (err) {
			Logs.error(`Failed to reboot frame: ${data.frameIP}`, err);
		}
		let command = `rolltrak -a ${data.frameIP} 4114@0000:${address}:${slot}=1`;
		Logs.debug(`Rebboting frame: ${data.frameIP}, slot: ${data.slot}, command: ${command}`);
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
		frontendSend('groups', groups);
		Logs.info('Added/updated groups', groups);
	})

	frontend.on('getGroups', (event,data) => {
		frontendSend('groups', groups);
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
		frontendSend('groups', groups);
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
		frontendSend('frames', frames);
		Logs.debug('Saving');
		writeData('frames', frames);
		writeData('groups', groups);
	})


	app.on('activate', async () => {
		if (BrowserWindow.getAllWindows().length === 0) createWindow();
	});
}

async function createWindow() {
	const windowOptions: Electron.BrowserWindowConstructorOptions = {
		width: 1640,
		height: 1220,
		autoHideMenuBar: true,
		webPreferences: {
			contextIsolation: true,
			preload: path.resolve(__internal, 'preload.js')
		},
		icon: path.join(__static, 'img/icon/demeter.ico'),
		show: false,
		frame: false,
		titleBarStyle: 'hidden',
		titleBarOverlay: {
			color: '#313d48',
			symbolColor: '#ffffff',
			height: 49
		}
	}
	
	if (IS_WINDOWS_11) {
        mainWindow = new MicaBrowserWindow(windowOptions);
        if (mainWindow instanceof  MicaBrowserWindow) {
            mainWindow.setDarkTheme();
            mainWindow.setMicaEffect();
        }
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

	mainWindow.loadURL(path.resolve(__internal, 'views/app.ejs'));
}

async function sleep(seconds: number) {
	await new Promise (resolve => setTimeout(resolve, 1000*seconds));
}

async function getFrameAddress(frameIP: string) {
	try {		
		const [unitAddresses, err] = await getInfo([17044, 16482], frameIP, '00', '00');
		if (err) {
			return ['10', err];
		}
		if (unitAddresses[17044] !== 'Not In Use') {
			return [unitAddresses[17044].split('= 0x')[1] || '10', false];
		} else if (unitAddresses[16482].includes(':')) {
			return [unitAddresses[16482].split(':')[1] || '01', false];
		} else {
			return ['10', 'failed to get unit address'];
		}
	} catch (error) {
		return ['10', 'failed to get unit address'];
	}
}

async function checkFrame(frameIP: string) {
	Logs.debug(`Scanning frame ${frameIP}`);
	const rolltrak = new Shell(Logs, 'CHECK', 'D');

	const frame = frames[frameIP]
	if (!frame.scan) {
		frame.done = true;
		frontendSend('frameStatus', {'frameIP': frameIP, 'status': 'Not Scanning', 'offline':frame.offline});
		return 
	}
	const foundSlots:string[] = [];

	frame.done = false;

	frontendSend('frameStatus', {'frameIP': frameIP, 'status': 'Connecting to frame', 'offline':frame.offline});
	const [address, err] = await getFrameAddress(frameIP);
	if (err) {
		frame.offline = true;
		frontendSend('frameStatus', {'frameIP': frameIP, 'status': 'Cannot reach frame', 'offline':frame.offline});
		frame.done = true;
		save()
		Logs.debug(`Frame: ${frameIP} failed to get unit address, so probably not online`, err);
		return
	}
	
	frame.offline = false;

	const commands:number[] = [];
		
	for (let slot = 0; slot < 20; slot++) {
		commands.push(16530 + slot)
	}

	frontendSend('frameStatus', {'frameIP': frameIP, 'status': 'Discovering cards within frame', 'offline':frame.offline});
	const [slotsData, parseErr] = await getInfo(commands, frameIP, '00', address);

	if (parseErr) {
		Logs.error(`Frame: ${frameIP} Issue parsing slots data`, parseErr);
	}

	frontendSend('frameStatus', {'frameIP': frameIP, 'status': 'Getting cards current config', 'offline':frame.offline});
	for (const cmd in slotsData) {
		const slot = String(Number(cmd)-16529).padStart(2, '0');
		try {
			if (typeof slotsData[cmd] !== "string") {
				Logs.info(`Frame: ${frameIP} Slot: ${slot} is not a UCP`);
				if (frame.slots[slot]) frame.slots[slot].offline = true;
				continue;
			}
			if (!slotsData[cmd].includes('IQUCP25_SDI') && !slotsData[cmd].includes('IQUCP_MADI')) {
				Logs.info(`Frame: ${frameIP} Slot: ${slot} is not a UCP`);
				if (frame.slots[slot]) frame.slots[slot].offline = true;
				continue;
			}
			if (!frame.slots) frame.slots = {};
			if (!frame.slots[slot]) frame.slots[slot] = {
				offline: false,
				enabled: true,
				prefered: {},
				active: {},
				group: {},
				ins: 0,
				outs: 0
			}
			frame.slots[slot].offline = false;
			foundSlots.push(slot);
		} catch (error) {
			Logs.error(`Issue with slot: ${slot} at IP: ${frameIP}`, error);
			continue;
		}
	}

	const slotsPromises:Promise<void>[] = [];

	Logs.info(`Frame: ${frameIP} found slots`, foundSlots);
	foundSlots.forEach(async slot => {
		slotsPromises.push(new Promise(async (resolve, reject) => {
			const slotHex = Number(slot).toString(16).padStart(2, '0');
			let checkNull = false;
			const [cardInfo, err] = await getInfo([4101, 4103, 4105, 4108, 4128, 4129, 4201, 4203, 4205, 4208, 4228, 4229], frameIP, slotHex, address);

			const cardAIP = cardInfo[4101];
			const cardAMask = cardInfo[4103];
			const cardAGate = cardInfo[4105];
			const cardAMode = cardInfo[4108];
			const cardAUP = cardInfo[4128];
			const cardASFP = cardInfo[4129];
			const cardBIP = cardInfo[4201];
			const cardBMask = cardInfo[4203];
			const cardBGate = cardInfo[4205];
			const cardBMode = cardInfo[4208];
			const cardBUP = cardInfo[4228];
			const cardBSFP = cardInfo[4229];

			if (!frame.slots[slot]) frame.slots[slot] = {
				enabled: true,
				offline: false,
				ins: 0,
				outs: 0,
				prefered: {},
				active: {},
				group: {}
			};

			frame.slots[slot].ipa = cardAIP == "StringVal" ? null : cardAIP;
			frame.slots[slot].ipb = cardBIP == "StringVal" ? null : cardBIP;
			frame.slots[slot].ipaup = cardAUP;
			frame.slots[slot].ipbup = cardBUP;
			frame.slots[slot].sfp1 = cardASFP;
			frame.slots[slot].sfp2 = cardBSFP;
			
			if (err) {
				frontendSend('frameStatus', {'frameIP': frameIP, 'status': `Slot: ${slot} error resolving IPs`, 'offline':frame.offline});
				Logs.error(`Frame ${frameIP}, Slot: ${slot} error resolving IPs`, [cardAIP, cardBIP]);
				frame.slots[slot].offline = true;
				frame.done = true;
				resolve();
				return
			}
			
			let requestIP = '';
			if (cardAUP == "UP" && cardAIP !== "StringVal" && cardAIP !== "No rollcall connection") {
				requestIP = cardAIP;
				Logs.info(`Using primary IP: ${cardAIP}`)
			} else if (cardBUP == "UP" && cardBIP !== "StringVal" && cardBIP !== "No rollcall connection") {
				requestIP = cardBIP;
				Logs.info(`Using secondary IP: ${cardBIP}`)
			} else {
				Logs.warn(`Frame: ${frameIP} IPs for slot: ${slot} not found or not online`);
				Logs.info(`IP info for Frame: ${frameIP}, slot: ${slot}`, [[cardAIP, cardAMask, cardAGate, cardAUP],[cardBIP, cardBMask, cardBGate, cardBUP]])
			}

			if (frame.slots[slot].active === undefined) frame.slots[slot].active = {}
		

			if (requestIP) {
				const [slotInfo, err] = await checkCard(requestIP)
				if (err) {
					Logs.error(`Frame: ${frameIP}, Slot: ${slot} issue`, err);
					checkNull = true;
				} else {
					frame.slots[slot].ins = slotInfo.ins;
					frame.slots[slot].outs = slotInfo.outs;
					if (slotInfo.active !== undefined) frame.slots[slot].active = slotInfo.active;
				}
			}

			if (cardAIP !== "StringVal" && cardAIP !== "No rollcall connection") {
				frame.slots[slot].active['4101'] = cardAIP
			}
			if (cardAMask !== "StringVal" && cardAMask !== "No rollcall connection") {
				frame.slots[slot].active['4103'] = cardAMask
			}
			if (cardAGate !== "StringVal" && cardAGate !== "No rollcall connection") {
				frame.slots[slot].active['4105'] = cardAGate
			}
			if (cardAMode !== "StringVal" && cardAMode !== "No rollcall connection") {
				frame.slots[slot].active['4108'] = cardAMode
			}


			if (cardBIP !== "StringVal" && cardBIP !== "No rollcall connection") {
				frame.slots[slot].active['4201'] = cardBIP
			}
			if (cardBMask !== "StringVal" && cardBMask !== "No rollcall connection") {
				frame.slots[slot].active['4203'] = cardBMask
			}
			if (cardBGate !== "StringVal" && cardBGate !== "No rollcall connection") {
				frame.slots[slot].active['4205'] = cardBGate
			}
			if (cardBMode !== "StringVal" && cardBMode !== "No rollcall connection") {
				frame.slots[slot].active['4208'] = cardBMode
			}


			frame.slots[slot].group = computeGroupCommands(frame.group, frame.number, slot, frameIP);

			Logs.info(`Sending frame: ${frameIP} slot info to front end`);
			frontendSend('slotInfo', {
				"frame": frame,
				"slots": frame.slots
			});

			const cardCommands: {[key:string]:Command} = {};
			const frameCommands: {[key:string]:Command} = {};
			const cardTakes: {[key:string]:boolean} = {};
			const frameTakes: {[key:string]:boolean} = {};
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
					if (shufflesList.includes(Number(command))) {
						const index = ["Pass-through", "All Mute", "All Tone", "Custom"].indexOf(frame.slots[slot].active[command] as string)
						if (cmd.value != index) {
							cardCommands[command] = {
								"value": cmd.value,
								"type": "shuffle"
							}
						}
					} else {
						cardCommands[command] = {
							"value": cmd.value,
							"type": cmd.type
						}
					}
					if (cmd.take) cardTakes[cmd.take] = true;
				}
			}

			for (const command in frame.slots[slot].prefered) { // Working out card commands
				const cmd = frame.slots[slot].prefered[command];
				if (!cmd.enabled) continue;
				if (cmd.value == null) continue;
				if (cmd.value == frame.slots[slot].active[command]) {
					delete frameCommands[command]
					continue
				};
				if (frameCommandsList.includes(Number(command))) {
					frameCommands[command] = {
						"value": cmd.value,
						"type": cmd.type
					}
					if (cmd.take) frameTakes[cmd.take] = true;
				} else if (shufflesList.includes(Number(command))) {
					const index = ["Pass-through", "All Mute", "All Tone", "Custom"].indexOf(frame.slots[slot].active[command] as string)
					if (cmd.value != index) {
						Logs.object("Shuffles?", [cmd.value, index])
						cardCommands[command] = {
							"value": cmd.value,
							"type": "shuffle"
						}
					}
				} else {
					cardCommands[command] = {
						"value": cmd.value,
						"type": cmd.type
					}
				}
				if (cmd.take) cardTakes[cmd.take] = true;
			}

			save();

			if (!frame.enabled || !frame.slots[slot].enabled) {
				frame.done = true;
				resolve();
				frontendSend('frameStatus', {'frameIP': frameIP, 'status': `Slot: ${slot} not enabled`, 'offline':frame.offline});
				return
			}
			Logs.debug(`Frame: ${frameIP} Commands that need sending`, frameCommands);

			frontendSend('frameStatus', {'frameIP': frameIP, 'status': `Blasting slot: ${slot}`, 'offline':frame.offline});
			if (Object.keys(frameCommands).length > 0) {
				await doCommands(rolltrak, frameCommands, frameTakes, frameIP, address, slotHex);
			}
			if (cardAUP == "UP" || cardBUP == "UP") await doCommands(rolltrak, cardCommands, cardTakes, requestIP, cardAddress, cardSlot);
			save();
			resolve();
		}))
	})
	await Promise.all(slotsPromises);
	save();
	Logs.debug(`Done blasting frame ${frameIP}`);
	frame.done = true;
	frontendSend('frameStatus', {'frameIP': frameIP, 'status': 'Done', 'offline':frame.offline});
}

async function doCommands(rolltrak: Shell, cardCommands:{[key: string]: Command}, takes:{[key: string]: boolean}, requestIP:string, cardAddress:string, cardSlot:string) {
	const commandsArray:RolltrakCommand[] = []
	const shuffles = {};
	for (const command in cardCommands) {
		let val: string|number;
		switch (cardCommands[command].type) {
		case 'text':
		case 'smartip':
			val = `"${cardCommands[command].value}"`;
			break;
		case 'shuffle':
			shuffles[command] = cardCommands[command].value;
			continue;
		default:
			if (isNaN(parseFloat(cardCommands[command].value as string))) {
				val = `"${cardCommands[command].value}"`;
			} else {
				val = cardCommands[command].value;
			}
			break;
		}
		commandsArray.push({
			'command': `${command}@0000:${cardAddress}:${cardSlot}`,
			'value': val
		})
	}

	if (commandsArray.length > 0) {
		try {
			const toRun = commandsArray.map(command => `${command.command}=${command.value}`).join(' ');
			Logs.debug(`Running: rolltrak -a ${requestIP} ${toRun}`);
			Logs.log(`Frame: ${requestIP} Changes required, pushing: rolltrak -a ${requestIP} ${toRun}`);
			jobs++
			await rolltrak.run(`rolltrak -a ${requestIP} ${toRun}`, false);
			jobs--
			if (Object.keys(takes).length > 0) {
				const toTake = Object.keys(takes).filter(take=>take).map(take => `${take}@0000:${cardAddress}:${cardSlot}=1`).join(' ');
				Logs.debug(`Running: rolltrak -a ${requestIP} ${toTake}`);
				jobs++
				await rolltrak.run(`rolltrak -a ${requestIP} ${toTake}`, false);
				jobs--
			}
		} catch (error) {
			Logs.error('Error sending changes', error)
		}
	}
	// If there are shuffles, we need to run them separately
	// as they are not in the same format as the other commands
	if (Object.keys(shuffles).length > 0) {
		Logs.debug(`Frame: ${requestIP} shuffles needed`)
		frontendSend('frameStatus', {'frameIP': requestIP, 'status': `Doing shuffles for slot ${cardSlot}`, 'offline':false});
	}
	for (const command in shuffles) {
		const spigot = ((Number(command)-50265)/300);
		jobs++
		// Logs.log(`rolltrak -a ${requestIP} ${8500}@0000:${cardAddress}:${cardSlot}=${spigot} ${8501}@0000:${cardAddress}:${cardSlot}=${shuffles[command]}`)
		await rolltrak.run(`rolltrak -a ${requestIP} ${8500}@0000:${cardAddress}:${cardSlot}=${spigot} ${8501}@0000:${cardAddress}:${cardSlot}=${shuffles[command]}`, false);
		jobs--
	}
}

function computeGroupCommands(group:string, frameNumber:string, slotNum:string, frameIP:string) {
	const slotNumber = Number(slotNum)
	const commands = {}
	for (const commandID in groups[group].commands) {
		if (!Object.prototype.hasOwnProperty.call(groups[group].commands, commandID)) continue;
		const command = groups[group].commands[commandID];
		if (!command.enabled) continue;
		
		const value = command.value.replaceAll('FRAME', frameNumber).replaceAll('SLOT', slotNum).replaceAll('CARD', String(Math.floor(slotNumber/2)));

		if (command.type == "card") {
			const [cmd, err] = parseCommand(value, command.dataType, command.take)
			if (err) {
				frontendSend('frameError', {'frameIP': frameIP, 'message': err});
			} else {
				commands[commandID] = cmd
			}
		} else {
			for (let spigot = 0; spigot < 16; spigot++) {
				const take = Number(command.take)+(Number(command.increment)*spigot);
				const [cmd, err] = parseCommand(value.replaceAll('SPIGOT', String(spigot+1)), command.dataType, take);
				if (err) {
					frontendSend('frameError', {'frameIP': frameIP, 'message': err});
				} else {
					commands[Number(commandID)+(Number(command.increment)*spigot)] = cmd
				}
			}
		}
	}
	return commands
}

function parseCommand(command: string, type: string, take: number):[FrameGroup,boolean|string] {
	try {
		switch (type) {
		case 'smartip':
			const octets = command.split('.');
			return [{
				"value": octets.map(octet=>{return eval(octet)}).join('.'),
				"type": type,
				"enabled": true,
				"take": take
			}, false];
		default:
			try {
				return [{"value":eval(command), "type": type, "enabled": true, "take": take}, false];
			} catch (error) {
				return [{"value":command, "type": type, "enabled": true, "take": take}, false];
			}
		}
	} catch (error) {
		let empty:FrameGroup = <FrameGroup>{}
		if (error) return [empty, error]
		else return [empty, true]
	}
}

async function checkCard(cardIP: string):Promise<[SlotInfo, boolean | string]> {
	const requestAddress = '30';
	const requestSlot = '00';
	const slotInfo:SlotInfo = {
		"active": {},
		"commands": {},
		"ins": 0,
		"outs": 0
	};
	const [IOString, err] = await getInfo(18000, cardIP, requestSlot, requestAddress);
	if (err) {
		return [slotInfo, err];
	}
	try {		
		const [[string, ins, outs]] = IOString.matchAll(/([0-9]{1,2}) In.*?([0-9]{1,2}) Out/g);
		slotInfo.ins = Number(ins);
		slotInfo.outs = Number(outs);
	} catch (error) {
		return [slotInfo, 'Unable to match on IO string']
	}

	let commands:number[] = [];

	commandsDB.card.forEach(group => {
		group.commands.forEach(command => {
			// if (!command.shuffle) commands.push(command.command);
			commands.push(command.command);
		})
	});

	for (let index = 0; index < 16; index++) {
		commandsDB.spigot.forEach(group => {
			group.commands.forEach(command => {
				// if (command.inOnly && index < slotInfo.ins) {
				// 	return
				// } else {
					if (command.increment) {
						commands.push(command.command + (command.increment*index));
					} else {
						commands.push(command.command);
					}
				// }
			})
		});
	}

	if (commands.length < 1) return [slotInfo, 'No return values'];

	const [data, dataErr] = await getInfo(commands, cardIP, requestSlot, requestAddress)

	if(dataErr) {
		return [slotInfo, dataErr];
	}

	for (const command in data) {
		slotInfo.active[command] = data[command];
	}

	return [slotInfo, false];
}

async function getInfo(commandID: number[]|number, frameIP:string, slot:string, address = '10'):Promise<[any, boolean | string]> {
	jobs++
	try {
		if (!Array.isArray(commandID)) {
			commandID = [commandID]
		}
		const commandString = commandID.map(command => `${command}@0000:${address}:${slot}?`).join(' ');
		const rolltrak = new Shell(Logs, 'DDS', 'D');
		Logs.info(`rolltrak -a ${frameIP} ${commandString}`);
		const output = await rolltrak.run(`rolltrak -a ${frameIP} ${commandString}`, false);
		const {stdout, stderr} = output;
		const [rows, err] = parseTrackData(stdout)
		jobs--
		if (err) {
			return [rows, err]
		}
		switch (Object.keys(rows).length) {
		case 0:
			return [rows, 'No return data'];
		case 1:
			return [Object.values(rows)[0], false];
		default:
			return [rows, false];
		}
	} catch (error) {
		// Logs.warn(`Frame: ${frameIP}, Slot: ${slot}, General error connecting`, error);
		jobs--
		return ['', error];
	}
}

function parseTrackData(rows:string[]):[any, string | boolean] {
	try {
		if (rows[0].includes('No rollcall connection')) {
			return [{},'Rollcall connection timeout'];
		}
		if (rows.length < 1) {
			return [{},'Not enough rows returned'];
		}
		rows[0] = rows[0].split('\r')[1]
		const out = {}
		rows.forEach(row=>{
			const split = row.split('\t');
			let val:string|number = split[7];
			if (split[6] !== "") val = Number(split[6]);
			out[split[5]] = val;
		})
		return [out, false]
	} catch (error) {
		return [{}, 'Issue parsing data'];
	}
}


function startLoops() {
	Logs.debug("Scanning all frames")
	for (const frameIP in frames) {
		if (!Object.prototype.hasOwnProperty.call(frames, frameIP)) continue
		checkFrame(frameIP)
	}
	Logs.debug(`Current Jobs: ${jobs}`);
	setInterval(()=>{
		Logs.debug("Scanning all frames")
		for (const frameIP in frames) {
			if (!Object.prototype.hasOwnProperty.call(frames, frameIP)) continue
			if (!frames[frameIP].done) continue
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

function loadData(file: string) {
	try {
		if (!fs.existsSync(`${__data}/data/`)){
			fs.mkdirSync(`${__data}/data/`);
		}
		const dataRaw = fs.readFileSync(`${__data}/data/${file}.json`);
		try {
			return JSON.parse(dataRaw.toString());
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

function writeData(file: string, data: any) {
	try {
		if (!fs.existsSync(`${__data}/data/`)){
			fs.mkdirSync(`${__data}/data/`);
		}
		fs.writeFileSync(`${__data}/data/${file}.json`, JSON.stringify(data, undefined, 2));
	} catch (error) {
		Logs.error(`Could not write the file ${file}.json, do we have permission to access the file?`, error);
	}
}