/* BackupHarbor front end - core helpers: API bridge, i18n, theme, dialogs, toasts. */
'use strict';

const RP = {
  catalog: {},
  lang: 'en',
  boot: null,
  page: null,
  runState: null,
  dirty: false,
};

/* ---- Go bridge ---------------------------------------------------------- */
const api = new Proxy({}, {
  get(_, name) {
    return (...args) => {
      const fn = window.go && window.go.gui && window.go.gui.App && window.go.gui.App[name];
      if (!fn) return Promise.reject(new Error('Backend method not available: ' + name));
      return fn(...args);
    };
  },
});

function onEvent(name, cb) {
  if (window.runtime && window.runtime.EventsOn) window.runtime.EventsOn(name, cb);
}

/* ---- i18n ------------------------------------------------------------------ */
// Catalog templates use %s (sequential) or %[n]s (positional).
function fmt(tmpl, args) {
  if (!args || !args.length) return tmpl;
  let i = 0;
  return tmpl.replace(/%(?:\[(\d+)\])?s|%%/g, (m, pos) => {
    if (m === '%%') return '%';
    const idx = pos ? parseInt(pos, 10) - 1 : i++;
    return idx < args.length ? String(args[idx]) : '';
  });
}

function t(key, ...args) {
  const s = RP.catalog[key];
  if (s === undefined) return key;
  return fmt(s, args);
}

// Render a message object {code, args, text} produced by the Go back end.
function msg(m) {
  if (!m) return '';
  if (m.code && RP.catalog[m.code] !== undefined) return fmt(RP.catalog[m.code], m.args || []);
  return m.text || m.code || '';
}

function applyI18n(root) {
  (root || document).querySelectorAll('[data-i18n]').forEach((el) => { el.textContent = t(el.dataset.i18n); });
  (root || document).querySelectorAll('[data-i18n-title]').forEach((el) => { el.title = t(el.dataset.i18nTitle); });
  (root || document).querySelectorAll('[data-i18n-placeholder]').forEach((el) => { el.placeholder = t(el.dataset.i18nPlaceholder); });
  (root || document).querySelectorAll('[data-i18n-aria]').forEach((el) => { el.setAttribute('aria-label', t(el.dataset.i18nAria)); });
}

