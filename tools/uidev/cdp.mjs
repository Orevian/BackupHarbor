// Minimal Chrome DevTools Protocol client used by the UI smoke test. No dependencies.
import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export async function launchBrowser({ exe, port = 9333, width = 1280, height = 860, scale = 1, extraArgs = [] }) {
  const profile = mkdtempSync(join(tmpdir(), 'rp-edge-'));
  const proc = spawn(exe, [
    '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, `--window-size=${width},${height}`,
    `--force-device-scale-factor=${scale}`, '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--hide-scrollbars=false', ...extraArgs, 'about:blank',
  ], { stdio: 'ignore' });
  let targets;
  for (let i = 0; i < 100; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${port}/json/list`);
      targets = (await r.json()).filter((t) => t.type === 'page');
      if (targets.length) break;
    } catch { /* not up yet */ }
    await sleep(100);
  }
  if (!targets || !targets.length) { proc.kill(); throw new Error('browser did not start'); }
  const ws = new WebSocket(targets[0].webSocketDebuggerUrl);
  await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; });
  let id = 0;
  const pending = new Map();
  const logs = [];
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.id && pending.has(msg.id)) { const { res, rej } = pending.get(msg.id); pending.delete(msg.id); msg.error ? rej(new Error(msg.error.message)) : res(msg.result); }
    else if (msg.method === 'Runtime.consoleAPICalled' && ['error', 'warning'].includes(msg.params.type)) logs.push(msg.params.type + ': ' + msg.params.args.map((a) => a.value ?? a.description).join(' '));
    else if (msg.method === 'Runtime.exceptionThrown') logs.push('exception: ' + (msg.params.exceptionDetails.exception?.description || msg.params.exceptionDetails.text));
  };
  const send = (method, params = {}) => new Promise((res, rej) => { const i = ++id; pending.set(i, { res, rej }); ws.send(JSON.stringify({ id: i, method, params })); });
  await send('Page.enable'); await send('Runtime.enable');

  const api = {
    logs, send,
    async goto(url) { await send('Page.navigate', { url }); await sleep(300); },
    async eval(expr) {
      const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) throw new Error('eval failed: ' + (r.exceptionDetails.exception?.description || r.exceptionDetails.text) + '\n' + expr);
      return r.result.value;
    },
    async waitFor(expr, timeout = 15000, what = expr) {
      const end = Date.now() + timeout;
      while (Date.now() < end) { try { if (await api.eval(expr)) return true; } catch { /* page navigating */ } await sleep(100); }
      throw new Error('timeout waiting for: ' + what);
    },
    async click(sel) { await api.waitFor(`!!document.querySelector(${JSON.stringify(sel)})`, 8000, 'element ' + sel); await api.eval(`document.querySelector(${JSON.stringify(sel)}).click()`); },
    async clickText(sel, text) { await api.eval(`(() => { const e = Array.from(document.querySelectorAll(${JSON.stringify(sel)})).find((x) => x.textContent.trim().includes(${JSON.stringify(text)})); if (!e) throw new Error('no ${sel} with text ${text}'); e.click(); })()`); },
    async set(sel, value) { await api.eval(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e) throw new Error('missing ${sel}'); e.value = ${JSON.stringify(value)}; e.dispatchEvent(new Event('input', {bubbles:true})); e.dispatchEvent(new Event('change', {bubbles:true})); })()`); },
    async text(sel) { return api.eval(`(document.querySelector(${JSON.stringify(sel)}) || {}).textContent || ''`); },
    async shot(path, { fullPage = false } = {}) {
      mkdirSync(dirname(path), { recursive: true });
      let clip;
      if (fullPage) {
        const m = await send('Page.getLayoutMetrics');
        const h = await api.eval('Math.max(document.getElementById("main").scrollHeight + 0, window.innerHeight)');
        await send('Emulation.setDeviceMetricsOverride', { width, height: Math.min(h, 2400), deviceScaleFactor: scale, mobile: false });
      }
      const r = await send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: false });
      writeFileSync(path, Buffer.from(r.data, 'base64'));
      if (fullPage) await send('Emulation.clearDeviceMetricsOverride');
    },
    async setColorScheme(scheme) { await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: scheme }] }); },
    close() { try { ws.close(); } catch { /* ignore */ } proc.kill(); },
  };
  return api;
}
