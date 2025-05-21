window.pause = false;

let frames;
let groups;

document.addEventListener('DOMContentLoaded', () => {
	backend.on('frames', drawFrames);
	backend.on('log', doLog);
	backend.on('slotInfo', drawSlotInfo);
	backend.on('frameStatus', doFrameStatus);
	backend.on('groups', drawGroups);

	on('click', '#showLogs', ()=>showTab('logs'));
	on('click', '#showSelect', ()=>showTab('addDevices'));
	on('click', '#showCommands', ()=>showTab('addGroups'));
	on('click', '#addFrameIPBtn', addFrame);
	on('click', '#addGroupBtn', addGroup);

	on('click', '#exportGroups', ()=>{download('groups.json', JSON.stringify(window.groups, null,4))})
	on('click', '#exportFrames', ()=>{download('frames.json', JSON.stringify(window.frames, null,4))})
	on('click', '#importGroups', async ()=>{
		const _input = document.getElementById('importGroupsFile');
		console.log(_input)
		console.log(_input.files)
		console.log(_input.files.item(0))
		const groups = await _input.files.item(0).text()
		backend.send('setGroups', JSON.parse(groups));
	})
	on('click', '#importFrames', async ()=>{
		const _input = document.getElementById('importFramesFile');
		const frames = await _input.files.item(0).text()
		backend.send('setFrames', JSON.parse(frames));
	})

	/* Frame controls */

	on('change', '.frame_octet', (_element)=>doOctet(_element));
	on('change', '.frame_commandInput', (_element)=>doInput(_element));
	on('click', '.frame_commandCheck', (_element)=>doCheck(_element));
	on('click', '.frame_commandEnabled', (_element)=>doEnable(_element));
	on('click', '.slotEnable', (_element)=>doSlotEnable(_element));
	on('click', '.frameEnable', (_element)=>doFrameEnable(_element));
	on('click', '.frameDelete', (_element) => doFrameDelete(_element));
	on('click', '.groupDelete', (_element) => doGroupDelete(_element));

	/* Group controls */

	on('change', '.group_octet', (_element)=>doGroupOctet(_element));
	on('change', '.group_commandInput', (_element)=>doGroupInput(_element));
	on('click', '.group_commandCheck', (_element)=>doGroupCheck(_element));
	on('click', '.group_commandEnabled', (_element)=>doGroupCommandEnable(_element));
	on('click', '.groupEnable', (_element)=>doGroupEnable(_element));

	backend.send('getFrames')
	backend.send('getGroups')
});

