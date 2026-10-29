'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const jwt = require('jsonwebtoken');
const { io: connect } = require('socket.io-client');
const { createRealtime } = require('../src/server');

const SECRET = 's'.repeat(40);
const MATCH = '11111111-1111-1111-1111-111111111111';
const sign = (sub, opts = {}) =>
  jwt.sign({ typ: 'access', ...opts.claims }, opts.secret || SECRET,
    { algorithm: 'HS256', issuer: 'connectme', subject: sub, expiresIn: opts.expiresIn ?? 60,
      ...(opts.audience === null ? {} : { audience: opts.audience || 'connectme-api' }) });

// A stand-in for the Go API: membership and message persistence.
function startApi() {
  const state = { members: new Map([[MATCH, new Set(['alice', 'bob'])]]), posts: [], reads: 0 };
  const server = http.createServer((req, res) => {
    const tok = (req.headers.authorization || '').replace('Bearer ', '');
    let user;
    try { user = jwt.verify(tok, SECRET).sub; } catch { res.writeHead(401).end(); return; }
    const sg = req.url.match(/^\/signal\/(masked|game)\/([^/]+)\/access$/);
    if (sg) { res.writeHead(sg[2] === MATCH && user !== 'carol' ? 204 : 404).end(); return; }
    const m = req.url.match(/^\/chat\/matches\/([^/]+)\/(access|messages|read)$/);
    if (!m || !state.members.get(m[1])?.has(user)) { res.writeHead(404).end(); return; }
    let body = '';
    req.on('data', (d) => { body += d; });
    req.on('end', () => {
      if (m[2] === 'access') { res.writeHead(204).end(); return; }
      if (m[2] === 'read') {
        state.reads++;
        res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ count: 2, read_at: new Date().toISOString() }));
        return;
      }
      const b = JSON.parse(body);
      state.posts.push({ user, body: b });
      res.writeHead(201, { 'Content-Type': 'application/json' }).end(JSON.stringify({
        id: String(state.posts.length), match_id: m[1], type: b.type, ciphertext: b.ciphertext, from_me: true,
      }));
    });
  });
  return new Promise((resolve) => server.listen(0, () => resolve({ server, state, base: `http://127.0.0.1:${server.address().port}` })));
}

async function harness(t, opts = {}) {
  const api = await startApi();
  const rt = await createRealtime({ apiBase: api.base, jwtSecret: SECRET, ...opts });
  const clients = [];
  const client = (token) => {
    const s = connect(`http://127.0.0.1:${rt.port}`, { auth: { token }, transports: ['websocket'], reconnection: false });
    clients.push(s);
    return s;
  };
  const ready = (s) => new Promise((res, rej) => { s.on('connect', res); s.on('connect_error', rej); });
  const emit = (s, ev, p) => new Promise((res) => s.emit(ev, p, res));
  const next = (s, ev, ms = 1000) => new Promise((res, rej) => {
    const t = setTimeout(() => rej(new Error(`timeout waiting for ${ev}`)), ms);
    s.once(ev, (v) => { clearTimeout(t); res(v); });
  });
  const silent = (s, ev, ms = 250) => new Promise((res, rej) => {
    const h = () => rej(new Error(`unexpected ${ev}`));
    s.once(ev, h);
    setTimeout(() => { s.off(ev, h); res(); }, ms);
  });
  const close = async () => { clients.forEach((c) => c.close()); await rt.close(); api.server.close(); };
  t.after(close); // runs even when an assertion fails, so no handles leak
  return { api, rt, client, ready, emit, next, silent, close };
}

