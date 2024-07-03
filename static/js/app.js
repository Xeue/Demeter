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

function drawCommand(command) {
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
			break;
		case 'text':
			_input = document.createElement('input');
			_input.setAttribute('type', 'text');
			if (command.default) _input.setAttribute('value', command.default);
			break;
		case 'select':
			_input = document.createElement('select');
			for (const text in command.options) {
				if (!Object.hasOwnProperty.call(command.options, text)) return;
				_input.insertAdjacentHTML('beforeend', `<option value="${command.options[text]}">${text}</option>`)
			}
			break
		case 'smartip':
			_input = document.createElement('div');
			for (let index = 0; index < 4; index++) {
				let _octet = document.createElement('input');
				_octet.setAttribute('type', 'text');
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
	let index = 0;
	for (const frameIP in frames) {
		index++;
		const frame = frames[frameIP];
		const _frameCont = document.createElement('section');
		_frameCont.classList.add('frameCont');
		_frameCont.setAttribute('data-ip', frameIP);
		const _header = `<header>
			<input type="checkbox" class="form-check form-check-input frameEnable" id="frame_${index}" checked>
			<label class="frameName" for="frame_${index}">${frameIP} - ${frame.name} - ${frame.number}</label>
			<div class="frameStatus ms-auto"></div>
		</header>`
		_frameCont.insertAdjacentHTML('beforeend', _header);
		_framesCont.append(_frameCont);
	}
}

function drawSlotInfo(slotInfo) {
	const frameIP = slotInfo.frameIP;
	const _framesCont = document.getElementById('framesCont');
	const _frame = _framesCont.querySelector(`[data-ip="${frameIP}"]`);
	_frame.insertAdjacentHTML('beforeend', `<pre>${JSON.stringify(slotInfo, null, 4)}</pre>`);
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
	let currentCul = 37;
	let currnetSpec = 1;

	let logArr = log.split('[');

	let output = '';

	for (let index = 0; index < logArr.length; index++) {
		const element = logArr[index];
		const num = parseInt(element.substr(0, element.indexOf('m')));
		const text = element.substring(element.indexOf('m') + 1);

		if (cols.includes(num)) {
			currentCul = num;
		} else if (specials.includes(num)) {
			currnetSpec = num;
		} else if (num == reset) {
			currentCul = 37;
			currnetSpec = 1;
		}

		const colour = getClass(currentCul);
		const special = getClass(currnetSpec);
		output += `<span class="${colour} ${special}">${text}</span>`;
	}

	const $log = `<div class='log'>${output}</div>`;
	_logs.innerHTML += $log;
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
