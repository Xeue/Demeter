document.addEventListener('DOMContentLoaded', () => {
	window.electronAPI.log((event, log) => doLog(log));

	window.electronAPI.disks((event, disks) => doDisks(disks));

	window.electronAPI.progress((event, message) => {
		const _progress = document.getElementById('diskProgress');
		if (message.letter == "GENERAL") {
			if (message.status == "started") {
				showTab('diskProgress');
			}
		} else {
			_progress.innerHTML += `<div>${JSON.stringify(message)}</div>`;
		}
	})

	window.electronAPI.getFirmware();
	window.electronAPI.gotFirmware((event, fw) => firmware = fw);
	
	addClick('chooseGateway', gatewayConfig);
	addClick('makeGateway', gatewayStart);
	addClick('showSelect', showTab, 'diskSelect');
	addClick('showLogs', showTab, 'logs');
	addClick('showFiles', showTab, 'files');

	addClick('configCancel', showTab, 'diskSelect');
});

function doDisks(disks) {
	const _disks = document.getElementById('disks');
	const _allDisks = document.getElementsByClassName('activeDisk');
	for (let _disk of _allDisks) {
		_disk.classList.remove('activeDisk')
		_disk.classList.add('inactiveDisk')
	}
	disks.forEach(disk => {
		const diskLetter = String(disk.mounted).replace(':','');
		const _diskCont = document.getElementById(`disk-${diskLetter}`);
		let type = String(disk.filesystem).replace(/ /g,'');
		let disabled = '';
		let checked = '';
		switch (diskLetter) {
			case 'C':
				type = 'LocalFixedDiskC'
				disabled = 'disabled';
				break;
			case 'D':
				//checked = 'checked'
				break;
		}
		if (_diskCont) {
			_diskCont.classList.add('activeDisk');
			_diskCont.classList.remove('inactiveDisk');
			document.querySelector(`#disk-${diskLetter} .diskName`).innerHTML = `${disk.name} - ${disk.filesystem}`;
			document.querySelector(`#disk-${diskLetter} .diskLetter`).innerHTML = `(${diskLetter})`;
			document.querySelector(`#disk-${diskLetter} .diskCapacity`).setAttribute('style', `--capacity: ${disk.capacity};`);
			document.querySelector(`#disk-${diskLetter} .diskSpace`).innerHTML = `${convertBytes(disk.available)} free of ${convertBytes(disk.blocks)}`;
		} else {
			_disks.innerHTML += `<section
			id="disk-${diskLetter}"
			class="diskCard text-light activeDisk d-flex align-items-center"
				data-letter="${diskLetter}">
				<div class="diskIcon ${type}"></div>
				<div class="d-flex flex-column flex-fill me-4">
					<div>
						<span class="diskName">${disk.name} - ${disk.filesystem}</span>
						<span class="diskLetter">(${diskLetter})</span>
					</div>
					<div class="diskCapacity" style="--capacity: ${disk.capacity};"></div>
					<div class="diskSpace">${convertBytes(disk.available)} free of ${convertBytes(disk.blocks)}</div>
				</div>
				<input type="checkbox" ${disabled} ${checked} class="form-check-input me-4 mt-0">
			</section>`;
		}
	});
	const __inactiveDisks = document.getElementsByClassName('inactiveDisk');
	for (let _disk of __inactiveDisks) {
		_disk.remove();
	}
}

function gatewayConfig() {
	showTab('diskConfig');
	const __disks = document.querySelectorAll('.diskCard:has(input:checked)');
	const _cont = document.getElementById('diskConfigList');
	_cont.innerHTML = "";
	let options = '<option selected hidden disabled value="none">Select</option>';

	firmware.gateway.forEach(file => {
		options += `<option value="${file}">${file}</option>`;
	});

	for (let _disk of __disks) {
		_cont.innerHTML +=  `<section class="configItem card" data-letter="${_disk.dataset.letter}">
			<div class="card-header">Disk: ${_disk.dataset.letter}</div>
			<div class="card-body">
				<div class="d-flex">
					<input type="checkbox" class="configCopy" id="config-disk-${_disk.dataset.letter}">
					<label for="config-disk-${_disk.dataset.letter}">Attempt to restore existing IPs and config</label>
				</div>
				<div class="d-flex">
					<input id="config-diskFW-${_disk.dataset.letter}" type="checkbox" class="configFirmware">
					<label for="config-diskFW-${_disk.dataset.letter}">Load firmware</label>
					<select id="config-selectFW-${_disk.dataset.letter}" class="configFirmwareSelect form-select form-select-sm w-auto">${options}</select>
				</div>
			</div>
		</section>`;
	}
}

function gatewayStart() {
	const __disks = document.getElementsByClassName('configItem');
	const disks = [];
	for (let _disk of __disks) {
		const letter = _disk.dataset.letter
		const restoreConfig = document.getElementById('config-disk-'+letter).checked;
		const firmware = document.getElementById('config-diskFW-'+letter).checked;
		const firmwareVersion = firmware ? document.getElementById('config-selectFW-'+letter).value : null;
		disks.push({
			'letter': letter,
			'config': restoreConfig,
			'firmware': firmware,
			'firmwareVersion': firmwareVersion
		});
	}
	window.electronAPI.gatewayTx(disks);
}








/* GUI */

function showTab(tab = 'diskSelect') {
	const __tabs = document.getElementsByClassName('tab');
	for (const _tab of __tabs) {
		_tab.classList.add('d-none');
	}
	document.getElementById(tab).classList.remove('d-none');
}

function doLog(log) {
	const Logs = document.getElementById('logs');

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
	Logs.innerHTML += $log;
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

function addClick(id, callback, arguments) {
	document.getElementById(id).addEventListener('click', event => {
		if (Array.isArray(arguments)) {
			args = [...arguments, event];
		} else if (typeof arguments == 'string' || typeof arguments == 'number') {
			args = [arguments, event];
		} else {
			args = [event];
		}
		callback.apply(null, args);
	});
}

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