test('rejects missing, forged, expired and alg-none tokens', async (t) => {
  const h = await harness(t);
  const none = jwt.sign({ typ: 'access' }, null, { algorithm: 'none', subject: 'alice', issuer: 'connectme' });
  const cases = {
    missing: '',
    forged: sign('alice', { secret: 'x'.repeat(40) }),
    expired: sign('alice', { expiresIn: -10 }),
    'alg none': none,
    'refresh-typed': sign('alice', { claims: { typ: 'refresh' } }),
    'no audience': sign('alice', { audience: null }),
    'another audience': sign('alice', { audience: 'someone-else' }),
  };
  for (const [name, token] of Object.entries(cases)) {
    await assert.rejects(h.ready(h.client(token)), /unauthorized/, name);
  }
  const ok = h.client(sign('alice'));
  await h.ready(ok);
});

test('relays messages, typing and receipts only between joined participants', async (t) => {
  const h = await harness(t);
  const a = h.client(sign('alice')), b = h.client(sign('bob')), c = h.client(sign('carol'));
  await Promise.all([h.ready(a), h.ready(b), h.ready(c)]);

  assert.deepEqual(await h.emit(a, 'join_match', { match_id: MATCH }), { ok: true });
  assert.deepEqual(await h.emit(b, 'join_match', { match_id: MATCH }), { ok: true });
  assert.equal((await h.emit(c, 'join_match', { match_id: MATCH })).error, 'not_found');
  assert.equal((await h.emit(c, 'message', { match_id: MATCH, type: 'text', ciphertext: 'eA==' })).error, 'not_joined');

  const gotB = h.next(b, 'message'), gotA = h.next(a, 'message');
  const ack = await h.emit(a, 'message', { match_id: MATCH, type: 'text', ciphertext: 'eA==', client_id: 'c1' });
  assert.equal(ack.ok, true);
  const mb = await gotB, ma = await gotA;
  assert.equal(mb.from_me, false);
  assert.equal(ma.from_me, true);
  assert.equal(ma.client_id, 'c1');
  assert.equal(mb.client_id, undefined);
  await h.silent(c, 'message');

  const typing = h.next(b, 'typing');
  await h.emit(a, 'typing', { match_id: MATCH, is_typing: true });
  assert.deepEqual(await typing, { match_id: MATCH, is_typing: true });
  await h.silent(a, 'typing');

  const receipt = h.next(a, 'read_receipt');
  await h.emit(b, 'read_receipt', { match_id: MATCH });
  assert.equal((await receipt).count, 2);
});

test('only whitelisted fields reach the API (no sender spoofing)', async (t) => {
  const h = await harness(t);
  const a = h.client(sign('alice'));
  await h.ready(a);
  await h.emit(a, 'join_match', { match_id: MATCH });
  await h.emit(a, 'message', { match_id: MATCH, type: 'text', ciphertext: 'eA==', sender_id: 'bob', from_me: false, burn_after_s: 5 });
  const post = h.api.state.posts.at(-1);
  assert.deepEqual(Object.keys(post.body).sort(), ['burn_after_s', 'ciphertext', 'type']);
  assert.equal(post.user, 'alice');
});

test('invalid payloads get an error ack, not a crash', async (t) => {
  const h = await harness(t);
  const a = h.client(sign('alice'));
  await h.ready(a);
  for (const p of [null, 'str', [], {}, { match_id: 'nope' }, { match_id: 42 }]) {
    const r = await h.emit(a, 'join_match', p);
    assert.equal(r.ok, false);
  }
  await h.emit(a, 'join_match', { match_id: MATCH });
  assert.equal((await h.emit(a, 'message', { match_id: MATCH, type: 5, ciphertext: {} })).error, 'invalid');
});

test('rate limits to 30 messages per minute per user across devices', async (t) => {
  const h = await harness(t);
  const d1 = h.client(sign('alice')), d2 = h.client(sign('alice'));
  await Promise.all([h.ready(d1), h.ready(d2)]);
  await h.emit(d1, 'join_match', { match_id: MATCH });
  await h.emit(d2, 'join_match', { match_id: MATCH });
  let ok = 0;
  for (let i = 0; i < 40; i++) {
    const r = await h.emit(i % 2 ? d1 : d2, 'message', { match_id: MATCH, type: 'text', ciphertext: 'eA==' });
    if (r.ok) ok++; else assert.equal(r.error, 'rate_limited');
  }
  assert.equal(ok, 30);
  assert.equal(h.api.state.posts.length, 30);
});

