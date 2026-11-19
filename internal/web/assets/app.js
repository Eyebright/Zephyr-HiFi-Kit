"use strict";
/* Zephyr HiFi Kit · Operations Console · vanilla ES2020, no deps */

/* ---------- tiny helpers ---------- */
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
const enc = encodeURIComponent;
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const clamp = (n, a, b) => Math.min(b, Math.max(a, n));
const p2 = n => String(n).padStart(2, "0");
const val = id => { const el = document.getElementById(id); return el ? el.value : ""; };
const checked = id => { const el = document.getElementById(id); return !!(el && el.checked); };

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
  const d = new Date(t * 1000);
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())} ${p2(d.getHours())}:${p2(d.getMinutes())}:${p2(d.getSeconds())}`;
}
function ago(u) {
  const t = ts(u); if (!t) return "—";
  const s = Math.max(0, Math.floor(Date.now() / 1000) - t);
  if (s < 5) return "刚刚";
  if (s < 60) return s + " 秒前";
  if (s < 3600) return Math.floor(s / 60) + " 分钟前";
  if (s < 86400) return Math.floor(s / 3600) + " 小时前";
  return Math.floor(s / 86400) + " 天前";
}
function fmtUp(sec) {
  sec = Math.max(0, Math.floor(Number(sec) || 0));
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60), s = sec % 60;
  return (d ? d + " 天 " : "") + `${p2(h)}:${p2(m)}:${p2(s)}`;
}
function fmtBytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + " B";
  if (n < 1048576) return (n / 1024).toFixed(1) + " KB";
  if (n < 1073741824) return (n / 1048576).toFixed(1) + " MB";
  return (n / 1073741824).toFixed(2) + " GB";
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
  if (st === "online") return badge("在线", "ok");
  if (st === "offline") return badge("离线", "err");
  return badge("未知", "muted");
}
function authBadge(n) { return n && n.authorized ? badge("已授权", "ok") : badge("未授权", "warn"); }
const SEV_ZH = { critical: "严重", warning: "警告", info: "提示" };
function sevBadge(s) { return badge(SEV_ZH[s] || s || "提示", s === "critical" ? "err" : s === "warning" ? "warn" : "info"); }
const JOB_ZH = {
  queued: "排队中", precheck: "预检", resolve_deps: "解析依赖", waiting_idle: "等待空闲",
  downloading: "下载中", verifying: "校验签名", installing: "安装中", rendering_config: "渲染配置",
  enabling_units: "启用服务", validating: "验证", done: "已完成", rollback: "回滚中",
  rollback_verify: "回滚验证", failed_rolledback: "失败·已回滚", failed_unrecovered: "失败·未恢复",
  suspended: "已挂起", interrupted: "已中断", provision: "部署", upgrade: "升级", reconcile: "纠偏"
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
  const m = { pending: ["等待", "muted"], running: ["进行", "info"], ok: ["完成", "ok"], failed: ["失败", "err"], skipped: ["跳过", "muted"] };
  const x = m[st] || [st || "等待", "muted"]; return badge(x[0], x[1]);
}
function sigModeZh(m) { return ({ enforce: "强制校验", strict: "强制校验", dev: "开发模式", permissive: "宽松", warn: "仅警告", off: "关闭", disabled: "关闭" })[m] || (m || "—"); }
function catBadge(c) {
  const m = { renderer: "渲染器", dependency: "依赖", tool: "工具", source: "音源", core: "核心", audio: "音频", streamer: "串流" };
  return badge(m[c] || c || "应用", "violet");
}
function lcBadge(lc) {
  const m = { available: ["可用", "ok"], deprecated: ["弃用", "warn"], revoked: ["吊销", "err"] };
  const x = m[lc] || [lc || "可用", "muted"]; return badge(x[0], x[1]);
}
function resultBadge(r) {
  const s = String(r || "").toLowerCase();
  if (["ok", "success", "allowed", "done", "pass"].includes(s)) return badge("成功", "ok");
  if (["err", "error", "failed", "denied", "rejected", "fail"].includes(s)) return badge("失败", "err");
  return badge(r || "—", "muted");
}
function alertStatusBadge(a) {
  const s = a.status;
  if (s === "open") return badge("未处理", "warn");
  if (s === "ack" || s === "acked") return badge("已确认", "info");
  if (s === "resolved") return badge("已解决", "ok");
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
function setConn(ok) {
  const el = $("#conn"); if (!el) return;
  el.classList.toggle("ok", ok); el.classList.toggle("bad", !ok);
  const dot = el.querySelector(".sdot");
  if (dot) { dot.classList.toggle("ok", ok); dot.classList.toggle("err", !ok); dot.classList.toggle("live", ok); }
  const t = $("#conn-text"); if (t) t.textContent = ok ? "已连接" : "连接中断";
}
async function req(method, path, opt) {
  let r;
  try { r = await fetch(API_BASE + path, Object.assign({ method }, opt || {})); }
  catch (e) { setConn(false); throw new Error("无法连接服务器"); }
  setConn(true);
  const text = await r.text();
  let data = null;
  if (text) { try { data = JSON.parse(text); } catch (e) { data = null; } }
  if (!r.ok) {
    let msg = data && (data.error || data.message);
    if (msg && typeof msg === "object") msg = msg.message || JSON.stringify(msg);
    const err = new Error((typeof msg === "string" && msg) ? msg : (text ? text.slice(0, 160) : "请求失败 (" + r.status + ")"));
    err.status = r.status; err.data = data;
    throw err;
  }
  return data;
}
const api = (method, path, body) => req(method, path, method === "GET" || method === "HEAD" ? {} : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body === undefined ? {} : body) });
const apiForm = (path, fd) => req("POST", path, { body: fd });
async function rootJson(path) {
  const r = await fetch(path);
  if (!r.ok) throw new Error("请求失败 (" + r.status + ")");
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
    (opts.undo ? `<button class="t-undo" data-act="toast-undo" data-tid="${tid}">${esc(opts.undoText || "撤销")}</button>` : "") +
    `<button class="t-x" data-act="toast-close" data-tid="${tid}" aria-label="关闭">${icon("x", 14)}</button>`;
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
function confirmDlg({ title = "确认操作", message = "", okText = "确认", danger = true } = {}) {
  return new Promise(res => {
    confirmResolve = res;
    $("#confirm-root").innerHTML = `
      <div class="confirm-backdrop" data-act="confirm-backdrop">
        <div class="confirm ${danger ? "" : "safe"}" role="dialog" aria-modal="true" aria-label="${esc(title)}">
          <div class="c-ic">${icon(danger ? "alert" : "info", 22)}</div>
          <div class="c-title">${esc(title)}</div>
          <div class="c-msg">${esc(message)}</div>
          <div class="c-btns">
            <button class="btn ghost" data-act="confirm-cancel">取消</button>
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
          <button class="icon-btn" data-act="modal-close" aria-label="关闭">${icon("x", 18)}</button>
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
  const back = opts.back ? `<button class="icon-btn" data-act="${esc(opts.back.act)}" data-id="${esc(opts.back.id)}" title="返回" aria-label="返回">${icon("chevL", 18)}</button>` : "";
  $("#drawer-root").innerHTML = `
    <div class="drawer-backdrop" data-act="drawer-backdrop"></div>
    <div class="drawer" role="dialog" aria-modal="true">
      <div class="drawer-head">
        ${back}
        <div style="flex:1;min-width:0">
          <div class="d-title">${title}</div>
          ${sub ? `<div class="d-sub truncate">${sub}</div>` : ""}
        </div>
        <button class="icon-btn" data-act="drawer-close" aria-label="关闭">${icon("x", 18)}</button>
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
  { id: "overview", name: "主页", icon: "home" },
  { id: "library", name: "音乐库", icon: "library" },
  { id: "nodes", name: "终端中心", icon: "server" },
  { id: "audit", name: "操作日志", icon: "audit" },
  { id: "settings", name: "设置", icon: "settings" }
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
  return `<div class="kpi" style="--kc:var(--${color})"><div class="k-top">${icon(ic, 15)}<span class="k-lbl">${esc(label)}</span></div><div class="k-num">${num}${sub ? ` <small>${esc(sub)}</small>` : ""}</div></div>`;
}
function hit(...fields) {
  const q = S.search.trim().toLowerCase();
  if (!q) return true;
  return fields.some(f => String(f ?? "").toLowerCase().includes(q));
}
function viewError(e) {
  $("#view").innerHTML = `<div class="card">${empty("alert", "加载失败：" + ((e && e.message) || "未知错误"), `<button class="btn primary" data-act="refresh-view">${icon("refresh", 15)} 重试</button>`)}</div>`;
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
    if (hl && h) hl.textContent = `签名 ${sigModeZh(h.signature_mode)} · 门禁 ${h.play_gate ? "开" : "关"}`;
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
  if (el) { el.textContent = open > 99 ? "99+" : String(open); el.classList.toggle("hidden", open === 0); }
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
      <div><div class="ov-eyebrow">YOUR LISTENING ROOM</div><h1 id="ov-greeting">主页</h1><p class="ov-scope">仅ZAT推送 · 终端累计收听</p></div>
      <div class="ov-filters"><div class="chips" aria-label="统计日期范围">${[[1, "今天"], [7, "近7天"], [30, "近30天"]].map(([d, label]) => `<button class="chip ${OV.days === d ? "active" : ""}" data-act="ov-days" data-days="${d}" aria-pressed="${OV.days === d}">${label}</button>`).join("")}</div>
        <label class="ov-filter-label">统计终端<select id="ov-stat-node" class="inp" data-change="ov-stat-node" aria-label="统计终端"><option value="">全部终端</option></select></label>
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
    <section class="card mt16" id="ov-favorites" aria-label="喜欢的音乐"></section>
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
      || !["genres", "artists", "recent_albums"].every(k => Array.isArray(d[k]))) throw new Error("统计响应不完整");
    OV.data = d; OV.error = "";
  } catch (e) {
    if (epoch !== OV.epoch || seq !== OV.statsSeq || S.view !== "overview") return;
    OV.data = null; OV.error = e.message || "请求失败";
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
  let html = `<option value="">${all ? "全部终端" : "选择播放终端"}</option>` + nodes.map(n => `<option value="${esc(n.node_id)}">${esc(n.alias || shortId(n.node_id))}${n.status === "online" ? "" : " · 离线"}</option>`).join("");
  if (selected && !nodes.some(n => n.node_id === selected)) html += `<option value="${esc(selected)}">${esc(shortId(selected))} · 已移除</option>`;
  return html;
}
function paintOverview() { paintListening(); paintOvAlbums(); paintOvPlaylists(); paintSysStrip(); if (typeof paintFavoritePreview === "function") paintFavoritePreview(); }
function paintListening() {
  paintOverviewKpis(); paintOvRecent(); paintOvTastes();
  ovSyncSelect($("#ov-stat-node"), ovNodeOptions(OV.node, true), OV.node);
}
function fmtListen(ms) {
  const sec = Math.floor(Math.max(0, Number(ms) || 0) / 1000);
  if (sec < 60) return sec + " 秒";
  const min = Math.floor(sec / 60);
  return min < 60 ? min + " 分钟" : Math.floor(min / 60) + " 小时 " + min % 60 + " 分";
}
function ovRetry(act = "ov-retry") { return `<button class="btn xs ghost" data-act="${act}">${icon("refresh", 12)} 重试</button>`; }
function ovStatEmpty() { return empty(OV.error ? "alert" : "clock", OV.error ? "统计暂不可用" : "正在加载收听统计…", OV.error ? ovRetry() : "", true); }
function paintOverviewKpis() {
  const el = $("#ov-kpis"); if (!el) return;
  const d = OV.data;
  el.innerHTML = kpi(d && d.unmeasured_plays ? "收听时长 · 已知部分" : "收听时长", d ? (!d.listen_ms && d.unmeasured_plays ? "时长未知" : fmtListen(d.listen_ms)) : "—", "amber", "clock") +
    kpi("不同曲目", d ? d.track_count : "—", "amber", "music") +
    kpi("专辑", d ? d.album_count : "—", "amber", "disc") +
    kpi("艺术家", d ? d.artist_count : "—", "amber", "music");
  const note = $("#ov-stat-note");
  if (note) note.innerHTML = OV.error ? `<span class="txt-gold">收听统计加载失败：${esc(OV.error)}</span> ${ovRetry()}` :
    d ? `按服务端本地时区${d.timezone ? "（" + esc(d.timezone) + "）" : ""}统计 · ${OV.days === 1 ? "今天" : "含今天的 " + OV.days + " 个自然日"} · 多终端收听累计 · 历史数据独立保存${d.unmeasured_plays > 0 ? `<span class="ov-unmeasured">${d.unmeasured_plays} 条旧记录时长未知，不计入收听时长及占比；0 不代表已知时长</span>` : ""}` : "正在加载收听统计…";
}
function paintOvTastes() {
  const genres = $("#ov-genres"), artists = $("#ov-artists"); if (!genres || !artists) return;
  const gh = secHead(icon("disc", 17) + " 常听风格", `<span class="hint">按已知收听时长占比</span>`);
  const ah = secHead(icon("music", 17) + " 艺术家排行", `<span class="hint">TOP 5 · 收听时长</span>`);
  const d = OV.data;
  if (!d) { genres.innerHTML = gh + ovStatEmpty(); artists.innerHTML = ah + ovStatEmpty(); return; }
  const nonnegative = v => Math.max(0, Number(v) || 0);
  const sorted = d.genres.slice().sort((a, b) => nonnegative(b.listen_ms) - nonnegative(a.listen_ms));
  const known = sorted.filter(g => g.name && !["未分类", "未知"].includes(g.name));
  const unknown = sorted.filter(g => !g.name || ["未分类", "未知"].includes(g.name)).reduce((n, g) => n + nonnegative(g.listen_ms), 0);
  const total = sorted.reduce((n, g) => n + nonnegative(g.listen_ms), 0);
  const segments = known.slice(0, 5).map(g => ({ name: g.name, ms: nonnegative(g.listen_ms) }));
  const other = known.slice(5).reduce((n, g) => n + nonnegative(g.listen_ms), 0);
  if (other) segments.push({ name: "其他", ms: other });
  if (unknown) segments.push({ name: "未知 / 未分类", ms: unknown, unknown: true });
  const colors = ["#f6a821", "#d9bd7b", "#a79062", "#817658", "#c78641", "#6b7280"];
  genres.innerHTML = gh + (total ? `<div class="ov-genre-bar" aria-label="常听风格占比">${segments.map((g, i) => `<span style="width:${g.ms / total * 100}%;background:${g.unknown ? "#485363" : colors[i % colors.length]}" title="${esc(g.name)} ${ (g.ms / total * 100).toFixed(1)}%"></span>`).join("")}</div><div class="ov-genre-legend">${segments.map((g, i) => `<div><i style="background:${g.unknown ? "#485363" : colors[i % colors.length]}"></i><span class="truncate">${esc(g.name)}</span><b class="mono">${(g.ms / total * 100).toFixed(1)}%</b></div>`).join("")}</div>` : empty("disc", d.unmeasured_plays ? "暂无可计量的风格收听时长，旧记录时长未知" : "这段时间还没有收听时长", "", true));
  const rank = d.artists.slice().sort((a, b) => nonnegative(b.listen_ms) - nonnegative(a.listen_ms)).filter(a => nonnegative(a.listen_ms) > 0).slice(0, 5);
  artists.innerHTML = ah + (rank.length ? `<ol class="ov-rank">${rank.map((a, i) => `<li><span class="ov-rank-num mono">${p2(i + 1)}</span><div class="ov-rank-main"><div><span class="truncate">${esc(a.name || "未知艺术家")}</span><span class="ov-rank-value">${fmtListen(a.listen_ms)} · ${nonnegative(a.track_count)} 首</span></div><div class="ov-rank-track"><span style="width:${nonnegative(a.listen_ms) / nonnegative(rank[0].listen_ms) * 100}%"></span></div></div></li>`).join("")}</ol>` : empty("music", d.unmeasured_plays ? "旧记录时长未知，暂无法按时长排行" : "听几首喜欢的歌，留下你的排行", "", true));
}
function ovAlbumHead(title, kind, more, count) {
  return secHead(icon(kind === "recent" ? "clock" : "disc", 17) + " " + title,
    `${count > 8 ? `<button class="btn xs ghost" data-act="ov-more" data-kind="${kind}" aria-expanded="${more}">${more ? "收起" : "展开更多"}</button>` : ""}${kind === "recent" ? "" : `<button class="btn xs ghost" data-act="ov-goto" data-tab="albums">音乐库 ${icon("chevR", 12)}</button>`}`);
}
function ovAlbumCard(g, info = {}) {
  const historical = info.history === true, missing = !g;
  const album = historical ? info.album : g.album, artist = historical ? info.artist : g.artist;
  const hash = historical ? info.cover_hash : albumCoverHash(g);
  const playable = !missing && g.tracks.some(t => t.decodable);
  const key = g ? esc(albumKey(g)) : "";
  const count = historical ? "听过 " + info.track_count + " 首" : g.tracks.length + " 首";
  const unavailable = !OV.loaded ? "正在加载曲库" : OV.tracksError ? "曲库暂不可用，收听记录不受影响" : "曲库中已不可用，收听记录仍保留";
  return `<article class="ov-album">
    <button class="ov-album-open" ${missing ? 'disabled aria-disabled="true"' : `data-act="album-open" data-ak="${key}"`} ${missing ? `title="${unavailable}"` : ""} aria-label="查看专辑：${esc(album || "未分类专辑")}">
      <span class="al-cover">${hash ? `<img class="al-img" src="/api/v1/covers/${esc(hash)}" alt="" loading="lazy">` : icon("disc", 30)}<span class="al-cnt mono">${count}</span></span>
      <span class="al-name truncate">${esc(album || "未分类专辑")}</span><span class="al-artist truncate">${esc(artist || "未知艺术家")}</span>
    </button>
    <div class="ov-album-foot"><span class="hint truncate" title="${esc(info.time ? fmtTime(info.time) : "")}">${info.time ? ago(info.time) : ""}</span><button class="btn xs ghost" data-act="album-push" data-ak="${key}" ${playable ? "" : "disabled"} title="${missing ? unavailable : "推送播放 / 加入队列"}" aria-label="推送播放或加入队列：${esc(album || "未分类专辑")}">${icon("send", 12)}</button></div>
  </article>`;
}
function paintOvRecent() {
  const el = $("#ov-recent"); if (!el) return;
  const list = OV.data ? OV.data.recent_albums.filter(g => hit(g.artist, g.album)) : [];
  const head = ovAlbumHead("最近听过的专辑", "recent", OV.recentMore, list.length);
  if (!OV.data) { el.innerHTML = head + ovStatEmpty(); return; }
  const groups = new Map(OV.loaded && !OV.tracksError ? albumGroups().map(g => [albumKey(g), g]) : []);
  const note = `<p class="ov-note">独立收听记录 · 移除曲目不影响统计${OV.tracksError ? ` · 曲库暂不可用 ${ovRetry("ov-library-retry")}` : ""}</p>`;
  el.innerHTML = head + note + (list.length ? `<div class="ov-album-row ${OV.recentMore ? "expanded" : ""}">${list.slice(0, OV.recentMore ? 50 : 8).map(a => ovAlbumCard(groups.get(albumKey(a)), Object.assign({}, a, { history: true, time: a.last_played_at }))).join("")}</div>` : empty("disc", S.search ? "没有匹配的专辑" : "这段时间还没有听过专辑。从音乐库推送一张开始吧。", "", true));
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
  const head = ovAlbumHead("新添加的专辑", "added", OV.addedMore, list.length);
  if (!OV.loaded || OV.tracksError) { el.innerHTML = head + empty(OV.tracksError ? "alert" : "disc", OV.tracksError ? "音乐库加载失败" : "正在加载音乐库…", OV.tracksError ? ovRetry("ov-library-retry") : "", true); return; }
  const note = `<p class="ov-note">按专辑最早入库时间排序${unknown ? ` · ${unknown} 张老库专辑首次入库时间未知，未列入新添加` : ""}</p>`;
  el.innerHTML = head + note + (list.length ? `<div class="ov-album-row ${OV.addedMore ? "expanded" : ""}">${list.slice(0, OV.addedMore ? list.length : 8).map(a => ovAlbumCard(a.g, { time: a.added })).join("")}</div>` : empty("disc", S.search ? "没有匹配的新专辑" : unknown ? "老库首次入库时间未知，等待新专辑加入" : "还没有新添加的专辑", `<button class="btn xs ghost" data-act="ov-goto" data-tab="albums">前往音乐库</button>`, true));
}
function paintOvPlaylists() {
  const el = $("#ov-playlists"); if (!el) return;
  const head = secHead(icon("layers", 16) + " 播放列表", `<button class="btn xs ghost" data-act="pl-new">${icon("plus", 12)} 新建</button><button class="btn xs ghost" data-act="ov-goto" data-tab="playlists">全部列表 ${icon("chevR", 12)}</button>`);
  const list = S.playlists.filter(p => hit(p.name));
  el.innerHTML = head + (!OV.loaded ? `<span class="hint">正在加载…</span>` : OV.playlistsError ? `<span class="hint">播放列表加载失败</span> ${ovRetry("ov-library-retry")}` :
    `<div class="ov-playlist-links">${list.length ? list.slice(0, 6).map(p => `<button class="btn ghost sm" data-act="pl-open" data-id="${esc(p.id)}">${icon("layers", 13)}<span class="truncate">${esc(p.name)}</span><span class="hint">${p.count} 首</span></button>`).join("") : `<span class="hint">${S.search ? "没有匹配的播放列表" : "把喜欢的曲目组成列表，从这里快速开始。"}</span>`}</div>`);
}
// Keep focused controls mounted while live playback fields update.
const PB = {
  id: "", chosen: false, nodes: [], loaded: false, nodesError: false, nodesAt: 0,
  seq: 0, busy: null, fresh: false, trackID: "", track: null, dragging: false, volDrag: false,
  seekContext: null, seekPending: 0, volPending: 0, devicePending: 0
};
function playerBarShell() {
  return `<section class="player-bar" id="player-bar" aria-label="全局播放控制">
    <div class="player-bar-meta"><div id="pb-cover"></div><div class="player-bar-text"><div class="player-bar-title truncate" id="pb-title">空闲 · 未推流</div><div class="player-bar-sub truncate" id="pb-sub">选择终端，从音乐库推送播放</div><div class="player-bar-state" id="pb-state" role="status">正在连接…</div></div>${typeof favoritePlayerShell === "function" ? favoritePlayerShell() : ""}</div>
    <div class="player-bar-transport"><button class="btn ghost" id="pb-prev" data-act="queue-prev" aria-label="上一首" title="上一首" disabled>${icon("chevL", 17)}</button><button class="btn primary pc-play" id="pb-pause" data-act="play-pause" aria-label="继续播放" title="继续播放" disabled>${icon("play", 18)}</button><button class="btn ghost" id="pb-next" data-act="queue-next" aria-label="下一首" title="下一首" disabled>${icon("chevR", 17)}</button></div>
    <div class="player-bar-tools" tabindex="0" aria-label="播放进度、音量与终端设置，窄屏可横向滚动">
      <div class="player-bar-seek"><span id="pb-elapsed" class="mono">0:00</span><input id="pb-seek" type="range" min="0" max="0" step="500" value="0" data-change="pb-seek" aria-label="播放进度" disabled><span id="pb-total" class="mono">0:00</span></div>
      <label class="player-bar-volume" title="音量">${icon("speaker", 15)}<input id="pb-volume" type="range" min="0" max="100" step="1" value="100" data-change="pb-volume" aria-label="音量" disabled><span id="pb-volume-value" class="mono">100</span></label>
      <label class="player-bar-select">播放终端<select id="pb-node" class="inp" data-change="pb-node" aria-label="播放终端（独立于统计筛选）"><option value="">选择播放终端</option></select></label>
      <label class="player-bar-select">输出设备<select id="pb-device" class="inp mono" data-change="pb-device" aria-label="输出设备（下次播放生效）" title="下次播放生效" disabled><option value="default">default</option></select></label>
      <button class="btn xs ghost" id="pb-console" data-act="player-open" title="队列与完整控制台" disabled>${icon("layers", 14)} 队列 / 控制台</button>
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
  const title = active ? p.title || (t && t.title) || "未知曲目" : "空闲 · 未推流";
  const artist = active ? p.artist || (t && t.artist) || "未知艺术家" : "";
  const album = active ? p.album || (t && t.album) || "未分类专辑" : "";
  const set = (selector, text) => { const el = $(selector); if (el && el.textContent !== text) el.textContent = text; };
  set("#pb-title", title);
  set("#pb-sub", active ? artist + " · " + album : "选择终端，从音乐库推送播放");
  const status = !PB.loaded ? "正在加载终端…" : PB.nodesError ? "终端状态加载失败 · 控制已禁用" : !id ? (PB.nodes.length ? "未选择播放终端" : "暂无终端 · 请在终端中心配置") : !n ? "终端已移除 · 控制已禁用" : n.status !== "online" ? "离线 · 控制已禁用" : !PB.fresh ? "播放状态暂不可用 · 控制已禁用" : !connected ? "ZAT 未连接 · 控制已禁用" : p.playing ? "正在播放" : p.state === "paused" ? "已暂停" : "空闲 · 等待推送";
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
  const ppLabel = active && p.playing ? "暂停" : "继续播放";
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
    if (!PB.volDrag && !PB.volPending && document.activeElement !== vol) { vol.value = String(clamp(p.volume == null ? 100 : Number(p.volume), 0, 100)); set("#pb-volume-value", vol.value); }
    vol.setAttribute("aria-valuetext", vol.value + "%");
  }
  const node = $("#pb-node");
  ovSyncSelect(node, ovNodeOptions(id, false, PB.nodes), id);
  if (node) node.disabled = PB.dragging || PB.volDrag || !!(PB.seekPending || PB.volPending || PB.devicePending);
  const device = $("#pb-device"), dv = nodePlayDevs(p);
  if (device) {
    if (!PB.devicePending) ovSyncSelect(device, nodePlayOpts(dv.devs, dv.cur), dv.cur);
    device.disabled = !connected || !!PB.devicePending;
    device.title = (active && p.alsa_device ? "当前输出：" + p.alsa_device + " · " : "") + "更改下次播放生效";
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
  if (!volume && (!context || context.id !== id || context.track !== p.track_id || context.session !== p.session_id)) { toast("曲目已切换，请重新定位进度", "warn"); el.blur(); paintPlayerBar(); return; }
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
  try { await api("POST", `/nodes/${enc(id)}/device`, { device: el.value }); toast("输出设备已设置（下次播放生效）", "ok"); }
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
  el.innerHTML = secHead(icon("cpu", 17) + " 系统状态",
    `${m ? badge("v" + (m.version || "—"), "gold") : ""}<button class="btn xs ghost" data-act="ov-sys-toggle">${S.sysOpen ? "收起" : "展开"} ${icon(S.sysOpen ? "arrowUp" : "arrowDown", 12)}</button>`) +
    `<div class="sys-strip">
      <span class="sys-item">${icon("cpu", 13)} CPU <b class="mono">${cpu != null ? cpu.toFixed(1) + " %" : "—"}</b></span>
      <span class="sys-item">${icon("database", 13)} 内存 <b class="mono">${mem != null ? mem.toFixed(0) + " MB" : "—"}</b></span>
      <span class="sys-item">${icon("send", 13)} 运行任务 <b class="mono">${running}</b></span>
      <span class="sys-item">${icon("download", 13)} 待升级包 <b class="mono">${pkgs}</b></span>
      <button class="sys-item link" data-act="bell">${icon("bell", 13)} 未处理告警 <b class="mono ${open ? "txt-gold" : ""}">${open}</b></button>
    </div>` +
    (S.sysOpen ? `<div class="divider"></div>` + sysDetail() : "");
}
function sysDetail() {
  const m = S.metrics;
  if (!m) return empty("cpu", "暂无服务器指标", "", true);
  const cpu = Number(m.cpu_percent) || 0, mem = Number(m.mem_mb) || 0;
  return meter("CPU 占用", cpu, cpu.toFixed(1) + " %", "cyan") +
    `<div class="kv mt12">` +
    kvRow("内存", `<span class="mono">${mem.toFixed(0)} MB</span>`) +
    kvRow("Goroutines", `<span class="mono">${m.goroutines ?? "—"}</span>`) +
    kvRow("数据库", `<span class="mono">${fmtBytes(m.db_bytes)}</span>`) +
    kvRow("运行时长", `<span class="mono">${fmtUp(m.uptime_s)}</span>`) +
    kvRow("签名模式", esc(sigModeZh(m.signature_mode))) +
    kvRow("播放门禁", m.play_gate ? badge("开", "ok") : badge("关", "muted")) +
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
    <div class="al-cover">${albumCoverHash(g) ? `<img class="al-img" src="/api/v1/covers/${esc(albumCoverHash(g))}" alt="" loading="lazy">` : icon("disc", sm ? 22 : 30)}<span class="al-cnt mono">${g.tracks.length} 首</span></div>
    <div class="al-name truncate">${esc(g.album || "未分类专辑")}</div>
    <div class="al-artist truncate">${esc(g.artist || "未知艺术家")}</div>
    ${sm ? "" : `<div class="al-meta mono">${fmtDur(g.ms)}</div>`}
  </div>`;
}

/* ================= NODES ================= */

/* ---------- bulk operations ---------- */
async function bulkAuth(revoke) {
  const ids = Array.from(S.sel); if (!ids.length) return;
  let ok = 0, fail = 0;
  for (const id of ids) { try { await api("POST", `/nodes/${enc(id)}/${revoke ? "revoke" : "authorize"}`); ok++; } catch (e) { fail++; } }
  toast(`${revoke ? "撤销" : "授权"}完成：成功 ${ok} 台${fail ? `，失败 ${fail} 台` : ""}`, fail ? "warn" : "ok");
  S.sel.clear(); await refreshCurrent();
}
async function bulkDelete() {
  const ids = Array.from(S.sel); if (!ids.length) return;
  const okc = await confirmDlg({ title: "批量注销终端", message: `将注销 ${ids.length} 台终端。播放中的终端会被拒绝。注销后可在提示中撤销。`, okText: "注销" });
  if (!okc) return;
  await deleteNodes(ids);
}
async function deleteNodes(ids) {
  const deleted = [], failed = [];
  for (const id of ids) {
    try { const r = await api("DELETE", `/nodes/${enc(id)}`); deleted.push((r && r.node) || S.nodes.find(n => n.node_id === id) || { node_id: id }); }
    catch (e) { failed.push(`${shortId(id)}：${e.message}`); }
  }
  if (failed.length) toast("部分终端注销失败 — " + failed.join("；"), "err");
  if (deleted.length) {
    toast(`已注销 ${deleted.length} 台终端`, "ok", {
      undo: async () => {
        let ok = 0; for (const node of deleted) { try { await api("POST", "/nodes/restore", { node }); ok++; } catch (e) { /* best-effort */ } }
        toast(`已恢复 ${ok} 台终端`, ok ? "ok" : "err"); refreshCurrent();
      }, undoText: "撤销"
    });
  }
  S.sel.clear(); closeModal(); closeDrawer(); await refreshCurrent();
}

/* ================= ADD-NODE WIZARD ================= */
const WZ = { step: 1, alias: "", group: "", arch: "amd64", distro: "debian", ttl: 1800, note: "", token: "", expiresAt: 0, nodeId: "", node: null };
const ANDROID_APK = "https://github.com/Eyebright/Zephyr-HiFi-Kit/releases/download/v0.50/Zephyr-HiFi-Kit-terminal-0.50.apk";
function installCmd() {
  if (WZ.arch === "android") return `1. 下载安装 APK：${ANDROID_APK}\n2. 启动「Zephyr HiFi Kit」，服务端地址填 ${location.origin}\n3. 注册令牌填 ${WZ.token}`;
  return `curl -fsSL ${location.origin}/dl/agent-install.sh | sudo bash -s -- --server ${location.origin} --token ${WZ.token}`;
}
function openWizard() {
  Object.assign(WZ, { step: 1, alias: "", group: "", arch: "amd64", distro: "debian", ttl: 1800, note: "", token: "", expiresAt: 0, nodeId: "", node: null });
  renderWizard();
}
function wzStepsHtml() {
  const labels = ["填写信息", "安装 Agent", "完成"];
  return `<div class="wz-steps">` + labels.map((s, i) => {
    const n = i + 1, cls = WZ.step === n ? "active" : WZ.step > n ? "done" : "";
    return `<div class="wz-step ${cls}"><span class="w-dot">${WZ.step > n ? icon("check", 12) : n}</span><span class="w-lbl">${s}</span></div>` + (n < 3 ? `<div class="wz-line"></div>` : "");
  }).join("") + `</div>`;
}
function renderWizard() {
  let body = "", foot = "";
  if (WZ.step === 1) {
    body = wzStepsHtml() + wzStep1();
    foot = `<button class="btn ghost" data-act="modal-close">取消</button><button class="btn primary" data-act="wz-next">生成安装令牌 ${icon("chevR", 14)}</button>`;
  } else if (WZ.step === 2) {
    body = wzStepsHtml() + wzStep2();
    foot = `<button class="btn ghost" data-act="wz-back">${icon("chevL", 14)} 上一步</button><button class="btn ghost" data-act="modal-close">取消</button>`;
  } else {
    body = wzStepsHtml() + wzStep3();
    foot = `<button class="btn primary" data-act="modal-close">完成</button>`;
  }
  openModal(`${icon("plus", 18)} 添加终端`, body, foot, { narrow: true, onClose: () => { stopPoll("wz:token"); stopPoll("wz:cd"); } });
}
function wzStep1() {
  const opt = (v, l, cur) => `<option value="${v}" ${v === cur ? "selected" : ""}>${l}</option>`;
  return `<div id="wz-err"></div>
    <div class="f-row">
      <div class="field"><label>别名 *</label><input id="wz-alias" class="inp" placeholder="如：客厅胆机" value="${esc(WZ.alias)}" maxlength="60"></div>
      <div class="field"><label>分组</label><input id="wz-group" class="inp" placeholder="如：一楼" value="${esc(WZ.group)}" maxlength="40"></div>
    </div>
    <div class="f-row">
      <div class="field"><label>架构</label><select id="wz-arch" class="inp" data-change="wz-arch">${opt("amd64", "amd64 (x86_64)", WZ.arch)}${opt("arm64", "arm64 (aarch64)", WZ.arch)}${opt("armhf", "armhf", WZ.arch)}${opt("armv7hf", "armv7hf", WZ.arch)}${opt("android", "Android (arm64)", WZ.arch)}</select></div>
      <div class="field"><label>发行版</label><select id="wz-distro" class="inp">${opt("debian", "Debian", WZ.distro)}${opt("ubuntu", "Ubuntu", WZ.distro)}${opt("raspberry-pi-os", "Raspberry Pi OS", WZ.distro)}${opt("armbian", "Armbian", WZ.distro)}${opt("android", "Android", WZ.distro)}${opt("other", "其他", WZ.distro)}</select></div>
    </div>
    <div class="f-row">
      <div class="field"><label>令牌有效期</label><select id="wz-ttl" class="inp">${opt("600", "10 分钟", String(WZ.ttl))}${opt("1800", "30 分钟", String(WZ.ttl))}${opt("3600", "1 小时", String(WZ.ttl))}${opt("86400", "24 小时", String(WZ.ttl))}</select></div>
      <div class="field"><label>备注</label><input id="wz-note" class="inp" placeholder="可选" value="${esc(WZ.note)}" maxlength="120"></div>
    </div>
    <div class="f-hint">生成一次性令牌后，在目标主机执行安装命令；Agent 注册成功会自动进入下一步。</div>`;
}

function wzStep2() {
  const android = WZ.arch === "android";
  const installPanel = android
    ? `<div class="field"><label>在安卓设备安装</label>
      <div class="f-hint">下载 APK 安装后启动「Zephyr HiFi Kit」，填入下方服务端地址与令牌即可注册上线。</div>
      <a class="btn sm block" href="${ANDROID_APK}" target="_blank" rel="noopener">${icon("download", 14)} 下载 Zephyr HiFi Kit 终端 APK</a>
      <div class="cmd-box" style="white-space:pre-wrap"><span id="wz-cmd">${esc(installCmd())}</span></div>
      <button class="btn sm block" data-act="wz-copy-cmd">${icon("copy", 14)} 复制配置信息</button>
    </div>`
    : `<div class="field"><label>在目标主机执行（需要 sudo）</label>
      <div class="cmd-box"><span class="c-prompt">$</span><span id="wz-cmd">${esc(installCmd())}</span></div>
      <button class="btn sm block" data-act="wz-copy-cmd">${icon("copy", 14)} 复制安装命令</button>
    </div>`;
  return `<div class="field"><label>安装令牌</label>
      <div class="token-box"><span class="tok" id="wz-token">${esc(WZ.token)}</span><button class="btn xs" data-act="wz-copy-token">${icon("copy", 13)} 复制</button></div>
      <div class="f-hint">剩余有效时间 <span class="wz-count" id="wz-cd">--:--</span> · 一次性，注册后失效</div>
    </div>
    ${installPanel}
    <div class="waiting"><span class="w-pulse"></span><div style="flex:1"><div class="w-txt">等待 Agent 注册…</div><div class="w-sub">每 2 秒自动检测令牌使用状态</div></div><span class="spin"></span></div>`;
}
function wzStep3() {
  const n = WZ.node || { node_id: WZ.nodeId, alias: WZ.alias };
  return `<div class="banner ok">${icon("check", 16)}<div><b>${esc(n.alias || shortId(n.node_id))}</b> 已成功注册并上线。</div></div>
    <div class="tiles c2">
      <button class="tile" style="--tc:var(--green)" data-act="wz-auth" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("shield", 16)}</span><span class="t-name">授权终端</span><span class="t-sub">纳入授权席位管理</span></button>
      <button class="tile" style="--tc:var(--amber)" data-act="wz-config" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("sliders", 16)}</span><span class="t-name">配置期望态</span><span class="t-sub">绑定 BOM / DAC / 保护</span></button>
      <button class="tile" style="--tc:var(--cyan)" data-act="wz-console" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("terminal", 16)}</span><span class="t-name">打开控制台</span><span class="t-sub">远程终端界面</span></button>
      <button class="tile" style="--tc:var(--violet)" data-act="wz-detail" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("eye", 16)}</span><span class="t-name">查看详情</span><span class="t-sub">资源与已装应用</span></button>
    </div>`;
}
async function wzNext() {
  WZ.alias = val("wz-alias").trim(); WZ.group = val("wz-group").trim();
  WZ.arch = val("wz-arch"); WZ.distro = val("wz-distro"); WZ.ttl = Number(val("wz-ttl")) || 1800; WZ.note = val("wz-note").trim();
  if (WZ.arch === "android") WZ.distro = "android";
  const errEl = $("#wz-err");
  const fail = m => { if (errEl) errEl.innerHTML = `<div class="f-err mb12">${esc(m)}</div>`; };
  if (!WZ.alias) return fail("请填写终端别名");
  if (S.nodes.some(n => (n.alias || "").trim().toLowerCase() === WZ.alias.toLowerCase())) return fail("别名已存在，请换一个");
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
    if (w) w.innerHTML = `${icon("clock", 18)}<div style="flex:1"><div class="w-txt txt-warn">令牌已过期</div><div class="w-sub">请重新生成</div></div><button class="btn sm primary" data-act="wz-reissue">重新生成</button>`;
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
    toast(`终端「${WZ.alias || shortId(WZ.nodeId)}」注册成功`, "ok");
    refreshCurrent();
  }
}

/* ---------- shared tile builder ---------- */
function tile(ic, name, sub, act, color, id) {
  return `<button class="tile" style="--tc:var(--${color})" data-act="${act}" ${id ? `data-id="${esc(id)}"` : ""}><span class="t-ic">${icon(ic, 16)}</span><span class="t-name">${esc(name)}</span><span class="t-sub">${esc(sub)}</span></button>`;
}
const CMD_ZH = { "os-check": "检查更新", "os-upgrade": "系统升级", netdiag: "网络自检", "apt-install": "安装软件", "apt-source": "配置安装源", "agent-upgrade": "Agent 升级" };

/* ---- shared ALSA device picker (homepage and full player) ---- */
function nodePlayDevs(p) {
  const devs = Array.isArray(p.devices) ? p.devices.slice() : [];
  if (!devs.includes("default")) devs.unshift("default");
  const cur = p.pref_device || p.configured_device || "default";
  if (!devs.includes(cur)) devs.push(cur);
  return { devs, cur };
}
function nodePlayOpts(devs, cur) {
  return devs.map(d => `<option value="${esc(d)}" ${d === cur ? "selected" : ""}>${esc(d === "default" ? "default（系统默认）" : d)}</option>`).join("");
}

/* ---- shared server-side playback queue actions ---- */
async function queueMove(id, dir) {
  try {
    await api("POST", `/nodes/${enc(id)}/queue/${dir}`);
    toast(dir === "next" ? "播放下一首" : "播放上一首", "ok");
  } catch (e) { toast(e.message, "err"); }
  await queueAfter(id);
}
async function queueJump(id, idx) {
  try { await api("POST", `/nodes/${enc(id)}/queue/jump`, { index: idx }); toast("播放所选曲目", "ok"); }
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
  try { S.nodeQueue[id] = await api("PUT", `/nodes/${enc(id)}/queue`, { track_ids: ids, position: pos }); toast("已移出队列", "ok"); }
  catch (e) { toast(e.message, "err"); }
  paintNodePlay(id);
  await syncPlayUI();
}
async function queueClear(id) {
  try { S.nodeQueue[id] = await api("PUT", `/nodes/${enc(id)}/queue`, { track_ids: [], position: 0 }); toast("队列已清空", "ok"); }
  catch (e) { toast(e.message, "err"); }
  paintNodePlay(id);
  await syncPlayUI();
}

/* ================= OS UPDATES ================= */
const OSC = { id: "", state: null, sel: new Set(), busy: false, cmdId: "", cmdType: "", tries: 0, log: "", err: "", result: "" };
async function openOsDrawer(id) {
  OSC.id = id; OSC.state = null; OSC.sel = new Set(); OSC.busy = false;
  OSC.cmdId = ""; OSC.cmdType = ""; OSC.tries = 0; OSC.log = ""; OSC.err = ""; OSC.result = "";
  openDrawer("系统更新", esc(id), empty("refresh", "正在读取更新状态…", "", true), "", { ctx: { kind: "os", id }, back: { act: "node-open", id } });
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
  if (st.reboot_required) h += `<div class="banner warn">${icon("power", 15)}<span>该终端需要重启以完成此前更新，请在维护窗口内安排。</span></div>`;
  h += `<div class="kv mb12">` +
    kvRow("上次检查", st.last_check ? fmtTime(st.last_check) + ` <span class="txt-faint">${ago(st.last_check)}</span>` : "尚未检查") +
    kvRow("可升级包", `<span class="mono">${pkgs.length}</span>`) + `</div>`;
  if (OSC.busy) h += `<div class="waiting"><span class="w-pulse"></span><div><div class="w-txt">${esc(CMD_ZH[OSC.cmdType] || "命令")}执行中…</div><div class="w-sub">等待 Agent 回报结果，请勿关闭抽屉</div></div><span class="spin">${icon("refresh", 16)}</span></div>`;
  if (!OSC.busy && OSC.result) {
    h += `<div class="banner ${OSC.result === "ok" ? "ok" : "err"}">${icon(OSC.result === "ok" ? "check" : "alert", 15)}<span>${esc(CMD_ZH[OSC.cmdType] || "命令")}${OSC.result === "ok" ? "已完成" : "执行失败"}</span></div>`;
    if (OSC.log) h += `<div class="log-box mb12">${esc(OSC.log)}</div>`;
  }
  h += `<div class="d-sec"><span class="lbl">可升级软件包</span>`;
  if (!pkgs.length) h += empty("check", st.last_check ? "系统已是最新，暂无可升级包" : "尚未检查更新，点击下方「检查更新」", "", true);
  else {
    const allSel = pkgs.every(p => OSC.sel.has(p.name));
    h += `<div class="tbl-wrap"><table class="tbl" style="min-width:0"><thead><tr><th style="width:34px"><label class="check"><input type="checkbox" data-change="os-pkg-all" ${allSel ? "checked" : ""} ${OSC.busy ? "disabled" : ""}><span class="box"></span></label></th><th>软件包</th><th>当前版本</th><th>候选版本</th></tr></thead><tbody>` +
      pkgs.map(p => `<tr><td><label class="check"><input type="checkbox" data-change="os-pkg" data-name="${esc(p.name)}" ${OSC.sel.has(p.name) ? "checked" : ""} ${OSC.busy ? "disabled" : ""}><span class="box"></span></label></td><td class="cell-mono">${esc(p.name)}</td><td class="cell-mono sub">${esc(p.current || "—")}</td><td class="cell-mono txt-ok">${esc(p.candidate || "—")}</td></tr>`).join("") +
      `</tbody></table></div>`;
  }
  h += `</div>`;
  return h;
}
function osFoot() {
  const n = OSC.sel.size;
  return `<button class="btn ghost" data-act="os-check" ${OSC.busy ? "disabled" : ""}>${icon("refresh", 15)} 检查更新</button><div class="spacer"></div>` +
    `<button class="btn primary" data-act="os-upgrade" ${OSC.busy || !n ? "disabled" : ""}>${icon("arrowUp", 15)} 升级所选 (${n})</button>`;
}
async function runOsCommand(type) {
  if (OSC.busy) return;
  const packages = type === "os-upgrade" ? Array.from(OSC.sel) : [];
  if (type === "os-upgrade" && !packages.length) { toast("请先勾选要升级的软件包", "warn"); return; }
  if (type === "os-upgrade") {
    const ok = await confirmDlg({ title: "确认系统升级", message: `将在 ${OSC.id} 上升级 ${packages.length} 个软件包。开启播放保护的终端在播放中会拒绝升级。`, okText: "开始升级", danger: false });
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
    OSC.err = e.status === 409 ? "终端正在播放且开启了播放保护，已拒绝升级。请等待播放结束后重试。" : e.message;
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
      toast(`${CMD_ZH[OSC.cmdType] || "命令"}${st === "ok" ? "完成" : "失败"}`, st === "ok" ? "ok" : "err");
      refreshCurrent();
    } else if (OSC.tries > 160) {
      stopPoll("os:cmd"); OSC.busy = false;
      OSC.err = "等待超时：Agent 尚未回报结果，可稍后重新打开查看。"; repaintOs();
    }
  } catch (e) { /* 瞬时错误，继续轮询 */ }
}

/* ================= NETDIAG ================= */
const NETC = { id: "", state: null, busy: false, cmdId: "", tries: 0, err: "" };
async function openNetDrawer(id) {
  NETC.id = id; NETC.state = null; NETC.busy = false; NETC.cmdId = ""; NETC.tries = 0; NETC.err = "";
  openDrawer("网络自检", esc(id), empty("wifi", "正在读取诊断结果…", "", true), "", { ctx: { kind: "net", id }, back: { act: "node-open", id } });
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
    kvRow("上次自检", st.at ? fmtTime(st.at) + ` <span class="txt-faint">${ago(st.at)}</span>` : "尚未执行") +
    kvRow("探测项", `<span class="mono">${rs.length}</span>`) + `</div>`;
  if (NETC.busy) h += `<div class="waiting"><span class="w-pulse"></span><div><div class="w-txt">网络自检执行中…</div><div class="w-sub">Agent 正在探测网关、DNS、NTP 与服务器连通性</div></div><span class="spin">${icon("refresh", 16)}</span></div>`;
  h += `<div class="d-sec"><span class="lbl">探测结果</span>`;
  if (!rs.length) h += empty("wifi", "暂无诊断结果，点击下方「开始自检」", "", true);
  else {
    h += `<div class="tbl-wrap"><table class="tbl" style="min-width:0"><thead><tr><th>探测项</th><th>结果</th><th>耗时</th><th>详情</th></tr></thead><tbody>` +
      rs.map(r => `<tr><td>${esc(r.name || "—")}</td><td>${r.ok ? badge("通过", "ok") : badge("失败", "err")}</td><td class="cell-mono">${r.ms != null ? r.ms + " ms" : "—"}</td><td class="sub">${esc(r.detail || "—")}</td></tr>`).join("") +
      `</tbody></table></div>`;
  }
  h += `</div>`;
  return h;
}
function netFoot() {
  return `<div class="hint">自检由 Agent 执行，通常数秒内完成。</div><div class="spacer"></div>` +
    `<button class="btn cyan" data-act="net-run" ${NETC.busy ? "disabled" : ""}>${icon("activity", 15)} 开始自检</button>`;
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
        NETC.err = st === "err" ? ((c && c.log) || "自检执行失败") : "";
      } catch (e) { NETC.err = e.message; }
      repaintNet();
      toast(st === "ok" ? "网络自检完成" : "网络自检失败", st === "ok" ? "ok" : "err");
    } else if (NETC.tries > 160) {
      stopPoll("net:cmd"); NETC.busy = false; NETC.err = "等待超时：Agent 尚未回报结果。"; repaintNet();
    }
  } catch (e) { /* 瞬时错误，继续轮询 */ }
}

/* ================= CONSOLE ================= */
function openConsole(id) {
  const url = `${API_BASE}/nodes/${enc(id)}/console/`;
  openModal(`终端控制台 · ${esc(shortId(id))}`,
    `<div class="banner info mb12">${icon("terminal", 15)}<span>控制台由 Agent 本地服务代理提供；若终端离线或 Agent 未启用控制台，画面将不可用。</span></div>` +
    `<iframe class="console-frame" src="${url}" title="终端控制台" sandbox="allow-scripts allow-same-origin allow-forms"></iframe>`,
    `<a class="btn ghost" href="${url}" target="_blank" rel="noopener">${icon("external", 15)} 新标签页打开</a><div class="spacer"></div>` +
    `<button class="btn primary" data-act="modal-close">关闭</button>`);
}

/* ================= NODE CONFIG ================= */
const CFG = { id: "", node: null, boms: [], license: null, apps: [], nodes: [], offline: 45, busy: false };
async function openConfig(id) {
  Object.assign(CFG, { id, node: null, boms: [], license: null, apps: [], nodes: [], offline: 45, busy: false });
  openModal(`${icon("sliders", 18)} 终端配置 · ${esc(shortId(id))}`, empty("refresh", "正在加载配置…", "", true), "");
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
  const bomOpts = [`<option value="" ${!cur ? "selected" : ""}>不绑定期望态</option>`]
    .concat(pub.map(b => { const v = `${b.bom_id}@${b.version}`; return `<option value="${esc(v)}" ${v === cur ? "selected" : ""}>${esc(v)} · ${esc(b.role || "默认")}</option>`; }));
  if (cur && !pub.some(b => `${b.bom_id}@${b.version}` === cur)) bomOpts.push(`<option value="${esc(cur)}" selected>${esc(cur)}（当前绑定）</option>`);
  return `
  <div class="m-sec"><span class="lbl">基本信息</span>
    <div class="f-row">
      <div class="field"><label>别名 *</label><input id="cfg-alias" class="inp" maxlength="60" value="${esc(n.alias || "")}"></div>
      <div class="field"><label>分组</label><input id="cfg-group" class="inp" maxlength="40" value="${esc(n.group || "")}"></div>
    </div>
    <div class="field"><label>备注</label><input id="cfg-note" class="inp" maxlength="200" value="${esc(n.note || "")}"></div>
    <div class="field"><label>离线判定阈值</label>
      <div class="stepper">
        <button class="step-btn" data-act="cfg-off" data-d="-15">−15</button>
        <span class="inp" id="cfg-off-val" style="display:inline-flex;align-items:center;justify-content:center">${CFG.offline}s</span>
        <button class="step-btn" data-act="cfg-off" data-d="15">+15</button>
      </div>
      <div class="f-hint">超过该时长未收到心跳即判定离线（15s – 86400s，默认 45s）。</div>
    </div>
  </div>
  <div class="m-sec"><span class="lbl">期望态 BOM</span>
    <div class="field"><select id="cfg-bom" class="inp" data-change="cfg-bom">${bomOpts.join("")}</select></div>
    <div id="cfg-drift"></div>
  </div>
  <div class="m-sec"><span class="lbl">DAC 仲裁</span>
    <div class="f-row">
      <div class="field"><label>仲裁模式</label>
        <select id="cfg-dac" class="inp" data-change="cfg-dac">
          <option value="auto" ${n.dac_arbitration !== "manual" ? "selected" : ""}>自动（播放保护仲裁）</option>
          <option value="manual" ${n.dac_arbitration === "manual" ? "selected" : ""}>手动指定设备</option>
        </select>
      </div>
      <div class="field"><label>DAC 设备</label><input id="cfg-dacdev" class="inp mono" placeholder="如 hw:1,0" value="${esc(n.dac_device || "")}"></div>
    </div>
    <div id="cfg-dac-hint" class="f-hint"></div>
  </div>
  <div class="m-sec"><span class="lbl">保护与系统更新</span>
    <div class="f-row mb12">
      <label class="switch"><input type="checkbox" id="cfg-pp" ${n.play_protect ? "checked" : ""}><span class="track"></span><span class="sw-lbl">播放保护（播放中拒绝升级/重启）</span></label>
      <label class="switch"><input type="checkbox" id="cfg-aos" ${n.auto_os_update ? "checked" : ""}><span class="track"></span><span class="sw-lbl">自动系统更新</span></label>
    </div>
    <div class="field"><label>更新窗口</label><input id="cfg-win" class="inp mono" placeholder="如 02:00-05:00" value="${esc(n.os_window || "")}"><div class="f-hint">自动更新仅在该时间窗口内执行，留空表示不限制。</div></div>
  </div>
  <div class="m-sec"><span class="lbl">授权与席位</span><div id="cfg-seat"></div></div>
  <div class="m-sec"><span class="lbl" style="color:var(--red)">危险区</span>
    <div class="banner err">${icon("alert", 15)}<span>注销终端将删除其注册记录与期望态绑定；播放中的终端会被拒绝。可在注销后的提示中撤销。</span></div>
    <button class="btn danger" data-act="cfg-delete" data-id="${esc(n.node_id)}">${icon("trash", 15)} 注销该终端</button>
  </div>`;
}
function cfgFoot() {
  return `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div>` +
    `<button class="btn primary" data-act="cfg-save" ${CFG.busy ? "disabled" : ""}>${icon("check", 15)} 保存配置</button>`;
}
function paintCfgDrift() {
  const el = $("#cfg-drift"); if (!el) return;
  const sel = val("cfg-bom");
  if (!sel) { el.innerHTML = `<div class="f-hint">未绑定期望态：终端保持当前安装状态，不会被自动纠偏。</div>`; return; }
  const at = sel.lastIndexOf("@");
  const bid = sel.slice(0, at), bver = Number(sel.slice(at + 1)) || 0;
  const bom = CFG.boms.find(b => b.bom_id === bid && b.version === bver);
  const n = CFG.node;
  let h = "";
  if (n.bom_id === bid && (n.bom_version || 0) === bver) h += `<div class="banner ok">${icon("check", 15)}<span>与当前绑定期望态一致。</span></div>`;
  else if (n.bom_id) h += `<div class="banner warn">${icon("alert", 15)}<span>期望态将由 <span class="mono">${esc(n.bom_id)}@${n.bom_version || 0}</span> 切换为 <span class="mono">${esc(sel)}</span>，保存后可在「服务下发」执行纠偏。</span></div>`;
  else h += `<div class="banner info">${icon("info", 15)}<span>保存后绑定 <span class="mono">${esc(sel)}</span>，可在「服务下发」执行部署。</span></div>`;
  if (bom) {
    const want = new Map((bom.apps || []).map(a => [a.app_id, a.version || ""]));
    const have = new Map(CFG.apps.map(a => [a.app_id || a.id, a.version || ""]));
    const missing = [], mismatch = [], extra = [];
    for (const [id, v] of want) {
      if (!have.has(id)) missing.push(id + (v ? "@" + v : ""));
      else if (v && have.get(id) !== v) mismatch.push(`${id} ${have.get(id)} → ${v}`);
    }
    for (const [id] of have) if (!want.has(id)) extra.push(id);
    if (!missing.length && !mismatch.length && !extra.length) h += `<div class="f-hint txt-ok">已安装应用与该 BOM 完全一致，无漂移。</div>`;
    else {
      h += `<div class="f-hint">检测到漂移：</div><div class="kv mt8">`;
      if (missing.length) h += kvRow("缺失", missing.map(x => `<span class="mono txt-warn">${esc(x)}</span>`).join("、"));
      if (mismatch.length) h += kvRow("版本不符", mismatch.map(x => `<span class="mono txt-warn">${esc(x)}</span>`).join("、"));
      if (extra.length) h += kvRow("多余", extra.map(x => `<span class="mono txt-info">${esc(x)}</span>`).join("、"));
      h += `</div>`;
    }
  }
  el.innerHTML = h;
}
function paintCfgDacHint() {
  const el = $("#cfg-dac-hint"); if (!el) return;
  const manual = val("cfg-dac") === "manual";
  const dev = $("#cfg-dacdev"); if (dev) dev.disabled = !manual;
  el.textContent = manual ? "手动模式：固定使用指定 ALSA 设备，播放保护不再自动切换 DAC。" : "自动模式：由播放保护按播放会话自动仲裁 DAC 占用，设备字段仅作为记录。";
}
function paintCfgSeat() {
  const el = $("#cfg-seat"); if (!el) return;
  const n = CFG.node, lic = CFG.license;
  const used = CFG.nodes.filter(x => x.authorized).length;
  const builtin = Number(lic && lic.builtin_seats) || 3;
  const seats = Number(lic && lic.effective_seats) || builtin;
  const licensed = lic && lic.status === "activated";
  let h = "";
  if (!lic || lic.status === "none") h += `<div class="banner info">${icon("shield", 15)}<span>内置授权：可授权 ${builtin} 台终端，无需许可证；激活许可证可扩容。</span><button class="btn violet xs" data-act="goto-keys">扩容授权</button></div>`;
  else if (lic.status === "expired") h += `<div class="banner warn">${icon("alert", 15)}<span>许可证已过期，已回退内置授权（${builtin} 台终端）。</span><button class="btn violet xs" data-act="goto-keys">前往续期</button></div>`;
  const pct = clamp(used / seats * 100, 0, 100);
  h += `<div class="seats"><div class="s-top"><span>已授权席位${licensed ? "" : "（内置）"}</span><span class="mono">${used} / ${seats}</span></div><div class="seat-bar"><div class="seat-fill ${used > seats ? "over" : ""}" style="width:${pct}%"></div></div></div>`;
  if (used >= seats && !n.authorized) h += `<div class="f-hint txt-warn">席位已满，授权该终端前请先释放其他席位${licensed ? "" : "或激活许可证扩容"}。</div>`;
  h += `<div class="btn-row mt8">` +
    (n.authorized
      ? `${authBadge(n)}<button class="btn ghost sm" data-act="cfg-revoke" data-id="${esc(n.node_id)}">${icon("lock", 14)} 撤销授权</button>`
      : `${authBadge(n)}<button class="btn primary sm" data-act="cfg-auth" data-id="${esc(n.node_id)}">${icon("unlock", 14)} 授权该终端</button>`) +
    `</div>`;
  el.innerHTML = h;
}
function cfgOffStep(d) {
  CFG.offline = clamp(CFG.offline + d, 15, 86400);
  const el = $("#cfg-off-val"); if (el) el.textContent = CFG.offline + "s";
}
async function cfgAuth(revoke) {
  const id = CFG.id;
  try {
    await api("POST", `/nodes/${enc(id)}/${revoke ? "revoke" : "authorize"}`);
    CFG.node.authorized = !revoke;
    if (!revoke) CFG.node.authorized_at = Math.floor(Date.now() / 1000);
    paintCfgSeat(); toast(revoke ? "已撤销授权" : "已授权该终端", "ok");
    refreshCurrent();
  } catch (e) { toast(e.message, "err"); }
}
async function cfgSave() {
  if (CFG.busy) return;
  const alias = val("cfg-alias").trim();
  if (!alias) { toast("别名不能为空", "warn"); const el = $("#cfg-alias"); if (el) el.focus(); return; }
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
    toast("配置已保存", "ok");
    const id = CFG.id;
    closeModal();
    if (drawerCtx && drawerCtx.kind === "node" && drawerCtx.id === id) openNodeDrawer(id);
    await refreshCurrent();
  } catch (e) { toast(e.message, "err"); }
  finally { CFG.busy = false; if ($(".modal")) setModalFoot(cfgFoot()); }
}
async function cfgDelete() {
  const n = CFG.node; if (!n) return;
  const ok = await confirmDlg({ title: "注销终端", message: `将注销「${n.alias || n.node_id}」。播放中的终端会被拒绝，注销后可撤销。`, okText: "注销" });
  if (!ok) return;
  await deleteNodes([n.node_id]);
}

/* ================= CATALOG ================= */
function shellCatalog() {
  return `<section class="card stagger" style="--i:0">
      ${secHead(`${icon("box", 17)} 应用目录`, `<button class="btn primary sm" data-act="app-create">${icon("plus", 14)} 注册应用</button>`)}
      <div id="cat-apps"></div>
    </section>
    <div id="bom-report"></div>
    <section class="card mt16 stagger" style="--i:1">
      ${secHead(`${icon("layers", 17)} BOM 期望态`, `<button class="btn ghost sm" data-act="bom-import">${icon("upload", 14)} 导入</button><button class="btn primary sm" data-act="bom-new">${icon("plus", 14)} 新建 BOM</button>`)}
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
    el.innerHTML = empty("box", S.apps.length ? "没有匹配的应用" : "仓库为空，先注册一个应用（如 shairport-sync）",
      S.apps.length ? "" : `<button class="btn primary" data-act="app-create">${icon("plus", 15)} 注册应用</button>`);
    return;
  }
  el.innerHTML = `<div class="grid cols-3">` + list.map((a, i) => `
    <div class="card lift app-card stagger" style="--i:${i % 9}" data-act="app-open" data-id="${esc(a.app_id)}" role="button" tabindex="0">
      <div class="app-top"><span class="app-name">${esc(a.name || a.app_id)}</span>${catBadge(a.category)}</div>
      <div class="app-meta"><span class="tag-mono">${esc(a.app_id)}</span>${a.self_build_required ? badge("需自建", "warn") : ""}${a.maintainer_state === "inactive" ? badge("维护停止", "err") : ""}</div>
      <div class="app-meta mt8">${icon("globe", 13)} ${esc(a.upstream || "—")} · ${esc(a.license || "—")}</div>
      <div class="app-meta mt8">渠道 <span class="mono">${esc(a.default_channel || "stable")}</span><div class="spacer"></div><span class="txt-faint">管理版本 ${icon("chevR", 13)}</span></div>
    </div>`).join("") + `</div>`;
}
const APPV = { id: "", app: null, versions: [], busy: false };
async function openAppModal(id) {
  APPV.id = id; APPV.app = null; APPV.versions = []; APPV.busy = false;
  openModal(`${icon("box", 18)} ${esc(shortId(id))}`, empty("refresh", "正在加载版本…", "", true), "");
  try {
    const [app, vs] = await Promise.all([api("GET", `/apps/${enc(id)}`), api("GET", `/apps/${enc(id)}/versions`).catch(() => [])]);
    APPV.app = app; APPV.versions = Array.isArray(vs) ? vs : [];
  } catch (e) { closeModal(); toast(e.message, "err"); return; }
  if (!$(".modal") || APPV.id !== id) return;
  setModalBody(appModalBody());
  setModalFoot(`<button class="btn ghost" data-act="app-upload" data-id="${esc(id)}">${icon("upload", 15)} 上传新版本</button><button class="btn ghost" data-act="app-import-local" data-id="${esc(id)}">${icon("download", 15)} 本地包入库</button><div class="spacer"></div><button class="btn primary" data-act="modal-close">关闭</button>`);
}
function appModalBody() {
  const a = APPV.app || {};
  let h = `<div class="kv mb12">` +
    kvRow("应用 ID", `<span class="mono">${esc(a.app_id || APPV.id)}</span>`) +
    kvRow("名称", esc(a.name || "—")) +
    kvRow("分类", catBadge(a.category)) +
    kvRow("上游", esc(a.upstream || "—")) +
    kvRow("许可", esc(a.license || "—")) +
    kvRow("默认渠道", `<span class="mono">${esc(a.default_channel || "stable")}</span>`) +
    kvRow("维护状态", a.maintainer_state === "inactive" ? badge("停止", "err") : badge("活跃", "ok")) +
    `</div><div class="divider"></div><span class="lbl">版本与构件 (${APPV.versions.length})</span>`;
  if (!APPV.versions.length) return h + empty("upload", "尚无版本，上传第一个构件", `<button class="btn primary sm" data-act="app-upload" data-id="${esc(APPV.id)}">${icon("upload", 14)} 上传版本</button>`, true);
  h += `<div class="tbl-wrap mt8"><table class="tbl"><thead><tr><th>版本</th><th>架构</th><th>签名</th><th>生命周期</th><th>SHA256</th><th>大小</th><th>操作</th></tr></thead><tbody>` +
    APPV.versions.map(v => `<tr>
      <td class="cell-mono">${esc(v.version)}<div class="sub">${esc(v.format || "")}</div></td>
      <td class="cell-mono">${esc(v.arch || "—")}</td>
      <td>${v.signed ? badge("已签名", "ok") : badge("未签名", "warn")}${v.banned ? " " + badge("已封禁", "err") : ""}</td>
      <td><select class="inp" style="width:104px;padding:5px 26px 5px 8px;font-size:12px" data-change="ver-lc" data-id="${esc(APPV.id)}" data-ver="${esc(v.version)}" data-arch="${esc(v.arch)}">
        ${["available", "deprecated", "revoked"].map(s => `<option value="${s}" ${(v.lifecycle || "available") === s ? "selected" : ""}>${({ available: "可用", deprecated: "弃用", revoked: "吊销" })[s]}</option>`).join("")}
      </select></td>
      <td class="cell-mono sub" title="${esc(v.sha256 || "")}">${esc((v.sha256 || "—").slice(0, 10))}</td>
      <td class="cell-mono sub">${fmtBytes(v.size)}</td>
      <td class="acts"><button class="btn ${v.banned ? "ghost" : "danger"} xs" data-act="ver-ban" data-id="${esc(APPV.id)}" data-ver="${esc(v.version)}" data-arch="${esc(v.arch)}" data-banned="${v.banned ? "0" : "1"}">${v.banned ? "解封" : "封禁"}</button></td>
    </tr>`).join("") + `</tbody></table></div>`;
  return h;
}

