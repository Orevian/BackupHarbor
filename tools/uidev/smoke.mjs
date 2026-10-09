// End-to-end UI smoke test: real front end JavaScript + real Go back end + real restic.
//
//   node tools/uidev/smoke.mjs
//
// Environment: BH_TEST_RESTIC (restic.exe), UIDEV_BIN (built tools/uidev), EDGE (msedge.exe),
// SHOTS_DIR (where screenshots go, default ./docs/screenshots when SHOTS=1).
import { spawn, execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync, existsSync, rmSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { launchBrowser, sleep } from './cdp.mjs';

const restic = process.env.BH_TEST_RESTIC;
const uidev = process.env.UIDEV_BIN;
const edge = process.env.EDGE || 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';
const shots = process.env.SHOTS_DIR || '';
if (!restic || !existsSync(restic)) { console.log('SKIP: set BH_TEST_RESTIC to a restic executable'); process.exit(0); }
if (!uidev || !existsSync(uidev)) { console.error('set UIDEV_BIN to the built tools/uidev executable'); process.exit(2); }

// LAB_ROOT lets screenshots use a neutral path (for example a SUBST drive) instead of the user profile.
const lab = mkdtempSync(join(process.env.LAB_ROOT || tmpdir(), 'rp-ui-'));
const repo = join(lab, 'repo'), src = join(lab, 'src', 'docs');
mkdirSync(src, { recursive: true });
writeFileSync(join(src, 'data.json'), '{"hello":"world"}');
writeFileSync(join(src, 'notes.txt'), 'plain text file');
const renv = { ...process.env, RESTIC_REPOSITORY: repo, RESTIC_PASSWORD: 'ui-lab-pw' };
execFileSync(restic, ['init'], { env: renv, stdio: 'ignore' });
execFileSync(restic, ['backup', join(lab, 'src'), '--tag', 'daily', '--host', 'WORKSTATION'], { env: renv, stdio: 'ignore' });
const cfgPath = join(lab, 'config.yaml');
const y = (p) => "'" + p.replaceAll("'", "''") + "'";
writeFileSync(cfgPath, `version: 1
language: en
theme: light
restic: {path: ${y(restic)}}
temp_dir: ${y(join(lab, 'work'))}
reports: {dir: ${y(join(lab, 'reports'))}}
targets:
  - name: lab-docs
    repository: ${y(repo)}
    password: {source: env, env: UI_LAB_PW}
    rules:
      - {name: json, path: '**/data.json', type: json}
      - {name: text, path: '**/notes.txt', type: text}
`);

const port = 8765;
const server = spawn(uidev, ['-addr', `127.0.0.1:${port}`, '-save', join(lab, 'export'), '-pick-dir', join(lab, 'picked')], {
  env: { ...process.env, BACKUPHARBOR_CONFIG: cfgPath, LOCALAPPDATA: join(lab, 'local'), UI_LAB_PW: 'ui-lab-pw' }, stdio: 'ignore',
});

const results = [];
const check = (name, ok, detail = '') => { results.push({ name, ok }); console.log((ok ? 'PASS ' : 'FAIL ') + name + (ok ? '' : '  ' + detail)); };
const shot = async (b, name, opts) => { if (shots) await b.shot(join(shots, name + '.png'), opts); };

let browser;
try {
  for (let i = 0; i < 50; i++) { try { await fetch(`http://127.0.0.1:${port}/shim.js`); break; } catch { await sleep(100); } }
  browser = await launchBrowser({ exe: edge, width: 1280, height: 820 });
  const b = browser;
  await b.setColorScheme('light');
  await b.goto(`http://127.0.0.1:${port}/`);
  await b.waitFor(`document.querySelectorAll('#nav button').length === 6`, 15000, 'navigation');
  check('navigation renders six entries', true);

  // Dashboard
  await b.waitFor(`!!document.querySelector('table')`);
  check('dashboard lists the configured target', (await b.text('table')).includes('lab-docs'));
  check('never-verified target shows its status as text', (await b.text('table')).toLowerCase().includes('never'));
  await shot(b, '01-dashboard-light');

  // Real verification run from the dashboard
  await b.click('[data-test="lab-docs"]');
  await b.waitFor(`!!document.querySelector('#live-root')`, 10000, 'verify page');
  await b.waitFor(`!!document.querySelector('#view-report')`, 90000, 'run to finish');
  const doneText = await b.text('#live-root');
  check('real run passes end to end', doneText.includes('passed') && !doneText.includes('failed'), doneText.slice(0, 300));
  check('every stage is listed', ['Restic', 'Repository access', 'Integrity check', 'Restore', 'Content rules', 'Cleanup'].every((s) => doneText.includes(s)));
  check('no progress percentage is invented outside the restore', !/\bNaN%/.test(doneText));
  await shot(b, '03-verification-finished');

  // Report detail
  await b.click('#view-report');
  await b.waitFor(`document.body.textContent.includes('What this result proves')`, 8000, 'report detail');
  const rep = await b.text('#page');
  check('report shows snapshot, stages, rules and coverage', ['lab-docs', 'Snapshot', 'Restore duration', 'Content rules', 'json', 'does not prove'].every((s) => rep.includes(s)), rep.slice(0, 200));
  await shot(b, '04-report-detail', { fullPage: true });
  await b.click('[data-export="json"]');
  await b.waitFor(`document.querySelector('.toast')`, 5000, 'export toast');
  check('export reports the written file', (await b.text('.toast')).includes('export'));
  check('exported JSON exists on disk', existsSync(join(lab, 'export.json')));

  // Reports list with filter
  await b.click('[data-nav="reports"]');
  await b.waitFor(`document.querySelectorAll('[data-open]').length === 1`, 8000, 'report row');
  check('history lists the run', true);
  await shot(b, '05-reports');

  // Dashboard now shows the result
  await b.click('[data-nav="dashboard"]');
  await b.waitFor(`document.querySelector('#page h1') && document.querySelector('#page h1').textContent === 'Overview' && document.querySelector('table') && document.querySelector('table').textContent.includes('passed')`, 8000, 'passed card');
  check('dashboard shows the verified result with restore time', (await b.text('table')).includes('lab-docs') && /\d+(\.\d+)? ?(ms|s)/.test(await b.text('table')));
  await shot(b, '02-dashboard-after-run');

  // Target editor: test connection without restoring
  await b.click('[data-nav="targets"]');
  await b.waitFor(`!!document.querySelector('[data-edit]')`);
  await shot(b, '06-targets');
  await b.click('[data-edit="lab-docs"]');
  await b.waitFor(`document.getElementById('dlg').open`);
  await shot(b, '07-target-editor');
  await b.clickText('#dlg-foot button', 'Test connection');
  await b.waitFor(`document.getElementById('te-test').textContent.includes('Test result')`, 30000, 'connection test');
  const tr = await b.text('#te-test');
  check('connection test shows preflight stages only', tr.includes('Snapshot selection') && !tr.includes('Integrity check') && tr.includes('No data is restored'), tr.slice(0, 300));
  await b.eval(`document.getElementById('te-name').value = 'bad name!'`);
  await b.clickText('#dlg-foot button', 'Save');
  await b.waitFor(`document.getElementById('te-msgs').textContent.length > 0`, 5000, 'validation message');
  check('invalid target is rejected with an inline message and stays open', document_open(await b.eval(`document.getElementById('dlg').open`)));
  await b.eval(`document.getElementById('dlg').close()`);

  // Add a second target through the form
  await b.click('#add');
  await b.waitFor(`document.getElementById('dlg').open`);
  await b.set('#te-name', 'second-target');
  await b.set('#te-repo', repo);
  await b.set('#te-pwsrc', 'env');
  await b.set('#te-pwenv', 'UI_LAB_PW');
  await b.clickText('#dlg-foot button', 'Save');
  await b.waitFor(`!document.getElementById('dlg').open`, 8000, 'dialog to close after save');
  await b.waitFor(`document.getElementById('page').textContent.includes('second-target')`);
  check('a new target can be added and appears in the list', true);
  await b.eval(`document.querySelector('[data-enable="second-target"]').click()`);
  await sleep(400);
  check('a target can be disabled', await b.eval(`!document.querySelector('[data-enable="second-target"]').checked`));
  await b.click('[data-del="second-target"]');
  await b.waitFor(`document.getElementById('dlg').open`);
  await b.clickText('#dlg-foot button', 'Delete');
  await b.waitFor(`!document.getElementById('page').textContent.includes('second-target')`, 8000, 'target removal');
  check('a target can be deleted', true);

  // Schedule page loads and reports status honestly
  await b.click('[data-nav="schedule"]');
  await b.waitFor(`document.body.textContent.includes('Scheduled task')`);
  const sch = await b.text('#page');
  check('schedule page shows the real task state without errors', sch.includes('Not installed') && sch.includes('Install scheduled task') && !sch.includes('could not be read'), sch.slice(0, 200));
  await shot(b, '08-schedule');

  // Settings, validation and language switch
  await b.click('[data-nav="settings"]');
  await b.waitFor(`!!document.getElementById('s-lang')`);
  await shot(b, '09-settings', { fullPage: true });
  await b.set('#s-timeout', 'soon');
  await b.eval(`document.getElementById('setf').requestSubmit()`);
  await b.waitFor(`document.getElementById('set-msg').textContent.length > 0`, 5000, 'settings validation');
  check('invalid settings are rejected without saving', (await b.text('#set-msg')).includes('between'));
  await b.set('#s-timeout', '90m'); await b.set('#s-conc', '2');
  await b.set('#s-lang', 'tr');
  await b.eval(`document.getElementById('setf').requestSubmit()`);
  await b.waitFor(`document.querySelector('[data-nav="dashboard"]').textContent.includes('Genel')`, 8000, 'Turkish UI');
  check('language switches to Turkish immediately', true);
  await shot(b, '10-settings-turkish', { fullPage: true });
  await b.click('[data-nav="dashboard"]');
  await b.waitFor(`document.body.textContent.includes('Yedek hedefleri')`);
  check('dashboard is translated', true);
  await shot(b, '11-dashboard-turkish');
  await b.click('[data-nav="settings"]');
  await b.waitFor(`!!document.getElementById('s-lang')`);
  await b.set('#s-lang', 'en'); await b.set('#s-theme', 'dark');
  await b.eval(`document.getElementById('setf').requestSubmit()`);
  await b.waitFor(`document.documentElement.dataset.theme === 'dark'`, 8000, 'dark theme');
  check('dark theme applies', true);
  await b.click('[data-nav="dashboard"]');
  await b.waitFor(`!!document.querySelector('table')`);
  await shot(b, '12-dashboard-dark');
  await b.click('[data-nav="settings"]');
  await b.waitFor(`!!document.getElementById('s-theme')`);
  await b.set('#s-theme', 'light'); await b.eval(`document.getElementById('setf').requestSubmit()`);
  await b.waitFor(`document.documentElement.dataset.theme === 'light'`);

  // Demo mode: clearly separated, never touches real history
  await b.eval(`document.getElementById('demo-toggle').click()`);
  await b.waitFor(`document.body.textContent.includes('Demonstration mode')`, 8000, 'demo banner');
  const demoText = await b.text('#page');
  check('demo mode shows a clear banner and sample targets', demoText.includes('documents-daily') && demoText.includes('photos-weekly') && !demoText.includes('lab-docs'));
  await shot(b, '13-demo-dashboard', { fullPage: true });
  await b.click('[data-nav="verify"]');
  await b.waitFor(`!!document.getElementById('start')`);
  await b.click('#start');
  await b.waitFor(`!!document.querySelector('#view-report')`, 30000, 'demo run');
  check('simulated run finishes and is marked as demo', (await b.text('#live-root')).includes('Demonstration mode'));
  await shot(b, '14-demo-run');
  await b.click('[data-nav="reports"]');
  await b.waitFor(`document.querySelectorAll('[data-open]').length >= 3`);
  await b.click('[data-open]');
  await b.waitFor(`document.body.textContent.includes('What this result proves')`);
  await shot(b, '15-demo-report', { fullPage: true });
  await b.eval(`document.getElementById('demo-toggle').click()`);
  await b.waitFor(`!document.body.textContent.includes('Demonstration mode')`, 8000, 'demo off');
  await b.click('[data-nav="reports"]');
  await b.waitFor(`document.querySelectorAll('[data-open]').length === 1`);
  check('leaving demo mode restores only the real history', true);

  // Small window and 150% Windows scaling: nothing may overflow horizontally.
  await b.send('Emulation.setDeviceMetricsOverride', { width: 900, height: 700, deviceScaleFactor: 1.5, mobile: false });
  for (const page of ['dashboard', 'verify', 'targets', 'reports', 'schedule', 'settings']) {
    await b.click(`[data-nav="${page}"]`);
    await sleep(700);
    const overflow = await b.eval(`(() => { const m = document.getElementById('main'); return Math.max(document.documentElement.scrollWidth - window.innerWidth, m.scrollWidth - m.clientWidth); })()`);
    check(`${page} page fits a 900px window at 150% scaling`, overflow <= 1, `overflow ${overflow}px`);
  }
  await b.click('[data-nav="targets"]');
  await b.waitFor(`!!document.querySelector('[data-edit]')`);
  await b.click('[data-edit="lab-docs"]');
  await b.waitFor(`document.getElementById('dlg').open`);
  check('the target editor dialog fits a 900px window at 150% scaling', await b.eval(`(() => { const d = document.getElementById('dlg-body'); return d.scrollWidth <= d.clientWidth + 1; })()`));
  await shot(b, '16-small-window-editor');
  await b.eval(`document.getElementById('dlg').close()`);
  await b.send('Emulation.clearDeviceMetricsOverride');

  // Accessibility basics
  check('every nav button is keyboard reachable', await b.eval(`Array.from(document.querySelectorAll('#nav button')).every((x) => x.tabIndex >= 0)`));
  check('current page is exposed with aria-current', await b.eval(`document.querySelectorAll('[aria-current="page"]').length === 1`));
  check('status badges carry text, not colour alone', await b.eval(`Array.from(document.querySelectorAll('.badge')).every((x) => x.textContent.trim().length > 0)`));
  check('no stray script errors in the console', b.logs.length === 0, b.logs.join(' | '));
} catch (e) {
  check('smoke test ran to completion', false, String(e.stack || e));
  if (browser) {
    console.log('console:', browser.logs.join(' | '));
    try { console.log('page text:', (await browser.eval(`document.getElementById('page').textContent`)).slice(0, 600)); } catch { /* ignore */ }
  }
} finally {
  if (browser) browser.close();
  server.kill();
  await sleep(300);
  try { rmSync(lab, { recursive: true, force: true }); } catch { /* ignore */ }
}
function document_open(v) { return v === true; }
const failed = results.filter((r) => !r.ok).length;
console.log(`\n${results.length - failed}/${results.length} checks passed`);
process.exit(failed ? 1 : 0);
