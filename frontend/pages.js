/* BackupHarbor front end - pages. */
'use strict';

const STAGES = ['restic', 'directories', 'password', 'repository', 'snapshot', 'check', 'restore', 'content', 'cleanup'];

const PAGES = {};

/* =========================================================================
   Dashboard
   ========================================================================= */
PAGES.dashboard = async function () {
  const setPage = pageWriter();
  const [d, boot] = await Promise.all([api.GetDashboard(), api.Bootstrap()]);
  RP.boot = boot;
  const needsSetup = !d.demo && (!boot.restic.found || !boot.config_exists || d.targets.length === 0);
  let html = '<div class="page">';
  html += '<div class="page-head"><div><h1 data-i18n="nav.dashboard"></h1><p class="muted" data-i18n="ui.dashboard_sub"></p></div><div class="actions">';
  html += '<button class="btn primary" id="run-all" ' + (d.targets.length === 0 ? 'disabled' : '') + '>' + icon('i-run') + esc(t('ui.test_all')) + '</button></div></div>';
  if (d.demo) html += banner('warn', '<strong>' + esc(t('ui.demo_banner_title')) + '</strong> ' + esc(t('ui.demo_banner_text')));
  if (needsSetup) html += onboarding(d, boot);

  const c = d.counts || {};
  const tile = (key, n, st) => '<div class="card tile"><span class="muted">' + esc(t(key)) + '</span><span class="num">' + n + '</span>' + (st ? badge(st) : '') + '</div>';
  html += '<div class="grid cols-4">' +
    tile('ui.tile_passed', c.passed || 0, 'passed') +
    tile('ui.tile_failed', (c.failed || 0) + (c.partial || 0), 'failed') +
    tile('ui.tile_untestable', (c.not_run || 0) + (c.cancelled || 0) + (c.timed_out || 0), 'not_run') +
    tile('ui.tile_never', c.never || 0, 'never') + '</div>';

  html += '<div class="card"><div class="row between"><h2 data-i18n="ui.targets"></h2>';
  html += '<button class="btn small" data-go="targets">' + esc(t('ui.manage_targets')) + '</button></div>';
  if (d.targets.length === 0) {
    html += '<div class="empty">' + icon('i-target') + '<h3 data-i18n="ui.no_targets_title"></h3><p class="muted" data-i18n="ui.no_targets_text"></p>' +
      '<button class="btn primary" id="add-first">' + icon('i-plus') + esc(t('ui.add_first_target')) + '</button></div>';
  } else {
    html += '<div class="table-wrap"><table><thead><tr><th scope="col">' + esc(t('rpt.target')) + '</th><th scope="col">' + esc(t('rpt.status')) + '</th><th scope="col">' + esc(t('ui.last_verified')) +
      '</th><th scope="col">' + esc(t('rpt.snapshot')) + '</th><th scope="col">' + esc(t('rpt.restore_time')) + '</th><th scope="col">' + esc(t('rpt.files') + ' / ' + t('rpt.size')) + '</th><th scope="col"><span class="sr-only">' + esc(t('ui.actions')) + '</span></th></tr></thead><tbody>';
    d.targets.forEach((tg) => {
      html += '<tr><td><strong>' + esc(tg.name) + '</strong>' + (tg.enabled ? '' : ' <span class="badge st-skipped">' + esc(t('ui.disabled')) + '</span>') +
        '<div class="muted small mono">' + esc(tg.repository) + '</div></td>' +
        '<td>' + badge(tg.status) + '</td>' +
        '<td>' + (tg.last_verified_at ? '<span title="' + esc(fmtDate(tg.last_verified_at)) + '">' + esc(fmtAgo(tg.last_verified_at)) + '</span><div class="muted small">' + esc(fmtDate(tg.last_verified_at)) + '</div>' : esc(t('ui.never'))) + '</td>' +
        '<td>' + (tg.snapshot_id ? '<span class="mono">' + esc(tg.snapshot_id) + '</span><div class="muted small">' + esc(fmtDate(tg.snapshot_time)) + '</div>' : '-') + '</td>' +
        '<td>' + (tg.restore_duration_ms ? esc(fmtDur(tg.restore_duration_ms)) : '-') + '</td>' +
        '<td>' + (tg.files ? esc(fmtNum(tg.files)) + ' / ' + esc(fmtBytes(tg.bytes)) : '-') + '</td>' +
        '<td class="row"><button class="btn small" data-test="' + esc(tg.name) + '" ' + ((tg.enabled || d.demo) ? '' : 'disabled') + '>' + esc(t('ui.test_now')) + '</button>' +
        (tg.status !== 'never' ? '<button class="btn small" data-last="' + esc(tg.name) + '">' + esc(t('ui.last_report')) + '</button>' : '') + '</td></tr>';
    });
    html += '</tbody></table></div>';
  }
  html += '</div>';

  html += '<div class="card"><div class="row between"><h2 data-i18n="ui.recent_runs"></h2><button class="btn small" data-go="reports">' + esc(t('ui.all_reports')) + '</button></div>';
  if (!d.recent || d.recent.length === 0) html += '<p class="muted">' + esc(t('ui.no_reports_yet')) + '</p>';
  else {
    html += '<div class="table-wrap"><table><thead><tr><th scope="col">' + esc(t('rpt.started')) + '</th><th scope="col">' + esc(t('rpt.status')) + '</th><th scope="col">' + esc(t('rpt.targets')) + '</th><th scope="col">' + esc(t('rpt.duration')) + '</th></tr></thead><tbody>';
    d.recent.forEach((r) => {
      html += '<tr class="clickable" tabindex="0" role="link" data-report="' + esc(r.run_id) + '"><td>' + esc(fmtDate(r.started_at)) + '</td><td>' + badge(r.status) + '</td><td>' +
        esc((r.targets || []).map((x) => x.name).join(', ')) + '</td><td>' + esc(fmtDur(r.duration_ms)) + '</td></tr>';
    });
    html += '</tbody></table></div>';
  }
  html += '</div></div>';
  const p = setPage(html);

  $('#run-all', p).addEventListener('click', () => startVerification([]));
  $$('[data-test]', p).forEach((b) => b.addEventListener('click', () => startVerification([b.dataset.test])));
  $$('[data-last]', p).forEach((b) => b.addEventListener('click', () => openLastReportFor(b.dataset.last)));
  $$('[data-go]', p).forEach((b) => b.addEventListener('click', () => navigate(b.dataset.go)));
  $$('[data-report]', p).forEach((r) => {
    const open = () => { RP.reportView = r.dataset.report; navigate('reports'); };
    r.addEventListener('click', open);
    r.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(); } });
  });
  const af = $('#add-first', p); if (af) af.addEventListener('click', () => editTarget(null));
  wireOnboarding(p, boot);
};

function onboarding(d, boot) {
  const step = (done, title, body, action) => '<li class="row between" style="align-items:flex-start"><div><strong>' + (done ? icon('i-check') + ' ' : '') + esc(title) + '</strong><div class="muted small">' + body + '</div></div>' + (action || '') + '</li>';
  let h = '<div class="card"><h2 data-i18n="ui.setup_title"></h2><p class="muted" data-i18n="ui.setup_text"></p><ul class="plain">';
  h += step(boot.restic.found, t('ui.setup_restic'),
    boot.restic.found ? esc(t('restic.ok', boot.restic.version)) + ' &middot; <span class="mono">' + esc(boot.restic.path) + '</span>' :
      esc(boot.restic.error || t('ui.setup_restic_missing')) + '<br>' + esc(t('ui.setup_restic_help')),
    boot.restic.found ? '' : '<button class="btn small" id="ob-restic">' + esc(t('ui.browse_restic')) + '</button>');
  h += step(boot.config_exists, t('ui.setup_config'),
    boot.config_exists ? '<span class="mono">' + esc(boot.config_path) + '</span>' : esc(t('ui.setup_config_missing')) + ' <span class="mono">' + esc(boot.config_path) + '</span>',
    boot.config_exists ? '' : '<button class="btn small" id="ob-config">' + esc(t('ui.create_config')) + '</button>');
  h += step(d.targets.length > 0, t('ui.setup_target'), esc(t('ui.setup_target_text')),
    d.targets.length > 0 ? '' : '<button class="btn small primary" id="ob-target">' + esc(t('ui.add_first_target')) + '</button>');
  h += '</ul><div class="row" style="margin-top:.75rem"><button class="btn" id="ob-demo">' + esc(t('ui.try_demo')) + '</button><span class="muted small">' + esc(t('ui.try_demo_text')) + '</span></div></div>';
  return h;
}

