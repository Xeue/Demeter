document.addEventListener('DOMContentLoaded', () => {
	window.electronAPI.log((event, log) => doLog(log));
	window.electronAPI.request('frames');
	window.electronAPI.receive((event, data) => receiveData(data));

	addClick('chooseMV', ()=>{
		window.electronAPI.doRollTrak();
	});
	addClick('showSelect', showTab, 'diskSelect');
	addClick('showLogs', showTab, 'logs');
	addClick('showFiles', showTab, 'files');

	addClick('configCancel', showTab, 'diskSelect');
});




function receiveData(data) {

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