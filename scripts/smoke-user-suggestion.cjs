// Live smoke test for the Go backend `user_suggestion` handler.
// Connects a WebSocket, sends user_suggestion, and prints any
// conversation_response deltas received within the timeout window.
const WebSocket = require('ws');

const url = process.env.WS_URL || 'ws://localhost:8080/ws/test-smoke';
const ws = new WebSocket(url);

let convoDeltas = 0;
let finalSeen = false;
let total = 0;

ws.on('open', () => {
	console.log('OPEN');
	// Ask something short; the handler streams markdown back.
	ws.send(JSON.stringify({ type: 'user_suggestion', message: 'Hi! Just reply with exactly: hello world' }));
});

ws.on('message', (raw) => {
	let msg;
	try { msg = JSON.parse(String(raw)); } catch { return; }
	total++;
	if (msg.type === 'conversation_response') {
		convoDeltas++;
		if (msg.isStreaming) {
			process.stdout.write(msg.message || '');
		} else {
			finalSeen = true;
			console.log('\n[FINAL non-streaming event]');
		}
	} else {
		console.log('  [other]', msg.type, JSON.stringify(msg).slice(0, 200));
	}
});

ws.on('error', (e) => { console.error('WS_ERROR', e.message); process.exit(1); });
ws.on('close', () => {
	console.log('\nCLOSE total=' + total + ' convoDeltas=' + convoDeltas + ' finalSeen=' + finalSeen);
	process.exit(convoDeltas > 0 && finalSeen ? 0 : 2);
});

setTimeout(() => {
	console.log('\n[timeout] received ' + convoDeltas + ' conversation_response events');
	if (convoDeltas === 0) process.exit(2);
	ws.close();
}, 30000);
