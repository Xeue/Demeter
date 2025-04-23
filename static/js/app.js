window.pause = false;

document.addEventListener('DOMContentLoaded', () => {
	backend.on('frames', drawFrames);
	backend.on('log', doLog);
	backend.on('slotInfo', drawSlotInfo);
	backend.on('frameStatus', doFrameStatus);

	on('click', '#showLogs', ()=>showTab('logs'));
	on('click', '#showSelect', ()=>showTab('addDevices'));
	on('click', '#showCommands', ()=>showTab('cardCommands'));
	on('click', '#addFrameIPBtn', addFrame);

	const _cardCommands = document.getElementById('cardCommands');
	_cardCommands.insertAdjacentHTML('beforeend', `<h2 class="m-3">Card Level Commands</h2>`);
	commands.card.forEach((group, index) => {
		const _groupCont = document.createElement('section');
		_groupCont.setAttribute('data-name', group.name);
		_groupCont.classList.add('groupCont');
		_groupCont.insertAdjacentHTML('beforeend', `<header>
				<input type="checkbox" class="form-check form-check-input groupEnable" id="header_${index}">
				<label class="groupName" for="header_${index}">${group.name}</div>
			</header>`);
		const _groupCommands = document.createElement('section');
		_groupCommands.classList.add('groupCommands');
		group.commands.forEach(command => {
			_groupCommands.append(drawCommand(command));
		})
		_groupCont.append(_groupCommands);
		_cardCommands.append(_groupCont);
		checkDeps(_groupCommands);
	})

	_cardCommands.insertAdjacentHTML('beforeend', `<h2 class="m-3">Spigot Level Commands</h2>`);
	commands.spigot.forEach((group, index) => {
		const _groupCont = document.createElement('section');
		_groupCont.setAttribute('data-name', group.name);
		_groupCont.classList.add('groupCont');
		_groupCont.insertAdjacentHTML('beforeend', `<header>
				<input type="checkbox" class="form-check form-check-input groupEnable" id="header_spig_${index}">
				<label class="groupName" for="header_spig_${index}">${group.name}</div>
			</header>`);
		const _groupCommands = document.createElement('section');
		_groupCommands.classList.add('groupCommands');
		group.commands.forEach(command => {
			_groupCommands.append(drawCommand(command));
		})
		_groupCont.append(_groupCommands);
		_cardCommands.append(_groupCont);
		checkDeps(_groupCommands);
	})
});

function checkDeps(_cont) {
	if (!_cont.classList.contains('groupCommands')) _cont = _cont.closest('.groupCommands');
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

	
}

function addFrame() {
	const frameIP = document.getElementById('addFrameIP').value;
	const frameNumber = document.getElementById('addFrameNumber').value;
	const frameName = document.getElementById('addFrameName').value;
	backend.send('addFrame', {"ip":frameIP, "number": frameNumber, "name": frameName});
}