function wireOnboarding(p, boot) {
  const el = (id) => $(id, p);
  if (el('#ob-restic')) el('#ob-restic').addEventListener('click', async () => {
    try {
      const f = await api.BrowseFile(t('ui.browse_restic'), '*.exe');
      if (!f) return;
      const s = await api.GetSettings(); s.restic_path = f;
      const r = await api.SaveSettings(s);
      if (!r.saved) { toast((r.messages || []).join('\n'), 'bad'); return; }
      toast(t('ui.saved'), 'ok'); navigate('dashboard');
    } catch (e) { toast(errText(e), 'bad'); }
  });
  if (el('#ob-config')) el('#ob-config').addEventListener('click', async () => {
    try { await api.InitConfig(); toast(t('ui.config_created'), 'ok'); navigate('dashboard'); } catch (e) { toast(errText(e), 'bad'); }
  });
  if (el('#ob-target')) el('#ob-target').addEventListener('click', () => editTarget(null));
  if (el('#ob-demo')) el('#ob-demo').addEventListener('click', () => setDemo(true));
}

async function openLastReportFor(name) {
  try {
    const list = await api.ListReports({ target: name, limit: 1 });
    if (!list.length) { toast(t('cli.report_none'), 'info'); return; }
    RP.reportView = list[0].run_id; navigate('reports');
  } catch (e) { toast(errText(e), 'bad'); }
}

async function setDemo(on) {
  try {
    await api.SetDemo(on);
    RP.boot.demo = on;
    // A finished run belongs to the other mode; never show real results in demo mode or vice versa.
    if (!live.running) { live.done = null; live.order = []; live.targets = {}; }
    RP.reportView = null; RP.reportFilter = null;
    renderChrome(); navigate('dashboard');
  } catch (e) { toast(errText(e), 'bad'); renderChrome(); }
}

/* =========================================================================
   Verification (run) page with live progress
   ========================================================================= */
const live = { running: false, runId: '', demo: false, started: 0, targets: {}, order: [], log: [], done: null, timer: null, raf: false };

function resetLive(runId, demo, targets) {
  live.running = true; live.runId = runId; live.demo = demo; live.started = Date.now(); live.done = null; live.log = [];
  live.targets = {}; live.order = targets.slice();
  targets.forEach((n) => { live.targets[n] = newTargetState(); });
  startTimer();
}
function newTargetState() { return { status: '', stages: {}, percent: -1, files: 0, filesTotal: 0, bytes: 0, bytesTotal: 0, message: '' }; }
function tstate(name) { if (!live.targets[name]) { live.targets[name] = newTargetState(); if (!live.order.includes(name)) live.order.push(name); } return live.targets[name]; }

function applyEvent(ev) {
  if (!ev.target) return;
  const ts = tstate(ev.target);
  switch (ev.type) {
    case 'target_start': ts.status = 'running'; break;
    case 'stage_start': ts.stages[ev.stage] = { status: 'running', message: '' }; ts.current = ev.stage; ts.percent = -1; break;
    case 'stage_end': ts.stages[ev.stage] = { status: ev.status, message: msg(ev.message) }; if (ts.current === ev.stage) ts.percent = -1; break;
    case 'progress':
      ts.current = ev.stage;
      if (ev.percent >= 0) { ts.percent = ev.percent; ts.files = ev.files_done; ts.filesTotal = ev.files_total; ts.bytes = ev.bytes_done; ts.bytesTotal = ev.bytes_total; }
      if (ev.message && ev.message.code) ts.message = msg(ev.message);
      break;
    case 'target_end': ts.status = ev.status; break;
  }
}

function startTimer() {
  stopTimer();
  live.timer = setInterval(() => { const e = $('#elapsed'); if (e) e.textContent = fmtDur(Date.now() - live.started); }, 1000);
}
function stopTimer() { if (live.timer) { clearInterval(live.timer); live.timer = null; } }

function scheduleLiveRender() {
  if (live.raf) return;
  live.raf = true;
  requestAnimationFrame(() => { live.raf = false; if (RP.current === 'verify') renderLive(); });
}

onEvent('verify:event', (ev) => {
  if (ev.type === 'run_start' && !live.running) { /* started elsewhere (e.g. scheduled) */ }
  applyEvent(ev);
  if (ev.type === 'stage_end' && ev.target) announce(ev.target + ': ' + t('stage.' + ev.stage) + ' - ' + t('status.' + ev.status));
  scheduleLiveRender();
});
onEvent('verify:log', (l) => { live.log.push(l); if (live.log.length > 500) live.log.shift(); const box = $('#logbox'); if (box && RP.current === 'verify') appendLogLine(box, l); });
onEvent('verify:done', (d) => {
  live.running = false; live.done = d; stopTimer();
  announce(d.error ? d.error : t('status.' + (d.run ? d.run.status : 'failed')));
  if (RP.current === 'verify') renderLive(); else toast(d.error ? d.error : t('ui.run_finished') + ': ' + t('status.' + (d.run ? d.run.status : 'failed')), d.run && d.run.status === 'passed' ? 'ok' : 'info');
  refreshChrome();
});

async function startVerification(targets) {
  try {
    const sel = targets && targets.length ? targets : [];
    const id = await api.StartRun(sel);
    const st = await api.GetRunState();
    resetLive(id, st.demo, st.targets || sel);
    navigate('verify');
  } catch (e) { toast(errText(e), 'bad'); }
}

function appendLogLine(box, l) {
  const show = $('#log-debug') && $('#log-debug').checked;
  if (l.level === 'debug' && !show) return;
  const d = document.createElement('div');
  d.className = 'lv-' + l.level;
  d.textContent = new Date(l.time).toLocaleTimeString() + ' ' + l.level.padEnd(5) + ' ' + l.line;
  box.appendChild(d);
  box.scrollTop = box.scrollHeight;
}

PAGES.verify = async function () {
  const setPage = pageWriter();
  const st = await api.GetRunState();
  if (st.running && !live.running) {
    // The window was reloaded or opened mid-run: rebuild from the backend replay buffer.
    resetLive(st.run_id, st.demo, st.targets || []);
    live.started = new Date(st.started_at).getTime();
    (st.events || []).forEach(applyEvent);
    live.log = st.log || [];
  }
  if (!live.running && !live.done) {
    const view = await api.ListTargets();
    renderIdle(view, setPage);
    return;
  }
  setPage('<div class="page" id="live-root"></div>');
  renderLive();
};

async function renderIdle(view, setPage) {
  const dem = RP.boot.demo;
  let html = '<div class="page"><div class="page-head"><div><h1 data-i18n="nav.verify"></h1><p class="muted" data-i18n="ui.verify_sub"></p></div></div>';
  if (dem) html += banner('warn', '<strong>' + esc(t('ui.demo_banner_title')) + '</strong> ' + esc(t('ui.demo_banner_run')));
  html += banner('info', esc(t('ui.verify_explain')));
  if (dem) {
    html += '<div class="card"><p>' + esc(t('ui.demo_run_text')) + '</p><button class="btn primary" id="start">' + icon('i-run') + esc(t('ui.start')) + '</button></div>';
  } else if (view.targets.length === 0) {
    html += '<div class="card empty">' + icon('i-target') + '<h3 data-i18n="ui.no_targets_title"></h3><button class="btn primary" id="add">' + esc(t('ui.add_first_target')) + '</button></div>';
  } else {
    html += '<div class="card"><h2 data-i18n="ui.choose_targets"></h2><ul class="plain" style="margin-top:.75rem">';
    view.targets.forEach((tg, i) => {
      const enabled = tg.enabled !== false;
      html += '<li><label class="inline"><input type="checkbox" data-sel="' + esc(tg.name) + '" ' + (enabled ? 'checked' : '') + '> <strong>' + esc(tg.name) + '</strong>' +
        (enabled ? '' : ' <span class="badge st-skipped">' + esc(t('ui.disabled')) + '</span>') + '</label>' +
        '<div class="muted small" style="margin-left:1.6rem">' + esc(scopeText(tg)) + '</div></li>';
    });
    html += '</ul><div class="row" style="margin-top:1rem"><button class="btn primary" id="start">' + icon('i-run') + esc(t('ui.start')) + '</button></div></div>';
  }
  html += '</div>';
  const p = setPage(html);
  const s = $('#start', p);
  if (s) s.addEventListener('click', () => {
    const sel = $$('[data-sel]', p).filter((c) => c.checked).map((c) => c.dataset.sel);
    if (!dem && sel.length === 0) { toast(t('ui.select_one'), 'bad'); return; }
    startVerification(dem ? [] : sel);
  });
  const a = $('#add', p); if (a) a.addEventListener('click', () => editTarget(null));
}

