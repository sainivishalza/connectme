'use strict';
// Real-time relay for ConnectMe chat. It holds no message state and never
// sees plaintext: payloads are Signal ciphertext. All authorization and
// persistence happen in the Go API; this service only authenticates sockets,
// forwards to the API with the caller's own token, and fans results out.

const http = require('node:http');
const jwt = require('jsonwebtoken');
const { Server } = require('socket.io');

const ISSUER = 'connectme';
const AUDIENCE = 'connectme-api'; // must match the API's access-token audience
const ERROR_BY_STATUS = {
  400: 'invalid', 401: 'unauthorized', 403: 'forbidden', 404: 'not_found',
  413: 'too_large', 422: 'invalid', 429: 'rate_limited',
};

function verifyAccessToken(token, secret) {
  if (typeof token !== 'string' || token.length === 0 || token.length > 4096) throw new Error('bad token');
  const c = jwt.verify(token, secret, { algorithms: ['HS256'], issuer: ISSUER, audience: AUDIENCE });
  if (c.typ !== 'access' || typeof c.sub !== 'string' || !c.sub) throw new Error('bad token');
  return c;
}

const SIGNAL_KINDS = new Set(['masked', 'game']);
const SIGNAL_TOPIC = /^(masked|game):[0-9a-fA-F-]{36}$/;

function isObject(v) { return v !== null && typeof v === 'object' && !Array.isArray(v); }
function isId(v) { return typeof v === 'string' && /^[0-9a-fA-F-]{36}$/.test(v); }