function drawFrames(frames) {
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
		<input type="checkbox" class="form-check form-check-input frameEnable" id="frame_${frame.ip.replaceAll('.','_')}" checked>
		<label class="frameName" for="frame_${frame.ip.replaceAll('.','_')}">${frame.ip} - ${frame.name} - ${frame.number}</label>
		<div class="frameStatus ms-auto"></div>
	</header>`
	_frameCont.insertAdjacentHTML('beforeend', _header);
	_frameCont.insertAdjacentHTML('beforeend', `<div class="data"></div>`);
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
			_frameData.appendChild(_slotCont);
			_slotCont.insertAdjacentHTML('beforeend', `<header>
				<input type="checkbox" class="form-check form-check-input groupEnable" id="header_${frameIP.replaceAll('.','_')}_${slotName}">
				<label class="groupName" for="header_${frameIP.replaceAll('.','_')}_${slotName}">Slot ${slotName}</div>
			</header>`);
		}
		
		let _slot = _slotCont.querySelector(`.groupCommands`);
		if (!_slot) {
			slotExists = false;
			_slot = document.createElement('section');
			_slot.classList.add('groupCommands');
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
						<input type="checkbox" class="form-check form-check-input groupEnable" id="header_${frameIP.replaceAll('.','_')}_${slotName}_i${index}">
						<label class="groupName" for="header_${frameIP.replaceAll('.','_')}_${slotName}_i${index}">${group.name}</div>
					</header>`);
				_slot.append(_groupCont);
			}

			let _groupCommands = _groupCont.querySelector('.groupCommands');
			if (!_groupCommands) {
				_groupCommands = document.createElement('section');
				_groupCommands.classList.add('groupCommands');
				_groupCont.append(_groupCommands);
			}
			
			group.commands.forEach(command => {
				let _command = _groupCommands.querySelector(`[data-command="${command.command}"]`)
				if (!_command) {
					_groupCommands.append(drawCommand(command, slot.commands[command.command]));
				} else {
					updateCommand(_command)
				}
			})
			checkDeps(_groupCommands);
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
					<input type="checkbox" class="form-check form-check-input groupEnable" id="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_s${spigot}">
					<label class="groupName" for="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_s${spigot}">Spigot ${spigot+1} Settings</div>
					</header>`)
			}

			let _spigot = _spigotCont.querySelector('.groupCommands');
			if (!_spigot) {
				_spigot = document.createElement('section');
				_spigot.classList.add('groupCommands');
				_spigotCont.appendChild(_spigot)
			}

			commands.spigot.forEach((group, index) => {

				let _groupCont = _spigot.querySelector(`[data-name="${group.name}"]`)
				if (!_groupCont) {
					_groupCont = document.createElement('section');
					_groupCont.setAttribute('data-name', group.name);
					_groupCont.classList.add('groupCont');
					_groupCont.insertAdjacentHTML('beforeend', `<header>
							<input type="checkbox" class="form-check form-check-input groupEnable" id="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_${spigot}_${index}">
							<label class="groupName" for="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_${spigot}_${index}">${group.name}</div>
						</header>`);
					_spigot.append(_groupCont);
				}
				let _groupCommands = _groupCont.querySelector('.groupCommands');
				if (!_groupCommands) {
					_groupCommands = document.createElement('section');
					_groupCommands.classList.add('groupCommands');
					_groupCont.append(_groupCommands);
				}


				group.commands.forEach(command => {
					let _command = _groupCommands.querySelector(`[data-command="${command.command}"]`)
					if (!_command) {
						_groupCommands.append(drawCommand(command, slot.commands[command.command]));
					} else {
						updateCommand(_command)
					}
				})

				checkDeps(_groupCommands);
			})
		}
	}
	// _frameData.innerHTML = `<pre>${JSON.stringify(slotInfo, null, 4)}</pre>`;
}

function updateCommand() {
	console.log('already exists!')
}

function doFrameStatus(data) {
	const _framesCont = document.getElementById('framesCont');
	const _frame = _framesCont.querySelector(`[data-ip="${data.frameIP}"]`);
	const _status = _frame.querySelector('.frameStatus');
	_status.innerHTML = data.status;
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



















function drawCommand(command, value = null) {
	const opts = command.options ? JSON.stringify(command.options).replace(/\"/g, "'") : '';
	const deps = command.depends ? JSON.stringify(command.depends).replace(/\"/g, "'") : '';
	const _cont = document.createElement('div');
	_cont.classList.add('commandCont');
	_cont.setAttribute('data-command', command.command);
	_cont.setAttribute('data-name', command.name);
	_cont.setAttribute('data-type', command.type);
	_cont.setAttribute('data-increment', command.increment || '');
	_cont.setAttribute('data-options', opts);
	_cont.setAttribute('data-depends', deps);
	_cont.insertAdjacentHTML('beforeend', `<input type="checkbox" class="commandEnabled form-check form-check-input" checked>`);
	_cont.insertAdjacentHTML('beforeend', `<div class="commandName">${command.name}</div>`);
		
	let _input;
	switch (command.type) {
		case 'boolean':
			_input = document.createElement('input');
			_input.setAttribute('type', 'checkbox');
			_input.classList.add('form-check', 'form-check-input');
			if (command.default) _input.setAttribute('checked', 'checked');
			if (value) {
				if (value == 0) {
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
			if (value) _input.setAttribute('value', value);
			break;
		case 'select':
			_input = document.createElement('select');
			for (const text in command.options) {
				if (!Object.hasOwnProperty.call(command.options, text)) return;
				if (command.options[text] == value) {
					_input.insertAdjacentHTML('beforeend', `<option selected value="${command.options[text]}">${text}</option>`)
				} else {
					_input.insertAdjacentHTML('beforeend', `<option value="${command.options[text]}">${text}</option>`)
				}
			}
			break
		case 'smartip':
			_input = document.createElement('div');
			let valArr = [];
			if (value) valArr = value.split('.');
			for (let index = 0; index < 4; index++) {
				let _octet = document.createElement('input');
				_octet.setAttribute('type', 'text');
				if (value) _octet.setAttribute('value', valArr[index])
				_octet.classList.add('form-control', 'octet', 'form-control-sm');
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
			_input.classList.add('form-select');
		case 'text':
		case 'take':
			_input.classList.add('form-control');
			break;
		case 'smartip':
			break;
	}
	_input.classList.add('commandInput');
	_input.addEventListener('change', () => checkDeps(_input));
	_cont.append(_input);
	return _cont;
}