// Plain-language cost and coverage of one target's configuration.
function scopeText(tg) {
  const parts = [];
  if (tg.check && tg.check.enabled !== false) parts.push(t('ui.scope_check_' + (tg.check.read_data || 'none'), tg.check.subset || ''));
  else parts.push(t('ui.scope_check_off'));
  if (tg.restore && tg.restore.enabled !== false) {
    parts.push((tg.restore.include && tg.restore.include.length) ? t('ui.scope_restore_filtered', tg.restore.include.length) : t('ui.scope_restore_full'));
    if (tg.restore.verify !== false) parts.push(t('ui.scope_verify'));
  } else parts.push(t('ui.scope_restore_off'));
  if (tg.rules && tg.rules.length) parts.push(t('ui.scope_rules', tg.rules.length));
  return parts.join(' \u00b7 ');
}

function renderLive() {
  const root = $('#live-root');
  if (!root) return;
  const finished = Object.values(live.targets).filter((x) => x.status && x.status !== 'running').length;
  const total = live.order.length;
  let html = '<div class="page-head"><div><h1 data-i18n="nav.verify"></h1><p class="muted"><span class="mono">' + esc(live.runId) + '</span></p></div><div class="actions">';
  if (live.running) html += '<button class="btn danger" id="cancel">' + icon('i-stop') + esc(t('ui.cancel_run')) + '</button>';
  else html += '<button class="btn" id="again">' + esc(t('ui.new_run')) + '</button>';
  html += '</div></div>';
  if (live.demo) html += banner('warn', '<strong>' + esc(t('ui.demo_banner_title')) + '</strong> ' + esc(t('ui.demo_banner_run')));

  html += '<div class="card"><div class="row between"><div class="row"><strong>' + esc(live.running ? t('ui.running') : t('ui.finished')) + '</strong>' +
    '<span class="muted">' + esc(t('ui.elapsed')) + ': <span id="elapsed">' + esc(fmtDur((live.done ? (live.done.run ? live.done.run.duration_ms : Date.now() - live.started) : Date.now() - live.started))) + '</span></span></div>' +
    '<span class="muted">' + esc(t('ui.targets_done', finished, total)) + '</span></div></div>';

  if (live.done && live.done.error) html += banner('bad', '<strong>' + esc(t('ui.run_error')) + '</strong> ' + esc(live.done.error));
  if (live.done && live.done.save_error) html += banner('bad', '<strong>' + esc(t('ui.save_error')) + '</strong> ' + esc(live.done.save_error));
  if (live.done && live.done.run && !live.done.demo && (live.done.saved || []).length) html += banner('ok', esc(t('cli.report_saved')) + ' <span class="mono">' + esc(live.done.saved[0]) + '</span>');

  live.order.forEach((name) => {
    const ts = live.targets[name];
    const final = live.done && live.done.run ? live.done.run.targets.find((x) => x.name === name) : null;
    const status = final ? final.status : (ts.status || 'running');
    html += '<div class="card"><div class="row between"><h2>' + esc(name) + '</h2>' + (status === 'running' || !status ? '<span class="badge st-skipped">' + esc(t('ui.running')) + '</span>' : badge(status)) + '</div><ul class="stage-list" style="margin-top:.75rem">';
    STAGES.forEach((id) => {
      const s = ts.stages[id];
      const fs = final ? final.stages.find((x) => x.id === id) : null;
      const sstatus = fs ? fs.status : (s ? s.status : '');
      if (!sstatus && !(live.running && ts.current === id)) {
        // pending stage: only show the first ones while running to keep the list compact
        html += '<li class="muted"><span class="dot"></span><span>' + esc(t('stage.' + id)) + '</span><span></span><span></span></li>';
        return;
      }
      const m = fs ? msg(fs.message) : (s ? s.message : '');
      html += '<li class="' + esc(sstatus) + '"><span class="dot"></span><span>' + esc(t('stage.' + id)) + '</span><span class="small">' + esc(m) +
        (id === 'restore' && sstatus === 'running' ? restoreProgress(ts) : '') + '</span>' +
        '<span>' + (sstatus && sstatus !== 'running' ? badge(sstatus) : '') + '</span></li>';
    });
    html += '</ul>';
    if (final) {
      (final.stages || []).forEach((s) => {
        if (s.detail && s.status !== 'passed') html += '<details><summary>' + esc(t('rpt.details') + ': ' + t('stage.' + s.id)) + '</summary><pre class="detail">' + esc(s.detail) + '</pre></details>';
      });
    }
    html += '</div>';
  });

  if (live.done && live.done.run) {
    html += '<div class="row"><button class="btn primary" id="view-report">' + esc(live.done.demo ? t('ui.view_demo_report') : t('ui.view_report')) + '</button></div>';
    if (live.done.run.status === 'passed') html += '<p class="muted small">' + esc(t('cli.passed_notice')) + '</p>';
  }

  html += '<details class="card" id="logwrap"><summary><strong>' + esc(t('ui.log')) + '</strong></summary><div class="row" style="margin:.5rem 0"><label class="inline"><input type="checkbox" id="log-debug"> ' + esc(t('ui.show_debug')) + '</label></div><div class="log" id="logbox" tabindex="0" role="log" aria-label="' + esc(t('ui.log')) + '"></div></details>';
  const keepOpen = $('#logwrap') && $('#logwrap').open;
  const dbg = $('#log-debug') && $('#log-debug').checked;
  root.innerHTML = html;
  applyI18n(root);
  const lw = $('#logwrap'); if (keepOpen) lw.open = true;
  const box = $('#logbox');
  if (dbg) $('#log-debug').checked = true;
  live.log.forEach((l) => appendLogLine(box, l));
  $('#log-debug').addEventListener('change', () => { box.innerHTML = ''; live.log.forEach((l) => appendLogLine(box, l)); });
  const c = $('#cancel'); if (c) c.addEventListener('click', async () => { c.disabled = true; await api.CancelRun(); toast(t('ui.cancelling'), 'info'); });
  const ag = $('#again'); if (ag) ag.addEventListener('click', () => { live.done = null; live.running = false; navigate('verify'); });
  const vr = $('#view-report'); if (vr) vr.addEventListener('click', () => { RP.reportView = live.done.run.run_id; navigate('reports'); });
}

function restoreProgress(ts) {
  if (ts.percent < 0) return '';
  return ' <div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="' + Math.round(ts.percent) + '" aria-label="' + esc(t('stage.restore')) + '"><i data-w="' + ts.percent.toFixed(1) + '"></i></div>' +
    '<span class="small muted">' + esc(Math.round(ts.percent) + '% \u00b7 ' + fmtNum(ts.files) + ' / ' + fmtNum(ts.filesTotal) + ' ' + t('rpt.files') + ' \u00b7 ' + fmtBytes(ts.bytes) + ' / ' + fmtBytes(ts.bytesTotal)) + '</span>';
}

// CSP forbids inline style attributes, so bar widths are applied through the DOM.
new MutationObserver(() => { $$('.bar > i[data-w]').forEach((i) => { i.style.width = i.dataset.w + '%'; }); })
  .observe(document.documentElement, { childList: true, subtree: true });

/* =========================================================================
   Targets
   ========================================================================= */
