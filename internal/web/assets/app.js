"use strict";
/* Zephyr HiFi Kit · Operations Console · vanilla ES2020, no deps */

/* ---------- tiny helpers ---------- */
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
const enc = encodeURIComponent;
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const clamp = (n, a, b) => Math.min(b, Math.max(a, n));
const p2 = n => I18n.number(n, { minimumIntegerDigits: 2, useGrouping: false, maximumFractionDigits: 0 });
const val = id => { const el = document.getElementById(id); return el ? el.value : ""; };
const checked = id => { const el = document.getElementById(id); return !!(el && el.checked); };
// Translate only authored UI copy; never translate values passed to esc().
const appT = (key, params) => I18n.t("app." + key, params && Object.fromEntries(Object.entries(params).map(([name, value]) => [name,
  typeof value === "number" ? I18n.number(value, /^(id|version|status)/.test(name) ? { useGrouping: false } : undefined) : value
])));
const appH = (key, params) => esc(appT(key, params));
const appN = (value, options) => I18n.number(value, options);
const appFixed = (value, digits) => appN(value, { minimumFractionDigits: digits, maximumFractionDigits: digits });

function ts(u) {
  if (u == null || u === "") return 0;
  if (typeof u === "number") return u > 1e12 ? Math.floor(u / 1000) : Math.floor(u);
  const s = String(u).trim();
  if (/^\d+$/.test(s)) { const n = Number(s); return n > 1e12 ? Math.floor(n / 1000) : n; }
  const d = Date.parse(s);
  return isNaN(d) ? 0 : Math.floor(d / 1000);
}
function fmtTime(u) {
  const t = ts(u); if (!t) return "—";
  return I18n.date(new Date(t * 1000), { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}
function ago(u) {
  const t = ts(u); if (!t) return "—";
  const s = Math.max(0, Math.floor(Date.now() / 1000) - t);
  if (s < 5) return appT("m001");
  if (s < 60) return I18n.relative(-s, "second");
  if (s < 3600) return I18n.relative(-Math.floor(s / 60), "minute");
  if (s < 86400) return I18n.relative(-Math.floor(s / 3600), "hour");
  return I18n.relative(-Math.floor(s / 86400), "day");
}
function fmtUp(sec) {
  sec = Math.max(0, Math.floor(Number(sec) || 0));
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60), s = sec % 60;
  const clock = `${p2(h)}:${p2(m)}:${p2(s)}`;
  return d ? appT("uptimeDays", { count: d, clock }) : clock;
}
function fmtBytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return appN(n) + " B";
  if (n < 1048576) return appFixed(n / 1024, 1) + " KB";
  if (n < 1073741824) return appFixed(n / 1048576, 1) + " MB";
  return appFixed(n / 1073741824, 2) + " GB";
}
function shortId(s) { s = String(s ?? ""); return s.length > 16 ? s.slice(0, 12) + "…" : s; }
function badge(text, kind) { return `<span class="badge ${kind || ""}">${esc(text)}</span>`; }
function splitList(s) { return String(s ?? "").split(/[,，\s]+/).map(x => x.trim()).filter(Boolean); }

/* ---------- inline SVG icons ---------- */
const ICONS = {
  gauge: '<path d="M12 14l4-4"/><path d="M3.5 15a9 9 0 1117 0"/><circle cx="12" cy="14" r="1.4"/>',
  server: '<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/>',
  box: '<path d="M21 8l-9-5-9 5v8l9 5 9-5V8z"/><path d="M3 8l9 5 9-5"/><path d="M12 13v8"/>',
  send: '<path d="M22 2L11 13"/><path d="M22 2l-7 20-4-9-9-4 20-7z"/>',
  key: '<circle cx="8" cy="15" r="4"/><path d="M10.8 12.2L20 3"/><path d="M16 7l3 3"/>',
  audit: '<path d="M14 3H6a2 2 0 00-2 2v14a2 2 0 002 2h12a2 2 0 002-2V9l-6-6z"/><path d="M14 3v6h6"/><path d="M9 13h6M9 17h6"/>',
  bell: '<path d="M18 8a6 6 0 10-12 0c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.7 21a2 2 0 01-3.4 0"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  x: '<path d="M18 6L6 18M6 6l12 12"/>',
  check: '<path d="M20 6L9 17l-5-5"/>',
  alert: '<path d="M12 3l10 18H2L12 3z"/><path d="M12 10v4"/><path d="M12 17.5h.01"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><path d="M12 8h.01"/>',
  cpu: '<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="10" y="10" width="4" height="4"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>',
  play: '<path d="M7 4l13 8-13 8V4z"/>',
  terminal: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3"/><path d="M13 15h4"/>',
  sliders: '<path d="M4 7h16M4 12h16M4 17h16"/><circle cx="9" cy="7" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="7" cy="17" r="2"/>',
  refresh: '<path d="M21 12a9 9 0 11-2.6-6.4"/><path d="M21 3v6h-6"/>',
  download: '<path d="M12 3v12"/><path d="M7 10l5 5 5-5"/><path d="M4 21h16"/>',
  upload: '<path d="M12 21V9"/><path d="M7 14l5-5 5 5"/><path d="M4 3h16"/>',
  copy: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/>',
  trash: '<path d="M3 6h18"/><path d="M8 6V4a1 1 0 011-1h6a1 1 0 011 1v2"/><path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6"/><path d="M10 11v6M14 11v6"/>',
  shield: '<path d="M12 2l8 4v6c0 5-3.5 8.5-8 10-4.5-1.5-8-5-8-10V6l8-4z"/>',
  unlock: '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 017.7-1.5"/>',
  lock: '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 018 0v4"/>',
  chevR: '<path d="M9 6l6 6-6 6"/>',
  chevL: '<path d="M15 6l-6 6 6 6"/>',
  arrowUp: '<path d="M12 19V5"/><path d="M5 12l7-7 7 7"/>',
  arrowDown: '<path d="M12 5v14"/><path d="M19 12l-7 7-7-7"/>',
  wifi: '<path d="M2 9a15 15 0 0120 0"/><path d="M5.5 12.5a10 10 0 0113 0"/><path d="M9 16a5 5 0 016 0"/><path d="M12 19.5h.01"/>',
  activity: '<path d="M22 12h-4l-3 8-6-16-3 8H2"/>',
  layers: '<path d="M12 2l10 6-10 6L2 8l10-6z"/><path d="M2 14l10 6 10-6"/>',
  branch: '<circle cx="6" cy="5" r="2.5"/><circle cx="6" cy="19" r="2.5"/><circle cx="18" cy="9" r="2.5"/><path d="M6 7.5v9"/><path d="M18 11.5c0 4-4 4.5-9 4.5"/>',
  external: '<path d="M14 4h6v6"/><path d="M20 4L10 14"/><path d="M20 14v5a1 1 0 01-1 1H5a1 1 0 01-1-1V5a1 1 0 011-1h5"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a14 14 0 010 18 14 14 0 010-18z"/>',
  undo: '<path d="M9 14L4 9l5-5"/><path d="M4 9h10a6 6 0 016 6v2"/>',
  power: '<path d="M12 3v8"/><path d="M6.3 6.3a8 8 0 1011.4 0"/>',
  music: '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
  disc: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="2.5"/>',
  speaker: '<rect x="6" y="3" width="12" height="18" rx="2"/><circle cx="12" cy="14" r="3.5"/><path d="M12 7h.01"/>',
  edit: '<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 013 3L7 19l-4 1 1-4L16.5 3.5z"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  database: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5"/><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z"/><circle cx="12" cy="12" r="3"/>',
  stop: '<rect x="6" y="6" width="12" height="12" rx="2"/>',
  folder: '<path d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z"/>',
  pause: '<rect x="7" y="5" width="3.5" height="14" rx="1"/><rect x="13.5" y="5" width="3.5" height="14" rx="1"/>'
};
function icon(name, size = 16, cls = "") {
  return `<svg class="ic ${cls}" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name] || ICONS.info}</svg>`;
}

/* ---------- domain badges ---------- */
function statusBadge(st) {
  if (st === "online") return badge(appT("m003"), "ok");
  if (st === "offline") return badge(appT("m004"), "err");
  return badge(appT("m005"), "muted");
}
function authBadge(n) { return n && n.authorized ? badge(appT("m006"), "ok") : badge(appT("m007"), "warn"); }
const SEV_ZH = { get critical() { return appT("m008"); }, get warning() { return appT("m009"); }, get info() { return appT("m010"); } };
function sevBadge(s) { return badge(SEV_ZH[s] || s || appT("m010"), s === "critical" ? "err" : s === "warning" ? "warn" : "info"); }
const JOB_ZH = {
  get queued() { return appT("m011"); }, get precheck() { return appT("m012"); }, get resolve_deps() { return appT("m013"); }, get waiting_idle() { return appT("m014"); },
  get downloading() { return appT("m015"); }, get verifying() { return appT("m016"); }, get installing() { return appT("m017"); }, get rendering_config() { return appT("m018"); },
  get enabling_units() { return appT("m019"); }, get validating() { return appT("m020"); }, get done() { return appT("m021"); }, get rollback() { return appT("m022"); },
  get rollback_verify() { return appT("m023"); }, get failed_rolledback() { return appT("m024"); }, get failed_unrecovered() { return appT("m025"); },
  get suspended() { return appT("m026"); }, get interrupted() { return appT("m027"); }, get provision() { return appT("m028"); }, get upgrade() { return appT("m029"); }, get reconcile() { return appT("m030"); }
};
const JOB_DONE = new Set(["done", "failed_rolledback", "failed_unrecovered"]);
const jobState = j => (j && (j.state || j.status)) || "queued";
const jobActive = j => !JOB_DONE.has(jobState(j));
function jobBadge(j) {
  const s = jobState(j);
  const k = s === "done" ? "ok" : s.startsWith("failed") ? "err" : (s === "suspended" || s === "interrupted") ? "warn" : "info";
  return badge(JOB_ZH[s] || s, k);
}
function phaseBadge(st) {
  const m = { pending: [appT("m031"), "muted"], running: [appT("m032"), "info"], ok: [appT("m033"), "ok"], failed: [appT("m034"), "err"], skipped: [appT("m035"), "muted"] };
  const x = m[st] || [st || appT("m031"), "muted"]; return badge(x[0], x[1]);
}
function sigModeZh(m) { return ({ enforce: appT("m036"), strict: appT("m036"), dev: appT("m037"), permissive: appT("m038"), warn: appT("m039"), off: appT("signatureOff"), disabled: appT("signatureOff") })[m] || (m || "—"); }
function catBadge(c) {
  const m = { renderer: appT("m041"), dependency: appT("m042"), tool: appT("m043"), source: appT("m044"), core: appT("m045"), audio: appT("m046"), streamer: appT("m047") };
  return badge(m[c] || c || appT("m048"), "violet");
}
function lcBadge(lc) {
  const m = { available: [appT("m049"), "ok"], deprecated: [appT("m050"), "warn"], revoked: [appT("m051"), "err"] };
  const x = m[lc] || [lc || appT("m049"), "muted"]; return badge(x[0], x[1]);
}
function resultBadge(r) {
  const s = String(r || "").toLowerCase();
  if (["ok", "success", "allowed", "done", "pass"].includes(s)) return badge(appT("m052"), "ok");
  if (["err", "error", "failed", "denied", "rejected", "fail"].includes(s)) return badge(appT("m034"), "err");
  return badge(r || "—", "muted");
}
function alertStatusBadge(a) {
  const s = a.status;
  if (s === "open") return badge(appT("m053"), "warn");
  if (s === "ack" || s === "acked") return badge(appT("m054"), "info");
  if (s === "resolved") return badge(appT("m055"), "ok");
  return badge(s || "—", "muted");
}
function cmdStatus(c) {
  if (!c) return "pending";
  if (c.status === "ok" || c.status === "err") return c.status;
  if (c.status === "done") return c.result_status === "err" ? "err" : "ok";
  return "pending";
}

/* ---------- API layer ---------- */
const API_BASE = "/api/v1";
let appConnOK = true;
function setConn(ok) {
  appConnOK = !!ok;
  const el = $("#conn"); if (!el) return;
  el.classList.toggle("ok", ok); el.classList.toggle("bad", !ok);
  const dot = el.querySelector(".sdot");
  if (dot) { dot.classList.toggle("ok", ok); dot.classList.toggle("err", !ok); dot.classList.toggle("live", ok); }
  const t = $("#conn-text"); if (t) t.textContent = ok ? appT("m056") : appT("m057");
}
async function req(method, path, opt) {
  let r;
  try { r = await fetch(API_BASE + path, Object.assign({ method }, opt || {})); }
  catch (e) {
    setConn(false);
    const err = new Error(appT("m058"));
    err.data = e.data; err.code = e.code; err.rawMessage = e.message;
    throw err;
  }
  setConn(true);
  const text = await r.text();
  let data = null;
  if (text) { try { data = JSON.parse(text); } catch (e) { data = null; } }
  if (!r.ok) {
    let msg = data && (data.error || data.message);
    if (msg && typeof msg === "object") msg = msg.message || JSON.stringify(msg);
    const rawMessage = (typeof msg === "string" && msg) ? msg : (text ? text.slice(0, 160) : "HTTP " + r.status);
    const err = new Error(I18n.message(data, rawMessage));
    err.status = r.status; err.data = data;
    err.code = data && (data.code || (data.error && data.error.code));
    err.rawMessage = rawMessage;
    throw err;
  }
  return data;
}
const api = (method, path, body) => req(method, path, method === "GET" || method === "HEAD" ? {} : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body === undefined ? {} : body) });
const apiForm = (path, fd) => req("POST", path, { body: fd });
async function rootJson(path) {
  const r = await fetch(path);
  if (!r.ok) {
    const text = await r.text();
    let data = null; try { data = JSON.parse(text); } catch (e) { /* non-JSON response */ }
    const err = new Error(I18n.message(data, appT("requestFailed", { status: r.status })));
    err.status = r.status; err.data = data; err.code = data && (data.code || data.error?.code);
    err.rawMessage = data?.error?.message || data?.error || data?.message || text;
    throw err;
  }
  return r.json();
}
function download(path) { window.location.href = API_BASE + path; }
async function copyText(text) {
  try { await navigator.clipboard.writeText(text); return true; }
  catch (e) {
    try {
      const ta = document.createElement("textarea");
      ta.value = text; ta.style.position = "fixed"; ta.style.opacity = "0";
      document.body.appendChild(ta); ta.select();
      const ok = document.execCommand("copy"); ta.remove(); return ok;
    } catch (e2) { return false; }
  }
}
function b64utf8(s) {
  const bytes = new TextEncoder().encode(s); let bin = "";
  for (let i = 0; i < bytes.length; i += 8192) bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 8192));
  return btoa(bin);
}

/* ---------- toasts ---------- */
let toastSeq = 0;
const toastUndos = new Map();
function toast(msg, kind = "info", opts = {}) {
  const root = $("#toast-root"); if (!root) return;
  while (root.children.length >= 4) removeToast(root.firstElementChild.dataset.tid, true);
  const tid = "t" + (++toastSeq);
  const el = document.createElement("div");
  el.className = "toast " + kind; el.dataset.tid = tid;
  const ic = kind === "ok" ? "check" : kind === "err" ? "alert" : kind === "warn" ? "alert" : "info";
  el.innerHTML = `<span class="t-ic">${icon(ic, 16)}</span><div class="t-msg">${esc(msg)}</div>` +
    (opts.undo ? `<button class="t-undo" data-act="toast-undo" data-tid="${tid}">${esc(opts.undoText || appT("m060"))}</button>` : "") +
    `<button class="t-x" data-act="toast-close" data-tid="${tid}" aria-label="${appH("m040")}">${icon("x", 14)}</button>`;
  root.appendChild(el);
  if (opts.undo) toastUndos.set(tid, opts.undo);
  el._timer = setTimeout(() => removeToast(tid), opts.undo ? 9000 : 4200);
}
function removeToast(tid, instant) {
  const el = $(`#toast-root .toast[data-tid="${tid}"]`); if (!el) return;
  clearTimeout(el._timer); toastUndos.delete(tid);
  if (instant) { el.remove(); return; }
  el.classList.add("out"); setTimeout(() => el.remove(), 220);
}

/* ---------- confirm dialog ---------- */
let confirmResolve = null;
function confirmDlg({ title = appT("m061"), message = "", okText = appT("m062"), danger = true } = {}) {
  return new Promise(res => {
    confirmResolve = res;
    $("#confirm-root").innerHTML = `
      <div class="confirm-backdrop" data-act="confirm-backdrop">
        <div class="confirm ${danger ? "" : "safe"}" role="dialog" aria-modal="true" aria-label="${esc(title)}">
          <div class="c-ic">${icon(danger ? "alert" : "info", 22)}</div>
          <div class="c-title">${esc(title)}</div>
          <div class="c-msg">${esc(message)}</div>
          <div class="c-btns">
            <button class="btn ghost" data-act="confirm-cancel">${appH("m063")}</button>
            <button class="btn ${danger ? "danger" : "primary"}" data-act="confirm-ok">${esc(okText)}</button>
          </div>
        </div>
      </div>`;
  });
}
function closeConfirm(v) {
  if (!confirmResolve) return;
  const r = confirmResolve; confirmResolve = null;
  $("#confirm-root").innerHTML = ""; r(v);
}

/* ---------- modal ---------- */
let modalOnClose = null;
function openModal(title, body, foot = "", opts = {}) {
  closeModal();
  modalOnClose = opts.onClose || null;
  $("#modal-root").innerHTML = `
    <div class="modal-backdrop" data-act="modal-backdrop">
      <div class="modal ${opts.narrow ? "narrow" : ""}" role="dialog" aria-modal="true">
        <div class="modal-head">
          <div class="m-title">${title}</div>
          <button class="icon-btn" data-act="modal-close" aria-label="${appH("m040")}">${icon("x", 18)}</button>
        </div>
        <div class="modal-body">${body}</div>
        ${foot ? `<div class="modal-foot">${foot}</div>` : ""}
      </div>
    </div>`;
}
function setModalBody(html) { const b = $(".modal-body"); if (!b) return; const st = b.scrollTop; b.innerHTML = html; b.scrollTop = st; }
function setModalFoot(html) {
  let f = $(".modal-foot");
  if (!f && html) { const m = $(".modal"); if (m) m.insertAdjacentHTML("beforeend", `<div class="modal-foot"></div>`); f = $(".modal-foot"); }
  if (f) f.innerHTML = html;
}
function closeModal() {
  const root = $("#modal-root");
  if (!root || !root.firstElementChild) { modalOnClose = null; return; }
  const cb = modalOnClose; modalOnClose = null;
  root.innerHTML = "";
  if (cb) { try { cb(); } catch (e) { /* cleanup best-effort */ } }
}

/* ---------- drawer ---------- */
let drawerOnClose = null, drawerCtx = null;
function openDrawer(title, sub, body, foot = "", opts = {}) {
  closeDrawer();
  drawerOnClose = opts.onClose || null;
  drawerCtx = opts.ctx || null;
  const back = opts.back ? `<button class="icon-btn" data-act="${esc(opts.back.act)}" data-id="${esc(opts.back.id)}" title="${appH("m064")}" aria-label="${appH("m064")}">${icon("chevL", 18)}</button>` : "";
  $("#drawer-root").innerHTML = `
    <div class="drawer-backdrop" data-act="drawer-backdrop"></div>
    <div class="drawer" role="dialog" aria-modal="true">
      <div class="drawer-head">
        ${back}
        <div style="flex:1;min-width:0">
          <div class="d-title">${title}</div>
          ${sub ? `<div class="d-sub truncate">${sub}</div>` : ""}
        </div>
        <button class="icon-btn" data-act="drawer-close" aria-label="${appH("m040")}">${icon("x", 18)}</button>
      </div>
      <div class="drawer-body">${body}</div>
      ${foot ? `<div class="drawer-foot">${foot}</div>` : ""}
    </div>`;
}
function setDrawerBody(html) { const b = $(".drawer-body"); if (!b) return; const st = b.scrollTop; b.innerHTML = html; b.scrollTop = st; }
function setDrawerFoot(html) {
  let f = $(".drawer-foot");
  if (!f && html) { const d = $(".drawer"); if (d) d.insertAdjacentHTML("beforeend", `<div class="drawer-foot"></div>`); f = $(".drawer-foot"); }
  if (f) f.innerHTML = html;
}
function setDrawerTitle(title, sub) {
  const t = $(".d-title"); if (t) t.innerHTML = title;
  const s = $(".d-sub"); if (s && sub !== undefined) s.innerHTML = sub;
}
function closeDrawer() {
  const root = $("#drawer-root");
  ["node:poll", "os:cmd", "net:cmd", "job:poll"].forEach(stopPoll);
  if (!root || !root.firstElementChild) { drawerOnClose = null; drawerCtx = null; return; }
  const cb = drawerOnClose; drawerOnClose = null; drawerCtx = null;
  root.innerHTML = "";
  if (cb) { try { cb(); } catch (e) { /* cleanup best-effort */ } }
}

/* ---------- poll manager (no leaked intervals) ---------- */
const polls = new Map();
function poll(key, fn, ms, immediate = false) {
  stopPoll(key);
  const run = () => {
    if (document.hidden) return;
    try { const p = fn(); if (p && p.catch) p.catch(() => {}); } catch (e) { /* tick errors are non-fatal */ }
  };
  polls.set(key, setInterval(run, ms));
  // The first tick paints regardless of tab visibility; only later ticks idle
  // while hidden, so a backgrounded tab never shows a stale connecting state.
  if (immediate) { try { const p = fn(); if (p && p.catch) p.catch(() => {}); } catch (e) { /* non-fatal */ } }
}
function stopPoll(key) { const t = polls.get(key); if (t !== undefined) { clearInterval(t); polls.delete(key); } }
function stopPrefix(pre) { for (const k of Array.from(polls.keys())) if (k.startsWith(pre)) stopPoll(k); }

/* ---------- app state ---------- */
const S = {
  view: "overview", search: "",
  health: null, metrics: null,
  nodes: [], apps: [], boms: [], pubBoms: [], jobs: [], rollouts: [], alerts: [], keys: [], license: null, audit: [],
  nodeApps: {}, nodeOs: {}, nodePlay: {}, nodeQueue: {}, nodeSvc: {},
  nodeFilter: "all", nodeSort: { key: "alias", dir: 1 }, sel: new Set(),
  alertFilter: "open", auditFilter: "all",
  mounts: [], tracks: [], libMount: "", play: {},
  aptApps: [], aptSources: [],
  libTab: "albums", playlists: [], sysOpen: false,
  upd: { server: null, policy: null, fleet: null, disabled: false, busy: false }
};
const NAV = [
  { id: "overview", get name() { return appT("m065"); }, icon: "home" },
  { id: "library", get name() { return appT("m066"); }, icon: "library" },
  { id: "nodes", get name() { return appT("m067"); }, icon: "server" },
  { id: "audit", get name() { return appT("m068"); }, icon: "audit" },
  { id: "settings", get name() { return appT("m069"); }, icon: "settings" }
];

/* ---------- shared builders ---------- */
function empty(ic, line, cta = "", sm = false) {
  return `<div class="empty ${sm ? "sm" : ""}"><div class="e-ic">${icon(ic, sm ? 20 : 26)}</div><div class="e-line">${esc(line)}</div>${cta}</div>`;
}
function secHead(title, right = "") { return `<div class="sec-head"><div class="sec-title">${title}</div><div class="spacer"></div>${right}</div>`; }
function meter(label, pct, valText, color = "") {
  const p = clamp(Number(pct) || 0, 0, 100);
  return `<div class="meter"><div class="m-top"><span>${esc(label)}</span><span class="m-val">${esc(valText)}</span></div><div class="m-track"><div class="m-fill ${color}" style="width:${p}%"></div></div></div>`;
}
function kvRow(k, v) { return `<div class="k">${esc(k)}</div><div class="v">${v}</div>`; }
function kpi(label, num, color, ic, sub = "") {
  return `<div class="kpi" style="--kc:var(--${color})"><div class="k-top">${icon(ic, 15)}<span class="k-lbl">${esc(label)}</span></div><div class="k-num">${esc(typeof num === "number" ? appN(num) : num)}${sub ? ` <small>${esc(sub)}</small>` : ""}</div></div>`;
}
function hit(...fields) {
  const q = S.search.trim().toLowerCase();
  if (!q) return true;
  return fields.some(f => String(f ?? "").toLowerCase().includes(q));
}
function viewError(e) {
  $("#view").innerHTML = `<div class="card">${empty("alert", appT("m070") + ((e && e.message) || appT("m071")), `<button class="btn primary" data-act="refresh-view">${icon("refresh", 15)} ${appH("m072")}</button>`)}</div>`;
}

/* ---------- routing ---------- */
function shellFor(v) {
  if (v === "settings") return shellSettings();
  return ({ overview: shellOverview, library: shellLibrary, nodes: shellNodes, catalog: shellCatalog, keys: shellKeys, audit: shellAudit }[v] || shellOverview)();
}
async function refreshFor(v, silent) {
  try {
    if (v === "overview") await refreshOverview();
    else if (v === "library") await refreshLibrary();
    else if (v === "settings") await refreshSettings();
    else if (v === "nodes") await refreshTerminalCenter();
    else if (v === "catalog") await refreshCatalog();
    else if (v === "keys") await refreshKeys();
    else if (v === "audit") await refreshAudit();
  } catch (e) { if (!silent) viewError(e); }
}
function refreshCurrent() { return refreshFor(S.view, true); }
function setView(v) {
  if (v === "keys" || v === "catalog") { openSettings(v === "keys" ? "authorization" : "packages"); return; }
  stopPrefix("view:");
  cleanupOverview();
  cleanupTerminalCenter();
  closeModal(); closeDrawer();
  S.view = v; S.search = ""; S.sel.clear();
  const gs = $("#global-search"); if (gs) gs.value = "";
  if (typeof renderSidebarNav === "function") renderSidebarNav();
  const el = $("#view");
  el.dataset.view = v;
  el.innerHTML = shellFor(v);
  el.classList.remove("view-enter"); void el.offsetWidth; el.classList.add("view-enter");
  setTimeout(() => el.classList.remove("view-enter"), 1000);
  refreshFor(v);
  if (v === "overview") {
    paintOverview();
    refreshListening();
    poll("view:main", refreshOverview, 15000);
    poll("view:listening", refreshListening, 30000);
  }
  else if (v === "nodes") poll("view:main", () => refreshFor(v, true), 4000);
  else if (v === "library") poll("view:main", () => refreshLibraryLive(), 3000);
}

/* ---------- global live tick ---------- */
async function globalTick() {
  try {
    const h = await api("GET", "/health");
    S.health = h;
    const bv = $("#brand-ver"); if (bv) bv.textContent = h && h.version ? ("v" + h.version) : "v—";
    const hl = $("#health-line");
    if (hl && h) hl.textContent = appT("m075", { value: sigModeZh(h.signature_mode), value2: h.play_gate ? appT("m073") : appT("m074") });
  } catch (e) { /* connection state handled in api */ }
  try {
    const al = await api("GET", "/alerts");
    S.alerts = Array.isArray(al) ? al : [];
    paintBell();
    if (drawerCtx && drawerCtx.kind === "alerts") paintAlerts();
    if (S.view === "overview") paintOverviewKpis();
  } catch (e) { /* non-fatal */ }
}
function paintBell() {
  const open = S.alerts.filter(a => a.status === "open").length;
  const el = $("#bell-count");
  if (el) { el.textContent = open > 99 ? appN(99) + "+" : appN(open); el.classList.toggle("hidden", open === 0); }
}