test('losing access (unmatch/block) cuts off relayed events', async (t) => {
  const h = await harness(t, { accessCacheMs: 30 });
  const a = h.client(sign('alice')), b = h.client(sign('bob'));
  await Promise.all([h.ready(a), h.ready(b)]);
  await h.emit(a, 'join_match', { match_id: MATCH });
  await h.emit(b, 'join_match', { match_id: MATCH });

  h.api.state.members.get(MATCH).delete('bob'); // bob is unmatched
  await new Promise((r) => setTimeout(r, 60));
  const closed = h.next(b, 'match_closed');
  assert.equal((await h.emit(b, 'typing', { match_id: MATCH, is_typing: true })).error, 'not_joined');
  assert.equal((await closed).match_id, MATCH);
  await h.silent(a, 'typing');
  assert.equal((await h.emit(b, 'message', { match_id: MATCH, type: 'text', ciphertext: 'eA==' })).error, 'not_joined');
});

test('expired token blocks events until re-authenticated as the same user', async (t) => {
  const h = await harness(t);
  const a = h.client(sign('alice', { expiresIn: 1 }));
  await h.ready(a);
  assert.deepEqual(await h.emit(a, 'join_match', { match_id: MATCH }), { ok: true });
  await new Promise((r) => setTimeout(r, 1200));

  const expired = h.next(a, 'auth_expired');
  assert.equal((await h.emit(a, 'message', { match_id: MATCH, type: 'text', ciphertext: 'eA==' })).error, 'token_expired');
  await expired;

  assert.equal((await h.emit(a, 'authenticate', { token: sign('bob') })).error, 'unauthorized');
  assert.deepEqual(await h.emit(a, 'authenticate', { token: sign('alice') }), { ok: true });
  assert.equal((await h.emit(a, 'message', { match_id: MATCH, type: 'text', ciphertext: 'eA==' })).ok, true);
});

test('typing, receipts and joins share a per-user budget so a socket cannot flood a peer', async (t) => {
  const h = await harness(t, { eventsPerMinute: 20 });
  const a = h.client(sign('alice')), a2 = h.client(sign('alice')), b = h.client(sign('bob'));
  await Promise.all([h.ready(a), h.ready(a2), h.ready(b)]);
  await h.emit(a, 'join_match', { match_id: MATCH });
  await h.emit(b, 'join_match', { match_id: MATCH });
  let typingSeen = 0;
  b.on('typing', () => { typingSeen++; });
  let limited = 0;
  // 18 more events on top of the join: 19 so far; the budget is 20 across both devices.
  for (let i = 0; i < 18; i++) await h.emit(a, 'typing', { match_id: MATCH, is_typing: true });
  for (let i = 0; i < 6; i++) {
    const r = await h.emit(a2, 'typing', { match_id: MATCH, is_typing: true });
    if (r.error === 'rate_limited') limited++;
  }
  assert.ok(limited >= 5, `extra devices share the budget, limited=${limited}`);
  assert.ok(typingSeen <= 19, `the peer was not flooded (saw ${typingSeen})`);
  // The other person is unaffected.
  assert.deepEqual(await h.emit(b, 'join_match', { match_id: MATCH }), { ok: true });
});