PAGES.targets = async function () {
  const setPage = pageWriter();
  const v = await api.ListTargets();
  let html = '<div class="page"><div class="page-head"><div><h1 data-i18n="nav.targets"></h1><p class="muted" data-i18n="ui.targets_sub"></p></div><div class="actions"><button class="btn primary" id="add">' + icon('i-plus') + esc(t('ui.add_target')) + '</button></div></div>';
  if (RP.boot.demo) html += banner('warn', esc(t('ui.demo_targets_note')));
  html += '<p class="muted small">' + esc(t('ui.config_file')) + ' <span class="mono">' + esc(v.config_path) + '</span></p>';
  if (v.targets.length === 0) {
    html += '<div class="card empty">' + icon('i-target') + '<h3 data-i18n="ui.no_targets_title"></h3><p class="muted" data-i18n="ui.no_targets_text"></p></div>';
  } else {
    html += '<div class="card"><div class="table-wrap"><table><thead><tr><th scope="col">' + esc(t('rpt.target')) + '</th><th scope="col">' + esc(t('ui.enabled')) + '</th><th scope="col">' + esc(t('ui.scope')) + '</th><th scope="col"><span class="sr-only">' + esc(t('ui.actions')) + '</span></th></tr></thead><tbody>';
    v.targets.forEach((tg) => {
      const pwNote = tg.password.source === 'credential' ? (v.has_password[tg.name] ? t('ui.pw_stored') : t('ui.pw_missing')) : t('ui.pw_source_' + tg.password.source);
      html += '<tr><td><strong>' + esc(tg.name) + '</strong><div class="muted small mono">' + esc(tg.repository) + '</div><div class="small ' + (tg.password.source === 'credential' && !v.has_password[tg.name] ? '' : 'muted') + '">' + icon('i-info').replace('<svg', '<svg width="12" height="12"') + ' ' + esc(pwNote) + '</div></td>' +
        '<td><label class="switch"><input type="checkbox" data-enable="' + esc(tg.name) + '" ' + (tg.enabled !== false ? 'checked' : '') + ' aria-label="' + esc(t('ui.enabled') + ': ' + tg.name) + '"><span></span></label></td>' +
        '<td class="small">' + esc(scopeText(tg)) + '</td>' +
        '<td class="row"><button class="btn small" data-edit="' + esc(tg.name) + '">' + esc(t('ui.edit')) + '</button><button class="btn small" data-results="' + esc(tg.name) + '">' + esc(t('ui.results')) + '</button><button class="btn small danger" data-del="' + esc(tg.name) + '">' + esc(t('ui.delete')) + '</button></td></tr>';
    });
    html += '</tbody></table></div></div>';
  }
  html += '</div>';
  const p = setPage(html);
  $('#add', p).addEventListener('click', () => editTarget(null));
  $$('[data-edit]', p).forEach((b) => b.addEventListener('click', () => editTarget(v.targets.find((x) => x.name === b.dataset.edit))));
  $$('[data-results]', p).forEach((b) => b.addEventListener('click', () => openLastReportFor(b.dataset.results)));
  $$('[data-del]', p).forEach((b) => b.addEventListener('click', async () => {
    const tg = v.targets.find((x) => x.name === b.dataset.del);
    const ok = await confirmDialog(t('ui.delete_target'), t('ui.delete_target_text', tg.name), t('ui.delete'), true);
    if (!ok) return;
    try { await api.DeleteTarget(tg.name, tg.password.source === 'credential'); toast(t('ui.deleted'), 'ok'); navigate('targets'); } catch (e) { toast(errText(e), 'bad'); }
  }));
  $$('[data-enable]', p).forEach((c) => c.addEventListener('change', async () => {
    try { await api.SetTargetEnabled(c.dataset.enable, c.checked); toast(t('ui.saved'), 'ok'); } catch (e) { c.checked = !c.checked; toast(errText(e), 'bad'); }
  }));
};

function ruleRowHTML(r) {
  r = r || { name: '', path: '', type: 'json', sha256: '', required: true, max_files: 0 };
  const types = ['exists', 'non_empty', 'text', 'json', 'xml', 'zip', 'pdf', 'sqlite', 'sha256'];
  return '<div class="rule-row" data-rule>' +
    '<label>' + esc(t('ui.rule_name')) + '<input type="text" data-r="name" value="' + esc(r.name) + '"></label>' +
    '<label>' + esc(t('ui.rule_path')) + '<input type="text" data-r="path" value="' + esc(r.path) + '" placeholder="**/data.json"></label>' +
    '<label>' + esc(t('ui.rule_type')) + '<select data-r="type">' + types.map((x) => '<option value="' + x + '"' + (x === r.type ? ' selected' : '') + '>' + esc(t('ui.rt_' + x)) + '</option>').join('') + '</select></label>' +
    '<label>SHA-256<input type="text" data-r="sha256" value="' + esc(r.sha256) + '" ' + (r.type === 'sha256' ? '' : 'disabled') + '></label>' +
    '<label class="inline"><input type="checkbox" data-r="required" ' + (r.required !== false ? 'checked' : '') + '> ' + esc(t('ui.rule_required')) + '</label>' +
    '<button type="button" class="btn small danger" data-rm aria-label="' + esc(t('ui.remove')) + '">' + icon('i-x') + '</button></div>';
}

