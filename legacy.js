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

		const [unitAddress, err] = await getInfo(17044, frameIP, '00', '00');
		
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
