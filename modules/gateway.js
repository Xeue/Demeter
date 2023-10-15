const fs = require('fs');
const files = require('fs').promises;
const temp = require('temp').track();
const {Shell} = require('xeue-shell');

class Gateway {
	constructor (
		logger,
		send,
		CFImager,
		__data
	) {
		this.logger = logger;
		this.send = send;
		this.CFImager = CFImager;
		this.__data = __data;
	}

	create(disks) {
		this.send('progress', {'letter':'GENERAL','status':'started'});
		const promises = [];
		disks.forEach(async disk => {
			promises.push(new Promise(async (resolve) => {
				this.send('progress', {'letter':disk.letter, 'status': 'started'});
				if (disk.config) {
					this.send('progress', {'letter':disk.letter, 'status': 'config'});
					await new Promise((resolve, reject) => {
						temp.mkdir('confBU', async (err, dirPath) => {
							const current = `${disk.letter}:/configuration`;
							let skip = false;
							this.logger.debug('Saving config');
							if (fs.existsSync(current)) {
								await files.cp(current, dirPath, {recursive: true});
							} else {
								this.logger.warn('No config file found');
								skip = true;
							}
							await this.init(disk.letter);
							if (!skip) {
								this.logger.debug('Restoring config');
								await files.cp(dirPath, current, {recursive: true});
							}
							resolve();
						})
					})
				} else {
					await this.init(disk.letter);
				}
				if (disk.firmware) await this.firmware(disk);
				this.send('progress', {'letter':disk.letter, 'status': 'complete'});
				resolve();
			}))
		});
		Promise.allSettled(promises).then(()=>{
			this.send('progress', {'letter':'GENERAL','status':'complete'});
		});
	}

	async firmware(disk) {
		this.send('progress', {'letter':disk.letter, 'status': 'firmware'});
		try {
			this.logger.debug('Starting copy of firmware files');
			await files.cp(`${this.__data}/firmware/gateway/${disk.firmwareVersion}`, `${disk.letter}:/`, {recursive: true});
			this.logger.debug('Firmware finished copying');
		} catch (error) {
			this.logger.error(error);
		}
	}

	async init(driveLetter) {
		this.send('progress', {'letter':driveLetter, 'status': 'format'});
		const shell = new Shell(this.logger, 'FORMAT', 'D', 'cmd.exe');
	
		const selectDiskCmd = temp.path({ suffix: '.txt' });
		await files.writeFile(selectDiskCmd, [`SELECT VOLUME ${driveLetter}`,`LIST DISK`, `EXIT`].join('\n'));
		const {stdout} = await shell.run(`diskpart /s "${selectDiskCmd}"`);
		let selectedDisk;
		stdout.forEach(row => {
			if (row.includes("* Disk")) {
				selectedDisk = String(row.match(/\* Disk (.*?) Online/g)).replace('* Disk ', '').replace(/\s*Online/g, '');
				return;
			}
		});
		this.logger.debug('Selected disk is: '+selectedDisk);
	
		const cleanDiskCommand = temp.path({ suffix: '.txt' });
		await files.writeFile(cleanDiskCommand, [
			`SELECT DISK ${selectedDisk}`,
			`CLEAN`,
			`EXIT`
		].join('\n'));
		this.logger.debug('Cleaning disk');
		this.send('diskLog', {'disk':driveLetter, 'message':'Cleaning disk'});
		await shell.run(`diskpart /s "${cleanDiskCommand}"`);
	
		this.logger.debug('Creating new partition');
		this.send('diskLog', {'disk':driveLetter, 'message':'Creating new partition'});
	
		const partition32 = temp.path({ suffix: '.txt' });
		await files.writeFile(partition32, [
			`SELECT DISK ${selectedDisk}`,
			`CREATE PARTITION PRIMARY SIZE=27400 OFFSET=10240`,
			`EXIT`
		].join('\n'));
		const partition16 = temp.path({ suffix: '.txt' });
		await files.writeFile(partition16, [
			`SELECT DISK ${selectedDisk}`,
			`CREATE PARTITION PRIMARY SIZE=12560 OFFSET=10240`,
			`EXIT`
		].join('\n'));
		const partition8 = temp.path({ suffix: '.txt' });
		await files.writeFile(partition8, [
			`SELECT DISK ${selectedDisk}`,
			`CREATE PARTITION PRIMARY SIZE=5550 OFFSET=10240`,
			`EXIT`
		].join('\n'));
		{
			this.logger.debug('Trying to create 27GB partition');
			this.send('diskLog', {'disk':driveLetter, 'message':'Trying to create 27GB partition'});
			const {stdout} = await shell.run(`diskpart /s "${partition32}"`);
			if (stdout.join().includes('not enough usable space')) {
				this.logger.debug('Trying to create 12GB partition');
				this.send('diskLog', {'disk':driveLetter, 'message':'Disk too small, trying to create 12GB partition'});
				const {stdout} = await shell.run(`diskpart /s "${partition16}"`);
				if (stdout.join().includes('not enough usable space')) {
					this.logger.debug('Trying to create 5.5GB partition');
					this.send('diskLog', {'disk':driveLetter, 'message':'Disk too small, trying to create 5.5GB partition'});
					const {stdout} = await shell.run(`diskpart /s "${partition8}"`);
					if (stdout.join().includes('not enough usable space')) {
						this.logger.error('Cannot create partition');
						this.send('diskLog', {'disk':driveLetter, 'message':'Disk too small, failed to create partition'});
						return;
					}
				}
			}
		}
	
		this.logger.debug('Formating new partition');
		this.send('diskLog', {'disk':driveLetter, 'message':'Formatting new partition'});
		await shell.run(`ECHO Y | format ${driveLetter}: /FS:FAT32 /Q /X /V:UCP25_SDI`);
		this.logger.debug('Copying boot files');
		this.send('diskLog', {'disk':driveLetter, 'message':'Copying boot files'});
		await shell.run(`${this.CFImager} -raw -offset 0x400 -skip 0x400 -f ipl.bin -d ${driveLetter}`);
		this.logger.debug('Disk prepared');
	}
}

module.exports.Gateway = Gateway;