function targetFormHTML(tg, isNew, hasPw) {
  const sel = (id, val, opts) => '<select id="' + id + '">' + opts.map(([v, l]) => '<option value="' + v + '"' + (v === val ? ' selected' : '') + '>' + esc(l) + '</option>').join('') + '</select>';
  const timeoutVal = tg.timeout && tg.timeout !== '0s' ? tg.timeout : '';
  return '<div id="te-msgs"></div>' +
    '<div class="grid cols-2"><label>' + esc(t('ui.t_name')) + '<input type="text" id="te-name" value="' + esc(tg.name) + '" autocomplete="off"><span class="hint">' + esc(t('ui.t_name_hint')) + '</span></label>' +
    '<label class="inline" style="align-self:center"><span class="switch"><input type="checkbox" id="te-enabled" ' + (tg.enabled !== false ? 'checked' : '') + '><span></span></span> ' + esc(t('ui.enabled')) + '</label></div>' +
    '<label>' + esc(t('rpt.repository')) + '<div class="field-row"><input type="text" id="te-repo" value="' + esc(tg.repository) + '" placeholder="D:\\Backups\\restic"><button type="button" class="btn" id="te-browse-repo">' + icon('i-folder') + esc(t('ui.browse')) + '</button></div><span class="hint">' + esc(t('ui.t_repo_hint')) + '</span></label>' +
    '<fieldset><legend>' + esc(t('ui.t_password')) + '</legend><label>' + esc(t('ui.t_pw_source')) + sel('te-pwsrc', tg.password.source, [['credential', t('ui.pw_source_credential')], ['env', t('ui.pw_source_env')], ['file', t('ui.pw_source_file')]]) + '</label>' +
    '<div id="pw-credential"><label>' + esc(t('ui.t_pw_value')) + '<input type="password" id="te-pw" autocomplete="new-password" placeholder="' + esc(hasPw ? t('ui.pw_keep') : '') + '"></label><span class="hint">' + esc(t('ui.t_pw_hint')) + '</span>' +
    '<label>' + esc(t('ui.t_pw_credname')) + '<input type="text" id="te-credname" value="' + esc(tg.password.credential || '') + '"></label></div>' +
    '<div id="pw-env"><label>' + esc(t('ui.t_pw_env')) + '<input type="text" id="te-pwenv" value="' + esc(tg.password.env || '') + '" placeholder="RESTIC_PASSWORD_DOCS"></label><span class="hint">' + esc(t('ui.t_pw_env_hint')) + '</span></div>' +
    '<div id="pw-file"><label>' + esc(t('ui.t_pw_file')) + '<div class="field-row"><input type="text" id="te-pwfile" value="' + esc(tg.password.file || '') + '"><button type="button" class="btn" id="te-browse-pw">' + icon('i-folder') + esc(t('ui.browse')) + '</button></div></label><span class="hint">' + esc(t('ui.t_pw_file_hint')) + '</span></div></fieldset>' +
    '<fieldset><legend>' + esc(t('ui.t_snapshot')) + '</legend><div class="grid cols-2"><label>' + esc(t('ui.t_snap_select')) + sel('te-snsel', tg.snapshot.select, [['latest', t('ui.snap_latest')], ['tag', t('ui.snap_tag')], ['id', t('ui.snap_id')]]) + '</label>' +
    '<label>' + esc(t('ui.t_snap_host')) + '<input type="text" id="te-snhost" value="' + esc(tg.snapshot.host || '') + '"></label></div>' +
    '<label id="sn-tags">' + esc(t('ui.t_snap_tags')) + '<input type="text" id="te-sntags" value="' + esc((tg.snapshot.tags || []).join(', ')) + '"><span class="hint">' + esc(t('ui.t_snap_tags_hint')) + '</span></label>' +
    '<label id="sn-id">' + esc(t('ui.t_snap_id')) + '<input type="text" id="te-snid" value="' + esc(tg.snapshot.id || '') + '"></label></fieldset>' +
    '<fieldset><legend>' + esc(t('ui.t_check')) + '</legend><label class="inline"><span class="switch"><input type="checkbox" id="te-check" ' + (tg.check.enabled !== false ? 'checked' : '') + '><span></span></span> ' + esc(t('ui.t_check_enable')) + '</label>' +
    '<div class="grid cols-2"><label>' + esc(t('ui.t_readdata')) + sel('te-rd', tg.check.read_data, [['none', t('ui.rd_none')], ['subset', t('ui.rd_subset')], ['all', t('ui.rd_all')]]) + '</label>' +
    '<label id="rd-subset">' + esc(t('ui.t_subset')) + '<input type="text" id="te-subset" value="' + esc(tg.check.subset || '') + '" placeholder="5%"></label></div>' +
    '<div class="banner info" id="rd-cost">' + icon('i-info') + '<div></div></div></fieldset>' +
    '<fieldset><legend>' + esc(t('ui.t_restore')) + '</legend><label class="inline"><span class="switch"><input type="checkbox" id="te-restore" ' + (tg.restore.enabled !== false ? 'checked' : '') + '><span></span></span> ' + esc(t('ui.t_restore_enable')) + '</label>' +
    '<label class="inline"><input type="checkbox" id="te-verify" ' + (tg.restore.verify !== false ? 'checked' : '') + '> ' + esc(t('ui.t_verify')) + '</label><span class="hint">' + esc(t('ui.t_verify_hint')) + '</span>' +
    '<div class="grid cols-2"><label>' + esc(t('ui.t_minfiles')) + '<input type="number" min="0" id="te-minfiles" value="' + (tg.restore.min_files === undefined ? 1 : tg.restore.min_files) + '"></label>' +
    '<label>' + esc(t('ui.t_timeout')) + '<input type="text" id="te-timeout" value="' + esc(timeoutVal) + '" placeholder="2h"><span class="hint">' + esc(t('ui.t_timeout_hint')) + '</span></label></div>' +
    '<label>' + esc(t('ui.t_include')) + '<textarea id="te-include" spellcheck="false">' + esc((tg.restore.include || []).join('\n')) + '</textarea><span class="hint">' + esc(t('ui.t_include_hint')) + '</span></label>' +
    '<label class="inline"><input type="checkbox" id="te-lock" ' + (tg.use_repo_lock ? 'checked' : '') + '> ' + esc(t('ui.t_lock')) + '</label><span class="hint">' + esc(t('ui.t_lock_hint')) + '</span></fieldset>' +
    '<fieldset><legend>' + esc(t('ui.t_rules')) + '</legend><p class="hint">' + esc(t('ui.t_rules_hint')) + '</p><div id="rules" class="stack">' + (tg.rules || []).map(ruleRowHTML).join('') + '</div>' +
    '<div><button type="button" class="btn small" id="add-rule">' + icon('i-plus') + esc(t('ui.add_rule')) + '</button></div></fieldset>' +
    '<div id="te-test"></div>';
}

function collectTarget(body) {
  const v = (id) => body.querySelector('#' + id);
  const tagsText = v('te-sntags').value.split(',').map((s) => s.trim()).filter(Boolean);
  const include = v('te-include').value.split('\n').map((s) => s.trim()).filter(Boolean);
  const rules = $$('[data-rule]', body).map((row) => {
    const g = (k) => row.querySelector('[data-r="' + k + '"]');
    return { name: g('name').value.trim(), path: g('path').value.trim(), type: g('type').value, sha256: g('type').value === 'sha256' ? g('sha256').value.trim() : '', required: g('required').checked, max_files: 0 };
  });
  return {
    name: v('te-name').value.trim(), enabled: v('te-enabled').checked, repository: v('te-repo').value.trim(),
    password: { source: v('te-pwsrc').value, credential: v('te-credname').value.trim(), env: v('te-pwenv').value.trim(), file: v('te-pwfile').value.trim() },
    snapshot: { select: v('te-snsel').value, id: v('te-snid').value.trim(), tags: tagsText, host: v('te-snhost').value.trim() },
    timeout: v('te-timeout').value.trim() || '0s', use_repo_lock: v('te-lock').checked,
    check: { enabled: v('te-check').checked, read_data: v('te-rd').value, subset: v('te-subset').value.trim() },
    restore: { enabled: v('te-restore').checked, include, verify: v('te-verify').checked, min_files: parseInt(v('te-minfiles').value || '0', 10) || 0 },
    rules,
  };
}

async function editTarget(existing) {
  const isNew = !existing;
  let tg = existing;
  if (isNew) tg = await api.NewTarget();
  let hasPw = false;
  if (!isNew) { try { const v = await api.ListTargets(); hasPw = !!v.has_password[tg.name]; } catch (e) { /* ignore */ } }
  const originalName = isNew ? '' : tg.name;
  document.getElementById('dlg').classList.add('wide');
  const body = dlg.open(isNew ? t('ui.add_target') : t('ui.edit_target') + ': ' + tg.name, targetFormHTML(tg, isNew, hasPw), [
    { label: t('ui.cancel') },
    { label: t('ui.test_connection'), keepOpen: true, onClick: (b) => testTargetFromForm(b) },
    { label: t('ui.save'), cls: 'primary', onClick: (b) => saveTargetFromForm(b, originalName) },
  ], { onClose: () => document.getElementById('dlg').classList.remove('wide') });
  wireTargetForm(body);
}

function wireTargetForm(body) {
  const v = (id) => body.querySelector('#' + id);
  const sync = () => {
    const src = v('te-pwsrc').value;
    ['credential', 'env', 'file'].forEach((s) => { v('pw-' + s).hidden = s !== src; });
    v('sn-tags').hidden = v('te-snsel').value !== 'tag';
    v('sn-id').hidden = v('te-snsel').value !== 'id';
    v('rd-subset').hidden = v('te-rd').value !== 'subset';
    const rd = v('te-rd').value;
    v('rd-cost').lastChild.textContent = t('ui.rd_cost_' + rd);
  };
  ['te-pwsrc', 'te-snsel', 'te-rd'].forEach((id) => v(id).addEventListener('change', sync));
  sync();
  v('te-browse-repo').addEventListener('click', async () => { try { const d = await api.BrowseFolder(t('rpt.repository')); if (d) v('te-repo').value = d; } catch (e) { toast(errText(e), 'bad'); } });
  v('te-browse-pw').addEventListener('click', async () => { try { const f = await api.BrowseFile(t('ui.t_pw_file'), '*'); if (f) v('te-pwfile').value = f; } catch (e) { toast(errText(e), 'bad'); } });
  v('add-rule').addEventListener('click', () => { v('rules').insertAdjacentHTML('beforeend', ruleRowHTML(null)); wireRules(body); });
  wireRules(body);
}

function wireRules(body) {
  $$('[data-rule]', body).forEach((row) => {
    const type = row.querySelector('[data-r="type"]');
    const sha = row.querySelector('[data-r="sha256"]');
    type.onchange = () => { sha.disabled = type.value !== 'sha256'; };
    row.querySelector('[data-rm]').onclick = () => row.remove();
  });
}