/* ---- formatting ----------------------------------------------------------- */
function esc(s) {
  return String(s === undefined || s === null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function fmtDate(iso) {
  if (!iso) return '-';
  const d = new Date(iso);
  if (isNaN(d)) return '-';
  return d.toLocaleString(RP.lang === 'tr' ? 'tr-TR' : 'en-GB', { dateStyle: 'medium', timeStyle: 'short' });
}

function fmtAgo(iso) {
  if (!iso) return t('ui.never');
  const d = new Date(iso);
  const sec = Math.round((Date.now() - d.getTime()) / 1000);
  const rtf = new Intl.RelativeTimeFormat(RP.lang === 'tr' ? 'tr' : 'en', { numeric: 'auto' });
  const abs = Math.abs(sec);
  if (abs < 60) return rtf.format(-sec, 'second');
  if (abs < 3600) return rtf.format(-Math.round(sec / 60), 'minute');
  if (abs < 86400) return rtf.format(-Math.round(sec / 3600), 'hour');
  return rtf.format(-Math.round(sec / 86400), 'day');
}

function fmtDur(ms) {
  if (ms === undefined || ms === null) return '-';
  if (ms < 1000) return ms + ' ms';
  const s = ms / 1000;
  if (s < 60) return s.toFixed(1) + ' s';
  const m = Math.floor(s / 60);
  if (m < 60) return m + 'm ' + String(Math.floor(s % 60)).padStart(2, '0') + 's';
  return Math.floor(m / 60) + 'h ' + String(m % 60).padStart(2, '0') + 'm';
}

function fmtBytes(n) {
  if (n === undefined || n === null) return '-';
  if (n < 1024) return n + ' B';
  const u = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n >= 100 ? 0 : 1) + ' ' + u[i];
}

function fmtNum(n) { return n === undefined || n === null ? '-' : Number(n).toLocaleString(RP.lang === 'tr' ? 'tr-TR' : 'en-GB'); }

const STATUS_ICON = { passed: 'i-check', failed: 'i-x', partial: 'i-half', not_run: 'i-minus', cancelled: 'i-stop', timed_out: 'i-clock', skipped: 'i-minus', never: 'i-minus' };

function badge(status) {
  const key = status === 'never' ? 'ui.never' : 'status.' + status;
  return '<span class="badge st-' + esc(status) + '"><svg aria-hidden="true"><use href="#' + (STATUS_ICON[status] || 'i-minus') + '"/></svg>' + esc(t(key)) + '</span>';
}

function icon(id) { return '<svg aria-hidden="true"><use href="#' + id + '"/></svg>'; }

/* ---- theme ------------------------------------------------------------------ */
const darkQuery = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
function applyTheme(setting) {
  RP.themeSetting = setting || 'system';
  let dark = RP.themeSetting === 'dark';
  if (RP.themeSetting === 'system') dark = !!(darkQuery && darkQuery.matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
}
if (darkQuery) darkQuery.addEventListener('change', () => { if (RP.themeSetting === 'system') applyTheme('system'); });

/* ---- toasts and announcements --------------------------------------------- */
function toast(text, kind) {
  const el = document.createElement('div');
  el.className = 'toast ' + (kind || 'info');
  el.textContent = text;
  document.getElementById('toasts').appendChild(el);
  setTimeout(() => el.remove(), kind === 'bad' ? 9000 : 4500);
}

function announce(text) {
  const a = document.getElementById('announce');
  a.textContent = '';
  setTimeout(() => { a.textContent = text; }, 30);
}

function errText(e) { return (e && e.message) ? e.message : String(e); }

/* ---- dialogs ---------------------------------------------------------------- */
const dlg = {
  el: null,
  open(title, bodyHTML, buttons, opts) {
    opts = opts || {};
    this.el = this.el || document.getElementById('dlg');
    document.getElementById('dlg-title').textContent = title;
    const body = document.getElementById('dlg-body');
    body.innerHTML = bodyHTML;
    const foot = document.getElementById('dlg-foot');
    foot.innerHTML = '';
    (buttons || []).forEach((b) => {
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'btn ' + (b.cls || '');
      btn.textContent = b.label;
      btn.addEventListener('click', async () => {
        if (b.onClick) {
          btn.disabled = true;
          try { const keep = await b.onClick(body); if (keep === false) { btn.disabled = false; return; } } finally { btn.disabled = false; }
        }
        if (!b.keepOpen) this.close();
      });
      foot.appendChild(btn);
    });
    this.onClose = opts.onClose || null;
    applyI18n(body);
    if (!this.el.open) this.el.showModal();
    const first = body.querySelector('input:not([type=hidden]),select,textarea') || foot.querySelector('button');
    if (first && !opts.noFocus) first.focus();
    return body;
  },
  close() { if (this.el && this.el.open) this.el.close(); },
  body() { return document.getElementById('dlg-body'); },
};
document.addEventListener('DOMContentLoaded', () => {
  const el = document.getElementById('dlg');
  document.getElementById('dlg-x').addEventListener('click', () => el.close());
  el.addEventListener('close', () => { if (dlg.onClose) { const f = dlg.onClose; dlg.onClose = null; f(); } });
});

function confirmDialog(title, text, okLabel, danger) {
  return new Promise((resolve) => {
    let answered = false;
    dlg.open(title, '<p>' + esc(text) + '</p>', [
      { label: t('ui.cancel'), onClick: () => { answered = true; resolve(false); } },
      { label: okLabel || t('ui.ok'), cls: danger ? 'danger' : 'primary', onClick: () => { answered = true; resolve(true); } },
    ], { onClose: () => { if (!answered) resolve(false); } });
  });
}

/* ---- small DOM helpers ---------------------------------------------------- */
function $(sel, root) { return (root || document).querySelector(sel); }
function $$(sel, root) { return Array.from((root || document).querySelectorAll(sel)); }
function setPageNow(html) { const p = document.getElementById("page"); p.innerHTML = html; applyI18n(p); return p; }
// Each page captures a writer when it starts; output from a navigation that was
// superseded meanwhile (a slow page the user already left) goes to a detached node.
function pageWriter() {
  const tk = RP.navToken;
  return (html) => (tk === RP.navToken ? setPageNow(html) : (() => { const d = document.createElement("div"); d.innerHTML = html; return d; })());
}
const setPage = setPageNow;

function banner(kind, html) {
  const ic = { info: 'i-info', warn: 'i-warn', bad: 'i-warn', ok: 'i-check' }[kind] || 'i-info';
  return '<div class="banner ' + kind + '" role="' + (kind === 'bad' ? 'alert' : 'note') + '">' + icon(ic) + '<div>' + html + '</div></div>';
}
