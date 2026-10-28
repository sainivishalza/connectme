// Dev tool: drive the Flutter web build in headless Chrome at phone size and
// save screenshots, so the real UI can be reviewed without a phone.
//
//   node shoot.mjs '[{"goto":"http://localhost:8081"},{"wait":3000},{"shot":"login"}]' [outDir] [profileDir]
//
// Actions: goto URL | wait ms | click [x,y] | type "text" | key "Enter" |
//          scroll [x,y,dy] | drag [x1,y1,x2,y2] | shot "name" | eval "js"
// Coordinates are CSS pixels in a 390x844 viewport. The Chrome profile persists
// in profileDir, so a signed-in session survives between runs.
import { spawn } from 'node:child_process';
import { mkdirSync, writeFileSync, existsSync } from 'node:fs';
import path from 'node:path';

const actions = JSON.parse(process.argv[2] || '[]');
const out = path.resolve(process.argv[3] || 'shots');
const profile = path.resolve(process.argv[4] || path.join(out, '.profile'));
const W = Number(process.env.SHOT_W || 390), H = Number(process.env.SHOT_H || 844);
mkdirSync(out, { recursive: true });

const chromePaths = [
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
];
const exe = chromePaths.find(existsSync);
if (!exe) throw new Error('no Chrome or Edge found');
const port = 9300 + Math.floor(Math.random() * 500);
const chrome = spawn(exe, [
  '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, '--no-first-run',
  '--disable-gpu', '--enable-unsafe-swiftshader', '--use-gl=angle', '--use-angle=swiftshader',
  `--window-size=${W},${H}`, 'about:blank',
], { stdio: 'ignore' });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function wsUrl() {
  for (let i = 0; i < 60; i++) {
    try {
      const j = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
      const page = j.find((t) => t.type === 'page');
      if (page) return page.webSocketDebuggerUrl;
    } catch { /* not up yet */ }
    await sleep(250);
  }
  throw new Error('Chrome did not start');
}

const ws = new WebSocket(await wsUrl());
await new Promise((r) => (ws.onopen = r));
let id = 0;
const pending = new Map();
ws.onmessage = (m) => {
  const d = JSON.parse(m.data);
  if (d.id && pending.has(d.id)) {
    const { res, rej } = pending.get(d.id);
    pending.delete(d.id);
    d.error ? rej(new Error(JSON.stringify(d.error))) : res(d.result);
  }
};
const cdp = (method, params = {}) => new Promise((res, rej) => {
  const i = ++id;
  pending.set(i, { res, rej });
  ws.send(JSON.stringify({ id: i, method, params }));
});

await cdp('Page.enable');
await cdp('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: 2, mobile: true });
await cdp('Emulation.setTouchEmulationEnabled', { enabled: true });

const mouse = (type, x, y, extra = {}) =>
  cdp('Input.dispatchMouseEvent', { type, x, y, button: 'left', clickCount: 1, pointerType: 'mouse', ...extra });

try {
  for (const a of actions) {
    if (a.goto) { await cdp('Page.navigate', { url: a.goto }); await sleep(2500); }
    else if (a.wait) await sleep(a.wait);
    else if (a.click) {
      const [x, y] = a.click;
      await mouse('mouseMoved', x, y);
      await mouse('mousePressed', x, y);
      await mouse('mouseReleased', x, y);
      await sleep(400);
    } else if (a.type !== undefined) { await cdp('Input.insertText', { text: a.type }); await sleep(200); }
    else if (a.key) {
      const k = { Enter: ['Enter', 13], Tab: ['Tab', 9], ArrowRight: ['ArrowRight', 39], ArrowLeft: ['ArrowLeft', 37], ArrowUp: ['ArrowUp', 38] }[a.key];
      for (const t of ['keyDown', 'keyUp']) await cdp('Input.dispatchKeyEvent', { type: t, key: k[0], windowsVirtualKeyCode: k[1], code: k[0] });
      await sleep(400);
    } else if (a.scroll) {
      const [x, y, dy] = a.scroll;
      await cdp('Input.dispatchMouseEvent', { type: 'mouseWheel', x, y, deltaX: 0, deltaY: dy });
      await sleep(500);
    } else if (a.drag) {
      const [x1, y1, x2, y2] = a.drag;
      await mouse('mouseMoved', x1, y1);
      await mouse('mousePressed', x1, y1);
      for (let s = 1; s <= 12; s++) { await mouse('mouseMoved', x1 + ((x2 - x1) * s) / 12, y1 + ((y2 - y1) * s) / 12, { buttons: 1 }); await sleep(16); }
      await mouse('mouseReleased', x2, y2);
      await sleep(700);
    } else if (a.eval) {
      const r = await cdp('Runtime.evaluate', { expression: a.eval, returnByValue: true, awaitPromise: true });
      console.log('eval:', JSON.stringify(r.result?.value));
    } else if (a.shot) {
      const r = await cdp('Page.captureScreenshot', { format: 'png' });
      const file = path.join(out, `${a.shot}.png`);
      writeFileSync(file, Buffer.from(r.data, 'base64'));
      console.log('saved', file);
    }
  }
} finally {
  ws.close();
  chrome.kill();
}