function showMessages(body, kind, list) {
  const box = body.querySelector('#te-msgs');
  if (!list || !list.length) { box.innerHTML = ''; return; }
  box.innerHTML = banner(kind, '<ul class="bullets">' + list.map((m) => '<li>' + esc(m) + '</li>').join('') + '</ul>');
  box.scrollIntoView({ block: 'nearest' });
}

async function saveTargetFromForm(body, originalName) {
  const tg = collectTarget(body);
  const pw = body.querySelector('#te-pw').value;
  try {
    const r = await api.SaveTarget(tg, originalName, pw);
    if (!r.saved) { showMessages(body, 'bad', r.messages); return false; }
    toast(t('ui.saved'), 'ok');
    navigate(RP.current === 'dashboard' ? 'dashboard' : 'targets');
  } catch (e) { showMessages(body, 'bad', [errText(e)]); return false; }
}

async function testTargetFromForm(body) {
  const tg = collectTarget(body);
  const out = body.querySelector('#te-test');
  out.innerHTML = '<p class="muted">' + esc(t('ui.testing')) + '</p>';
  showMessages(body, 'bad', []);
  try {
    const r = await api.TestTarget(tg, body.querySelector('#te-pw').value);
    if (r.error) { out.innerHTML = ''; showMessages(body, 'bad', [r.error]); return false; }
    if (r.messages && r.messages.length) { out.innerHTML = ''; showMessages(body, 'bad', r.messages); return false; }
    const res = r.result;
    let h = '<div class="card"><div class="row between"><h3>' + esc(t('ui.test_result')) + '</h3>' + badge(res.status) + '</div><ul class="stage-list" style="margin-top:.5rem">';
    (res.stages || []).forEach((s) => {
      if (s.status === 'skipped') return;
      h += '<li class="' + esc(s.status) + '"><span class="dot"></span><span>' + esc(t('stage.' + s.id)) + '</span><span class="small">' + esc(msg(s.message)) + '</span><span>' + badge(s.status) + '</span></li>';
    });
    h += '</ul>';
    (res.stages || []).forEach((s) => { if (s.detail && s.status !== 'passed') h += '<details><summary>' + esc(t('rpt.details') + ': ' + t('stage.' + s.id)) + '</summary><pre class="detail">' + esc(s.detail) + '</pre></details>'; });
    h += '<p class="hint">' + esc(t('ui.test_note')) + '</p></div>';
    out.innerHTML = h;
    out.scrollIntoView({ block: 'nearest' });
  } catch (e) { out.innerHTML = ''; showMessages(body, 'bad', [errText(e)]); }
  return false;
}

/* =========================================================================
   Reports
   ========================================================================= */
PAGES.reports = async function () {
  const setPage = pageWriter();
  if (RP.reportView) { const id = RP.reportView; return renderReport(id); }
  const view = await api.ListTargets().catch(() => ({ targets: [] }));
  const f = RP.reportFilter || { target: '', since: '', until: '', status: '' };
  let html = '<div class="page"><div class="page-head"><div><h1 data-i18n="nav.reports"></h1><p class="muted" data-i18n="ui.reports_sub"></p></div><div class="actions"><button class="btn" id="open-folder">' + icon('i-folder') + esc(t('ui.open_reports_folder')) + '</button></div></div>';
  if (RP.boot.demo) html += banner('warn', esc(t('ui.demo_reports_note')));
  html += '<div class="card"><form id="rf" class="row" style="align-items:flex-end">' +
    '<label>' + esc(t('rpt.target')) + '<select id="rf-target"><option value="">' + esc(t('ui.all')) + '</option>' + view.targets.concat(RP.boot.demo ? ['documents-daily', 'photos-weekly', 'database-nightly', 'offsite-nas'].map((n) => ({ name: n })) : []).map((x) => '<option' + (x.name === f.target ? ' selected' : '') + '>' + esc(x.name) + '</option>').join('') + '</select></label>' +
    '<label>' + esc(t('ui.from')) + '<input type="date" id="rf-since" value="' + esc(f.since) + '"></label>' +
    '<label>' + esc(t('ui.to')) + '<input type="date" id="rf-until" value="' + esc(f.until) + '"></label>' +
    '<label>' + esc(t('rpt.status')) + '<select id="rf-status"><option value="">' + esc(t('ui.all')) + '</option>' + ['passed', 'failed', 'partial', 'not_run', 'cancelled', 'timed_out'].map((s) => '<option value="' + s + '"' + (f.status === s ? ' selected' : '') + '>' + esc(t('status.' + s)) + '</option>').join('') + '</select></label>' +
    '<button class="btn primary" type="submit">' + esc(t('ui.filter')) + '</button><button class="btn" type="button" id="rf-clear">' + esc(t('ui.clear')) + '</button></form></div>';
  html += '<div class="card" id="rlist"></div></div>';
  const p = setPage(html);
  const load = async () => {
    RP.reportFilter = { target: $('#rf-target').value, since: $('#rf-since').value, until: $('#rf-until').value, status: $('#rf-status').value };
    const box = $('#rlist');
    try {
      const list = await api.ListReports(Object.assign({ limit: 200 }, RP.reportFilter));
      if (!list.length) { box.innerHTML = '<div class="empty">' + icon('i-report') + '<p>' + esc(t('cli.report_none')) + '</p></div>'; return; }
      let h = '<div class="table-wrap"><table><thead><tr><th scope="col">' + esc(t('rpt.started')) + '</th><th scope="col">' + esc(t('rpt.status')) + '</th><th scope="col">' + esc(t('rpt.targets')) + '</th><th scope="col">' + esc(t('rpt.duration')) + '</th><th scope="col">' + esc(t('rpt.trigger')) + '</th></tr></thead><tbody>';
      list.forEach((r) => {
        h += '<tr class="clickable" tabindex="0" role="link" data-open="' + esc(r.run_id) + '"><td>' + esc(fmtDate(r.started_at)) + '<div class="muted small mono">' + esc(r.run_id) + '</div></td><td>' + badge(r.status) + '</td><td>' +
          (r.targets || []).map((x) => esc(x.name) + ' ' + badge(x.status)).join('<br>') + '</td><td>' + esc(fmtDur(r.duration_ms)) + '</td><td>' + esc(t('trigger.' + r.trigger)) + '</td></tr>';
      });
      box.innerHTML = h + '</tbody></table></div>';
      $$('[data-open]', box).forEach((r) => {
        const open = () => { RP.reportView = r.dataset.open; navigate('reports'); };
        r.addEventListener('click', open);
        r.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(); } });
      });
    } catch (e) { box.innerHTML = banner('bad', esc(errText(e))); }
  };
  $('#rf', p).addEventListener('submit', (e) => { e.preventDefault(); load(); });
  $('#rf-clear', p).addEventListener('click', () => { RP.reportFilter = null; navigate('reports'); });
  $('#open-folder', p).addEventListener('click', () => api.OpenFolder('reports').catch((e) => toast(errText(e), 'bad')));
  load();
};

