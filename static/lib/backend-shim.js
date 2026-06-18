/* backend-shim.js - provides window.backend.{send,on} over a reconnecting
 * WebSocket, replicating the Electron preload bridge exactly so app.js is
 * otherwise unchanged. Replaces the (unused) commonWS.js.
 *
 * Wire format matches the Go hub: {"command": <channel>, "data": <payload>}.
 */
(function () {
	const handlers = {};
	let ws = null;
	let open = false;
	let queue = [];
	let firstConnect = true;

	function deliver(command, data) {
		(handlers[command] || []).forEach(cb => {
			try { cb(data); } catch (e) { console.error('handler error', command, e); }
		});
	}

	function connect() {
		const proto = location.protocol === 'https:' ? 'wss' : 'ws';
		ws = new WebSocket(`${proto}://${location.host}/ws`);

		ws.onopen = () => {
			open = true;
			// flush anything queued before the socket opened
			queue.forEach(m => ws.send(m));
			queue = [];
			// (re)sync full state on every (re)connect: late joiners + reconnects
			if (!firstConnect) {
				window.backend.send('getFrames');
				window.backend.send('getGroups');
			}
			firstConnect = false;
		};

		ws.onmessage = event => {
			let env;
			try { env = JSON.parse(event.data); } catch (e) { return; }
			if (env && env.command) deliver(env.command, env.data);
		};

		ws.onclose = () => {
			open = false;
			setTimeout(connect, 500); // reconnect
		};

		ws.onerror = () => { try { ws.close(); } catch (e) {} };
	}

	window.backend = {
		send(command, data) {
			const msg = JSON.stringify({ command: command, data: data === undefined ? null : data });
			if (open) ws.send(msg); else queue.push(msg);
		},
		on(command, callback) {
			(handlers[command] = handlers[command] || []).push(callback);
		}
	};

	connect();
})();
