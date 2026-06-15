window.pause = false;

let frames;
let groups;

document.addEventListener('DOMContentLoaded', () => {
	backend.on('frames', drawFrames);
	backend.on('log', doLog);
	backend.on('slotInfo', drawSlotInfo);
	backend.on('frameStatus', doFrameStatus);
	backend.on('groups', drawGroups);
	backend.on('frameError', doFrameError);
	backend.on('users', drawUsers);
	backend.on('credentials', showCredentials);

	on('click', '#addUserBtn', addUser);
	on('change', '.userRole', _el => {
		const u = _el.closest('[data-user]').getAttribute('data-user');
		backend.send('setUserRole', { username: u, role: _el.value });
	});
	on('click', '.userReset', _el => {
		const row = _el.closest('[data-user]');
		const pass = row.querySelector('.userPass').value;
		if (pass) backend.send('resetPassword', { username: row.getAttribute('data-user'), password: pass });
	});
	on('click', '.userDelete', _el => {
		backend.send('deleteUser', { username: _el.closest('[data-user]').getAttribute('data-user') });
	});
	if (typeof isAdmin !== 'undefined' && isAdmin) backend.send('getUsers');

	on('click', '.tabSelect', showTab)
	on('click', '#addFrameIPBtn', addFrame);
	on('click', '#addGroupBtn', addGroup);

	// Granular, unified import/export.
	on('click', '#importExport', () => backend.send('getExport')); // refresh export tree from authoritative state
	backend.on('exportData', data => {
		window._exportData = data || { frames: {}, groups: {} };
		buildIOTree(document.getElementById('exportTree'), window._exportData, true);
	});
	on('click', '.ioTab', _el => switchIOTab(_el.getAttribute('data-iotab')));
	on('click', '.ioSelectAll', () => setIOTreeChecked(true));
	on('click', '.ioSelectNone', () => setIOTreeChecked(false));
	on('change', '.ioGroupChk', _el => cascadeGroup(_el));
	on('change', '.ioFrameChk', _el => cascadeFrame(_el));
	on('click', '#exportSelected', doExportSelected);
	on('click', '#importSelected', doImportSelected);
	const _impFile = document.getElementById('importFile');
	if (_impFile) _impFile.addEventListener('change', onImportFile);
	on('click', '.cardReboot', _element => {
		const slot = _element.closest('.slotCont').getAttribute('data-slot')
		const frameIP = _element.closest('.frameCont').getAttribute('data-ip')
		backend.send('cardReboot', {"frameIP":frameIP, "slot": slot});
	})
	on('click', '.addCardBtn', _element => {
		const _frame = _element.closest('.frameCont');
		const slot = _frame.querySelector('.addCardSlot').value;
		stageCard(_frame.getAttribute('data-ip'), slot);
	})
	on('click', '.cardRemove', _element => {
		const slot = _element.closest('.slotCont').getAttribute('data-slot');
		const frameIP = _element.closest('.frameCont').getAttribute('data-ip');
		removeCard(frameIP, slot);
	})
	on('click', '.cardRetry', _element => {
		const frameIP = _element.closest('.frameCont').getAttribute('data-ip');
		backend.send('pollNow', { ip: frameIP });
	})
	on('click', '#showOffline', _element => {
		const _body = document.getElementById('body');
		if (_element.checked) {
			_body.classList.add('showOffline');
		} else {
			_body.classList.remove('showOffline');
		}
	})
	on('click', '.collapseFrames', () => {
		const __checks = document.querySelectorAll('.frameCollapse')
		for (const _check of __checks) {
			_check.checked = false;
		}
	})
	on('click', '.collapseCards', _element => {
		const _frameCont = _element.closest('.frameCont');
		const __checks = _frameCont.querySelectorAll('.cardCollapse')
		for (const _check of __checks) {
			_check.checked = false;
		}
	})
	on('click', '.collapseSettings', _element => {
		const _groupCont = _element.closest('.groupCont');
		const __checks = _groupCont.querySelectorAll('.groupCollapse')
		for (const _check of __checks) {
			_check.checked = false;
		}
	})

	/* Frame controls */

	on('change', '.frame_octet', (_element)=>doOctet(_element));
	on('change', '.frame_commandInput', (_element)=>doInput(_element));
	on('click', '.frame_commandCheck', (_element)=>doCheck(_element));
	on('click', '.frame_commandEnabled', (_element)=>doEnable(_element));
	on('click', '.slotEnable', (_element)=>doSlotEnable(_element));
	on('click', '.frameEnable', (_element)=>doFrameEnable(_element));
	on('click', '.frameScan', (_element)=>doFrameScan(_element));
	on('click', '.frameDelete', (_element) => doFrameDelete(_element));
	on('click', '.groupDelete', (_element) => doGroupDelete(_element));
	on('click', '.frameScanMode', (_element) => doFrameMode(_element));
	on('change', '.frameAutoReboot', _el => {
		const frameIP = _el.closest('.frameCont').getAttribute('data-ip');
		backend.send('setAutoReboot', { frameIP: frameIP, mode: _el.value });
	});
	const _globalAR = document.getElementById('globalAutoReboot');
	if (_globalAR) _globalAR.addEventListener('change', () => {
		backend.send('setGlobalAutoReboot', { enabled: _globalAR.checked });
	});
	const _scanInterval = document.getElementById('scanInterval');
	if (_scanInterval) _scanInterval.addEventListener('change', () => {
		let s = parseInt(_scanInterval.value, 10);
		if (isNaN(s)) return;
		s = Math.min(3600, Math.max(1, s));
		_scanInterval.value = s; // reflect clamping
		backend.send('setScanInterval', { seconds: s });
	});

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

// mediaText renders a card interface line, tolerating absent values (e.g. a
// staged card that has no IPs yet) instead of printing "undefined".
function mediaText(n, ip, up, sfp) {
	return `Media ${n}: ${ip == null || ip === '' ? '—' : ip} - ${up || '—'}/${sfp || '—'}`;
}

// stageCard pre-configures a slot before its card is online. The server persists
// it; we also render it optimistically so the override editor appears at once.
function stageCard(frameIP, slot) {
	backend.send('stageCard', { ip: frameIP, slot: slot });
	const frame = window.frames && window.frames[frameIP];
	if (!frame) return;
	frame.slots = frame.slots || {};
	if (!frame.slots[slot]) {
		frame.slots[slot] = { enabled: true, staged: true, offline: true, prefered: {}, active: {}, group: {}, ins: 0, outs: 0, ipa: null, ipb: null, ipaup: '', ipbup: '', sfp1: '', sfp2: '' };
	} else {
		frame.slots[slot].staged = true;
	}
	delete _slotRenderCache[frameIP + '|' + slot];
	applySlotInfo({ frame: frame, slotName: slot, slot: frame.slots[slot] });
}

// removeCard unstages an expected card (only offered for staged slots).
function removeCard(frameIP, slot) {
	backend.send('removeCard', { ip: frameIP, slot: slot });
	const frame = window.frames && window.frames[frameIP];
	if (frame && frame.slots) delete frame.slots[slot];
	delete _slotRenderCache[frameIP + '|' + slot];
	const _slotCont = document.querySelector(`#framesCont [data-ip="${frameIP}"] [data-slot="${slot}"]`);
	if (_slotCont) _slotCont.remove();
}

function drawFrames(frames) {
	window.frames = frames;
	// Any frame the backend now reports is live again (e.g. it was re-added), so stop suppressing it
	if (window.deletedFrames) {
		for (const frameIP in frames) window.deletedFrames.delete(frameIP);
	}
	const _framesCont = document.getElementById('framesCont');
	_framesCont.innerHTML = '';
	// The slot DOM was just wiped, so the per-slot render cache is stale.
	for (const k in _slotRenderCache) delete _slotRenderCache[k];
	for (const frameIP in frames) {
		const _frame = drawFrame(frames[frameIP]);
		_framesCont.append(_frame);
		// Render known/staged slots from frame data so pre-configured (and
		// last-known) cards show without waiting for the next live scan.
		const _slots = frames[frameIP].slots || {};
		for (const slotName in _slots) {
			applySlotInfo({ frame: frames[frameIP], slotName: slotName, slot: _slots[slotName] });
		}
	}
}

function drawFrame(frame) {
	const _frameCont = document.createElement('section');
	_frameCont.classList.add('frameCont');
	_frameCont.style.order = frame.number;
	if (frame.offline) _frameCont.classList.add('offline')
	_frameCont.setAttribute('data-ip', frame.ip);
	let options = ""
	if (frame.enabled) {
		options = `<option value="ignore">Ignore</option>
			<option value="scan">Scan Only</option>
			<option value="blast" selected>Scan & Blast</option>`
	} else if (frame.scan) {
		options = `<option value="ignore">Ignore</option>
			<option value="scan" selected>Scan Only</option>
			<option value="blast">Scan & Blast</option>`
	} else {
		options = `<option value="ignore" selected>Ignore</option>
			<option value="scan">Scan Only</option>
			<option value="blast">Scan & Blast</option>`
	}
	const _header = `<header>
		<button class="btn btn-secondary btn-sm me-2 collapseCards">Collapse All</button>
		<input type="checkbox" class="form-check form-check-input collapseHeader frameCollapse" id="frame_${frame.ip.replaceAll('.','_')}">
		<label class="frameName" for="frame_${frame.ip.replaceAll('.','_')}">F${frame.number}-${frame.name} - ${frame.ip}<span class="labelGroup">${frame.group}</span></label>
		<div class="form-switch"><input type="checkbox" class="form-check-input frameEnable d-none" ${frame.enabled ? 'checked' : ''}></div>
		<div class="d-none">Scan</div>
		<div class="form-switch"><input type="checkbox" class="form-check-input frameScan d-none" ${frame.scan ? 'checked' : ''}></div>
		<div class="frameError ms-auto"></div>
		<div class="frameStatus ms-auto"></div>
		<select class="frameScanMode form-select form-select-sm w-auto ms-1">
			${options}
		</select>
		<select class="frameAutoReboot form-select form-select-sm w-auto ms-1" title="Auto-reboot a card after an IP/mode change (needs a reboot to apply)">
			<option value="" ${!frame.autoReboot ? 'selected' : ''}>Auto-reboot: default</option>
			<option value="on" ${frame.autoReboot === 'on' ? 'selected' : ''}>Auto-reboot: on</option>
			<option value="off" ${frame.autoReboot === 'off' ? 'selected' : ''}>Auto-reboot: off</option>
		</select>
		<select class="addCardSlot form-select form-select-sm w-auto ms-1" title="Pre-configure a card before it is online">
			${Array.from({length:20},(_,i)=>{const s=String(i+1).padStart(2,'0');return `<option value="${s}">Slot ${s}</option>`}).join('')}
		</select>
		<button class="addCardBtn btn btn-outline-primary btn-sm ms-1" title="Stage this slot so its settings apply when the card comes online">Add Card</button>
		<button class="frameDelete btn btn-danger btn-sm ms-1">Delete</button>
	</header>`
	_frameCont.insertAdjacentHTML('beforeend', _header);
	_frameCont.insertAdjacentHTML('beforeend', `<section class="data collapseSection"></div>`);
	return _frameCont;
}

const _slotRenderCache = {};
const _slotQueue = new Map();
let _slotRAF = null;

// drawSlotInfo now receives a per-slot delta {frame, slotName, slot} instead of
// the whole frame + all slots sent once per slot (the old O(slots^2) cost).
// Bursts from one scan cycle are coalesced into a single requestAnimationFrame
// flush, and a slot whose data is unchanged is skipped before any DOM work.
function drawSlotInfo(slotInfo) {
	if (window.pause) return;
	if (!slotInfo || !slotInfo.frame) return;
	_slotQueue.set(slotInfo.frame.ip + '|' + slotInfo.slotName, slotInfo);
	if (_slotRAF === null) _slotRAF = requestAnimationFrame(flushSlotInfo);
}

function flushSlotInfo() {
	_slotRAF = null;
	const items = Array.from(_slotQueue.values());
	_slotQueue.clear();
	for (const slotInfo of items) applySlotInfo(slotInfo);
}

function applySlotInfo(slotInfo) {
	const frameIP = slotInfo.frame.ip;
	// A late slotInfo from an in-flight scan can arrive after the user deleted the frame; don't redraw it
	if (window.deletedFrames && window.deletedFrames.has(frameIP)) return;
	const cacheKey = frameIP + '|' + slotInfo.slotName;
	const slotJSON = JSON.stringify(slotInfo.slot);
	if (_slotRenderCache[cacheKey] === slotJSON) return; // unchanged -> skip the DOM walk
	_slotRenderCache[cacheKey] = slotJSON;

	const _framesCont = document.getElementById('framesCont');
	let _frameData = _framesCont.querySelector(`[data-ip="${frameIP}"] .data`);
	if (!_frameData) {
		const _frame = drawFrame(slotInfo.frame);
		_framesCont.append(_frame);
		_frameData = _framesCont.querySelector(`[data-ip="${frameIP}"] .data`);
	}

	{
		const slotName = slotInfo.slotName;
		let slotExists = true;
		const slot = slotInfo.slot;
		if (!slot) return;

		let _slotCont = _frameData.querySelector(`[data-slot="${slotName}"]`);

		if (!_slotCont) {
			_slotCont = document.createElement('section');
			_slotCont.classList.add('slotCont');
			_slotCont.classList.add('groupCont');
			_slotCont.setAttribute('data-slot', slotName);
			if (slot.offline) _slotCont.classList.add('offline');
			_frameData.appendChild(_slotCont);
			_slotCont.insertAdjacentHTML('beforeend', `<header>
				<button class="btn btn-secondary btn-sm me-2 collapseSettings">Collapse All</button>
				<input type="checkbox" class="form-check form-check-input collapseHeader cardCollapse" id="header_${frameIP.replaceAll('.','_')}_${slotName}">
				<label class="groupName" for="header_${frameIP.replaceAll('.','_')}_${slotName}">Slot ${slotName}</label>
				<div class="cardIface card1Iface me-2" data-status="${slot.ipaup || ''}">${mediaText(1, slot.ipa, slot.ipaup, slot.sfp1)}</div>
				<div class="cardIface card2Iface me-auto" data-status="${slot.ipbup || ''}">${mediaText(2, slot.ipb, slot.ipbup, slot.sfp2)}</div>
				<div class="blastLabel d-flex">Blast
					<div class="form-switch">
						<input type="checkbox" class="form-check-input slotEnable" ${slot.enabled ? 'checked' : ''}>
					</div>
				</div>
				<span class="rebootNeeded badge bg-warning text-dark ms-1 d-none" title="">⟳ Reboot needed</span>
				<button class="cardReboot btn btn-secondary btn-sm">Reboot</button>
				<span class="stagedBadge badge bg-warning text-dark ms-1 d-none">Expected</span>
				<button class="cardRemove btn btn-outline-danger btn-sm ms-1 d-none">Remove</button>
				<button class="cardRetry btn btn-warning btn-sm ms-1 d-none" title="Re-scan and re-push this frame now to retry controls that didn't apply">Retry</button>
			</header>`);
		} else {
			const _iface1 = _slotCont.querySelector('.card1Iface')
			_iface1.innerHTML = mediaText(1, slot.ipa, slot.ipaup, slot.sfp1);
			_iface1.setAttribute('data-status', slot.ipaup || '')
			const _iface2 = _slotCont.querySelector('.card2Iface')
			_iface2.innerHTML = mediaText(2, slot.ipb, slot.ipbup, slot.sfp2);
			_iface2.setAttribute('data-status', slot.ipbup || '')
		}

		// Reflect staged ("expected", pre-configured before discovery) state.
		_slotCont.classList.toggle('staged', !!slot.staged);
		// Keep offline state current on every render, but never hide a staged card
		// (it has no live card yet by definition).
		_slotCont.classList.toggle('offline', !!slot.offline && !slot.staged);
		const _sb = _slotCont.querySelector('.stagedBadge');
		const _cr = _slotCont.querySelector('.cardRemove');
		if (_sb) _sb.classList.toggle('d-none', !slot.staged);
		if (_cr) _cr.classList.toggle('d-none', !slot.staged);

		// Reboot-needed indicator: show next to the Reboot button, with the
		// reasons (which restart-required settings changed) as a hover tooltip.
		const _rb = _slotCont.querySelector('.rebootNeeded');
		if (_rb) {
			_rb.classList.toggle('d-none', !slot.rebootNeeded);
			_rb.title = (slot.rebootReasons || []).join('\n');
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
						<input type="checkbox" class="form-check form-check-input collapseHeader groupCollapse" id="header_${frameIP.replaceAll('.','_')}_${slotName}_i${index}">
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
			
			group.commands?.forEach(command => {
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
		
		for (let spigot = 0; spigot < 16; spigot++) {

			const isInput = slot.ins > spigot;
			const direction = isInput ? "IN" : "OUT";

			let _spigotCont = _slot.querySelector(`[data-spigot="${spigot}"]`);
			if (!_spigotCont) {
				_spigotCont = document.createElement('section');
				_spigotCont.classList.add('groupCont');
				_spigotCont.setAttribute('data-spigot', spigot);
	
				_slot.appendChild(_spigotCont)
	
				_spigotCont.insertAdjacentHTML('beforeend', `<header>
					<input type="checkbox" class="form-check form-check-input collapseHeader" id="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_s${spigot}">
					<label class="groupName" for="header_spig_${frameIP.replaceAll('.','_')}_${slotName}_s${spigot}">${direction} Spigot ${spigot+1} Settings</div>
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


				group.commands?.forEach(command => {
					if ((command.inOnly && isInput) || !command.inOnly) {
						const commandID = Number(command.command) + Number(command.increment * spigot)
						const _command = _collapseSection.querySelector(`[data-command="${commandID}"]`)
						try {						
							// if (commandID == 50265) console.log([commandID, slot.group[commandID]])
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
					}
				})

				checkDeps(_collapseSection);
			})
		}

		markFailures(_slotCont, slot.failed);
	}
	// _frameData.innerHTML = `<pre>${JSON.stringify(slotInfo, null, 4)}</pre>`;
}

// markFailures flags command rows whose blasted SET never took effect (from
// slot.failed: command id -> reason), and clears flags that have since cleared.
function markFailures(_slotCont, failed) {
	failed = failed || {};
	_slotCont.querySelectorAll('.commandCont.cmdFailed').forEach(_c => {
		if (!failed[_c.getAttribute('data-command')]) {
			_c.classList.remove('cmdFailed');
			const _r = _c.querySelector('.commandRead');
			if (_r) _r.removeAttribute('title');
		}
	});
	for (const cmd in failed) {
		const _c = _slotCont.querySelector(`[data-command="${cmd}"]`);
		if (!_c) continue;
		_c.classList.add('cmdFailed');
		const _r = _c.querySelector('.commandRead');
		if (_r) _r.setAttribute('title', failed[cmd]);
	}
	// Offer a Retry button on the card whenever it has any unapplied controls.
	const _retry = _slotCont.querySelector('.cardRetry');
	if (_retry) _retry.classList.toggle('d-none', Object.keys(failed).length === 0);
}

function doFrameStatus(data) {
	try {
		const _framesCont = document.getElementById('framesCont');
		const _frame = _framesCont.querySelector(`[data-ip="${data.frameIP}"]`);
		const _status = _frame.querySelector('.frameStatus');
		if (data.offline) {
			_frame.classList.add('offline');
		} else {
			_frame.classList.remove('offline');
		}
		_status.innerHTML = data.status;
	} catch (error) {
		console.log(error)
	}
}


function doFrameError(data) {
	try {
		const _framesCont = document.getElementById('framesCont');
		const _frame = _framesCont.querySelector(`[data-ip="${data.frameIP}"]`);
		const _status = _frame.querySelector('.frameError');
		_status.innerHTML = data.error;
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

function showTab(_element) {
	const __tabSelects = document.getElementsByClassName('tabSelect');
	for (const _tab of __tabSelects) {
		_tab.classList.remove('active');
	}
	_element.classList.add('active');

	const tab = _element.getAttribute('data-tab');
	const __tabs = document.getElementsByClassName('tab');
	for (const _tab of __tabs) {
		if (_tab.getAttribute('data-tab') == tab) {
			_tab.classList.remove('d-none');
		} else {
			_tab.classList.add('d-none');
		}
	}
	// (removed a querySelector-on-HTMLCollection call here that threw on every switch)
	if (tab == 'users') backend.send('getUsers');
}

// doLog consumes the structured log event {timeString, level, category, message,
// colour} from the Go server and prepends a single node + trims — no more
// re-parsing the entire (up to 499-entry) list via innerHTML, and no ANSI parsing.
function doLog(log) {
	if (!log) return;
	const _logs = document.getElementById('logs');
	const _log = document.createElement('div');
	_log.className = 'log';
	_log.setAttribute('data-level', log.level || '');
	const colourClass = colourToClass(log.colour);
	_log.innerHTML =
		`<span class="logTimestamp">[${log.timeString || ''}]</span>` +
		`<span class="logLevel ${colourClass}">(${log.level || ''})</span>` +
		`<span class="${colourClass} logCatagory">${escapeHTML(log.category || '')} </span>` +
		`<span class="whiteLog">${escapeHTML(log.message || '')}</span>`;
	_logs.insertBefore(_log, _logs.firstChild);
	const maxLogs = 499;
	while (_logs.childElementCount > maxLogs) _logs.lastElementChild.remove();
}

function colourToClass(c) {
	switch (c) {
		case 'red': return 'redLog';
		case 'green': return 'greenLog';
		case 'yellow': return 'yellowLog';
		case 'blue': return 'blueLog';
		case 'purple': return 'purpleLog';
		case 'cyan': return 'cyanLog';
		default: return 'whiteLog';
	}
}

function escapeHTML(s) {
	return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
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
	const dataType = _element.closest('.commandCont').getAttribute('data-type');

	backend.send('setCommand', {
		"ip":frame,
		"slot": slot,
		"command": command,
		"value": _element.value,
		"enabled": enabled,
		"take": take,
		"dataType": dataType
	});
}

function doCheck(_element) {
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const take = _element.closest('.commandCont').getAttribute('data-take');
	const slot = _element.closest('.slotCont').getAttribute('data-slot');
	const frame = _element.closest('.frameCont').getAttribute('data-ip');
	const enabled = _element.closest('.commandCont').querySelector('.commandEnabled').checked;
	const dataType = _element.closest('.commandCont').getAttribute('data-type');

	backend.send('setCommand', {
		"ip":frame,
		"slot": slot,
		"command": command,
		"value": _element.checked ? '1' : '0',
		"enabled": enabled,
		"take": take,
		"dataType": dataType
	});
}

function doOctet(_element) {
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const take = _element.closest('.commandCont').getAttribute('data-take');
	const slot = _element.closest('.slotCont').getAttribute('data-slot');
	const frame = _element.closest('.frameCont').getAttribute('data-ip');
	const __octets = _element.parentElement.querySelectorAll('.octet');
	const enabled = _element.closest('.commandCont').querySelector('.commandEnabled').checked;
	const dataType = _element.closest('.commandCont').getAttribute('data-type');
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
		"take": take,
		"dataType": dataType
	});
}

function doEnable(_element) {
	const command = _element.closest('.commandCont').getAttribute('data-command');
	const slot = _element.closest('.slotCont').getAttribute('data-slot');
	const frame = _element.closest('.frameCont').getAttribute('data-ip');
	const dataType = _element.closest('.commandCont').getAttribute('data-type');
	const take = _element.closest('.commandCont').getAttribute('data-take');

	backend.send('setEnable', {
		"ip":frame,
		"slot": slot,
		"command": command,
		"enabled": _element.checked,
		"dataType": dataType,
		"take": take
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

function doFrameScan(_element) {
	const frame = _element.closest('.frameCont').getAttribute('data-ip');

	backend.send('scanFrame', {
		"ip":frame,
		"scan": _element.checked
	});
}

function doFrameMode(_element) {
	const frame = _element.closest('.frameCont').getAttribute('data-ip');
	switch (_element.value) {
		case "scan":
			backend.send('scanFrame', {
				"ip":frame,
				"scan": true
			});
			backend.send('enableFrame', {
				"ip":frame,
				"enabled": false
			});
			break;
		case "blast":
			backend.send('scanFrame', {
				"ip":frame,
				"scan": true
			});
			backend.send('enableFrame', {
				"ip":frame,
				"enabled": true
			});
			break;
		case "ignore":
			backend.send('scanFrame', {
				"ip":frame,
				"scan": false
			});
			backend.send('enableFrame', {
				"ip":frame,
				"enabled": false
			});
			break;
		default:
			break;
	}
}


function doFrameDelete(_element) {
	const _frame = _element.closest('.frameCont');
	const frame = _frame.getAttribute('data-ip');
	_frame.remove();
	window.deletedFrames = window.deletedFrames || new Set();
	window.deletedFrames.add(frame);
	backend.send('deleteFrame', {
		"ip":frame
	});
}

/* Handle group inputs */

function doGroupInput(_element) {
	if (_element.classList.contains('group_commandCheck')) return
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

/* Users (admin) */

function drawUsers(users) {
	const _cont = document.getElementById('usersCont');
	if (!_cont || !Array.isArray(users)) return;
	_cont.innerHTML = '';
	users.sort((a, b) => a.username.localeCompare(b.username));
	for (const u of users) {
		const _row = document.createElement('div');
		_row.className = 'd-flex align-items-center gap-2 mb-2';
		_row.setAttribute('data-user', u.username);
		_row.innerHTML =
			`<span class="me-2" style="min-width:160px">${escapeHTML(u.username)}${u.disabled ? ' (disabled)' : ''}</span>` +
			`<select class="form-select form-select-sm w-auto userRole">` +
				`<option value="operator"${u.role === 'operator' ? ' selected' : ''}>operator</option>` +
				`<option value="admin"${u.role === 'admin' ? ' selected' : ''}>admin</option>` +
			`</select>` +
			`<input class="form-control form-control-sm w-auto userPass" type="password" placeholder="new password">` +
			`<button class="btn btn-sm btn-secondary userReset">Reset Password</button>` +
			`<button class="btn btn-sm btn-danger userDelete">Delete</button>`;
		_cont.append(_row);
	}
}

function addUser() {
	const username = document.getElementById('addUserName').value;
	const password = document.getElementById('addUserPass').value;
	const role = document.getElementById('addUserRole').value;
	if (!username || !password) return;
	backend.send('addUser', { username: username, password: password, role: role });
	document.getElementById('addUserName').value = '';
	document.getElementById('addUserPass').value = '';
}

/* Import / Export (granular, unified: frames, cards and groups in one file) */

function switchIOTab(tab) {
	document.querySelectorAll('.ioTab').forEach(t => t.classList.toggle('active', t.getAttribute('data-iotab') === tab));
	document.querySelectorAll('.ioPane').forEach(p => p.classList.toggle('d-none', p.getAttribute('data-iotab') !== tab));
}

function visibleIOTree() {
	const pane = document.querySelector('.ioPane:not(.d-none)');
	return pane ? pane.querySelector('.ioTree') : null;
}

function setIOTreeChecked(checked) {
	const tree = visibleIOTree();
	if (tree) tree.querySelectorAll('input[type="checkbox"]').forEach(c => { c.checked = checked; });
}

// buildIOTree renders groups + frames(+cards) with checkboxes from a
// {frames, groups} bundle. `checked` sets the initial tick state.
function buildIOTree(container, data, checked) {
	if (!container) return;
	const frames = (data && data.frames) || {};
	const groups = (data && data.groups) || {};
	container.innerHTML = '';

	const groupNames = Object.keys(groups).sort();
	if (groupNames.length) {
		container.insertAdjacentHTML('beforeend', `<div class="ioSection">Groups</div>`);
		for (const name of groupNames) {
			container.insertAdjacentHTML('beforeend',
				`<label class="ioRow ioGroup"><input type="checkbox" class="form-check-input ioGroupChk" data-group="${escAttr(name)}" ${checked ? 'checked' : ''}> <span>${escapeHTML(name)}</span></label>`);
		}
	}

	container.insertAdjacentHTML('beforeend', `<div class="ioSection">Frames &amp; Cards</div>`);
	for (const ip of Object.keys(frames).sort()) {
		const f = frames[ip] || {};
		const groupTag = f.group ? ` <span class="labelGroup">${escapeHTML(f.group)}</span>` : '';
		container.insertAdjacentHTML('beforeend',
			`<label class="ioRow ioFrame"><input type="checkbox" class="form-check-input ioFrameChk" data-frame="${escAttr(ip)}" data-group-of="${escAttr(f.group || '')}" ${checked ? 'checked' : ''}> <span>F${escapeHTML(f.number || '')} ${escapeHTML(f.name || '')} <span class="text-muted">(${escapeHTML(ip)})</span>${groupTag}</span></label>`);
		const slots = f.slots || {};
		for (const slotName of Object.keys(slots).sort()) {
			const staged = slots[slotName] && slots[slotName].staged ? ' <span class="badge bg-secondary">expected</span>' : '';
			container.insertAdjacentHTML('beforeend',
				`<label class="ioRow ioSlot"><input type="checkbox" class="form-check-input ioSlotChk" data-frame="${escAttr(ip)}" data-slot="${escAttr(slotName)}" ${checked ? 'checked' : ''}> <span>Slot ${escapeHTML(slotName)}${staged}</span></label>`);
		}
	}
}

function cascadeFrame(_el) {
	const tree = _el.closest('.ioTree');
	if (!tree) return;
	tree.querySelectorAll(`.ioSlotChk[data-frame="${attrSel(_el.getAttribute('data-frame'))}"]`).forEach(c => { c.checked = _el.checked; });
}

function cascadeGroup(_el) {
	const tree = _el.closest('.ioTree');
	if (!tree) return;
	// Ticking a group ticks every frame assigned to it (and its cards).
	tree.querySelectorAll(`.ioFrameChk[data-group-of="${attrSel(_el.getAttribute('data-group'))}"]`).forEach(fc => {
		fc.checked = _el.checked;
		cascadeFrame(fc);
	});
}

// collectIO filters the source {frames, groups} by the ticked boxes in `tree`.
function collectIO(tree, source) {
	const out = { frames: {}, groups: {} };
	const srcGroups = (source && source.groups) || {};
	const srcFrames = (source && source.frames) || {};
	tree.querySelectorAll('.ioGroupChk:checked').forEach(c => {
		const name = c.getAttribute('data-group');
		if (srcGroups[name]) out.groups[name] = srcGroups[name];
	});
	tree.querySelectorAll('.ioFrameChk').forEach(fc => {
		const ip = fc.getAttribute('data-frame');
		const checkedSlots = tree.querySelectorAll(`.ioSlotChk[data-frame="${attrSel(ip)}"]:checked`);
		if (!fc.checked && checkedSlots.length === 0) return;
		const src = srcFrames[ip];
		if (!src) return;
		const f = Object.assign({}, src);
		f.slots = {};
		checkedSlots.forEach(sc => {
			const s = sc.getAttribute('data-slot');
			if (src.slots && src.slots[s]) f.slots[s] = src.slots[s];
		});
		out.frames[ip] = f;
	});
	return out;
}

function doExportSelected() {
	const tree = document.getElementById('exportTree');
	if (!tree || !window._exportData) return;
	const sel = collectIO(tree, window._exportData);
	if (Object.keys(sel.frames).length === 0 && Object.keys(sel.groups).length === 0) {
		alert('Nothing selected to export.');
		return;
	}
	const bundle = { demeterExport: 1, exportedAt: new Date().toISOString(), frames: sel.frames, groups: sel.groups };
	download('demeter-export.json', JSON.stringify(bundle, null, 2));
}

async function onImportFile(e) {
	const file = e.target.files && e.target.files[0];
	const btn = document.getElementById('importSelected');
	if (btn) btn.disabled = true;
	if (!file) return;
	try {
		const parsed = JSON.parse(await file.text());
		if (!parsed || typeof parsed !== 'object' || !('demeterExport' in parsed)) {
			alert('That does not look like a Demeter export file.');
			return;
		}
		window._importData = { frames: parsed.frames || {}, groups: parsed.groups || {} };
		buildIOTree(document.getElementById('importTree'), window._importData, true);
		if (btn) btn.disabled = false;
	} catch (err) {
		console.error(err);
		alert('Could not read that file: ' + err.message);
	}
}

function doImportSelected() {
	const tree = document.getElementById('importTree');
	if (!tree || !window._importData) return;
	const sel = collectIO(tree, window._importData);
	if (Object.keys(sel.frames).length === 0 && Object.keys(sel.groups).length === 0) {
		alert('Nothing selected to import.');
		return;
	}
	backend.send('importData', { frames: sel.frames, groups: sel.groups });
	const _modal = window.bootstrap && bootstrap.Modal.getInstance(document.getElementById('importPop'));
	if (_modal) _modal.hide();
}

function escAttr(s) { return String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;'); }
function attrSel(s) { return String(s).replace(/\\/g, '\\\\').replace(/"/g, '\\"'); }

/* First-run generated credentials notice (shown to the desktop admin) */

function showCredentials(notice) {
	if (!notice || !notice.username) return;
	if (document.getElementById('credNotice')) return; // already showing
	const _overlay = document.createElement('div');
	_overlay.id = 'credNotice';
	_overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,.6);z-index:2000;display:flex;align-items:center;justify-content:center;';
	_overlay.innerHTML =
		`<div class="card p-4" style="min-width:420px;max-width:90vw">` +
			`<h4>Admin account created</h4>` +
			`<p class="mb-2">An admin account was generated on first run. Save these credentials somewhere safe, then set a new password.</p>` +
			`<div class="mb-1"><strong>Username:</strong> <code id="credUser"></code></div>` +
			`<div class="mb-3"><strong>Password:</strong> <code id="credPass"></code></div>` +
			`<label class="form-label">New password</label>` +
			`<input id="credNewPass" class="form-control mb-3" type="password" placeholder="New password">` +
			`<div class="d-flex gap-2 justify-content-end">` +
				`<button id="credDismiss" class="btn btn-secondary">Dismiss</button>` +
				`<button id="credChange" class="btn btn-primary">Change password</button>` +
			`</div>` +
		`</div>`;
	document.body.appendChild(_overlay);
	// set values via textContent to avoid any HTML injection from the strings
	document.getElementById('credUser').textContent = notice.username;
	document.getElementById('credPass').textContent = notice.password;
	document.getElementById('credDismiss').addEventListener('click', () => {
		backend.send('dismissNotice');
		_overlay.remove();
	});
	document.getElementById('credChange').addEventListener('click', () => {
		const pw = document.getElementById('credNewPass').value;
		if (!pw) return;
		backend.send('resetPassword', { username: notice.username, password: pw });
		_overlay.remove();
	});
}