async function renderReport(runId) {
  const setPage = pageWriter();
  let run;
  try { run = await api.GetReport(runId); } catch (e) { RP.reportView = null; toast(errText(e), 'bad'); return navigate('reports'); }
  const exp = (fmtName, target) => '<button class="btn small" data-export="' + fmtName + '" data-etarget="' + esc(target || '') + '">' + esc(fmtName.toUpperCase()) + '</button>';
  let html = '<div class="page"><div class="page-head"><div><button class="btn small" id="back">\u2190 ' + esc(t('nav.reports')) + '</button><h1 style="margin-top:.6rem">' + esc(t('rpt.title')) + '</h1><p class="muted mono">' + esc(run.run_id) + '</p></div>' +
    '<div class="actions"><span class="muted" style="align-self:center">' + esc(t('ui.export')) + ':</span>' + exp('json') + exp('html') + exp('txt') + '<button class="btn small" id="open-html">' + esc(t('ui.open_html')) + '</button></div></div>';
  if (run.demo) html += banner('warn', '<strong>' + esc(t('ui.demo_banner_title')) + '</strong> ' + esc(t('rpt.demo_banner')));
  html += '<div class="card"><div class="row between"><h2>' + esc(t('rpt.overall')) + '</h2>' + badge(run.status) + '</div><dl class="kv">' +
    kv(t('rpt.started'), fmtDate(run.started_at)) + kv(t('rpt.finished'), fmtDate(run.finished_at)) + kv(t('rpt.duration'), fmtDur(run.duration_ms)) +
    kv(t('rpt.trigger'), t('trigger.' + run.trigger)) + kv(t('rpt.app_version'), run.app_version) + kv(t('rpt.restic_version'), run.restic_version || '-') +
    kv(t('rpt.targets'), t('rpt.counts', run.summary.total, run.summary.passed, run.summary.failed, run.summary.partial, run.summary.not_run, run.summary.cancelled + run.summary.timed_out)) + '</dl></div>';
  run.targets.forEach((tg) => { html += reportTarget(tg, exp); });
  html += '<p class="muted small">' + esc(t('rpt.disclaimer')) + '</p></div>';
  const p = setPage(html);
  $('#back', p).addEventListener('click', () => { RP.reportView = null; navigate('reports'); });
  $$('[data-export]', p).forEach((b) => b.addEventListener('click', async () => {
    try { const path = await api.ExportReport(run.run_id, b.dataset.export, b.dataset.etarget); if (path) toast(t('cli.report_written') + ' ' + path, 'ok'); } catch (e) { toast(errText(e), 'bad'); }
  }));
  $('#open-html', p).addEventListener('click', () => api.OpenReport(run.run_id).catch((e) => toast(errText(e), 'bad')));
}

function kv(k, v) { return '<dt>' + esc(k) + '</dt><dd>' + esc(v) + '</dd>'; }

function reportTarget(tg, exp) {
  let h = '<div class="card"><div class="row between"><h2>' + esc(tg.name) + '</h2><div class="row">' + badge(tg.status) + exp('json', tg.name) + exp('html', tg.name) + exp('txt', tg.name) + '</div></div><dl class="kv">' +
    kv(t('rpt.repository'), tg.repository);
  if (tg.snapshot) {
    h += kv(t('rpt.snapshot'), tg.snapshot.short_id + ' (' + tg.snapshot.id + ')') + kv(t('rpt.snapshot_time'), fmtDate(tg.snapshot.time));
    if (tg.snapshot.hostname) h += kv(t('rpt.snapshot_host'), tg.snapshot.hostname);
    if (tg.snapshot.paths && tg.snapshot.paths.length) h += kv(t('rpt.snapshot_paths'), tg.snapshot.paths.join('; '));
    if (tg.snapshot.tags && tg.snapshot.tags.length) h += kv(t('rpt.snapshot_tags'), tg.snapshot.tags.join(', '));
  }
  if (tg.restore) {
    h += kv(t('rpt.restore_time'), fmtDur(tg.restore.duration_ms)) + kv(t('rpt.files_dirs'), fmtNum(tg.restore.files) + ' / ' + fmtNum(tg.restore.directories)) + kv(t('rpt.size'), fmtBytes(tg.restore.bytes));
    if (tg.restore.metadata_warnings) h += kv(t('rpt.metadata_warnings'), tg.restore.metadata_warnings);
  }
  h += '</dl><h3>' + esc(t('rpt.stages')) + '</h3><div class="table-wrap"><table><thead><tr><th scope="col">' + esc(t('rpt.stage')) + '</th><th scope="col">' + esc(t('rpt.status')) + '</th><th scope="col">' + esc(t('rpt.time')) + '</th><th scope="col">' + esc(t('rpt.result')) + '</th></tr></thead><tbody>';
  tg.stages.forEach((s) => { h += '<tr><td>' + esc(t('stage.' + s.id)) + '</td><td>' + badge(s.status) + '</td><td>' + esc(fmtDur(s.duration_ms)) + '</td><td>' + esc(msg(s.message)) + '</td></tr>'; });
  h += '</tbody></table></div>';
  tg.stages.forEach((s) => {
    if (s.rules && s.rules.length) {
      h += '<h3>' + esc(t('rpt.rules')) + '</h3><div class="table-wrap"><table><thead><tr><th scope="col">' + esc(t('rpt.rule')) + '</th><th scope="col">' + esc(t('rpt.status')) + '</th><th scope="col">' + esc(t('rpt.result')) + '</th></tr></thead><tbody>';
      s.rules.forEach((r) => {
        h += '<tr><td>' + esc(r.name || r.pattern) + '<div class="muted small mono">' + esc(r.type + ' \u00b7 ' + r.pattern) + '</div></td><td>' + badge(r.status) + '</td><td>' + esc(msg(r.message));
        if (r.failures && r.failures.length) h += '<ul class="bullets">' + r.failures.map((f) => '<li><span class="mono">' + esc(f.path) + '</span>: ' + esc(msg(f.message)) + '</li>').join('') + '</ul>';
        h += '</td></tr>';
      });
      h += '</tbody></table></div>';
    }
  });
  tg.stages.forEach((s) => { if (s.detail && ['failed', 'not_run', 'partial'].includes(s.status)) h += '<details><summary>' + esc(t('rpt.details') + ': ' + t('stage.' + s.id)) + '</summary><pre class="detail">' + esc(s.detail) + '</pre></details>'; });
  if (tg.coverage && tg.coverage.length) h += '<h3>' + esc(t('rpt.coverage')) + '</h3><ul class="bullets">' + tg.coverage.map((c) => '<li>' + esc(msg(c)) + '</li>').join('') + '</ul>';
  return h + '</div>';
}

/* =========================================================================
   Schedule
   ========================================================================= */
PAGES.schedule = async function () {
  const setPage = pageWriter();
  const info = await api.GetSchedule();
  const st = info.status || {};
  const c = info.config;
  const days = ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday'];
  let html = '<div class="page"><div class="page-head"><div><h1 data-i18n="nav.schedule"></h1><p class="muted" data-i18n="ui.schedule_sub"></p></div></div>';
  if (RP.boot.demo) html += banner('warn', esc(t('ui.demo_schedule_note')));
  if (info.error) html += banner('bad', '<strong>' + esc(t('cli.schedule_status_failed', '')) + '</strong> ' + esc(info.error));
  if (!info.cli_exists) html += banner('warn', esc(t('ui.cli_missing', info.cli_path)));
  html += '<div class="card"><div class="row between"><h2 data-i18n="ui.task_status"></h2>' + (st.installed ? '<span class="badge st-passed">' + icon('i-check') + esc(t('ui.installed')) + '</span>' : '<span class="badge st-never">' + icon('i-minus') + esc(t('ui.not_installed')) + '</span>') + '</div>';
  if (st.installed) {
    html += '<dl class="kv">' + kv(t('cli.schedule_task'), '\\BackupHarbor\\Verify') + kv(t('cli.schedule_state'), st.state || '-') +
      kv(t('cli.schedule_when'), (st.frequency || '') + ' ' + (st.day ? st.day + ' ' : '') + (st.time || '')) + kv(t('cli.schedule_next'), st.next_run ? fmtDate(st.next_run) : '-') +
      kv(t('cli.schedule_last'), st.last_run ? fmtDate(st.last_run) + (st.last_result !== undefined && st.last_result !== null ? ' (0x' + (st.last_result >>> 0).toString(16).toUpperCase() + ')' : '') : '-') + '</dl>';
  } else html += '<p class="muted">' + esc(t('cli.schedule_not_installed')) + '</p>';
  html += '</div>';
  html += '<form class="card stack" id="sf"><h2 data-i18n="ui.schedule_settings"></h2><div class="grid cols-2">' +
    '<label>' + esc(t('ui.frequency')) + '<select id="sc-freq"><option value="daily"' + (c.frequency === 'daily' ? ' selected' : '') + '>' + esc(t('ui.daily')) + '</option><option value="weekly"' + (c.frequency === 'weekly' ? ' selected' : '') + '>' + esc(t('ui.weekly')) + '</option></select></label>' +
    '<label>' + esc(t('ui.time')) + '<input type="time" id="sc-time" value="' + esc(c.time) + '"></label></div>' +
    '<label id="sc-day-wrap">' + esc(t('ui.weekday')) + '<select id="sc-day">' + days.map((d) => '<option value="' + d + '"' + ((c.day || 'sunday') === d ? ' selected' : '') + '>' + esc(t('ui.day_' + d)) + '</option>').join('') + '</select></label>' +
    '<p class="hint">' + esc(t('ui.schedule_note')) + '</p>' +
    '<div id="sc-msg"></div><div class="row"><button class="btn primary" type="submit" ' + (RP.boot.demo ? 'disabled' : '') + '>' + esc(st.installed ? t('ui.update_task') : t('ui.install_task')) + '</button>' +
    (st.installed ? '<button class="btn danger" type="button" id="sc-remove">' + esc(t('ui.remove_task')) + '</button>' : '') + '</div></form></div>';
  const p = setPage(html);
  const sync = () => { $('#sc-day-wrap', p).hidden = $('#sc-freq', p).value !== 'weekly'; };
  $('#sc-freq', p).addEventListener('change', sync); sync();
  $('#sf', p).addEventListener('submit', async (e) => {
    e.preventDefault();
    const msgBox = $('#sc-msg', p);
    msgBox.innerHTML = '';
    try {
      await api.InstallSchedule({ frequency: $('#sc-freq', p).value, time: $('#sc-time', p).value, day: $('#sc-freq', p).value === 'weekly' ? $('#sc-day', p).value : '' });
      toast(t('ui.task_installed'), 'ok'); navigate('schedule');
    } catch (err) { msgBox.innerHTML = banner('bad', '<strong>' + esc(t('ui.task_failed')) + '</strong> ' + esc(errText(err))); }
  });
  const rm = $('#sc-remove', p);
  if (rm) rm.addEventListener('click', async () => {
    if (!(await confirmDialog(t('ui.remove_task'), t('ui.remove_task_text'), t('ui.remove'), true))) return;
    try { await api.RemoveSchedule(); toast(t('cli.schedule_removed'), 'ok'); navigate('schedule'); } catch (err) { toast(errText(err), 'bad'); }
  });
};