/* ---- local vendor package import ---- */
function openImportLocal(id) {
  openModal(`${icon("download", 18)} 本地包入库 · ${esc(shortId(id))}`, `
    <div id="imp-err"></div>
    <div class="field"><label>服务端本机路径 *</label><input id="imp-path" class="inp mono" placeholder="如 D:\\Download\\Roon\\…\\RoonBridge_linuxarmv8.tar.bz2"><div class="f-hint">安装包已在服务端主机上时直接入库，无需经浏览器上传；路径为服务端视角的绝对路径。</div></div>
    <div class="f-row">
      <div class="field"><label>版本 *</label><input id="imp-ver" class="inp mono" placeholder="如 2.70.1671 / 5.1.5-67"></div>
      <div class="field"><label>架构 *</label><select id="imp-arch" class="inp"><option value="arm64">arm64（Pi 4 / armv8）</option><option value="armhf">armhf（armv7）</option><option value="amd64">amd64（x64）</option></select></div>
    </div>
    <div class="f-row">
      <div class="field"><label>格式 *</label><select id="imp-fmt" class="inp"><option value="tar.bz2">tar.bz2（Roon Bridge）</option><option value="deb">deb（NAA 等）</option></select></div>
      <div class="field"><label>签名文件（.asc）</label><input id="imp-sig" class="inp mono" placeholder="enforce 模式必填，可选绝对路径"></div>
    </div>
    <div class="f-row">
      <div class="field"><label>systemd units</label><input id="imp-units" class="inp mono" placeholder="逗号分隔，如 roonbridge.service"></div>
      <div class="field"><label>OS 依赖包</label><input id="imp-deps" class="inp mono" placeholder="逗号分隔，如 libasound2"></div>
    </div>
    <div class="banner info">${icon("info", 15)}<span>入库与浏览器上传走同一条校验链：SHA256 完整性 + GPG detached 签名（enforce 模式拒绝未签名包）。tar.bz2 由终端解包到 /opt 并自动写入 systemd 单元。</span></div>`,
    `<button class="btn ghost" data-act="app-open" data-id="${esc(id)}">返回</button><div class="spacer"></div><button class="btn primary" data-act="app-import-local-submit" data-id="${esc(id)}">${icon("check", 15)} 入库</button>`, { narrow: true });
}
async function submitImportLocal(id) {
  const err = $("#imp-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const path = val("imp-path").trim(), ver = val("imp-ver").trim();
  if (!path) return fail("请填写服务端本机路径");
  if (!ver) return fail("请填写版本");
  try {
    const v = await api("POST", `/apps/${enc(id)}/versions/import-local`, {
      version: ver, arch: val("imp-arch"), format: val("imp-fmt"),
      local_path: path, signature_path: val("imp-sig").trim(),
      units: splitList(val("imp-units")), deps_pkgs: splitList(val("imp-deps"))
    });
    toast(`版本 ${v.version} (${v.arch}) 已入库`, "ok");
    openAppModal(id);
  } catch (e) { fail(e.message); }
}

/* ---- app create / upload ---- */
function openCreateApp() {
  openModal(`${icon("plus", 18)} 注册应用`, `
    <div id="ap-err"></div>
    <div class="f-row">
      <div class="field"><label>应用 ID *</label><input id="ap-id" class="inp mono" placeholder="如 shairport-sync" maxlength="64"><div class="f-hint">小写字母 / 数字 / . _ -，全局唯一</div></div>
      <div class="field"><label>名称 *</label><input id="ap-name" class="inp" placeholder="如 AirPlay 接收器" maxlength="80"></div>
    </div>
    <div class="f-row">
      <div class="field"><label>分类</label><select id="ap-cat" class="inp"><option value="renderer">渲染器</option><option value="dependency">依赖</option><option value="tool">工具</option></select></div>
      <div class="field"><label>默认渠道</label><select id="ap-channel" class="inp"><option value="stable">stable</option><option value="beta">beta</option><option value="nightly">nightly</option></select></div>
    </div>
    <div class="field"><label>上游地址</label><input id="ap-up" class="inp mono" placeholder="https://…"></div>
    <div class="field"><label>许可证</label><input id="ap-lic" class="inp" placeholder="如 MIT / GPL-2.0"></div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn primary" data-act="app-create-submit">${icon("check", 15)} 注册</button>`, { narrow: true });
}
async function submitCreateApp() {
  const err = $("#ap-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const id = val("ap-id").trim(), name = val("ap-name").trim();
  if (!/^[a-z0-9][a-z0-9._-]{0,63}$/.test(id)) return fail("应用 ID 需以小写字母或数字开头，仅含小写字母、数字、. _ -");
  if (!name) return fail("请填写应用名称");
  try {
    await api("POST", "/apps", { app_id: id, name, category: val("ap-cat"), upstream: val("ap-up").trim(), license: val("ap-lic").trim(), default_channel: val("ap-channel"), maintainer_state: "active" });
    toast(`应用 ${id} 已注册`, "ok");
    await refreshCatalog(); openAppModal(id);
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
const UP = { id: "", busy: false };
function openUpload(id) {
  UP.id = id; UP.busy = false;
  openModal(`${icon("upload", 18)} 上传版本 · ${esc(shortId(id))}`, `
    <div id="up-err"></div>
    <div class="f-row">
      <div class="field"><label>版本号 *</label><input id="up-ver" class="inp mono" placeholder="如 4.3.2" maxlength="40"></div>
      <div class="field"><label>架构 *</label><select id="up-arch" class="inp"><option>amd64</option><option>arm64</option><option>armhf</option><option>armv7hf</option></select></div>
    </div>
    <div class="f-row">
      <div class="field"><label>包格式</label><select id="up-format" class="inp"><option value="deb">deb</option><option value="rpm">rpm</option><option value="tar">tar 归档</option><option value="binary">裸二进制</option></select></div>
      <div class="field"><label>生命周期</label><select id="up-lc" class="inp"><option value="available">可用</option><option value="deprecated">弃用</option></select></div>
    </div>
    <div class="field"><label>构件文件 *</label><input id="up-file" type="file" class="inp"></div>
    <div class="field"><label>签名文件（.asc，可选）</label><input id="up-sig" type="file" class="inp" accept=".asc,.sig,.gpg"></div>
    <div class="field"><label>能力标签（逗号分隔）</label><input id="up-caps" class="inp mono" placeholder="airplay, raop"></div>
    <div class="f-row">
      <div class="field"><label>systemd 单元（逗号分隔）</label><input id="up-units" class="inp mono" placeholder="shairport-sync.service"></div>
      <div class="field"><label>系统依赖包（逗号分隔）</label><input id="up-deps" class="inp mono" placeholder="libasound2, avahi-daemon"></div>
    </div>
    <div class="f-row mb12">
      <label class="switch"><input type="checkbox" id="up-dac"><span class="track"></span><span class="sw-lbl">占用 DAC</span></label>
      <label class="switch"><input type="checkbox" id="up-ap2"><span class="track"></span><span class="sw-lbl">AirPlay 2 支持</span></label>
    </div>
    <div class="banner info">${icon("shield", 15)}<span>签名强制模式下，未签名构件可能被仓库拒绝；建议同时上传 detached 签名。</span></div>`,
    `<button class="btn ghost" data-act="app-open" data-id="${esc(id)}">${icon("chevL", 14)} 返回</button><div class="spacer"></div><button class="btn primary" data-act="up-submit" id="up-btn">${icon("upload", 15)} 上传</button>`, { narrow: true });
}
async function submitVersion() {
  if (UP.busy) return;
  const err = $("#up-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const version = val("up-ver").trim(), arch = val("up-arch");
  const f = $("#up-file"), file = f && f.files && f.files[0];
  if (!version) return fail("请填写版本号");
  if (!file) return fail("请选择构件文件");
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
  const btn = $("#up-btn"); if (btn) { btn.disabled = true; btn.innerHTML = `<span class="spin">${icon("refresh", 15)}</span> 上传中…`; }
  try {
    await apiForm(`/apps/${enc(UP.id)}/versions`, fd);
    toast(`版本 ${version}（${arch}）已上传`, "ok");
    const id = UP.id; UP.busy = false;
    await refreshCatalog(); openAppModal(id);
  } catch (e) {
    UP.busy = false; fail(e.message); toast(e.message, "err");
    if (btn) { btn.disabled = false; btn.innerHTML = `${icon("upload", 15)} 上传`; }
  }
}
async function verBan(ds) {
  const id = ds.id, ver = ds.ver, arch = ds.arch, banned = ds.banned === "1";
  if (banned) {
    const ok = await confirmDlg({ title: "封禁版本", message: `封禁 ${id}@${ver}（${arch}）后，该版本将不再被解析与下发。`, okText: "封禁" });
    if (!ok) return;
  }
  try {
    await api("POST", `/apps/${enc(id)}/versions/${enc(ver)}/${enc(arch)}/ban`, { banned });
    const v = APPV.versions.find(x => x.version === ver && x.arch === arch); if (v) v.banned = banned;
    if ($(".modal") && APPV.id === id) setModalBody(appModalBody());
    toast(banned ? "已封禁该版本" : "已解封该版本", "ok");
  } catch (e) { toast(e.message, "err"); }
}
async function verLifecycle(ds, lc) {
  const id = ds.id, ver = ds.ver, arch = ds.arch;
  try {
    await api("POST", `/apps/${enc(id)}/versions/${enc(ver)}/${enc(arch)}/lifecycle`, { lifecycle: lc });
    const v = APPV.versions.find(x => x.version === ver && x.arch === arch); if (v) v.lifecycle = lc;
    toast(`生命周期已更新为「${({ available: "可用", deprecated: "弃用", revoked: "吊销" })[lc] || lc}」`, "ok");
  } catch (e) { toast(e.message, "err"); if ($(".modal") && APPV.id === id) setModalBody(appModalBody()); }
}

/* ================= BOM ================= */
function paintBoms() {
  const el = $("#cat-boms"); if (!el) return;
  const list = S.boms.filter(b => hit(b.bom_id, b.role, b.status)).sort((a, b) => a.bom_id === b.bom_id ? b.version - a.version : String(a.bom_id).localeCompare(String(b.bom_id)));
  if (!list.length) {
    el.innerHTML = empty("layers", S.boms.length ? "没有匹配的 BOM" : "暂无 BOM，新建一个期望态清单",
      S.boms.length ? "" : `<button class="btn primary" data-act="bom-new">${icon("plus", 15)} 新建 BOM</button>`);
    return;
  }
  el.innerHTML = `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>BOM</th><th>角色</th><th>应用</th><th>状态</th><th>更新时间</th><th>操作</th></tr></thead><tbody>` +
    list.map(b => `<tr>
      <td class="cell-mono">${esc(b.bom_id)}<span class="txt-faint">@</span><span class="txt-info">${b.version}</span></td>
      <td>${esc(b.role || "—")}</td>
      <td class="cell-mono sub">${(b.apps || []).length} 个</td>
      <td>${b.status === "published" ? badge("已发布", "ok") : badge("草稿", "warn")}</td>
      <td class="sub nowrap">${ago(b.updated_at || b.created_at)}</td>
      <td class="acts">
        <button class="btn ghost xs" data-act="bom-edit" data-id="${esc(b.bom_id)}" data-ver="${b.version}">${icon("edit", 12)} 编辑</button>
        <button class="btn ghost xs" data-act="bom-validate" data-id="${esc(b.bom_id)}" data-ver="${b.version}">校验</button>
        ${b.status !== "published" ? `<button class="btn primary xs" data-act="bom-publish" data-id="${esc(b.bom_id)}" data-ver="${b.version}">发布</button>` : ""}
        <button class="btn ghost xs" data-act="bom-history" data-id="${esc(b.bom_id)}" title="版本历史">${icon("audit", 12)} 历史</button>
        <button class="btn ghost xs" data-act="bom-export" data-id="${esc(b.bom_id)}" data-ver="${b.version}" title="导出 JSON">${icon("download", 12)}</button>
        <button class="btn ghost xs" data-act="bom-rollback" data-id="${esc(b.bom_id)}" data-ver="${b.version}" title="回滚到此版本">${icon("undo", 12)}</button>
      </td></tr>`).join("") + `</tbody></table></div>`;
}
const BOMH = { id: "", versions: [] };
async function openBomHistory(id) {
  BOMH.id = id; BOMH.versions = [];
  openModal(`${icon("audit", 18)} 版本历史 · ${esc(id)}`, empty("refresh", "正在加载版本历史…", "", true),
    `<div class="spacer"></div><button class="btn primary" data-act="modal-close">关闭</button>`);
  try {
    const vs = await api("GET", `/boms/${enc(id)}/versions`);
    BOMH.versions = Array.isArray(vs) ? vs : [];
  } catch (e) { closeModal(); toast(e.message, "err"); return; }
  if (!$(".modal") || BOMH.id !== id) return;
  setModalBody(bomHistoryHtml());
}
function bomHistoryHtml() {
  const vs = BOMH.versions;
  if (!vs.length) return empty("layers", "暂无历史版本");
  return `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>版本</th><th>状态</th><th>应用</th><th>更新时间</th><th style="text-align:right">操作</th></tr></thead><tbody>` +
    vs.map(b => `<tr>
      <td class="cell-mono">v<span class="txt-info">${b.version}</span></td>
      <td>${b.status === "published" ? badge("已发布", "ok") : badge("草稿", "warn")}</td>
      <td class="cell-mono sub">${(b.apps || []).length} 个</td>
      <td class="sub nowrap">${ago(b.updated_at || b.created_at)}</td>
      <td class="acts">
        <button class="btn ghost xs" data-act="bom-hist" data-do="edit" data-id="${esc(b.bom_id)}" data-ver="${b.version}">${icon("edit", 12)} 编辑</button>
        <button class="btn ghost xs" data-act="bom-hist" data-do="validate" data-id="${esc(b.bom_id)}" data-ver="${b.version}">校验</button>
        ${b.status !== "published" ? `<button class="btn primary xs" data-act="bom-hist" data-do="publish" data-id="${esc(b.bom_id)}" data-ver="${b.version}">发布</button>` : ""}
        <button class="btn ghost xs" data-act="bom-hist" data-do="export" data-id="${esc(b.bom_id)}" data-ver="${b.version}" title="导出 JSON">${icon("download", 12)}</button>
        <button class="btn ghost xs" data-act="bom-hist" data-do="rollback" data-id="${esc(b.bom_id)}" data-ver="${b.version}" title="回滚到此版本">${icon("undo", 12)} 回滚</button>
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
  if (!errs.length && !warns.length) h += `<div class="line ok">${icon("check", 14)}<span>校验通过：无错误、无警告。</span></div>`;
  errs.forEach(x => h += `<div class="line err">${icon("alert", 14)}<span>${esc(x)}</span></div>`);
  warns.forEach(x => h += `<div class="line warn">${icon("info", 14)}<span>${esc(x)}</span></div>`);
  return h + `</div>`;
}
function showBomReport(title, rep) {
  const el = $("#bom-report");
  if (el) el.innerHTML = `<section class="card mt16"><div class="sec-head"><div class="sec-title">${icon("shield", 16)} ${esc(title)}</div><div class="spacer"></div><button class="icon-btn" data-act="bom-report-close" aria-label="关闭">${icon("x", 16)}</button></div>${reportHtml(rep)}</section>`;
  else openModal(`${icon("shield", 18)} ${esc(title)}`, reportHtml(rep), `<div class="spacer"></div><button class="btn primary" data-act="modal-close">关闭</button>`, { narrow: true });
}
async function bomValidate(ds) {
  try {
    const rep = await api("POST", `/boms/${enc(ds.id)}/versions/${enc(ds.ver)}/validate?arch=amd64`);
    showBomReport(`校验 ${ds.id}@${ds.ver}`, rep);
    toast((rep && rep.errors && rep.errors.length) ? "校验发现错误" : "校验通过", (rep && rep.errors && rep.errors.length) ? "warn" : "ok");
  } catch (e) { toast(e.message, "err"); }
}
async function bomPublish(ds) {
  const ok = await confirmDlg({ title: "发布 BOM", message: `发布 ${ds.id}@${ds.ver} 后即可绑定终端并下发。发布前将执行完整校验（amd64）。`, okText: "发布", danger: false });
  if (!ok) return;
  try {
    const r = await api("POST", `/boms/${enc(ds.id)}/versions/${enc(ds.ver)}/publish?arch=amd64`);
    toast(`${ds.id}@${ds.ver} 已发布`, "ok");
    showBomReport(`发布 ${ds.id}@${ds.ver}`, r && r.report);
    await refreshCatalog();
  } catch (e) {
    toast(e.message, "err");
    if (e.data && e.data.report) showBomReport(`发布失败 ${ds.id}@${ds.ver}`, e.data.report);
  }
}
function openBomImport() {
  openModal(`${icon("upload", 18)} 导入 BOM`, `
    <div id="bi-err"></div>
    <div class="field"><label>从文件读取</label><input id="bi-file" type="file" class="inp" data-change="bi-file" accept=".json,application/json"></div>
    <div class="field"><label>BOM JSON *</label><textarea id="bi-json" class="inp" rows="12" placeholder='{"bom_id":"hifi-default","role":"renderer","apps":[{"app_id":"shairport-sync","version":"4.3.2"}]}'></textarea><div class="f-hint">支持裸 BOM 文档或 {"bom":{…}} 包装；导入后保存为新的草稿版本。</div></div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn primary" data-act="bom-import-submit">${icon("upload", 15)} 导入</button>`, { narrow: true });
}
async function submitBomImport() {
  const err = $("#bi-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  let obj;
  try { obj = JSON.parse(val("bi-json")); } catch (e) { return fail("JSON 解析失败：" + e.message); }
  const bom = obj && obj.bom && typeof obj.bom === "object" ? obj.bom : obj;
  if (!bom || !bom.bom_id) return fail("缺少 bom_id 字段");
  try {
    const b = await api("POST", "/boms/import", { bom });
    toast(`已导入 ${b.bom_id}@${b.version}（草稿）`, "ok");
    closeModal(); await refreshCatalog();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
function bomExport(ds) { download(`/boms/${enc(ds.id)}/versions/${enc(ds.ver)}/export`); toast("已开始导出 JSON", "info"); }
async function bomRollback(ds) {
  const ok = await confirmDlg({ title: "回滚 BOM", message: `将以 ${ds.id}@${ds.ver} 的内容生成一个新的已发布版本，终端随后收敛回该期望态。`, okText: "回滚并发布", danger: false });
  if (!ok) return;
  try {
    const b = await api("POST", `/boms/${enc(ds.id)}/rollback`, { version: Number(ds.ver) });
    toast(`已回滚：${ds.id}@${ds.ver} → 新版本 v${b && b.version}`, "ok");
    await refreshCatalog();
  } catch (e) { toast(e.message, "err"); }
}

/* ---- BOM editor ---- */
const BOMD = { id: "", isNew: true, version: 0, busy: false };
async function openBomEditor(id, ver) {
  Object.assign(BOMD, { id: id || "", isNew: !id, version: 0, busy: false });
  let b = null;
  if (id) {
    openModal(`${icon("layers", 18)} 编辑 BOM · ${esc(shortId(id))}`, empty("refresh", "正在加载 BOM…", "", true), "");
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
      <div class="field"><label>BOM ID *</label><input id="b-id" class="inp mono" maxlength="64" placeholder="如 hifi-default" value="${esc(b ? b.bom_id : "")}" ${b ? "disabled" : ""}></div>
      <div class="field"><label>角色</label><input id="b-role" class="inp" maxlength="40" placeholder="如 renderer / streamer" value="${esc(b ? b.role || "" : "")}"></div>
    </div>
    <div class="banner violet">${icon("info", 15)}<span>版本号由服务器自动递增${b ? `（当前 v${b.version}，保存将生成 v${b.version + 1} 草稿）` : "（首次保存为 v1 草稿）"}；发布后才可绑定与下发。</span></div>
    <div class="m-sec"><span class="lbl">应用清单</span>
      <datalist id="app-ids">${S.apps.map(a => `<option value="${esc(a.app_id)}">${esc(a.name || "")}</option>`).join("")}</datalist>
      <div id="bom-rows">${bomRowsHtml(rows)}</div>
      <button class="btn ghost sm" data-act="bom-row-add">${icon("plus", 13)} 添加应用</button>
    </div>
    <div class="m-sec"><span class="lbl">变量（JSON 对象）</span>
      <div class="field"><textarea id="b-vars" class="inp" rows="5">${esc(vars)}</textarea><div class="f-hint">键值将全部按字符串处理，用于配置模板渲染。</div></div>
    </div>
    <div class="m-sec"><span class="lbl">启用单元（逗号分隔）</span>
      <div class="field"><input id="b-units" class="inp mono" placeholder="shairport-sync.service, upmpdcli.service" value="${esc(units)}"></div>
    </div>
    <div id="bom-ed-report"></div>`;
  const foot = `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div>` +
    `<button class="btn ghost" data-act="bom-validate-ed">${icon("shield", 14)} 保存并校验</button>` +
    `<button class="btn ghost" data-act="bom-save-ed">${icon("check", 14)} 保存草稿</button>` +
    `<button class="btn primary" data-act="bom-publish-ed">${icon("send", 14)} 保存并发布</button>`;
  if (b) { setModalBody(body); setModalFoot(foot); }
  else openModal(`${icon("plus", 18)} 新建 BOM`, body, foot);
}
function bomRowsHtml(rows) {
  return rows.map((r, i) => `<div class="bom-row" data-i="${i}">
    <div style="display:flex;gap:8px;min-width:0">
      <input class="inp mono b-app" list="app-ids" placeholder="应用 ID" value="${esc(r.app_id)}" style="flex:1;min-width:0">
      <select class="inp b-ch" style="width:96px;flex:none">
        ${["", "stable", "beta", "nightly"].map(c => `<option value="${c}" ${r.channel === c ? "selected" : ""}>${c || "渠道"}</option>`).join("")}
      </select>
    </div>
    <input class="inp mono b-ver" placeholder="版本（留空=最新）" value="${esc(r.version)}">
    <button class="row-del" data-act="bom-row-del" data-i="${i}" title="移除" aria-label="移除">${icon("x", 14)}</button>
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
  if (!/^[a-z0-9][a-z0-9._-]{0,63}$/.test(id)) fail("BOM ID 需以小写字母或数字开头，仅含小写字母、数字、. _ -");
  const apps = collectBomRows();
  if (!apps.length) fail("请至少填写一个应用");
  let variables = {};
  const rawVars = val("b-vars").trim();
  if (rawVars) {
    let obj; try { obj = JSON.parse(rawVars); } catch (e) { fail("变量 JSON 解析失败：" + e.message); }
    if (!obj || typeof obj !== "object" || Array.isArray(obj)) fail("变量必须是 JSON 对象");
    for (const k of Object.keys(obj)) variables[k] = String(obj[k] ?? "");
  }
  const payload = { bom_id: id, role: val("b-role").trim(), apps, variables, units_enabled: splitList(val("b-units")) };
  const b = await api("POST", "/boms", payload);
  BOMD.id = b.bom_id; BOMD.version = b.version; BOMD.isNew = false;
  const idEl = $("#b-id"); if (idEl) { idEl.value = b.bom_id; idEl.disabled = true; }
  if (!silent) { toast(`已保存草稿 ${b.bom_id}@${b.version}`, "ok"); closeModal(); await refreshCatalog(); }
  return b;
}
async function bomValidateEd() {
  if (BOMD.busy) return; BOMD.busy = true;
  try {
    const b = await bomSave(true);
    const rep = await api("POST", `/boms/${enc(b.bom_id)}/versions/${b.version}/validate?arch=amd64`);
    const el = $("#bom-ed-report"); if (el) el.innerHTML = `<span class="lbl">校验报告 · v${b.version}</span>` + reportHtml(rep);
    toast((rep && rep.errors && rep.errors.length) ? `已保存 v${b.version}，校验发现错误` : `已保存 v${b.version}，校验通过`, (rep && rep.errors && rep.errors.length) ? "warn" : "ok");
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
      toast(`${b.bom_id}@${b.version} 已保存并发布`, "ok");
      closeModal(); await refreshCatalog();
      showBomReport(`发布 ${b.bom_id}@${b.version}`, r && r.report);
    } catch (e) {
      toast(e.message, "err");
      const el = $("#bom-ed-report");
      if (el) el.innerHTML = `<span class="lbl">发布失败 · v${b.version}</span>` + reportHtml(e.data && e.data.report);
      await refreshCatalog();
    }
  } catch (e) { toast(e.message, "err"); }
  finally { BOMD.busy = false; }
}

/* ================= APT SOURCE MANAGEMENT ================= */
function aptSourceRows() {
  const list = S.aptSources || [];
  if (!list.length) return empty("globe", "暂无第三方源，开源服务将直接使用发行版官方仓库", "", true);
  return list.map(s => `<div class="key-row">
    <div class="key-ic">${icon("globe", 16)}</div>
    <div class="key-info">
      <div class="key-name">${esc(s.name)} ${(s.distros || []).length ? s.distros.map(d => badge(d, "info")).join(" ") : badge("全部发行版", "info")}</div>
      <div class="key-fp">${esc(s.url)} · ${esc(s.components || "main")}</div>
      <div class="key-meta">${s.key_url ? "签名密钥 " + esc(s.key_url) : "无独立密钥（使用发行版信任链）"}</div>
    </div>
    <button class="row-del" data-act="apt-src-del" data-id="${esc(s.id)}" title="删除">${icon("trash", 15)}</button>
  </div>`).join("");
}
async function openAptSources() {
  openModal(`${icon("globe", 18)} 第三方 APT 安装源`, empty("refresh", "正在读取安装源…", "", true),
    `<button class="btn ghost" data-act="modal-close">关闭</button>`);
  const root = $("#modal-root .modal");
  try {
    const sources = await api("GET", "/apt-sources");
    if ($("#modal-root .modal") !== root) return;
    if (!Array.isArray(sources)) throw new Error("安装源响应格式无效");
    S.aptSources = sources;
    setModalBody(`
    <div id="apt-src-err"></div>
    <div id="apt-src-list">${aptSourceRows()}</div>
    <div class="f-row mt12">
      <div class="field"><label>名称 *</label><input id="apt-src-name" class="inp" maxlength="80" placeholder="如 upmpdcli 上游仓库"></div>
      <div class="field"><label>适用发行版代号</label><input id="apt-src-distros" class="inp mono" placeholder="bookworm,trixie（留空 = 全部）"></div>
    </div>
    <div class="field"><label>仓库 URL *</label><input id="apt-src-url" class="inp mono" placeholder="https://…/debian/"></div>
    <div class="f-row">
      <div class="field"><label>组件</label><input id="apt-src-comp" class="inp mono" placeholder="main（默认）"></div>
      <div class="field"><label>签名密钥 URL</label><input id="apt-src-key" class="inp mono" placeholder="https://…/key.gpg（可选）"></div>
    </div>
    <div class="banner info">${icon("info", 15)}<span>下发时按终端发行版代号过滤；Suite 由终端本地解析，密钥自动 dearmor 后以 signed-by 写入 sources.list.d。</span></div>`);
    setModalFoot(`<button class="btn ghost" data-act="modal-close">关闭</button><div class="spacer"></div><button class="btn primary" data-act="apt-src-add">${icon("plus", 15)} 添加源</button>`);
    settingsApplyAccess();
  } catch (e) {
    if ($("#modal-root .modal") !== root) return;
    setModalBody(empty("alert", "安装源加载失败：" + e.message, `<button class="btn" data-act="apt-sources">重试读取</button>`, true));
  }
}
async function submitAptSource() {
  const root = $("#modal-root .modal");
  const err = $("#apt-src-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const name = val("apt-src-name").trim(), url = val("apt-src-url").trim();
  if (!name) return fail("请填写名称");
  if (!/^https?:\/\//.test(url)) return fail("仓库 URL 必须以 http:// 或 https:// 开头");
  try {
    await api("POST", "/apt-sources", {
      name, url, components: val("apt-src-comp").trim(), key_url: val("apt-src-key").trim(),
      distros: splitList(val("apt-src-distros")).map(d => d.toLowerCase())
    });
    toast(`安装源 ${name} 已添加`, "ok");
    if ($("#modal-root .modal") === root) await openAptSources();
  } catch (e) { fail(e.message); }
}
async function aptSrcDel(id) {
  const root = $("#modal-root .modal"), list = $("#apt-src-list");
  const s = (S.aptSources || []).find(x => x.id === id);
  const ok = await confirmDlg({ title: "删除安装源", message: `将删除「${s ? s.name : id}」。已下发到终端的源文件不受影响，下次下发安装时不再携带该源。`, okText: "删除" });
  if (!ok) return;
  try {
    await api("DELETE", `/apt-sources/${enc(id)}`);
    toast("安装源已删除", "ok");
    const sources = await api("GET", "/apt-sources");
    if ($("#modal-root .modal") !== root || $("#apt-src-list") !== list) return;
    if (!Array.isArray(sources)) throw new Error("安装源响应格式无效");
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
  openDrawer(`任务 ${esc(shortId(id))}`, esc(id), empty("refresh", "正在读取任务…", "", true), "", {
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
    if (!j || j.id !== id) throw new Error("任务响应无效");
    setDrawerTitle(`任务 ${esc(shortId(id))}`, esc(j.bom_id || id));
    setDrawerBody(jobDrawerBody(j)); setDrawerFoot(jobDrawerFoot(j));
  } catch (e) {
    if (!current()) return;
    setDrawerBody(empty("alert", "任务状态读取失败（当前状态未知）：" + e.message, "", true));
    setDrawerFoot(`<button class="btn ghost" data-act="drawer-close">关闭</button>`);
  } finally { ctx.loading = false; }
}
function jobDrawerBody(j) {
  const st = jobState(j);
  let h = "";
  if (j.reason) h += `<div class="banner ${st.startsWith("failed") ? "err" : "warn"}">${icon("info", 15)}<span>${esc(j.reason)}</span></div>`;
  if (st === "waiting_idle") h += `<div class="banner info">${icon("clock", 15)}<span>播放保护生效：等待所有协议空闲后自动继续，服务不受影响。</span></div>`;
  h += `<div class="kv mb12">` +
    kvRow("任务 ID", `<span class="mono">${esc(j.id)}</span>`) +
    kvRow("终端", jobNodes(j).map(id => { const n = S.nodes.find(x => x.node_id === id); return `<button class="btn ghost xs" data-act="node-open" data-id="${esc(id)}">${esc(n ? (n.alias || shortId(id)) : shortId(id))}</button>`; }).join(" ") || "—") +
    kvRow("期望态", `<span class="mono">${esc(j.bom_id || "—")}${j.bom_version ? "@" + j.bom_version : ""}</span>`) +
    kvRow("类型", badge(JOB_ZH[j.kind] || j.kind || "部署", "violet")) +
    kvRow("状态", jobBadge(j)) +
    kvRow("创建", fmtTime(j.created_at)) +
    (j.started_at ? kvRow("开始", fmtTime(j.started_at)) : "") +
    (j.finished_at ? kvRow("结束", fmtTime(j.finished_at)) : "") +
    `</div>`;
  h += `<div class="d-sec"><span class="lbl">阶段时间线</span>`;
  const ph = j.phases || [];
  if (!ph.length) h += empty("clock", "等待预检启动…", "", true);
  else {
    h += `<div class="tl">` + ph.map(p => `<div class="tl-item">
      <span class="p-dot ${p.state === "ok" ? "ok" : p.state === "running" ? "running" : p.state === "failed" ? "failed" : p.state === "skipped" ? "skipped" : ""}"></span>
      <div class="p-body"><div class="p-name">${esc(JOB_ZH[p.name] || p.name)} ${phaseBadge(p.state)}</div>
      ${p.detail ? `<div class="p-detail">${esc(p.detail)}</div>` : ""}
      <div class="p-time">${p.started_at ? fmtTime(p.started_at) : ""}${p.finished_at ? " → " + fmtTime(p.finished_at) : ""}</div></div>
    </div>`).join("") + `</div>`;
  }
  h += `</div>`;
  return h;
}
function jobDrawerFoot(j) {
  const st = jobState(j);
  let h = "";
  if (st === "rollback" || st === "rollback_verify") h += `<button class="btn ghost" data-act="job-rb-complete" data-id="${esc(j.id)}">${icon("undo", 15)} 确认回滚完成</button>`;
  h += `<div class="spacer"></div>`;
  if (jobActive(j)) h += `<button class="btn primary" data-act="job-advance" data-id="${esc(j.id)}">${icon("chevR", 15)} ${st === "waiting_idle" ? "重试推进" : "推进任务"}</button>`;
  else h += `<button class="btn primary" data-act="drawer-close">关闭</button>`;
  return h;
}
async function jobAdvance(id) {
  try {
    const j = await api("POST", `/jobs/${enc(id)}/advance`);
    const st = jobState(j);
    toast(st === "waiting_idle" ? "终端仍在播放，任务继续等待空闲" : `任务已推进：${JOB_ZH[st] || st}`, st === "waiting_idle" ? "info" : "ok");
    await repaintJob(id); refreshTerminalCenter();
  } catch (e) { toast(e.message, "err"); }
}
async function jobRbComplete(id) {
  const ok = await confirmDlg({ title: "确认回滚完成", message: "确认终端已恢复到上一已知良好状态？任务将标记为「失败·已回滚」。", okText: "确认恢复", danger: false });
  if (!ok) return;
  try {
    await api("POST", `/jobs/${enc(id)}/rollback-complete`, { recovered: true });
    toast("已确认回滚完成", "ok");
    await repaintJob(id); refreshTerminalCenter();
  } catch (e) { toast(e.message, "err"); }
}

/* ================= ROLLOUTS ================= */
function ringStatusZh(s) { return ({ pending: "等待", running: "进行", done: "完成", failed: "失败" })[s] || s || "等待"; }
function paintRollouts() {
  const el = $("#dl-rollouts"); if (!el) return;
  if (!S.rollouts.length) { el.innerHTML = empty("branch", "暂无灰度发布，创建一个分环推进计划", `<button class="btn primary sm" data-act="ro-create">${icon("plus", 13)} 新建灰度</button>`, true); return; }
  el.innerHTML = S.rollouts.map(ro => {
    const rings = ro.rings || [];
    const done = ro.status === "done";
    const segs = rings.map((r, i) => {
      const cls = r.status === "done" ? "done" : r.status === "failed" ? "failed" : (i === ro.current_ring && !done ? "active" : "");
      const flex = clamp(r.percent || 1, 1, 100);
      return `<div class="ring-seg ${cls}" style="flex:${flex}" title="${esc(r.name)} · ${r.percent}% · ${ringStatusZh(r.status)}">
        <div class="r-name">${esc(r.name || "环 " + (i + 1))}</div>
        <div class="r-pct">${r.percent || 0}%</div>
        <div class="r-nodes">${(r.nodes || []).length} 台 · ${ringStatusZh(r.status)}</div>
      </div>`;
    }).join("");
    const canAdv = !done && ro.status !== "failed";
    return `<div class="rollout-card">
      <div class="ro-top">
        <span class="ro-name">${esc(ro.name || shortId(ro.id))}</span>
        ${done ? badge("已完成", "ok") : ro.status === "failed" ? badge("已失败", "err") : badge("进行中", "info")}
        <span class="tag-mono">${esc(ro.bom_id)}@${ro.bom_version || 0}</span>
        <div class="spacer"></div>
        ${canAdv ? `<button class="btn cyan xs" data-act="ro-advance" data-id="${esc(ro.id)}">${icon("chevR", 12)} 推进下一环</button>` : ""}
        <button class="btn ghost xs" data-act="ro-delete" data-id="${esc(ro.id)}">${icon("trash", 12)} 删除</button>
      </div>
      <div class="rings">${segs || '<span class="hint">无环</span>'}</div>
      <div class="hint">当前第 ${Math.min((ro.current_ring || 0) + 1, rings.length)} / ${rings.length} 环 · 创建于 ${ago(ro.created_at)}</div>
    </div>`;
  }).join("");
}
const ROD = { rings: [] };
function openRolloutCreate() {
  const pub = (S.pubBoms || []).slice().sort((a, b) => a.bom_id === b.bom_id ? b.version - a.version : String(a.bom_id).localeCompare(String(b.bom_id)));
  ROD.rings = [{ name: "金丝雀", percent: 10 }, { name: "第一环", percent: 40 }, { name: "全量", percent: 50 }];
  const bomOpts = pub.length ? pub.map(b => { const v = `${b.bom_id}@${b.version}`; return `<option value="${esc(v)}">${esc(v)} · ${esc(b.role || "默认")}</option>`; }).join("") : `<option value="">暂无已发布 BOM</option>`;
  openModal(`${icon("branch", 18)} 新建灰度发布`, `
    <div id="ro-err"></div>
    <div class="f-row">
      <div class="field"><label>名称 *</label><input id="ro-name" class="inp" maxlength="60" placeholder="如 AirPlay 4.3.2 灰度"></div>
      <div class="field"><label>期望态 BOM *</label><select id="ro-bom" class="inp">${bomOpts}</select></div>
    </div>
    <div class="banner info">${icon("info", 15)}<span>范围为全终端中的在线且已授权终端，与列表勾选无关；创建后立即启动首环任务，授权与播放保护门禁仍生效。上一环所有任务结束（含失败）后才能推进。百分比之和必须为 100%。</span></div>
    <div class="m-sec"><span class="lbl">发布环</span>
      <div id="ro-rings">${roRingsHtml()}</div>
      <div class="btn-row"><button class="btn ghost sm" data-act="ro-ring-add">${icon("plus", 13)} 添加环</button><div class="spacer"></div><span class="tag-mono" id="ro-sum"></span></div>
    </div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn primary" data-act="ro-submit" ${pub.length ? "" : "disabled"}>${icon("branch", 15)} 创建灰度</button>`, { narrow: true });
  paintRoSum();
}
function roRingsHtml() {
  return ROD.rings.map((r, i) => `<div class="ring-row" data-i="${i}">
    <input class="inp ro-name-i" placeholder="环名称" value="${esc(r.name)}" maxlength="30">
    <input class="inp mono ro-pct-i" type="number" min="1" max="100" data-change="ro-pct" data-i="${i}" value="${r.percent}" style="text-align:center">
    <button class="row-del" data-act="ro-ring-del" data-i="${i}" title="移除" aria-label="移除">${icon("x", 14)}</button>
  </div>`).join("");
}
function readRoRings() {
  return $$("#ro-rings .ring-row").map(row => ({ name: ($(".ro-name-i", row) || {}).value || "", percent: Number(($(".ro-pct-i", row) || {}).value) || 0 }));
}
function paintRoSum() {
  const rings = readRoRings(); const sum = rings.reduce((s, r) => s + r.percent, 0);
  const el = $("#ro-sum"); if (el) { el.textContent = `合计 ${sum}%`; el.className = "tag-mono " + (sum !== 100 ? "txt-err" : "txt-ok"); }
}
function roRingAdd() { ROD.rings = readRoRings(); ROD.rings.push({ name: "新环", percent: 10 }); const el = $("#ro-rings"); if (el) el.innerHTML = roRingsHtml(); paintRoSum(); }
function roRingDel(i) { ROD.rings = readRoRings(); ROD.rings.splice(i, 1); if (!ROD.rings.length) ROD.rings.push({ name: "全量", percent: 100 }); const el = $("#ro-rings"); if (el) el.innerHTML = roRingsHtml(); paintRoSum(); }
async function roSubmit() {
  const err = $("#ro-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const name = val("ro-name").trim();
  if (!name) return fail("请填写灰度名称");
  const sel = val("ro-bom");
  if (!sel) return fail("请选择已发布的 BOM");
  const at = sel.lastIndexOf("@");
  const rings = readRoRings().filter(r => r.name.trim());
  if (!rings.length) return fail("请至少配置一个环");
  for (const r of rings) if (r.percent < 1 || r.percent > 100) return fail(`环「${r.name}」百分比需在 1–100 之间`);
  const sum = rings.reduce((s, r) => s + r.percent, 0);
  if (sum !== 100) return fail(`百分比合计必须为 100%（当前 ${sum}%）`);
  try {
    await api("POST", "/rollouts", { name, bom_id: sel.slice(0, at), bom_version: Number(sel.slice(at + 1)) || 0, rings: rings.map(r => ({ name: r.name.trim(), percent: r.percent })) });
    toast(`灰度「${name}」已创建`, "ok");
    closeModal(); await refreshTerminalCenter();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
async function roAdvance(id) {
  try {
    const ro = await api("POST", `/rollouts/${enc(id)}/advance`);
    toast(ro && ro.status === "done" ? "灰度已全部完成" : "已推进到下一环", "ok");
    await refreshTerminalCenter();
  } catch (e) { toast(e.status === 409 ? "当前环仍有任务未完成，无法推进" : e.message, "err"); }
}
async function roDelete(id) {
  const ro = S.rollouts.find(r => r.id === id);
  const ok = await confirmDlg({ title: "删除灰度发布", message: `将删除灰度「${(ro && ro.name) || shortId(id)}」及其环计划。已下发的任务不受影响。`, okText: "删除" });
  if (!ok) return;
  try {
    await api("DELETE", `/rollouts/${enc(id)}`);
    toast("灰度计划已删除，已下发任务保留在任务记录中", "ok");
    await refreshTerminalCenter();
  } catch (e) { toast(e.message, "err"); }
}

/* ================= AUTO UPDATE ================= */
const updateReads = new WeakMap();
const UPD_STATE_ZH = {
  idle: ["待机", "muted"], checking: ["检查中", "info"], staged: ["已暂存", "info"],
  applying: ["升级中", "info"], restarting: ["重启中", "warn"], failed: ["失败", "err"], offline: ["更新源不可达", "warn"]
};
const FLEET_STATE_ZH = {
  converged: ["已对齐", "ok"], pending: ["待下发", "info"], upgrading: ["升级中", "info"],
  waiting_idle: ["等待空闲", "warn"], suspended: ["已挂起", "err"], unsupported: ["不在范围", "muted"], offline: ["离线", "muted"]
};
function updStateBadge(st) {
  const s = st && st.state;
  if (s === "idle" && st.available) return badge("发现新版本", "gold");
  const x = UPD_STATE_ZH[s] || [s || "待机", "muted"];
  return badge(x[0], x[1]);
}
function fleetStateBadge(s) { const x = FLEET_STATE_ZH[s] || [s || "待下发", "muted"]; return badge(x[0], x[1]); }
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
        throw new Error("更新状态响应无效");
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
      root.innerHTML = `<div class="banner warn">${icon("info", 15)}<span>${esc(S.upd.disabled ? "更新服务未启用。" : "更新状态加载失败，先前状态已过期：" + e.message)}</span></div>`;
    }
  })();
  updateReads.set(root, read);
  try { await read; } finally { updateReads.delete(root); }
}
function refreshUpdateLive() { return refreshUpdate(); }
function paintUpdateServer() {
  const el = $("#upd-server"); if (!el) return;
  const st = S.upd.server;
  if (!st) { el.innerHTML = `<div class="upd-pane-t">${icon("server", 15)} 服务端自升级</div>` + empty("refresh", "正在读取更新状态…", "", true); return; }
  const busy = st.state === "checking" || st.state === "applying" || st.state === "restarting";
  const canApply = st.available && !busy;
  let h = `<div class="upd-pane-t">${icon("server", 15)} 服务端自升级 ${updStateBadge(st)}</div>`;
  h += `<div class="ver-flow">
    <div class="vf-box"><div class="vf-lbl">当前运行</div><div class="vf-ver">v${esc(st.current || "—")}</div></div>
    <div class="vf-arrow ${st.available ? "hot" : ""}">${icon("chevR", 20)}</div>
    <div class="vf-box ${st.available ? "new" : ""}"><div class="vf-lbl">发布源最新</div><div class="vf-ver">${st.latest ? "v" + esc(st.latest) : "—"}</div></div>
  </div>`;
  if (st.state === "restarting") h += `<div class="banner info"><span class="spin"></span><span>新版本已就位，服务正在重启，控制台将自动重连…</span></div>`;
  else if (st.state === "applying") h += `<div class="banner info"><span class="spin"></span><span>${esc(st.detail || "正在下载并校验发布包…")}</span></div>`;
  else if (st.state === "failed") h += `<div class="banner err">${icon("alert", 15)}<span>${esc(st.last_error || st.detail || "升级失败")}</span></div>`;
  else if (st.state === "offline") h += `<div class="banner warn">${icon("wifi", 15)}<span>${esc(st.detail || "无法访问发布源，回连后自动重试。")}</span></div>`;
  else if (st.available) h += `<div class="banner ok">${icon("arrowUp", 15)}<span>发现新版本 v${esc(st.latest)}，${st.auto_apply ? "空闲时将自动升级。" : "可立即手动升级。"}</span></div>`;
  else if (st.detail) h += `<div class="hint mb12">${esc(st.detail)}</div>`;
  h += `<div class="kv mb12">` +
    kvRow("更新源", `<span class="mono upd-feed" title="${esc(st.feed_url || "")}">${esc(st.feed_url || "官方发布源")}</span>`) +
    kvRow("最近检查", st.checked_at ? ago(st.checked_at) : "尚未检查") +
    (st.staged_at ? kvRow("暂存时间", fmtTime(st.staged_at)) : "") +
    `</div>`;
  h += `<label class="switch mb12"><input type="checkbox" id="upd-auto" data-change="upd-auto" ${st.auto_apply ? "checked" : ""} ${busy ? "disabled" : ""}><span class="track"></span><span class="sw-lbl">自动升级（检测到新版本且空闲时自动应用）</span></label>`;
  h += `<div class="btn-row">
    <button class="btn cyan sm" data-act="upd-check" ${busy ? "disabled" : ""}>${icon("refresh", 14)} 立即检查</button>
    <button class="btn primary sm" data-act="upd-apply" ${canApply ? "" : "disabled"}>${icon("arrowUp", 14)} ${st.available ? "升级到 v" + esc(st.latest) : "立即升级"}</button>
  </div>`;
  el.innerHTML = h;
}
function paintUpdatePolicy(force = false) {
  const el = $("#upd-policy"); if (!el) return;
  const p = S.upd.policy;
  if (!p) { el.innerHTML = `<div class="upd-pane-t">${icon("sliders", 15)} 终端收敛策略</div>` + empty("sliders", "正在读取策略…", "", true); return; }
  // Skip rebuilding while the operator is interacting with a control, so a live
  // refresh never yanks an open select or steals focus mid-change.
  if (!force && TC.admin && el.contains(document.activeElement) && el.dataset.built) { syncUpdatePolicy(); return; }
  const disabled = TC.admin ? "" : "disabled";
  const opt = (v, cur, lbl) => `<option value="${v}" ${Number(cur) === v ? "selected" : ""}>${lbl || v}</option>`;
  el.innerHTML = `<div class="upd-pane-t">${icon("sliders", 15)} 终端收敛策略 <span class="hint">${TC.admin ? "改动即时生效" : "只读 · 管理操作需要登录"}</span></div>
    <label class="switch mb12"><input type="checkbox" id="upd-en" data-change="upd-policy" ${disabled} ${p.enabled ? "checked" : ""}><span class="track"></span><span class="sw-lbl">启用终端自动收敛（服务端轮询下发 agent-upgrade）</span></label>
    <div class="f-row">
      <div class="field"><label>并行上限</label><select id="upd-mp" class="inp" data-change="upd-policy" ${disabled}>${[1, 2, 3, 4, 6, 8, 12, 16].map(v => opt(v, p.max_parallel, v + " 台")).join("")}</select><div class="f-hint">同时升级的终端数。</div></div>
      <div class="field"><label>失败挂起阈值</label><select id="upd-ma" class="inp" data-change="upd-policy" ${disabled}>${[1, 2, 3, 4, 5, 6, 8, 10].map(v => opt(v, p.max_attempts, v + " 次")).join("")}</select><div class="f-hint">连续失败达到次数后挂起。</div></div>
    </div>
    <div class="field"><label>金丝雀环比例</label><select id="upd-rp" class="inp" data-change="upd-policy" ${disabled}>${[5, 10, 20, 25, 50, 100].map(v => opt(v, p.ring_percent, v + "%")).join("")}</select><div class="f-hint">首环放量比例；整环对齐后才推进下一环。</div></div>
    <div class="banner info">${icon("shield", 15)}<span>播放保护（FR-3.10）与授权门禁始终生效：播放中的终端等待空闲，未授权终端不下发。升级包经 SHA256 校验后由终端 detached 安装。</span></div>
    <div class="btn-row"><button class="btn cyan sm" data-act="upd-converge">${icon("send", 14)} 立即收敛一轮</button><div class="spacer"></div><span class="hint">目标版本 v${esc((S.upd.fleet && S.upd.fleet.target) || (S.upd.server && S.upd.server.current) || "—")}</span></div>`;
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
  if (!nodes.length) { el.innerHTML = empty("server", "暂无终端，添加并授权后自动纳入版本收敛", `<button class="btn primary sm" data-act="goto-add-node">${icon("plus", 13)} 添加终端</button>`, true); return; }
  const c = rep.counts || {};
  const parts = nodes.filter(r => r.state !== "offline" && r.state !== "unsupported");
  const conv = parts.filter(r => r.state === "converged").length;
  const pct = parts.length ? Math.round(conv / parts.length * 100) : 100;
  const stat = (k, lbl, color) => c[k] ? `<span class="ust" style="--uc:var(--${color})"><i></i>${lbl} <b>${c[k]}</b></span>` : "";
  let h = `<div class="upd-stats">
    ${stat("converged", "已对齐", "green")}${stat("upgrading", "升级中", "cyan")}${stat("pending", "待下发", "blue")}
    ${stat("waiting_idle", "等待空闲", "amber")}${stat("suspended", "已挂起", "red")}${stat("unsupported", "不在范围", "faint")}${stat("offline", "离线", "faint")}
    <span class="spacer"></span><span class="tag-mono">目标 v${esc(rep.target || "—")}</span>
  </div>`;
  h += meter(`收敛进度 · 第 ${(rep.ring || 0) + 1} 环（每环 ${rep.ring_size || 1} 台）`, pct, `${conv}/${parts.length} 台已对齐`, pct === 100 ? "green" : "cyan");
  const list = nodes.filter(r => { const n = S.nodes.find(x => x.node_id === r.node_id); return hit(n && n.alias, r.node_id, r.current, r.target, r.state, r.detail); });
  if (!list.length) { el.innerHTML = h + empty("search", "没有匹配的终端", "", true); return; }
  h += `<div class="tbl-wrap mt12"><table class="tbl"><thead><tr><th>终端</th><th>版本</th><th>状态</th><th>尝试</th><th>最近尝试</th><th>说明</th><th style="text-align:right">操作</th></tr></thead><tbody>` +
    list.map(r => {
      const n = S.nodes.find(x => x.node_id === r.node_id);
      const alias = n ? (n.alias || shortId(r.node_id)) : shortId(r.node_id);
      const same = r.current && r.current === r.target;
      return `<tr>
        <td><button class="btn ghost xs" data-act="node-open" data-id="${esc(r.node_id)}">${esc(alias)}</button><div class="sub mono">${esc(shortId(r.node_id))}${n && n.arch ? " · " + esc(n.arch) : ""}</div></td>
        <td class="mono nowrap">${same ? `<span class="txt-ok">${esc(r.current)}</span>` : `${esc(r.current || "—")} <span class="vf-mini">→</span> <span class="txt-gold">${esc(r.target || "—")}</span>`}</td>
        <td>${fleetStateBadge(r.state)}</td>
        <td class="mono">${r.attempts || 0}${r.state === "suspended" ? " / " + ((rep.policy && rep.policy.max_attempts) || 3) : ""}</td>
        <td class="sub nowrap">${r.last_attempt ? ago(r.last_attempt) : "—"}</td>
        <td class="sub upd-detail" title="${esc(r.detail || "")}">${esc(r.detail || "—")}</td>
        <td class="acts">${r.state === "suspended" ? `<button class="btn cyan xs" data-act="upd-retry" data-id="${esc(r.node_id)}">${icon("undo", 12)} 重试</button>` : ""}</td>
      </tr>`;
    }).join("") + `</tbody></table></div>`;
  el.innerHTML = h;
}
async function updCheck() {
  const btn = $('[data-act="upd-check"]');
  if (btn) { btn.disabled = true; btn.innerHTML = `<span class="spin">${icon("refresh", 14)}</span> 检查中…`; }
  try {
    const st = await api("POST", "/update/check");
    S.upd.server = st;
    if (st.state === "offline") toast("更新源不可达：" + (st.detail || "网络异常"), "warn");
    else if (st.available) toast(`发现新版本 v${st.latest}`, "ok");
    else toast(`已是最新版本 v${st.current}`, "info");
    paintUpdateServer();
  } catch (e) { toast(e.message, "err"); }
  finally { paintUpdateServer(); }
}
async function updApply() {
  const st = S.upd.server; if (!st || !st.available) return;
  const ok = await confirmDlg({
    title: "升级服务端", danger: false, okText: "升级并重启",
    message: `将下载 v${st.latest} 官方发布包，校验 SHA256 与 GPG 签名后替换二进制并重启服务。控制台会短暂断连并自动恢复；播放保护生效或存在进行中任务时将被拒绝。`
  });
  if (!ok) return;
  S.upd.busy = true;
  try {
    const r = await api("POST", "/update/apply");
    S.upd.server = r;
    toast(r && r.state === "restarting" ? "升级完成，服务正在重启…" : "升级流程已启动", "ok");
    paintUpdateServer();
  } catch (e) {
    toast(e.status === 409 ? "播放保护或进行中任务阻止了升级，空闲后重试" : e.message, e.status === 409 ? "warn" : "err");
  } finally { S.upd.busy = false; }
}
async function updAuto(el) {
  const want = el.checked;
  try {
    const st = await api("POST", "/update/auto", { auto_apply: want });
    S.upd.server = st;
    toast(want ? "已开启自动升级" : "已关闭自动升级", "ok");
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
    toast("收敛策略已保存", "ok");
    paintUpdateFleet();
  } catch (e) { toast(e.message, "err"); syncUpdatePolicy(); }
}
async function updConverge() {
  const btn = $('[data-act="upd-converge"]');
  if (btn) { btn.disabled = true; btn.innerHTML = `<span class="spin">${icon("send", 14)}</span> 收敛中…`; }
  try {
    const rep = await api("POST", "/update/fleet/converge");
    S.upd.fleet = rep;
    const c = rep.counts || {};
    toast(`收敛一轮完成：${c.converged || 0} 台已对齐，${c.upgrading || 0} 台升级中`, "ok");
    paintUpdateFleet(); paintUpdatePolicy();
  } catch (e) { toast(e.message, "err"); }
  finally { paintUpdatePolicy(); }
}
async function updRetry(id) {
  try {
    await api("POST", `/update/fleet/${enc(id)}/retry`);
    toast("已重置失败计数，下一轮将重新下发", "ok");
    await refreshUpdateLive();
  } catch (e) { toast(e.message, "err"); }
}

/* ================= KEYS & LICENSE ================= */
function shellKeys() {
  return `<div class="grid cols-2">
      <section class="card stagger" style="--i:0" id="lic-card"></section>
      <section class="card stagger" style="--i:1" id="gpg-card"></section>
    </div>
    <div class="banner info mt16">${icon("server", 16)}<span>终端授权与席位分配请在终端中心管理。</span><button class="btn ghost sm" data-act="tc-goto">前往终端中心</button></div>`;
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
  return `<div class="seats"><div class="s-top"><span>已授权席位</span><span class="mono">${used} / ${seats || "∞"}</span></div><div class="seat-bar"><div class="seat-fill ${seats && used > seats ? "over" : ""}" style="width:${pct}%"></div></div></div>`;
}
function paintLicense() {
  const el = $("#lic-card"); if (!el) return;
  const lic = S.license;
  const used = S.nodes.filter(n => n.authorized).length;
  const builtin = Number(lic && lic.builtin_seats) || 3;
  const activated = lic && lic.status === "activated";
  let h = secHead(`${icon("shield", 17)} 许可证`, `${activated ? `<button class="btn ghost sm" data-act="lic-receipt">${icon("download", 14)} 导出激活回执</button>` : ""}<button class="btn violet sm" data-act="lic-open">${icon("key", 14)} ${activated ? "重新激活" : "激活许可证"}</button>`);
  if (!lic || lic.status === "none") {
    h += `<div class="mb12">${badge("内置授权", "ok")}</div>`;
    h += `<div class="kv">` +
      kvRow("授权对象", "服务端（终端侧无需证书）") +
      kvRow("内置席位", `<span class="mono">${builtin} 台终端</span>`) +
      kvRow("许可证", "未激活（超出内置席位时用于扩容）") +
      `</div>`;
    h += seatsHtml(used, builtin);
    h += `<div class="banner info mt16">${icon("shield", 15)}<span>服务端自带 ${builtin} 台终端授权，无需许可证即可授权终端并下发服务；需要更多终端时激活许可证扩容。</span></div>`;
  } else {
    const st = lic.status;
    const seats = Number(lic.effective_seats) || builtin;
    h += `<div class="mb12">${st === "activated" ? badge("已激活", "ok") : badge("已过期", "err")}</div>`;
    h += `<div class="kv">` +
      kvRow("许可 key", `<span class="mono">${esc(lic.key || "—")}</span>`) +
      kvRow("账户", esc(lic.account || "—")) +
      kvRow("席位", `<span class="mono">${lic.seats || "∞"}</span>`) +
      kvRow("激活时间", fmtTime(lic.activated_at)) +
      kvRow("到期时间", lic.expires_at ? fmtTime(lic.expires_at) : "永久") +
      `</div>`;
    h += seatsHtml(used, seats);
    if (st === "expired") h += `<div class="banner warn mt16">${icon("alert", 15)}<span>许可证已过期，已回退内置授权（${builtin} 台终端），请及时续期恢复原有席位。</span></div>`;
    else if (used >= seats) h += `<div class="banner warn mt16">${icon("info", 15)}<span>席位已满，授权新终端前请先释放席位。</span></div>`;
  }
  el.innerHTML = h;
}
function paintKeys() {
  const el = $("#gpg-card"); if (!el) return;
  let h = secHead(`${icon("key", 17)} GPG 信任根`, `<button class="btn ghost sm" data-act="key-export">${icon("download", 14)} 导出 .asc</button><button class="btn violet sm" data-act="key-import">${icon("upload", 14)} 导入公钥</button>`);
  if (!S.keys.length) {
    h += empty("key", "钥匙串为空", `<button class="btn violet" data-act="key-import">${icon("upload", 15)} 导入公钥</button>`);
    h += `<div class="banner info mt16">${icon("shield", 15)}<span>签名强制模式下，构件与许可证签名将校验到此处的信任根公钥。</span></div>`;
  } else {
    h += S.keys.map(k => `<div class="key-row">
      <span class="key-ic">${icon("key", 17)}</span>
      <div class="key-info">
        <div class="key-name">${esc(k.name || "未命名密钥")} ${k.is_trust_root ? badge("信任根", "gold") : ""}</div>
        <div class="key-fp">${esc(k.fingerprint || "—")}</div>
        <div class="key-meta">${esc(k.email || "—")} · ${esc(k.algo || "—")} · ${fmtTime(k.created_at)}</div>
      </div>
      ${k.is_trust_root ? "" : `<button class="btn ghost xs" data-act="key-trust" data-id="${esc(k.id)}">${icon("shield", 12)} 设为信任根</button>`}
    </div>`).join("");
  }
  el.innerHTML = h;
}
async function nodeAuth(id, revoke) {
  try {
    await api("POST", `/nodes/${enc(id)}/${revoke ? "revoke" : "authorize"}`);
    toast(revoke ? "已撤销授权" : "已授权终端", "ok");
    await refreshCurrent();
  } catch (e) { toast(e.message, "err"); }
}

/* ---- license activation & GPG ---- */
function fileToText(input, cb) {
  const f = input && input.files && input.files[0]; if (!f) return;
  const r = new FileReader();
  r.onload = () => cb(String(r.result || ""));
  r.onerror = () => toast("文件读取失败", "err");
  r.readAsText(f);
}
function openLicenseActivate() {
  openModal(`${icon("key", 18)} 激活许可证`, `
    <div id="lic-err"></div>
    <div class="banner violet">${icon("shield", 15)}<span>payload 为许可 JSON 文档，签名为其 detached GPG 签名（armored）。服务器将 base64 编码后校验签名与有效期。</span></div>
    <div class="field"><label>从文件读取 payload（.json）</label><input id="lic-pfile" type="file" class="inp" data-change="lic-pfile" accept=".json,application/json"></div>
    <div class="field"><label>许可 payload JSON *</label><textarea id="lic-payload" class="inp" rows="7" placeholder='{"key":"ZEP-XXXX-XXXX","account":"studio@zephyr.hk","seats":8,"expires_at":1893456000}'></textarea></div>
    <div class="field"><label>从文件读取签名（.asc / .sig）</label><input id="lic-sfile" type="file" class="inp" data-change="lic-sfile" accept=".asc,.sig,.gpg,.txt"></div>
    <div class="field"><label>detached 签名（armored）</label><textarea id="lic-sig" class="inp" rows="6" placeholder="-----BEGIN PGP SIGNATURE-----&#10;…&#10;-----END PGP SIGNATURE-----"></textarea><div class="f-hint">开发模式可留空签名；强制模式必须提供有效签名。</div></div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn violet" data-act="lic-submit">${icon("check", 15)} 激活</button>`, { narrow: true });
}
async function licSubmit() {
  const err = $("#lic-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const payloadText = val("lic-payload").trim(), sigText = val("lic-sig").trim();
  if (!payloadText) return fail("请提供许可 payload JSON");
  let p; try { p = JSON.parse(payloadText); } catch (e) { return fail("payload JSON 解析失败：" + e.message); }
  if (!p || !p.key) return fail("payload 缺少 key 字段");
  if (!(Number(p.seats) >= 1)) return fail("payload seats 必须不少于 1");
  try {
    await api("POST", "/license/activate", { payload_b64: b64utf8(payloadText), sig_b64: b64utf8(sigText) });
    toast("许可证已激活", "ok");
    closeModal();
    if (S.view === "keys") await refreshKeys(); else await refreshCurrent();
    globalTick();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
function openKeyImport() {
  openModal(`${icon("upload", 18)} 导入 GPG 公钥`, `
    <div id="gpg-err"></div>
    <div class="field"><label>从文件读取（.asc / .gpg）</label><input id="gpg-file" type="file" class="inp" data-change="gpg-file" accept=".asc,.gpg,.pub,.txt"></div>
    <div class="field"><label>armored 公钥 *</label><textarea id="gpg-armored" class="inp" rows="10" placeholder="-----BEGIN PGP PUBLIC KEY BLOCK-----&#10;…&#10;-----END PGP PUBLIC KEY BLOCK-----"></textarea><div class="f-hint">仅接受公钥材料，私钥将被拒绝。</div></div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn violet" data-act="key-import-submit">${icon("upload", 15)} 导入</button>`, { narrow: true });
}
async function keyImportSubmit() {
  const err = $("#gpg-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const armored = val("gpg-armored").trim();
  if (!armored.includes("BEGIN PGP PUBLIC KEY BLOCK")) return fail("无效的公钥材料：缺少 PUBLIC KEY BLOCK");
  try {
    const r = await api("POST", "/keys/import", { armored });
    const n = r && Array.isArray(r.imported) ? r.imported.length : 0;
    toast(n ? `已导入 ${n} 个新公钥` : "公钥已存在，无新增", "ok");
    closeModal(); await refreshKeys();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
async function keyTrust(id) {
  const ok = await confirmDlg({ title: "设为信任根", message: "将该公钥设为服务器信任根，用于校验构件与许可证签名。原信任根将被替换。", okText: "设为信任根", danger: false });
  if (!ok) return;
  try {
    await api("POST", `/keys/${enc(id)}/trust-root`);
    toast("已更新信任根", "ok");
    await refreshKeys();
  } catch (e) { toast(e.message, "err"); }
}
async function keyExport() {
  try {
    const r = await fetch(API_BASE + "/keys/trust-root.asc");
    if (!r.ok) {
      let m = "导出失败 (" + r.status + ")";
      try { const d = await r.json(); m = d.error || d.message || m; } catch (e) { /* 非 JSON */ }
      throw new Error(m);
    }
    const text = await r.text();
    if (!text.trim()) { toast("尚未设置信任根密钥", "warn"); return; }
    const url = URL.createObjectURL(new Blob([text], { type: "application/pgp-keys" }));
    const a = document.createElement("a");
    a.href = url; a.download = "trust-root.asc";
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 4000);
    toast("trust-root.asc 已导出", "ok");
  } catch (e) { toast(e.message, "err"); }
}

/* ================= AUDIT ================= */
function shellAudit() {
  return `<section class="card stagger" style="--i:0">
      ${secHead(`${icon("audit", 17)} 操作日志`, `<button class="btn ghost sm" data-act="audit-export" data-fmt="csv">${icon("download", 14)} CSV</button><button class="btn ghost sm" data-act="audit-export" data-fmt="json">${icon("download", 14)} JSON</button>`)}
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
  chips.innerHTML = `<button class="chip ${S.auditFilter === "all" ? "active" : ""}" data-act="audit-filter" data-f="all">全部 <span class="cnt">${S.audit.length}</span></button>` +
    top.map(a => `<button class="chip ${S.auditFilter === a ? "active" : ""}" data-act="audit-filter" data-f="${esc(a)}">${esc(a)} <span class="cnt">${counts[a]}</span></button>`).join("");
  let list = S.audit.slice();
  if (S.auditFilter !== "all") list = list.filter(r => r.action === S.auditFilter);
  list = list.filter(r => hit(r.actor, r.action, r.target, r.detail, r.ip, r.result));
  list.sort((a, b) => ts(b.at) - ts(a.at));
  if (!list.length) { tbl.innerHTML = empty("audit", S.audit.length ? "没有匹配的操作日志" : "暂无操作日志", "", true); return; }
  tbl.innerHTML = `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>时间</th><th>操作者</th><th>动作</th><th>目标 / 详情</th><th>结果</th><th>耗时</th></tr></thead><tbody>` +
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
  toast(`已开始导出 ${fmt.toUpperCase()}`, "info");
}

/* ================= ALERTS ================= */
function openAlerts() {
  openDrawer(`${icon("bell", 18)} 告警中心`, "实时告警与处置", alertsHtml(), "", { ctx: { kind: "alerts" } });
}
function paintAlerts() {
  if (!drawerCtx || drawerCtx.kind !== "alerts") return;
  setDrawerBody(alertsHtml());
}
function alertsHtml() {
  const counts = { open: 0, ack: 0, resolved: 0 };
  S.alerts.forEach(a => { const s = a.status === "acked" ? "ack" : a.status; if (counts[s] != null) counts[s]++; });
  let h = `<div class="btn-row mb12">
      <button class="btn cyan sm" data-act="alert-scan">${icon("refresh", 14)} 立即扫描</button>
      <button class="btn ghost sm" data-act="alert-ack-all" ${counts.open ? "" : "disabled"}>${icon("check", 14)} 全部确认</button>
    </div>
    <div class="chips mb12">` +
    [["open", "未处理", counts.open], ["ack", "已确认", counts.ack], ["resolved", "已解决", counts.resolved], ["all", "全部", S.alerts.length]]
      .map(x => `<button class="chip ${S.alertFilter === x[0] ? "active" : ""}" data-act="alert-filter" data-f="${x[0]}">${x[1]} <span class="cnt">${x[2]}</span></button>`).join("") +
    `</div>`;
  let list = S.alerts.slice();
  if (S.alertFilter !== "all") list = list.filter(a => (a.status === "acked" ? "ack" : a.status) === S.alertFilter);
  list = list.filter(a => hit(a.message, a.kind, a.node_id, a.severity));
  list.sort((a, b) => ts(b.at) - ts(a.at));
  if (!list.length) return h + empty("bell", S.alerts.length ? "没有匹配的告警" : "暂无告警，系统运行正常", "", true);
  h += list.map(a => {
    const st = a.status === "acked" ? "ack" : a.status;
    return `<div class="alert-item sev-${esc(a.severity || "info")}">
      <div class="a-top">${sevBadge(a.severity)}${alertStatusBadge(a)}<span class="badge muted">${esc(a.kind || "alert")}</span><span class="spacer"></span><span class="txt-faint" style="font-size:11px">${ago(a.at)}</span></div>
      <div class="a-msg">${esc(a.message || "—")}</div>
      <div class="a-meta">${a.node_id ? `<button class="btn ghost xs" data-act="node-open" data-id="${esc(a.node_id)}">${icon("server", 12)} ${esc(shortId(a.node_id))}</button>` : ""}<span class="txt-faint">${fmtTime(a.at)}</span></div>
      <div class="a-btns">
        ${st === "open" ? `<button class="btn ghost xs" data-act="alert-ack" data-id="${esc(a.id)}">${icon("eye", 12)} 确认</button>` : ""}
        ${st !== "resolved" ? `<button class="btn primary xs" data-act="alert-resolve" data-id="${esc(a.id)}">${icon("check", 12)} 解决</button>` : ""}
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
    toast(act === "ack" ? "告警已确认" : "告警已解决", "ok");
    await refetchAlerts();
  } catch (e) { toast(e.message, "err"); }
}
async function alertAckAll() {
  try { await api("POST", "/alerts/ack-all"); toast("已确认全部未处理告警", "ok"); await refetchAlerts(); }
  catch (e) { toast(e.message, "err"); }
}
async function alertScan() {
  try { await api("POST", "/alerts/scan"); toast("告警扫描完成", "ok"); await refetchAlerts(); }
  catch (e) { toast(e.message, "err"); }
}

/* ================= MUSIC LIBRARY ================= */
function fmtDur(ms) {
  const s = Math.max(0, Math.round((Number(ms) || 0) / 1000));
  return Math.floor(s / 60) + ":" + p2(s % 60);
}
function codecBadge(c) {
  const k = String(c || "").toLowerCase();
  const lossless = ["flac", "alac", "pcm", "wav", "aiff", "dsd"];
  const kind = !k || k === "unknown" ? "muted" : lossless.some(x => k.includes(x)) ? "ok" : "warn";
  return badge(c || "未知", kind);
}
function mountBadge(st) {
  if (st === "mounted") return badge("已挂载", "ok");
  if (st === "error") return badge("错误", "err");
  return badge("未挂载", "muted");
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
      <span class="q-sub txt-faint">${p.count} 首 · 创建于 ${fmtTime(p.created_at)} · 更新于 ${ago(p.updated_at)}</span>
    </span>
    <span class="q-acts">
      <button class="btn xs ghost" data-act="pl-open" data-id="${esc(p.id)}" title="查看 / 编辑">${icon("eye", 12)} 查看</button>
      <button class="btn xs primary" data-act="pl-push" data-id="${esc(p.id)}" ${p.count ? "" : "disabled"} title="推送到终端">${icon("send", 12)} 推送</button>
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
  openModal(`${icon("folder", 18)} 添加 SMB 共享`, `
    <div id="mnt-err"></div>
    <div class="f-row">
      <div class="field"><label>主机 *</label><input id="mnt-host" class="inp mono" placeholder="如 192.168.1.10 或 nas.local" maxlength="120"><div class="f-hint">SMB 服务器 hostname / IP，不含 / \\ 空格</div></div>
      <div class="field"><label>共享名 *</label><input id="mnt-share" class="inp mono" placeholder="如 music" maxlength="120"><div class="f-hint">单段共享名，如 music / Media</div></div>
    </div>
    <div class="f-row">
      <div class="field"><label>用户名</label><input id="mnt-user" class="inp mono" placeholder="留空 = guest 访客" maxlength="120"></div>
      <div class="field"><label>密码</label><input id="mnt-pass" type="password" class="inp mono" placeholder="仅服务端保存，不回显" maxlength="200"></div>
    </div>
    <div class="f-row">
      <div class="field"><label>域</label><input id="mnt-domain" class="inp mono" placeholder="如 WORKGROUP（可选）" maxlength="120"></div>
      ${win ? "" : `<div class="field"><label>SMB 版本</label><select id="mnt-ver" class="inp"><option value="3.0">3.0（默认）</option><option value="3.1.1">3.1.1</option><option value="2.1">2.1</option><option value="2.0">2.0</option><option value="1.0">1.0</option></select></div>`}
    </div>
    ${win ? "" : `<div class="field"><label class="switch"><input id="mnt-rw" type="checkbox"><span class="track"></span><span class="sw-lbl">可写挂载（默认只读，媒体库仅读取）</span></label></div>`}
    <div class="banner info">${icon("info", 15)}<span>${win
      ? "Windows 通过系统 SMB 客户端直连共享（UNC），无需 cifs-utils 与 root。凭据仅服务端保存，经系统 API 传递，绝不出现在命令行或日志。"
      : "挂载使用内核 mount.cifs（需 cifs-utils 与 root）。凭据写入临时 0600 文件，挂载后立即删除，绝不出现在命令行或日志。"}</span></div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn primary" data-act="lib-add-submit">${icon("check", 15)} 添加共享</button>`, { narrow: true });
}
async function submitMountAdd() {
  const err = $("#mnt-err");
  const fail = m => { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>${esc(m)}</span></div>`; };
  const host = val("mnt-host").trim(), share = val("mnt-share").trim();
  if (!host) return fail("请填写主机");
  if (!share) return fail("请填写共享名");
  if (/[/\\\s]/.test(host)) return fail("主机不能包含 / \\ 或空格");
  if (/[/\\\s]/.test(share)) return fail("共享名不能包含 / \\ 或空格");
  try {
    const result = await api("POST", "/mounts", {
      host, share, username: val("mnt-user").trim(), password: val("mnt-pass"),
      domain: val("mnt-domain").trim(), version: val("mnt-ver"), writable: checked("mnt-rw")
    });
    toast(result?.reused ? `已复用 ${host}/${share} 的音乐源与索引；已有连接凭据不会被覆盖，修改请使用“编辑源”。` : `共享 ${host}/${share} 已添加，挂载后可更新扫描`, "ok");
    closeModal();
    await refreshCurrent();
  } catch (e) { fail(e.message); }
}
async function libMountAct(id, act) {
  const m = S.mounts.find(x => x.id === id);
  const label = m ? (m.kind === "local" ? m.target : m.host + "/" + m.share) : shortId(id);
  try {
    if (act === "scan" || act === "scan-full") {
      if (act === "scan-full" && !await confirmDlg({ title: "全量重新扫描？", message: "将重新读取所有音频标签与封面，耗时较长；曲目 ID、播放列表与历史保持不变。日常同步使用“更新扫描”即可。", okText: "全量重扫" })) return;
      const r = await api("POST", `/mounts/${enc(id)}/scan${act === "scan-full" ? "?mode=full" : ""}`);
      toast(`扫描完成：共 ${r.count ?? 0} 首 · 新增 ${r.added ?? 0} / 更新 ${r.updated ?? 0} / 未变 ${r.unchanged ?? 0} / 缺失 ${r.removed ?? 0}`, "ok");
    } else {
      const r = await api("POST", `/mounts/${enc(id)}/${act}`);
      if (r && r.status === "error") {
        toast(`${act === "mount" ? "挂载" : "卸载"}失败：${r.detail || "未知错误"}`, "err");
      } else {
        toast(act === "mount" ? `已挂载 ${label}` : `已卸载 ${label}`, "ok");
      }
    }
    await refreshCurrent();
  } catch (e) { toast(e.message, "err"); await refreshCurrent(); }
}
async function libArchive(id) {
  const ok = await confirmDlg({ title: "移除并保留索引？", message: "将停止此音乐源连接并移除 NAS 登录凭据，曲目索引、封面、播放列表与历史仍保留。下次添加同一地址可直接复用索引；不会删除原始音频。", okText: "保留索引并移除" });
  if (!ok) return;
  try { await api("POST", `/mounts/${enc(id)}/archive`); toast("音乐源已移除，索引已保留", "ok"); await refreshCurrent(); }
  catch (e) { toast(e.message, "err"); }
}
async function libDelete(id) {
  const m = S.mounts.find(x => x.id === id);
  const label = m ? (m.kind === "local" ? m.target : m.host + "/" + m.share) : shortId(id);
  const ok = await confirmDlg({ title: "彻底删除音乐源与索引？", message: `将永久删除 ${label} 的源配置、曲目索引与身份记录；同地址下次需重新建立索引。播放列表中的对应曲目会标记为缺失。不删除原始音频文件或独立收听历史。只想断开连接请取消并选择“移除并保留索引”。`, okText: "彻底删除", danger: true });
  if (!ok) return;
  try { await api("DELETE", `/mounts/${enc(id)}`); toast(`共享 ${label} 已删除`, "ok"); await refreshCurrent(); }
  catch (e) { toast(e.message, "err"); }
}
async function libRefresh() { try { await refreshCurrent(); toast("已刷新", "ok"); } catch (e) { toast(e.message, "err"); } }

/* ---- push playback: terminal picker (single track / album / playlist) ---- */
const TP = { cur: null, trackIDs: [], track: null, label: "", playlistID: "", sel: "", busy: false };
async function openTrackPlay(trackID) {
  const t = await api("GET", `/tracks/${enc(trackID)}`);
  await openPushPicker({ trackIDs: [trackID], track: t, label: t.title || "未知曲目" });
}
async function openPlPush(id) {
  let d;
  try { d = await api("GET", `/playlists/${enc(id)}`); } catch (e) { toast(e.message, "err"); return; }
  const ids = (d.items || []).filter(t => !t.missing).map(t => t.track_id);
  if (!ids.length) { toast("播放列表为空或曲目已全部缺失", "warn"); return; }
  openPushPicker({ trackIDs: ids, label: d.name, playlistID: id });
}
async function openPushPicker(opts) {
  TP.cur = opts; TP.trackIDs = opts.trackIDs; TP.track = opts.track || null;
  TP.label = opts.label || ""; TP.playlistID = opts.playlistID || ""; TP.sel = ""; TP.busy = false;
  openModal(`${icon("send", 18)} 推送播放`, `<div id="tp-body">${empty("refresh", "正在加载终端…", "", true)}</div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn" id="tp-next" data-act="track-next-go" disabled>下一首播放</button><button class="btn" id="tp-enq" data-act="track-enqueue-go" disabled>${icon("plus", 15)} 加入队列</button><button class="btn primary" id="tp-go" data-act="track-play-go" disabled>${icon("play", 15)} 推送播放</button>`, { narrow: true });
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
      <div class="tp-title truncate">${esc(t.title || "未知曲目")}</div>
      <div class="tp-meta">${esc(t.artist || "—")} · ${esc(t.album || "—")} ${codecBadge(t.codec)} <span class="mono">${t.rate ? (t.rate / 1000).toFixed(1) + " kHz" : ""}${t.channels ? " · " + t.channels + " ch" : ""}${t.bits ? " · " + t.bits + " bit" : ""}</span></div>
    </div>`
    : `<div class="tp-track">
      <div class="tp-title truncate">${esc(TP.label)}</div>
      <div class="tp-meta">${TP.trackIDs.length} 首曲目${TP.playlistID ? " · 播放列表" : " · 全部匹配曲目"}</div>
    </div>`;
  let h = head + `<div class="divider"></div><span class="lbl">选择终端（在线 ${online.length}）</span>`;
  if (!online.length) {
    h += empty("server", "没有在线终端。请先在「终端中心」添加并授权终端，终端 Agent 连接 ZAT 后即可推送。", `<button class="btn ghost sm" data-act="tp-goto-nodes">${icon("server", 14)} 前往终端中心</button>`, true);
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
        <span class="tp-nbadges">${authBadge(n)}${p.connected ? badge("ZAT 已连接", "ok") : badge("未连接", "muted")}${n.playing ? badge("播放中", "info") : ""}</span>
      </div>`;
    }).join("") + `</div>`;
    h += `<div class="f-hint mt8">推送播放＝替换队列并立即播放；加入队列＝追加到队尾（队列为空时立即播放）。</div>`;
    if (!online.some(n => n.authorized)) h += `<div class="banner warn mt12">${icon("alert", 15)}<span>在线终端均未授权，ZAT 推流会被拒绝。请先在终端中心授权。</span></div>`;
    else if (TP.sel && !(S.nodes.find(n => n.node_id === TP.sel) || {}).authorized) h += `<div class="banner warn mt12">${icon("alert", 15)}<span>该终端未授权，无法推流。请选择已授权终端。</span></div>`;
  }
  body.innerHTML = h;
  const go = $("#tp-go"); if (go) go.disabled = !TP.sel || TP.busy;
  const enq = $("#tp-enq"); if (enq) enq.disabled = !TP.sel || TP.busy;
  const next = $("#tp-next"); if (next) next.disabled = !TP.sel || TP.busy;
}
async function trackPlayGo() {
  const nodeID = TP.sel, selection = TP.cur; if (!nodeID || TP.busy || !TP.trackIDs.length) return;
  if (!await confirmDlg({ title: "替换终端播放队列？", message: `将用所选 ${TP.trackIDs.length} 首曲目替换该终端现有队列并立即播放。`, okText: "替换并播放", danger: true })) return;
  if (TP.cur !== selection || TP.busy || TP.sel !== nodeID) return;
  const n = S.nodes.find(x => x.node_id === nodeID);
  if (n && !n.authorized) { toast("终端未授权，无法推流", "warn"); return; }
  TP.busy = true;
  const go = $("#tp-go"); if (go) { go.disabled = true; go.innerHTML = `${icon("refresh", 15)} 推送中…`; }
  const label = n ? (n.alias || shortId(nodeID)) : shortId(nodeID);
  try {
    let r;
    if (TP.playlistID) {
      r = await api("POST", `/playlists/${enc(TP.playlistID)}/push`, { node_id: nodeID });
    } else {
      r = await api("POST", `/nodes/${enc(nodeID)}/play`, { track_id: TP.trackIDs[0] });
      if (TP.trackIDs.length > 1) await api("PUT", `/nodes/${enc(nodeID)}/queue`, { track_ids: TP.trackIDs, position: 0 });
    }
    toast(`已推送${TP.trackIDs.length > 1 ? " " + TP.trackIDs.length + " 首" : ""}到 ${label}${r && r.session_id ? " · " + shortId(r.session_id) : ""}`, "ok");
    closeModal();
    await refreshCurrent();
  } catch (e) {
    TP.busy = false;
    if (go) { go.disabled = false; go.innerHTML = `${icon("play", 15)} 推送播放`; }
    toast(e.message, "err");
  }
}
async function trackEnqueueGo(next = false) {
  const nodeID = TP.sel; if (!nodeID || TP.busy || !TP.trackIDs.length) return;
  const n = S.nodes.find(x => x.node_id === nodeID);
  if (n && !n.authorized) { toast("终端未授权，无法推流", "warn"); return; }
  TP.busy = true;
  const enq = $("#tp-enq"); if (enq) { enq.disabled = true; enq.innerHTML = `${icon("refresh", 15)} 加入中…`; }
  const label = n ? (n.alias || shortId(nodeID)) : shortId(nodeID);
  try {
    const r = await api("POST", `/nodes/${enc(nodeID)}/queue/batch`, { track_ids: TP.trackIDs, next });
    toast(`已${next ? "插入下一首" : "加入队尾"} · ${label} · ${TP.trackIDs.length} 首${r && r.session_id ? " · 已开始播放" : ""}`, "ok");
    closeModal();
    await refreshCurrent();
  } catch (e) {
    TP.busy = false;
    if (enq) { enq.disabled = false; enq.innerHTML = `${icon("plus", 15)} 加入队列`; }
    toast(e.message, "err");
  }
}
/* ---- album detail ---- */
function openAlbum(key) {
  const g = albumByKey(key); if (!g) { toast("专辑已不在音乐库中，请刷新", "warn"); return; }
  const dec = g.tracks.filter(t => t.decodable).length;
  const rows = g.tracks.map((t, i) => `
    <div class="q-row">
      <span class="q-idx mono">${i + 1}</span>
      <span class="q-info">
        <span class="q-title truncate">${esc(t.title || "—")}</span>
        <span class="q-sub txt-faint">${esc(t.codec || "?")}${t.rate ? " · " + (t.rate / 1000).toFixed(1) + " kHz" : ""}${t.duration_ms ? " · " + fmtDur(t.duration_ms) : ""}${t.decodable ? "" : " · 仅识别"}</span>
      </span>
      <span class="q-acts">
        <button class="btn xs ghost" data-act="track-pl" data-id="${esc(t.id)}" title="加入播放列表">${icon("layers", 12)}</button>
        <button class="btn xs ghost" data-act="track-play" data-id="${esc(t.id)}" ${t.decodable ? "" : "disabled"} title="推送到终端">${icon("send", 12)}</button>
      </span>
    </div>`).join("");
  openModal(`${icon("disc", 18)} 专辑`, `
    <div style="display:flex;gap:12px;align-items:center">
      ${coverArt(g.album, g.artist, "thumb", 22, albumCoverHash(g))}
      <div class="tp-track" style="min-width:0">
        <div class="tp-title truncate">${esc(g.album || "未分类专辑")}</div>
        <div class="tp-meta">${esc(g.artist || "未知艺术家")} · ${g.tracks.length} 首 · ${fmtDur(g.ms)}${dec < g.tracks.length ? ` · ${dec} 首可解码` : ""}</div>
      </div>
    </div>
    <div class="divider"></div>
    <div class="q-list">${rows}</div>`,
    `<button class="btn ghost" data-act="modal-close">关闭</button><div class="spacer"></div>
     ${typeof favoriteAlbumButton === "function" && g.tracks.length && g.tracks.every(t => (t.album_artist || t.artist || "") === (g.tracks[0].album_artist || g.tracks[0].artist || "")) ? favoriteAlbumButton(g.tracks[0].album_artist || g.tracks[0].artist, g.album) : ""}
     <button class="btn" data-act="album-pl" data-ak="${esc(key)}">${icon("layers", 15)} 加入播放列表</button>
     <button class="btn primary" data-act="album-push" data-ak="${esc(key)}" ${dec ? "" : "disabled"}>${icon("send", 15)} 推送 / 加入队列</button>`);
}
function albumPush(key) {
  const g = albumByKey(key); if (!g) { toast("专辑已不在音乐库中，请刷新", "warn"); return; }
  const ids = g.tracks.filter(t => t.decodable).map(t => t.id);
  if (!ids.length) { toast("该专辑没有可解码的曲目", "warn"); return; }
  openPushPicker({ trackIDs: ids, label: (g.album || "未分类专辑") + " · " + (g.artist || "未知艺术家") });
}

/* ---- add tracks to a playlist (picker + create) ---- */
const PP = { ids: [] };
const PLNEW = { ids: [] };
async function openPlPicker(trackIDs) {
  PP.ids = trackIDs;
  openModal(`${icon("layers", 18)} 加入播放列表`, `<div id="pp-body">${empty("refresh", "正在加载…", "", true)}</div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn primary" data-act="pl-pick-new">${icon("plus", 15)} 新建并添加</button>`, { narrow: true });
  try { const p = await api("GET", "/playlists"); S.playlists = Array.isArray(p) ? p : []; } catch (e) { /* keep cached */ }
  if (!$(".modal")) return;
  const el = $("#pp-body"); if (!el) return;
  el.innerHTML = `<div class="f-hint mb8">将 ${PP.ids.length} 首曲目添加到所选播放列表末尾。</div>` +
    (S.playlists.length
      ? `<div class="q-list">` + S.playlists.map(p => `
        <div class="q-row">
          <span class="q-idx mono">${icon("layers", 12)}</span>
          <span class="q-info"><span class="q-title truncate">${esc(p.name)}</span><span class="q-sub txt-faint">${p.count} 首</span></span>
          <span class="q-acts"><button class="btn xs primary" data-act="pl-pick" data-id="${esc(p.id)}">${icon("plus", 12)} 添加</button></span>
        </div>`).join("") + `</div>`
      : empty("layers", "还没有播放列表，点右下「新建并添加」", "", true));
}
async function plPick(id) {
  try {
    await api("POST", `/playlists/${enc(id)}/items`, { track_ids: PP.ids });
    toast(`已添加 ${PP.ids.length} 首到播放列表`, "ok");
    closeModal();
    await syncPlaylists();
  } catch (e) { toast(e.message, "err"); }
}
function openPlNew(ids, reopenPlayerID) {
  PLNEW.ids = ids || [];
  PLNEW.reopen = reopenPlayerID || "";
  openModal(`${icon("layers", 18)} 新建播放列表`, `
    <div id="pln-err"></div>
    <div class="field"><label>名称 *</label><input id="pln-name" class="inp" maxlength="80" placeholder="如：深夜爵士"></div>
    ${PLNEW.ids.length ? `<div class="f-hint">创建后自动添加 ${PLNEW.ids.length} 首曲目。</div>` : ""}`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn primary" data-act="pl-new-submit">${icon("check", 15)} 创建</button>`, { narrow: true });
  const i = $("#pln-name"); if (i) i.focus();
}
async function plNewSubmit() {
  const name = val("pln-name").trim();
  const err = $("#pln-err");
  if (!name) { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>请填写名称</span></div>`; return; }
  try {
    const p = await api("POST", "/playlists", { name });
    if (PLNEW.ids.length) await api("POST", `/playlists/${enc(p.id)}/items`, { track_ids: PLNEW.ids });
    toast(`播放列表「${name}」已创建${PLNEW.ids.length ? `，已添加 ${PLNEW.ids.length} 首` : ""}`, "ok");
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
  openModal(`${icon("layers", 18)} 播放列表`, `<div id="pl-body">${empty("refresh", "正在加载…", "", true)}</div>`, "");
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
        <span class="q-title truncate">${esc(t.title || t.track_id)}${t.missing ? " " + badge("缺失", "warn") : ""}</span>
        <span class="q-sub txt-faint">${esc([t.artist, t.album].filter(Boolean).join(" · ") || "—")}${t.duration_ms ? " · " + fmtDur(t.duration_ms) : ""}</span>
      </span>
      <span class="q-acts">
        <button class="btn xs ghost" data-act="pl-item-up" data-i="${i}" ${i === 0 ? "disabled" : ""} title="上移">${icon("arrowUp", 12)}</button>
        <button class="btn xs ghost" data-act="pl-item-down" data-i="${i}" ${i === items.length - 1 ? "disabled" : ""} title="下移">${icon("arrowDown", 12)}</button>
        <button class="btn xs ghost" data-act="pl-item-del" data-i="${i}" title="移除">${icon("x", 12)}</button>
      </span>
    </div>`).join("");
  setModalBody(`
    <div class="tp-track">
      <div class="tp-title truncate">${esc(d.name)}</div>
      <div class="tp-meta">${items.length} 首 · 更新于 ${ago(d.updated_at)}</div>
    </div>
    <div class="divider"></div>
    ${items.length ? `<div class="q-list">${rows}</div>` : empty("music", "列表为空。在曲库或专辑里点「加列表」添加曲目。", "", true)}`);
  const ok = items.some(t => !t.missing);
  setModalFoot(`
    <button class="btn ghost" data-act="pl-rename" data-id="${esc(d.id)}">${icon("edit", 14)} 重命名</button>
    <button class="btn danger" data-act="pl-delete" data-id="${esc(d.id)}">${icon("trash", 14)} 删除</button>
    <div class="spacer"></div>
    <button class="btn primary" data-act="pl-push" data-id="${esc(d.id)}" ${ok ? "" : "disabled"}>${icon("send", 15)} 推送到终端</button>`);
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
  openModal(`${icon("edit", 18)} 重命名播放列表`, `
    <div id="plr-err"></div>
    <div class="field"><label>名称 *</label><input id="plr-name" class="inp" maxlength="80" value="${esc(p ? p.name : "")}"></div>`,
    `<button class="btn ghost" data-act="pl-open" data-id="${esc(id)}">返回</button><div class="spacer"></div><button class="btn primary" data-act="pl-rename-submit" data-id="${esc(id)}">${icon("check", 15)} 保存</button>`, { narrow: true });
  const i = $("#plr-name"); if (i) i.focus();
}
async function plRenameSubmit(id) {
  const name = val("plr-name").trim();
  const err = $("#plr-err");
  if (!name) { if (err) err.innerHTML = `<div class="banner err">${icon("alert", 15)}<span>请填写名称</span></div>`; return; }
  try {
    await api("PATCH", `/playlists/${enc(id)}`, { name });
    toast("已重命名", "ok");
    await syncPlaylists();
    openPlaylist(id);
  } catch (e) { toast(e.message, "err"); }
}
async function plDelete(id) {
  const p = (PL.detail && PL.detail.id === id) ? PL.detail : S.playlists.find(x => x.id === id);
  const name = p ? p.name : shortId(id);
  const ok = await confirmDlg({ title: "删除播放列表", message: `将删除「${name}」及其全部条目。此操作不可撤销。`, okText: "删除" });
  if (!ok) return;
  try {
    await api("DELETE", `/playlists/${enc(id)}`);
    toast(`播放列表「${name}」已删除`, "ok");
    closeModal();
    await syncPlaylists();
  } catch (e) { toast(e.message, "err"); }
}

async function playStop(nodeID) {
  try { await api("POST", `/nodes/${enc(nodeID)}/stop`); toast("已停止播放", "ok"); }
  catch (e) { toast(e.message, "err"); }
  await syncPlayUI();
}
async function playPause(nodeID) {
  const p = S.play[nodeID] || S.nodePlay[nodeID] || {};
  try {
    if (p.playing) { await api("POST", `/nodes/${enc(nodeID)}/pause`); toast("已暂停", "ok"); }
    else if (p.state === "paused") { await api("POST", `/nodes/${enc(nodeID)}/resume`); toast("继续播放", "ok"); }
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
  openModal(`${icon("sliders", 18)} 播放控制台`, `<div id="ply-body">${empty("refresh", "正在加载播放状态…", "", true)}</div>`, "", {
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
  const { devs, cur } = nodePlayDevs(p);
  const plOpts = `<option value="">载入播放列表…</option>` + S.playlists.map(pl => `<option value="${esc(pl.id)}">${esc(pl.name)}（${pl.count} 首）</option>`).join("");
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
          <div class="field"><label>播放终端</label><select id="ply-node" class="inp" data-change="ply-node" data-sig="${esc(online.map(x => x.node_id).join("|"))}">${nodeOpts}</select></div>
          <div class="field"><label>输出设备</label><select id="ply-dev" class="inp mono" data-change="ply-device" data-sig="${esc(devs.join("|"))}" ${p.connected ? "" : "disabled"}>${nodePlayOpts(devs, cur)}</select></div>
        </div>
      </div>
    </div>
    <div class="ply-seek">
      <span class="ply-time mono" id="ply-el">0:00</span>
      <input type="range" id="ply-seek" min="0" max="0" step="500" value="0" data-change="ply-seek" disabled>
      <span class="ply-time mono" id="ply-total">0:00</span>
    </div>
    <div class="ply-transport">
      <button class="btn" id="ply-prev" data-act="queue-prev" data-id="${esc(id)}" title="上一首">${icon("chevL", 18)}</button>
      <button class="btn primary ply-big" id="ply-pp" data-act="play-pause" data-id="${esc(id)}" title="播放 / 暂停">${icon("play", 20)}</button>
      <button class="btn" id="ply-next" data-act="queue-next" data-id="${esc(id)}" title="下一首">${icon("chevR", 18)}</button>
      <button class="btn danger" id="ply-stop" data-act="play-stop" data-id="${esc(id)}" title="停止">${icon("stop", 16)}</button>
      <div class="ply-vol">${icon("speaker", 15)}<input type="range" id="ply-vol" min="0" max="100" step="1" value="100" data-change="ply-vol" disabled><span class="ply-time mono" id="ply-vol-v">100</span></div>
    </div>
    <div class="divider"></div>
    <div class="sec-head">
      <div class="sec-title">${icon("layers", 16)} 播放队列 <span class="txt-faint" id="ply-qcnt"></span></div>
      <div class="spacer"></div>
      <button class="btn xs ghost" data-act="ply-add-toggle">${icon("plus", 12)} 添加曲目</button>
      <select id="ply-load" class="inp ply-load" data-change="ply-load">${plOpts}</select>
      <button class="btn xs ghost" data-act="ply-save">${icon("layers", 12)} 存为列表</button>
      <button class="btn xs ghost" data-act="queue-clear" data-id="${esc(id)}">${icon("trash", 12)} 清空</button>
    </div>
    <div id="ply-add" class="ply-add hidden">
      <input id="ply-add-search" class="inp" placeholder="搜索曲目 / 艺术家 / 专辑…" value="" autocomplete="off">
      <div class="ply-add-list" id="ply-add-list"></div>
      <div class="acts mt8"><span class="f-hint" id="ply-add-cnt"></span><div class="spacer"></div><button class="btn primary sm" id="ply-add-go" data-act="ply-add-go" disabled>${icon("plus", 14)} 加入队列</button></div>
    </div>
    <div id="ply-qlist"></div>
    <div class="f-hint mt8">队列保存在服务端 · 自然播完自动续播 · 进度条可双向拖动定位</div>
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
  set("#ply-title", hasTrack ? esc(title || "—") : "空闲 · 未推流");
  set("#ply-artist", hasTrack ? esc(artist || "未知艺术家") : "从下方队列添加曲目，或到音乐库推送");
  set("#ply-album", hasTrack ? `${icon("disc", 13)}<span class="truncate">${esc(album || "未分类专辑")}</span>` : "");
  set("#ply-fmt", hasTrack ? `${esc(p.codec || (t && t.codec) || "—")} · ${rate ? (rate / 1000).toFixed(1) + " kHz" : "—"} · ${p.channels || (t && t.channels) || "—"} ch${p.buffer_ms != null ? " · 缓冲 " + p.buffer_ms + " ms" : ""}` : "");
  set("#ply-badges", (connected ? badge("ZAT 已连接", "ok") : badge("播放连接不可用", "muted")) +
    (playing ? " " + badge("播放中", "ok") : paused ? " " + badge("已暂停", "warn") : "") +
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
      const lv = $("#ply-vol-v"); if (lv) lv.textContent = String(v);
    }
  }
  const pp = $("#ply-pp");
  if (pp) { pp.disabled = !active; pp.innerHTML = icon(playing ? "pause" : "play", 20); pp.title = playing ? "暂停" : "继续播放"; }
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
    if (dsel.dataset.sig !== sig) { dsel.innerHTML = nodePlayOpts(dv.devs, dv.cur); dsel.dataset.sig = sig; }
    dsel.disabled = !connected;
    if (document.activeElement !== dsel && dsel.value !== dv.cur) dsel.value = dv.cur;
  }
  const cnt = $("#ply-qcnt"); if (cnt) cnt.textContent = `(${tracks.length})`;
  set("#ply-qlist", playerQueueList(id, q));
}
function playerQueueList(id, q) {
  const tracks = (q && q.tracks) || [];
  if (!tracks.length) return empty("music", "队列为空", "点上方「添加曲目」从曲库选择，或载入已有播放列表", true);
  const pos = q.position || 0;
  return `<div class="q-list">` + tracks.map((t, i) => `
    <div class="q-row ${i === pos ? "cur" : ""}">
      <span class="q-idx mono">${i === pos ? icon("speaker", 12) : i + 1}</span>
      <span class="q-info">
        <span class="q-title truncate">${esc(t.title || t.track_id)}${t.missing ? " " + badge("缺失", "warn") : ""}</span>
        <span class="q-sub txt-faint">${esc([t.artist, t.album].filter(Boolean).join(" · ") || "—")}${t.duration_ms ? " · " + fmtDur(t.duration_ms) : ""}</span>
      </span>
      <span class="q-acts">
        <button class="btn xs ghost" data-act="ply-q-up" data-idx="${i}" ${i === 0 ? "disabled" : ""} title="上移">${icon("arrowUp", 12)}</button>
        <button class="btn xs ghost" data-act="ply-q-down" data-idx="${i}" ${i === tracks.length - 1 ? "disabled" : ""} title="下移">${icon("arrowDown", 12)}</button>
        ${i !== pos ? `<button class="btn xs ghost" data-act="queue-jump" data-id="${esc(id)}" data-idx="${i}" title="播放此曲">${icon("play", 12)}</button>` : ""}
        <button class="btn xs ghost" data-act="queue-remove" data-id="${esc(id)}" data-idx="${i}" title="移出队列">${icon("x", 12)}</button>
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
  if (cnt) cnt.textContent = `已选 ${PLY.addSel.size} 首${plyAddFiltered().length > 200 ? " · 仅显示前 200 条，请细化搜索" : ""}`;
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
    </label>`).join("") : empty("search", "没有匹配的可解码曲目", "", true);
  plyAddCnt();
}
async function plyAddGo() {
  const id = PLY.id; if (!id || !PLY.addSel.size) return;
  const ids = Array.from(PLY.addSel);
  const go = $("#ply-add-go"); if (go) { go.disabled = true; go.innerHTML = `${icon("refresh", 14)} 加入中…`; }
  try {
    let started = false;
    for (const tid of ids) {
      const r = await api("POST", `/nodes/${enc(id)}/queue/enqueue`, { track_id: tid });
      if (r && r.session_id) started = true;
    }
    toast(`已加入队列 ${ids.length} 首${started ? " · 已开始播放" : ""}`, "ok");
    PLY.addSel.clear();
    paintPlayerAddList();
    await refreshPlayerBar();
  } catch (e) { toast(e.message, "err"); }
  if (go) { go.innerHTML = `${icon("plus", 14)} 加入队列`; go.disabled = !PLY.addSel.size; }
}
async function plyLoadPlaylist(plID) {
  const id = PLY.id; if (!id || !plID) return;
  let d;
  try { d = await api("GET", `/playlists/${enc(plID)}`); } catch (e) { toast(e.message, "err"); return; }
  const ids = (d.items || []).filter(t => !t.missing).map(t => t.track_id);
  const sel = $("#ply-load"); if (sel) sel.value = "";
  if (!ids.length) { toast("该播放列表为空或曲目已全部缺失", "warn"); return; }
  try { await api("PUT", `/nodes/${enc(id)}/queue`, { track_ids: ids, position: 0 }); }
  catch (e) { toast(e.message, "err"); await refreshPlayerBar(); return; }
  try {
    await api("POST", `/nodes/${enc(id)}/queue/jump`, { index: 0 });
    toast(`已载入「${d.name}」${ids.length} 首并开始播放`, "ok");
  } catch (e) { toast(`已载入「${d.name}」到队列（终端未连接或不可播放）`, "warn"); }
  await refreshPlayerBar();
}
function plySaveQueue() {
  const q = S.nodeQueue[PLY.id];
  const ids = ((q && q.tracks) || []).map(t => t.track_id);
  if (!ids.length) { toast("队列为空，无法保存", "warn"); return; }
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
  "wz-copy-token": async () => { toast(await copyText(WZ.token) ? "令牌已复制" : "复制失败，请手动选择", "ok"); },
  "wz-copy-cmd": async () => { const ok = await copyText(installCmd()); toast(ok ? (WZ.arch === "android" ? "配置信息已复制" : "安装命令已复制") : "复制失败，请手动选择", "ok"); },
  "wz-auth": async ds => { try { await api("POST", `/nodes/${enc(ds.id)}/authorize`); toast("终端已授权", "ok"); refreshCurrent(); } catch (e) { toast(e.message, "err"); } },
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
  "lic-receipt": () => { download("/license/receipt"); toast("已开始导出激活回执", "info"); },
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
    toast("输出设备已设置（下次播放生效）", "ok");
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
    toast("输出设备已设置（下次播放生效）", "ok");
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
    if (r && typeof r.catch === "function") r.catch(err => toast((err && err.message) || "操作失败", "err"));
  } catch (err) { toast((err && err.message) || "操作失败", "err"); }
}
function onChange(e) {
  const el = e.target.closest("[data-change]");
  if (!el) return;
  const fn = CHG[el.dataset.change];
  if (!fn) return;
  try {
    const r = fn(el, el.dataset);
    if (r && typeof r.catch === "function") r.catch(err => toast((err && err.message) || "操作失败", "err"));
  } catch (err) { toast((err && err.message) || "操作失败", "err"); }
}
function onInput(e) {
  const t = e.target;
  if (t.id === "global-search") { S.search = t.value; paintSearch(); return; }
  if (t.id === "pb-seek" || t.id === "pb-volume") {
    playerBarBeginRange(t);
    const volume = t.id === "pb-volume", label = $(volume ? "#pb-volume-value" : "#pb-elapsed");
    if (label) label.textContent = volume ? t.value : fmtDur(Number(t.value));
    t.setAttribute("aria-valuetext", volume ? t.value + "%" : fmtDur(Number(t.value)));
    return;
  }
  if (t.id === "ply-seek") { PLY.dragging = true; const el = $("#ply-el"); if (el) el.textContent = fmtDur(Number(t.value)); return; }
  if (t.id === "ply-vol") { PLY.volDrag = true; const v = $("#ply-vol-v"); if (v) v.textContent = t.value; return; }
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