function checkDeps(_cont) {
	try {		
		if (!_cont.classList.contains('collapseSection')) _cont = _cont.closest('.collapseSection');
		for (const _command of _cont.children) {
			const depsString = _command.getAttribute('data-depends').replace(/'/g, '"');
			if (depsString == '') continue;
			const deps = JSON.parse(depsString);
			let hidden = 0;
			for (const command in deps) {
				if (!Object.hasOwnProperty.call(deps, command)) continue;
				const _parent = _cont.querySelector(`[data-command="${command}"]`);
				const _input = _parent.querySelector('.commandInput');
				const value = _input.value;
				if (deps[command] != value) hidden++;
			}
			if (hidden > 0) _command.classList.add('d-none')
			else _command.classList.remove('d-none')
		}
	} catch (error) {
		console.log(error)
	}
}

/* Frames */

function addFrame() {
	const frameIP = document.getElementById('addFrameIP').value;
	const frameNumber = document.getElementById('addFrameNumber').value;
	const frameName = document.getElementById('addFrameName').value;
	const frameGroup = document.getElementById('addFrameGroup').value;
	backend.send('addFrame', {"ip":frameIP, "number": frameNumber, "name": frameName, "group": frameGroup});
}

function drawFrames(frames) {
	window.frames = frames;
	const _framesCont = document.getElementById('framesCont');
	_framesCont.innerHTML = '';
	for (const frameIP in frames) {
		const _frame = drawFrame(frames[frameIP]);
		_framesCont.append(_frame);
	}
}

function drawFrame(frame) {
	const _frameCont = document.createElement('section');
	_frameCont.classList.add('frameCont');
	_frameCont.setAttribute('data-ip', frame.ip);
	const _header = `<header>
		<input type="checkbox" class="form-check form-check-input collapseHeader" id="frame_${frame.ip.replaceAll('.','_')}" checked>
		<label class="frameName" for="frame_${frame.ip.replaceAll('.','_')}">${frame.ip} - ${frame.number} - ${frame.name} - ${frame.group}</label>
		<div class="form-switch"><input type="checkbox" class="form-check-input frameEnable" ${frame.enabled ? 'checked' : ''}></div>
		<div class="frameStatus ms-auto"></div>
		<button class="frameDelete btn btn-danger btn-sm ms-2">Delete</button>
	</header>`
	_frameCont.insertAdjacentHTML('beforeend', _header);
	_frameCont.insertAdjacentHTML('beforeend', `<section class="data collapseSection"></div>`);
	return _frameCont;
}

function drawSlotInfo(slotInfo) {
	if (window.pause) return
	const frameIP = slotInfo.frame.ip;
	const _framesCont = document.getElementById('framesCont');
	let _frameData = _framesCont.querySelector(`[data-ip="${frameIP}"] .data`);
	if (!_frameData) {
		_frame = drawFrame(slotInfo.frame);
		_framesCont.append(_frame);
		_frameData = _framesCont.querySelector(`[data-ip="${frameIP}"] .data`);
	}
	
	for (const slotName in slotInfo.slots) {
		let slotExists = true;
		if (!Object.prototype.hasOwnProperty.call(slotInfo.slots, slotName)) return;
		const slot = slotInfo.slots[slotName];
		
		let _slotCont = _frameData.querySelector(`[data-slot="${slotName}"]`);

		if (!_slotCont) {
			_slotCont = document.createElement('section');
			_slotCont.classList.add('groupCont');
			_slotCont.setAttribute('data-slot', slotName);
			_slotCont.classList.add('slotCont');
			_frameData.appendChild(_slotCont);
			_slotCont.insertAdjacentHTML('beforeend', `<header>
				<input type="checkbox" class="form-check form-check-input collapseHeader" id="header_${frameIP.replaceAll('.','_')}_${slotName}">
				<label class="groupName" for="header_${frameIP.replaceAll('.','_')}_${slotName}">Slot ${slotName}</label>
				<div class="form-switch"><input type="checkbox" class="form-check-input slotEnable" ${slot.enabled ? 'checked' : ''}></div>
			</header>`);
		}
		
		let _slot = _slotCont.querySelector(`.collapseSection`);
		if (!_slot) {
			slotExists = false;
			_slot = document.createElement('section');
			_slot.classList.add('collapseSection');
			_slotCont.append(_slot);
			_slot.insertAdjacentHTML('beforeend', `<h4 class="m-1">Card Settings</h2>`);
		}

		commands.card.forEach((group, index) => {
			let _groupCont = _slot.querySelector(`[data-name="${group.name}"]`);
			if (!_groupCont) {
				_groupCont = document.createElement('section');
				_groupCont.setAttribute('data-name', group.name);
				_groupCont.classList.add('groupCont');
				_groupCont.insertAdjacentHTML('beforeend', `<header>
						<input type="checkbox" class="form-check form-check-input collapseHeader" id="header_${frameIP.replaceAll('.','_')}_${slotName}_i${index}">
						<label class="groupName" for="header_${frameIP.replaceAll('.','_')}_${slotName}_i${index}">${group.name}</div>
					</header>`);
				_slot.append(_groupCont);
			}

			let _collapseSection = _groupCont.querySelector('.collapseSection');
			if (!_collapseSection) {
				_collapseSection = document.createElement('section');
				_collapseSection.classList.add('collapseSection');
				_groupCont.append(_collapseSection);
			}
			
			group.commands.forEach(command => {
				let _command = _collapseSection.querySelector(`[data-command="${command.command}"]`)
				try {					
					const prefered = slot.prefered[command.command];
					const active = slot.active[command.command];
					const group = slot.group[command.command] ? slot.group[command.command].value : null;
					if (!_command) {
						_collapseSection.append(drawCommand('frame', command, command.command, prefered, active, group));
					} else {
						updateCommand(_command, command, prefered, active, group)
					}
				} catch (error) {
					console.log(error)
				}
			})
			checkDeps(_collapseSection);
		})

		if (!slotExists) {
			_slot.insertAdjacentHTML('beforeend', `<h4 class="m-1">Spigots</h2>`);
		}
		
		for (let spigot = 0; spigot < slot.ins; spigot++) {
			let _spigotCont = _slot.querySelector(`[data-spigot="${spigot}"]`);
			if (!_spigotCont) {
				_spigotCont = document.createElement('section');
				_spigotCont.classList.add('groupCont');
				_spigotCont.setAttribute('data-spigot', spigot);
	
				_slot.appendChild(_spigotCont)
	
				_spigotCont.insertAdjacentHTML('beforeend', `<header>
					<input type="checkbox" class="form-check form-check-input collapseHeader" id="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_s${spigot}">
					<label class="groupName" for="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_s${spigot}">Spigot ${spigot+1} Settings</div>
					</header>`)
			}

			let _spigot = _spigotCont.querySelector('.collapseSection');
			if (!_spigot) {
				_spigot = document.createElement('section');
				_spigot.classList.add('collapseSection');
				_spigotCont.appendChild(_spigot)
			}

			commands.spigot.forEach((group, index) => {

				let _groupCont = _spigot.querySelector(`[data-name="${group.name}"]`)
				if (!_groupCont) {
					_groupCont = document.createElement('section');
					_groupCont.setAttribute('data-name', group.name);
					_groupCont.classList.add('groupCont');
					_groupCont.insertAdjacentHTML('beforeend', `<header>
							<input type="checkbox" class="form-check form-check-input collapseHeader" id="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_${spigot}_${index}">
							<label class="groupName" for="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_${spigot}_${index}">${group.name}</div>
						</header>`);
					_spigot.append(_groupCont);
				}
				let _collapseSection = _groupCont.querySelector('.collapseSection');
				if (!_collapseSection) {
					_collapseSection = document.createElement('section');
					_collapseSection.classList.add('collapseSection');
					_groupCont.append(_collapseSection);
				}


				group.commands.forEach(command => {
					const commandID = Number(command.command) + Number(command.increment * spigot)
					const _command = _collapseSection.querySelector(`[data-command="${commandID}"]`)
					try {						
						const prefered = slot.prefered[commandID];
						const active = slot.active[commandID];
						const group = slot.group[commandID] ? slot.group[commandID].value : null;
						if (!_command) {
							_collapseSection.append(drawCommand('frame', command, commandID, prefered, active, group));
						} else {
							updateCommand(_command, command, prefered, active, group);
						}
					} catch (error) {
						console.log(error)
					}
				})

				checkDeps(_collapseSection);
			})
		}
	}
	// _frameData.innerHTML = `<pre>${JSON.stringify(slotInfo, null, 4)}</pre>`;
}

function doFrameStatus(data) {
	try {
		const _framesCont = document.getElementById('framesCont');
		const _frame = _framesCont.querySelector(`[data-ip="${data.frameIP}"]`);
		const _status = _frame.querySelector('.frameStatus');
		_status.innerHTML = data.status;
	} catch (error) {
		console.log(error)
	}
}

/* Frame commands */

function drawCommand(prefix, command, commandID, editValue = null, readValue = null, computed = null) {
	const opts = command.options ? JSON.stringify(command.options).replace(/\"/g, "'") : '';
	const deps = command.depends ? JSON.stringify(command.depends).replace(/\"/g, "'") : '';
	const _cont = document.createElement('div');
	_cont.classList.add('commandCont');
	_cont.setAttribute('data-command', commandID);
	_cont.setAttribute('data-name', command.name);
	_cont.setAttribute('data-type', command.type);
	_cont.setAttribute('data-take', command.take);
	_cont.setAttribute('data-increment', command.increment || '');
	_cont.setAttribute('data-options', opts);
	_cont.setAttribute('data-depends', deps);
	try {
		if (editValue !== null) {
			_cont.insertAdjacentHTML('beforeend', `<div class="form-switch"><input type="checkbox" class="${prefix}_commandEnabled commandEnabled form-check-input" ${editValue.enabled ? 'checked': ''}></div>`);
		} else {
			_cont.insertAdjacentHTML('beforeend', `<div class="form-switch"><input type="checkbox" class="${prefix}_commandEnabled commandEnabled form-check-input"></div>`);
		}
		_cont.insertAdjacentHTML('beforeend', `<div class="commandName">${command.name}</div>`);
		if (readValue) {
			let val = readValue;
			let match = 'bg-success';
			switch (command.type) {
				case 'boolean':
					val = readValue == 0 ? 'False' : 'True';
					break;
				case 'select':
					val = command.options[readValue];
					break;
				default:
					break;
			}
			if (command.shuffle) val = readValue;
			if (readValue == "ERROR") {
				match = '';
				val = 'ERROR'
			} else if (editValue !== null) {
				match = readValue == editValue.value ? 'bg-success' : 'bg-danger'
			} else {
				match = readValue == command.default ? 'bg-success' : 'bg-danger'
			}
			_cont.insertAdjacentHTML('beforeend', `<div class="commandRead form-control form-control-sm w-75 ${match}" data-raw="${readValue}">${val}</div>`);
		} else {
			_cont.insertAdjacentHTML('beforeend', `<div class="commandRead"></div>`);
		}

		if (computed) {
			let val = computed;
			switch (command.type) {
				case 'boolean':
					val = readValue == 0 ? 'False' : 'True';
					break;
				case 'select':
					val = command.options[computed];
					break;
				default:
					break;
			}
			_cont.insertAdjacentHTML('beforeend', `<div class="commandComputed form-control form-control-sm w-75">${val}</div>`);
		} else {
			_cont.insertAdjacentHTML('beforeend', `<div class="commandComputed"></div>`);
		}
	
			
		let _input;
		switch (command.type) {
			case 'boolean':
				_input = document.createElement('input');
				_input.setAttribute('type', 'checkbox');
				_input.classList.add('form-check', 'form-check-input', `${prefix}_commandCheck`);
				if (command.default) _input.setAttribute('checked', 'checked');
				if (editValue !== null) {
					if (editValue.value == 0) {
						_input.removeAttribute('checked');
					} else {
						_input.setAttribute('checked', 'checked');
					}
				}
				break;
			case 'text':
				_input = document.createElement('input');
				_input.setAttribute('type', 'text');
				if (command.default) _input.setAttribute('value', command.default);
				if (editValue !== null) _input.setAttribute('value', editValue.value);
				break;
			case 'select':
				_input = document.createElement('select');
				for (const opt in command.options) {
					if (!Object.hasOwnProperty.call(command.options, opt)) return;
					let selected = '';
					if (editValue !== null) {
						if (opt == editValue.value) selected = 'selected';
					} else {
						if (opt == command.default) selected = 'selected';
					}
					_input.insertAdjacentHTML('beforeend', `<option ${selected} value="${opt}">${command.options[opt]}</option>`)
				}
				break
			case 'smartip':
				_input = document.createElement('div');
				let valArr = [];
				if (editValue !== null && editValue.value !== null) valArr = editValue.value.split('.');
				for (let index = 0; index < 4; index++) {
					let _octet = document.createElement('input');
					_octet.setAttribute('type', 'text');
					if (editValue !== null) _octet.setAttribute('value', valArr[index])
					_octet.classList.add('form-control', 'octet', `${prefix}_octet`, 'form-control-sm', `octet_${index}`);
					_input.append(_octet);
					if (index < 3) _input.insertAdjacentHTML('beforeend', '<span class="mx-2">.</span>');
				}
				_input.classList.add('d-flex');
				break;
			case 'take':
				_input = document.createElement('input');
				_input.setAttribute('type', 'checkbox');
				break;
		}
		switch (command.type) {
			case 'boolean':
				//_input.classList.add('form-control');
				break;
			case 'select':
				_input.classList.add('form-select', 'form-select-sm');
			case 'text':
			case 'take':
				_input.classList.add('form-control', 'form-control-sm');
				break;
			case 'smartip':
				break;
		}
		_input.classList.add(`${prefix}_commandInput`, 'commandInput');
		_input.addEventListener('change', () => checkDeps(_input));
		_cont.append(_input);
		return _cont;
	} catch (error) {
		console.log(error)
	}
}

function updateCommand(_command, command, prefered = null, active, computed = null) {
	try {
		// if (command.command == 4052) console.log(command, prefered, active)
		const _read = _command.querySelector('.commandRead');
		switch (command.type) {
			case 'boolean':
				_read.innerHTML = active == 0 ? 'False' : 'True';
				break;
			case 'select':
				_read.innerHTML = command.options[active];
				break;
			default:
				_read.innerHTML = active;
				break;
		}
		if (command.shuffle) _read.innerHTML = active;
		try {
			_read.classList.remove('bg-success', 'bg-danger');
		} catch (error) {
			console.log(error)
		}

		const enabled = _command.querySelector('.frame_commandEnabled').checked;

		if (active == "ERROR") {
			_read.innerHTML = "ERROR";
		} else {

			if (enabled) {
				if (prefered != null) {
					if (prefered.value == active) {
						_read.classList.add('bg-success');
					} else {
						_read.classList.add('bg-danger');
					}
				} else {
					if (command.default == active) {
						_read.classList.add('bg-success');
					} else {
						_read.classList.add('bg-danger');
					}
				}
			} else {
				if (computed != null) {
					if (computed == active) {
						_read.classList.add('bg-success');
					} else {
						_read.classList.add('bg-danger');
					}
				} else {
					if (command.default == active) {
						_read.classList.add('bg-success');
					} else {
						_read.classList.add('bg-danger');
					}
				}
			}
		}

		const _computed = _command.querySelector('.commandComputed');
		if (computed) {
			_computed.classList.add('form-control','form-control-sm','w-75');
			switch (command.type) {
				case 'boolean':
					_computed.innerHTML = computed == 0 ? 'False' : 'True';
					break;
				case 'select':
					_computed.innerHTML = command.options[computed];
					break;
				default:
					_computed.innerHTML = computed;
					break;
			}
		} else {
			_computed.innerHTML = '';
			_computed.classList.remove('form-control','form-control-sm','w-75');
		}
	} catch (error) {
		console.log(error)
	}
}

/* Groups */

function addGroup() {
	const groupName = document.getElementById('addGroup').value
	backend.send('addGroup', {"name": groupName, "enabled": true});
}

function drawGroups(groups) {
	window.groups = groups;
	const _groupsCont = document.getElementById('groupCont');
	_groupsCont.innerHTML = '';
	const _groupSelect = document.getElementById('addFrameGroup');
	let options = '<option value="" selected readonly hidden>Select Group (optional)</option>';
	for (const groupName in groups) {
		const _group = drawGroup(groups[groupName]);
		_groupsCont.append(_group);
		options += `<option value="${groupName}">${groupName}</option>`;
	}
	_groupSelect.innerHTML = options;
}

function drawGroup(group) {
	const groupIdName = group.name.replaceAll('.','_').replaceAll(' ','_')
	const _groupCont = document.createElement('section');
	_groupCont.classList.add('groupCont', 'groupCommandCont');
	_groupCont.setAttribute('data-name', group.name);
	const _header = `<header>
		<input type="checkbox" class="form-check form-check-input collapseHeader" id="group_${groupIdName}">
		<label class="groupName" for="group_${groupIdName}">${group.name}</label>
		<div class="form-switch"><input type="checkbox" class="form-check-input groupEnable" ${group.enabled ? 'checked' : ''}></div>
		<button class="groupDelete btn btn-danger btn-sm ms-auto">Delete</button>
	</header>`
	_groupCont.insertAdjacentHTML('beforeend', _header);

	const _groupCommandsSection = document.createElement('section');
	_groupCommandsSection.classList.add('data', 'collapseSection');
	_groupCont.append(_groupCommandsSection);

	_groupCommandsSection.insertAdjacentHTML('beforeend', `<h4 class="m-1">Card Settings</h4>`);
	commands.card.forEach((cmdGroup, index) => {
		const _groupCont = document.createElement('section');
		_groupCont.setAttribute('data-name', cmdGroup.name);
		_groupCont.setAttribute('data-type', 'card');
		_groupCont.classList.add('groupCont');
		_groupCont.insertAdjacentHTML('beforeend', `<header>
				<input type="checkbox" class="form-check form-check-input collapseHeader" id="group_${groupIdName}_${index}">
				<label class="groupName" for="group_${groupIdName}_${index}">${cmdGroup.name}</div>
			</header>`);
		const _groupCommands = document.createElement('section');
		_groupCommands.classList.add('collapseSection');
		cmdGroup.commands.forEach(command => {
			_groupCommands.append(drawCommand('group', command, command.command, window.groups[group.name].commands[command.command]));
		})
		_groupCont.append(_groupCommands);
		_groupCommandsSection.append(_groupCont);
		checkDeps(_groupCommands);
	})

	_groupCommandsSection.insertAdjacentHTML('beforeend', `<h4 class="m-1">Spigot Settings</h4>`);
	commands.spigot.forEach((cmdGroup, index) => {
		const _groupCont = document.createElement('section');
		_groupCont.setAttribute('data-name', cmdGroup.name);
		_groupCont.setAttribute('data-type', 'spigot');
		_groupCont.classList.add('groupCont');
		_groupCont.insertAdjacentHTML('beforeend', `<header>
				<input type="checkbox" class="form-check form-check-input collapseHeader" id="group_spig_${groupIdName}_${index}">
				<label class="groupName" for="group_spig_${groupIdName}_${index}">${cmdGroup.name}</div>
			</header>`);
		const _groupCommands = document.createElement('section');
		_groupCommands.classList.add('collapseSection');
		cmdGroup.commands.forEach(command => {
			_groupCommands.append(drawCommand('group', command, command.command, window.groups[group.name].commands[command.command]));
		})
		_groupCont.append(_groupCommands);
		_groupCommandsSection.append(_groupCont);
		checkDeps(_groupCommands);
	})

	return _groupCont;
}

/* GUI */

function showTab(tab = 'addDevices') {
	const __tabs = document.getElementsByClassName('tab');
	for (const _tab of __tabs) {
		_tab.classList.add('d-none');
	}
	document.getElementById(tab).classList.remove('d-none');
}

function doLog(log) {
	const _logs = document.getElementById('logs');
	const cols = [31,32,33,34,35,36,37];
	const specials = [1,2];
	const reset = 0;
	let currentCul = getClass(log.textColour);
	let currnetSpec = 1;
	let output = `<span class="logTimestamp">[${log.timeString}]</span><span class="logLevel ${getClass(log.levelColour)}">(${log.level})</span><span class="${getClass(log.colour)} logCatagory">${log.catagory}${log.seperator} </span>`;
	const logArr = log.message.split('\x1b[');
	logArr.forEach((element, index) => {
		const num = parseInt(element.substr(0, element.indexOf('m')));
		const text = index==0 ? element : element.substring(element.indexOf('m') + 1);
		if (cols.includes(num)) {
			currentCul = num;
		} else if (specials.includes(num)) {
			currnetSpec = num;
		} else if (num == reset) {
			currentCul = 37;
			currnetSpec = 1;
		}
		output += `<span class="${getClass(currentCul)} ${getClass(currnetSpec)}">${text}</span>`;
	})
	output += `<span class="purpleLog logLinenum"> ${log.lineNumString}</span>`;

	const _log = `<div class='log' data-level="${log.level}">${output}</div>`;
	_logs.innerHTML = _log + _logs.innerHTML;
	const maxLogs = 499;
	_logs.childElementCount
	if (_logs.childElementCount > maxLogs) {
		_logs.children[maxLogs+1].remove();
	}
}

function getClass(num) {
	let value;
	switch (num) {
	case 31:
		value = 'redLog';
		break;
	case 32:
		value = 'greenLog';
		break;
	case 33:
		value = 'yellowLog';
		break;
	case 34:
		value = 'blueLog';
		break;
	case 35:
		value = 'purpleLog';
		break;
	case 36:
		value = 'cyanLog';
		break;
	case 37:
		value = 'whiteLog';
		break;
	case 2:
		value = 'dimLog';
		break;
	case 1:
		value = 'brightLog';
		break;
	}
	return value;
}

/* Utility */

function convertBytes(raw) {
	raw = Number(raw);
	let iterations = 0;
	const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
	for (let index = 0; index < 4; index++) {
		if (raw > 1024) {
			iterations++;
			raw = raw/1024;
		}
	}
	return `${parseFloat(raw.toPrecision(3))} ${sizes[iterations]}`;
}

function on(eventNames, selectors, callback) {
	if (!Array.isArray(selectors)) selectors = [selectors];
	if (!Array.isArray(eventNames)) eventNames = [eventNames];
	selectors.forEach(selector => {
		eventNames.forEach(eventName => {
			if (selector.nodeType) {
				selector.addEventListener(eventName, event => {callback(event.target)});
			} else {
				document.addEventListener(eventName, event => {
					if (event.target.matches(selector)) callback(event.target);
				});
			};
		});
	});
};


/* Handle frame inputs */


function doInput(_element) {
	if (_element.classList.contains('commandCheck')) return
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const take = _element.closest('.commandCont').getAttribute('data-take');
	const slot = _element.closest('.slotCont').getAttribute('data-slot');
	const frame = _element.closest('.frameCont').getAttribute('data-ip');
	const enabled = _element.closest('.commandCont').querySelector('.commandEnabled').checked;

	backend.send('setCommand', {
		"ip":frame,
		"slot": slot,
		"command": command,
		"value": _element.value,
		"enabled": enabled,
		"take": take
	});
}

function doCheck(_element) {
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const take = _element.closest('.commandCont').getAttribute('data-take');
	const slot = _element.closest('.slotCont').getAttribute('data-slot');
	const frame = _element.closest('.frameCont').getAttribute('data-ip');
	const enabled = _element.closest('.commandCont').querySelector('.commandEnabled').checked;

	backend.send('setCommand', {
		"ip":frame,
		"slot": slot,
		"command": command,
		"value": _element.checked ? '1' : '0',
		"enabled": enabled,
		"take": take
	});
}

function doOctet(_element) {
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const take = _element.closest('.commandCont').getAttribute('data-take');
	const slot = _element.closest('.slotCont').getAttribute('data-slot');
	const frame = _element.closest('.frameCont').getAttribute('data-ip');
	const __octets = _element.parentElement.querySelectorAll('.octet');
	const enabled = _element.closest('.commandCont').querySelector('.commandEnabled').checked;
	const ip = [];
	for (const _octet of __octets) {
		ip.push(_octet.value)
	}

	backend.send('setCommand', {
		"ip":frame,
		"slot": slot,
		"command": command,
		"value": ip.join('.'),
		"enabled": enabled,
		"take": take
	});
}

function doEnable(_element) {
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const slot = _element.closest('.slotCont').getAttribute('data-slot');
	const frame = _element.closest('.frameCont').getAttribute('data-ip');

	backend.send('setEnable', {
		"ip":frame,
		"slot": slot,
		"command": command,
		"enabled": _element.checked
	});
}

function doSlotEnable(_element) {
	const slot = _element.closest('.slotCont').getAttribute('data-slot');
	const frame = _element.closest('.frameCont').getAttribute('data-ip');

	backend.send('enableSlot', {
		"ip":frame,
		"slot": slot,
		"enabled": _element.checked
	});
}

function doFrameEnable(_element) {
	const frame = _element.closest('.frameCont').getAttribute('data-ip');

	backend.send('enableFrame', {
		"ip":frame,
		"enabled": _element.checked
	});
}

function doFrameDelete(_element) {
	const _frame = _element.closest('.frameCont');
	const frame = _frame.getAttribute('data-ip');
	_frame.remove();
	backend.send('deleteFrame', {
		"ip":frame
	});
}

/* Handle group inputs */

function doGroupInput(_element) {
	if (_element.classList.contains('commandCheck')) return
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const group = _element.closest('.groupCommandCont').getAttribute('data-name');
	const type = _element.closest('.groupCont').getAttribute('data-type');
	const take = _element.closest('.commandCont').getAttribute('data-take');
	const dataType = _element.closest('.commandCont').getAttribute('data-type');
	const increment = _element.closest('.commandCont').getAttribute('data-increment');
	const enabled = _element.closest('.commandCont').querySelector('.commandEnabled').checked;

	backend.send('setGroupCommand', {
		"group":group,
		"type": type,
		"dataType": dataType,
		"increment": increment,
		"command": command,
		"value": _element.value,
		"enabled": enabled,
		"take": take
	});
}

function doGroupCheck(_element) {
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const group = _element.closest('.groupCommandCont').getAttribute('data-name');
	const type = _element.closest('.groupCont').getAttribute('data-type');
	const take = _element.closest('.commandCont').getAttribute('data-take');
	const dataType = _element.closest('.commandCont').getAttribute('data-type');
	const increment = _element.closest('.commandCont').getAttribute('data-increment');
	const enabled = _element.closest('.commandCont').querySelector('.commandEnabled').checked;

	backend.send('setGroupCommand', {
		"group":group,
		"type": type,
		"dataType": dataType,
		"increment": increment,
		"command": command,
		"value": _element.checked ? '1' : '0',
		"enabled": enabled,
		"take": take
	});
}

function doGroupOctet(_element) {
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const group = _element.closest('.groupCommandCont').getAttribute('data-name');
	const type = _element.closest('.groupCont').getAttribute('data-type');
	const take = _element.closest('.commandCont').getAttribute('data-take');
	const dataType = _element.closest('.commandCont').getAttribute('data-type');
	const increment = _element.closest('.commandCont').getAttribute('data-increment');
	const __octets = _element.parentElement.querySelectorAll('.octet');
	const enabled = _element.closest('.commandCont').querySelector('.commandEnabled').checked;
	const ip = [];
	for (const _octet of __octets) {
		ip.push(_octet.value)
	}

	backend.send('setGroupCommand', {
		"group":group,
		"type": type,
		"dataType": dataType,
		"increment": increment,
		"command": command,
		"value": ip.join('.'),
		"enabled": enabled,
		"take": take
	});
}

function doGroupCommandEnable(_element) {
	const _cont = _element.closest('.commandCont');
	const type = _cont.getAttribute('data-type');

	switch (type) {
		case 'text':
		case 'select':
			doGroupInput(_cont.querySelector('.commandInput'));;
			break;
		case 'smartip':
			doGroupOctet(_cont.querySelector('.commandInput'));
			break;
		case 'boolean':
			doGroupCheck(_cont.querySelector('.commandInput'));
			break;
		default:
			break;
	}
}

function doGroupEnable(_element) {
	const group = _element.closest('.groupCommandCont').getAttribute('data-name');

	backend.send('enableGroup', {
		"name":group,
		"enabled": _element.checked
	});
}

function doGroupDelete(_element) {
	const _group = _element.closest('.groupCommandCont');
	const group = _group.getAttribute('data-name');
	_group.remove();
	backend.send('deleteGroup', {
		"name":group
	});
}



function download(filename, text) {
	var element = document.createElement('a');
	element.setAttribute('href', 'data:text/plain;charset=utf-8,' + encodeURIComponent(text));
	element.setAttribute('download', filename);
	element.style.display = 'none';
	document.body.appendChild(element);
	element.click();
	document.body.removeChild(element);
}