test('a signal reaches only the sockets allowed to watch that topic', async (t) => {
  const h = await harness(t);
  const a = h.client(sign('alice')), c = h.client(sign('carol'));
  await Promise.all([h.ready(a), h.ready(c)]);
  assert.deepEqual(await h.emit(a, 'watch', { kind: 'game', id: MATCH }), { ok: true });
  assert.equal((await h.emit(c, 'watch', { kind: 'game', id: MATCH })).error, 'not_found');
  assert.equal((await h.emit(a, 'watch', { kind: 'nope', id: MATCH })).error, 'invalid');
  assert.equal((await h.emit(a, 'watch', { kind: 'game', id: 'x' })).error, 'invalid');

  const got = h.next(a, 'signal');
  h.rt.signal(`game:${MATCH}`);
  assert.deepEqual(await got, { topic: `game:${MATCH}` });
  await h.silent(c, 'signal');

  await h.emit(a, 'unwatch', { kind: 'game', id: MATCH });
  h.rt.signal(`game:${MATCH}`);
  await h.silent(a, 'signal');
});

// ---- hardening: expiry, connection caps, shared budgets ----

test('a socket is closed once its token has expired and the grace period has passed', async (t) => {
  const h = await harness(t, { expiryGraceMs: 150 });
  const s = h.client(sign('alice', { expiresIn: 1 }));
  await h.ready(s);
  await h.next(s, 'auth_expired', 3000);
  await new Promise((res, rej) => {
    const to = setTimeout(() => rej(new Error('still connected after the grace period')), 2000);
    s.on('disconnect', () => { clearTimeout(to); res(); });
  });
});

test('authenticating again with a fresh token before the grace ends keeps the socket open', async (t) => {
  const h = await harness(t, { expiryGraceMs: 600 });
  const s = h.client(sign('alice', { expiresIn: 1 }));
  await h.ready(s);
  await h.next(s, 'auth_expired', 3000);
  assert.deepEqual(await h.emit(s, 'authenticate', { token: sign('alice', { expiresIn: 60 }) }), { ok: true });
  await new Promise((r) => setTimeout(r, 1000));
  assert.equal(s.connected, true);
});

test('one member cannot hold more sockets than the cap', async (t) => {
  const h = await harness(t, { maxSocketsPerUser: 2 });
  await h.ready(h.client(sign('alice')));
  await h.ready(h.client(sign('alice')));
  await assert.rejects(h.ready(h.client(sign('alice'))), /too_many_connections/);
  await h.ready(h.client(sign('bob'))); // someone else is not affected
});

test('one address cannot open connections faster than the limit', async (t) => {
  const h = await harness(t, { connectsPerMinute: 3 });
  for (const u of ['u1', 'u2', 'u3']) await h.ready(h.client(sign(u)));
  await assert.rejects(h.ready(h.client(sign('u4'))), /rate_limited/);
});

test('the message budget is shared by every relay node when Redis is available', async (t) => {
  const url = process.env.TEST_REDIS_URL;
  if (!url) { t.skip('TEST_REDIS_URL not set'); return; }
  const api = await startApi();
  const mk = () => createRealtime({ apiBase: api.base, jwtSecret: SECRET, redisUrl: url, messagesPerMinute: 3 });
  const [a, b] = [await mk(), await mk()];
  const user = `shared${Date.now()}`;
  api.state.members.get(MATCH).add(user);
  const open = async (rt) => {
    const s = connect(`http://127.0.0.1:${rt.port}`, { auth: { token: sign(user) }, transports: ['websocket'], reconnection: false });
    await new Promise((res, rej) => { s.on('connect', res); s.on('connect_error', rej); });
    await new Promise((res) => s.emit('join_match', { match_id: MATCH }, res));
    return s;
  };
  const [sa, sb] = [await open(a), await open(b)];
  t.after(async () => { sa.close(); sb.close(); await a.close(); await b.close(); api.server.close(); });
  const send = (s) => new Promise((res) => s.emit('message', { match_id: MATCH, type: 'text', ciphertext: 'eA==' }, res));
  assert.equal((await send(sa)).ok, true);
  assert.equal((await send(sb)).ok, true);
  assert.equal((await send(sa)).ok, true);
  assert.equal((await send(sb)).error, 'rate_limited', 'the 4th message in a minute must be refused whichever node it lands on');
});