/* ================= OVERVIEW ================= */
// Listening filters and playback target deliberately have separate state.
const OV = {
  days: 7, node: "", data: null, error: "", statsBusy: false, statsSeq: 0,
  epoch: 0, busy: false, loaded: false, tracksError: false, playlistsError: false, nodesError: false,
  recentMore: false, addedMore: false
};
function cleanupOverview() {
  OV.epoch++; OV.statsSeq++;
  OV.busy = OV.statsBusy = false;
  OV.data = null; OV.error = ""; OV.loaded = false;
  OV.nodesError = OV.tracksError = OV.playlistsError = false;
}
function shellOverview() {
  return `<header class="ov-head">
      <div><div class="ov-eyebrow">YOUR LISTENING ROOM</div><h1 id="ov-greeting">${appH("m065")}</h1><p class="ov-scope">${appH("m079")}</p></div>
      <div class="ov-filters"><div class="chips" aria-label="${appH("m080")}">${[[1, appT("m076")], [7, appT("m077")], [30, appT("m078")]].map(([d, label]) => `<button class="chip ${OV.days === d ? "active" : ""}" data-act="ov-days" data-days="${d}" aria-pressed="${OV.days === d}">${esc(label)}</button>`).join("")}</div>
        <label class="ov-filter-label">${appH("m081")}<select id="ov-stat-node" class="inp" data-change="ov-stat-node" aria-label="${appH("m081")}"><option value="">${appH("m082")}</option></select></label>
      </div>
    </header>
    <div id="ov-stat-note" class="ov-note" role="status"></div>
    <div class="kpis ov-kpis stagger" style="--i:0" id="ov-kpis"></div>
    <div class="ov-albums-grid">
      <section class="card ov-section stagger" style="--i:1" id="ov-recent"></section>
      <section class="card ov-section stagger" style="--i:2" id="ov-albums"></section>
    </div>
    <div class="grid cols-2 mt16 ov-tastes">
      <section class="card stagger" style="--i:3" id="ov-genres"></section>
      <section class="card stagger" style="--i:4" id="ov-artists"></section>
    </div>
    <section class="card mt16" id="ov-favorites" aria-label="${appH("m083")}"></section>
    <section class="card mt16 ov-shortcuts" id="ov-playlists"></section>
    <section class="card mt16 ov-system" id="ov-sys"></section>`;
}
async function refreshOverview() {
  if (S.view !== "overview" || OV.busy) return;
  OV.busy = true;
  const epoch = OV.epoch;
  try {
    const [nodes, tracks, playlists, metrics, jobs, alerts] = await Promise.all([
      api("GET", "/nodes").catch(() => null), api("GET", "/tracks").catch(() => null),
      api("GET", "/playlists").catch(() => null), rootJson("/metrics/server").catch(() => null),
      api("GET", "/jobs").catch(() => null), api("GET", "/alerts").catch(() => null)
    ]);
    if (S.view !== "overview" || epoch !== OV.epoch) return;
    OV.nodesError = !Array.isArray(nodes); OV.tracksError = !Array.isArray(tracks); OV.playlistsError = !Array.isArray(playlists);
    if (Array.isArray(nodes)) S.nodes = nodes;
    if (Array.isArray(tracks)) S.tracks = tracks;
    if (Array.isArray(playlists)) S.playlists = playlists;
    S.metrics = metrics;
    if (Array.isArray(jobs)) S.jobs = jobs;
    if (Array.isArray(alerts)) S.alerts = alerts;
    OV.loaded = true;
    paintBell(); paintOverview();
    if (typeof refreshFavoritePreview === "function") refreshFavoritePreview();
    const updates = await Promise.all(S.nodes.filter(n => n.status === "online").map(async n =>
      [n.node_id, await api("GET", `/nodes/${enc(n.node_id)}/osupdates`).catch(() => null)]));
    if (S.view !== "overview" || epoch !== OV.epoch) return;
    for (const [id, o] of updates) if (o) S.nodeOs[id] = o;
    paintSysStrip();
  } finally { if (epoch === OV.epoch) OV.busy = false; }
}
async function refreshListening(force = false) {
  if (S.view !== "overview" || (OV.statsBusy && !force)) return;
  const seq = ++OV.statsSeq, epoch = OV.epoch;
  const days = OV.days, node = OV.node;
  OV.statsBusy = true;
  if (force) { OV.data = null; OV.error = ""; paintListening(); }
  try {
    const d = await api("GET", `/listening?days=${days}${node ? "&node_id=" + enc(node) : ""}`);
    if (epoch !== OV.epoch || seq !== OV.statsSeq || S.view !== "overview") return;
    if (!d || !["listen_ms", "track_count", "album_count", "artist_count", "unmeasured_plays"].every(k => typeof d[k] === "number" && Number.isFinite(d[k]) && d[k] >= 0)
      || !["genres", "artists", "recent_albums"].every(k => Array.isArray(d[k]))) throw new Error(appT("m084"));
    OV.data = d; OV.error = "";
  } catch (e) {
    if (epoch !== OV.epoch || seq !== OV.statsSeq || S.view !== "overview") return;
    OV.data = null; OV.error = e.message || appT("m085");
  } finally {
    if (epoch === OV.epoch && seq === OV.statsSeq && S.view === "overview") { OV.statsBusy = false; paintListening(); }
  }
}
function ovSetDays(days) {
  if (![1, 7, 30].includes(days) || days === OV.days) return;
  OV.days = days; OV.recentMore = false;
  $$("[data-act='ov-days']").forEach(b => { const on = Number(b.dataset.days) === days; b.classList.toggle("active", on); b.setAttribute("aria-pressed", String(on)); });
  refreshListening(true);
}
function ovSyncSelect(el, html, value) {
  if (!el || document.activeElement === el) return;
  if (el.dataset.options !== html) { el.innerHTML = html; el.dataset.options = html; }
  if (el.value !== value) el.value = value;
}
function ovNodeOptions(selected, all, nodes = S.nodes) {
  let html = `<option value="">${all ? appH("m082") : appH("m086")}</option>` + nodes.map(n => `<option value="${esc(n.node_id)}">${esc(n.alias || shortId(n.node_id))}${n.status === "online" ? "" : appH("m087")}</option>`).join("");
  if (selected && !nodes.some(n => n.node_id === selected)) html += `<option value="${esc(selected)}">${appH("m088", { value: shortId(selected) })}</option>`;
  return html;
}
function paintOverview() { paintListening(); paintOvAlbums(); paintOvPlaylists(); paintSysStrip(); if (typeof paintFavoritePreview === "function") paintFavoritePreview(); }
function paintListening() {
  paintOverviewKpis(); paintOvRecent(); paintOvTastes();
  ovSyncSelect($("#ov-stat-node"), ovNodeOptions(OV.node, true), OV.node);
}
function fmtListen(ms) {
  const sec = Math.floor(Math.max(0, Number(ms) || 0) / 1000);
  if (sec < 60) return appT("listenSeconds", { count: sec });
  const min = Math.floor(sec / 60);
  return min < 60 ? appT("listenMinutes", { count: min }) : appT("listenHours", { hours: Math.floor(min / 60), minutes: min % 60 });
}
function ovRetry(act = "ov-retry") { return `<button class="btn xs ghost" data-act="${act}">${icon("refresh", 12)} ${appH("m072")}</button>`; }
function ovStatEmpty() { return empty(OV.error ? "alert" : "clock", OV.error ? appT("m093") : appT("m094"), OV.error ? ovRetry() : "", true); }
function paintOverviewKpis() {
  const el = $("#ov-kpis"); if (!el) return;
  const d = OV.data;
  el.innerHTML = kpi(d && d.unmeasured_plays ? appT("m095") : appT("m096"), d ? (!d.listen_ms && d.unmeasured_plays ? appT("m097") : fmtListen(d.listen_ms)) : "—", "amber", "clock") +
    kpi(appT("m098"), d ? d.track_count : "—", "amber", "music") +
    kpi(appT("m099"), d ? d.album_count : "—", "amber", "disc") +
    kpi(appT("m100"), d ? d.artist_count : "—", "amber", "music");
  const note = $("#ov-stat-note");
  if (note) note.innerHTML = OV.error ? `<span class="txt-gold">${appH("m101", { message: OV.error })}</span> ${ovRetry()}` :
    d ? `${appH("listeningScope", { timezone: d.timezone ? " (" + d.timezone + ")" : "", period: OV.days === 1 ? appT("m076") : appT("listeningDays", { count: OV.days }) })}${d.unmeasured_plays > 0 ? `<span class="ov-unmeasured">${appH("m104", { value: d.unmeasured_plays })}</span>` : ""}` : appH("m094");
}
function paintOvTastes() {
  const genres = $("#ov-genres"), artists = $("#ov-artists"); if (!genres || !artists) return;
  const gh = secHead(icon("disc", 17) + appH("m108"), `<span class="hint">${appH("m109")}</span>`);
  const ah = secHead(icon("music", 17) + appH("m110"), `<span class="hint">${appH("m111")}</span>`);
  const d = OV.data;
  if (!d) { genres.innerHTML = gh + ovStatEmpty(); artists.innerHTML = ah + ovStatEmpty(); return; }
  const nonnegative = v => Math.max(0, Number(v) || 0);
  const sorted = d.genres.slice().sort((a, b) => nonnegative(b.listen_ms) - nonnegative(a.listen_ms));
  const known = sorted.filter(g => g.name && !["未分类", "未知"].includes(g.name));
  const unknown = sorted.filter(g => !g.name || ["未分类", "未知"].includes(g.name)).reduce((n, g) => n + nonnegative(g.listen_ms), 0);
  const total = sorted.reduce((n, g) => n + nonnegative(g.listen_ms), 0);
  const segments = known.slice(0, 5).map(g => ({ name: g.name, ms: nonnegative(g.listen_ms) }));
  const other = known.slice(5).reduce((n, g) => n + nonnegative(g.listen_ms), 0);
  if (other) segments.push({ get name() { return appT("m112"); }, ms: other });
  if (unknown) segments.push({ get name() { return appT("m113"); }, ms: unknown, unknown: true });
  const colors = ["#f6a821", "#d9bd7b", "#a79062", "#817658", "#c78641", "#6b7280"];
  genres.innerHTML = gh + (total ? `<div class="ov-genre-bar" aria-label="${appH("m114")}">${segments.map((g, i) => `<span style="width:${g.ms / total * 100}%;background:${g.unknown ? "#485363" : colors[i % colors.length]}" title="${esc(g.name)} ${appFixed(g.ms / total * 100, 1)}%"></span>`).join("")}</div><div class="ov-genre-legend">${segments.map((g, i) => `<div><i style="background:${g.unknown ? "#485363" : colors[i % colors.length]}"></i><span class="truncate">${esc(g.name)}</span><b class="mono">${appFixed(g.ms / total * 100, 1)}%</b></div>`).join("")}</div>` : empty("disc", d.unmeasured_plays ? appT("m115") : appT("m116"), "", true));
  const rank = d.artists.slice().sort((a, b) => nonnegative(b.listen_ms) - nonnegative(a.listen_ms)).filter(a => nonnegative(a.listen_ms) > 0).slice(0, 5);
  artists.innerHTML = ah + (rank.length ? `<ol class="ov-rank">${rank.map((a, i) => `<li><span class="ov-rank-num mono">${p2(i + 1)}</span><div class="ov-rank-main"><div><span class="truncate">${esc(a.name || appT("m117"))}</span><span class="ov-rank-value">${appH("m118", { value: fmtListen(a.listen_ms), count: nonnegative(a.track_count) })}</span></div><div class="ov-rank-track"><span style="width:${nonnegative(a.listen_ms) / nonnegative(rank[0].listen_ms) * 100}%"></span></div></div></li>`).join("")}</ol>` : empty("music", d.unmeasured_plays ? appT("m119") : appT("m120"), "", true));
}
function ovAlbumHead(title, kind, more, count) {
  return secHead(icon(kind === "recent" ? "clock" : "disc", 17) + " " + esc(title),
    `${count > 8 ? `<button class="btn xs ghost" data-act="ov-more" data-kind="${kind}" aria-expanded="${more}">${more ? appH("m121") : appH("m122")}</button>` : ""}${kind === "recent" ? "" : `<button class="btn xs ghost" data-act="ov-goto" data-tab="albums">${appH("m066")} ${icon("chevR", 12)}</button>`}`);
}
function ovAlbumCard(g, info = {}) {
  const historical = info.history === true, missing = !g;
  const album = historical ? info.album : g.album, artist = historical ? info.artist : g.artist;
  const hash = historical ? info.cover_hash : albumCoverHash(g);
  const playable = !missing && g.tracks.some(t => t.decodable);
  const key = g ? esc(albumKey(g)) : "";
  const count = appT(historical ? "listenedTracks" : "m151", { count: historical ? info.track_count : g.tracks.length });
  const unavailable = !OV.loaded ? appT("m125") : OV.tracksError ? appT("m126") : appT("m127");
  return `<article class="ov-album">
    <button class="ov-album-open" ${missing ? 'disabled aria-disabled="true"' : `data-act="album-open" data-ak="${key}"`} ${missing ? `title="${esc(unavailable)}"` : ""} aria-label="${appH("m130", { value: album || appT("m128") })}">
      <span class="al-cover">${hash ? `<img class="al-img" src="/api/v1/covers/${esc(hash)}" alt="" loading="lazy">` : icon("disc", 30)}<span class="al-cnt mono">${esc(count)}</span></span>
      <span class="al-name truncate">${esc(album || appT("m128"))}</span><span class="al-artist truncate">${esc(artist || appT("m117"))}</span>
    </button>
    <div class="ov-album-foot"><span class="hint truncate" title="${esc(info.time ? fmtTime(info.time) : "")}">${info.time ? ago(info.time) : ""}</span><button class="btn xs ghost" data-act="album-push" data-ak="${key}" ${playable ? "" : "disabled"} title="${missing ? esc(unavailable) : appH("m129")}" aria-label="${appH("m131", { value: album || appT("m128") })}">${icon("send", 12)}</button></div>
  </article>`;
}
function paintOvRecent() {
  const el = $("#ov-recent"); if (!el) return;
  const list = OV.data ? OV.data.recent_albums.filter(g => hit(g.artist, g.album)) : [];
  const head = ovAlbumHead(appT("m132"), "recent", OV.recentMore, list.length);
  if (!OV.data) { el.innerHTML = head + ovStatEmpty(); return; }
  const groups = new Map(OV.loaded && !OV.tracksError ? albumGroups().map(g => [albumKey(g), g]) : []);
  const note = `<p class="ov-note">${appH("m134")}${OV.tracksError ? appH("m133") + " " + ovRetry("ov-library-retry") : ""}</p>`;
  el.innerHTML = head + note + (list.length ? `<div class="ov-album-row ${OV.recentMore ? "expanded" : ""}">${list.slice(0, OV.recentMore ? 50 : 8).map(a => ovAlbumCard(groups.get(albumKey(a)), Object.assign({}, a, { history: true, time: a.last_played_at }))).join("")}</div>` : empty("disc", S.search ? appT("m135") : appT("m136"), "", true));
}
function ovAddedAlbums(groups) {
  return groups.map(g => {
    const times = g.tracks.map(t => ts(t.added_at));
    return { g, added: times.every(t => t > 0) ? Math.min(...times) : 0 };
  }).sort((a, b) => b.added - a.added || albumKey(a.g).localeCompare(albumKey(b.g), "zh"));
}
function paintOvAlbums() {
  const el = $("#ov-albums"); if (!el) return;
  const all = ovAddedAlbums(albumGroups());
  const unknown = all.filter(a => !a.added).length;
  const list = all.filter(a => a.added && hit(a.g.artist, a.g.album));
  const head = ovAlbumHead(appT("m137"), "added", OV.addedMore, list.length);
  if (!OV.loaded || OV.tracksError) { el.innerHTML = head + empty(OV.tracksError ? "alert" : "disc", OV.tracksError ? appT("m138") : appT("m139"), OV.tracksError ? ovRetry("ov-library-retry") : "", true); return; }
  const note = `<p class="ov-note">${appH("m141")}${unknown ? appH("m140", { count: unknown }) : ""}</p>`;
  el.innerHTML = head + note + (list.length ? `<div class="ov-album-row ${OV.addedMore ? "expanded" : ""}">${list.slice(0, OV.addedMore ? list.length : 8).map(a => ovAlbumCard(a.g, { time: a.added })).join("")}</div>` : empty("disc", S.search ? appT("m142") : unknown ? appT("m143") : appT("m144"), `<button class="btn xs ghost" data-act="ov-goto" data-tab="albums">${appH("m145")}</button>`, true));
}
function paintOvPlaylists() {
  const el = $("#ov-playlists"); if (!el) return;
  const head = secHead(icon("layers", 16) + appH("m146"), `<button class="btn xs ghost" data-act="pl-new">${icon("plus", 12)} ${appH("m147")}</button><button class="btn xs ghost" data-act="ov-goto" data-tab="playlists">${appH("m148")} ${icon("chevR", 12)}</button>`);
  const list = S.playlists.filter(p => hit(p.name));
  el.innerHTML = head + (!OV.loaded ? `<span class="hint">${appH("m149")}</span>` : OV.playlistsError ? `<span class="hint">${appH("m150")}</span> ${ovRetry("ov-library-retry")}` :
    `<div class="ov-playlist-links">${list.length ? list.slice(0, 6).map(p => `<button class="btn ghost sm" data-act="pl-open" data-id="${esc(p.id)}">${icon("layers", 13)}<span class="truncate">${esc(p.name)}</span><span class="hint">${appH("m151", { count: p.count })}</span></button>`).join("") : `<span class="hint">${S.search ? appH("m152") : appH("m153")}</span>`}</div>`);
}
// Keep focused controls mounted while live playback fields update.
const PB = {
  id: "", chosen: false, nodes: [], loaded: false, nodesError: false, nodesAt: 0,
  seq: 0, busy: null, fresh: false, trackID: "", track: null, dragging: false, volDrag: false,
  seekContext: null, seekPending: 0, volPending: 0, devicePending: 0
};
function playerBarShell() {
  return `<section class="player-bar" id="player-bar" aria-label="${appH("m154")}">
    <div class="player-bar-meta"><div id="pb-cover"></div><div class="player-bar-text"><div class="player-bar-title truncate" id="pb-title">${appH("m155")}</div><div class="player-bar-sub truncate" id="pb-sub">${appH("m156")}</div><div class="player-bar-state" id="pb-state" role="status">${appH("m157")}</div></div>${typeof favoritePlayerShell === "function" ? favoritePlayerShell() : ""}</div>
    <div class="player-bar-transport"><button class="btn ghost" id="pb-prev" data-act="queue-prev" aria-label="${appH("m158")}" title="${appH("m158")}" disabled>${icon("chevL", 17)}</button><button class="btn primary pc-play" id="pb-pause" data-act="play-pause" aria-label="${appH("m159")}" title="${appH("m159")}" disabled>${icon("play", 18)}</button><button class="btn ghost" id="pb-next" data-act="queue-next" aria-label="${appH("m160")}" title="${appH("m160")}" disabled>${icon("chevR", 17)}</button></div>
    <div class="player-bar-tools" tabindex="0" aria-label="${appH("m161")}">
      <div class="player-bar-seek"><span id="pb-elapsed" class="mono">0:00</span><input id="pb-seek" type="range" min="0" max="0" step="500" value="0" data-change="pb-seek" aria-label="${appH("m162")}" disabled><span id="pb-total" class="mono">0:00</span></div>
      <label class="player-bar-volume" title="${appH("m163")}">${icon("speaker", 15)}<input id="pb-volume" type="range" min="0" max="100" step="1" value="100" data-change="pb-volume" aria-label="${appH("m163")}" disabled><span id="pb-volume-value" class="mono">100</span></label>
      <label class="player-bar-select"><span id="pb-node-label">${appH("m164")}</span><select id="pb-node" class="inp" data-change="pb-node" aria-label="${appH("m165")}"><option value="">${appH("m086")}</option></select></label>
      <label class="player-bar-select"><span id="pb-device-label">${appH("m166")}</span><select id="pb-device" class="inp mono" data-change="pb-device" aria-label="${appH("m167")}" title="${appH("m168")}" disabled><option value="default">default</option></select></label>
      <button class="btn xs ghost" id="pb-console" data-act="player-open" title="${appH("m169")}" disabled>${icon("layers", 14)} <span id="pb-console-label">${appH("m170")}</span></button>
    </div>
  </section>`;
}
function initPlayerBar() {
  $("#player-bar-root").innerHTML = playerBarShell();
  new ResizeObserver(entries => {
    document.documentElement.style.setProperty("--player-bar-height", Math.ceil(entries[0].target.getBoundingClientRect().height) + "px");
  }).observe($("#player-bar"));
  poll("playback", refreshPlayerBar, 1000, true);
}
function selectPlayerBar(id) {
  if (PLY.id && $("#ply-body") && PLY.id !== id) {
    if (id) return openPlayer(id);
    closeModal();
  }
  PB.chosen = true;
  if (id !== PB.id) {
    PB.id = id; PB.seq++; PB.busy = null; PB.fresh = false; PB.nodesAt = 0;
    PB.trackID = ""; PB.track = null;
    PB.dragging = PB.volDrag = false; PB.seekContext = null;
    PB.seekPending = PB.volPending = PB.devicePending = 0;
  }
  paintPlayerBar();
  return refreshPlayerBar();
}
function refreshPlayerBar() {
  if (!$("#player-bar")) return Promise.resolve();
  if (PB.busy) return PB.busy;
  const seq = PB.seq;
  const work = (async () => {
    if (!PB.loaded || Date.now() - PB.nodesAt >= 5000) {
      const nodes = await api("GET", "/nodes").catch(() => null);
      if (seq !== PB.seq) return;
      PB.loaded = true; PB.nodesError = !Array.isArray(nodes); PB.nodesAt = Date.now();
      if (Array.isArray(nodes)) PB.nodes = nodes;
      if (!PB.chosen && !PB.nodesError && PB.nodes.length) {
        PB.id = (PB.nodes.find(n => n.status === "online") || PB.nodes[0]).node_id;
        PB.chosen = true;
      }
    }
    const id = PB.id, n = PB.nodes.find(n => n.node_id === id);
    if (!id || !n || n.status !== "online" || PB.nodesError) {
      PB.fresh = false;
    } else {
      const [p, q] = await Promise.all([
        api("GET", `/nodes/${enc(id)}/play`).catch(() => null),
        api("GET", `/nodes/${enc(id)}/queue`).catch(() => null)
      ]);
      if (seq !== PB.seq || id !== PB.id) return;
      PB.fresh = !!p;
      S.play[id] = p || { connected: false, playing: false };
      S.nodePlay[id] = S.play[id];
      S.nodeQueue[id] = q;
      const trackID = p && p.track_id || "";
      if (trackID !== PB.trackID) {
        PB.trackID = trackID; PB.track = null;
        const cached = S.tracks.find(t => t.id === trackID);
        const track = cached || (trackID && !p.cover_hash ? await api("GET", `/tracks/${enc(trackID)}`).catch(() => null) : null);
        if (seq !== PB.seq || id !== PB.id) return;
        PB.track = track;
      }
    }
    paintPlayerBar();
    if (PLY.id === id && $("#ply-body")) paintPlayerLive();
  })().finally(() => { if (PB.busy === work) PB.busy = null; });
  PB.busy = work;
  return work;
}
function playerBarConnected() {
  const n = PB.nodes.find(n => n.node_id === PB.id);
  return !!(PB.loaded && !PB.nodesError && PB.fresh && n && n.status === "online" && (S.play[PB.id] || {}).connected);
}
function paintPlayerBar() {
  const root = $("#player-bar"); if (!root) return;
  const id = PB.id, n = PB.nodes.find(n => n.node_id === id), p = S.play[id] || {};
  const connected = playerBarConnected(), active = connected && (p.playing || p.state === "paused");
  const t = active && p.track_id ? S.tracks.find(t => t.id === p.track_id) || (PB.trackID === p.track_id ? PB.track : null) : null;
  if (typeof paintPlayerFavorite === "function") paintPlayerFavorite(active ? p.track_id || "" : "");
  const title = active ? p.title || (t && t.title) || appT("m171") : appT("m155");
  const artist = active ? p.artist || (t && t.artist) || appT("m117") : "";
  const album = active ? p.album || (t && t.album) || appT("m128") : "";
  const set = (selector, text) => { const el = $(selector); if (el && el.textContent !== text) el.textContent = text; };
  set("#pb-title", title);
  set("#pb-sub", active ? artist + " · " + album : appT("m156"));
  const status = !PB.loaded ? appT("m172") : PB.nodesError ? appT("m173") : !id ? (PB.nodes.length ? appT("m174") : appT("m175")) : !n ? appT("m176") : n.status !== "online" ? appT("m177") : !PB.fresh ? appT("m178") : !connected ? appT("m179") : p.playing ? appT("m180") : p.state === "paused" ? appT("m181") : appT("m182");
  set("#pb-state", status);
  root.classList.toggle("is-playing", !!(active && p.playing));
  const cover = $("#pb-cover"), hash = active ? (t && t.cover_hash) || p.cover_hash : "";
  const sig = JSON.stringify([album, artist, hash]);
  if (cover && cover.dataset.sig !== sig) { cover.innerHTML = coverArt(album, artist, "thumb", 22, hash); cover.dataset.sig = sig; }
  const q = S.nodeQueue[id], tracks = (q && q.tracks) || [], pos = (q && q.position) || 0;
  const button = (sel, disabled) => { const b = $(sel); if (b) { b.disabled = disabled; b.dataset.id = id; } return b; };
  button("#pb-prev", !connected || !tracks.length);
  button("#pb-next", !connected || !tracks.length || pos >= tracks.length - 1);
  const pp = button("#pb-pause", !active);
  const ppLabel = active && p.playing ? appT("m183") : appT("m159");
  if (pp && pp.title !== ppLabel) { pp.innerHTML = icon(active && p.playing ? "pause" : "play", 18); pp.title = ppLabel; pp.setAttribute("aria-label", ppLabel); }
  button("#pb-console", !connected);
  const rate = p.rate || (t && t.rate) || 0;
  const total = active ? Math.max(0, p.duration_ms || (t && t.duration_ms) || 0) : 0;
  const elapsed = active && rate && p.pts ? clamp(p.pts / rate * 1000, 0, total || Infinity) : 0;
  const seek = $("#pb-seek"), vol = $("#pb-volume");
  if (seek) {
    seek.disabled = !active || !rate || !total;
    if (!PB.dragging && !PB.seekPending && document.activeElement !== seek) { seek.max = String(Math.round(total)); seek.value = String(Math.round(elapsed)); set("#pb-elapsed", fmtDur(elapsed)); }
    seek.setAttribute("aria-valuetext", fmtDur(Number(seek.value)) + " / " + fmtDur(total));
    set("#pb-total", fmtDur(total));
  }
  if (vol) {
    vol.disabled = !active;
    if (!PB.volDrag && !PB.volPending && document.activeElement !== vol) { vol.value = String(clamp(p.volume == null ? 100 : Number(p.volume), 0, 100)); set("#pb-volume-value", appN(Number(vol.value))); }
    vol.setAttribute("aria-valuetext", appN(Number(vol.value) / 100, { style: "percent" }));
  }
  const node = $("#pb-node");
  ovSyncSelect(node, ovNodeOptions(id, false, PB.nodes), id);
  if (node) node.disabled = PB.dragging || PB.volDrag || !!(PB.seekPending || PB.volPending || PB.devicePending);
  const device = $("#pb-device"), dv = nodePlayDevs(p);
  if (device) {
    if (!PB.devicePending) ovSyncSelect(device, nodePlayOpts(dv.devs, dv.cur, dv.names), dv.cur);
    device.disabled = !connected || !!PB.devicePending;
    device.title = (active && p.alsa_device ? appT("m184") + p.alsa_device + " · " : "") + appT("m185");
  }
}
function playerBarBeginRange(el) {
  if (el.disabled) return;
  if (el.id === "pb-seek" && !PB.dragging) {
    const p = S.play[PB.id] || {};
    PB.seekContext = { id: PB.id, track: p.track_id, session: p.session_id };
    PB.dragging = true;
  } else if (el.id === "pb-volume") PB.volDrag = true;
}
async function playerBarRangeChange(el, volume) {
  const id = PB.id, seq = PB.seq, field = volume ? "volPending" : "seekPending";
  const value = Number(el.value), p = S.play[id] || {}, context = PB.seekContext;
  if (volume) PB.volDrag = false; else { PB.dragging = false; PB.seekContext = null; }
  if (!playerBarConnected() || !(p.playing || p.state === "paused") || el.disabled) { paintPlayerBar(); return; }
  if (!volume && (!context || context.id !== id || context.track !== p.track_id || context.session !== p.session_id)) { toast(appT("m186"), "warn"); el.blur(); paintPlayerBar(); return; }
  PB[field]++;
  paintPlayerBar();
  try {
    await api("POST", `/nodes/${enc(id)}/${volume ? "volume" : "seek"}`, volume ? { volume: clamp(value, 0, 100) } : { position_ms: Math.max(0, value) });
  } catch (e) { toast(e.message, "err"); el.blur(); }
  finally {
    if (seq === PB.seq) { PB[field] = Math.max(0, PB[field] - 1); await refreshPlayerBar(); }
  }
}
async function playerBarDeviceChange(el) {
  if (!playerBarConnected() || el.disabled) return;
  const id = PB.id, seq = PB.seq;
  PB.devicePending++;
  paintPlayerBar();
  try { await api("POST", `/nodes/${enc(id)}/device`, { device: el.value }); toast(appT("m187"), "ok"); }
  catch (e) { toast(e.message, "err"); el.blur(); }
  finally { if (seq === PB.seq) { PB.devicePending--; await refreshPlayerBar(); } }
}
function paintSysStrip() {
  const el = $("#ov-sys"); if (!el) return;
  const m = S.metrics;
  const running = S.jobs.filter(jobActive).length;
  let pkgs = 0; S.nodes.forEach(n => { const o = S.nodeOs[n.node_id]; if (o && Array.isArray(o.packages)) pkgs += o.packages.length; });
  const open = S.alerts.filter(a => a.status === "open").length;
  const cpu = m ? Number(m.cpu_percent) || 0 : null;
  const mem = m ? Number(m.mem_mb) || 0 : null;
  el.innerHTML = secHead(icon("cpu", 17) + appH("m188"),
    `${m ? badge("v" + (m.version || "—"), "gold") : ""}<button class="btn xs ghost" data-act="ov-sys-toggle">${S.sysOpen ? appH("m121") : appH("m189")} ${icon(S.sysOpen ? "arrowUp" : "arrowDown", 12)}</button>`) +
    `<div class="sys-strip">
      <span class="sys-item">${icon("cpu", 13)} CPU <b class="mono">${cpu != null ? appFixed(cpu, 1) + " %" : "—"}</b></span>
      <span class="sys-item">${icon("database", 13)} ${appH("m190")} <b class="mono">${mem != null ? appFixed(mem, 0) + " MB" : "—"}</b></span>
      <span class="sys-item">${icon("send", 13)} ${appH("m191")} <b class="mono">${esc(typeof (running) === "number" ? appN(running) : (running))}</b></span>
      <span class="sys-item">${icon("download", 13)} ${appH("m192")} <b class="mono">${esc(typeof (pkgs) === "number" ? appN(pkgs) : (pkgs))}</b></span>
      <button class="sys-item link" data-act="bell">${icon("bell", 13)} ${appH("m193")} <b class="mono ${open ? "txt-gold" : ""}">${esc(typeof (open) === "number" ? appN(open) : (open))}</b></button>
    </div>` +
    (S.sysOpen ? `<div class="divider"></div>` + sysDetail() : "");
}
function sysDetail() {
  const m = S.metrics;
  if (!m) return empty("cpu", appT("m194"), "", true);
  const cpu = Number(m.cpu_percent) || 0, mem = Number(m.mem_mb) || 0;
  return meter(appT("m195"), cpu, appFixed(cpu, 1) + " %", "cyan") +
    `<div class="kv mt12">` +
    kvRow(appT("m190"), `<span class="mono">${appFixed(mem, 0)} MB</span>`) +
    kvRow("Goroutines", `<span class="mono">${esc(typeof (m.goroutines ?? "—") === "number" ? appN(m.goroutines ?? "—") : (m.goroutines ?? "—"))}</span>`) +
    kvRow(appT("m196"), `<span class="mono">${fmtBytes(m.db_bytes)}</span>`) +
    kvRow(appT("m197"), `<span class="mono">${fmtUp(m.uptime_s)}</span>`) +
    kvRow(appT("m198"), esc(sigModeZh(m.signature_mode))) +
    kvRow(appT("m199"), m.play_gate ? badge(appT("m073"), "ok") : badge(appT("m074"), "muted")) +
    `</div>`;
}

/* ---- album aggregation (shared by overview & library) ---- */
function albumGroups() {
  const map = new Map();
  for (const t of S.tracks) {
    const artist = t.artist || "", album = t.album || "";
    const key = JSON.stringify([artist, album]);
    let g = map.get(key);
    if (!g) { g = { artist, album, tracks: [], ms: 0 }; map.set(key, g); }
    g.tracks.push(t); g.ms += t.duration_ms || 0;
  }
  const out = [...map.values()];
  out.sort((a, b) => String(a.artist).localeCompare(String(b.artist), "zh") || String(a.album).localeCompare(String(b.album), "zh"));
  return out;
}
function filteredAlbums() { return albumGroups().filter(g => hit(g.artist, g.album)); }
function albumKey(g) { return JSON.stringify([g.artist || "", g.album || ""]); }
function albumByKey(key) { return albumGroups().find(g => albumKey(g) === key); }
function albumCard(g, i, sm) {
  return `<div class="album-card ${sm ? "sm" : ""}" data-act="album-open" data-ak="${esc(albumKey(g))}" role="button" tabindex="0">
    <div class="al-cover">${albumCoverHash(g) ? `<img class="al-img" src="/api/v1/covers/${esc(albumCoverHash(g))}" alt="" loading="lazy">` : icon("disc", sm ? 22 : 30)}<span class="al-cnt mono">${appH("m151", { count: appN(g.tracks.length) })}</span></div>
    <div class="al-name truncate">${esc(g.album || appT("m128"))}</div>
    <div class="al-artist truncate">${esc(g.artist || appT("m117"))}</div>
    ${sm ? "" : `<div class="al-meta mono">${fmtDur(g.ms)}</div>`}
  </div>`;
}

/* ================= NODES ================= */

/* ---------- bulk operations ---------- */
async function bulkAuth(revoke) {
  const ids = Array.from(S.sel); if (!ids.length) return;
  let ok = 0, fail = 0;
  for (const id of ids) { try { await api("POST", `/nodes/${enc(id)}/${revoke ? "revoke" : "authorize"}`); ok++; } catch (e) { fail++; } }
  toast(appT("m202", { value: revoke ? appT("m060") : appT("m200"), count: ok, value2: fail ? appT("m201", { count: fail }) : "" }), fail ? "warn" : "ok");
  S.sel.clear(); await refreshCurrent();
}
async function bulkDelete() {
  const ids = Array.from(S.sel); if (!ids.length) return;
  const okc = await confirmDlg({ title: appT("m203"), message: appT("m204", { count: appN(ids.length) }), okText: appT("m205") });
  if (!okc) return;
  await deleteNodes(ids);
}
async function deleteNodes(ids) {
  const deleted = [], failed = [];
  for (const id of ids) {
    try { const r = await api("DELETE", `/nodes/${enc(id)}`); deleted.push((r && r.node) || S.nodes.find(n => n.node_id === id) || { node_id: id }); }
    catch (e) { failed.push(`${shortId(id)}：${e.message}`); }
  }
  if (failed.length) toast(appT("m206") + failed.join("；"), "err");
  if (deleted.length) {
    toast(appT("m207", { count: appN(deleted.length) }), "ok", {
      undo: async () => {
        let ok = 0; for (const node of deleted) { try { await api("POST", "/nodes/restore", { node }); ok++; } catch (e) { /* best-effort */ } }
        toast(appT("m208", { count: ok }), ok ? "ok" : "err"); refreshCurrent();
      }, undoText: appT("m060")
    });
  }
  S.sel.clear(); closeModal(); closeDrawer(); await refreshCurrent();
}

/* ================= ADD-NODE WIZARD ================= */
const WZ = { step: 1, alias: "", group: "", arch: "amd64", distro: "debian", ttl: 1800, note: "", token: "", expiresAt: 0, nodeId: "", node: null };
const ANDROID_APK = "https://github.com/Eyebright/Zephyr-HiFi-Kit/releases/download/v0.58/Zephyr-HiFi-Kit-terminal-0.58.apk";
function installCmd() {
  if (WZ.arch === "android") return appT("m209", { id: ANDROID_APK, value: location.origin, value2: WZ.token });
  return `curl -fsSL ${location.origin}/dl/agent-install.sh | sudo bash -s -- --server ${location.origin} --token ${WZ.token}`;
}
function openWizard() {
  Object.assign(WZ, { step: 1, alias: "", group: "", arch: "amd64", distro: "debian", ttl: 1800, note: "", token: "", expiresAt: 0, nodeId: "", node: null });
  renderWizard();
}
function wzStepsHtml() {
  const labels = [appT("m210"), appT("m211"), appT("m033")];
  return `<div class="wz-steps">` + labels.map((s, i) => {
    const n = i + 1, cls = WZ.step === n ? "active" : WZ.step > n ? "done" : "";
    return `<div class="wz-step ${cls}"><span class="w-dot">${WZ.step > n ? icon("check", 12) : n}</span><span class="w-lbl">${esc(s)}</span></div>` + (n < 3 ? `<div class="wz-line"></div>` : "");
  }).join("") + `</div>`;
}
function renderWizard() {
  let body = "", foot = "";
  if (WZ.step === 1) {
    body = wzStepsHtml() + wzStep1();
    foot = `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><button class="btn primary" data-act="wz-next">${appH("m212")} ${icon("chevR", 14)}</button>`;
  } else if (WZ.step === 2) {
    body = wzStepsHtml() + wzStep2();
    foot = `<button class="btn ghost" data-act="wz-back">${icon("chevL", 14)} ${appH("m213")}</button><button class="btn ghost" data-act="modal-close">${appH("m063")}</button>`;
  } else {
    body = wzStepsHtml() + wzStep3();
    foot = `<button class="btn primary" data-act="modal-close">${appH("m033")}</button>`;
  }
  openModal(`${icon("plus", 18)} ${appH("m214")}`, body, foot, { narrow: true, onClose: () => { stopPoll("wz:token"); stopPoll("wz:cd"); } });
}
function wzStep1() {
  const opt = (v, l, cur) => `<option value="${v}" ${v === cur ? "selected" : ""}>${esc(l)}</option>`;
  return `<div id="wz-err"></div>
    <div class="f-row">
      <div class="field"><label>${appH("m219")}</label><input id="wz-alias" class="inp" placeholder="${appH("m220")}" value="${esc(WZ.alias)}" maxlength="60"></div>
      <div class="field"><label>${appH("m221")}</label><input id="wz-group" class="inp" placeholder="${appH("m222")}" value="${esc(WZ.group)}" maxlength="40"></div>
    </div>
    <div class="f-row">
      <div class="field"><label>${appH("m223")}</label><select id="wz-arch" class="inp" data-change="wz-arch">${opt("amd64", "amd64 (x86_64)", WZ.arch)}${opt("arm64", "arm64 (aarch64)", WZ.arch)}${opt("armhf", "armhf", WZ.arch)}${opt("armv7hf", "armv7hf", WZ.arch)}${opt("android", "Android (arm64)", WZ.arch)}</select></div>
      <div class="field"><label>${appH("m224")}</label><select id="wz-distro" class="inp">${opt("debian", "Debian", WZ.distro)}${opt("ubuntu", "Ubuntu", WZ.distro)}${opt("raspberry-pi-os", "Raspberry Pi OS", WZ.distro)}${opt("armbian", "Armbian", WZ.distro)}${opt("android", "Android", WZ.distro)}${opt("other", appT("m112"), WZ.distro)}</select></div>
    </div>
    <div class="f-row">
      <div class="field"><label>${appH("m225")}</label><select id="wz-ttl" class="inp">${opt("600", appT("m215"), String(WZ.ttl))}${opt("1800", appT("m216"), String(WZ.ttl))}${opt("3600", appT("m217"), String(WZ.ttl))}${opt("86400", appT("m218"), String(WZ.ttl))}</select></div>
      <div class="field"><label>${appH("m226")}</label><input id="wz-note" class="inp" placeholder="${appH("m227")}" value="${esc(WZ.note)}" maxlength="120"></div>
    </div>
    <div class="f-hint">${appH("m228")}</div>`;
}

function wzStep2() {
  const android = WZ.arch === "android";
  const installPanel = android
    ? `<div class="field"><label>${appH("m229")}</label>
      <div class="f-hint">${appH("m230")}</div>
      <a class="btn sm block" href="${ANDROID_APK}" target="_blank" rel="noopener">${icon("download", 14)} ${appH("m231")}</a>
      <div class="cmd-box" style="white-space:pre-wrap"><span id="wz-cmd">${esc(installCmd())}</span></div>
      <button class="btn sm block" data-act="wz-copy-cmd">${icon("copy", 14)} ${appH("m232")}</button>
    </div>`
    : `<div class="field"><label>${appH("m233")}</label>
      <div class="cmd-box"><span class="c-prompt">$</span><span id="wz-cmd">${esc(installCmd())}</span></div>
      <button class="btn sm block" data-act="wz-copy-cmd">${icon("copy", 14)} ${appH("m234")}</button>
    </div>`;
  return `<div class="field"><label>${appH("m235")}</label>
      <div class="token-box"><span class="tok" id="wz-token">${esc(WZ.token)}</span><button class="btn xs" data-act="wz-copy-token">${icon("copy", 13)} ${appH("m236")}</button></div>
      <div class="f-hint">${appH("m237")} <span class="wz-count" id="wz-cd">--:--</span> ${appH("m238")}</div>
    </div>
    ${installPanel}
    <div class="waiting"><span class="w-pulse"></span><div style="flex:1"><div class="w-txt">${appH("m239")}</div><div class="w-sub">${appH("m240")}</div></div><span class="spin"></span></div>`;
}
function wzStep3() {
  const n = WZ.node || { node_id: WZ.nodeId, alias: WZ.alias };
  return `<div class="banner ok">${icon("check", 16)}<div><b>${esc(n.alias || shortId(n.node_id))}</b> ${appH("m241")}</div></div>
    <div class="tiles c2">
      <button class="tile" style="--tc:var(--green)" data-act="wz-auth" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("shield", 16)}</span><span class="t-name">${appH("m242")}</span><span class="t-sub">${appH("m243")}</span></button>
      <button class="tile" style="--tc:var(--amber)" data-act="wz-config" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("sliders", 16)}</span><span class="t-name">${appH("m244")}</span><span class="t-sub">${appH("m245")}</span></button>
      <button class="tile" style="--tc:var(--cyan)" data-act="wz-console" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("terminal", 16)}</span><span class="t-name">${appH("m246")}</span><span class="t-sub">${appH("m247")}</span></button>
      <button class="tile" style="--tc:var(--violet)" data-act="wz-detail" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("eye", 16)}</span><span class="t-name">${appH("m248")}</span><span class="t-sub">${appH("m249")}</span></button>
    </div>`;
}
async function wzNext() {
  WZ.alias = val("wz-alias").trim(); WZ.group = val("wz-group").trim();
  WZ.arch = val("wz-arch"); WZ.distro = val("wz-distro"); WZ.ttl = Number(val("wz-ttl")) || 1800; WZ.note = val("wz-note").trim();
  if (WZ.arch === "android") WZ.distro = "android";
  const errEl = $("#wz-err");
  const fail = m => { if (errEl) errEl.innerHTML = `<div class="f-err mb12">${esc(m)}</div>`; };
  if (!WZ.alias) return fail(appT("m250"));
  if (S.nodes.some(n => (n.alias || "").trim().toLowerCase() === WZ.alias.toLowerCase())) return fail(appT("m251"));
  try {
    const r = await api("POST", "/nodes/token", { ttl_seconds: WZ.ttl });
    WZ.token = r.token; WZ.expiresAt = ts(r.expires_at) || (Math.floor(Date.now() / 1000) + WZ.ttl);
    WZ.step = 2; renderWizard(); startWzPolls();
  } catch (e) { fail(e.message); }
}
function startWzPolls() { poll("wz:cd", tickWzCountdown, 1000, true); poll("wz:token", wzCheckToken, 2000); }
function tickWzCountdown() {
  const el = $("#wz-cd"); if (!el) return;
  const left = Math.max(0, WZ.expiresAt - Math.floor(Date.now() / 1000));
  el.textContent = `${p2(Math.floor(left / 60))}:${p2(left % 60)}`;
  el.classList.toggle("expired", left <= 0);
  if (left <= 0) {
    stopPoll("wz:token"); stopPoll("wz:cd");
    const w = $(".waiting");
    if (w) w.innerHTML = `${icon("clock", 18)}<div style="flex:1"><div class="w-txt txt-warn">${appH("m252")}</div><div class="w-sub">${appH("m253")}</div></div><button class="btn sm primary" data-act="wz-reissue">${appH("m254")}</button>`;
  }
}
async function wzReissue() {
  try {
    const r = await api("POST", "/nodes/token", { ttl_seconds: WZ.ttl });
    WZ.token = r.token; WZ.expiresAt = ts(r.expires_at) || (Math.floor(Date.now() / 1000) + WZ.ttl);
    renderWizard(); startWzPolls();
  } catch (e) { toast(e.message, "err"); }
}
async function wzCheckToken() {
  if (!WZ.token) return;
  let r; try { r = await api("GET", `/nodes/token/${enc(WZ.token)}`); } catch (e) { return; }
  if (r && r.used) {
    stopPoll("wz:token"); stopPoll("wz:cd");
    WZ.nodeId = r.node_id || WZ.nodeId;
    try { await api("PATCH", `/nodes/${enc(WZ.nodeId)}`, { alias: WZ.alias, group: WZ.group, note: WZ.note }); } catch (e) { /* alias best-effort */ }
    try { WZ.node = await api("GET", `/nodes/${enc(WZ.nodeId)}`); } catch (e) { WZ.node = { node_id: WZ.nodeId, alias: WZ.alias }; }
    WZ.step = 3; renderWizard();
    toast(appT("m255", { name: WZ.alias || shortId(WZ.nodeId) }), "ok");
    refreshCurrent();
  }
}

/* ---------- shared tile builder ---------- */
function tile(ic, name, sub, act, color, id) {
  return `<button class="tile" style="--tc:var(--${color})" data-act="${act}" ${id ? `data-id="${esc(id)}"` : ""}><span class="t-ic">${icon(ic, 16)}</span><span class="t-name">${esc(name)}</span><span class="t-sub">${esc(sub)}</span></button>`;
}
const CMD_ZH = { get "os-check"() { return appT("m256"); }, get "os-upgrade"() { return appT("m257"); }, get netdiag() { return appT("m258"); }, get "apt-install"() { return appT("m259"); }, get "apt-source"() { return appT("m260"); }, get "agent-upgrade"() { return appT("m261"); } };

/* ---- shared ALSA device picker (homepage and full player) ---- */
function nodePlayDevs(p) {
  const devs = Array.isArray(p.devices) ? p.devices.slice() : [];
  if (!devs.includes("default")) devs.unshift("default");
  const cur = p.pref_device || p.configured_device || "default";
  if (!devs.includes(cur)) devs.push(cur);
  const names = new Map((Array.isArray(p.device_info) ? p.device_info : []).filter(d => d && d.hw && d.name).map(d => [d.hw, d.name]));
  return { devs, cur, names };
}
function nodePlayOpts(devs, cur, names) {
  return devs.map(d => {
    const label = d === "default" ? appT("m262") : names?.get(d) ? names.get(d) + " · " + d : d;
    return `<option value="${esc(d)}" ${d === cur ? "selected" : ""}>${esc(label)}</option>`;
  }).join("");
}

/* ---- shared server-side playback queue actions ---- */
async function queueMove(id, dir) {
  try {
    await api("POST", `/nodes/${enc(id)}/queue/${dir}`);
    toast(dir === "next" ? appT("m263") : appT("m264"), "ok");
  } catch (e) { toast(e.message, "err"); }
  await queueAfter(id);
}
async function queueJump(id, idx) {
  try { await api("POST", `/nodes/${enc(id)}/queue/jump`, { index: idx }); toast(appT("m265"), "ok"); }
  catch (e) { toast(e.message, "err"); }
  await queueAfter(id);
}
async function queueRemove(id, idx) {
  const q = S.nodeQueue[id]; if (!q) return;
  const ids = (q.tracks || []).map(t => t.track_id);
  if (idx < 0 || idx >= ids.length) return;
  ids.splice(idx, 1);
  let pos = q.position || 0;
  if (idx < pos) pos--;
  if (pos >= ids.length) pos = ids.length - 1;
  if (pos < 0) pos = 0;
  try { S.nodeQueue[id] = await api("PUT", `/nodes/${enc(id)}/queue`, { track_ids: ids, position: pos }); toast(appT("m266"), "ok"); }
  catch (e) { toast(e.message, "err"); }
  paintNodePlay(id);
  await syncPlayUI();
}
async function queueClear(id) {
  try { S.nodeQueue[id] = await api("PUT", `/nodes/${enc(id)}/queue`, { track_ids: [], position: 0 }); toast(appT("m267"), "ok"); }
  catch (e) { toast(e.message, "err"); }
  paintNodePlay(id);
  await syncPlayUI();
}

/* ================= OS UPDATES ================= */
const OSC = { id: "", state: null, sel: new Set(), busy: false, cmdId: "", cmdType: "", tries: 0, log: "", err: "", result: "" };
async function openOsDrawer(id) {
  OSC.id = id; OSC.state = null; OSC.sel = new Set(); OSC.busy = false;
  OSC.cmdId = ""; OSC.cmdType = ""; OSC.tries = 0; OSC.log = ""; OSC.err = ""; OSC.result = "";
  openDrawer(appH("m268"), esc(id), empty("refresh", appT("m269"), "", true), "", { ctx: { kind: "os", id }, back: { act: "node-open", id } });
  try { OSC.state = await api("GET", `/nodes/${enc(id)}/osupdates`); }
  catch (e) { OSC.err = e.message; }
  if (drawerCtx && drawerCtx.kind === "os" && drawerCtx.id === id) repaintOs();
}
function osGuard() { return drawerCtx && drawerCtx.kind === "os" && drawerCtx.id === OSC.id; }
function repaintOs() { if (!osGuard()) return; setDrawerBody(osBody()); setDrawerFoot(osFoot()); }
function osBody() {
  const st = OSC.state || {}; const pkgs = st.packages || [];
  let h = "";
  if (OSC.err) h += `<div class="banner err">${icon("alert", 15)}<span>${esc(OSC.err)}</span></div>`;
  if (st.reboot_required) h += `<div class="banner warn">${icon("power", 15)}<span>${appH("m270")}</span></div>`;
  h += `<div class="kv mb12">` +
    kvRow(appT("m271"), st.last_check ? fmtTime(st.last_check) + ` <span class="txt-faint">${ago(st.last_check)}</span>` : appH("m272")) +
    kvRow(appT("m273"), `<span class="mono">${esc(typeof (pkgs.length) === "number" ? appN(pkgs.length) : (pkgs.length))}</span>`) + `</div>`;
  if (OSC.busy) h += `<div class="waiting"><span class="w-pulse"></span><div><div class="w-txt">${appH("m275", { value: CMD_ZH[OSC.cmdType] || appT("m274") })}</div><div class="w-sub">${appH("m276")}</div></div><span class="spin">${icon("refresh", 16)}</span></div>`;
  if (!OSC.busy && OSC.result) {
    h += `<div class="banner ${OSC.result === "ok" ? "ok" : "err"}">${icon(OSC.result === "ok" ? "check" : "alert", 15)}<span>${appH("commandResult", { command: CMD_ZH[OSC.cmdType] || appT("m274"), result: appT(OSC.result === "ok" ? "m021" : "m277") })}</span></div>`;
    if (OSC.log) h += `<div class="log-box mb12">${esc(OSC.log)}</div>`;
  }
  h += `<div class="d-sec"><span class="lbl">${appH("m278")}</span>`;
  if (!pkgs.length) h += empty("check", st.last_check ? appT("m279") : appT("m280"), "", true);
  else {
    const allSel = pkgs.every(p => OSC.sel.has(p.name));
    h += `<div class="tbl-wrap"><table class="tbl" style="min-width:0"><thead><tr><th style="width:34px"><label class="check"><input type="checkbox" data-change="os-pkg-all" ${allSel ? "checked" : ""} ${OSC.busy ? "disabled" : ""}><span class="box"></span></label></th><th>${appH("m281")}</th><th>${appH("m282")}</th><th>${appH("m283")}</th></tr></thead><tbody>` +
      pkgs.map(p => `<tr><td><label class="check"><input type="checkbox" data-change="os-pkg" data-name="${esc(p.name)}" ${OSC.sel.has(p.name) ? "checked" : ""} ${OSC.busy ? "disabled" : ""}><span class="box"></span></label></td><td class="cell-mono">${esc(p.name)}</td><td class="cell-mono sub">${esc(p.current || "—")}</td><td class="cell-mono txt-ok">${esc(p.candidate || "—")}</td></tr>`).join("") +
      `</tbody></table></div>`;
  }
  h += `</div>`;
  return h;
}
function osFoot() {
  const n = OSC.sel.size;
  return `<button class="btn ghost" data-act="os-check" ${OSC.busy ? "disabled" : ""}>${icon("refresh", 15)} ${appH("m256")}</button><div class="spacer"></div>` +
    `<button class="btn primary" data-act="os-upgrade" ${OSC.busy || !n ? "disabled" : ""}>${icon("arrowUp", 15)} ${appH("m284", { count: n })}</button>`;
}
async function runOsCommand(type) {
  if (OSC.busy) return;
  const packages = type === "os-upgrade" ? Array.from(OSC.sel) : [];
  if (type === "os-upgrade" && !packages.length) { toast(appT("m285"), "warn"); return; }
  if (type === "os-upgrade") {
    const ok = await confirmDlg({ title: appT("m286"), message: appT("m287", { id: OSC.id, count: appN(packages.length) }), okText: appT("m288"), danger: false });
    if (!ok) return;
  }
  OSC.busy = true; OSC.cmdType = type; OSC.result = ""; OSC.log = ""; OSC.err = ""; OSC.tries = 0; OSC.cmdId = "";
  repaintOs();
  try {
    const c = await api("POST", `/nodes/${enc(OSC.id)}/commands`, { type, params: { packages } });
    OSC.cmdId = (c && c.id) || "";
    poll("os:cmd", osPollCmd, 1500);
  } catch (e) {
    OSC.busy = false;
    OSC.err = e.status === 409 ? appT("m289") : e.message;
    repaintOs(); toast(OSC.err, "err");
  }
}
async function osPollCmd() {
  if (!osGuard() || !OSC.busy) { stopPoll("os:cmd"); return; }
  OSC.tries++;
  try {
    const cs = await api("GET", `/nodes/${enc(OSC.id)}/commands`);
    const list = Array.isArray(cs) ? cs : [];
    const c = list.find(x => x.id === OSC.cmdId) ||
      list.filter(x => x.type === OSC.cmdType).sort((a, b) => ts(b.created_at) - ts(a.created_at))[0];
    const st = cmdStatus(c);
    if (st === "ok" || st === "err") {
      stopPoll("os:cmd");
      OSC.busy = false; OSC.result = st;
      let log = (c && c.log) || "";
      if (!log && c && c.data) { try { log = JSON.stringify(typeof c.data === "string" ? JSON.parse(c.data) : c.data, null, 2); } catch (e) { log = String(c.data); } }
      OSC.log = log;
      if (st === "ok" && OSC.cmdType === "os-upgrade") OSC.sel.clear();
      try { OSC.state = await api("GET", `/nodes/${enc(OSC.id)}/osupdates`); OSC.err = ""; } catch (e) { /* 保留旧状态 */ }
      repaintOs();
      toast(appT("commandResult", { command: CMD_ZH[OSC.cmdType] || appT("m274"), result: appT(st === "ok" ? "m033" : "m034") }), st === "ok" ? "ok" : "err");
      refreshCurrent();
    } else if (OSC.tries > 160) {
      stopPoll("os:cmd"); OSC.busy = false;
      OSC.err = appT("m290"); repaintOs();
    }
  } catch (e) { /* 瞬时错误，继续轮询 */ }
}

/* ================= NETDIAG ================= */
const NETC = { id: "", state: null, busy: false, cmdId: "", tries: 0, err: "" };
async function openNetDrawer(id) {
  NETC.id = id; NETC.state = null; NETC.busy = false; NETC.cmdId = ""; NETC.tries = 0; NETC.err = "";
  openDrawer(appH("m258"), esc(id), empty("wifi", appT("m291"), "", true), "", { ctx: { kind: "net", id }, back: { act: "node-open", id } });
  try { NETC.state = await api("GET", `/nodes/${enc(id)}/netdiag`); }
  catch (e) { NETC.err = e.message; }
  if (drawerCtx && drawerCtx.kind === "net" && drawerCtx.id === id) repaintNet();
}
function netGuard() { return drawerCtx && drawerCtx.kind === "net" && drawerCtx.id === NETC.id; }
function repaintNet() { if (!netGuard()) return; setDrawerBody(netBody()); setDrawerFoot(netFoot()); }
function netBody() {
  const st = NETC.state || {}; const rs = st.results || [];
  let h = "";
  if (NETC.err) h += `<div class="banner err">${icon("alert", 15)}<span>${esc(NETC.err)}</span></div>`;
  h += `<div class="kv mb12">` +
    kvRow(appT("m292"), st.at ? fmtTime(st.at) + ` <span class="txt-faint">${ago(st.at)}</span>` : appH("m293")) +
    kvRow(appT("m294"), `<span class="mono">${esc(typeof (rs.length) === "number" ? appN(rs.length) : (rs.length))}</span>`) + `</div>`;
  if (NETC.busy) h += `<div class="waiting"><span class="w-pulse"></span><div><div class="w-txt">${appH("m295")}</div><div class="w-sub">${appH("m296")}</div></div><span class="spin">${icon("refresh", 16)}</span></div>`;
  h += `<div class="d-sec"><span class="lbl">${appH("m297")}</span>`;
  if (!rs.length) h += empty("wifi", appT("m298"), "", true);
  else {
    h += `<div class="tbl-wrap"><table class="tbl" style="min-width:0"><thead><tr><th>${appH("m294")}</th><th>${appH("m299")}</th><th>${appH("m300")}</th><th>${appH("m301")}</th></tr></thead><tbody>` +
      rs.map(r => `<tr><td>${esc(r.name || "—")}</td><td>${r.ok ? badge(appT("m302"), "ok") : badge(appT("m034"), "err")}</td><td class="cell-mono">${r.ms != null ? r.ms + " ms" : "—"}</td><td class="sub">${esc(r.detail || "—")}</td></tr>`).join("") +
      `</tbody></table></div>`;
  }
  h += `</div>`;
  return h;
}
function netFoot() {
  return `<div class="hint">${appH("m303")}</div><div class="spacer"></div>` +
    `<button class="btn cyan" data-act="net-run" ${NETC.busy ? "disabled" : ""}>${icon("activity", 15)} ${appH("m304")}</button>`;
}
async function runNetdiag() {
  if (NETC.busy) return;
  NETC.busy = true; NETC.err = ""; NETC.tries = 0; NETC.cmdId = "";
  repaintNet();
  try {
    const c = await api("POST", `/nodes/${enc(NETC.id)}/commands`, { type: "netdiag", params: {} });
    NETC.cmdId = (c && c.id) || "";
    poll("net:cmd", netPollCmd, 1500);
  } catch (e) { NETC.busy = false; NETC.err = e.message; repaintNet(); toast(e.message, "err"); }
}
async function netPollCmd() {
  if (!netGuard() || !NETC.busy) { stopPoll("net:cmd"); return; }
  NETC.tries++;
  try {
    const cs = await api("GET", `/nodes/${enc(NETC.id)}/commands`);
    const list = Array.isArray(cs) ? cs : [];
    const c = list.find(x => x.id === NETC.cmdId) ||
      list.filter(x => x.type === "netdiag").sort((a, b) => ts(b.created_at) - ts(a.created_at))[0];
    const st = cmdStatus(c);
    if (st === "ok" || st === "err") {
      stopPoll("net:cmd"); NETC.busy = false;
      try {
        NETC.state = await api("GET", `/nodes/${enc(NETC.id)}/netdiag`);
        NETC.err = st === "err" ? ((c && c.log) || appT("m305")) : "";
      } catch (e) { NETC.err = e.message; }
      repaintNet();
      toast(st === "ok" ? appT("m306") : appT("m307"), st === "ok" ? "ok" : "err");
    } else if (NETC.tries > 160) {
      stopPoll("net:cmd"); NETC.busy = false; NETC.err = appT("m308"); repaintNet();
    }
  } catch (e) { /* 瞬时错误，继续轮询 */ }
}

/* ================= CONSOLE ================= */
function openConsole(id) {
  const url = `${API_BASE}/nodes/${enc(id)}/console/`;
  openModal(appH("m309", { id: shortId(id) }),
    `<div class="banner info mb12">${icon("terminal", 15)}<span>${appH("m310")}</span></div>` +
    `<iframe class="console-frame" src="${url}" title="${appH("m311")}" sandbox="allow-scripts allow-same-origin allow-forms"></iframe>`,
    `<a class="btn ghost" href="${url}" target="_blank" rel="noopener">${icon("external", 15)} ${appH("m312")}</a><div class="spacer"></div>` +
    `<button class="btn primary" data-act="modal-close">${appH("m040")}</button>`);
}

/* ================= NODE CONFIG ================= */
const CFG = { id: "", node: null, boms: [], license: null, apps: [], nodes: [], offline: 45, busy: false };
async function openConfig(id) {
  Object.assign(CFG, { id, node: null, boms: [], license: null, apps: [], nodes: [], offline: 45, busy: false });
  openModal(`${icon("sliders", 18)} ${appH("m313", { id: shortId(id) })}`, empty("refresh", appT("m314"), "", true), "");
  let n;
  try { n = await api("GET", `/nodes/${enc(id)}`); } catch (e) { closeModal(); toast(e.message, "err"); return; }
  CFG.node = n; CFG.offline = clamp(Number(n.offline_after) || 45, 15, 86400);
  const [boms, lic, apps, nodes] = await Promise.all([
    api("GET", "/boms?status=published").catch(() => []),
    api("GET", "/license").catch(() => null),
    api("GET", `/nodes/${enc(id)}/apps`).catch(() => []),
    api("GET", "/nodes").catch(() => [])
  ]);
  CFG.boms = Array.isArray(boms) ? boms : [];
  CFG.license = lic;
  CFG.apps = Array.isArray(apps) ? apps : (apps && apps.apps) || [];
  CFG.nodes = Array.isArray(nodes) ? nodes : [];
  if (!$(".modal") || CFG.id !== id) return;
  setModalBody(cfgBody()); setModalFoot(cfgFoot());
  paintCfgDrift(); paintCfgDacHint(); paintCfgSeat();
}
function cfgBody() {
  const n = CFG.node;
  const pub = CFG.boms.filter(b => b.status === "published");
  const cur = n.bom_id ? `${n.bom_id}@${n.bom_version || 0}` : "";
  const bomOpts = [`<option value="" ${!cur ? "selected" : ""}>${appH("m315")}</option>`]
    .concat(pub.map(b => { const v = `${b.bom_id}@${b.version}`; return `<option value="${esc(v)}" ${v === cur ? "selected" : ""}>${esc(v)} · ${esc(b.role || appT("m316"))}</option>`; }));
  if (cur && !pub.some(b => `${b.bom_id}@${b.version}` === cur)) bomOpts.push(`<option value="${esc(cur)}" selected>${appH("m317", { value: cur })}</option>`);
  return `
  <div class="m-sec"><span class="lbl">${appH("m318")}</span>
    <div class="f-row">
      <div class="field"><label>${appH("m219")}</label><input id="cfg-alias" class="inp" maxlength="60" value="${esc(n.alias || "")}"></div>
      <div class="field"><label>${appH("m221")}</label><input id="cfg-group" class="inp" maxlength="40" value="${esc(n.group || "")}"></div>
    </div>
    <div class="field"><label>${appH("m226")}</label><input id="cfg-note" class="inp" maxlength="200" value="${esc(n.note || "")}"></div>
    <div class="field"><label>${appH("m319")}</label>
      <div class="stepper">
        <button class="step-btn" data-act="cfg-off" data-d="-15">−15</button>
        <span class="inp" id="cfg-off-val" style="display:inline-flex;align-items:center;justify-content:center">${esc(typeof (CFG.offline) === "number" ? appN(CFG.offline) : (CFG.offline))}s</span>
        <button class="step-btn" data-act="cfg-off" data-d="15">+15</button>
      </div>
      <div class="f-hint">${appH("m320")}</div>
    </div>
  </div>
  <div class="m-sec"><span class="lbl">${appH("m321")}</span>
    <div class="field"><select id="cfg-bom" class="inp" data-change="cfg-bom">${bomOpts.join("")}</select></div>
    <div id="cfg-drift"></div>
  </div>
  <div class="m-sec"><span class="lbl">${appH("m322")}</span>
    <div class="f-row">
      <div class="field"><label>${appH("m323")}</label>
        <select id="cfg-dac" class="inp" data-change="cfg-dac">
          <option value="auto" ${n.dac_arbitration !== "manual" ? "selected" : ""}>${appH("m324")}</option>
          <option value="manual" ${n.dac_arbitration === "manual" ? "selected" : ""}>${appH("m325")}</option>
        </select>
      </div>
      <div class="field"><label>${appH("m326")}</label><input id="cfg-dacdev" class="inp mono" placeholder="${appH("m327")}" value="${esc(n.dac_device || "")}"></div>
    </div>
    <div id="cfg-dac-hint" class="f-hint"></div>
  </div>
  <div class="m-sec"><span class="lbl">${appH("m328")}</span>
    <div class="f-row mb12">
      <label class="switch"><input type="checkbox" id="cfg-pp" ${n.play_protect ? "checked" : ""}><span class="track"></span><span class="sw-lbl">${appH("m329")}</span></label>
      <label class="switch"><input type="checkbox" id="cfg-aos" ${n.auto_os_update ? "checked" : ""}><span class="track"></span><span class="sw-lbl">${appH("m330")}</span></label>
    </div>
    <div class="field"><label>${appH("m331")}</label><input id="cfg-win" class="inp mono" placeholder="${appH("m332")}" value="${esc(n.os_window || "")}"><div class="f-hint">${appH("m333")}</div></div>
  </div>
  <div class="m-sec"><span class="lbl">${appH("m334")}</span><div id="cfg-seat"></div></div>
  <div class="m-sec"><span class="lbl" style="color:var(--red)">${appH("m335")}</span>
    <div class="banner err">${icon("alert", 15)}<span>${appH("m336")}</span></div>
    <button class="btn danger" data-act="cfg-delete" data-id="${esc(n.node_id)}">${icon("trash", 15)} ${appH("m337")}</button>
  </div>`;
}
function cfgFoot() {
  return `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div>` +
    `<button class="btn primary" data-act="cfg-save" ${CFG.busy ? "disabled" : ""}>${icon("check", 15)} ${appH("m338")}</button>`;
}
function paintCfgDrift() {
  const el = $("#cfg-drift"); if (!el) return;
  const sel = val("cfg-bom");
  if (!sel) { el.innerHTML = `<div class="f-hint">${appH("m339")}</div>`; return; }
  const at = sel.lastIndexOf("@");
  const bid = sel.slice(0, at), bver = Number(sel.slice(at + 1)) || 0;
  const bom = CFG.boms.find(b => b.bom_id === bid && b.version === bver);
  const n = CFG.node;
  let h = "";
  if (n.bom_id === bid && (n.bom_version || 0) === bver) h += `<div class="banner ok">${icon("check", 15)}<span>${appH("m340")}</span></div>`;
  else if (n.bom_id) h += `<div class="banner warn">${icon("alert", 15)}<span>${appH("m341")} <span class="mono">${esc(n.bom_id)}@${n.bom_version || 0}</span> ${appH("m342")} <span class="mono">${esc(sel)}</span>${appH("m343")}</span></div>`;
  else h += `<div class="banner info">${icon("info", 15)}<span>${appH("m344")} <span class="mono">${esc(sel)}</span>${appH("m345")}</span></div>`;
  if (bom) {
    const want = new Map((bom.apps || []).map(a => [a.app_id, a.version || ""]));
    const have = new Map(CFG.apps.map(a => [a.app_id || a.id, a.version || ""]));
    const missing = [], mismatch = [], extra = [];
    for (const [id, v] of want) {
      if (!have.has(id)) missing.push(id + (v ? "@" + v : ""));
      else if (v && have.get(id) !== v) mismatch.push(`${id} ${have.get(id)} → ${v}`);
    }
    for (const [id] of have) if (!want.has(id)) extra.push(id);
    if (!missing.length && !mismatch.length && !extra.length) h += `<div class="f-hint txt-ok">${appH("m346")}</div>`;
    else {
      h += `<div class="f-hint">${appH("m347")}</div><div class="kv mt8">`;
      if (missing.length) h += kvRow(appT("m348"), missing.map(x => `<span class="mono txt-warn">${esc(x)}</span>`).join("、"));
      if (mismatch.length) h += kvRow(appT("m349"), mismatch.map(x => `<span class="mono txt-warn">${esc(x)}</span>`).join("、"));
      if (extra.length) h += kvRow(appT("m350"), extra.map(x => `<span class="mono txt-info">${esc(x)}</span>`).join("、"));
      h += `</div>`;
    }
  }
  el.innerHTML = h;
}
function paintCfgDacHint() {
  const el = $("#cfg-dac-hint"); if (!el) return;
  const manual = val("cfg-dac") === "manual";
  const dev = $("#cfg-dacdev"); if (dev) dev.disabled = !manual;
  el.textContent = manual ? appT("m351") : appT("m352");
}
function paintCfgSeat() {
  const el = $("#cfg-seat"); if (!el) return;
  const n = CFG.node, lic = CFG.license;
  const used = CFG.nodes.filter(x => x.authorized).length;
  const builtin = Number(lic && lic.builtin_seats) || 3;
  const seats = Number(lic && lic.effective_seats) || builtin;
  const licensed = lic && lic.status === "activated";
  let h = "";
  if (!lic || lic.status === "none") h += `<div class="banner info">${icon("shield", 15)}<span>${appH("m353", { count: builtin })}</span><button class="btn violet xs" data-act="goto-keys">${appH("m354")}</button></div>`;
  else if (lic.status === "expired") h += `<div class="banner warn">${icon("alert", 15)}<span>${appH("m355", { count: builtin })}</span><button class="btn violet xs" data-act="goto-keys">${appH("m356")}</button></div>`;
  const pct = clamp(used / seats * 100, 0, 100);
  h += `<div class="seats"><div class="s-top"><span>${appH("m358")}${licensed ? "" : appH("m357")}</span><span class="mono">${esc(typeof (used) === "number" ? appN(used) : (used))} / ${esc(typeof (seats) === "number" ? appN(seats) : (seats))}</span></div><div class="seat-bar"><div class="seat-fill ${used > seats ? "over" : ""}" style="width:${pct}%"></div></div></div>`;
  if (used >= seats && !n.authorized) h += `<div class="f-hint txt-warn">${appH("m360")}${licensed ? "" : appH("m359")}。</div>`;
  h += `<div class="btn-row mt8">` +
    (n.authorized
      ? `${authBadge(n)}<button class="btn ghost sm" data-act="cfg-revoke" data-id="${esc(n.node_id)}">${icon("lock", 14)} ${appH("m361")}</button>`
      : `${authBadge(n)}<button class="btn primary sm" data-act="cfg-auth" data-id="${esc(n.node_id)}">${icon("unlock", 14)} ${appH("m362")}</button>`) +
    `</div>`;
  el.innerHTML = h;
}
function cfgOffStep(d) {
  CFG.offline = clamp(CFG.offline + d, 15, 86400);
  const el = $("#cfg-off-val"); if (el) el.textContent = appN(CFG.offline) + "s";
}
async function cfgAuth(revoke) {
  const id = CFG.id;
  try {
    await api("POST", `/nodes/${enc(id)}/${revoke ? "revoke" : "authorize"}`);
    CFG.node.authorized = !revoke;
    if (!revoke) CFG.node.authorized_at = Math.floor(Date.now() / 1000);
    paintCfgSeat(); toast(revoke ? appT("m363") : appT("m364"), "ok");
    refreshCurrent();
  } catch (e) { toast(e.message, "err"); }
}
async function cfgSave() {
  if (CFG.busy) return;
  const alias = val("cfg-alias").trim();
  if (!alias) { toast(appT("m365"), "warn"); const el = $("#cfg-alias"); if (el) el.focus(); return; }
  const sel = val("cfg-bom");
  let bom_id = "", bom_version = 0;
  if (sel) { const at = sel.lastIndexOf("@"); bom_id = sel.slice(0, at); bom_version = Number(sel.slice(at + 1)) || 0; }
  const body = {
    alias, group: val("cfg-group").trim(), note: val("cfg-note").trim(),
    offline_after: CFG.offline, bom_id, bom_version,
    dac_arbitration: val("cfg-dac") === "manual" ? "manual" : "auto",
    dac_device: val("cfg-dacdev").trim(),
    play_protect: checked("cfg-pp"), auto_os_update: checked("cfg-aos"), os_window: val("cfg-win").trim()
  };
  CFG.busy = true; setModalFoot(cfgFoot());
  try {
    await api("PATCH", `/nodes/${enc(CFG.id)}`, body);
    toast(appT("m366"), "ok");
    const id = CFG.id;
    closeModal();
    if (drawerCtx && drawerCtx.kind === "node" && drawerCtx.id === id) openNodeDrawer(id);
    await refreshCurrent();
  } catch (e) { toast(e.message, "err"); }
  finally { CFG.busy = false; if ($(".modal")) setModalFoot(cfgFoot()); }
}
async function cfgDelete() {
  const n = CFG.node; if (!n) return;
  const ok = await confirmDlg({ title: appT("m367"), message: appT("m368", { name: n.alias || n.node_id }), okText: appT("m205") });
  if (!ok) return;
  await deleteNodes([n.node_id]);
}

/* ================= CATALOG ================= */
function shellCatalog() {
  return `<section class="card stagger" style="--i:0">
      ${secHead(`${icon("box", 17)} ${appH("m369")}`, `<button class="btn primary sm" data-act="app-create">${icon("plus", 14)} ${appH("m370")}</button>`)}
      <div id="cat-apps"></div>
    </section>
    <div id="bom-report"></div>
    <section class="card mt16 stagger" style="--i:1">
      ${secHead(`${icon("layers", 17)} ${appH("m371")}`, `<button class="btn ghost sm" data-act="bom-import">${icon("upload", 14)} ${appH("m372")}</button><button class="btn primary sm" data-act="bom-new">${icon("plus", 14)} ${appH("m373")}</button>`)}
      <div id="cat-boms"></div>
    </section>`;
}
async function refreshCatalog() {
  const apps = await api("GET", "/apps");
  S.apps = Array.isArray(apps) ? apps : [];
  const boms = await api("GET", "/boms").catch(() => []);
  S.boms = Array.isArray(boms) ? boms : [];
  const pub = await api("GET", "/boms?status=published").catch(() => []);
  S.pubBoms = Array.isArray(pub) ? pub : [];
  paintApps(); paintBoms();
}
function paintApps() {
  const el = $("#cat-apps"); if (!el) return;
  const list = S.apps.filter(a => hit(a.app_id, a.name, a.category, a.upstream, a.license));
  if (!list.length) {
    el.innerHTML = empty("box", S.apps.length ? appT("m374") : appT("m375"),
      S.apps.length ? "" : `<button class="btn primary" data-act="app-create">${icon("plus", 15)} ${appH("m370")}</button>`);
    return;
  }
  el.innerHTML = `<div class="grid cols-3">` + list.map((a, i) => `
    <div class="card lift app-card stagger" style="--i:${i % 9}" data-act="app-open" data-id="${esc(a.app_id)}" role="button" tabindex="0">
      <div class="app-top"><span class="app-name">${esc(a.name || a.app_id)}</span>${catBadge(a.category)}</div>
      <div class="app-meta"><span class="tag-mono">${esc(a.app_id)}</span>${a.self_build_required ? badge(appT("m376"), "warn") : ""}${a.maintainer_state === "inactive" ? badge(appT("m377"), "err") : ""}</div>
      <div class="app-meta mt8">${icon("globe", 13)} ${esc(a.upstream || "—")} · ${esc(a.license || "—")}</div>
      <div class="app-meta mt8">${appH("m378")} <span class="mono">${esc(a.default_channel || "stable")}</span><div class="spacer"></div><span class="txt-faint">${appH("m379")} ${icon("chevR", 13)}</span></div>
    </div>`).join("") + `</div>`;
}
const APPV = { id: "", app: null, versions: [], busy: false };
async function openAppModal(id) {
  APPV.id = id; APPV.app = null; APPV.versions = []; APPV.busy = false;
  openModal(`${icon("box", 18)} ${esc(shortId(id))}`, empty("refresh", appT("m380"), "", true), "");
  try {
    const [app, vs] = await Promise.all([api("GET", `/apps/${enc(id)}`), api("GET", `/apps/${enc(id)}/versions`).catch(() => [])]);
    APPV.app = app; APPV.versions = Array.isArray(vs) ? vs : [];
  } catch (e) { closeModal(); toast(e.message, "err"); return; }
  if (!$(".modal") || APPV.id !== id) return;
  setModalBody(appModalBody());
  setModalFoot(`<button class="btn ghost" data-act="app-upload" data-id="${esc(id)}">${icon("upload", 15)} ${appH("m381")}</button><button class="btn ghost" data-act="app-import-local" data-id="${esc(id)}">${icon("download", 15)} ${appH("m382")}</button><div class="spacer"></div><button class="btn primary" data-act="modal-close">${appH("m040")}</button>`);
}
function appModalBody() {
  const a = APPV.app || {};
  let h = `<div class="kv mb12">` +
    kvRow(appT("m383"), `<span class="mono">${esc(a.app_id || APPV.id)}</span>`) +
    kvRow(appT("m384"), esc(a.name || "—")) +
    kvRow(appT("m385"), catBadge(a.category)) +
    kvRow(appT("m386"), esc(a.upstream || "—")) +
    kvRow(appT("m387"), esc(a.license || "—")) +
    kvRow(appT("m388"), `<span class="mono">${esc(a.default_channel || "stable")}</span>`) +
    kvRow(appT("m389"), a.maintainer_state === "inactive" ? badge(appT("m390"), "err") : badge(appT("m391"), "ok")) +
    `</div><div class="divider"></div><span class="lbl">${appH("m392", { count: appN(APPV.versions.length) })}</span>`;
  if (!APPV.versions.length) return h + empty("upload", appT("m393"), `<button class="btn primary sm" data-act="app-upload" data-id="${esc(APPV.id)}">${icon("upload", 14)} ${appH("m394")}</button>`, true);
  h += `<div class="tbl-wrap mt8"><table class="tbl"><thead><tr><th>${appH("m395")}</th><th>${appH("m223")}</th><th>${appH("m396")}</th><th>${appH("m397")}</th><th>SHA256</th><th>${appH("m398")}</th><th>${appH("m399")}</th></tr></thead><tbody>` +
    APPV.versions.map(v => `<tr>
      <td class="cell-mono">${esc(v.version)}<div class="sub">${esc(v.format || "")}</div></td>
      <td class="cell-mono">${esc(v.arch || "—")}</td>
      <td>${v.signed ? badge(appT("m400"), "ok") : badge(appT("m401"), "warn")}${v.banned ? " " + badge(appT("m402"), "err") : ""}</td>
      <td><select class="inp" style="width:104px;padding:5px 26px 5px 8px;font-size:12px" data-change="ver-lc" data-id="${esc(APPV.id)}" data-ver="${esc(v.version)}" data-arch="${esc(v.arch)}">
        ${["available", "deprecated", "revoked"].map(s => `<option value="${s}" ${(v.lifecycle || "available") === s ? "selected" : ""}>${({ available: appT("m049"), deprecated: appT("m050"), revoked: appT("m051") })[s]}</option>`).join("")}
      </select></td>
      <td class="cell-mono sub" title="${esc(v.sha256 || "")}">${esc((v.sha256 || "—").slice(0, 10))}</td>
      <td class="cell-mono sub">${fmtBytes(v.size)}</td>
      <td class="acts"><button class="btn ${v.banned ? "ghost" : "danger"} xs" data-act="ver-ban" data-id="${esc(APPV.id)}" data-ver="${esc(v.version)}" data-arch="${esc(v.arch)}" data-banned="${v.banned ? "0" : "1"}">${v.banned ? appH("m403") : appH("m404")}</button></td>
    </tr>`).join("") + `</tbody></table></div>`;
  return h;
}

/* ---- local vendor package import ---- */
function openImportLocal(id) {
  openModal(`${icon("download", 18)} ${appH("m405", { id: shortId(id) })}`, `
    <div id="imp-err"></div>
    <div class="field"><label>${appH("m406")}</label><input id="imp-path" class="inp mono" placeholder="${appH("m407")}"><div class="f-hint">${appH("m408")}</div></div>
    <div class="f-row">
      <div class="field"><label>${appH("m409")}</label><input id="imp-ver" class="inp mono" placeholder="${appH("m410")}"></div>
      <div class="field"><label>${appH("m411")}</label><select id="imp-arch" class="inp"><option value="arm64">arm64（Pi 4 / armv8）</option><option value="armhf">armhf（armv7）</option><option value="amd64">amd64（x64）</option></select></div>
    </div>
    <div class="f-row">
      <div class="field"><label>${appH("m412")}</label><select id="imp-fmt" class="inp"><option value="tar.bz2">tar.bz2（Roon Bridge）</option><option value="deb">${appH("m413")}</option></select></div>
      <div class="field"><label>${appH("m414")}</label><input id="imp-sig" class="inp mono" placeholder="${appH("m415")}"></div>
    </div>
    <div class="f-row">
      <div class="field"><label>systemd units</label><input id="imp-units" class="inp mono" placeholder="${appH("m416")}"></div>
      <div class="field"><label>${appH("m417")}</label><input id="imp-deps" class="inp mono" placeholder="${appH("m418")}"></div>
    </div>
    <div class="banner info">${icon("info", 15)}<span>${appH("m419")}</span></div>`,
    `<button class="btn ghost" data-act="app-open" data-id="${esc(id)}">${appH("m064")}</button><div class="spacer"></div><button class="btn primary" data-act="app-import-local-submit" data-id="${esc(id)}">${icon("check", 15)} ${appH("m420")}</button>`, { narrow: true });
}
async function submitImportLocal(id) {
  const err = $("#imp-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const path = val("imp-path").trim(), ver = val("imp-ver").trim();
  if (!path) return fail(appT("m421"));
  if (!ver) return fail(appT("m422"));
  try {
    const v = await api("POST", `/apps/${enc(id)}/versions/import-local`, {
      version: ver, arch: val("imp-arch"), format: val("imp-fmt"),
      local_path: path, signature_path: val("imp-sig").trim(),
      units: splitList(val("imp-units")), deps_pkgs: splitList(val("imp-deps"))
    });
    toast(appT("m423", { version: v.version, value: v.arch }), "ok");
    openAppModal(id);
  } catch (e) { fail(e.message); }
}

/* ---- app create / upload ---- */
function openCreateApp() {
  openModal(`${icon("plus", 18)} ${appH("m370")}`, `
    <div id="ap-err"></div>
    <div class="f-row">
      <div class="field"><label>${appH("m424")}</label><input id="ap-id" class="inp mono" placeholder="${appH("m425")}" maxlength="64"><div class="f-hint">${appH("m426")}</div></div>
      <div class="field"><label>${appH("m427")}</label><input id="ap-name" class="inp" placeholder="${appH("m428")}" maxlength="80"></div>
    </div>
    <div class="f-row">
      <div class="field"><label>${appH("m385")}</label><select id="ap-cat" class="inp"><option value="renderer">${appH("m041")}</option><option value="dependency">${appH("m042")}</option><option value="tool">${appH("m043")}</option></select></div>
      <div class="field"><label>${appH("m388")}</label><select id="ap-channel" class="inp"><option value="stable">stable</option><option value="beta">beta</option><option value="nightly">nightly</option></select></div>
    </div>
    <div class="field"><label>${appH("m429")}</label><input id="ap-up" class="inp mono" placeholder="https://…"></div>
    <div class="field"><label>${appH("m430")}</label><input id="ap-lic" class="inp" placeholder="${appH("m431")}"></div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn primary" data-act="app-create-submit">${icon("check", 15)} ${appH("m432")}</button>`, { narrow: true });
}
async function submitCreateApp() {
  const err = $("#ap-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const id = val("ap-id").trim(), name = val("ap-name").trim();
  if (!/^[a-z0-9][a-z0-9._-]{0,63}$/.test(id)) return fail(appT("m433"));
  if (!name) return fail(appT("m434"));
  try {
    await api("POST", "/apps", { app_id: id, name, category: val("ap-cat"), upstream: val("ap-up").trim(), license: val("ap-lic").trim(), default_channel: val("ap-channel"), maintainer_state: "active" });
    toast(appT("m435", { id: id }), "ok");
    await refreshCatalog(); openAppModal(id);
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
const UP = { id: "", busy: false };
function openUpload(id) {
  UP.id = id; UP.busy = false;
  openModal(`${icon("upload", 18)} ${appH("m436", { id: shortId(id) })}`, `
    <div id="up-err"></div>
    <div class="f-row">
      <div class="field"><label>${appH("m437")}</label><input id="up-ver" class="inp mono" placeholder="${appH("m438")}" maxlength="40"></div>
      <div class="field"><label>${appH("m411")}</label><select id="up-arch" class="inp"><option>amd64</option><option>arm64</option><option>armhf</option><option>armv7hf</option></select></div>
    </div>
    <div class="f-row">
      <div class="field"><label>${appH("m439")}</label><select id="up-format" class="inp"><option value="deb">deb</option><option value="rpm">rpm</option><option value="tar">${appH("m440")}</option><option value="binary">${appH("m441")}</option></select></div>
      <div class="field"><label>${appH("m397")}</label><select id="up-lc" class="inp"><option value="available">${appH("m049")}</option><option value="deprecated">${appH("m050")}</option></select></div>
    </div>
    <div class="field"><label>${appH("m442")}</label><input id="up-file" type="file" class="inp"></div>
    <div class="field"><label>${appH("m443")}</label><input id="up-sig" type="file" class="inp" accept=".asc,.sig,.gpg"></div>
    <div class="field"><label>${appH("m444")}</label><input id="up-caps" class="inp mono" placeholder="airplay, raop"></div>
    <div class="f-row">
      <div class="field"><label>${appH("m445")}</label><input id="up-units" class="inp mono" placeholder="shairport-sync.service"></div>
      <div class="field"><label>${appH("m446")}</label><input id="up-deps" class="inp mono" placeholder="libasound2, avahi-daemon"></div>
    </div>
    <div class="f-row mb12">
      <label class="switch"><input type="checkbox" id="up-dac"><span class="track"></span><span class="sw-lbl">${appH("m447")}</span></label>
      <label class="switch"><input type="checkbox" id="up-ap2"><span class="track"></span><span class="sw-lbl">${appH("m448")}</span></label>
    </div>
    <div class="banner info">${icon("shield", 15)}<span>${appH("m449")}</span></div>`,
    `<button class="btn ghost" data-act="app-open" data-id="${esc(id)}">${icon("chevL", 14)} ${appH("m064")}</button><div class="spacer"></div><button class="btn primary" data-act="up-submit" id="up-btn">${icon("upload", 15)} ${appH("m450")}</button>`, { narrow: true });
}
async function submitVersion() {
  if (UP.busy) return;
  const err = $("#up-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const version = val("up-ver").trim(), arch = val("up-arch");
  const f = $("#up-file"), file = f && f.files && f.files[0];
  if (!version) return fail(appT("m451"));
  if (!file) return fail(appT("m452"));
  const meta = {
    version, arch, format: val("up-format"), lifecycle: val("up-lc") || "available",
    capabilities: splitList(val("up-caps")), units: splitList(val("up-units")), deps_pkgs: splitList(val("up-deps")),
    occupies_dac: checked("up-dac"), airplay2_enabled: checked("up-ap2")
  };
  const fd = new FormData();
  fd.append("meta", JSON.stringify(meta));
  fd.append("artifact", file, file.name);
  const sf = $("#up-sig"), sig = sf && sf.files && sf.files[0];
  if (sig) fd.append("signature", sig, sig.name);
  UP.busy = true;
  const btn = $("#up-btn"); if (btn) { btn.disabled = true; btn.innerHTML = `<span class="spin">${icon("refresh", 15)}</span> ${appH("m453")}`; }
  try {
    await apiForm(`/apps/${enc(UP.id)}/versions`, fd);
    toast(appT("m454", { version: version, value: arch }), "ok");
    const id = UP.id; UP.busy = false;
    await refreshCatalog(); openAppModal(id);
  } catch (e) {
    UP.busy = false; fail(e.message); toast(e.message, "err");
    if (btn) { btn.disabled = false; btn.innerHTML = `${icon("upload", 15)} ${appH("m450")}`; }
  }
}
async function verBan(ds) {
  const id = ds.id, ver = ds.ver, arch = ds.arch, banned = ds.banned === "1";
  if (banned) {
    const ok = await confirmDlg({ title: appT("m455"), message: appT("m456", { id: id, version: ver, value: arch }), okText: appT("m404") });
    if (!ok) return;
  }
  try {
    await api("POST", `/apps/${enc(id)}/versions/${enc(ver)}/${enc(arch)}/ban`, { banned });
    const v = APPV.versions.find(x => x.version === ver && x.arch === arch); if (v) v.banned = banned;
    if ($(".modal") && APPV.id === id) setModalBody(appModalBody());
    toast(banned ? appT("m457") : appT("m458"), "ok");
  } catch (e) { toast(e.message, "err"); }
}
async function verLifecycle(ds, lc) {
  const id = ds.id, ver = ds.ver, arch = ds.arch;
  try {
    await api("POST", `/apps/${enc(id)}/versions/${enc(ver)}/${enc(arch)}/lifecycle`, { lifecycle: lc });
    const v = APPV.versions.find(x => x.version === ver && x.arch === arch); if (v) v.lifecycle = lc;
    toast(appT("m459", { value: ({ available: appT("m049"), deprecated: appT("m050"), revoked: appT("m051") })[lc] || lc }), "ok");
  } catch (e) { toast(e.message, "err"); if ($(".modal") && APPV.id === id) setModalBody(appModalBody()); }
}

/* ================= BOM ================= */
function paintBoms() {
  const el = $("#cat-boms"); if (!el) return;
  const list = S.boms.filter(b => hit(b.bom_id, b.role, b.status)).sort((a, b) => a.bom_id === b.bom_id ? b.version - a.version : String(a.bom_id).localeCompare(String(b.bom_id)));
  if (!list.length) {
    el.innerHTML = empty("layers", S.boms.length ? appT("m460") : appT("m461"),
      S.boms.length ? "" : `<button class="btn primary" data-act="bom-new">${icon("plus", 15)} ${appH("m373")}</button>`);
    return;
  }
  el.innerHTML = `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>BOM</th><th>${appH("m462")}</th><th>${appH("m048")}</th><th>${appH("m463")}</th><th>${appH("m464")}</th><th>${appH("m399")}</th></tr></thead><tbody>` +
    list.map(b => `<tr>
      <td class="cell-mono">${esc(b.bom_id)}<span class="txt-faint">@</span><span class="txt-info">${b.version}</span></td>
      <td>${esc(b.role || "—")}</td>
      <td class="cell-mono sub">${appH("m468", { count: appN((b.apps || []).length) })}</td>
      <td>${b.status === "published" ? badge(appT("m465"), "ok") : badge(appT("m466"), "warn")}</td>
      <td class="sub nowrap">${ago(b.updated_at || b.created_at)}</td>
      <td class="acts">
        <button class="btn ghost xs" data-act="bom-edit" data-id="${esc(b.bom_id)}" data-ver="${b.version}">${icon("edit", 12)} ${appH("m469")}</button>
        <button class="btn ghost xs" data-act="bom-validate" data-id="${esc(b.bom_id)}" data-ver="${b.version}">${appH("m470")}</button>
        ${b.status !== "published" ? `<button class="btn primary xs" data-act="bom-publish" data-id="${esc(b.bom_id)}" data-ver="${b.version}">${appH("m467")}</button>` : ""}
        <button class="btn ghost xs" data-act="bom-history" data-id="${esc(b.bom_id)}" title="${appH("m471")}">${icon("audit", 12)} ${appH("m472")}</button>
        <button class="btn ghost xs" data-act="bom-export" data-id="${esc(b.bom_id)}" data-ver="${b.version}" title="${appH("m473")}">${icon("download", 12)}</button>
        <button class="btn ghost xs" data-act="bom-rollback" data-id="${esc(b.bom_id)}" data-ver="${b.version}" title="${appH("m474")}">${icon("undo", 12)}</button>
      </td></tr>`).join("") + `</tbody></table></div>`;
}
const BOMH = { id: "", versions: [] };
async function openBomHistory(id) {
  BOMH.id = id; BOMH.versions = [];
  openModal(`${icon("audit", 18)} ${appH("m475", { id: id })}`, empty("refresh", appT("m476"), "", true),
    `<div class="spacer"></div><button class="btn primary" data-act="modal-close">${appH("m040")}</button>`);
  try {
    const vs = await api("GET", `/boms/${enc(id)}/versions`);
    BOMH.versions = Array.isArray(vs) ? vs : [];
  } catch (e) { closeModal(); toast(e.message, "err"); return; }
  if (!$(".modal") || BOMH.id !== id) return;
  setModalBody(bomHistoryHtml());
}
function bomHistoryHtml() {
  const vs = BOMH.versions;
  if (!vs.length) return empty("layers", appT("m477"));
  return `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>${appH("m395")}</th><th>${appH("m463")}</th><th>${appH("m048")}</th><th>${appH("m464")}</th><th style="text-align:right">${appH("m399")}</th></tr></thead><tbody>` +
    vs.map(b => `<tr>
      <td class="cell-mono">v<span class="txt-info">${b.version}</span></td>
      <td>${b.status === "published" ? badge(appT("m465"), "ok") : badge(appT("m466"), "warn")}</td>
      <td class="cell-mono sub">${appH("m468", { count: appN((b.apps || []).length) })}</td>
      <td class="sub nowrap">${ago(b.updated_at || b.created_at)}</td>
      <td class="acts">
        <button class="btn ghost xs" data-act="bom-hist" data-do="edit" data-id="${esc(b.bom_id)}" data-ver="${b.version}">${icon("edit", 12)} ${appH("m469")}</button>
        <button class="btn ghost xs" data-act="bom-hist" data-do="validate" data-id="${esc(b.bom_id)}" data-ver="${b.version}">${appH("m470")}</button>
        ${b.status !== "published" ? `<button class="btn primary xs" data-act="bom-hist" data-do="publish" data-id="${esc(b.bom_id)}" data-ver="${b.version}">${appH("m467")}</button>` : ""}
        <button class="btn ghost xs" data-act="bom-hist" data-do="export" data-id="${esc(b.bom_id)}" data-ver="${b.version}" title="${appH("m473")}">${icon("download", 12)}</button>
        <button class="btn ghost xs" data-act="bom-hist" data-do="rollback" data-id="${esc(b.bom_id)}" data-ver="${b.version}" title="${appH("m474")}">${icon("undo", 12)} ${appH("m478")}</button>
      </td></tr>`).join("") + `</tbody></table></div>`;
}
function bomHistAct(ds) {
  closeModal();
  const d = { id: ds.id, ver: ds.ver };
  if (ds.do === "edit") return openBomEditor(ds.id, ds.ver);
  if (ds.do === "validate") return bomValidate(d);
  if (ds.do === "publish") return bomPublish(d);
  if (ds.do === "export") return bomExport(d);
  if (ds.do === "rollback") return bomRollback(d);
}
function reportHtml(rep) {
  const errs = (rep && rep.errors) || [], warns = (rep && rep.warnings) || [];
  let h = `<div class="report">`;
  if (!errs.length && !warns.length) h += `<div class="line ok">${icon("check", 14)}<span>${appH("m479")}</span></div>`;
  errs.forEach(x => h += `<div class="line err">${icon("alert", 14)}<span>${esc(x)}</span></div>`);
  warns.forEach(x => h += `<div class="line warn">${icon("info", 14)}<span>${esc(x)}</span></div>`);
  return h + `</div>`;
}
function showBomReport(title, rep) {
  const el = $("#bom-report");
  if (el) el.innerHTML = `<section class="card mt16"><div class="sec-head"><div class="sec-title">${icon("shield", 16)} ${esc(title)}</div><div class="spacer"></div><button class="icon-btn" data-act="bom-report-close" aria-label="${appH("m040")}">${icon("x", 16)}</button></div>${reportHtml(rep)}</section>`;
  else openModal(`${icon("shield", 18)} ${esc(title)}`, reportHtml(rep), `<div class="spacer"></div><button class="btn primary" data-act="modal-close">${appH("m040")}</button>`, { narrow: true });
}
async function bomValidate(ds) {
  try {
    const rep = await api("POST", `/boms/${enc(ds.id)}/versions/${enc(ds.ver)}/validate?arch=amd64`);
    showBomReport(appT("m480", { id: ds.id, version: ds.ver }), rep);
    toast((rep && rep.errors && rep.errors.length) ? appT("m481") : appT("m482"), (rep && rep.errors && rep.errors.length) ? "warn" : "ok");
  } catch (e) { toast(e.message, "err"); }
}
async function bomPublish(ds) {
  const ok = await confirmDlg({ title: appT("m483"), message: appT("m484", { id: ds.id, version: ds.ver }), okText: appT("m467"), danger: false });
  if (!ok) return;
  try {
    const r = await api("POST", `/boms/${enc(ds.id)}/versions/${enc(ds.ver)}/publish?arch=amd64`);
    toast(appT("m485", { id: ds.id, version: ds.ver }), "ok");
    showBomReport(appT("m486", { id: ds.id, version: ds.ver }), r && r.report);
    await refreshCatalog();
  } catch (e) {
    toast(e.message, "err");
    if (e.data && e.data.report) showBomReport(appT("m487", { id: ds.id, version: ds.ver }), e.data.report);
  }
}
function openBomImport() {
  openModal(`${icon("upload", 18)} ${appH("m488")}`, `
    <div id="bi-err"></div>
    <div class="field"><label>${appH("m489")}</label><input id="bi-file" type="file" class="inp" data-change="bi-file" accept=".json,application/json"></div>
    <div class="field"><label>BOM JSON *</label><textarea id="bi-json" class="inp" rows="12" placeholder='{"bom_id":"hifi-default","role":"renderer","apps":[{"app_id":"shairport-sync","version":"4.3.2"}]}'></textarea><div class="f-hint">${appH("m490")}</div></div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn primary" data-act="bom-import-submit">${icon("upload", 15)} ${appH("m372")}</button>`, { narrow: true });
}
async function submitBomImport() {
  const err = $("#bi-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  let obj;
  try { obj = JSON.parse(val("bi-json")); } catch (e) { return fail(appT("m491") + e.message); }
  const bom = obj && obj.bom && typeof obj.bom === "object" ? obj.bom : obj;
  if (!bom || !bom.bom_id) return fail(appT("m492"));
  try {
    const b = await api("POST", "/boms/import", { bom });
    toast(appT("m493", { id: b.bom_id, version: b.version }), "ok");
    closeModal(); await refreshCatalog();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
function bomExport(ds) { download(`/boms/${enc(ds.id)}/versions/${enc(ds.ver)}/export`); toast(appT("m494"), "info"); }
async function bomRollback(ds) {
  const ok = await confirmDlg({ title: appT("m495"), message: appT("m496", { id: ds.id, version: ds.ver }), okText: appT("m497"), danger: false });
  if (!ok) return;
  try {
    const b = await api("POST", `/boms/${enc(ds.id)}/rollback`, { version: Number(ds.ver) });
    toast(appT("m498", { id: ds.id, version: ds.ver, version2: b && b.version }), "ok");
    await refreshCatalog();
  } catch (e) { toast(e.message, "err"); }
}

/* ---- BOM editor ---- */
const BOMD = { id: "", isNew: true, version: 0, busy: false };
async function openBomEditor(id, ver) {
  Object.assign(BOMD, { id: id || "", isNew: !id, version: 0, busy: false });
  let b = null;
  if (id) {
    openModal(`${icon("layers", 18)} ${appH("m499", { id: shortId(id) })}`, empty("refresh", appT("m500"), "", true), "");
    try { b = await api("GET", `/boms/${enc(id)}/versions/${enc(ver)}`); } catch (e) { closeModal(); toast(e.message, "err"); return; }
    if (!$(".modal")) return;
    BOMD.version = b.version;
  }
  const rows = b && (b.apps || []).length ? b.apps.map(a => ({ app_id: a.app_id || "", version: a.version || "", channel: a.channel || "" })) : [{ app_id: "", version: "", channel: "" }];
  const vars = b && b.variables ? JSON.stringify(b.variables, null, 2) : "{}";
  const units = b ? (b.units_enabled || []).join(", ") : "";
  const body = `
    <div id="bom-ed-err"></div>
    <div class="f-row">
      <div class="field"><label>BOM ID *</label><input id="b-id" class="inp mono" maxlength="64" placeholder="${appH("m503")}" value="${esc(b ? b.bom_id : "")}" ${b ? "disabled" : ""}></div>
      <div class="field"><label>${appH("m462")}</label><input id="b-role" class="inp" maxlength="40" placeholder="${appH("m504")}" value="${esc(b ? b.role || "" : "")}"></div>
    </div>
    <div class="banner violet">${icon("info", 15)}<span>${appH("m505")}${b ? appH("m501", { version: b.version, version2: b.version + 1 }) : appH("m502")}${appH("m506")}</span></div>
    <div class="m-sec"><span class="lbl">${appH("m507")}</span>
      <datalist id="app-ids">${S.apps.map(a => `<option value="${esc(a.app_id)}">${esc(a.name || "")}</option>`).join("")}</datalist>
      <div id="bom-rows">${bomRowsHtml(rows)}</div>
      <button class="btn ghost sm" data-act="bom-row-add">${icon("plus", 13)} ${appH("m508")}</button>
    </div>
    <div class="m-sec"><span class="lbl">${appH("m509")}</span>
      <div class="field"><textarea id="b-vars" class="inp" rows="5">${esc(vars)}</textarea><div class="f-hint">${appH("m510")}</div></div>
    </div>
    <div class="m-sec"><span class="lbl">${appH("m511")}</span>
      <div class="field"><input id="b-units" class="inp mono" placeholder="shairport-sync.service, upmpdcli.service" value="${esc(units)}"></div>
    </div>
    <div id="bom-ed-report"></div>`;
  const foot = `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div>` +
    `<button class="btn ghost" data-act="bom-validate-ed">${icon("shield", 14)} ${appH("m512")}</button>` +
    `<button class="btn ghost" data-act="bom-save-ed">${icon("check", 14)} ${appH("m513")}</button>` +
    `<button class="btn primary" data-act="bom-publish-ed">${icon("send", 14)} ${appH("m514")}</button>`;
  if (b) { setModalBody(body); setModalFoot(foot); }
  else openModal(`${icon("plus", 18)} ${appH("m373")}`, body, foot);
}
function bomRowsHtml(rows) {
  return rows.map((r, i) => `<div class="bom-row" data-i="${i}">
    <div style="display:flex;gap:8px;min-width:0">
      <input class="inp mono b-app" list="app-ids" placeholder="${appH("m383")}" value="${esc(r.app_id)}" style="flex:1;min-width:0">
      <select class="inp b-ch" style="width:96px;flex:none">
        ${["", "stable", "beta", "nightly"].map(c => `<option value="${c}" ${r.channel === c ? "selected" : ""}>${c || appH("m378")}</option>`).join("")}
      </select>
    </div>
    <input class="inp mono b-ver" placeholder="${appH("m515")}" value="${esc(r.version)}">
    <button class="row-del" data-act="bom-row-del" data-i="${i}" title="${appH("m516")}" aria-label="${appH("m516")}">${icon("x", 14)}</button>
  </div>`).join("");
}
function readBomRows() {
  return $$("#bom-rows .bom-row").map(row => ({
    app_id: ($(".b-app", row) || {}).value || "",
    version: ($(".b-ver", row) || {}).value || "",
    channel: ($(".b-ch", row) || {}).value || ""
  }));
}
function collectBomRows() {
  return readBomRows().filter(r => r.app_id.trim()).map(r => {
    const o = { app_id: r.app_id.trim() };
    if (r.version.trim()) o.version = r.version.trim();
    if (r.channel) o.channel = r.channel;
    return o;
  });
}
function bomRowAdd() { const rows = readBomRows(); rows.push({ app_id: "", version: "", channel: "" }); const el = $("#bom-rows"); if (el) el.innerHTML = bomRowsHtml(rows); }
function bomRowDel(i) { const rows = readBomRows(); rows.splice(i, 1); if (!rows.length) rows.push({ app_id: "", version: "", channel: "" }); const el = $("#bom-rows"); if (el) el.innerHTML = bomRowsHtml(rows); }
async function bomSave(silent) {
  const err = $("#bom-ed-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; throw new Error(m); };
  const id = BOMD.isNew ? val("b-id").trim() : BOMD.id;
  if (!/^[a-z0-9][a-z0-9._-]{0,63}$/.test(id)) fail(appT("m517"));
  const apps = collectBomRows();
  if (!apps.length) fail(appT("m518"));
  let variables = {};
  const rawVars = val("b-vars").trim();
  if (rawVars) {
    let obj; try { obj = JSON.parse(rawVars); } catch (e) { fail(appT("m519") + e.message); }
    if (!obj || typeof obj !== "object" || Array.isArray(obj)) fail(appT("m520"));
    for (const k of Object.keys(obj)) variables[k] = String(obj[k] ?? "");
  }
  const payload = { bom_id: id, role: val("b-role").trim(), apps, variables, units_enabled: splitList(val("b-units")) };
  const b = await api("POST", "/boms", payload);
  BOMD.id = b.bom_id; BOMD.version = b.version; BOMD.isNew = false;
  const idEl = $("#b-id"); if (idEl) { idEl.value = b.bom_id; idEl.disabled = true; }
  if (!silent) { toast(appT("m521", { id: b.bom_id, version: b.version }), "ok"); closeModal(); await refreshCatalog(); }
  return b;
}
async function bomValidateEd() {
  if (BOMD.busy) return; BOMD.busy = true;
  try {
    const b = await bomSave(true);
    const rep = await api("POST", `/boms/${enc(b.bom_id)}/versions/${b.version}/validate?arch=amd64`);
    const el = $("#bom-ed-report"); if (el) el.innerHTML = `<span class="lbl">${appH("m522", { version: b.version })}</span>` + reportHtml(rep);
    toast((rep && rep.errors && rep.errors.length) ? appT("m523", { version: b.version }) : appT("m524", { version: b.version }), (rep && rep.errors && rep.errors.length) ? "warn" : "ok");
    await refreshCatalog();
  } catch (e) { toast(e.message, "err"); }
  finally { BOMD.busy = false; }
}
async function bomPublishEd() {
  if (BOMD.busy) return; BOMD.busy = true;
  try {
    const b = await bomSave(true);
    try {
      const r = await api("POST", `/boms/${enc(b.bom_id)}/versions/${b.version}/publish?arch=amd64`);
      toast(appT("m525", { id: b.bom_id, version: b.version }), "ok");
      closeModal(); await refreshCatalog();
      showBomReport(appT("m486", { id: b.bom_id, version: b.version }), r && r.report);
    } catch (e) {
      toast(e.message, "err");
      const el = $("#bom-ed-report");
      if (el) el.innerHTML = `<span class="lbl">${appH("m526", { version: b.version })}</span>` + reportHtml(e.data && e.data.report);
      await refreshCatalog();
    }
  } catch (e) { toast(e.message, "err"); }
  finally { BOMD.busy = false; }
}

/* ================= APT SOURCE MANAGEMENT ================= */
function aptSourceRows() {
  const list = S.aptSources || [];
  if (!list.length) return empty("globe", appT("m527"), "", true);
  return list.map(s => `<div class="key-row">
    <div class="key-ic">${icon("globe", 16)}</div>
    <div class="key-info">
      <div class="key-name">${esc(s.name)} ${(s.distros || []).length ? s.distros.map(d => badge(d, "info")).join(" ") : badge(appT("m528"), "info")}</div>
      <div class="key-fp">${esc(s.url)} · ${esc(s.components || "main")}</div>
      <div class="key-meta">${s.key_url ? appH("m529") + esc(s.key_url) : appH("m530")}</div>
    </div>
    <button class="row-del" data-act="apt-src-del" data-id="${esc(s.id)}" title="${appH("m531")}">${icon("trash", 15)}</button>
  </div>`).join("");
}
async function openAptSources() {
  openModal(`${icon("globe", 18)} ${appH("m532")}`, empty("refresh", appT("m533"), "", true),
    `<button class="btn ghost" data-act="modal-close">${appH("m040")}</button>`);
  const root = $("#modal-root .modal");
  try {
    const sources = await api("GET", "/apt-sources");
    if ($("#modal-root .modal") !== root) return;
    if (!Array.isArray(sources)) throw new Error(appT("m534"));
    S.aptSources = sources;
    setModalBody(`
    <div id="apt-src-err"></div>
    <div id="apt-src-list">${aptSourceRows()}</div>
    <div class="f-row mt12">
      <div class="field"><label>${appH("m427")}</label><input id="apt-src-name" class="inp" maxlength="80" placeholder="${appH("m535")}"></div>
      <div class="field"><label>${appH("m536")}</label><input id="apt-src-distros" class="inp mono" placeholder="${appH("m537")}"></div>
    </div>
    <div class="field"><label>${appH("m538")}</label><input id="apt-src-url" class="inp mono" placeholder="https://…/debian/"></div>
    <div class="f-row">
      <div class="field"><label>${appH("m539")}</label><input id="apt-src-comp" class="inp mono" placeholder="${appH("m540")}"></div>
      <div class="field"><label>${appH("m541")}</label><input id="apt-src-key" class="inp mono" placeholder="${appH("m542")}"></div>
    </div>
    <div class="banner info">${icon("info", 15)}<span>${appH("m543")}</span></div>`);
    setModalFoot(`<button class="btn ghost" data-act="modal-close">${appH("m040")}</button><div class="spacer"></div><button class="btn primary" data-act="apt-src-add">${icon("plus", 15)} ${appH("m544")}</button>`);
    settingsApplyAccess();
  } catch (e) {
    if ($("#modal-root .modal") !== root) return;
    setModalBody(empty("alert", appT("m545") + e.message, `<button class="btn" data-act="apt-sources">${appH("m546")}</button>`, true));
  }
}
async function submitAptSource() {
  const root = $("#modal-root .modal");
  const err = $("#apt-src-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const name = val("apt-src-name").trim(), url = val("apt-src-url").trim();
  if (!name) return fail(appT("m547"));
  if (!/^https?:\/\//.test(url)) return fail(appT("m548"));
  try {
    await api("POST", "/apt-sources", {
      name, url, components: val("apt-src-comp").trim(), key_url: val("apt-src-key").trim(),
      distros: splitList(val("apt-src-distros")).map(d => d.toLowerCase())
    });
    toast(appT("m549", { name: name }), "ok");
    if ($("#modal-root .modal") === root) await openAptSources();
  } catch (e) { fail(e.message); }
}
async function aptSrcDel(id) {
  const root = $("#modal-root .modal"), list = $("#apt-src-list");
  const s = (S.aptSources || []).find(x => x.id === id);
  const ok = await confirmDlg({ title: appT("m550"), message: appT("m551", { name: s ? s.name : id }), okText: appT("m531") });
  if (!ok) return;
  try {
    await api("DELETE", `/apt-sources/${enc(id)}`);
    toast(appT("m552"), "ok");
    const sources = await api("GET", "/apt-sources");
    if ($("#modal-root .modal") !== root || $("#apt-src-list") !== list) return;
    if (!Array.isArray(sources)) throw new Error(appT("m534"));
    S.aptSources = sources;
    if (list) list.innerHTML = aptSourceRows();
    settingsApplyAccess();
  } catch (e) { toast(e.message, "err"); }
}
const jobNodes = j => (Array.isArray(j.node_ids) && j.node_ids.length) ? j.node_ids : (j.node_id ? [j.node_id] : []);
function jobProgress(j) {
  if (jobState(j) === "done") return 100;
  const ph = j.phases || [];
  if (!ph.length) return 0;
  const done = ph.filter(p => p.state === "ok" || p.state === "skipped").length;
  return clamp(Math.round(done / ph.length * 100), 0, 100);
}
function currentPhase(j) {
  const ph = j.phases || [];
  return ph.find(p => p.state === "running") || ph.slice().reverse().find(p => p.state !== "pending") || ph[0] || { name: jobState(j), state: "" };
}
async function openJobDrawer(id) {
  openDrawer(appH("m553", { id: shortId(id) }), esc(id), empty("refresh", appT("m554"), "", true), "", {
    ctx: { kind: "job", id, loading: false }, onClose: () => stopPoll("job:poll")
  });
  poll("job:poll", () => repaintJob(id), 2000);
  await repaintJob(id);
}
async function repaintJob(id) {
  const root = $("#drawer-root .drawer"), ctx = drawerCtx;
  if (!root || !ctx || ctx.kind !== "job" || ctx.id !== id || ctx.loading) return;
  const current = () => $("#drawer-root .drawer") === root && drawerCtx === ctx;
  ctx.loading = true;
  try {
    const j = await api("GET", `/jobs/${enc(id)}`);
    if (!current()) return;
    if (!j || j.id !== id) throw new Error(appT("m555"));
    setDrawerTitle(appH("m553", { id: shortId(id) }), esc(j.bom_id || id));
    setDrawerBody(jobDrawerBody(j)); setDrawerFoot(jobDrawerFoot(j));
  } catch (e) {
    if (!current()) return;
    setDrawerBody(empty("alert", appT("m556") + e.message, "", true));
    setDrawerFoot(`<button class="btn ghost" data-act="drawer-close">${appH("m040")}</button>`);
  } finally { ctx.loading = false; }
}
function jobDrawerBody(j) {
  const st = jobState(j);
  let h = "";
  if (j.reason) h += `<div class="banner ${st.startsWith("failed") ? "err" : "warn"}">${icon("info", 15)}<span>${esc(I18n.detail(j.reason, "app.operationDetails"))}</span></div>`;
  if (st === "waiting_idle") h += `<div class="banner info">${icon("clock", 15)}<span>${appH("m557")}</span></div>`;
  h += `<div class="kv mb12">` +
    kvRow(appT("m558"), `<span class="mono">${esc(j.id)}</span>`) +
    kvRow(appT("m559"), jobNodes(j).map(id => { const n = S.nodes.find(x => x.node_id === id); return `<button class="btn ghost xs" data-act="node-open" data-id="${esc(id)}">${esc(n ? (n.alias || shortId(id)) : shortId(id))}</button>`; }).join(" ") || "—") +
    kvRow(appT("m560"), `<span class="mono">${esc(j.bom_id || "—")}${j.bom_version ? "@" + j.bom_version : ""}</span>`) +
    kvRow(appT("m561"), badge(JOB_ZH[j.kind] || j.kind || appT("m028"), "violet")) +
    kvRow(appT("m463"), jobBadge(j)) +
    kvRow(appT("m562"), fmtTime(j.created_at)) +
    (j.started_at ? kvRow(appT("m563"), fmtTime(j.started_at)) : "") +
    (j.finished_at ? kvRow(appT("m564"), fmtTime(j.finished_at)) : "") +
    `</div>`;
  h += `<div class="d-sec"><span class="lbl">${appH("m565")}</span>`;
  const ph = j.phases || [];
  if (!ph.length) h += empty("clock", appT("m566"), "", true);
  else {
    h += `<div class="tl">` + ph.map(p => `<div class="tl-item">
      <span class="p-dot ${p.state === "ok" ? "ok" : p.state === "running" ? "running" : p.state === "failed" ? "failed" : p.state === "skipped" ? "skipped" : ""}"></span>
      <div class="p-body"><div class="p-name">${esc(JOB_ZH[p.name] || p.name)} ${phaseBadge(p.state)}</div>
      ${p.detail ? `<div class="p-detail">${esc(I18n.detail(p.detail, "app.operationDetails"))}</div>` : ""}
      <div class="p-time">${p.started_at ? fmtTime(p.started_at) : ""}${p.finished_at ? " → " + fmtTime(p.finished_at) : ""}</div></div>
    </div>`).join("") + `</div>`;
  }
  h += `</div>`;
  return h;
}
function jobDrawerFoot(j) {
  const st = jobState(j);
  let h = "";
  if (st === "rollback" || st === "rollback_verify") h += `<button class="btn ghost" data-act="job-rb-complete" data-id="${esc(j.id)}">${icon("undo", 15)} ${appH("m567")}</button>`;
  h += `<div class="spacer"></div>`;
  if (jobActive(j)) h += `<button class="btn primary" data-act="job-advance" data-id="${esc(j.id)}">${icon("chevR", 15)} ${st === "waiting_idle" ? appH("m568") : appH("m569")}</button>`;
  else h += `<button class="btn primary" data-act="drawer-close">${appH("m040")}</button>`;
  return h;
}
async function jobAdvance(id) {
  try {
    const j = await api("POST", `/jobs/${enc(id)}/advance`);
    const st = jobState(j);
    toast(st === "waiting_idle" ? appT("m570") : appT("m571", { value: JOB_ZH[st] || st }), st === "waiting_idle" ? "info" : "ok");
    await repaintJob(id); refreshTerminalCenter();
  } catch (e) { toast(e.message, "err"); }
}
async function jobRbComplete(id) {
  const ok = await confirmDlg({ title: appT("m567"), message: appT("m572"), okText: appT("m573"), danger: false });
  if (!ok) return;
  try {
    await api("POST", `/jobs/${enc(id)}/rollback-complete`, { recovered: true });
    toast(appT("m574"), "ok");
    await repaintJob(id); refreshTerminalCenter();
  } catch (e) { toast(e.message, "err"); }
}

/* ================= ROLLOUTS ================= */
function ringStatusZh(s) { return ({ pending: appT("m031"), running: appT("m032"), done: appT("m033"), failed: appT("m034") })[s] || s || appT("m031"); }
function paintRollouts() {
  const el = $("#dl-rollouts"); if (!el) return;
  if (!S.rollouts.length) { el.innerHTML = empty("branch", appT("m575"), `<button class="btn primary sm" data-act="ro-create">${icon("plus", 13)} ${appH("m576")}</button>`, true); return; }
  el.innerHTML = S.rollouts.map(ro => {
    const rings = ro.rings || [];
    const done = ro.status === "done";
    const segs = rings.map((r, i) => {
      const cls = r.status === "done" ? "done" : r.status === "failed" ? "failed" : (i === ro.current_ring && !done ? "active" : "");
      const flex = clamp(r.percent || 1, 1, 100);
      return `<div class="ring-seg ${cls}" style="flex:${flex}" title="${esc(r.name)} · ${r.percent}% · ${ringStatusZh(r.status)}">
        <div class="r-name">${esc(r.name || appT("m577") + (i + 1))}</div>
        <div class="r-pct">${esc(typeof (r.percent || 0) === "number" ? appN(r.percent || 0) : (r.percent || 0))}%</div>
        <div class="r-nodes">${appH("m578", { count: appN((r.nodes || []).length) })} ${ringStatusZh(r.status)}</div>
      </div>`;
    }).join("");
    const canAdv = !done && ro.status !== "failed";
    return `<div class="rollout-card">
      <div class="ro-top">
        <span class="ro-name">${esc(ro.name || shortId(ro.id))}</span>
        ${done ? badge(appT("m021"), "ok") : ro.status === "failed" ? badge(appT("m579"), "err") : badge(appT("m580"), "info")}
        <span class="tag-mono">${esc(ro.bom_id)}@${ro.bom_version || 0}</span>
        <div class="spacer"></div>
        ${canAdv ? `<button class="btn cyan xs" data-act="ro-advance" data-id="${esc(ro.id)}">${icon("chevR", 12)} ${appH("m581")}</button>` : ""}
        <button class="btn ghost xs" data-act="ro-delete" data-id="${esc(ro.id)}">${icon("trash", 12)} ${appH("m531")}</button>
      </div>
      <div class="rings">${segs || `<span class="hint">${appH("m582")}</span>`}</div>
      <div class="hint">${appH("m583", { count: Math.min((ro.current_ring || 0) + 1, rings.length), count2: appN(rings.length), time: ago(ro.created_at) })}</div>
    </div>`;
  }).join("");
}
const ROD = { rings: [] };
function openRolloutCreate() {
  const pub = (S.pubBoms || []).slice().sort((a, b) => a.bom_id === b.bom_id ? b.version - a.version : String(a.bom_id).localeCompare(String(b.bom_id)));
  ROD.rings = [{ get name() { return appT("m584"); }, percent: 10 }, { get name() { return appT("m585"); }, percent: 40 }, { get name() { return appT("m586"); }, percent: 50 }];
  const bomOpts = pub.length ? pub.map(b => { const v = `${b.bom_id}@${b.version}`; return `<option value="${esc(v)}">${esc(v)} · ${esc(b.role || appT("m316"))}</option>`; }).join("") : `<option value="">${appH("m587")}</option>`;
  openModal(`${icon("branch", 18)} ${appH("m588")}`, `
    <div id="ro-err"></div>
    <div class="f-row">
      <div class="field"><label>${appH("m427")}</label><input id="ro-name" class="inp" maxlength="60" placeholder="${appH("m589")}"></div>
      <div class="field"><label>${appH("m590")}</label><select id="ro-bom" class="inp">${bomOpts}</select></div>
    </div>
    <div class="banner info">${icon("info", 15)}<span>${appH("m591")}</span></div>
    <div class="m-sec"><span class="lbl">${appH("m592")}</span>
      <div id="ro-rings">${roRingsHtml()}</div>
      <div class="btn-row"><button class="btn ghost sm" data-act="ro-ring-add">${icon("plus", 13)} ${appH("m593")}</button><div class="spacer"></div><span class="tag-mono" id="ro-sum"></span></div>
    </div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn primary" data-act="ro-submit" ${pub.length ? "" : "disabled"}>${icon("branch", 15)} ${appH("m594")}</button>`, { narrow: true });
  paintRoSum();
}
function roRingsHtml() {
  return ROD.rings.map((r, i) => `<div class="ring-row" data-i="${i}">
    <input class="inp ro-name-i" placeholder="${appH("m595")}" value="${esc(r.name)}" maxlength="30">
    <input class="inp mono ro-pct-i" type="number" min="1" max="100" data-change="ro-pct" data-i="${i}" value="${r.percent}" style="text-align:center">
    <button class="row-del" data-act="ro-ring-del" data-i="${i}" title="${appH("m516")}" aria-label="${appH("m516")}">${icon("x", 14)}</button>
  </div>`).join("");
}
function readRoRings() {
  return $$("#ro-rings .ring-row").map(row => ({ name: ($(".ro-name-i", row) || {}).value || "", percent: Number(($(".ro-pct-i", row) || {}).value) || 0 }));
}
function paintRoSum() {
  const rings = readRoRings(); const sum = rings.reduce((s, r) => s + r.percent, 0);
  const el = $("#ro-sum"); if (el) { el.textContent = appT("m596", { value: sum }); el.className = "tag-mono " + (sum !== 100 ? "txt-err" : "txt-ok"); }
}
function roRingAdd() { ROD.rings = readRoRings(); ROD.rings.push({ get name() { return appT("m597"); }, percent: 10 }); const el = $("#ro-rings"); if (el) el.innerHTML = roRingsHtml(); paintRoSum(); }
function roRingDel(i) { ROD.rings = readRoRings(); ROD.rings.splice(i, 1); if (!ROD.rings.length) ROD.rings.push({ get name() { return appT("m586"); }, percent: 100 }); const el = $("#ro-rings"); if (el) el.innerHTML = roRingsHtml(); paintRoSum(); }
async function roSubmit() {
  const err = $("#ro-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const name = val("ro-name").trim();
  if (!name) return fail(appT("m598"));
  const sel = val("ro-bom");
  if (!sel) return fail(appT("m599"));
  const at = sel.lastIndexOf("@");
  const rings = readRoRings().filter(r => r.name.trim());
  if (!rings.length) return fail(appT("m600"));
  for (const r of rings) if (r.percent < 1 || r.percent > 100) return fail(appT("m601", { name: r.name }));
  const sum = rings.reduce((s, r) => s + r.percent, 0);
  if (sum !== 100) return fail(appT("m602", { value: sum }));
  try {
    await api("POST", "/rollouts", { name, bom_id: sel.slice(0, at), bom_version: Number(sel.slice(at + 1)) || 0, rings: rings.map(r => ({ name: r.name.trim(), percent: r.percent })) });
    toast(appT("m603", { name: name }), "ok");
    closeModal(); await refreshTerminalCenter();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
async function roAdvance(id) {
  try {
    const ro = await api("POST", `/rollouts/${enc(id)}/advance`);
    toast(ro && ro.status === "done" ? appT("m604") : appT("m605"), "ok");
    await refreshTerminalCenter();
  } catch (e) { toast(e.status === 409 ? appT("m606") : e.message, "err"); }
}
async function roDelete(id) {
  const ro = S.rollouts.find(r => r.id === id);
  const ok = await confirmDlg({ title: appT("m607"), message: appT("m608", { name: (ro && ro.name) || shortId(id) }), okText: appT("m531") });
  if (!ok) return;
  try {
    await api("DELETE", `/rollouts/${enc(id)}`);
    toast(appT("m609"), "ok");
    await refreshTerminalCenter();
  } catch (e) { toast(e.message, "err"); }
}

/* ================= AUTO UPDATE ================= */
const updateReads = new WeakMap();
const UPD_STATE_ZH = {
  get idle() { return [appT("m610"), "muted"]; }, get checking() { return [appT("m611"), "info"]; }, get staged() { return [appT("m612"), "info"]; },
  get applying() { return [appT("m613"), "info"]; }, get restarting() { return [appT("m614"), "warn"]; }, get failed() { return [appT("m034"), "err"]; }, get offline() { return [appT("m615"), "warn"]; }
};
const FLEET_STATE_ZH = {
  get converged() { return [appT("m616"), "ok"]; }, get pending() { return [appT("m617"), "info"]; }, get upgrading() { return [appT("m613"), "info"]; },
  get waiting_idle() { return [appT("m014"), "warn"]; }, get suspended() { return [appT("m026"), "err"]; }, get unsupported() { return [appT("m618"), "muted"]; }, get offline() { return [appT("m004"), "muted"]; }
};
function updStateBadge(st) {
  const s = st && st.state;
  if (s === "idle" && st.available) return badge(appT("m619"), "gold");
  const x = UPD_STATE_ZH[s] || [s || appT("m610"), "muted"];
  return badge(x[0], x[1]);
}
function fleetStateBadge(s) { const x = FLEET_STATE_ZH[s] || [s || appT("m617"), "muted"]; return badge(x[0], x[1]); }
async function refreshUpdate() {
  const root = $("#upd-body"), scope = root && root.dataset.updateScope;
  if (!root || !["server", "fleet"].includes(scope) || S.upd.busy) return;
  if (updateReads.has(root)) return updateReads.get(root);
  const current = () => $("#upd-body") === root && root.dataset.updateScope === scope;
  const read = (async () => {
    try {
      // Wait for every request, even on failure, before allowing another live tick.
      const requests = [api("GET", "/update/status")];
      if (scope === "fleet") requests.push(api("GET", "/update/fleet"));
      const results = await Promise.allSettled(requests);
      if (!current()) return;
      const failed = results.find(r => r.status === "rejected");
      if (failed) throw failed.reason;
      const status = results[0].value, fleet = scope === "fleet" ? results[1].value : null;
      if (!status || !status.server || (scope === "fleet" && (!status.policy || !fleet || !Array.isArray(fleet.nodes)))) {
        throw new Error(appT("m620"));
      }
      S.upd.disabled = false;
      S.upd.server = status.server;
      if (scope === "server") {
        if (!$("#upd-server", root) || $("#upd-policy, #upd-fleet", root)) root.innerHTML = `<div id="upd-server"></div>`;
        paintUpdateServer();
        settingsApplyAccess();
      } else {
        S.upd.policy = status.policy; S.upd.fleet = fleet;
        if (!$("#upd-policy", root) || !$("#upd-fleet", root) || $("#upd-server", root)) {
          root.innerHTML = `<div class="upd-pane" id="upd-policy"></div><div id="upd-fleet" class="mt16"></div>`;
        }
        paintUpdatePolicy(); paintUpdateFleet();
      }
    } catch (e) {
      if (!current()) return;
      S.upd.disabled = e.status === 503;
      root.innerHTML = `<div class="banner warn">${icon("info", 15)}<span>${esc(S.upd.disabled ? appT("m621") : appT("m622") + e.message)}</span></div>`;
    }
  })();
  updateReads.set(root, read);
  try { await read; } finally { updateReads.delete(root); }
}
function refreshUpdateLive() { return refreshUpdate(); }
function paintUpdateServer() {
  const el = $("#upd-server"); if (!el) return;
  const st = S.upd.server;
  if (!st) { el.innerHTML = `<div class="upd-pane-t">${icon("server", 15)} ${appH("m623")}</div>` + empty("refresh", appT("m269"), "", true); return; }
  const busy = st.state === "checking" || st.state === "applying" || st.state === "restarting";
  const canApply = st.available && !busy;
  let h = `<div class="upd-pane-t">${icon("server", 15)} ${appH("m623")} ${updStateBadge(st)}</div>`;
  h += `<div class="ver-flow">
    <div class="vf-box"><div class="vf-lbl">${appH("m624")}</div><div class="vf-ver">v${esc(st.current || "—")}</div></div>
    <div class="vf-arrow ${st.available ? "hot" : ""}">${icon("chevR", 20)}</div>
    <div class="vf-box ${st.available ? "new" : ""}"><div class="vf-lbl">${appH("m625")}</div><div class="vf-ver">${st.latest ? "v" + esc(st.latest) : "—"}</div></div>
  </div>`;
  if (st.state === "restarting") h += `<div class="banner info"><span class="spin"></span><span>${appH("m626")}</span></div>`;
  else if (st.state === "applying") h += `<div class="banner info"><span class="spin"></span><span>${esc(I18n.detail(st.detail, "app.m627"))}</span></div>`;
  else if (st.state === "failed") h += `<div class="banner err">${icon("alert", 15)}<span>${esc(I18n.detail(st.last_error || st.detail, "app.m628"))}</span></div>`;
  else if (st.state === "offline") h += `<div class="banner warn">${icon("wifi", 15)}<span>${esc(I18n.detail(st.detail, "app.m629"))}</span></div>`;
  else if (st.available) h += `<div class="banner ok">${icon("arrowUp", 15)}<span>${appH("m632", { version: st.latest })}${st.auto_apply ? appH("m630") : appH("m631")}</span></div>`;
  else if (st.detail) h += `<div class="hint mb12">${esc(st.detail)}</div>`;
  h += `<div class="kv mb12">` +
    kvRow(appT("m633"), `<span class="mono upd-feed" title="${esc(st.feed_url || "")}">${esc(st.feed_url || appT("m634"))}</span>`) +
    kvRow(appT("m635"), st.checked_at ? ago(st.checked_at) : appH("m272")) +
    (st.staged_at ? kvRow(appT("m636"), fmtTime(st.staged_at)) : "") +
    `</div>`;
  h += `<label class="switch mb12"><input type="checkbox" id="upd-auto" data-change="upd-auto" ${st.auto_apply ? "checked" : ""} ${busy ? "disabled" : ""}><span class="track"></span><span class="sw-lbl">${appH("m637")}</span></label>`;
  h += `<div class="btn-row">
    <button class="btn cyan sm" data-act="upd-check" ${busy ? "disabled" : ""}>${icon("refresh", 14)} ${appH("m640")}</button>
    <button class="btn primary sm" data-act="upd-apply" ${canApply ? "" : "disabled"}>${icon("arrowUp", 14)} ${st.available ? appH("m638") + esc(st.latest) : appH("m639")}</button>
  </div>`;
  el.innerHTML = h;
}
function paintUpdatePolicy(force = false) {
  const el = $("#upd-policy"); if (!el) return;
  const p = S.upd.policy;
  if (!p) { el.innerHTML = `<div class="upd-pane-t">${icon("sliders", 15)} ${appH("m641")}</div>` + empty("sliders", appT("m642"), "", true); return; }
  // Skip rebuilding while the operator is interacting with a control, so a live
  // refresh never yanks an open select or steals focus mid-change.
  if (!force && TC.admin && el.contains(document.activeElement) && el.dataset.built) { syncUpdatePolicy(); return; }
  const disabled = TC.admin ? "" : "disabled";
  const opt = (v, cur, lbl) => `<option value="${v}" ${Number(cur) === v ? "selected" : ""}>${esc(lbl || appN(v))}</option>`;
  el.innerHTML = `<div class="upd-pane-t">${icon("sliders", 15)} ${appH("m641")} <span class="hint">${TC.admin ? appH("m643") : appH("m644")}</span></div>
    <label class="switch mb12"><input type="checkbox" id="upd-en" data-change="upd-policy" ${disabled} ${p.enabled ? "checked" : ""}><span class="track"></span><span class="sw-lbl">${appH("m647")}</span></label>
    <div class="f-row">
      <div class="field"><label>${appH("m648")}</label><select id="upd-mp" class="inp" data-change="upd-policy" ${disabled}>${[1, 2, 3, 4, 6, 8, 12, 16].map(v => opt(v, p.max_parallel, appT("m691", { count: v }))).join("")}</select><div class="f-hint">${appH("m649")}</div></div>
      <div class="field"><label>${appH("m650")}</label><select id="upd-ma" class="inp" data-change="upd-policy" ${disabled}>${[1, 2, 3, 4, 5, 6, 8, 10].map(v => opt(v, p.max_attempts, appT("attemptCount", { count: v }))).join("")}</select><div class="f-hint">${appH("m651")}</div></div>
    </div>
    <div class="field"><label>${appH("m652")}</label><select id="upd-rp" class="inp" data-change="upd-policy" ${disabled}>${[5, 10, 20, 25, 50, 100].map(v => opt(v, p.ring_percent, appN(v / 100, { style: "percent" }))).join("")}</select><div class="f-hint">${appH("m653")}</div></div>
    <div class="banner info">${icon("shield", 15)}<span>${appH("m654")}</span></div>
    <div class="btn-row"><button class="btn cyan sm" data-act="upd-converge">${icon("send", 14)} ${appH("m655")}</button><div class="spacer"></div><span class="hint">${appH("m656", { version: (S.upd.fleet && S.upd.fleet.target) || (S.upd.server && S.upd.server.current) || "—" })}</span></div>`;
  el.dataset.built = "1";
}
function syncUpdatePolicy() {
  const p = S.upd.policy; if (!p) return;
  const en = $("#upd-en"), mp = $("#upd-mp"), ma = $("#upd-ma"), rp = $("#upd-rp");
  if (en && document.activeElement !== en) en.checked = !!p.enabled;
  if (mp && document.activeElement !== mp) mp.value = String(p.max_parallel);
  if (ma && document.activeElement !== ma) ma.value = String(p.max_attempts);
  if (rp && document.activeElement !== rp) rp.value = String(p.ring_percent);
}
function paintUpdateFleet() {
  const el = $("#upd-fleet"); if (!el) return;
  const rep = S.upd.fleet;
  if (!rep || !Array.isArray(rep.nodes)) { el.innerHTML = ""; return; }
  const nodes = rep.nodes;
  if (!nodes.length) { el.innerHTML = empty("server", appT("m657"), `<button class="btn primary sm" data-act="goto-add-node">${icon("plus", 13)} ${appH("m214")}</button>`, true); return; }
  const c = rep.counts || {};
  const parts = nodes.filter(r => r.state !== "offline" && r.state !== "unsupported");
  const conv = parts.filter(r => r.state === "converged").length;
  const pct = parts.length ? Math.round(conv / parts.length * 100) : 100;
  const stat = (k, lbl, color) => c[k] ? `<span class="ust" style="--uc:var(--${color})"><i></i>${esc(lbl)} <b>${esc(appN(c[k]))}</b></span>` : "";
  let h = `<div class="upd-stats">
    ${stat("converged", appT("m616"), "green")}${stat("upgrading", appT("m613"), "cyan")}${stat("pending", appT("m617"), "blue")}
    ${stat("waiting_idle", appT("m014"), "amber")}${stat("suspended", appT("m026"), "red")}${stat("unsupported", appT("m618"), "faint")}${stat("offline", appT("m004"), "faint")}
    <span class="spacer"></span><span class="tag-mono">${appH("m658", { value: rep.target || "—" })}</span>
  </div>`;
  h += meter(appT("m659", { value: (rep.ring || 0) + 1, count: rep.ring_size || 1 }), pct, appT("m660", { value: conv, count: appN(parts.length) }), pct === 100 ? "green" : "cyan");
  const list = nodes.filter(r => { const n = S.nodes.find(x => x.node_id === r.node_id); return hit(n && n.alias, r.node_id, r.current, r.target, r.state, r.detail); });
  if (!list.length) { el.innerHTML = h + empty("search", appT("m661"), "", true); return; }
  h += `<div class="tbl-wrap mt12"><table class="tbl"><thead><tr><th>${appH("m559")}</th><th>${appH("m395")}</th><th>${appH("m463")}</th><th>${appH("m662")}</th><th>${appH("m663")}</th><th>${appH("m664")}</th><th style="text-align:right">${appH("m399")}</th></tr></thead><tbody>` +
    list.map(r => {
      const n = S.nodes.find(x => x.node_id === r.node_id);
      const alias = n ? (n.alias || shortId(r.node_id)) : shortId(r.node_id);
      const same = r.current && r.current === r.target;
      return `<tr>
        <td><button class="btn ghost xs" data-act="node-open" data-id="${esc(r.node_id)}">${esc(alias)}</button><div class="sub mono">${esc(shortId(r.node_id))}${n && n.arch ? " · " + esc(n.arch) : ""}</div></td>
        <td class="mono nowrap">${same ? `<span class="txt-ok">${esc(r.current)}</span>` : `${esc(r.current || "—")} <span class="vf-mini">→</span> <span class="txt-gold">${esc(r.target || "—")}</span>`}</td>
        <td>${fleetStateBadge(r.state)}</td>
        <td class="mono">${esc(typeof (r.attempts || 0) === "number" ? appN(r.attempts || 0) : (r.attempts || 0))}${r.state === "suspended" ? " / " + ((rep.policy && rep.policy.max_attempts) || 3) : ""}</td>
        <td class="sub nowrap">${r.last_attempt ? ago(r.last_attempt) : "—"}</td>
        <td class="sub upd-detail" title="${esc(r.detail || "")}">${esc(r.detail || "—")}</td>
        <td class="acts">${r.state === "suspended" ? `<button class="btn cyan xs" data-act="upd-retry" data-id="${esc(r.node_id)}">${icon("undo", 12)} ${appH("m072")}</button>` : ""}</td>
      </tr>`;
    }).join("") + `</tbody></table></div>`;
  el.innerHTML = h;
}
async function updCheck() {
  const btn = $('[data-act="upd-check"]');
  if (btn) { btn.disabled = true; btn.innerHTML = `<span class="spin">${icon("refresh", 14)}</span> ${appH("m665")}`; }
  try {
    const st = await api("POST", "/update/check");
    S.upd.server = st;
    if (st.state === "offline") toast(appT("m666") + (st.detail || appT("m667")), "warn");
    else if (st.available) toast(appT("m668", { version: st.latest }), "ok");
    else toast(appT("m669", { version: st.current }), "info");
    paintUpdateServer();
  } catch (e) { toast(e.message, "err"); }
  finally { paintUpdateServer(); }
}
async function updApply() {
  const st = S.upd.server; if (!st || !st.available) return;
  const ok = await confirmDlg({
    title: appT("m670"), danger: false, okText: appT("m671"),
    message: appT("m672", { version: st.latest })
  });
  if (!ok) return;
  S.upd.busy = true;
  try {
    const r = await api("POST", "/update/apply");
    S.upd.server = r;
    toast(r && r.state === "restarting" ? appT("m673") : appT("m674"), "ok");
    paintUpdateServer();
  } catch (e) {
    toast(e.status === 409 ? appT("m675") : e.message, e.status === 409 ? "warn" : "err");
  } finally { S.upd.busy = false; }
}
async function updAuto(el) {
  const want = el.checked;
  try {
    const st = await api("POST", "/update/auto", { auto_apply: want });
    S.upd.server = st;
    toast(want ? appT("m676") : appT("m677"), "ok");
  } catch (e) { toast(e.message, "err"); }
  paintUpdateServer();
}
async function updPolicySave() {
  const body = {
    enabled: checked("upd-en"),
    max_parallel: Number(val("upd-mp")) || 2,
    max_attempts: Number(val("upd-ma")) || 3,
    ring_percent: Number(val("upd-rp")) || 20
  };
  try {
    const p = await api("POST", "/update/fleet/policy", body);
    S.upd.policy = p;
    if (S.upd.fleet) S.upd.fleet.policy = p;
    toast(appT("m678"), "ok");
    paintUpdateFleet();
  } catch (e) { toast(e.message, "err"); syncUpdatePolicy(); }
}
async function updConverge() {
  const btn = $('[data-act="upd-converge"]');
  if (btn) { btn.disabled = true; btn.innerHTML = `<span class="spin">${icon("send", 14)}</span> ${appH("m679")}`; }
  try {
    const rep = await api("POST", "/update/fleet/converge");
    S.upd.fleet = rep;
    const c = rep.counts || {};
    toast(appT("m680", { count: c.converged || 0, upgrading: c.upgrading || 0 }), "ok");
    paintUpdateFleet(); paintUpdatePolicy();
  } catch (e) { toast(e.message, "err"); }
  finally { paintUpdatePolicy(); }
}
async function updRetry(id) {
  try {
    await api("POST", `/update/fleet/${enc(id)}/retry`);
    toast(appT("m681"), "ok");
    await refreshUpdateLive();
  } catch (e) { toast(e.message, "err"); }
}

/* ================= KEYS & LICENSE ================= */
function shellKeys() {
  return `<div class="grid cols-2">
      <section class="card stagger" style="--i:0" id="lic-card"></section>
      <section class="card stagger" style="--i:1" id="gpg-card"></section>
    </div>
    <div class="banner info mt16">${icon("server", 16)}<span>${appH("m682")}</span><button class="btn ghost sm" data-act="tc-goto">${appH("m683")}</button></div>`;
}
async function refreshKeys() {
  const [lic, keys, nodes] = await Promise.all([
    api("GET", "/license").catch(() => null),
    api("GET", "/keys").catch(() => []),
    api("GET", "/nodes").catch(() => [])
  ]);
  S.license = lic; S.keys = Array.isArray(keys) ? keys : []; S.nodes = Array.isArray(nodes) ? nodes : [];
  paintLicense(); paintKeys();
}
function seatsHtml(used, seats) {
  const pct = seats > 0 ? clamp(used / seats * 100, 0, 100) : 0;
  return `<div class="seats"><div class="s-top"><span>${appH("m358")}</span><span class="mono">${esc(typeof (used) === "number" ? appN(used) : (used))} / ${esc(typeof (seats || "∞") === "number" ? appN(seats || "∞") : (seats || "∞"))}</span></div><div class="seat-bar"><div class="seat-fill ${seats && used > seats ? "over" : ""}" style="width:${pct}%"></div></div></div>`;
}
function paintLicense() {
  const el = $("#lic-card"); if (!el) return;
  const lic = S.license;
  const used = S.nodes.filter(n => n.authorized).length;
  const builtin = Number(lic && lic.builtin_seats) || 3;
  const activated = lic && lic.status === "activated";
  let h = secHead(`${icon("shield", 17)} ${appH("m430")}`, `${activated ? `<button class="btn ghost sm" data-act="lic-receipt">${icon("download", 14)} ${appH("m684")}</button>` : ""}<button class="btn violet sm" data-act="lic-open">${icon("key", 14)} ${activated ? appH("m685") : appH("m686")}</button>`);
  if (!lic || lic.status === "none") {
    h += `<div class="mb12">${badge(appT("m687"), "ok")}</div>`;
    h += `<div class="kv">` +
      kvRow(appT("m688"), appH("m689")) +
      kvRow(appT("m690"), `<span class="mono">${appH("m691", { count: builtin })}</span>`) +
      kvRow(appT("m430"), appH("m692")) +
      `</div>`;
    h += seatsHtml(used, builtin);
    h += `<div class="banner info mt16">${icon("shield", 15)}<span>${appH("m693", { count: builtin })}</span></div>`;
  } else {
    const st = lic.status;
    const seats = Number(lic.effective_seats) || builtin;
    h += `<div class="mb12">${st === "activated" ? badge(appT("m694"), "ok") : badge(appT("m695"), "err")}</div>`;
    h += `<div class="kv">` +
      kvRow(appT("m696"), `<span class="mono">${esc(lic.key || "—")}</span>`) +
      kvRow(appT("m697"), esc(lic.account || "—")) +
      kvRow(appT("m698"), `<span class="mono">${esc(typeof (lic.seats || "∞") === "number" ? appN(lic.seats || "∞") : (lic.seats || "∞"))}</span>`) +
      kvRow(appT("m699"), fmtTime(lic.activated_at)) +
      kvRow(appT("m700"), lic.expires_at ? fmtTime(lic.expires_at) : appH("m701")) +
      `</div>`;
    h += seatsHtml(used, seats);
    if (st === "expired") h += `<div class="banner warn mt16">${icon("alert", 15)}<span>${appH("m702", { count: builtin })}</span></div>`;
    else if (used >= seats) h += `<div class="banner warn mt16">${icon("info", 15)}<span>${appH("m703")}</span></div>`;
  }
  el.innerHTML = h;
}
function paintKeys() {
  const el = $("#gpg-card"); if (!el) return;
  let h = secHead(`${icon("key", 17)} ${appH("m704")}`, `<button class="btn ghost sm" data-act="key-export">${icon("download", 14)} ${appH("m705")}</button><button class="btn violet sm" data-act="key-import">${icon("upload", 14)} ${appH("m706")}</button>`);
  if (!S.keys.length) {
    h += empty("key", appT("m707"), `<button class="btn violet" data-act="key-import">${icon("upload", 15)} ${appH("m706")}</button>`);
    h += `<div class="banner info mt16">${icon("shield", 15)}<span>${appH("m708")}</span></div>`;
  } else {
    h += S.keys.map(k => `<div class="key-row">
      <span class="key-ic">${icon("key", 17)}</span>
      <div class="key-info">
        <div class="key-name">${esc(k.name || appT("m709"))} ${k.is_trust_root ? badge(appT("m710"), "gold") : ""}</div>
        <div class="key-fp">${esc(k.fingerprint || "—")}</div>
        <div class="key-meta">${esc(k.email || "—")} · ${esc(k.algo || "—")} · ${fmtTime(k.created_at)}</div>
      </div>
      ${k.is_trust_root ? "" : `<button class="btn ghost xs" data-act="key-trust" data-id="${esc(k.id)}">${icon("shield", 12)} ${appH("m711")}</button>`}
    </div>`).join("");
  }
  el.innerHTML = h;
}
async function nodeAuth(id, revoke) {
  try {
    await api("POST", `/nodes/${enc(id)}/${revoke ? "revoke" : "authorize"}`);
    toast(revoke ? appT("m363") : appT("m712"), "ok");
    await refreshCurrent();
  } catch (e) { toast(e.message, "err"); }
}

/* ---- license activation & GPG ---- */
function fileToText(input, cb) {
  const f = input && input.files && input.files[0]; if (!f) return;
  const r = new FileReader();
  r.onload = () => cb(String(r.result || ""));
  r.onerror = () => toast(appT("m713"), "err");
  r.readAsText(f);
}
function openLicenseActivate() {
  openModal(`${icon("key", 18)} ${appH("m686")}`, `
    <div id="lic-err"></div>
    <div class="banner violet">${icon("shield", 15)}<span>${appH("m714")}</span></div>
    <div class="field"><label>${appH("m715")}</label><input id="lic-pfile" type="file" class="inp" data-change="lic-pfile" accept=".json,application/json"></div>
    <div class="field"><label>${appH("m716")}</label><textarea id="lic-payload" class="inp" rows="7" placeholder='{"key":"ZEP-XXXX-XXXX","account":"studio@zephyr.hk","seats":8,"expires_at":1893456000}'></textarea></div>
    <div class="field"><label>${appH("m717")}</label><input id="lic-sfile" type="file" class="inp" data-change="lic-sfile" accept=".asc,.sig,.gpg,.txt"></div>
    <div class="field"><label>${appH("m718")}</label><textarea id="lic-sig" class="inp" rows="6" placeholder="-----BEGIN PGP SIGNATURE-----&#10;…&#10;-----END PGP SIGNATURE-----"></textarea><div class="f-hint">${appH("m719")}</div></div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn violet" data-act="lic-submit">${icon("check", 15)} ${appH("m720")}</button>`, { narrow: true });
}
async function licSubmit() {
  const err = $("#lic-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const payloadText = val("lic-payload").trim(), sigText = val("lic-sig").trim();
  if (!payloadText) return fail(appT("m721"));
  let p; try { p = JSON.parse(payloadText); } catch (e) { return fail(appT("m722") + e.message); }
  if (!p || !p.key) return fail(appT("m723"));
  if (!(Number(p.seats) >= 1)) return fail(appT("m724"));
  try {
    await api("POST", "/license/activate", { payload_b64: b64utf8(payloadText), sig_b64: b64utf8(sigText) });
    toast(appT("m725"), "ok");
    closeModal();
    if (S.view === "keys") await refreshKeys(); else await refreshCurrent();
    globalTick();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
function openKeyImport() {
  openModal(`${icon("upload", 18)} ${appH("m726")}`, `
    <div id="gpg-err"></div>
    <div class="field"><label>${appH("m727")}</label><input id="gpg-file" type="file" class="inp" data-change="gpg-file" accept=".asc,.gpg,.pub,.txt"></div>
    <div class="field"><label>${appH("m728")}</label><textarea id="gpg-armored" class="inp" rows="10" placeholder="-----BEGIN PGP PUBLIC KEY BLOCK-----&#10;…&#10;-----END PGP PUBLIC KEY BLOCK-----"></textarea><div class="f-hint">${appH("m729")}</div></div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn violet" data-act="key-import-submit">${icon("upload", 15)} ${appH("m372")}</button>`, { narrow: true });
}
async function keyImportSubmit() {
  const err = $("#gpg-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const armored = val("gpg-armored").trim();
  if (!armored.includes("BEGIN PGP PUBLIC KEY BLOCK")) return fail(appT("m730"));
  try {
    const r = await api("POST", "/keys/import", { armored });
    const n = r && Array.isArray(r.imported) ? r.imported.length : 0;
    toast(n ? appT("m731", { count: n }) : appT("m732"), "ok");
    closeModal(); await refreshKeys();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
async function keyTrust(id) {
  const ok = await confirmDlg({ title: appT("m711"), message: appT("m733"), okText: appT("m711"), danger: false });
  if (!ok) return;
  try {
    await api("POST", `/keys/${enc(id)}/trust-root`);
    toast(appT("m734"), "ok");
    await refreshKeys();
  } catch (e) { toast(e.message, "err"); }
}
async function keyExport() {
  try {
    const r = await fetch(API_BASE + "/keys/trust-root.asc");
    if (!r.ok) {
      const text = await r.text();
      let data = null; try { data = JSON.parse(text); } catch (e) { /* non-JSON response */ }
      const err = new Error(I18n.message(data, appT("exportFailed", { status: r.status })));
      err.status = r.status; err.data = data; err.code = data && (data.code || data.error?.code);
      err.rawMessage = data?.error?.message || data?.error || data?.message || text;
      throw err;
    }
    const text = await r.text();
    if (!text.trim()) { toast(appT("m736"), "warn"); return; }
    const url = URL.createObjectURL(new Blob([text], { type: "application/pgp-keys" }));
    const a = document.createElement("a");
    a.href = url; a.download = "trust-root.asc";
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 4000);
    toast(appT("m737"), "ok");
  } catch (e) { toast(e.message, "err"); }
}

/* ================= AUDIT ================= */
function shellAudit() {
  return `<section class="card stagger" style="--i:0">
      ${secHead(`${icon("audit", 17)} ${appH("m068")}`, `<button class="btn ghost sm" data-act="audit-export" data-fmt="csv">${icon("download", 14)} CSV</button><button class="btn ghost sm" data-act="audit-export" data-fmt="json">${icon("download", 14)} JSON</button>`)}
      <div class="chips mb12" id="au-chips"></div>
      <div id="au-table"></div>
    </section>`;
}
async function refreshAudit() {
  const rows = await api("GET", "/audit?limit=200");
  S.audit = Array.isArray(rows) ? rows : [];
  paintAudit();
}
function auditActionBadge(a) {
  const s = String(a || "");
  const k = /delete|revoke|ban|rollback|fail|deny/.test(s) ? "err" : /create|import|activate|authorize|publish|add|register|advance/.test(s) ? "ok" : /update|patch|config|set|trust/.test(s) ? "violet" : "info";
  return badge(s || "—", k);
}
function paintAudit() {
  const chips = $("#au-chips"), tbl = $("#au-table");
  if (!chips || !tbl) return;
  const counts = {};
  S.audit.forEach(r => { counts[r.action] = (counts[r.action] || 0) + 1; });
  const top = Object.keys(counts).sort((a, b) => counts[b] - counts[a]).slice(0, 10);
  chips.innerHTML = `<button class="chip ${S.auditFilter === "all" ? "active" : ""}" data-act="audit-filter" data-f="all">${appH("m738")} <span class="cnt">${esc(typeof (S.audit.length) === "number" ? appN(S.audit.length) : (S.audit.length))}</span></button>` +
    top.map(a => `<button class="chip ${S.auditFilter === a ? "active" : ""}" data-act="audit-filter" data-f="${esc(a)}">${esc(a)} <span class="cnt">${esc(typeof (counts[a]) === "number" ? appN(counts[a]) : (counts[a]))}</span></button>`).join("");
  let list = S.audit.slice();
  if (S.auditFilter !== "all") list = list.filter(r => r.action === S.auditFilter);
  list = list.filter(r => hit(r.actor, r.action, r.target, r.detail, r.ip, r.result));
  list.sort((a, b) => ts(b.at) - ts(a.at));
  if (!list.length) { tbl.innerHTML = empty("audit", S.audit.length ? appT("m739") : appT("m740"), "", true); return; }
  tbl.innerHTML = `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>${appH("m741")}</th><th>${appH("m742")}</th><th>${appH("m743")}</th><th>${appH("m744")}</th><th>${appH("m299")}</th><th>${appH("m300")}</th></tr></thead><tbody>` +
    list.map(r => `<tr>
      <td class="sub nowrap">${fmtTime(r.at)}</td>
      <td>${esc(r.actor || "—")}<div class="sub mono">${esc(r.ip || "")}</div></td>
      <td>${auditActionBadge(r.action)}</td>
      <td><span class="mono truncate" style="max-width:280px;display:inline-block;vertical-align:bottom">${esc(r.target || "—")}</span>${r.detail ? `<div class="sub truncate" style="max-width:320px">${esc(r.detail)}</div>` : ""}</td>
      <td>${resultBadge(r.result)}</td>
      <td class="cell-mono sub">${r.duration_ms != null ? r.duration_ms + " ms" : "—"}</td>
    </tr>`).join("") + `</tbody></table></div>`;
}
function auditExport(fmt) {
  download(`/audit/export?format=${fmt === "json" ? "json" : "csv"}`);
  toast(appT("m745", { value: fmt.toUpperCase() }), "info");
}

/* ================= ALERTS ================= */
function openAlerts() {
  openDrawer(`${icon("bell", 18)} ${appH("m746")}`, appH("m747"), alertsHtml(), "", { ctx: { kind: "alerts" } });
}
function paintAlerts() {
  if (!drawerCtx || drawerCtx.kind !== "alerts") return;
  setDrawerBody(alertsHtml());
}
function alertsHtml() {
  const counts = { open: 0, ack: 0, resolved: 0 };
  S.alerts.forEach(a => { const s = a.status === "acked" ? "ack" : a.status; if (counts[s] != null) counts[s]++; });
  let h = `<div class="btn-row mb12">
      <button class="btn cyan sm" data-act="alert-scan">${icon("refresh", 14)} ${appH("m748")}</button>
      <button class="btn ghost sm" data-act="alert-ack-all" ${counts.open ? "" : "disabled"}>${icon("check", 14)} ${appH("m749")}</button>
    </div>
    <div class="chips mb12">` +
    [["open", appT("m053"), counts.open], ["ack", appT("m054"), counts.ack], ["resolved", appT("m055"), counts.resolved], ["all", appT("m738"), S.alerts.length]]
      .map(x => `<button class="chip ${S.alertFilter === x[0] ? "active" : ""}" data-act="alert-filter" data-f="${x[0]}">${esc(x[1])} <span class="cnt">${esc(appN(x[2]))}</span></button>`).join("") +
    `</div>`;
  let list = S.alerts.slice();
  if (S.alertFilter !== "all") list = list.filter(a => (a.status === "acked" ? "ack" : a.status) === S.alertFilter);
  list = list.filter(a => hit(a.message, a.kind, a.node_id, a.severity));
  list.sort((a, b) => ts(b.at) - ts(a.at));
  if (!list.length) return h + empty("bell", S.alerts.length ? appT("m750") : appT("m751"), "", true);
  h += list.map(a => {
    const st = a.status === "acked" ? "ack" : a.status;
    return `<div class="alert-item sev-${esc(a.severity || "info")}">
      <div class="a-top">${sevBadge(a.severity)}${alertStatusBadge(a)}<span class="badge muted">${esc(a.kind || "alert")}</span><span class="spacer"></span><span class="txt-faint" style="font-size:11px">${ago(a.at)}</span></div>
      <div class="a-msg">${esc(I18n.has("app.alert_" + a.kind) ? appT("alert_" + a.kind) : a.message || "—")}</div>
      ${a.message && I18n.has("app.alert_" + a.kind) ? `<details class="a-meta"><summary>${appH("originalDiagnostic")}</summary><div>${esc(a.message)}</div></details>` : ""}
      <div class="a-meta">${a.node_id ? `<button class="btn ghost xs" data-act="node-open" data-id="${esc(a.node_id)}">${icon("server", 12)} ${esc(shortId(a.node_id))}</button>` : ""}<span class="txt-faint">${fmtTime(a.at)}</span></div>
      <div class="a-btns">
        ${st === "open" ? `<button class="btn ghost xs" data-act="alert-ack" data-id="${esc(a.id)}">${icon("eye", 12)} ${appH("m062")}</button>` : ""}
        ${st !== "resolved" ? `<button class="btn primary xs" data-act="alert-resolve" data-id="${esc(a.id)}">${icon("check", 12)} ${appH("m752")}</button>` : ""}
      </div>
    </div>`;
  }).join("");
  return h;
}
async function refetchAlerts() {
  try {
    const al = await api("GET", "/alerts");
    S.alerts = Array.isArray(al) ? al : [];
    paintBell(); paintAlerts();
    if (S.view === "overview") paintOverviewKpis();
  } catch (e) { /* 连接状态由 api 层处理 */ }
}
async function alertAct(id, act) {
  try {
    await api("POST", `/alerts/${enc(id)}/${act}`);
    toast(act === "ack" ? appT("m753") : appT("m754"), "ok");
    await refetchAlerts();
  } catch (e) { toast(e.message, "err"); }
}
async function alertAckAll() {
  try { await api("POST", "/alerts/ack-all"); toast(appT("m755"), "ok"); await refetchAlerts(); }
  catch (e) { toast(e.message, "err"); }
}
async function alertScan() {
  try { await api("POST", "/alerts/scan"); toast(appT("m756"), "ok"); await refetchAlerts(); }
  catch (e) { toast(e.message, "err"); }
}

/* ================= MUSIC LIBRARY ================= */
function fmtDur(ms) {
  const s = Math.max(0, Math.round((Number(ms) || 0) / 1000));
  return appN(Math.floor(s / 60), { useGrouping: false }) + ":" + p2(s % 60);
}
function codecBadge(c) {
  const k = String(c || "").toLowerCase();
  const lossless = ["flac", "alac", "pcm", "wav", "aiff", "dsd"];
  const kind = !k || k === "unknown" ? "muted" : lossless.some(x => k.includes(x)) ? "ok" : "warn";
  return badge(c || appT("m005"), kind);
}
function mountBadge(st) {
  if (st === "mounted") return badge(appT("m757"), "ok");
  if (st === "error") return badge(appT("m758"), "err");
  return badge(appT("m759"), "muted");
}
function shellLibrary() { return shellLibraryBrowser(); }
async function fetchPlay(nodes) {
  const online = (nodes || []).filter(n => n.status === "online");
  const entries = await Promise.all(online.map(async n => {
    try { return [n.node_id, await api("GET", `/nodes/${enc(n.node_id)}/play`)]; }
    catch (e) { return [n.node_id, { node_id: n.node_id, connected: false, playing: false }]; }
  }));
  const map = {};
  for (const [id, p] of entries) map[id] = p || {};
  S.play = map;
}
async function refreshLibrary() { await refreshLibraryBrowser(); }
async function refreshLibraryLive() {
  if (S.libTab === "playlists") await refreshLibraryBrowser();
}
function paintLibTabs() { paintLibraryBrowserHeader(); }
function paintLibTab() { paintLibraryBrowserItems(); }
function plRow(p) {
  return `<div class="q-row">
    <span class="q-idx mono">${icon("layers", 12)}</span>
    <span class="q-info">
      <span class="q-title truncate">${esc(p.name)}</span>
      <span class="q-sub txt-faint">${appH("m760", { count: p.count, time: fmtTime(p.created_at), time2: ago(p.updated_at) })}</span>
    </span>
    <span class="q-acts">
      <button class="btn xs ghost" data-act="pl-open" data-id="${esc(p.id)}" title="${appH("m761")}">${icon("eye", 12)} ${appH("m762")}</button>
      <button class="btn xs primary" data-act="pl-push" data-id="${esc(p.id)}" ${p.count ? "" : "disabled"} title="${appH("m763")}">${icon("send", 12)} ${appH("m764")}</button>
    </span>
  </div>`;
}
function albumHue(s) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return h % 360;
}
function coverArt(album, artist, cls, icSize, hash) {
  if (hash) return `<img class="cover ${cls || ""}" src="/api/v1/covers/${esc(hash)}" alt="" loading="lazy">`;
  const hue = albumHue(String(artist || "") + "/" + String(album || ""));
  return `<div class="cover ${cls || ""}" style="--h:${hue}">${icon("disc", icSize || 20)}</div>`;
}
function albumCoverHash(g) {
  const t = g.tracks.find(x => x.cover_hash);
  return t ? t.cover_hash : "";
}

/* ---- add SMB share ---- */
function openMountAdd() {
  const win = S.health && S.health.platform === "windows";
  openModal(`${icon("folder", 18)} ${appH("m765")}`, `
    <div id="mnt-err"></div>
    <div class="f-row">
      <div class="field"><label>${appH("m771")}</label><input id="mnt-host" class="inp mono" placeholder="${appH("m772")}" maxlength="120"><div class="f-hint">${appH("m773")}</div></div>
      <div class="field"><label>${appH("m774")}</label><input id="mnt-share" class="inp mono" placeholder="${appH("m775")}" maxlength="120"><div class="f-hint">${appH("m776")}</div></div>
    </div>
    <div class="f-row">
      <div class="field"><label>${appH("m777")}</label><input id="mnt-user" class="inp mono" placeholder="${appH("m778")}" maxlength="120"></div>
      <div class="field"><label>${appH("m779")}</label><input id="mnt-pass" type="password" class="inp mono" placeholder="${appH("m780")}" maxlength="200"></div>
    </div>
    <div class="f-row">
      <div class="field"><label>${appH("m781")}</label><input id="mnt-domain" class="inp mono" placeholder="${appH("m782")}" maxlength="120"></div>
      ${win ? "" : `<div class="field"><label>${appH("m766")}</label><select id="mnt-ver" class="inp"><option value="3.0">${appH("m767")}</option><option value="3.1.1">3.1.1</option><option value="2.1">2.1</option><option value="2.0">2.0</option><option value="1.0">1.0</option></select></div>`}
    </div>
    ${win ? "" : `<div class="field"><label class="switch"><input id="mnt-rw" type="checkbox"><span class="track"></span><span class="sw-lbl">${appH("m768")}</span></label></div>`}
    <div class="banner info">${icon("info", 15)}<span>${win
      ? appH("m769")
      : appH("m770")}</span></div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn primary" data-act="lib-add-submit">${icon("check", 15)} ${appH("m783")}</button>`, { narrow: true });
}
async function submitMountAdd() {
  const err = $("#mnt-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const host = val("mnt-host").trim(), share = val("mnt-share").trim();
  if (!host) return fail(appT("m784"));
  if (!share) return fail(appT("m785"));
  if (/[/\\\s]/.test(host)) return fail(appT("m786"));
  if (/[/\\\s]/.test(share)) return fail(appT("m787"));
  try {
    const result = await api("POST", "/mounts", {
      host, share, username: val("mnt-user").trim(), password: val("mnt-pass"),
      domain: val("mnt-domain").trim(), version: val("mnt-ver"), writable: checked("mnt-rw")
    });
    toast(result?.reused ? appT("m788", { value: host, value2: share }) : appT("m789", { value: host, value2: share }), "ok");
    closeModal();
    await refreshCurrent();
  } catch (e) { fail(e.message); }
}
async function libMountAct(id, act) {
  const m = S.mounts.find(x => x.id === id);
  const label = m ? (m.kind === "local" ? m.target : m.host + "/" + m.share) : shortId(id);
  try {
    if (act === "scan" || act === "scan-full") {
      if (act === "scan-full" && !await confirmDlg({ title: appT("m790"), message: appT("m791"), okText: appT("m792") })) return;
      const r = await api("POST", `/mounts/${enc(id)}/scan${act === "scan-full" ? "?mode=full" : ""}`);
      toast(appT("m793", { count: r.count ?? 0, value: r.added ?? 0, value2: r.updated ?? 0, value3: r.unchanged ?? 0, value4: r.removed ?? 0 }), "ok");
    } else {
      const r = await api("POST", `/mounts/${enc(id)}/${act}`);
      if (r && r.status === "error") {
        toast(appT("m796", { value: act === "mount" ? appT("m794") : appT("m795"), message: r.detail || appT("m071") }), "err");
      } else {
        toast(act === "mount" ? appT("m797", { value: label }) : appT("m798", { value: label }), "ok");
      }
    }
    await refreshCurrent();
  } catch (e) { toast(e.message, "err"); await refreshCurrent(); }
}
async function libArchive(id) {
  const ok = await confirmDlg({ title: appT("m799"), message: appT("m800"), okText: appT("m801") });
  if (!ok) return;
  try { await api("POST", `/mounts/${enc(id)}/archive`); toast(appT("m802"), "ok"); await refreshCurrent(); }
  catch (e) { toast(e.message, "err"); }
}
async function libDelete(id) {
  const m = S.mounts.find(x => x.id === id);
  const label = m ? (m.kind === "local" ? m.target : m.host + "/" + m.share) : shortId(id);
  const ok = await confirmDlg({ title: appT("m803"), message: appT("m804", { value: label }), okText: appT("m805"), danger: true });
  if (!ok) return;
  try { await api("DELETE", `/mounts/${enc(id)}`); toast(appT("m806", { value: label }), "ok"); await refreshCurrent(); }
  catch (e) { toast(e.message, "err"); }
}
async function libRefresh() { try { await refreshCurrent(); toast(appT("m807"), "ok"); } catch (e) { toast(e.message, "err"); } }

/* ---- push playback: terminal picker (single track / album / playlist) ---- */
const TP = { cur: null, trackIDs: [], track: null, label: "", playlistID: "", sel: "", busy: false };
async function openTrackPlay(trackID) {
  const t = await api("GET", `/tracks/${enc(trackID)}`);
  await openPushPicker({ trackIDs: [trackID], track: t, label: t.title || appT("m171") });
}
async function openPlPush(id) {
  let d;
  try { d = await api("GET", `/playlists/${enc(id)}`); } catch (e) { toast(e.message, "err"); return; }
  const ids = (d.items || []).filter(t => !t.missing).map(t => t.track_id);
  if (!ids.length) { toast(appT("m808"), "warn"); return; }
  openPushPicker({ trackIDs: ids, label: d.name, playlistID: id });
}
async function openPushPicker(opts) {
  TP.cur = opts; TP.trackIDs = opts.trackIDs; TP.track = opts.track || null;
  TP.label = opts.label || ""; TP.playlistID = opts.playlistID || ""; TP.sel = ""; TP.busy = false;
  openModal(`${icon("send", 18)} ${appH("m809")}`, `<div id="tp-body">${empty("refresh", appT("m172"), "", true)}</div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn" id="tp-next" data-act="track-next-go" disabled>${appH("m810")}</button><button class="btn" id="tp-enq" data-act="track-enqueue-go" disabled>${icon("plus", 15)} ${appH("m811")}</button><button class="btn primary" id="tp-go" data-act="track-play-go" disabled>${icon("play", 15)} ${appH("m809")}</button>`, { narrow: true });
  try {
    const nodes = await api("GET", "/nodes");
    S.nodes = Array.isArray(nodes) ? nodes : [];
    await fetchPlay(S.nodes);
  } catch (e) { /* keep cached nodes */ }
  if (!$(".modal") || TP.cur !== opts) return;
  if (S.nodes.some(n => n.node_id === PB.id && n.status === "online" && n.authorized)) TP.sel = PB.id;
  paintTrackPlay();
}
function paintTrackPlay() {
  const body = $("#tp-body"); if (!body) return;
  const t = TP.track || {};
  const online = S.nodes.filter(n => n.status === "online");
  const head = TP.track
    ? `<div class="tp-track">
      <div class="tp-title truncate">${esc(t.title || appT("m171"))}</div>
      <div class="tp-meta">${esc(t.artist || "—")} · ${esc(t.album || "—")} ${codecBadge(t.codec)} <span class="mono">${t.rate ? appFixed(t.rate / 1000, 1) + " kHz" : ""}${t.channels ? " · " + t.channels + " ch" : ""}${t.bits ? " · " + t.bits + " bit" : ""}</span></div>
    </div>`
    : `<div class="tp-track">
      <div class="tp-title truncate">${esc(TP.label)}</div>
      <div class="tp-meta">${appH("m814", { count: appN(TP.trackIDs.length) })}${TP.playlistID ? appH("m812") : appH("m813")}</div>
    </div>`;
  let h = head + `<div class="divider"></div><span class="lbl">${appH("m815", { count: appN(online.length) })}</span>`;
  if (!online.length) {
    h += empty("server", appT("m816"), `<button class="btn ghost sm" data-act="tp-goto-nodes">${icon("server", 14)} ${appH("m683")}</button>`, true);
  } else {
    h += `<div class="tp-nodes">` + online.map(n => {
      const p = S.play[n.node_id] || {};
      const sel = TP.sel === n.node_id;
      return `<div class="tp-node ${sel ? "sel" : ""} ${n.authorized ? "" : "dis"}" data-act="tp-pick" data-id="${esc(n.node_id)}" role="button" tabindex="0">
        <span class="tp-radio"></span>
        <span class="tp-ninfo">
          <span class="tp-nalias truncate">${esc(n.alias || shortId(n.node_id))}</span>
          <span class="tp-nmeta mono">${esc(shortId(n.node_id))}${n.ip ? " · " + esc(n.ip) : ""}</span>
        </span>
        <span class="tp-nbadges">${authBadge(n)}${p.connected ? badge(appT("m817"), "ok") : badge(appT("m818"), "muted")}${n.playing ? badge(appT("m819"), "info") : ""}</span>
      </div>`;
    }).join("") + `</div>`;
    h += `<div class="f-hint mt8">${appH("m820")}</div>`;
    if (!online.some(n => n.authorized)) h += `<div class="banner warn mt12">${icon("alert", 15)}<span>${appH("m821")}</span></div>`;
    else if (TP.sel && !(S.nodes.find(n => n.node_id === TP.sel) || {}).authorized) h += `<div class="banner warn mt12">${icon("alert", 15)}<span>${appH("m822")}</span></div>`;
  }
  body.innerHTML = h;
  const go = $("#tp-go"); if (go) go.disabled = !TP.sel || TP.busy;
  const enq = $("#tp-enq"); if (enq) enq.disabled = !TP.sel || TP.busy;
  const next = $("#tp-next"); if (next) next.disabled = !TP.sel || TP.busy;
}
async function trackPlayGo() {
  const nodeID = TP.sel, selection = TP.cur; if (!nodeID || TP.busy || !TP.trackIDs.length) return;
  if (!await confirmDlg({ title: appT("m823"), message: appT("m824", { count: appN(TP.trackIDs.length) }), okText: appT("m825"), danger: true })) return;
  if (TP.cur !== selection || TP.busy || TP.sel !== nodeID) return;
  const n = S.nodes.find(x => x.node_id === nodeID);
  if (n && !n.authorized) { toast(appT("m826"), "warn"); return; }
  TP.busy = true;
  const go = $("#tp-go"); if (go) { go.disabled = true; go.innerHTML = `${icon("refresh", 15)} ${appH("m827")}`; }
  const label = n ? (n.alias || shortId(nodeID)) : shortId(nodeID);
  try {
    let r;
    if (TP.playlistID) {
      r = await api("POST", `/playlists/${enc(TP.playlistID)}/push`, { node_id: nodeID });
    } else {
      r = await api("POST", `/nodes/${enc(nodeID)}/play`, { track_id: TP.trackIDs[0] });
      if (TP.trackIDs.length > 1) await api("PUT", `/nodes/${enc(nodeID)}/queue`, { track_ids: TP.trackIDs, position: 0 });
    }
    toast(appT("m828", { count: TP.trackIDs.length > 1 ? " " + TP.trackIDs.length + appT("m124") : "", value: label, id: r && r.session_id ? " · " + shortId(r.session_id) : "" }), "ok");
    closeModal();
    await refreshCurrent();
  } catch (e) {
    TP.busy = false;
    if (go) { go.disabled = false; go.innerHTML = `${icon("play", 15)} ${appH("m809")}`; }
    toast(e.message, "err");
  }
}
async function trackEnqueueGo(next = false) {
  const nodeID = TP.sel; if (!nodeID || TP.busy || !TP.trackIDs.length) return;
  const n = S.nodes.find(x => x.node_id === nodeID);
  if (n && !n.authorized) { toast(appT("m826"), "warn"); return; }
  TP.busy = true;
  const enq = $("#tp-enq"); if (enq) { enq.disabled = true; enq.innerHTML = `${icon("refresh", 15)} ${appH("m829")}`; }
  const label = n ? (n.alias || shortId(nodeID)) : shortId(nodeID);
  try {
    const r = await api("POST", `/nodes/${enc(nodeID)}/queue/batch`, { track_ids: TP.trackIDs, next });
    toast(appT("m833", { value: next ? appT("m830") : appT("m831"), value2: label, count: appN(TP.trackIDs.length), id: r && r.session_id ? appT("m832") : "" }), "ok");
    closeModal();
    await refreshCurrent();
  } catch (e) {
    TP.busy = false;
    if (enq) { enq.disabled = false; enq.innerHTML = `${icon("plus", 15)} ${appH("m811")}`; }
    toast(e.message, "err");
  }
}
/* ---- album detail ---- */
function openAlbum(key) {
  const g = albumByKey(key); if (!g) { toast(appT("m834"), "warn"); return; }
  const dec = g.tracks.filter(t => t.decodable).length;
  const rows = g.tracks.map((t, i) => `
    <div class="q-row">
      <span class="q-idx mono">${i + 1}</span>
      <span class="q-info">
        <span class="q-title truncate">${esc(t.title || "—")}</span>
        <span class="q-sub txt-faint">${esc(t.codec || "?")}${t.rate ? " · " + appFixed(t.rate / 1000, 1) + " kHz" : ""}${t.duration_ms ? " · " + fmtDur(t.duration_ms) : ""}${t.decodable ? "" : appH("m835")}</span>
      </span>
      <span class="q-acts">
        <button class="btn xs ghost" data-act="track-pl" data-id="${esc(t.id)}" title="${appH("m836")}">${icon("layers", 12)}</button>
        <button class="btn xs ghost" data-act="track-play" data-id="${esc(t.id)}" ${t.decodable ? "" : "disabled"} title="${appH("m763")}">${icon("send", 12)}</button>
      </span>
    </div>`).join("");
  openModal(`${icon("disc", 18)} ${appH("m099")}`, `
    <div style="display:flex;gap:12px;align-items:center">
      ${coverArt(g.album, g.artist, "thumb", 22, albumCoverHash(g))}
      <div class="tp-track" style="min-width:0">
        <div class="tp-title truncate">${esc(g.album || appT("m128"))}</div>
        <div class="tp-meta">${appH("m838", { value: g.artist || appT("m117"), count: appN(g.tracks.length), value2: fmtDur(g.ms) })}${dec < g.tracks.length ? appH("m837", { value: dec }) : ""}</div>
      </div>
    </div>
    <div class="divider"></div>
    <div class="q-list">${rows}</div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m040")}</button><div class="spacer"></div>
     ${typeof favoriteAlbumButton === "function" && g.tracks.length && g.tracks.every(t => (t.album_artist || t.artist || "") === (g.tracks[0].album_artist || g.tracks[0].artist || "")) ? favoriteAlbumButton(g.tracks[0].album_artist || g.tracks[0].artist, g.album) : ""}
     <button class="btn" data-act="album-pl" data-ak="${esc(key)}">${icon("layers", 15)} ${appH("m836")}</button>
     <button class="btn primary" data-act="album-push" data-ak="${esc(key)}" ${dec ? "" : "disabled"}>${icon("send", 15)} ${appH("m839")}</button>`);
}
function albumPush(key) {
  const g = albumByKey(key); if (!g) { toast(appT("m834"), "warn"); return; }
  const ids = g.tracks.filter(t => t.decodable).map(t => t.id);
  if (!ids.length) { toast(appT("m840"), "warn"); return; }
  openPushPicker({ trackIDs: ids, label: (g.album || appT("m128")) + " · " + (g.artist || appT("m117")) });
}

/* ---- add tracks to a playlist (picker + create) ---- */
const PP = { ids: [] };
const PLNEW = { ids: [] };
async function openPlPicker(trackIDs) {
  PP.ids = trackIDs;
  openModal(`${icon("layers", 18)} ${appH("m836")}`, `<div id="pp-body">${empty("refresh", appT("m149"), "", true)}</div>`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn primary" data-act="pl-pick-new">${icon("plus", 15)} ${appH("m841")}</button>`, { narrow: true });
  try { const p = await api("GET", "/playlists"); S.playlists = Array.isArray(p) ? p : []; } catch (e) { /* keep cached */ }
  if (!$(".modal")) return;
  const el = $("#pp-body"); if (!el) return;
  el.innerHTML = `<div class="f-hint mb8">${appH("m842", { count: appN(PP.ids.length) })}</div>` +
    (S.playlists.length
      ? `<div class="q-list">` + S.playlists.map(p => `
        <div class="q-row">
          <span class="q-idx mono">${icon("layers", 12)}</span>
          <span class="q-info"><span class="q-title truncate">${esc(p.name)}</span><span class="q-sub txt-faint">${appH("m151", { count: p.count })}</span></span>
          <span class="q-acts"><button class="btn xs primary" data-act="pl-pick" data-id="${esc(p.id)}">${icon("plus", 12)} ${appH("m843")}</button></span>
        </div>`).join("") + `</div>`
      : empty("layers", appT("m844"), "", true));
}
async function plPick(id) {
  try {
    await api("POST", `/playlists/${enc(id)}/items`, { track_ids: PP.ids });
    toast(appT("m845", { count: appN(PP.ids.length) }), "ok");
    closeModal();
    await syncPlaylists();
  } catch (e) { toast(e.message, "err"); }
}
function openPlNew(ids, reopenPlayerID) {
  PLNEW.ids = ids || [];
  PLNEW.reopen = reopenPlayerID || "";
  openModal(`${icon("layers", 18)} ${appH("m846")}`, `
    <div id="pln-err"></div>
    <div class="field"><label>${appH("m427")}</label><input id="pln-name" class="inp" maxlength="80" placeholder="${appH("m848")}"></div>
    ${PLNEW.ids.length ? `<div class="f-hint">${appH("m847", { count: appN(PLNEW.ids.length) })}</div>` : ""}`,
    `<button class="btn ghost" data-act="modal-close">${appH("m063")}</button><div class="spacer"></div><button class="btn primary" data-act="pl-new-submit">${icon("check", 15)} ${appH("m562")}</button>`, { narrow: true });
  const i = $("#pln-name"); if (i) i.focus();
}
async function plNewSubmit() {
  const name = val("pln-name").trim();
  const err = $("#pln-err");
  if (!name) { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${appH("m547")}</span></div>`; return; }
  try {
    const p = await api("POST", "/playlists", { name });
    if (PLNEW.ids.length) await api("POST", `/playlists/${enc(p.id)}/items`, { track_ids: PLNEW.ids });
    toast(appT("m850", { name: name, count: PLNEW.ids.length ? appT("m849", { count: appN(PLNEW.ids.length) }) : "" }), "ok");
    closeModal();
    await syncPlaylists();
    const re = PLNEW.reopen; PLNEW.reopen = "";
    if (re) openPlayer(re);
  } catch (e) { toast(e.message, "err"); }
}
async function syncPlaylists() {
  try { const p = await api("GET", "/playlists"); S.playlists = Array.isArray(p) ? p : []; } catch (e) { return; }
  if (S.view === "overview") { paintOverviewKpis(); paintOvPlaylists(); }
  else if (S.view === "library") { paintLibTabs(); if (S.libTab === "playlists") paintLibTab(); }
}

/* ---- playlist detail (view / reorder / remove / rename / delete) ---- */
const PL = { id: "", detail: null };
async function openPlaylist(id) {
  PL.id = id; PL.detail = null;
  openModal(`${icon("layers", 18)} ${appH("m851")}`, `<div id="pl-body">${empty("refresh", appT("m149"), "", true)}</div>`, "");
  try { PL.detail = await api("GET", `/playlists/${enc(id)}`); }
  catch (e) { toast(e.message, "err"); closeModal(); return; }
  if (!$(".modal") || PL.id !== id) return;
  paintPlaylist();
}
function paintPlaylist() {
  const d = PL.detail; if (!d || !$(".modal")) return;
  const items = d.items || [];
  const rows = items.map((t, i) => `
    <div class="q-row">
      <span class="q-idx mono">${i + 1}</span>
      <span class="q-info">
        <span class="q-title truncate">${esc(t.title || t.track_id)}${t.missing ? " " + badge(appT("m348"), "warn") : ""}</span>
        <span class="q-sub txt-faint">${esc([t.artist, t.album].filter(Boolean).join(" · ") || "—")}${t.duration_ms ? " · " + fmtDur(t.duration_ms) : ""}</span>
      </span>
      <span class="q-acts">
        <button class="btn xs ghost" data-act="pl-item-up" data-i="${i}" ${i === 0 ? "disabled" : ""} title="${appH("m852")}">${icon("arrowUp", 12)}</button>
        <button class="btn xs ghost" data-act="pl-item-down" data-i="${i}" ${i === items.length - 1 ? "disabled" : ""} title="${appH("m853")}">${icon("arrowDown", 12)}</button>
        <button class="btn xs ghost" data-act="pl-item-del" data-i="${i}" title="${appH("m516")}">${icon("x", 12)}</button>
      </span>
    </div>`).join("");
  setModalBody(`
    <div class="tp-track">
      <div class="tp-title truncate">${esc(d.name)}</div>
      <div class="tp-meta">${appH("m855", { count: appN(items.length), time: ago(d.updated_at) })}</div>
    </div>
    <div class="divider"></div>
    ${items.length ? `<div class="q-list">${rows}</div>` : empty("music", appT("m854"), "", true)}`);
  const ok = items.some(t => !t.missing);
  setModalFoot(`
    <button class="btn ghost" data-act="pl-rename" data-id="${esc(d.id)}">${icon("edit", 14)} ${appH("m856")}</button>
    <button class="btn danger" data-act="pl-delete" data-id="${esc(d.id)}">${icon("trash", 14)} ${appH("m531")}</button>
    <div class="spacer"></div>
    <button class="btn primary" data-act="pl-push" data-id="${esc(d.id)}" ${ok ? "" : "disabled"}>${icon("send", 15)} ${appH("m763")}</button>`);
}
async function plSaveItems(ids) {
  try {
    PL.detail = await api("PUT", `/playlists/${enc(PL.id)}/items`, { track_ids: ids });
    paintPlaylist();
    await syncPlaylists();
  } catch (e) { toast(e.message, "err"); }
}
function plItemMove(i, delta) {
  const items = (PL.detail && PL.detail.items) || [];
  const j = i + delta;
  if (j < 0 || j >= items.length) return;
  const ids = items.map(t => t.track_id);
  const tmp = ids[i]; ids[i] = ids[j]; ids[j] = tmp;
  plSaveItems(ids);
}
function plItemDel(i) {
  const ids = ((PL.detail && PL.detail.items) || []).map(t => t.track_id);
  if (i < 0 || i >= ids.length) return;
  ids.splice(i, 1);
  plSaveItems(ids);
}
function openPlRename(id) {
  const p = (PL.detail && PL.detail.id === id) ? PL.detail : S.playlists.find(x => x.id === id);
  openModal(`${icon("edit", 18)} ${appH("m857")}`, `
    <div id="plr-err"></div>
    <div class="field"><label>${appH("m427")}</label><input id="plr-name" class="inp" maxlength="80" value="${esc(p ? p.name : "")}"></div>`,
    `<button class="btn ghost" data-act="pl-open" data-id="${esc(id)}">${appH("m064")}</button><div class="spacer"></div><button class="btn primary" data-act="pl-rename-submit" data-id="${esc(id)}">${icon("check", 15)} ${appH("m858")}</button>`, { narrow: true });
  const i = $("#plr-name"); if (i) i.focus();
}
async function plRenameSubmit(id) {
  const name = val("plr-name").trim();
  const err = $("#plr-err");
  if (!name) { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${appH("m547")}</span></div>`; return; }
  try {
    await api("PATCH", `/playlists/${enc(id)}`, { name });
    toast(appT("m859"), "ok");
    await syncPlaylists();
    openPlaylist(id);
  } catch (e) { toast(e.message, "err"); }
}
async function plDelete(id) {
  const p = (PL.detail && PL.detail.id === id) ? PL.detail : S.playlists.find(x => x.id === id);
  const name = p ? p.name : shortId(id);
  const ok = await confirmDlg({ title: appT("m860"), message: appT("m861", { name: name }), okText: appT("m531") });
  if (!ok) return;
  try {
    await api("DELETE", `/playlists/${enc(id)}`);
    toast(appT("m862", { name: name }), "ok");
    closeModal();
    await syncPlaylists();
  } catch (e) { toast(e.message, "err"); }
}

async function playStop(nodeID) {
  try { await api("POST", `/nodes/${enc(nodeID)}/stop`); toast(appT("m863"), "ok"); }
  catch (e) { toast(e.message, "err"); }
  await syncPlayUI();
}
async function playPause(nodeID) {
  const p = S.play[nodeID] || S.nodePlay[nodeID] || {};
  try {
    if (p.playing) { await api("POST", `/nodes/${enc(nodeID)}/pause`); toast(appT("m181"), "ok"); }
    else if (p.state === "paused") { await api("POST", `/nodes/${enc(nodeID)}/resume`); toast(appT("m159"), "ok"); }
    else return;
  } catch (e) { toast(e.message, "err"); }
  await syncPlayUI();
}
async function syncPlayUI() {
  await refreshPlayerBar();
  if (drawerCtx && drawerCtx.kind === "node") {
    const id = drawerCtx.id;
    if (id !== PB.id) {
      try { S.nodePlay[id] = await api("GET", `/nodes/${enc(id)}/play`); } catch (e) { /* aux */ }
    }
    if (drawerCtx && drawerCtx.kind === "node" && drawerCtx.id === id) paintNodePlay(id);
  }
}
async function queueAfter(id) {
  try { S.nodeQueue[id] = await api("GET", `/nodes/${enc(id)}/queue`); } catch (e) { /* keep last */ }
  paintNodePlay(id);
  await syncPlayUI();
}

/* ================= PLAYER CONSOLE ================= */
const PLY = { id: "", dragging: false, volDrag: false, addOpen: false, addSel: new Set(), addSearch: "" };
async function openPlayer(nodeID) {
  PLY.dragging = false; PLY.volDrag = false; PLY.addOpen = false; PLY.addSel = new Set(); PLY.addSearch = "";
  openModal(`${icon("sliders", 18)} ${appH("m864")}`, `<div id="ply-body">${empty("refresh", appT("m865"), "", true)}</div>`, "", {
    onClose: () => { if (PLY.id === nodeID) PLY.id = ""; }
  });
  PLY.id = nodeID;
  const root = $("#ply-body");
  await selectPlayerBar(nodeID);
  if (!S.tracks.length) { const tr = await api("GET", "/tracks").catch(() => []); S.tracks = Array.isArray(tr) ? tr : []; }
  if (!S.playlists.length) { const pl = await api("GET", "/playlists").catch(() => []); S.playlists = Array.isArray(pl) ? pl : []; }
  if ($("#ply-body") !== root || PLY.id !== nodeID) return;
  root.innerHTML = playerShell();
  paintPlayerLive();
}
function playerShell() {
  const id = PLY.id;
  const p = S.play[id] || {};
  const online = PB.nodes.filter(x => x.status === "online");
  const nodeList = online.some(x => x.node_id === id) ? online : online.concat(PB.nodes.filter(x => x.node_id === id));
  const nodeOpts = nodeList.map(x => `<option value="${esc(x.node_id)}" ${x.node_id === id ? "selected" : ""}>${esc(x.alias || shortId(x.node_id))}${x.ip ? " · " + esc(x.ip) : ""}</option>`).join("");
  const { devs, cur, names } = nodePlayDevs(p);
  const plOpts = `<option value="">${appH("m866")}</option>` + S.playlists.map(pl => `<option value="${esc(pl.id)}">${appH("m867", { name: pl.name, count: pl.count })}</option>`).join("");
  return `<div class="ply">
    <div class="ply-top">
      <div id="ply-cover"></div>
      <div class="ply-meta">
        <div class="ply-title truncate" id="ply-title">—</div>
        <div class="ply-artist truncate" id="ply-artist"></div>
        <div class="ply-album" id="ply-album"></div>
        <div class="ply-fmt mono" id="ply-fmt"></div>
        <div id="ply-badges"></div>
        <div class="ply-sel-row">
          <div class="field"><label>${appH("m164")}</label><select id="ply-node" class="inp" data-change="ply-node" data-sig="${esc(online.map(x => x.node_id).join("|"))}">${nodeOpts}</select></div>
          <div class="field"><label>${appH("m166")}</label><select id="ply-dev" class="inp mono" data-change="ply-device" data-sig="${esc(devs.join("|"))}" ${p.connected ? "" : "disabled"}>${nodePlayOpts(devs, cur, names)}</select></div>
        </div>
      </div>
    </div>
    <div class="ply-seek">
      <span class="ply-time mono" id="ply-el">0:00</span>
      <input type="range" id="ply-seek" min="0" max="0" step="500" value="0" data-change="ply-seek" disabled>
      <span class="ply-time mono" id="ply-total">0:00</span>
    </div>
    <div class="ply-transport">
      <button class="btn" id="ply-prev" data-act="queue-prev" data-id="${esc(id)}" title="${appH("m158")}">${icon("chevL", 18)}</button>
      <button class="btn primary ply-big" id="ply-pp" data-act="play-pause" data-id="${esc(id)}" title="${appH("m868")}">${icon("play", 20)}</button>
      <button class="btn" id="ply-next" data-act="queue-next" data-id="${esc(id)}" title="${appH("m160")}">${icon("chevR", 18)}</button>
      <button class="btn danger" id="ply-stop" data-act="play-stop" data-id="${esc(id)}" title="${appH("m390")}">${icon("stop", 16)}</button>
      <div class="ply-vol">${icon("speaker", 15)}<input type="range" id="ply-vol" min="0" max="100" step="1" value="100" data-change="ply-vol" disabled><span class="ply-time mono" id="ply-vol-v">100</span></div>
    </div>
    <div class="divider"></div>
    <div class="sec-head">
      <div class="sec-title">${icon("layers", 16)} ${appH("m869")} <span class="txt-faint" id="ply-qcnt"></span></div>
      <div class="spacer"></div>
      <button class="btn xs ghost" data-act="ply-add-toggle">${icon("plus", 12)} ${appH("m870")}</button>
      <select id="ply-load" class="inp ply-load" data-change="ply-load">${plOpts}</select>
      <button class="btn xs ghost" data-act="ply-save">${icon("layers", 12)} ${appH("m871")}</button>
      <button class="btn xs ghost" data-act="queue-clear" data-id="${esc(id)}">${icon("trash", 12)} ${appH("m872")}</button>
    </div>
    <div id="ply-add" class="ply-add hidden">
      <input id="ply-add-search" class="inp" placeholder="${appH("m873")}" value="" autocomplete="off">
      <div class="ply-add-list" id="ply-add-list"></div>
      <div class="acts mt8"><span class="f-hint" id="ply-add-cnt"></span><div class="spacer"></div><button class="btn primary sm" id="ply-add-go" data-act="ply-add-go" disabled>${icon("plus", 14)} ${appH("m811")}</button></div>
    </div>
    <div id="ply-qlist"></div>
    <div class="f-hint mt8">${appH("m874")}</div>
  </div>`;
}

function paintPlayerLive() {
  const id = PLY.id; if (!id || !$("#ply-body")) return;
  const p = S.play[id] || {};
  const t = p.track_id ? S.tracks.find(x => x.id === p.track_id) || (PB.trackID === p.track_id ? PB.track : null) : null;
  const q = S.nodeQueue[id] || {};
  const tracks = q.tracks || [];
  const pos = q.position || 0;
  const connected = id === PB.id && playerBarConnected();
  const playing = connected && !!p.playing, paused = connected && p.state === "paused", active = playing || paused;
  const hasTrack = !!(p.track_id || p.title);
  const title = p.title || (t && t.title) || "";
  const artist = p.artist || (t && t.artist) || "";
  const album = p.album || (t && t.album) || "";
  const rate = p.rate || (t && t.rate) || 0;
  const total = p.duration_ms || (t && t.duration_ms) || 0;
  let elapsed = rate && p.pts ? p.pts / rate * 1000 : 0;
  if (total && elapsed > total) elapsed = total;
  const set = (sel, html) => { const el = $(sel); if (el) el.innerHTML = html; };
  set("#ply-cover", coverArt(album, artist, "big", 56, t && t.cover_hash));
  set("#ply-title", hasTrack ? esc(title || "—") : appT("m155"));
  set("#ply-artist", hasTrack ? esc(artist || appT("m117")) : appT("m875"));
  set("#ply-album", hasTrack ? `${icon("disc", 13)}<span class="truncate">${esc(album || appT("m128"))}</span>` : "");
  set("#ply-fmt", hasTrack ? `${esc(p.codec || (t && t.codec) || "—")} · ${rate ? appFixed(rate / 1000, 1) + " kHz" : "—"} · ${p.channels || (t && t.channels) || "—"} ch${p.buffer_ms != null ? appT("m876") + p.buffer_ms + " ms" : ""}` : "");
  set("#ply-badges", (connected ? badge(appT("m817"), "ok") : badge(appT("m877"), "muted")) +
    (playing ? " " + badge(appT("m819"), "ok") : paused ? " " + badge(appT("m181"), "warn") : "") +
    (p.underruns ? " " + badge("underrun ×" + p.underruns, "warn") : ""));
  const seek = $("#ply-seek");
  if (seek) {
    seek.disabled = !(connected && active && rate > 0 && total > 0);
    seek.max = String(Math.round(total));
    if (!PLY.dragging && document.activeElement !== seek) seek.value = String(Math.round(elapsed));
    if (!PLY.dragging) { const el = $("#ply-el"); if (el) el.textContent = fmtDur(elapsed); }
    const tt = $("#ply-total"); if (tt) tt.textContent = fmtDur(total);
  }
  const vol = $("#ply-vol");
  if (vol) {
    vol.disabled = !connected || !active;
    const v = p.volume != null ? p.volume : 100;
    if (!PLY.volDrag && document.activeElement !== vol) {
      vol.value = String(v);
      const lv = $("#ply-vol-v"); if (lv) lv.textContent = appN(v);
    }
  }
  const pp = $("#ply-pp");
  if (pp) { pp.disabled = !active; pp.innerHTML = icon(playing ? "pause" : "play", 20); pp.title = playing ? appT("m183") : appT("m159"); }
  const prev = $("#ply-prev"), next = $("#ply-next"), stop = $("#ply-stop");
  if (prev) prev.disabled = !connected || !tracks.length;
  if (next) next.disabled = !connected || !tracks.length || pos >= tracks.length - 1;
  if (stop) stop.disabled = !active;
  const nsel = $("#ply-node");
  if (nsel) {
    const online = PB.nodes.filter(x => x.status === "online");
    const sig = online.map(x => x.node_id).join("|");
    if (nsel.dataset.sig !== sig) {
      const list = online.some(x => x.node_id === id) ? online : online.concat(PB.nodes.filter(x => x.node_id === id));
      nsel.innerHTML = list.map(x => `<option value="${esc(x.node_id)}" ${x.node_id === id ? "selected" : ""}>${esc(x.alias || shortId(x.node_id))}${x.ip ? " · " + esc(x.ip) : ""}</option>`).join("");
      nsel.dataset.sig = sig;
    }
  }
  const dsel = $("#ply-dev");
  if (dsel) {
    const dv = nodePlayDevs(p);
    const sig = dv.devs.join("|");
    if (dsel.dataset.sig !== sig) { dsel.innerHTML = nodePlayOpts(dv.devs, dv.cur, dv.names); dsel.dataset.sig = sig; }
    dsel.disabled = !connected;
    if (document.activeElement !== dsel && dsel.value !== dv.cur) dsel.value = dv.cur;
  }
  const cnt = $("#ply-qcnt"); if (cnt) cnt.textContent = `(${appN(tracks.length)})`;
  set("#ply-qlist", playerQueueList(id, q));
}
function playerQueueList(id, q) {
  const tracks = (q && q.tracks) || [];
  if (!tracks.length) return empty("music", appT("m878"), appH("m879"), true);
  const pos = q.position || 0;
  return `<div class="q-list">` + tracks.map((t, i) => `
    <div class="q-row ${i === pos ? "cur" : ""}">
      <span class="q-idx mono">${i === pos ? icon("speaker", 12) : i + 1}</span>
      <span class="q-info">
        <span class="q-title truncate">${esc(t.title || t.track_id)}${t.missing ? " " + badge(appT("m348"), "warn") : ""}</span>
        <span class="q-sub txt-faint">${esc([t.artist, t.album].filter(Boolean).join(" · ") || "—")}${t.duration_ms ? " · " + fmtDur(t.duration_ms) : ""}</span>
      </span>
      <span class="q-acts">
        <button class="btn xs ghost" data-act="ply-q-up" data-idx="${i}" ${i === 0 ? "disabled" : ""} title="${appH("m852")}">${icon("arrowUp", 12)}</button>
        <button class="btn xs ghost" data-act="ply-q-down" data-idx="${i}" ${i === tracks.length - 1 ? "disabled" : ""} title="${appH("m853")}">${icon("arrowDown", 12)}</button>
        ${i !== pos ? `<button class="btn xs ghost" data-act="queue-jump" data-id="${esc(id)}" data-idx="${i}" title="${appH("m880")}">${icon("play", 12)}</button>` : ""}
        <button class="btn xs ghost" data-act="queue-remove" data-id="${esc(id)}" data-idx="${i}" title="${appH("m881")}">${icon("x", 12)}</button>
      </span>
    </div>`).join("") + `</div>`;
}

async function plyQueueMove(idx, dir) {
  const id = PLY.id; const q = S.nodeQueue[id]; if (!id || !q) return;
  const ids = (q.tracks || []).map(t => t.track_id);
  const j = idx + dir;
  if (idx < 0 || j < 0 || idx >= ids.length || j >= ids.length) return;
  [ids[idx], ids[j]] = [ids[j], ids[idx]];
  let pos = q.position || 0;
  if (pos === idx) pos = j; else if (pos === j) pos = idx;
  try { S.nodeQueue[id] = await api("PUT", `/nodes/${enc(id)}/queue`, { track_ids: ids, position: pos }); }
  catch (e) { toast(e.message, "err"); }
  paintPlayerLive();
}
function plyAddToggle() {
  PLY.addOpen = !PLY.addOpen;
  const box = $("#ply-add"); if (box) box.classList.toggle("hidden", !PLY.addOpen);
  if (PLY.addOpen) { paintPlayerAddList(); const s = $("#ply-add-search"); if (s) s.focus(); }
}
function plyAddFiltered() {
  const q = PLY.addSearch.trim().toLowerCase();
  return S.tracks.filter(t => t.decodable && (!q || [t.title, t.artist, t.album].some(x => String(x || "").toLowerCase().includes(q))));
}
function plyAddCnt() {
  const cnt = $("#ply-add-cnt");
  if (cnt) cnt.textContent = appT("m883", { count: appN(PLY.addSel.size), count2: plyAddFiltered().length > 200 ? appT("m882") : "" });
  const go = $("#ply-add-go"); if (go) go.disabled = !PLY.addSel.size;
}
function paintPlayerAddList() {
  const el = $("#ply-add-list"); if (!el) return;
  const list = plyAddFiltered().slice(0, 200);
  el.innerHTML = list.length ? list.map(t => `
    <label class="ply-add-row ${PLY.addSel.has(t.id) ? "sel" : ""}">
      <input type="checkbox" data-change="ply-add-sel" data-id="${esc(t.id)}" ${PLY.addSel.has(t.id) ? "checked" : ""}>
      <span class="q-info">
        <span class="q-title truncate">${esc(t.title || t.id)}</span>
        <span class="q-sub txt-faint">${esc([t.artist, t.album].filter(Boolean).join(" · ") || "—")} · ${esc(t.codec || "?")}${t.duration_ms ? " · " + fmtDur(t.duration_ms) : ""}</span>
      </span>
    </label>`).join("") : empty("search", appT("m884"), "", true);
  plyAddCnt();
}
async function plyAddGo() {
  const id = PLY.id; if (!id || !PLY.addSel.size) return;
  const ids = Array.from(PLY.addSel);
  const go = $("#ply-add-go"); if (go) { go.disabled = true; go.innerHTML = `${icon("refresh", 14)} ${appH("m829")}`; }
  try {
    let started = false;
    for (const tid of ids) {
      const r = await api("POST", `/nodes/${enc(id)}/queue/enqueue`, { track_id: tid });
      if (r && r.session_id) started = true;
    }
    toast(appT("m885", { count: appN(ids.length), value: started ? appT("m832") : "" }), "ok");
    PLY.addSel.clear();
    paintPlayerAddList();
    await refreshPlayerBar();
  } catch (e) { toast(e.message, "err"); }
  if (go) { go.innerHTML = `${icon("plus", 14)} ${appH("m811")}`; go.disabled = !PLY.addSel.size; }
}
async function plyLoadPlaylist(plID) {
  const id = PLY.id; if (!id || !plID) return;
  let d;
  try { d = await api("GET", `/playlists/${enc(plID)}`); } catch (e) { toast(e.message, "err"); return; }
  const ids = (d.items || []).filter(t => !t.missing).map(t => t.track_id);
  const sel = $("#ply-load"); if (sel) sel.value = "";
  if (!ids.length) { toast(appT("m886"), "warn"); return; }
  try { await api("PUT", `/nodes/${enc(id)}/queue`, { track_ids: ids, position: 0 }); }
  catch (e) { toast(e.message, "err"); await refreshPlayerBar(); return; }
  try {
    await api("POST", `/nodes/${enc(id)}/queue/jump`, { index: 0 });
    toast(appT("m887", { name: d.name, count: appN(ids.length) }), "ok");
  } catch (e) { toast(appT("m888", { name: d.name }), "warn"); }
  await refreshPlayerBar();
}
function plySaveQueue() {
  const q = S.nodeQueue[PLY.id];
  const ids = ((q && q.tracks) || []).map(t => t.track_id);
  if (!ids.length) { toast(appT("m889"), "warn"); return; }
  openPlNew(ids, PLY.id);
}

/* ================= ACTION MAP ================= */
const ACT = {
  "nav": ds => setView(ds.view),
  "bell": () => openAlerts(),
  "refresh-view": () => S.view === "overview" ? Promise.all([refreshOverview(), refreshListening(true)]) : refreshFor(S.view),
  "goto-add-node": () => { setView("nodes"); openWizard(); },
  "goto-keys": () => { closeModal(); closeDrawer(); setView("keys"); },
  "modal-close": () => closeModal(),
  "modal-backdrop": (ds, el, e) => { if (e.target === el) closeModal(); },
  "drawer-close": () => closeDrawer(),
  "drawer-backdrop": (ds, el, e) => { if (e.target === el) closeDrawer(); },
  "confirm-ok": () => closeConfirm(true),
  "confirm-cancel": () => closeConfirm(false),
  "confirm-backdrop": (ds, el, e) => { if (e.target === el) closeConfirm(false); },
  "toast-close": ds => removeToast(ds.tid),
  "toast-undo": ds => { const fn = toastUndos.get(ds.tid); removeToast(ds.tid); return fn ? fn() : undefined; },
  "node-open": ds => openNodeDrawer(ds.id),
  "node-config": ds => openConfig(ds.id),
  "node-os": ds => openOsDrawer(ds.id),
  "node-net": ds => openNetDrawer(ds.id),
  "node-console": ds => openConsole(ds.id),
  "node-auth": ds => nodeAuth(ds.id, false),
  "node-revoke": ds => nodeAuth(ds.id, true),
  "nd-filter": ds => { S.nodeFilter = ds.f; paintNodes(); },
  "nd-sort": ds => { const k = ds.key; if (S.nodeSort.key === k) S.nodeSort.dir *= -1; else { S.nodeSort.key = k; S.nodeSort.dir = 1; } paintNodeTable(); },
  "nd-clear-sel": () => { S.sel.clear(); paintNodes(); },
  "bulk-auth": () => bulkAuth(false),
  "bulk-revoke": () => bulkAuth(true),
  "bulk-delete": () => bulkDelete(),
  "wz-open": () => openWizard(),
  "wz-next": () => wzNext(),
  "wz-back": () => { WZ.step = 1; renderWizard(); },
  "wz-reissue": () => wzReissue(),
  "wz-copy-token": async () => { toast(await copyText(WZ.token) ? appT("m890") : appT("m891"), "ok"); },
  "wz-copy-cmd": async () => { const ok = await copyText(installCmd()); toast(ok ? (WZ.arch === "android" ? appT("m892") : appT("m893")) : appT("m891"), "ok"); },
  "wz-auth": async ds => { try { await api("POST", `/nodes/${enc(ds.id)}/authorize`); toast(appT("m894"), "ok"); refreshCurrent(); } catch (e) { toast(e.message, "err"); } },
  "wz-config": ds => { closeModal(); openConfig(ds.id); },
  "wz-console": ds => { closeModal(); openConsole(ds.id); },
  "wz-detail": ds => { closeModal(); openNodeDrawer(ds.id); },
  "os-check": () => runOsCommand("os-check"),
  "os-upgrade": () => runOsCommand("os-upgrade"),
  "net-run": () => runNetdiag(),
  "cfg-off": ds => cfgOffStep(Number(ds.d) || 0),
  "cfg-save": () => cfgSave(),
  "cfg-delete": () => cfgDelete(),
  "cfg-auth": () => cfgAuth(false),
  "cfg-revoke": () => cfgAuth(true)
};
Object.assign(ACT, {
  "app-open": ds => openAppModal(ds.id),
  "app-create": () => openCreateApp(),
  "app-create-submit": () => submitCreateApp(),
  "app-upload": ds => openUpload(ds.id),
  "app-import-local": ds => openImportLocal(ds.id),
  "app-import-local-submit": ds => submitImportLocal(ds.id),
  "up-submit": () => submitVersion(),
  "ver-ban": ds => verBan(ds),
  "bom-new": () => openBomEditor(),
  "bom-edit": ds => openBomEditor(ds.id, ds.ver),
  "bom-validate": ds => bomValidate(ds),
  "bom-publish": ds => bomPublish(ds),
  "bom-export": ds => bomExport(ds),
  "bom-rollback": ds => bomRollback(ds),
  "bom-history": ds => openBomHistory(ds.id),
  "bom-hist": ds => bomHistAct(ds),
  "bom-import": () => openBomImport(),
  "bom-import-submit": () => submitBomImport(),
  "bom-report-close": () => { const el = $("#bom-report"); if (el) el.innerHTML = ""; },
  "bom-row-add": () => bomRowAdd(),
  "bom-row-del": ds => bomRowDel(Number(ds.i)),
  "bom-save-ed": () => bomSave(false).catch(e => { const el = $("#bom-ed-err"); if (!el || !el.innerHTML) toast(e.message, "err"); }),
  "bom-validate-ed": () => bomValidateEd(),
  "bom-publish-ed": () => bomPublishEd(),
  "apt-sources": () => openAptSources(),
  "apt-src-add": () => submitAptSource(),
  "apt-src-del": ds => aptSrcDel(ds.id),
  "job-open": ds => openJobDrawer(ds.id),
  "job-advance": ds => jobAdvance(ds.id),
  "job-rb-complete": ds => jobRbComplete(ds.id),
  "ro-create": () => openRolloutCreate(),
  "ro-submit": () => roSubmit(),
  "ro-ring-add": () => roRingAdd(),
  "ro-ring-del": ds => roRingDel(Number(ds.i)),
  "ro-advance": ds => roAdvance(ds.id),
  "ro-delete": ds => roDelete(ds.id),
  "upd-check": () => updCheck(),
  "upd-apply": () => updApply(),
  "upd-converge": () => updConverge(),
  "upd-retry": ds => updRetry(ds.id),
  "lic-open": () => openLicenseActivate(),
  "lic-submit": () => licSubmit(),
  "lic-receipt": () => { download("/license/receipt"); toast(appT("m895"), "info"); },
  "key-import": () => openKeyImport(),
  "key-import-submit": () => keyImportSubmit(),
  "key-trust": ds => keyTrust(ds.id),
  "key-export": () => keyExport(),
  "audit-export": ds => auditExport(ds.fmt),
  "audit-filter": ds => { S.auditFilter = ds.f; paintAudit(); },
  "alert-scan": () => alertScan(),
  "alert-ack-all": () => alertAckAll(),
  "alert-filter": ds => { S.alertFilter = ds.f; paintAlerts(); },
  "alert-ack": ds => alertAct(ds.id, "ack"),
  "alert-resolve": ds => alertAct(ds.id, "resolve"),
  "lib-add": () => openMountAdd(),
  "lib-add-submit": () => submitMountAdd(),
  "lib-refresh": () => libRefresh(),
  "lib-mount": ds => libMountAct(ds.id, "mount"),
  "lib-unmount": ds => libMountAct(ds.id, "unmount"),
  "lib-scan": ds => libMountAct(ds.id, "scan"),
  "lib-scan-full": ds => libMountAct(ds.id, "scan-full"),
  "lib-archive": ds => libArchive(ds.id),
  "lib-delete": ds => libDelete(ds.id),
  "lib-tab": ds => libraryBrowserTab(ds.f),
  "lib-goto-nodes": () => { closeModal(); setView("nodes"); },
  "ov-goto": ds => { S.libTab = ds.tab || "albums"; setView("library"); },
  "ov-sys-toggle": () => { S.sysOpen = !S.sysOpen; paintSysStrip(); },
  "ov-days": ds => ovSetDays(Number(ds.days)),
  "ov-retry": () => refreshListening(true),
  "ov-library-retry": () => refreshOverview(),
  "ov-more": ds => { if (ds.kind === "recent") { OV.recentMore = !OV.recentMore; paintOvRecent(); } else { OV.addedMore = !OV.addedMore; paintOvAlbums(); } },
  "album-open": ds => openAlbum(ds.ak),
  "album-push": ds => albumPush(ds.ak),
  "album-pl": ds => { const g = albumByKey(ds.ak); if (g) openPlPicker(g.tracks.map(t => t.id)); },
  "track-pl": ds => openPlPicker([ds.id]),
  "pl-new": () => openPlNew([]),
  "pl-new-submit": () => plNewSubmit(),
  "pl-pick": ds => plPick(ds.id),
  "pl-pick-new": () => openPlNew(PP.ids),
  "pl-open": ds => openPlaylist(ds.id),
  "pl-push": ds => openPlPush(ds.id),
  "pl-item-up": ds => plItemMove(Number(ds.i), -1),
  "pl-item-down": ds => plItemMove(Number(ds.i), 1),
  "pl-item-del": ds => plItemDel(Number(ds.i)),
  "pl-rename": ds => openPlRename(ds.id),
  "pl-rename-submit": ds => plRenameSubmit(ds.id),
  "pl-delete": ds => plDelete(ds.id),
  "track-play": ds => openTrackPlay(ds.id),
  "tp-pick": ds => { TP.sel = ds.id; paintTrackPlay(); },
  "tp-goto-nodes": () => { closeModal(); setView("nodes"); },
  "track-play-go": () => trackPlayGo(),
  "play-stop": ds => playStop(ds.id),
  "queue-prev": ds => queueMove(ds.id, "prev"),
  "queue-next": ds => queueMove(ds.id, "next"),
  "queue-jump": ds => queueJump(ds.id, parseInt(ds.idx, 10)),
  "queue-remove": ds => queueRemove(ds.id, parseInt(ds.idx, 10)),
  "queue-clear": ds => queueClear(ds.id),
  "track-enqueue-go": () => trackEnqueueGo(),
  "track-next-go": () => trackEnqueueGo(true),
  "player-open": ds => openPlayer(ds.id),
  "play-pause": ds => playPause(ds.id),
  "ply-q-up": ds => plyQueueMove(Number(ds.idx), -1),
  "ply-q-down": ds => plyQueueMove(Number(ds.idx), 1),
  "ply-add-toggle": () => plyAddToggle(),
  "ply-add-go": () => plyAddGo(),
  "ply-save": () => plySaveQueue()
});

/* ================= CHANGE MAP ================= */
const CHG = {
  "ov-stat-node": el => { OV.node = el.value; OV.recentMore = false; return refreshListening(true); },
  "pb-node": el => selectPlayerBar(el.value),
  "pb-seek": el => playerBarRangeChange(el, false),
  "pb-volume": el => playerBarRangeChange(el, true),
  "pb-device": el => playerBarDeviceChange(el),
  "node-sel": (el, ds) => { if (el.checked) S.sel.add(ds.id); else S.sel.delete(ds.id); paintBulkBar(); paintNodeTable(); },
  "node-sel-all": el => { const list = filteredNodes(); list.forEach(n => { if (el.checked) S.sel.add(n.node_id); else S.sel.delete(n.node_id); }); paintNodes(); },
  "os-pkg": (el, ds) => { if (el.checked) OSC.sel.add(ds.name); else OSC.sel.delete(ds.name); repaintOs(); },
  "os-pkg-all": el => { const pkgs = (OSC.state && OSC.state.packages) || []; pkgs.forEach(p => { if (el.checked) OSC.sel.add(p.name); else OSC.sel.delete(p.name); }); repaintOs(); },
  "cfg-bom": () => paintCfgDrift(),
  "cfg-dac": () => paintCfgDacHint(),
  "node-device": async (el, ds) => {
    await api("POST", `/nodes/${enc(ds.id)}/device`, { device: el.value });
    toast(appT("m187"), "ok");
    try { S.nodePlay[ds.id] = await api("GET", `/nodes/${enc(ds.id)}/play`); } catch (e) { /* aux */ }
    paintNodePlay(ds.id);
  },
  "ply-seek": async el => {
    PLY.dragging = false;
    const id = PLY.id; if (!id) return;
    try { await api("POST", `/nodes/${enc(id)}/seek`, { position_ms: Number(el.value) }); }
    catch (e) { toast(e.message, "err"); }
    refreshPlayerBar();
  },
  "ply-vol": async el => {
    PLY.volDrag = false;
    const id = PLY.id; if (!id) return;
    try { await api("POST", `/nodes/${enc(id)}/volume`, { volume: Number(el.value) }); }
    catch (e) { toast(e.message, "err"); }
    refreshPlayerBar();
  },
  "ply-node": el => { if (el.value && el.value !== PLY.id) return openPlayer(el.value); },
  "ply-device": async el => {
    const id = PLY.id; if (!id) return;
    await api("POST", `/nodes/${enc(id)}/device`, { device: el.value });
    toast(appT("m187"), "ok");
    refreshPlayerBar();
  },
  "ply-load": el => plyLoadPlaylist(el.value),
  "ply-add-sel": (el, ds) => {
    if (el.checked) PLY.addSel.add(ds.id); else PLY.addSel.delete(ds.id);
    const row = el.closest(".ply-add-row"); if (row) row.classList.toggle("sel", el.checked);
    plyAddCnt();
  },
  "ver-lc": (el, ds) => verLifecycle(ds, el.value),
  "ro-pct": () => paintRoSum(),
  "upd-auto": el => updAuto(el),
  "upd-policy": () => updPolicySave(),
  "lic-pfile": el => fileToText(el, txt => { const t = $("#lic-payload"); if (t) t.value = txt; }),
  "lic-sfile": el => fileToText(el, txt => { const t = $("#lic-sig"); if (t) t.value = txt; }),
  "gpg-file": el => fileToText(el, txt => { const t = $("#gpg-armored"); if (t) t.value = txt; }),
  "bi-file": el => fileToText(el, txt => { const t = $("#bi-json"); if (t) t.value = txt; }),
  "wz-arch": el => { const d = $("#wz-distro"); if (!d) return; if (el.value === "android") d.value = "android"; else if (d.value === "android") d.value = "debian"; }
};

/* ================= EVENTS & BOOT ================= */
// Locale changes must not replay actions or replace live playback controls.
function appLanguageChanged() {
  document.title = appT("consoleTitle");
  const text = (selector, key) => { const el = $(selector); if (el) el.textContent = appT(key); };
  const attr = (selector, name, key) => { const el = $(selector); if (el) el.setAttribute(name, appT(key)); };
  text(".brand-sub", "brandSubtitle");
  text("#pb-node-label", "m164"); text("#pb-device-label", "m166"); text("#pb-console-label", "m170");
  attr("#global-search", "placeholder", "globalSearchPlaceholder");
  attr("#global-search", "aria-label", "globalSearch");
  attr("#conn", "title", "connectionStatus");
  attr("#sidebar", "aria-label", "mainNavigation"); attr("#nav", "aria-label", "mainNavigation");
  attr("#nav-backdrop", "aria-label", "closeNavigation"); attr("#mobile-nav-close", "aria-label", "closeNavigation");
  attr("#mobile-nav-toggle", "aria-label", "openNavigation");
  attr("#xp-account", "aria-label", "adminPreferences");
  attr(".topbar .bell", "title", "m746"); attr(".topbar .bell", "aria-label", "m746");
  attr("#player-bar", "aria-label", "m154"); attr(".player-bar-tools", "aria-label", "m161");
  for (const [selector, key] of [["#pb-prev", "m158"], ["#pb-next", "m160"]]) {
    attr(selector, "title", key); attr(selector, "aria-label", key);
  }
  attr("#pb-seek", "aria-label", "m162"); attr("#pb-volume", "aria-label", "m163");
  attr(".player-bar-volume", "title", "m163"); attr("#pb-node", "aria-label", "m165");
  attr("#pb-device", "aria-label", "m167"); attr("#pb-console", "title", "m169");
  const health = $("#health-line");
  if (health) health.textContent = S.health ? appT("m075", { value: sigModeZh(S.health.signature_mode), value2: appT(S.health.play_gate ? "m073" : "m074") }) : appT("connectingServer");
  setConn(appConnOK); paintBell(); paintPlayerBar();
  if (S.view === "overview") {
    text("#ov-greeting", "m065"); text(".ov-scope", "m079");
    attr(".ov-filters .chips", "aria-label", "m080");
    attr("#ov-stat-node", "aria-label", "m081"); attr("#ov-favorites", "aria-label", "m083");
    const label = $(".ov-filter-label");
    if (label?.firstChild?.nodeType === 3) label.firstChild.nodeValue = appT("m081");
    for (const [days, key] of [[1, "m076"], [7, "m077"], [30, "m078"]]) text(`[data-act='ov-days'][data-days='${days}']`, key);
    paintOverview();
  }
}

function onClick(e) {
  const link = e.target.closest("a[href]");
  if (link && !link.dataset.act) return;
  const el = e.target.closest("[data-act]");
  if (!el) return;
  if (e.target.closest("input, select, textarea, label")) return;
  const fn = ACT[el.dataset.act];
  if (!fn) return;
  e.preventDefault();
  try {
    const r = fn(el.dataset, el, e);
    if (r && typeof r.catch === "function") r.catch(err => toast((err && err.message) || appT("m896"), "err"));
  } catch (err) { toast((err && err.message) || appT("m896"), "err"); }
}
function onChange(e) {
  const el = e.target.closest("[data-change]");
  if (!el) return;
  const fn = CHG[el.dataset.change];
  if (!fn) return;
  try {
    const r = fn(el, el.dataset);
    if (r && typeof r.catch === "function") r.catch(err => toast((err && err.message) || appT("m896"), "err"));
  } catch (err) { toast((err && err.message) || appT("m896"), "err"); }
}
function onInput(e) {
  const t = e.target;
  if (t.id === "global-search") { S.search = t.value; paintSearch(); return; }
  if (t.id === "pb-seek" || t.id === "pb-volume") {
    playerBarBeginRange(t);
    const volume = t.id === "pb-volume", label = $(volume ? "#pb-volume-value" : "#pb-elapsed");
    if (label) label.textContent = volume ? appN(Number(t.value)) : fmtDur(Number(t.value));
    t.setAttribute("aria-valuetext", volume ? appN(Number(t.value) / 100, { style: "percent" }) : fmtDur(Number(t.value)));
    return;
  }
  if (t.id === "ply-seek") { PLY.dragging = true; const el = $("#ply-el"); if (el) el.textContent = fmtDur(Number(t.value)); return; }
  if (t.id === "ply-vol") { PLY.volDrag = true; const v = $("#ply-vol-v"); if (v) v.textContent = appN(Number(t.value)); return; }
  if (t.id === "ply-add-search") { PLY.addSearch = t.value; paintPlayerAddList(); return; }
  if (t.classList && t.classList.contains("ro-pct-i")) paintRoSum();
}
function onKey(e) {
  if (e.key === "Escape") {
    if (confirmResolve) { closeConfirm(false); return; }
    const m = $("#modal-root"), d = $("#drawer-root");
    if (m && m.firstElementChild) { closeModal(); return; }
    if (d && d.firstElementChild) { closeDrawer(); return; }
    return;
  }
  if (e.key === "/" && !(e.target.closest && e.target.closest("input, textarea, select"))) {
    e.preventDefault();
    const gs = $("#global-search"); if (gs) gs.focus();
    return;
  }
  if ((e.key === "Enter" || e.key === " ") && e.target.matches && e.target.matches('[role="button"][data-act]')) {
    e.preventDefault(); e.target.click();
  }
}
function paintSearch() {
  const v = S.view;
  if (v === "overview") { paintOvRecent(); paintOvAlbums(); paintOvPlaylists(); }
  else if (v === "nodes") paintTerminalSearch();
  else if (v === "library") libraryBrowserSearch(S.search);
  else if (v === "catalog" || (v === "settings" && SETTINGS.section === "packages")) { paintApps(); paintBoms(); }
  else if (v === "audit") paintAudit();
}
function boot() {
  renderSidebarNav();
  document.addEventListener("click", onClick);
  document.addEventListener("change", onChange);
  document.addEventListener("input", onInput);
  document.addEventListener("keydown", onKey);
  document.addEventListener("pointerdown", e => { if (e.target.matches("#pb-seek, #pb-volume")) playerBarBeginRange(e.target); });
  document.addEventListener("pointerup", () => { PB.dragging = PB.volDrag = false; });
  document.addEventListener("pointercancel", e => {
    if (e.target.matches("#pb-seek, #pb-volume")) { PB.dragging = PB.volDrag = false; PB.seekContext = null; e.target.blur(); paintPlayerBar(); }
  });
  document.addEventListener("focusout", e => {
    if (e.target.matches("#pb-seek, #pb-volume")) { PB.dragging = PB.volDrag = false; PB.seekContext = null; }
  });
  initPlayerBar();
  setView("overview");
  bootstrapExperience();
  poll("global", globalTick, 4000, true);
}
document.addEventListener("DOMContentLoaded", boot);
