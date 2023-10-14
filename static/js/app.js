document.addEventListener('DOMContentLoaded', () => {
	window.electronAPI.log((event, log) => {
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
	});

	window.electronAPI.disks((event, disks) => {
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
			if (diskLetter == 'C') {
				type = 'LocalFixedDiskC'
				disabled = 'disabled';
			}
			if (_diskCont) {
				_diskCont.classList.add('activeDisk');
				_diskCont.classList.remove('inactiveDisk');
				document.querySelector(`#disk-${diskLetter} .diskName`).innerHTML = `${disk.name} - ${disk.filesystem}`;
				document.querySelector(`#disk-${diskLetter} .diskLetter`).innerHTML = `(${diskLetter})`;
				document.querySelector(`#disk-${diskLetter} .diskCapacity`).setAttribute('style', `--capacity: ${disk.capacity};`);
				document.querySelector(`#disk-${diskLetter} .diskSpace`).innerHTML = `${convertBytes(disk.available)} free of ${convertBytes(disk.blocks)}`;
			} else {
				_disks.innerHTML += `<section id="disk-${diskLetter}" class="diskCard text-light activeDisk d-flex align-items-center">
					<div class="diskIcon ${type}"></div>
					<div class="d-flex flex-column flex-fill me-4">
						<div>
							<span class="diskName">${disk.name} - ${disk.filesystem}</span>
							<span class="diskLetter">(${diskLetter})</span>
						</div>
						<div class="diskCapacity" style="--capacity: ${disk.capacity};"></div>
						<div class="diskSpace">${convertBytes(disk.available)} free of ${convertBytes(disk.blocks)}</div>
					</div>
					<input type="checkbox" ${disabled} class="form-check-input me-4 mt-0">
				</section>`;
			}
		});
		const _inactiveDisks = document.getElementsByClassName('inactiveDisk');
		for (let _disk of _inactiveDisks) {
			_disk.remove();
		}
	})
});

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