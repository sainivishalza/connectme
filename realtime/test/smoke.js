'use strict';
// Full-stack smoke test (manual): needs the Go API and this relay running
// against real databases. Usage: node test/smoke.js <go-log-file>
// It registers two users, reads their dev verification tokens from the API
// log, matches them, then chats over Socket.io and checks REST history.
const fs = require('node:fs');
const { io } = require('socket.io-client');

const API = process.env.API_BASE || 'http://localhost:8080';
const RT = process.env.RT_BASE || 'http://localhost:3001';
const LOG = process.argv[2];
const PW = 'Smoke-Test-Pw-42!';

const j = async (method, path, token, body) => {
  const r = await fetch(API + path, {
    method,
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await r.text();
  return { status: r.status, json: text ? JSON.parse(text) : null };
};
const check = (cond, msg) => { if (!cond) { console.error('FAIL:', msg); process.exit(1); } console.log('ok  -', msg); };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function signup(tag) {
  const email = `smoke-${tag}-${Date.now()}@example.com`;
  let r = await j('POST', '/auth/register', null, { email, password: PW, date_of_birth: '1992-04-04' });
  check(r.status === 201, `register ${tag}`);
  await sleep(300);
  const lines = fs.readFileSync(LOG, 'utf8').split('\n').filter((l) => l.includes(email) && l.includes('verify_email'));
  const token = /token=(\S+)/.exec(lines.at(-1) || '')?.[1];
  check(token, `dev verification token for ${tag} found in log`);
  r = await j('POST', '/auth/verify-email', null, { token });
  check(r.status === 204, `verify email ${tag}`);
  r = await j('POST', '/auth/login', null, { email, password: PW });
  check(r.status === 200 && r.json.email_verified === true, `login ${tag}`);
  return r.json.access_token;
}

(async () => {
  const a = await signup('a'), b = await signup('b');
  const meA = (await j('GET', '/users/me', a)).json, meB = (await j('GET', '/users/me', b)).json;
  await j('PATCH', '/users/me', a, { display_name: 'Smoke A' });
  await j('PATCH', '/users/me', b, { display_name: 'Smoke B' });

  let r = await j('POST', '/matching/swipe', a, { target_ref: meB.ref, action: 'like' });
  check(r.status === 200 && !r.json.matched, 'A likes B (no match yet)');
  r = await j('POST', '/matching/swipe', b, { target_ref: meA.ref, action: 'like' });
  check(r.status === 200 && r.json.matched, 'B likes A (match)');
  const matchId = r.json.match_id;

  const connect = (token) => new Promise((res, rej) => {
    const s = io(RT, { auth: { token }, transports: ['websocket'], reconnection: false });
    s.on('connect', () => res(s));
    s.on('connect_error', rej);
  });
  const emit = (s, ev, p) => new Promise((res) => s.emit(ev, p, res));
  const sa = await connect(a), sb = await connect(b);
  check((await emit(sa, 'join_match', { match_id: matchId })).ok, 'A joined match room');
  check((await emit(sb, 'join_match', { match_id: matchId })).ok, 'B joined match room');

  const cipher = Buffer.from('opaque-signal-ciphertext').toString('base64');
  const incoming = new Promise((res) => sb.once('message', res));
  const ack = await emit(sa, 'message', { match_id: matchId, type: 'text', ciphertext: cipher, burn_after_s: 60 });
  check(ack.ok, 'A sent message over Socket.io');
  const got = await incoming;
  check(got.ciphertext === cipher && got.from_me === false, 'B received the ciphertext in real time');

  const hist = await j('GET', `/chat/matches/${matchId}/messages`, b);
  check(hist.json.messages.length === 1 && hist.json.messages[0].ciphertext === cipher, 'REST history matches');

  const rec = new Promise((res) => sa.once('read_receipt', res));
  await emit(sb, 'read_receipt', { match_id: matchId });
  check((await rec).count === 1, 'A received B\'s read receipt');

  sa.close(); sb.close();
  console.log('\nSMOKE TEST PASSED');
})().catch((e) => { console.error('ERROR', e); process.exit(1); });