async function createRealtime(opts) {
  const {
    apiBase, jwtSecret, port = 0, redisUrl = '', allowedOrigins = [],
    accessCacheMs = 20_000, messagesPerMinute = 30, eventsPerMinute = 240,
    expiryGraceMs = 30_000,      // after a token expires the client gets this long to authenticate again
    maxSocketsPerUser = 5,       // per node
    connectsPerMinute = 60,      // per client address
    trustProxy = false,          // read the client address from X-Forwarded-For (only behind our own load balancer)
  } = opts;
  if (!jwtSecret || jwtSecret.length < 32) throw new Error('JWT_SECRET must be at least 32 characters');

  const httpServer = http.createServer((req, res) => {
    if (req.url === '/healthz') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end('{"status":"ok"}');
    } else {
      res.writeHead(404).end();
    }
  });

  const io = new Server(httpServer, {
    maxHttpBufferSize: 64 * 1024,
    cors: allowedOrigins.length ? { origin: allowedOrigins, credentials: true } : undefined,
    pingInterval: 20_000,
    pingTimeout: 20_000,
  });

  const cleanup = [];
  let redis = null; // set when Redis is available: budgets are then shared by every node
  if (redisUrl) {
    const { createClient } = require('redis');
    const { createAdapter } = require('@socket.io/redis-adapter');
    const pub = createClient({ url: redisUrl });
    const sub = pub.duplicate();
    await Promise.all([pub.connect(), sub.connect()]);
    redis = pub;
    io.adapter(createAdapter(pub, sub)); // multi-node fan-out
    cleanup.push(() => pub.quit(), () => sub.quit());
    // The Go API publishes "this topic changed" here after a write; every
    // node forwards it to the sockets watching that topic.
    const signals = pub.duplicate();
    await signals.connect();
    // At most one signal per topic every 400 ms (a busy room would otherwise
    // make every viewer refetch on every message); the last change always
    // goes out.
    const lastSent = new Map(), pending = new Map();
    const fire = (topic) => {
      lastSent.set(topic, Date.now());
      io.to(`sig:${topic}`).emit('signal', { topic });
    };
    await signals.pSubscribe('sig:*', (_msg, channel) => {
      const topic = channel.slice(4);
      if (!SIGNAL_TOPIC.test(topic) || pending.has(topic)) return;
      const wait = 400 - (Date.now() - (lastSent.get(topic) || 0));
      if (wait <= 0) return fire(topic);
      pending.set(topic, setTimeout(() => { pending.delete(topic); fire(topic); }, wait));
    });
    const sweep = setInterval(() => {
      const now = Date.now();
      for (const [t, at] of lastSent) if (now - at > 60_000) lastSent.delete(t);
    }, 60_000);
    sweep.unref();
    cleanup.push(() => { clearInterval(sweep); pending.forEach(clearTimeout); });
    cleanup.push(() => signals.quit());
  }

  // Budgets are per user (not per socket) so extra devices cannot bypass them.
  // With Redis they are shared by every relay node; if Redis cannot be reached
  // the answer is "no" (fail closed) rather than an unlimited budget. Without
  // Redis (tests, a single node) an in-memory sliding window is used.
  const windows = new Map();
  async function allow(name, id, limit) {
    if (redis) {
      const key = `rt:${name}:${id}:${Math.floor(Date.now() / 60_000)}`;
      try {
        const n = await redis.incr(key);
        if (n === 1) await redis.expire(key, 120);
        return n <= limit;
      } catch { return false; }
    }
    const k = `${name}:${id}`, now = Date.now();
    const arr = (windows.get(k) || []).filter((x) => now - x < 60_000);
    if (arr.length >= limit) { windows.set(k, arr); return false; }
    arr.push(now);
    windows.set(k, arr);
    return true;
  }
  const allowMessage = (userId) => allow('msg', userId, messagesPerMinute);
  // Every event of a user (typing, receipts, joins and so on) shares one
  // budget, so a socket cannot be used to flood a peer or to hammer the API
  // through the relay. Messages keep their own, stricter limit above.
  const allowEvent = (userId) => allow('evt', userId, eventsPerMinute);
  const allowConnect = (addr) => allow('conn', addr, connectsPerMinute);
  const sweeper = setInterval(() => {
    const now = Date.now();
    for (const [k, arr] of windows) {
      const fresh = arr.filter((x) => now - x < 60_000);
      if (fresh.length) windows.set(k, fresh); else windows.delete(k);
    }
  }, 60_000);
  sweeper.unref();

  async function api(method, path, token, body) {
    const res = await fetch(apiBase + path, {
      method,
      headers: { Authorization: `Bearer ${token}`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
      body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(8_000),
    });
    let json = null;
    if (res.status !== 204) { try { json = await res.json(); } catch { json = null; } }
    return { status: res.status, json };
  }

  const roomOf = (matchId) => `match:${matchId}`;

  // Re-validates membership periodically so unmatch/block/ban cut a live
  // socket off from relayed events (typing, receipts) within accessCacheMs.
  async function ensureAccess(socket, matchId) {
    const joined = socket.rooms.has(roomOf(matchId));
    if (!joined) return false;
    const checked = socket.data.checked.get(matchId) || 0;
    if (Date.now() - checked < accessCacheMs) return true;
    try {
      const r = await api('GET', `/chat/matches/${matchId}/access`, socket.data.token);
      if (r.status === 204) { socket.data.checked.set(matchId, Date.now()); return true; }
    } catch { /* fall through: fail closed */ }
    socket.leave(roomOf(matchId));
    socket.data.checked.delete(matchId);
    socket.emit('match_closed', { match_id: matchId });
    return false;
  }

  const socketsOf = new Map(); // userId -> open sockets on this node
  const clientAddr = (socket) => {
    if (trustProxy) {
      const hops = String(socket.handshake.headers['x-forwarded-for'] || '').split(',').map((h) => h.trim()).filter(Boolean);
      if (hops.length) return hops[hops.length - 1];
    }
    return socket.handshake.address || 'unknown';
  };

  io.use(async (socket, next) => {
    if (!(await allowConnect(clientAddr(socket)))) return next(new Error('rate_limited'));
    try {
      const c = verifyAccessToken(socket.handshake.auth && socket.handshake.auth.token, jwtSecret);
      if ((socketsOf.get(c.sub) || 0) >= maxSocketsPerUser) return next(new Error('too_many_connections'));
      socket.data.userId = c.sub;
      socket.data.token = socket.handshake.auth.token;
      socket.data.exp = c.exp * 1000;
      socket.data.checked = new Map();
      socket.data.watching = new Set();
      next();
    } catch {
      next(new Error('unauthorized'));
    }
  });

  // When the token runs out the client is told, and gets a short grace period
  // to authenticate again; otherwise the socket is closed. An open socket must
  // not outlive the session that opened it.
  // Timers live here, not in socket.data: the Redis adapter serializes
  // socket.data when it asks other nodes for their sockets.
  const expiryTimers = new Map(); // socket.id -> { exp, grace }
  function clearExpiry(socket) {
    const tm = expiryTimers.get(socket.id);
    if (tm) { clearTimeout(tm.exp); clearTimeout(tm.grace); expiryTimers.delete(socket.id); }
  }
  function armExpiry(socket) {
    clearExpiry(socket);
    const tm = {};
    expiryTimers.set(socket.id, tm);
    tm.exp = setTimeout(() => {
      socket.emit('auth_expired');
      tm.grace = setTimeout(() => socket.disconnect(true), expiryGraceMs);
      tm.grace.unref();
    }, Math.max(0, socket.data.exp - Date.now()));
    tm.exp.unref();
  }

  io.on('connection', (socket) => {
    socketsOf.set(socket.data.userId, (socketsOf.get(socket.data.userId) || 0) + 1);
    armExpiry(socket);
    socket.on('disconnect', () => {
      clearExpiry(socket);
      const n =(socketsOf.get(socket.data.userId) || 1) - 1;
      if (n > 0) socketsOf.set(socket.data.userId, n); else socketsOf.delete(socket.data.userId);
    });
    // Wraps a handler: payload shape, token expiry, uniform error acks.
    const on = (event, handler) => socket.on(event, async (payload, ack) => {
      const reply = typeof ack === 'function' ? ack : () => {};
      try {
        if (!isObject(payload)) return reply({ ok: false, error: 'invalid' });
        if (!(await allowEvent(socket.data.userId))) return reply({ ok: false, error: 'rate_limited' });
        if (Date.now() >= socket.data.exp && event !== 'authenticate') {
          socket.emit('auth_expired');
          return reply({ ok: false, error: 'token_expired' });
        }
        await handler(payload, reply);
      } catch {
        reply({ ok: false, error: 'internal' });
      }
    });

    on('authenticate', (p, reply) => {
      try {
        const c = verifyAccessToken(p.token, jwtSecret);
        if (c.sub !== socket.data.userId) return reply({ ok: false, error: 'unauthorized' });
        socket.data.token = p.token;
        socket.data.exp = c.exp * 1000;
        armExpiry(socket);
        reply({ ok: true });
      } catch {
        reply({ ok: false, error: 'unauthorized' });
      }
    });

    on('join_match', async (p, reply) => {
      if (!isId(p.match_id)) return reply({ ok: false, error: 'invalid' });
      const r = await api('GET', `/chat/matches/${p.match_id}/access`, socket.data.token);
      if (r.status !== 204) return reply({ ok: false, error: ERROR_BY_STATUS[r.status] || 'internal' });
      socket.join(roomOf(p.match_id));
      socket.data.checked.set(p.match_id, Date.now());
      reply({ ok: true });
    });

    on('leave_match', (p, reply) => {
      if (isId(p.match_id)) socket.leave(roomOf(p.match_id));
      reply({ ok: true });
    });

    // watch/unwatch: "tell me when this room or game changes". The API says
    // whether this person may watch it; the signal itself carries no data.
    on('watch', async (p, reply) => {
      if (!SIGNAL_KINDS.has(p.kind) || !isId(p.id)) return reply({ ok: false, error: 'invalid' });
      if (socket.data.watching.size >= 8 && !socket.data.watching.has(`${p.kind}:${p.id}`)) return reply({ ok: false, error: 'too_many' });
      const r = await api('GET', `/signal/${p.kind}/${p.id}/access`, socket.data.token);
      if (r.status !== 204) return reply({ ok: false, error: ERROR_BY_STATUS[r.status] || 'internal' });
      socket.join(`sig:${p.kind}:${p.id}`);
      socket.data.watching.add(`${p.kind}:${p.id}`);
      reply({ ok: true });
    });

    on('unwatch', (p, reply) => {
      if (SIGNAL_KINDS.has(p.kind) && isId(p.id)) {
        socket.leave(`sig:${p.kind}:${p.id}`);
        socket.data.watching.delete(`${p.kind}:${p.id}`);
      }
      reply({ ok: true });
    });

    on('message', async (p, reply) => {
      if (!isId(p.match_id) || typeof p.type !== 'string' || typeof p.ciphertext !== 'string') {
        return reply({ ok: false, error: 'invalid' });
      }
      if (!socket.rooms.has(roomOf(p.match_id))) return reply({ ok: false, error: 'not_joined' });
      if (!(await allowMessage(socket.data.userId))) return reply({ ok: false, error: 'rate_limited' });

      const body = { type: p.type, ciphertext: p.ciphertext };
      if (p.media_id !== undefined) body.media_id = p.media_id;
      if (p.burn_after_s !== undefined) body.burn_after_s = p.burn_after_s;
      const r = await api('POST', `/chat/matches/${p.match_id}/messages`, socket.data.token, body);
      if (r.status !== 201 || !isObject(r.json)) {
        if (r.status === 404) { socket.leave(roomOf(p.match_id)); socket.data.checked.delete(p.match_id); }
        return reply({ ok: false, error: ERROR_BY_STATUS[r.status] || 'internal' });
      }
      const senderId = socket.data.userId;
      for (const s of await io.in(roomOf(p.match_id)).fetchSockets()) {
        s.emit('message', { ...r.json, from_me: s.data.userId === senderId, client_id: s.data.userId === senderId ? p.client_id : undefined });
      }
      reply({ ok: true, message: { ...r.json, from_me: true } });
    });

    on('typing', async (p, reply) => {
      if (!isId(p.match_id)) return reply({ ok: false, error: 'invalid' });
      if (!(await ensureAccess(socket, p.match_id))) return reply({ ok: false, error: 'not_joined' });
      socket.to(roomOf(p.match_id)).emit('typing', { match_id: p.match_id, is_typing: p.is_typing === true });
      reply({ ok: true });
    });

    on('read_receipt', async (p, reply) => {
      if (!isId(p.match_id)) return reply({ ok: false, error: 'invalid' });
      if (!socket.rooms.has(roomOf(p.match_id))) return reply({ ok: false, error: 'not_joined' });
      const r = await api('POST', `/chat/matches/${p.match_id}/read`, socket.data.token);
      if (r.status !== 200 || !isObject(r.json)) return reply({ ok: false, error: ERROR_BY_STATUS[r.status] || 'internal' });
      socket.to(roomOf(p.match_id)).emit('read_receipt', { match_id: p.match_id, count: r.json.count, read_at: r.json.read_at });
      reply({ ok: true, count: r.json.count });
    });
  });

  await new Promise((resolve) => httpServer.listen(port, resolve));
  return {
    port: httpServer.address().port,
    io,
    // Used by tests and single-node setups without Redis.
    signal(topic) { if (SIGNAL_TOPIC.test(topic)) io.to(`sig:${topic}`).emit('signal', { topic }); },
    async close() {
      clearInterval(sweeper);
      await new Promise((resolve) => io.close(resolve));
      for (const fn of cleanup) await fn();
    },
  };
}

module.exports = { createRealtime, verifyAccessToken };