/* =========================================================================
   Settings
   ========================================================================= */
PAGES.settings = async function () {
  const setPage = pageWriter();
  const [s, info] = await Promise.all([api.GetSettings(), api.AppInfo()]);
  const sel = (id, val, opts) => '<select id="' + id + '">' + opts.map(([v, l]) => '<option value="' + v + '"' + (v === val ? ' selected' : '') + '>' + esc(l) + '</option>').join('') + '</select>';
  const pathRow = (id, label, val, eff, browse) => '<label>' + esc(label) + '<div class="field-row"><input type="text" id="' + id + '" value="' + esc(val) + '" placeholder="' + esc(eff || '') + '"><button type="button" class="btn" data-browse="' + id + '" data-kind="' + browse + '">' + icon('i-folder') + esc(t('ui.browse')) + '</button></div></label>';
  let html = '<div class="page"><div class="page-head"><div><h1 data-i18n="nav.settings"></h1><p class="muted" data-i18n="ui.settings_sub"></p></div></div>';
  html += '<form class="card stack" id="setf"><div id="set-msg"></div><div class="grid cols-2">' +
    '<label>' + esc(t('ui.language')) + sel('s-lang', s.language, [['auto', t('ui.lang_auto')], ['en', 'English'], ['tr', 'T\u00fcrk\u00e7e']]) + '</label>' +
    '<label>' + esc(t('ui.theme')) + sel('s-theme', s.theme, [['system', t('ui.theme_system')], ['light', t('ui.theme_light')], ['dark', t('ui.theme_dark')]]) + '</label></div>' +
    pathRow('s-restic', t('ui.restic_path'), s.restic_path, 'restic.exe (PATH)', 'file') +
    pathRow('s-temp', t('ui.temp_dir'), s.temp_dir, s.effective_temp_dir, 'dir') + '<span class="hint">' + esc(t('ui.temp_dir_hint')) + '</span>' +
    pathRow('s-report', t('ui.report_dir'), s.report_dir, s.effective_report_dir, 'dir') +
    '<div class="grid cols-2"><label>' + esc(t('ui.concurrency')) + '<input type="number" min="1" max="8" id="s-conc" value="' + s.concurrency + '"><span class="hint">' + esc(t('ui.concurrency_hint')) + '</span></label>' +
    '<label>' + esc(t('ui.timeout')) + '<input type="text" id="s-timeout" value="' + esc(s.timeout) + '"><span class="hint">' + esc(t('ui.timeout_hint')) + '</span></label></div>' +
    '<div class="grid cols-2"><label>' + esc(t('ui.log_level')) + sel('s-log', s.log_level, [['debug', 'debug'], ['info', 'info'], ['warn', 'warn'], ['error', 'error']]) + '</label>' +
    '<fieldset><legend>' + esc(t('ui.extra_formats')) + '</legend><label class="inline"><input type="checkbox" id="s-fhtml" ' + ((s.report_formats || []).includes('html') ? 'checked' : '') + '> HTML</label><label class="inline"><input type="checkbox" id="s-ftxt" ' + ((s.report_formats || []).includes('txt') ? 'checked' : '') + '> TXT</label><span class="hint">' + esc(t('ui.extra_formats_hint')) + '</span></fieldset></div>' +
    '<div class="row"><button class="btn primary" type="submit">' + esc(t('ui.save')) + '</button></div></form>';

  const dep = Object.entries(info.dependencies || {}).map(([k, v]) => kv(k, v)).join('');
  html += '<div class="card"><h2 data-i18n="ui.about"></h2><dl class="kv">' + kv(t('rpt.app_version'), info.version + ' (' + info.commit + ', ' + info.build_date + ')') +
    kv('restic', info.restic.found ? info.restic.version + ' - ' + info.restic.path : (info.restic.error || t('ui.not_found'))) + kv('WebView2', info.webview2 || t('ui.not_found')) +
    kv('Go', info.go_version + ' ' + info.platform) + dep + kv(t('ui.config_file'), info.config_path) + kv(t('ui.report_dir'), info.report_dir) + kv(t('ui.log_file'), info.log_path) + '</dl>' +
    '<div class="row" style="margin-top:.75rem"><button class="btn small" data-folder="config">' + esc(t('ui.open_config_folder')) + '</button><button class="btn small" data-folder="reports">' + esc(t('ui.open_reports_folder')) + '</button><button class="btn small" data-folder="logs">' + esc(t('ui.open_log_folder')) + '</button></div>' +
    '<p class="muted small" style="margin-top:.75rem">' + esc(t('ui.privacy_note')) + '</p></div></div>';
  const p = setPage(html);
  $$('[data-browse]', p).forEach((b) => b.addEventListener('click', async () => {
    try { const r = b.dataset.kind === 'dir' ? await api.BrowseFolder('') : await api.BrowseFile('', '*.exe'); if (r) $('#' + b.dataset.browse, p).value = r; } catch (e) { toast(errText(e), 'bad'); }
  }));
  $$('[data-folder]', p).forEach((b) => b.addEventListener('click', () => api.OpenFolder(b.dataset.folder).catch((e) => toast(errText(e), 'bad'))));
  $('#setf', p).addEventListener('submit', async (e) => {
    e.preventDefault();
    const formats = []; if ($('#s-fhtml', p).checked) formats.push('html'); if ($('#s-ftxt', p).checked) formats.push('txt');
    const ns = { language: $('#s-lang', p).value, theme: $('#s-theme', p).value, restic_path: $('#s-restic', p).value, temp_dir: $('#s-temp', p).value, report_dir: $('#s-report', p).value,
      concurrency: parseInt($('#s-conc', p).value, 10) || 0, timeout: $('#s-timeout', p).value, log_level: $('#s-log', p).value, report_formats: formats };
    try {
      const r = await api.SaveSettings(ns);
      if (!r.saved) { $('#set-msg', p).innerHTML = banner('bad', '<ul class="bullets">' + r.messages.map((m) => '<li>' + esc(m) + '</li>').join('') + '</ul>'); return; }
      toast(t('ui.saved'), 'ok');
      await reloadBootstrap();
      navigate('settings');
    } catch (err) { $('#set-msg', p).innerHTML = banner('bad', esc(errText(err))); }
  });
};
