// Dev-only HTTPS preview server for trying the Flutter web build on a phone.
//
// One origin serves everything, so the browser needs no CORS and the app gets
// the secure context that secure storage, crypto, camera and location require:
//   /                -> mobile/build/web (static)
//   /socket.io/*     -> realtime relay (WebSocket upgrades included)
//   everything else  -> the Go API
//
// Not for production: self-signed certificate, no hardening. Node built-ins only.
const https = require('https');
const http = require('http');
const net = require('net');
const fs = require('fs');
const os = require('os');
const { execFileSync } = require('child_process');
const path = require('path');

const PORT = Number(process.env.PREVIEW_PORT || 8443);
const API = new URL(process.env.API_URL || 'http://127.0.0.1:8080');
const RT = new URL(process.env.REALTIME_URL || 'http://127.0.0.1:3001');
const ROOT = path.resolve(process.env.PREVIEW_ROOT || path.join(__dirname, '../../build/web'));

const TYPES = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.mjs': 'text/javascript',
  '.json': 'application/json', '.css': 'text/css', '.png': 'image/png', '.jpg': 'image/jpeg',
  '.svg': 'image/svg+xml', '.ico': 'image/x-icon', '.wasm': 'application/wasm',
  '.otf': 'font/otf', '.ttf': 'font/ttf', '.woff2': 'font/woff2', '.map': 'application/json',
};

function target(req) {
  return req.url.startsWith('/socket.io') ? RT : API;
}

const zlib = require('zlib');
const gzCache = new Map(); // file -> {mtime, buf}

function serveStatic(req, res) {
  const url = new URL(req.url, 'http://x');
  let rel = decodeURIComponent(url.pathname);
  if (rel === '/') rel = '/index.html';
  const file = path.normalize(path.join(ROOT, rel));
  if (!file.startsWith(ROOT)) return false; // no traversal
  let st;
  try { st = fs.statSync(file); } catch { return false; }
  if (!st.isFile()) return false;
  const etag = `"${st.size}-${Math.floor(st.mtimeMs)}"`;
  const headers = {
    'Content-Type': TYPES[path.extname(file)] || 'application/octet-stream',
    'Cache-Control': 'no-cache', // always revalidate; unchanged files answer 304
    ETag: etag,
  };
  if (req.headers['if-none-match'] === etag) { res.writeHead(304, headers); res.end(); return true; }
  // The wasm and JS bundles are many MB; compress them (phones on a hotspot are slow).
  const compressible = /\.(js|mjs|wasm|json|html|css|svg|symbols|otf|ttf)$/.test(file);
  if (compressible && /gzip/.test(req.headers['accept-encoding'] || '')) {
    let c = gzCache.get(file);
    if (!c || c.etag !== etag) {
      c = { etag, buf: zlib.gzipSync(fs.readFileSync(file), { level: 6 }) };
      gzCache.set(file, c);
    }
    res.writeHead(200, { ...headers, 'Content-Encoding': 'gzip', 'Content-Length': c.buf.length, Vary: 'Accept-Encoding' });
    res.end(req.method === 'HEAD' ? undefined : c.buf);
    return true;
  }
  res.writeHead(200, { ...headers, 'Content-Length': st.size });
  if (req.method === 'HEAD') { res.end(); return true; }
  fs.createReadStream(file).pipe(res);
  return true;
}

function proxy(req, res) {
  const t = target(req);
  const up = http.request({
    host: t.hostname, port: t.port, method: req.method, path: req.url,
    headers: { ...req.headers, host: t.host, 'x-forwarded-proto': 'https' },
  }, (r) => { res.writeHead(r.statusCode, r.headers); r.pipe(res); });
  up.on('error', () => { res.writeHead(502); res.end('upstream unavailable'); });
  req.pipe(up);
}

// PREVIEW_HTTP=1 serves plain HTTP on localhost only (handy for debugging on this PC).
const handler = (req, res) => {
  if ((req.method === 'GET' || req.method === 'HEAD') && !req.url.startsWith('/socket.io') && serveStatic(req, res)) return;
  // Unknown GET paths that look like app routes fall back to the app shell.
  if (req.method === 'GET' && (req.headers.accept || '').includes('text/html') && !req.url.startsWith('/admin')) {
    req.url = '/';
    if (serveStatic(req, res)) return;
  }
  proxy(req, res);
};
// The certificate must name every address a phone may use to reach this
// machine. It is (re)generated whenever the machine's addresses change, so a
// new Wi-Fi or hotspot needs no manual steps. Self-signed: browsers warn once.
function localIPv4s() {
  const out = [];
  for (const list of Object.values(os.networkInterfaces())) {
    for (const a of list || []) if (a.family === 'IPv4' && !a.internal) out.push(a.address);
  }
  return out.sort();
}

function ensureCert() {
  const keyFile = path.join(__dirname, 'key.pem');
  const certFile = path.join(__dirname, 'cert.pem');
  const stamp = path.join(__dirname, 'cert.ips');
  const ips = localIPv4s();
  const have = fs.existsSync(stamp) ? fs.readFileSync(stamp, 'utf8').trim() : '';
  if (!(fs.existsSync(keyFile) && fs.existsSync(certFile) && have === ips.join(','))) {
    const san = ['IP:127.0.0.1', 'DNS:localhost', ...ips.map((i) => `IP:${i}`)].join(',');
    execFileSync('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '30', '-keyout', keyFile,
      '-out', certFile, '-subj', '/CN=connectme-dev', '-addext', `subjectAltName=${san}`], { stdio: 'ignore' });
    fs.writeFileSync(stamp, ips.join(','));
    console.log(`generated a certificate for ${ips.join(', ') || 'localhost'}`);
  }
  return { key: fs.readFileSync(keyFile), cert: fs.readFileSync(certFile) };
}

const server = process.env.PREVIEW_HTTP === '1'
  ? http.createServer(handler)
  : https.createServer(ensureCert(), handler);

// WebSocket upgrades (Socket.io) are tunnelled byte-for-byte.
server.on('upgrade', (req, socket, head) => {
  const t = target(req);
  const up = net.connect(Number(t.port), t.hostname, () => {
    up.write(`${req.method} ${req.url} HTTP/1.1\r\n` +
      Object.entries({ ...req.headers, host: t.host }).map(([k, v]) => `${k}: ${v}`).join('\r\n') + '\r\n\r\n');
    up.write(head);
    socket.pipe(up).pipe(socket);
  });
  up.on('error', () => socket.destroy());
  socket.on('error', () => up.destroy());
});

const host = process.env.PREVIEW_HTTP === '1' ? '127.0.0.1' : '0.0.0.0';
server.listen(PORT, host, () => {
  console.log(`preview listening on ${host}:${PORT}`);
  if (process.env.PREVIEW_HTTP !== '1') for (const ip of localIPv4s()) console.log(`  open https://${ip}:${PORT} on a device on the same network`);
});
