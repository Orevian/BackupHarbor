/* BackupHarbor front end - navigation and start-up. */
'use strict';

const NAV = [
  ['dashboard', 'nav.dashboard', 'i-dash'],
  ['verify', 'nav.verify', 'i-run'],
  ['targets', 'nav.targets', 'i-target'],
  ['reports', 'nav.reports', 'i-report'],
  ['schedule', 'nav.schedule', 'i-clock'],
  ['settings', 'nav.settings', 'i-gear'],
];

RP.current = '';
let navToken = 0;

function renderChrome() {
  const nav = document.getElementById('nav');
  nav.innerHTML = NAV.map(([id, key, ic]) =>
    '<button type="button" data-nav="' + id + '"' + (RP.current === id ? ' aria-current="page"' : '') + '>' + icon(ic) + '<span>' + esc(t(key)) + '</span>' +
    (id === 'verify' && live.running ? '<span class="badge st-skipped" style="margin-left:auto">' + esc(t('ui.running')) + '</span>' : '') + '</button>').join('');
  $$('[data-nav]', nav).forEach((b) => b.addEventListener('click', () => { RP.reportView = null; navigate(b.dataset.nav); }));
  const demo = document.getElementById('demo-state');
  demo.innerHTML = '<label class="inline"><span class="switch"><input type="checkbox" id="demo-toggle" ' + (RP.boot && RP.boot.demo ? 'checked' : '') + ' ' + (live.running ? 'disabled' : '') + '><span></span></span> ' + esc(t('ui.demo_mode')) + '</label>' +
    (RP.boot && RP.boot.demo ? '<div class="small">' + esc(t('ui.demo_on_note')) + '</div>' : '');
  const toggle = document.getElementById('demo-toggle');
  if (toggle) toggle.addEventListener('change', () => setDemo(toggle.checked));
  document.getElementById('app-version').textContent = 'v' + (RP.boot ? RP.boot.version : '');
  document.title = 'BackupHarbor';
}

function refreshChrome() { renderChrome(); }

async function reloadBootstrap() {
  const b = await api.Bootstrap();
  RP.boot = b;
  RP.catalog = b.catalog;
  RP.lang = b.language;
  document.documentElement.lang = b.language;
  applyTheme(b.theme);
  applyI18n(document);
  renderChrome();
  return b;
}

async function navigate(page) {
  RP.navToken = (RP.navToken || 0) + 1;
  const token = ++navToken;
  RP.current = page;
  renderChrome();
  try {
    if (RP.boot && RP.boot.config_error && page !== 'settings') {
      setPage('<div class="page"><h1 data-i18n="ui.config_problem"></h1>' + banner('bad', '<p>' + esc(RP.boot.config_error) + '</p><p>' + esc(t('ui.config_problem_help')) + '</p>') +
        '<div class="row"><button class="btn" id="cfg-open">' + esc(t('ui.open_config_folder')) + '</button><button class="btn" id="cfg-retry">' + esc(t('ui.retry')) + '</button></div></div>');
      $('#cfg-open').addEventListener('click', () => api.OpenFolder('config').catch((e) => toast(errText(e), 'bad')));
      $('#cfg-retry').addEventListener('click', async () => { await reloadBootstrap(); navigate(page); });
      return;
    }
    await PAGES[page]();
  } catch (e) {
    if (token !== navToken) return;
    setPage('<div class="page"><h1 data-i18n="ui.error"></h1>' + banner('bad', esc(errText(e))) + '</div>');
  }
  if (token === navToken) {
    const main = document.getElementById('main');
    main.scrollTop = 0;
    main.focus({ preventScroll: true });
  }
}

async function start() {
  // Wails injects the bindings shortly after load; wait for them.
  for (let i = 0; i < 100 && !(window.go && window.go.gui && window.go.gui.App); i++) await new Promise((r) => setTimeout(r, 50));
  if (!(window.go && window.go.gui)) {
    document.getElementById('page').textContent = 'The BackupHarbor back end is not available.';
    return;
  }
  const b = await reloadBootstrap();
  const rs = await api.GetRunState();
  if (rs.running) { resetLive(rs.run_id, rs.demo, rs.targets || []); live.started = new Date(rs.started_at).getTime(); (rs.events || []).forEach(applyEvent); live.log = rs.log || []; }
  navigate(rs.running ? 'verify' : (b.first_run && !b.demo ? 'dashboard' : 'dashboard'));
}

// Test hook: lets automated UI checks switch pages deterministically.
window.__rp = { navigate };

document.addEventListener('DOMContentLoaded', start);
