'use strict';
// Manual end-to-end check against the REAL running API, Redis and relay:
//   node test/signal_e2e.js <match_id> <user_id>
// Signs in as that member, watches the game topic, makes a write through the
// API and expects the relay to push a "signal". Needs ../.env (JWT_SECRET).
const jwt = require('jsonwebtoken');
const { io } = require('socket.io-client');
try { process.loadEnvFile('../.env'); } catch { /* optional */ }

const [match, user] = process.argv.slice(2);
const token = jwt.sign({ typ: 'access' }, process.env.JWT_SECRET, { algorithm: 'HS256', issuer: 'connectme', audience: 'connectme-api', subject: user, expiresIn: 120 });
const api = process.env.API_BASE || 'http://localhost:8080';
const s = io('http://localhost:3001', { auth: { token }, transports: ['websocket'], reconnection: false });
const fail = (m) => { console.error('FAIL:', m); process.exit(1); };
setTimeout(() => fail('no signal within 6 s'), 6000);
s.on('connect_error', (e) => fail('connect ' + e.message));
s.on('signal', (d) => { console.log('signal received:', JSON.stringify(d)); process.exit(0); });
s.on('connect', () => {
  s.emit('watch', { kind: 'game', id: match }, async (r) => {
    console.log('watch ->', JSON.stringify(r));
    if (!r || !r.ok) fail('watch refused');
    const res = await fetch(`${api}/games/td/${match}/start`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } });
    console.log('write ->', res.status);
  });
});
