"use strict";
/* Zephyr Music Player · Operations Console · vanilla ES2020, no deps */

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
const api = (method, path, body) => req(method, path, body === undefined ? {} : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
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
  nodeApps: {}, nodeOs: {},
  nodeFilter: "all", nodeSort: { key: "alias", dir: 1 }, sel: new Set(),
  alertFilter: "open", auditFilter: "all",
  jobBom: "", jobSel: new Set(),
  mounts: [], tracks: [], libMount: "", play: {}
};
const NAV = [
  { id: "overview", name: "总览", icon: "gauge" },
  { id: "library", name: "音乐库", icon: "music" },
  { id: "nodes", name: "终端管理", icon: "server" },
  { id: "catalog", name: "应用仓库", icon: "box" },
  { id: "deliver", name: "服务下发", icon: "send" },
  { id: "keys", name: "密钥与授权", icon: "key" },
  { id: "audit", name: "审计", icon: "audit" }
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
  return ({ overview: shellOverview, library: shellLibrary, nodes: shellNodes, catalog: shellCatalog, deliver: shellDeliver, keys: shellKeys, audit: shellAudit }[v] || shellOverview)();
}
async function refreshFor(v, silent) {
  try {
    if (v === "overview") await refreshOverview();
    else if (v === "library") await refreshLibrary();
    else if (v === "nodes") await refreshNodes();
    else if (v === "catalog") await refreshCatalog();
    else if (v === "deliver") await refreshDeliver();
    else if (v === "keys") await refreshKeys();
    else if (v === "audit") await refreshAudit();
  } catch (e) { if (!silent) viewError(e); }
}
function refreshCurrent() { return refreshFor(S.view, true); }
function setView(v) {
  stopPrefix("view:");
  closeModal(); closeDrawer();
  S.view = v; S.search = ""; S.sel.clear();
  const gs = $("#global-search"); if (gs) gs.value = "";
  $$("#nav .nav-item").forEach(b => b.classList.toggle("active", b.dataset.view === v));
  const el = $("#view");
  el.dataset.view = v;
  el.innerHTML = shellFor(v);
  el.classList.remove("view-enter"); void el.offsetWidth; el.classList.add("view-enter");
  setTimeout(() => el.classList.remove("view-enter"), 1000);
  refreshFor(v);
  if (v === "overview" || v === "nodes") poll("view:main", () => refreshFor(v, true), 4000);
  else if (v === "deliver") poll("view:main", () => refreshDeliverLive(), 2000);
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
const AUDIO_SVCS = [
  { id: "shairport-sync", name: "AirPlay", sub: "shairport-sync", ic: "music", color: "green" },
  { id: "roonbridge", name: "Roon Bridge", sub: "roonbridge", ic: "disc", color: "violet" },
  { id: "networkaudiod", name: "NAA 网播", sub: "networkaudiod", ic: "speaker", color: "cyan" },
  { id: "upmpdcli", name: "UPnP / DLNA", sub: "upmpdcli", ic: "globe", color: "blue" }
];
function shellOverview() {
  return `<div class="kpis stagger" style="--i:0" id="ov-kpis"></div>
    <div class="grid cols-2">
      <section class="card stagger" style="--i:1" id="ov-server"></section>
      <section class="card stagger" style="--i:2" id="ov-audio"></section>
    </div>
    <section class="card mt16 stagger" style="--i:3" id="ov-nodes"></section>`;
}
async function refreshOverview() {
  const nodes = await api("GET", "/nodes");
  S.nodes = Array.isArray(nodes) ? nodes : [];
  const [metrics, jobs, alerts] = await Promise.all([
    rootJson("/metrics/server").catch(() => null),
    api("GET", "/jobs").catch(() => []),
    api("GET", "/alerts").catch(() => [])
  ]);
  S.metrics = metrics || null; S.jobs = Array.isArray(jobs) ? jobs : []; S.alerts = Array.isArray(alerts) ? alerts : [];
  paintBell();
  await Promise.all(S.nodes.filter(n => n.status === "online").map(async n => {
    const [a, o] = await Promise.all([
      api("GET", `/nodes/${enc(n.node_id)}/apps`).catch(() => null),
      api("GET", `/nodes/${enc(n.node_id)}/osupdates`).catch(() => null)
    ]);
    if (a) S.nodeApps[n.node_id] = Array.isArray(a) ? a : (a.apps || []);
    if (o) S.nodeOs[n.node_id] = o;
  }));
  paintOverview();
}
function paintOverview() { paintOverviewKpis(); paintServerCard(); paintAudio(); paintOverviewNodes(); }
function paintOverviewKpis() {
  const el = $("#ov-kpis"); if (!el) return;
  const online = S.nodes.filter(n => n.status === "online").length;
  const playing = S.nodes.filter(n => n.playing).length;
  const running = S.jobs.filter(jobActive).length;
  let pkgs = 0; S.nodes.forEach(n => { const o = S.nodeOs[n.node_id]; if (o && Array.isArray(o.packages)) pkgs += o.packages.length; });
  const open = S.alerts.filter(a => a.status === "open").length;
  el.innerHTML =
    kpi("在线终端", online, "green", "server", "/ " + S.nodes.length) +
    kpi("播放中", playing, "cyan", "play") +
    kpi("运行任务", running, "blue", "send") +
    kpi("待升级包", pkgs, "amber", "download") +
    kpi("未处理告警", open, open ? "red" : "muted", "bell");
}
function paintServerCard() {
  const el = $("#ov-server"); if (!el) return;
  const m = S.metrics;
  if (!m) { el.innerHTML = secHead(icon("cpu", 17) + " 服务器消耗") + empty("cpu", "暂无服务器指标", "", true); return; }
  const cpu = Number(m.cpu_percent) || 0, mem = Number(m.mem_mb) || 0;
  el.innerHTML = secHead(icon("cpu", 17) + " 服务器消耗", badge("v" + (m.version || "—"), "gold")) +
    meter("CPU 占用", cpu, cpu.toFixed(1) + " %", "cyan") +
    `<div class="kv mt12">` +
    kvRow("内存", `<span class="mono">${mem.toFixed(0)} MB</span>`) +
    kvRow("Goroutines", `<span class="mono">${m.goroutines ?? "—"}</span>`) +
    kvRow("数据库", `<span class="mono">${fmtBytes(m.db_bytes)}</span>`) +
    kvRow("运行时长", `<span class="mono">${fmtUp(m.uptime_s)}</span>`) +
    kvRow("签名模式", esc(sigModeZh(m.signature_mode))) +
    kvRow("播放门禁", m.play_gate ? badge("开", "ok") : badge("关", "muted")) +
    `</div>`;
}
function nodeAppIds(n) {
  let apps = n.apps;
  if (!Array.isArray(apps)) apps = S.nodeApps[n.node_id] || [];
  return apps.map(a => typeof a === "string" ? a : (a.app_id || a.id || "")).filter(Boolean);
}
function paintAudio() {
  const el = $("#ov-audio"); if (!el) return;
  const rows = AUDIO_SVCS.map(s => {
    const nodes = S.nodes.filter(n => nodeAppIds(n).includes(s.id));
    const chips = nodes.length
      ? nodes.map(n => `<button class="svc-chip" data-act="node-open" data-id="${esc(n.node_id)}">${n.playing ? '<span class="sdot ok live"></span>' : ""}${esc(n.alias || shortId(n.node_id))}</button>`).join("")
      : `<span class="svc-none">未部署</span>`;
    return `<div class="svc-row"><div class="svc-ic" style="--sc:var(--${s.color})">${icon(s.ic, 18)}</div><div class="svc-info"><div class="svc-name">${esc(s.name)}</div><div class="svc-id">${esc(s.sub)}</div></div><div class="svc-nodes">${chips}</div></div>`;
  }).join("");
  el.innerHTML = secHead(icon("music", 17) + " 核心音频服务", `<span class="hint">${S.nodes.length} 台终端</span>`) + rows;
}
function paintOverviewNodes() {
  const el = $("#ov-nodes"); if (!el) return;
  const nodes = S.nodes.filter(n => hit(n.alias, n.node_id, n.group, n.ip));
  const head = secHead(icon("server", 17) + " 终端矩阵", `<button class="btn sm ghost" data-act="nav" data-view="nodes">管理终端 ${icon("chevR", 13)}</button>`);
  if (!nodes.length) {
    el.innerHTML = head + empty("server", S.nodes.length ? "没有匹配的终端" : "还没有终端，添加第一台 Hi-Fi 终端开始组网", S.nodes.length ? "" : `<button class="btn primary" data-act="goto-add-node">${icon("plus", 15)} 添加终端</button>`);
    return;
  }
  el.innerHTML = head + `<div class="node-grid">` + nodes.map(n => {
    const cpu = Number(n.cpu) || 0, mem = Number(n.mem_mb) || 0;
    return `<div class="node-card ${n.playing ? "playing" : ""} ${n.status !== "online" ? "offline" : ""}" data-act="node-open" data-id="${esc(n.node_id)}">
      <div class="n-top"><span class="sdot ${n.status === "online" ? "ok live" : "err"}"></span><span class="n-alias">${esc(n.alias || shortId(n.node_id))}</span>${n.playing ? badge("播放", "ok") : ""}</div>
      <div class="n-id">${esc(n.node_id)}</div>
      <div class="n-meters">${meter("CPU", cpu, cpu.toFixed(0) + "%", "cyan")}${meter("内存", clamp(mem / 2048 * 100, 0, 100), mem.toFixed(0) + "M", "violet")}</div>
      <div class="n-meta">${statusBadge(n.status)}<span>${esc(n.ip || "—")}</span><span class="spacer"></span><span>${ago(n.last_seen)}</span></div>
    </div>`;
  }).join("") + `</div>`;
}

/* ================= NODES ================= */
function shellNodes() {
  return `<div class="kpis stagger" style="--i:0" id="nd-stats"></div>
    <section class="card stagger" style="--i:1">
      <div class="sec-head">
        <div class="sec-title">${icon("server", 17)} 终端列表</div>
        <div class="spacer"></div>
        <div class="chips" id="nd-chips"></div>
        <button class="btn primary sm" data-act="wz-open">${icon("plus", 14)} 添加终端</button>
      </div>
      <div id="nd-bulk"></div>
      <div id="nd-table"></div>
    </section>`;
}
async function refreshNodes() {
  const nodes = await api("GET", "/nodes");
  S.nodes = Array.isArray(nodes) ? nodes : [];
  paintNodes();
}
function filteredNodes() {
  let list = S.nodes.slice();
  if (S.nodeFilter === "online") list = list.filter(n => n.status === "online");
  else if (S.nodeFilter === "offline") list = list.filter(n => n.status !== "online");
  else if (S.nodeFilter === "playing") list = list.filter(n => n.playing);
  list = list.filter(n => hit(n.alias, n.node_id, n.group, n.ip, n.arch, n.distro, n.bom_id));
  const { key, dir } = S.nodeSort;
  list.sort((a, b) => {
    let va, vb;
    if (key === "bom") { va = (a.bom_id || "") + "@" + (a.bom_version || 0); vb = (b.bom_id || "") + "@" + (b.bom_version || 0); }
    else if (key === "last_seen") { va = ts(a.last_seen); vb = ts(b.last_seen); }
    else if (key === "authorized") { va = a.authorized ? 1 : 0; vb = b.authorized ? 1 : 0; }
    else { va = a[key]; vb = b[key]; }
    if (typeof va === "number" && typeof vb === "number") return (va - vb) * dir;
    return String(va ?? "").localeCompare(String(vb ?? ""), "zh") * dir;
  });
  return list;
}
function paintNodes() { paintNodeStats(); paintNodeChips(); paintBulkBar(); paintNodeTable(); }
function paintNodeStats() {
  const el = $("#nd-stats"); if (!el) return;
  const total = S.nodes.length, online = S.nodes.filter(n => n.status === "online").length;
  const playing = S.nodes.filter(n => n.playing).length, auth = S.nodes.filter(n => n.authorized).length;
  el.innerHTML =
    kpi("全部终端", total, "amber", "server") +
    kpi("在线", online, "green", "activity") +
    kpi("离线", total - online, "red", "power") +
    kpi("播放中", playing, "cyan", "play") +
    kpi("已授权", auth, "violet", "shield");
}
function paintNodeChips() {
  const el = $("#nd-chips"); if (!el) return;
  const c = { all: S.nodes.length, online: S.nodes.filter(n => n.status === "online").length, offline: S.nodes.filter(n => n.status !== "online").length, playing: S.nodes.filter(n => n.playing).length };
  el.innerHTML = [["all", "全部"], ["online", "在线"], ["offline", "离线"], ["playing", "播放中"]]
    .map(([f, label]) => `<button class="chip ${S.nodeFilter === f ? "active" : ""}" data-act="nd-filter" data-f="${f}">${label}<span class="cnt">${c[f]}</span></button>`).join("");
}
function paintBulkBar() {
  const el = $("#nd-bulk"); if (!el) return;
  if (!S.sel.size) { el.innerHTML = ""; return; }
  el.innerHTML = `<div class="bulk-bar">
    <span class="b-cnt">已选 ${S.sel.size} 台</span>
    <button class="btn xs" data-act="bulk-auth">${icon("shield", 13)} 批量授权</button>
    <button class="btn xs" data-act="bulk-revoke">${icon("unlock", 13)} 批量撤销</button>
    <button class="btn xs danger" data-act="bulk-delete">${icon("trash", 13)} 批量注销</button>
    <span class="spacer"></span>
    <button class="btn xs ghost" data-act="nd-clear-sel">取消选择</button>
  </div>`;
}
function sInd(key) { return S.nodeSort.key === key ? icon(S.nodeSort.dir > 0 ? "arrowUp" : "arrowDown", 12, "s-ind") : ""; }
function paintNodeTable() {
  const el = $("#nd-table"); if (!el) return;
  const list = filteredNodes();
  const allSel = list.length > 0 && list.every(n => S.sel.has(n.node_id));
  const someSel = list.some(n => S.sel.has(n.node_id));
  const head = `<div class="tbl-wrap"><table class="tbl"><thead><tr>
    <th style="width:36px"><label class="check"><input type="checkbox" data-change="node-sel-all" ${allSel ? "checked" : ""}><span class="box"></span></label></th>
    <th class="sortable" data-act="nd-sort" data-key="alias">终端 ${sInd("alias")}</th>
    <th class="sortable" data-act="nd-sort" data-key="status">状态 ${sInd("status")}</th>
    <th>播放</th>
    <th class="sortable" data-act="nd-sort" data-key="arch">架构 ${sInd("arch")}</th>
    <th class="sortable" data-act="nd-sort" data-key="bom">BOM ${sInd("bom")}</th>
    <th class="sortable" data-act="nd-sort" data-key="authorized">授权 ${sInd("authorized")}</th>
    <th class="sortable" data-act="nd-sort" data-key="last_seen">最近心跳 ${sInd("last_seen")}</th>
    <th style="text-align:right">操作</th>
  </tr></thead><tbody>`;
  const rows = list.length ? list.map(nodeRow).join("")
    : `<tr><td colspan="9" class="empty-cell">${empty("server", S.nodes.length ? "没有匹配的终端" : "还没有终端，点击「添加终端」开始", S.nodes.length ? "" : `<button class="btn primary" data-act="wz-open">${icon("plus", 15)} 添加终端</button>`, true)}</td></tr>`;
  el.innerHTML = head + rows + `</tbody></table></div>`;
  const all = $('[data-change="node-sel-all"]'); if (all) all.indeterminate = !allSel && someSel;
}
function nodeRow(n) {
  const id = n.node_id, sel = S.sel.has(id);
  return `<tr class="clickable ${sel ? "selected" : ""}" data-act="node-open" data-id="${esc(id)}">
    <td><label class="check"><input type="checkbox" data-change="node-sel" data-id="${esc(id)}" ${sel ? "checked" : ""}><span class="box"></span></label></td>
    <td><div>${esc(n.alias || shortId(id))}</div><div class="sub mono">${esc(shortId(id))}${n.group ? " · " + esc(n.group) : ""}</div></td>
    <td>${statusBadge(n.status)}</td>
    <td>${n.playing ? badge("播放中" + (n.active_proto ? " · " + n.active_proto : ""), "ok") : '<span class="txt-faint">空闲</span>'}</td>
    <td><div>${esc(n.arch || "—")}</div><div class="sub">${esc(n.distro || "")}</div></td>
    <td class="cell-mono">${n.bom_id ? esc(n.bom_id) + "@" + (n.bom_version || 0) : "—"}</td>
    <td>${authBadge(n)}</td>
    <td class="nowrap">${ago(n.last_seen)}</td>
    <td><div class="acts">
      <button class="btn xs ghost" data-act="node-config" data-id="${esc(id)}" title="配置期望态">${icon("sliders", 13)}</button>
      <button class="btn xs ghost" data-act="node-os" data-id="${esc(id)}" title="系统更新">${icon("download", 13)}</button>
      <button class="btn xs ghost" data-act="node-net" data-id="${esc(id)}" title="网络诊断">${icon("wifi", 13)}</button>
      <button class="btn xs ghost" data-act="node-console" data-id="${esc(id)}" title="终端界面">${icon("terminal", 13)}</button>
    </div></td>
  </tr>`;
}

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
function installCmd() { return `curl -fsSL ${location.origin}/dl/agent-install.sh | sudo bash -s -- --server ${location.origin} --token ${WZ.token}`; }
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
      <div class="field"><label>架构</label><select id="wz-arch" class="inp">${opt("amd64", "amd64 (x86_64)", WZ.arch)}${opt("arm64", "arm64 (aarch64)", WZ.arch)}${opt("armhf", "armhf", WZ.arch)}${opt("armv7hf", "armv7hf", WZ.arch)}</select></div>
      <div class="field"><label>发行版</label><select id="wz-distro" class="inp">${opt("debian", "Debian", WZ.distro)}${opt("ubuntu", "Ubuntu", WZ.distro)}${opt("raspberry-pi-os", "Raspberry Pi OS", WZ.distro)}${opt("armbian", "Armbian", WZ.distro)}${opt("other", "其他", WZ.distro)}</select></div>
    </div>
    <div class="f-row">
      <div class="field"><label>令牌有效期</label><select id="wz-ttl" class="inp">${opt("600", "10 分钟", String(WZ.ttl))}${opt("1800", "30 分钟", String(WZ.ttl))}${opt("3600", "1 小时", String(WZ.ttl))}${opt("86400", "24 小时", String(WZ.ttl))}</select></div>
      <div class="field"><label>备注</label><input id="wz-note" class="inp" placeholder="可选" value="${esc(WZ.note)}" maxlength="120"></div>
    </div>
    <div class="f-hint">生成一次性令牌后，在目标主机执行安装命令；Agent 注册成功会自动进入下一步。</div>`;
}

function wzStep2() {
  return `<div class="field"><label>安装令牌</label>
      <div class="token-box"><span class="tok" id="wz-token">${esc(WZ.token)}</span><button class="btn xs" data-act="wz-copy-token">${icon("copy", 13)} 复制</button></div>
      <div class="f-hint">剩余有效时间 <span class="wz-count" id="wz-cd">--:--</span> · 一次性，注册后失效</div>
    </div>
    <div class="field"><label>在目标主机执行（需要 sudo）</label>
      <div class="cmd-box"><span class="c-prompt">$</span><span id="wz-cmd">${esc(installCmd())}</span></div>
      <button class="btn sm block" data-act="wz-copy-cmd">${icon("copy", 14)} 复制安装命令</button>
    </div>
    <div class="waiting"><span class="w-pulse"></span><div style="flex:1"><div class="w-txt">等待 Agent 注册…</div><div class="w-sub">每 2 秒自动检测令牌使用状态</div></div><span class="spin"></span></div>`;
}
function wzStep3() {
  const n = WZ.node || { node_id: WZ.nodeId, alias: WZ.alias };
  return `<div class="banner ok">${icon("check", 16)}<div><b>${esc(n.alias || shortId(n.node_id))}</b> 已成功注册并上线。</div></div>
    <div class="tiles c2">
      <button class="tile" style="--tc:var(--green)" data-act="wz-auth" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("shield", 16)}</span><span class="t-name">授权终端</span><span class="t-sub">纳入许可席位管理</span></button>
      <button class="tile" style="--tc:var(--amber)" data-act="wz-config" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("sliders", 16)}</span><span class="t-name">配置期望态</span><span class="t-sub">绑定 BOM / DAC / 保护</span></button>
      <button class="tile" style="--tc:var(--cyan)" data-act="wz-console" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("terminal", 16)}</span><span class="t-name">打开控制台</span><span class="t-sub">远程终端界面</span></button>
      <button class="tile" style="--tc:var(--violet)" data-act="wz-detail" data-id="${esc(n.node_id)}"><span class="t-ic">${icon("eye", 16)}</span><span class="t-name">查看详情</span><span class="t-sub">资源与已装应用</span></button>
    </div>`;
}
async function wzNext() {
  WZ.alias = val("wz-alias").trim(); WZ.group = val("wz-group").trim();
  WZ.arch = val("wz-arch"); WZ.distro = val("wz-distro"); WZ.ttl = Number(val("wz-ttl")) || 1800; WZ.note = val("wz-note").trim();
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
const CMD_ZH = { "os-check": "检查更新", "os-upgrade": "系统升级", netdiag: "网络自检" };

/* ================= NODE DRAWER ================= */
async function openNodeDrawer(id) {
  let n; try { n = await api("GET", `/nodes/${enc(id)}`); } catch (e) { toast(e.message, "err"); return; }
  let apps = [];
  try { const a = await api("GET", `/nodes/${enc(id)}/apps`); apps = Array.isArray(a) ? a : (a && a.apps) || []; } catch (e) { /* aux */ }
  S.nodeApps[id] = apps;
  openDrawer(esc(n.alias || shortId(id)), esc(id), nodeDrawerBody(n, apps),
    `<button class="btn primary" data-act="node-console" data-id="${esc(id)}">${icon("terminal", 15)} 控制该主机</button>`,
    { ctx: { kind: "node", id }, onClose: () => stopPoll("node:poll") });
  poll("node:poll", async () => {
    if (!drawerCtx || drawerCtx.kind !== "node" || drawerCtx.id !== id) return;
    try {
      const nn = await api("GET", `/nodes/${enc(id)}`);
      let aa = S.nodeApps[id] || [];
      try { const a = await api("GET", `/nodes/${enc(id)}/apps`); aa = Array.isArray(a) ? a : (a && a.apps) || []; } catch (e) { /* aux */ }
      S.nodeApps[id] = aa;
      setDrawerBody(nodeDrawerBody(nn, aa));
      setDrawerTitle(esc(nn.alias || shortId(id)), esc(id));
    } catch (e) { /* keep last good paint */ }
  }, 4000);
}
function nodeDrawerBody(n, apps) {
  const id = n.node_id;
  let h = "";
  if (n.status !== "online") {
    h += `<div class="banner warn">${icon("power", 15)}<div><b>终端离线</b> · 最近心跳 ${ago(n.last_seen)}<br>检查电源与网络，并在主机运行 <span class="mono">systemctl status zephyr-agent</span>；离线判定阈值 ${n.offline_after || 45} 秒。<div class="mt8"><button class="btn xs" data-act="node-net" data-id="${esc(id)}">${icon("wifi", 12)} 网络诊断</button></div></div></div>`;
  }
  h += `<div class="tiles c4 mb12">` +
    tile("sliders", "配置", "期望态 / DAC", "node-config", "amber", id) +
    tile("download", "系统更新", "检查 / 升级", "node-os", "green", id) +
    tile("wifi", "网络诊断", "一键自检", "node-net", "cyan", id) +
    tile("terminal", "终端界面", "远程控制", "node-console", "violet", id) +
    `</div>`;
  const cpu = Number(n.cpu) || 0, mem = Number(n.mem_mb) || 0, temp = Number(n.temp_c) || 0;
  h += `<div class="d-sec"><span class="lbl">资源</span>` +
    meter("CPU", cpu, cpu.toFixed(1) + " %", "cyan") +
    meter("内存", clamp(mem / 2048 * 100, 0, 100), mem.toFixed(0) + " MB", "violet") +
    (temp ? meter("温度", clamp(temp / 90 * 100, 0, 100), temp.toFixed(1) + " °C", temp > 70 ? "red" : "green") : "") +
    `</div>`;
  h += `<div class="d-sec"><span class="lbl">事实</span><div class="kv">` +
    kvRow("状态", statusBadge(n.status) + (n.playing ? " " + badge("播放中" + (n.active_proto ? " · " + n.active_proto : ""), "ok") : "")) +
    kvRow("授权", authBadge(n) + (n.authorized_at ? ` <span class="txt-faint">${ago(n.authorized_at)}</span>` : "")) +
    kvRow("IP", `<span class="mono">${esc(n.ip || "—")}</span>`) +
    kvRow("架构", `${esc(n.arch || "—")} · ${esc(n.distro || "—")}`) +
    kvRow("内核", `<span class="mono">${esc(n.kernel || "—")}</span>`) +
    kvRow("Agent", `<span class="mono">${esc(n.agent_version || "—")}</span>`) +
    kvRow("分组", esc(n.group || "—")) +
    kvRow("期望态", n.bom_id ? `<span class="mono">${esc(n.bom_id)}@${n.bom_version || 0}</span>` : "—") +
    kvRow("DAC 仲裁", esc(n.dac_arbitration === "auto" ? "自动" : n.dac_arbitration === "manual" ? "手动" : (n.dac_arbitration || "—"))) +
    kvRow("DAC 设备", `<span class="mono">${esc(n.dac_device || "—")}</span>`) +
    kvRow("DAC 占用", n.dac_holder_pid ? `<span class="mono">PID ${n.dac_holder_pid}</span>` : "空闲") +
    kvRow("播放保护", n.play_protect ? badge("开", "ok") : badge("关", "muted")) +
    kvRow("自动系统更新", n.auto_os_update ? badge("开", "ok") : badge("关", "muted")) +
    kvRow("更新窗口", `<span class="mono">${esc(n.os_window || "—")}</span>`) +
    kvRow("注册时间", fmtTime(n.registered_at)) +
    kvRow("最近心跳", fmtTime(n.last_seen)) +
    (n.note ? kvRow("备注", esc(n.note)) : "") +
    `</div></div>`;
  h += `<div class="d-sec"><span class="lbl">已安装应用 (${apps.length})</span>`;
  if (apps.length) {
    h += `<div class="tbl-wrap"><table class="tbl" style="min-width:0"><thead><tr><th>应用</th><th>版本</th><th>服务</th><th>状态</th></tr></thead><tbody>` +
      apps.map(a => `<tr><td class="cell-mono">${esc(a.app_id || a.id || "—")}</td><td class="cell-mono">${esc(a.version || "—")}</td><td class="cell-mono">${esc(a.unit || "—")}<div class="sub">${esc(a.source || "")}</div></td><td>${a.enabled ? badge("启用", "ok") : badge("停用", "muted")}</td></tr>`).join("") +
      `</tbody></table></div>`;
  } else h += empty("box", "暂无已安装应用上报", "", true);
  h += `</div>`;
  return h;
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
  let h = "";
  if (!lic || lic.status === "none") h += `<div class="banner warn">${icon("lock", 15)}<span>服务器尚未激活授权，服务下发将被拒绝。</span><button class="btn violet xs" data-act="goto-keys">前往激活</button></div>`;
  else if (lic.status === "expired") h += `<div class="banner err">${icon("alert", 15)}<span>授权已过期，请重新激活。</span><button class="btn violet xs" data-act="goto-keys">前往激活</button></div>`;
  else {
    const seats = Number(lic.seats) || 0;
    const pct = seats > 0 ? clamp(used / seats * 100, 0, 100) : 0;
    h += `<div class="seats"><div class="s-top"><span>已授权席位</span><span class="mono">${used} / ${seats || "∞"}</span></div><div class="seat-bar"><div class="seat-fill ${seats && used > seats ? "over" : ""}" style="width:${pct}%"></div></div></div>`;
    if (seats && used >= seats && !n.authorized) h += `<div class="f-hint txt-warn">席位已满，授权该终端前请先释放其他席位。</div>`;
  }
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
  setModalFoot(`<button class="btn ghost" data-act="app-upload" data-id="${esc(id)}">${icon("upload", 15)} 上传新版本</button><div class="spacer"></div><button class="btn primary" data-act="modal-close">关闭</button>`);
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

/* ================= DELIVER ================= */
function shellDeliver() {
  return `<div id="dl-gate"></div>
    <section class="card stagger" style="--i:0">
      ${secHead(`${icon("send", 17)} 服务下发`, `<span class="hint">选择期望态 → 勾选终端 → 执行部署</span>`)}
      <div class="dl-steps">
        <div class="dl-step" id="dl-s1">
          <div class="ds-head"><span class="ds-n">1</span><span class="ds-t">期望态 BOM</span></div>
          <div class="field"><select id="dl-bom" class="inp" data-change="dl-bom"></select><div class="f-hint">仅列出已发布的 BOM 版本。</div></div>
        </div>
        <div class="dl-step" id="dl-s2">
          <div class="ds-head"><span class="ds-n">2</span><span class="ds-t">目标终端</span><span class="spacer"></span><span class="tag-mono" id="dl-cnt">0 台</span></div>
          <label class="check mb12"><input type="checkbox" id="dl-nodes-all" data-change="dl-node-all"><span class="box"></span><span class="sw-lbl">全选已授权终端</span></label>
          <div class="node-pick" id="dl-nodes"></div>
        </div>
        <div class="dl-step" id="dl-s3">
          <div class="ds-head"><span class="ds-n">3</span><span class="ds-t">执行</span></div>
          <div id="dl-summary" class="mb12"></div>
          <button class="btn primary block" data-act="dl-submit" id="dl-submit">${icon("send", 15)} 下发部署任务</button>
        </div>
      </div>
    </section>
    <section class="card mt16 stagger" style="--i:1">
      ${secHead(`${icon("activity", 17)} 任务队列`, `<span class="hint">每 2 秒刷新</span>`)}
      <div id="dl-jobs"></div>
    </section>
    <section class="card mt16 stagger" style="--i:2">
      ${secHead(`${icon("branch", 17)} 灰度发布`, `<button class="btn primary sm" data-act="ro-create">${icon("plus", 14)} 新建灰度</button>`)}
      <div id="dl-rollouts"></div>
    </section>`;
}
async function refreshDeliver() {
  const [boms, nodes] = await Promise.all([api("GET", "/boms?status=published"), api("GET", "/nodes")]);
  S.pubBoms = Array.isArray(boms) ? boms : [];
  S.nodes = Array.isArray(nodes) ? nodes : [];
  const [jobs, rollouts, lic] = await Promise.all([
    api("GET", "/jobs").catch(() => []),
    api("GET", "/rollouts").catch(() => []),
    api("GET", "/license").catch(() => null)
  ]);
  S.jobs = Array.isArray(jobs) ? jobs : [];
  S.rollouts = Array.isArray(rollouts) ? rollouts : [];
  S.license = lic;
  paintDlGate(); paintDlForm(); paintJobsStrip(); paintRollouts();
}
async function refreshDeliverLive() {
  try {
    const [jobs, rollouts] = await Promise.all([api("GET", "/jobs"), api("GET", "/rollouts").catch(() => S.rollouts)]);
    S.jobs = Array.isArray(jobs) ? jobs : [];
    S.rollouts = Array.isArray(rollouts) ? rollouts : [];
    paintJobsStrip(); paintRollouts();
    if (drawerCtx && drawerCtx.kind === "job") repaintJob(drawerCtx.id);
  } catch (e) { /* 连接状态由 api 层处理 */ }
}
function paintDlGate() {
  const el = $("#dl-gate"); if (!el) return;
  const lic = S.license;
  if (!lic || lic.status === "none") el.innerHTML = `<div class="banner warn stagger" style="--i:0">${icon("lock", 16)}<span>服务器尚未激活授权，无法下发任务。请先在「密钥与授权」激活许可证。</span><button class="btn violet sm" data-act="goto-keys">前往激活</button></div>`;
  else if (lic.status === "expired") el.innerHTML = `<div class="banner err stagger" style="--i:0">${icon("alert", 16)}<span>授权已过期，下发被拒绝。请重新激活许可证。</span><button class="btn violet sm" data-act="goto-keys">前往激活</button></div>`;
  else el.innerHTML = "";
}
function paintDlForm() {
  const bomSel = $("#dl-bom"); if (!bomSel) return;
  const pub = (S.pubBoms || []).slice().sort((a, b) => a.bom_id === b.bom_id ? b.version - a.version : String(a.bom_id).localeCompare(String(b.bom_id)));
  if (!pub.some(b => `${b.bom_id}@${b.version}` === S.jobBom)) S.jobBom = pub.length ? `${pub[0].bom_id}@${pub[0].version}` : "";
  bomSel.innerHTML = pub.length
    ? pub.map(b => { const v = `${b.bom_id}@${b.version}`; return `<option value="${esc(v)}" ${v === S.jobBom ? "selected" : ""}>${esc(v)} · ${esc(b.role || "默认")} · ${(b.apps || []).length} 应用</option>`; }).join("")
    : `<option value="">暂无已发布 BOM</option>`;
  const authNodes = S.nodes.filter(n => n.authorized);
  for (const id of Array.from(S.jobSel)) if (!authNodes.some(n => n.node_id === id)) S.jobSel.delete(id);
  const pick = $("#dl-nodes");
  if (!authNodes.length) pick.innerHTML = empty("server", "暂无已授权终端", `<button class="btn primary sm" data-act="goto-add-node">${icon("plus", 13)} 添加并授权终端</button>`, true);
  else pick.innerHTML = authNodes.map(n => `<label class="${n.status === "online" ? "" : "disabled"}"><input type="checkbox" data-change="dl-node" data-id="${esc(n.node_id)}" ${S.jobSel.has(n.node_id) ? "checked" : ""}><span class="box"></span><span class="np-alias">${esc(n.alias || shortId(n.node_id))}</span><span class="np-meta">${esc(n.arch || "")} · ${n.status === "online" ? "在线" : "离线"}</span></label>`).join("");
  const all = $("#dl-nodes-all"); if (all) { all.checked = authNodes.length > 0 && S.jobSel.size === authNodes.length; all.indeterminate = S.jobSel.size > 0 && S.jobSel.size < authNodes.length; }
  $("#dl-cnt").textContent = `${S.jobSel.size} 台`;
  $("#dl-s1").className = "dl-step " + (S.jobBom ? "done" : "active");
  $("#dl-s2").className = "dl-step " + (S.jobSel.size ? "done" : S.jobBom ? "active" : "");
  $("#dl-s3").className = "dl-step " + (S.jobBom && S.jobSel.size ? "active" : "");
  const bom = pub.find(b => `${b.bom_id}@${b.version}` === S.jobBom);
  $("#dl-summary").innerHTML = S.jobBom && S.jobSel.size
    ? `<div class="kv">${kvRow("期望态", `<span class="mono">${esc(S.jobBom)}</span>`)}${kvRow("应用数", `<span class="mono">${bom ? (bom.apps || []).length : 0}</span>`)}${kvRow("终端", `<span class="mono">${S.jobSel.size} 台</span>`)}${kvRow("类型", badge("部署 provision", "info"))}</div>`
    : `<div class="hint">完成前两步后在此确认下发摘要。</div>`;
  const btn = $("#dl-submit"); if (btn) btn.disabled = !(S.jobBom && S.jobSel.size);
}
async function dlSubmit() {
  if (!S.jobBom || !S.jobSel.size) return;
  const at = S.jobBom.lastIndexOf("@");
  const bom_id = S.jobBom.slice(0, at), bom_version = Number(S.jobBom.slice(at + 1)) || 0;
  const ids = Array.from(S.jobSel);
  const btn = $("#dl-submit");
  if (btn) { btn.disabled = true; btn.innerHTML = `<span class="spin">${icon("refresh", 15)}</span> 下发中…`; }
  let ok = 0, gateErr = ""; const failed = [];
  for (const id of ids) {
    try { await api("POST", "/jobs", { bom_id, bom_version, node_ids: [id], node_id: id, kind: "provision" }); ok++; }
    catch (e) {
      failed.push(`${shortId(id)}：${e.message}`);
      if (/许可证|授权|席位|license/i.test(e.message)) gateErr = e.message;
    }
  }
  if (gateErr) {
    const g = $("#dl-gate");
    if (g) g.innerHTML = `<div class="banner err">${icon("lock", 16)}<span>${esc(gateErr)}</span><button class="btn violet sm" data-act="goto-keys">前往密钥与授权</button></div>`;
  }
  if (ok) toast(`已下发 ${ok} 个部署任务`, "ok");
  if (failed.length) toast("部分任务下发失败 — " + failed.join("；"), "err");
  S.jobSel.clear();
  if (btn) btn.innerHTML = `${icon("send", 15)} 下发部署任务`;
  await refreshDeliverLive(); paintDlForm();
}
const jobNodes = j => (Array.isArray(j.node_ids) && j.node_ids.length) ? j.node_ids : (j.node_id ? [j.node_id] : []);
function jobProgress(j) {
  if (jobState(j) === "done") return 100;
  const ph = j.phases || [];
  if (!ph.length) return jobActive(j) ? 5 : 0;
  const done = ph.filter(p => p.state === "ok" || p.state === "skipped").length;
  return clamp(Math.round(done / ph.length * 100), 0, 100);
}
function currentPhase(j) {
  const ph = j.phases || [];
  return ph.find(p => p.state === "running") || ph.slice().reverse().find(p => p.state !== "pending") || ph[0] || { name: jobState(j), state: "" };
}
function paintJobsStrip() {
  const el = $("#dl-jobs"); if (!el) return;
  const active = S.jobs.filter(jobActive);
  const recent = S.jobs.filter(j => !jobActive(j)).sort((a, b) => ts(b.finished_at || b.created_at) - ts(a.finished_at || a.created_at)).slice(0, 4);
  const list = active.concat(recent);
  if (!list.length) { el.innerHTML = empty("send", "暂无任务，选择 BOM 与终端后下发", "", true); return; }
  el.innerHTML = `<div class="job-strip">` + list.map(j => {
    const alias = jobNodes(j).map(id => { const n = S.nodes.find(x => x.node_id === id); return n ? (n.alias || shortId(id)) : shortId(id); }).join("、");
    const cur = currentPhase(j), pct = jobProgress(j), st = jobState(j);
    return `<div class="job-card" data-act="job-open" data-id="${esc(j.id)}" role="button" tabindex="0">
      <div class="j-top"><span class="j-id">${esc(shortId(j.id))}</span>${jobBadge(j)}</div>
      <div class="j-bom"><span class="mono">${esc(j.bom_id || "—")}</span> → ${esc(alias || "—")}</div>
      ${meter("进度", pct, pct + "%", st === "done" ? "green" : st.startsWith("failed") ? "red" : "cyan")}
      <div class="j-phase"><span>${esc(JOB_ZH[cur.name] || cur.name || "—")}</span><span>${ago(j.created_at)}</span></div>
    </div>`;
  }).join("") + `</div>`;
}
async function openJobDrawer(id) {
  let j; try { j = await api("GET", `/jobs/${enc(id)}`); } catch (e) { toast(e.message, "err"); return; }
  openDrawer(`任务 ${esc(shortId(id))}`, esc(j.bom_id || ""), jobDrawerBody(j), jobDrawerFoot(j), { ctx: { kind: "job", id } });
  poll("job:poll", () => repaintJob(id), 2000);
}
async function repaintJob(id) {
  if (!drawerCtx || drawerCtx.kind !== "job" || drawerCtx.id !== id) return;
  try {
    const j = await api("GET", `/jobs/${enc(id)}`);
    setDrawerBody(jobDrawerBody(j)); setDrawerFoot(jobDrawerFoot(j));
  } catch (e) { /* 保留上次渲染 */ }
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
    await repaintJob(id); refreshDeliverLive();
  } catch (e) { toast(e.message, "err"); }
}
async function jobRbComplete(id) {
  const ok = await confirmDlg({ title: "确认回滚完成", message: "确认终端已恢复到上一已知良好状态？任务将标记为「失败·已回滚」。", okText: "确认恢复", danger: false });
  if (!ok) return;
  try {
    await api("POST", `/jobs/${enc(id)}/rollback-complete`, { recovered: true });
    toast("已确认回滚完成", "ok");
    await repaintJob(id); refreshDeliverLive();
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
    <div class="banner info">${icon("info", 15)}<span>按环逐步放量：每环推进需上一环任务全部完成。百分比之和不超过 100%。</span></div>
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
  const el = $("#ro-sum"); if (el) { el.textContent = `合计 ${sum}%`; el.className = "tag-mono " + (sum > 100 ? "txt-err" : "txt-ok"); }
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
  if (sum > 100) return fail(`百分比合计 ${sum}% 超过 100%`);
  try {
    await api("POST", "/rollouts", { name, bom_id: sel.slice(0, at), bom_version: Number(sel.slice(at + 1)) || 0, rings: rings.map(r => ({ name: r.name.trim(), percent: r.percent })) });
    toast(`灰度「${name}」已创建`, "ok");
    closeModal(); await refreshDeliverLive();
  } catch (e) { fail(e.message); toast(e.message, "err"); }
}
async function roAdvance(id) {
  try {
    const ro = await api("POST", `/rollouts/${enc(id)}/advance`);
    toast(ro && ro.status === "done" ? "灰度已全部完成" : "已推进到下一环", "ok");
    await refreshDeliverLive();
  } catch (e) { toast(e.status === 409 ? "当前环仍有任务未完成，无法推进" : e.message, "err"); }
}
async function roDelete(id) {
  const ro = S.rollouts.find(r => r.id === id);
  const ok = await confirmDlg({ title: "删除灰度发布", message: `将删除灰度「${(ro && ro.name) || shortId(id)}」及其环计划。已下发的任务不受影响。`, okText: "删除" });
  if (!ok) return;
  try {
    await api("DELETE", `/rollouts/${enc(id)}`);
    toast("灰度已删除", "ok", {
      undo: async () => {
        if (!ro) return;
        try { await api("POST", "/rollouts", { name: ro.name, bom_id: ro.bom_id, bom_version: ro.bom_version, rings: (ro.rings || []).map(r => ({ name: r.name, percent: r.percent })) }); toast("灰度已恢复", "ok"); }
        catch (e) { toast(e.message, "err"); }
        refreshDeliverLive();
      }, undoText: "撤销"
    });
    await refreshDeliverLive();
  } catch (e) { toast(e.message, "err"); }
}

/* ================= KEYS & LICENSE ================= */
function shellKeys() {
  return `<div class="grid cols-2">
      <section class="card stagger" style="--i:0" id="lic-card"></section>
      <section class="card stagger" style="--i:1" id="gpg-card"></section>
    </div>
    <section class="card mt16 stagger" style="--i:2" id="lic-nodes"></section>`;
}
async function refreshKeys() {
  const [lic, keys, nodes] = await Promise.all([
    api("GET", "/license").catch(() => null),
    api("GET", "/keys").catch(() => []),
    api("GET", "/nodes").catch(() => [])
  ]);
  S.license = lic; S.keys = Array.isArray(keys) ? keys : []; S.nodes = Array.isArray(nodes) ? nodes : [];
  paintLicense(); paintKeys(); paintLicNodes();
}
function seatsHtml(used, seats) {
  const pct = seats > 0 ? clamp(used / seats * 100, 0, 100) : 0;
  return `<div class="seats"><div class="s-top"><span>已授权席位</span><span class="mono">${used} / ${seats || "∞"}</span></div><div class="seat-bar"><div class="seat-fill ${seats && used > seats ? "over" : ""}" style="width:${pct}%"></div></div></div>`;
}
function paintLicense() {
  const el = $("#lic-card"); if (!el) return;
  const lic = S.license;
  const used = S.nodes.filter(n => n.authorized).length;
  let h = secHead(`${icon("shield", 17)} 许可证`, `<button class="btn violet sm" data-act="lic-open">${icon("key", 14)} ${lic && lic.status === "activated" ? "重新激活" : "激活"}</button>`);
  if (!lic || lic.status === "none") {
    h += empty("lock", "尚未激活授权", `<button class="btn violet" data-act="lic-open">${icon("key", 15)} 激活许可证</button>`);
    h += `<div class="banner warn mt16">${icon("info", 15)}<span>激活后才能授权终端并下发服务。支持导入 payload JSON 与 detached 签名，或直接粘贴 base64。</span></div>`;
  } else {
    const st = lic.status;
    h += `<div class="mb12">${st === "activated" ? badge("已激活", "ok") : st === "expired" ? badge("已过期", "err") : badge("未激活", "warn")}</div>`;
    h += `<div class="kv">` +
      kvRow("许可 key", `<span class="mono">${esc(lic.key || "—")}</span>`) +
      kvRow("账户", esc(lic.account || "—")) +
      kvRow("席位", `<span class="mono">${lic.seats || "∞"}</span>`) +
      kvRow("激活时间", fmtTime(lic.activated_at)) +
      kvRow("到期时间", lic.expires_at ? fmtTime(lic.expires_at) : "永久") +
      `</div>`;
    h += seatsHtml(used, Number(lic.seats) || 0);
    if (st === "expired") h += `<div class="banner err mt16">${icon("alert", 15)}<span>授权已过期，下发与授权被拒绝，请重新激活。</span></div>`;
    else if (lic.seats && used >= lic.seats) h += `<div class="banner warn mt16">${icon("info", 15)}<span>席位已满，授权新终端前请先释放席位。</span></div>`;
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
function paintLicNodes() {
  const el = $("#lic-nodes"); if (!el) return;
  const list = S.nodes.filter(n => hit(n.alias, n.node_id, n.group, n.ip));
  let h = secHead(`${icon("server", 17)} 终端授权`, `<span class="hint">在许可席位内授权终端后方可下发</span>`);
  if (!list.length) { el.innerHTML = h + empty("server", S.nodes.length ? "没有匹配的终端" : "暂无终端", S.nodes.length ? "" : `<button class="btn primary sm" data-act="goto-add-node">${icon("plus", 13)} 添加终端</button>`, true); return; }
  h += `<div class="tbl-wrap"><table class="tbl"><thead><tr><th>终端</th><th>状态</th><th>授权</th><th>授权时间</th><th style="text-align:right">操作</th></tr></thead><tbody>` +
    list.map(n => `<tr>
      <td><div>${esc(n.alias || shortId(n.node_id))}</div><div class="sub mono">${esc(shortId(n.node_id))}</div></td>
      <td>${statusBadge(n.status)}</td>
      <td>${authBadge(n)}</td>
      <td class="sub nowrap">${n.authorized_at ? ago(n.authorized_at) : "—"}</td>
      <td class="acts">${n.authorized
        ? `<button class="btn ghost xs" data-act="node-revoke" data-id="${esc(n.node_id)}">${icon("lock", 12)} 撤销</button>`
        : `<button class="btn primary xs" data-act="node-auth" data-id="${esc(n.node_id)}">${icon("unlock", 12)} 授权</button>`}</td>
    </tr>`).join("") + `</tbody></table></div>`;
  el.innerHTML = h;
}
async function nodeAuth(id, revoke) {
  try {
    await api("POST", `/nodes/${enc(id)}/${revoke ? "revoke" : "authorize"}`);
    toast(revoke ? "已撤销授权" : "已授权终端", "ok");
    await refreshKeys();
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
      ${secHead(`${icon("audit", 17)} 审计日志`, `<button class="btn ghost sm" data-act="audit-export" data-fmt="csv">${icon("download", 14)} CSV</button><button class="btn ghost sm" data-act="audit-export" data-fmt="json">${icon("download", 14)} JSON</button>`)}
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
  if (!list.length) { tbl.innerHTML = empty("audit", S.audit.length ? "没有匹配的审计记录" : "暂无审计记录", "", true); return; }
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
function shellLibrary() {
  return `<div class="kpis stagger" style="--i:0" id="lib-kpis"></div>
    <section class="card stagger" style="--i:1">
      ${secHead(`${icon("folder", 17)} SMB 文件空间`, `<button class="btn ghost sm" data-act="lib-refresh">${icon("refresh", 14)} 刷新</button><button class="btn primary sm" data-act="lib-add">${icon("plus", 14)} 添加共享</button>`)}
      <div id="lib-mounts"></div>
    </section>
    <section class="card mt16 stagger" style="--i:2">
      <div class="sec-head">
        <div class="sec-title">${icon("music", 17)} 音乐曲目</div>
        <div class="spacer"></div>
        <div class="chips" id="lib-chips"></div>
      </div>
      <div id="lib-tracks"></div>
    </section>
    <section class="card mt16 stagger" style="--i:3">
      ${secHead(`${icon("speaker", 17)} 正在播放`, `<span class="txt-faint" style="font-size:12px">ZAT 推流 · 终端 ALSA 出声</span>`)}
      <div id="lib-now"></div>
    </section>`;
}
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
async function refreshLibrary() {
  const [mounts, tracks, nodes] = await Promise.all([api("GET", "/mounts"), api("GET", "/tracks"), api("GET", "/nodes")]);
  S.mounts = Array.isArray(mounts) ? mounts : [];
  S.tracks = Array.isArray(tracks) ? tracks : [];
  S.nodes = Array.isArray(nodes) ? nodes : [];
  await fetchPlay(S.nodes);
  paintLibrary();
}
async function refreshLibraryLive() {
  const nodes = await api("GET", "/nodes");
  S.nodes = Array.isArray(nodes) ? nodes : [];
  await fetchPlay(S.nodes);
  paintLibKpis(); paintNowPlaying();
}
function paintLibrary() { paintLibKpis(); paintMounts(); paintLibChips(); paintTracks(); paintNowPlaying(); }
function paintLibKpis() {
  const el = $("#lib-kpis"); if (!el) return;
  const mounted = S.mounts.filter(m => m.status === "mounted").length;
  const dec = S.tracks.filter(t => t.decodable).length;
  const playing = Object.values(S.play).filter(p => p && p.playing).length;
  el.innerHTML =
    kpi("共享挂载", S.mounts.length, "amber", "folder") +
    kpi("已挂载", mounted, "green", "check") +
    kpi("曲目总数", S.tracks.length, "cyan", "music") +
    kpi("可解码", dec, "violet", "disc") +
    kpi("播放中", playing, "blue", "speaker");
}
function paintMounts() {
  const el = $("#lib-mounts"); if (!el) return;
  const list = S.mounts.filter(m => hit(m.host, m.share, m.target, m.username, m.domain));
  if (!list.length) {
    el.innerHTML = empty("folder", S.mounts.length ? "没有匹配的共享" : "还没有 SMB 文件空间。添加共享后挂载并扫描，即可识别音频并推送到终端。",
      S.mounts.length ? "" : `<button class="btn primary" data-act="lib-add">${icon("plus", 15)} 添加共享</button>`);
    return;
  }
  el.innerHTML = `<div class="tbl-wrap"><table class="tbl"><thead><tr>
    <th>共享</th><th>状态</th><th>认证</th><th>协议</th><th>曲目</th><th style="text-align:right">操作</th>
  </tr></thead><tbody>` + list.map(mountRow).join("") + `</tbody></table></div>`;
}
function mountRow(m) {
  const cnt = S.tracks.filter(t => t.mount_id === m.id).length;
  const mounted = m.status === "mounted";
  return `<tr>
    <td><div class="mono">${esc(m.host)}/${esc(m.share)}</div><div class="sub mono truncate" style="max-width:320px" title="${esc(m.target)}">${esc(m.target)}</div></td>
    <td>${mountBadge(m.status)}${m.detail ? `<div class="sub truncate" style="max-width:200px" title="${esc(m.detail)}">${esc(m.detail)}</div>` : ""}</td>
    <td>${m.username ? `<div>${esc(m.domain ? m.domain + "\\" : "")}${esc(m.username)}</div>` : '<span class="txt-faint">guest</span>'}</td>
    <td class="cell-mono">vers=${esc(m.version || "3.0")}<div class="sub">${m.writable ? "读写" : "只读"}</div></td>
    <td class="cell-mono">${mounted ? cnt : '<span class="txt-faint">—</span>'}</td>
    <td><div class="acts">
      ${mounted
        ? `<button class="btn xs ghost" data-act="lib-scan" data-id="${esc(m.id)}" title="扫描识别音频">${icon("refresh", 13)} 扫描</button>
           <button class="btn xs ghost" data-act="lib-unmount" data-id="${esc(m.id)}" title="卸载共享">${icon("power", 13)} 卸载</button>`
        : `<button class="btn xs cyan" data-act="lib-mount" data-id="${esc(m.id)}" title="挂载共享">${icon("download", 13)} 挂载</button>`}
      <button class="btn xs danger" data-act="lib-delete" data-id="${esc(m.id)}" title="删除共享">${icon("trash", 13)}</button>
    </div></td>
  </tr>`;
}
function paintLibChips() {
  const el = $("#lib-chips"); if (!el) return;
  const counts = {};
  S.tracks.forEach(t => { counts[t.mount_id] = (counts[t.mount_id] || 0) + 1; });
  const chips = [["", "全部", S.tracks.length]].concat(S.mounts.map(m => [m.id, m.host + "/" + m.share, counts[m.id] || 0]));
  el.innerHTML = chips.map(([id, label, c]) =>
    `<button class="chip ${S.libMount === id ? "active" : ""}" data-act="lib-filter" data-f="${esc(id)}">${esc(label)}<span class="cnt">${c}</span></button>`).join("");
}
function filteredTracks() {
  let list = S.tracks.slice();
  if (S.libMount) list = list.filter(t => t.mount_id === S.libMount);
  list = list.filter(t => hit(t.title, t.artist, t.album, t.codec, t.container, t.rel_path));
  list.sort((a, b) => String(a.artist || "").localeCompare(String(b.artist || ""), "zh")
    || String(a.album || "").localeCompare(String(b.album || ""), "zh")
    || String(a.title || "").localeCompare(String(b.title || ""), "zh"));
  return list;
}
function paintTracks() {
  const el = $("#lib-tracks"); if (!el) return;
  const list = filteredTracks();
  if (!list.length) {
    const msg = !S.mounts.length ? "先添加并挂载 SMB 共享，扫描后曲目会出现在这里。"
      : !S.tracks.length ? "还没有曲目。挂载共享后点击「扫描」识别音频文件。"
      : "没有匹配的曲目。";
    el.innerHTML = empty("music", msg, !S.mounts.length ? `<button class="btn primary" data-act="lib-add">${icon("plus", 15)} 添加共享</button>` : "", true);
    return;
  }
  el.innerHTML = `<div class="tbl-wrap"><table class="tbl"><thead><tr>
    <th>曲目</th><th>艺术家 / 专辑</th><th>格式</th><th>采样</th><th>时长</th><th>大小</th><th>解码</th><th style="text-align:right">操作</th>
  </tr></thead><tbody>` + list.map(trackRow).join("") + `</tbody></table></div>`;
}
function trackRow(t) {
  return `<tr>
    <td><div class="truncate" style="max-width:280px">${esc(t.title || "—")}</div><div class="sub mono truncate" style="max-width:280px" title="${esc(t.rel_path)}">${esc(t.rel_path)}</div></td>
    <td><div>${esc(t.artist || "—")}</div><div class="sub">${esc(t.album || "")}</div></td>
    <td>${codecBadge(t.codec)}<div class="sub">${esc(t.container || "")}</div></td>
    <td class="cell-mono nowrap">${t.rate ? (t.rate / 1000).toFixed(1) + " kHz" : "—"}<div class="sub">${t.channels || "?"} ch · ${t.bits || "?"} bit</div></td>
    <td class="cell-mono nowrap">${fmtDur(t.duration_ms)}</td>
    <td class="cell-mono nowrap">${fmtBytes(t.size)}</td>
    <td>${t.decodable ? badge("可解码", "ok") : badge("仅识别", "muted")}</td>
    <td><div class="acts"><button class="btn xs primary" data-act="track-play" data-id="${esc(t.id)}" ${t.decodable ? "" : "disabled"} title="${t.decodable ? "推送到终端播放" : "v0.1 仅支持 FLAC / WAV 解码"}">${icon("send", 13)} 推送</button></div></td>
  </tr>`;
}
function paintNowPlaying() {
  const el = $("#lib-now"); if (!el) return;
  const online = S.nodes.filter(n => n.status === "online");
  if (!online.length) {
    el.innerHTML = empty("speaker", "没有在线终端。终端 Agent 连接 ZAT 服务后，推送的曲目会在这里实时显示。", `<button class="btn ghost sm" data-act="lib-goto-nodes">${icon("server", 14)} 前往终端管理</button>`, true);
    return;
  }
  el.innerHTML = `<div class="now-list">` + online.map(nowRow).join("") + `</div>`;
}
function nowRow(n) {
  const p = S.play[n.node_id] || {};
  const t = p.track_id ? S.tracks.find(x => x.id === p.track_id) : null;
  const playing = !!p.playing;
  const elapsed = t && t.rate && p.pts ? fmtDur(p.pts / t.rate * 1000) : null;
  return `<div class="now-row ${playing ? "playing" : ""}">
    <div class="now-ic">${icon(playing ? "speaker" : "server", 18)}</div>
    <div class="now-info">
      <div class="now-alias">${esc(n.alias || shortId(n.node_id))} ${authBadge(n)}</div>
      <div class="now-meta mono">${esc(shortId(n.node_id))}${n.ip ? " · " + esc(n.ip) : ""}${n.active_proto ? " · " + esc(n.active_proto) : ""}</div>
    </div>
    <div class="now-track">
      ${playing || p.connected
        ? `<div class="now-title truncate">${esc(p.title || (t && t.title) || "—")}</div>
           <div class="now-sub">${p.connected ? badge("ZAT 已连接", "ok") : badge("未连接", "muted")}${playing ? badge("播放中", "info") : ""}${elapsed ? ` <span class="mono">${elapsed}</span>` : ""}${p.buffer_ms != null ? ` <span class="txt-faint">缓冲 ${p.buffer_ms} ms</span>` : ""}</div>`
        : `<div class="now-sub txt-faint">空闲 · 未推流</div>`}
    </div>
    <div class="now-acts">${playing ? `<button class="btn xs danger" data-act="play-stop" data-id="${esc(n.node_id)}">${icon("stop", 13)} 停止</button>` : ""}</div>
  </div>`;
}

/* ---- add SMB share ---- */
function openMountAdd() {
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
      <div class="field"><label>SMB 版本</label><select id="mnt-ver" class="inp"><option value="3.0">3.0（默认）</option><option value="3.1.1">3.1.1</option><option value="2.1">2.1</option><option value="2.0">2.0</option><option value="1.0">1.0</option></select></div>
    </div>
    <div class="field"><label class="switch"><input id="mnt-rw" type="checkbox"><span class="track"></span><span class="sw-lbl">可写挂载（默认只读，媒体库仅读取）</span></label></div>
    <div class="banner info">${icon("info", 15)}<span>挂载使用内核 mount.cifs（需 cifs-utils 与 root）。凭据写入临时 0600 文件，挂载后立即删除，绝不出现在命令行或日志。</span></div>`,
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
    await api("POST", "/mounts", {
      host, share, username: val("mnt-user").trim(), password: val("mnt-pass"),
      domain: val("mnt-domain").trim(), version: val("mnt-ver"), writable: checked("mnt-rw")
    });
    toast(`共享 ${host}/${share} 已添加`, "ok");
    closeModal();
    await refreshLibrary();
  } catch (e) { fail(e.message); }
}
async function libMountAct(id, act) {
  const m = S.mounts.find(x => x.id === id);
  const label = m ? m.host + "/" + m.share : shortId(id);
  try {
    if (act === "scan") {
      const r = await api("POST", `/mounts/${enc(id)}/scan`);
      toast(`扫描完成：识别 ${r && r.count != null ? r.count : 0} 个音频文件`, "ok");
    } else {
      const r = await api("POST", `/mounts/${enc(id)}/${act}`);
      if (r && r.status === "error") {
        toast(`${act === "mount" ? "挂载" : "卸载"}失败：${r.detail || "未知错误"}`, "err");
      } else {
        toast(act === "mount" ? `已挂载 ${label}` : `已卸载 ${label}`, "ok");
      }
    }
    await refreshLibrary();
  } catch (e) { toast(e.message, "err"); await refreshLibrary(); }
}
async function libDelete(id) {
  const m = S.mounts.find(x => x.id === id);
  const label = m ? m.host + "/" + m.share : shortId(id);
  const ok = await confirmDlg({ title: "删除共享", message: `将删除 ${label} 及其全部曲目记录。已挂载的共享会先卸载。此操作不可撤销。`, okText: "删除" });
  if (!ok) return;
  try { await api("DELETE", `/mounts/${enc(id)}`); toast(`共享 ${label} 已删除`, "ok"); await refreshLibrary(); }
  catch (e) { toast(e.message, "err"); }
}
async function libRefresh() { try { await refreshLibrary(); toast("已刷新", "ok"); } catch (e) { toast(e.message, "err"); } }

/* ---- push playback: terminal picker ---- */
const TP = { trackID: "", track: null, sel: "", busy: false };
async function openTrackPlay(trackID) {
  TP.trackID = trackID; TP.track = S.tracks.find(x => x.id === trackID) || null; TP.sel = ""; TP.busy = false;
  openModal(`${icon("send", 18)} 推送播放`, `<div id="tp-body">${empty("refresh", "正在加载终端…", "", true)}</div>`,
    `<button class="btn ghost" data-act="modal-close">取消</button><div class="spacer"></div><button class="btn primary" id="tp-go" data-act="track-play-go" disabled>${icon("play", 15)} 推送播放</button>`, { narrow: true });
  try {
    const nodes = await api("GET", "/nodes");
    S.nodes = Array.isArray(nodes) ? nodes : [];
    await fetchPlay(S.nodes);
  } catch (e) { /* keep cached nodes */ }
  if (!$(".modal") || TP.trackID !== trackID) return;
  paintTrackPlay();
}
function paintTrackPlay() {
  const body = $("#tp-body"); if (!body) return;
  const t = TP.track || {};
  const online = S.nodes.filter(n => n.status === "online");
  let h = `<div class="tp-track">
      <div class="tp-title truncate">${esc(t.title || "未知曲目")}</div>
      <div class="tp-meta">${esc(t.artist || "—")} · ${esc(t.album || "—")} ${codecBadge(t.codec)} <span class="mono">${t.rate ? (t.rate / 1000).toFixed(1) + " kHz" : ""}${t.channels ? " · " + t.channels + " ch" : ""}${t.bits ? " · " + t.bits + " bit" : ""}</span></div>
    </div><div class="divider"></div><span class="lbl">选择终端（在线 ${online.length}）</span>`;
  if (!online.length) {
    h += empty("server", "没有在线终端。请先在「终端管理」添加并授权终端，终端 Agent 连接 ZAT 后即可推送。", `<button class="btn ghost sm" data-act="tp-goto-nodes">${icon("server", 14)} 前往终端管理</button>`, true);
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
    if (!online.some(n => n.authorized)) h += `<div class="banner warn mt12">${icon("alert", 15)}<span>在线终端均未授权，ZAT 推流会被拒绝。请先在终端管理授权。</span></div>`;
    else if (TP.sel && !(S.nodes.find(n => n.node_id === TP.sel) || {}).authorized) h += `<div class="banner warn mt12">${icon("alert", 15)}<span>该终端未授权，无法推流。请选择已授权终端。</span></div>`;
  }
  body.innerHTML = h;
  const go = $("#tp-go"); if (go) go.disabled = !TP.sel || TP.busy;
}
async function trackPlayGo() {
  const nodeID = TP.sel; if (!nodeID || TP.busy) return;
  const n = S.nodes.find(x => x.node_id === nodeID);
  if (n && !n.authorized) { toast("终端未授权，无法推流", "warn"); return; }
  TP.busy = true;
  const go = $("#tp-go"); if (go) { go.disabled = true; go.innerHTML = `${icon("refresh", 15)} 推送中…`; }
  try {
    const r = await api("POST", `/nodes/${enc(nodeID)}/play`, { track_id: TP.trackID });
    toast(`已推送到 ${n ? (n.alias || shortId(nodeID)) : shortId(nodeID)}${r && r.session_id ? " · " + shortId(r.session_id) : ""}`, "ok");
    closeModal();
    await refreshLibraryLive();
  } catch (e) {
    TP.busy = false;
    if (go) { go.disabled = false; go.innerHTML = `${icon("play", 15)} 推送播放`; }
    toast(e.message, "err");
  }
}
async function playStop(nodeID) {
  try { await api("POST", `/nodes/${enc(nodeID)}/stop`); toast("已停止播放", "ok"); await refreshLibraryLive(); }
  catch (e) { toast(e.message, "err"); }
}

/* ================= ACTION MAP ================= */
const ACT = {
  "nav": ds => setView(ds.view),
  "bell": () => openAlerts(),
  "refresh-view": () => refreshFor(S.view),
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
  "nd-filter": ds => { S.nodeFilter = ds.f; paintNodeChips(); paintNodeTable(); },
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
  "wz-copy-cmd": async () => { toast(await copyText(installCmd()) ? "安装命令已复制" : "复制失败，请手动选择", "ok"); },
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
  "dl-submit": () => dlSubmit(),
  "job-open": ds => openJobDrawer(ds.id),
  "job-advance": ds => jobAdvance(ds.id),
  "job-rb-complete": ds => jobRbComplete(ds.id),
  "ro-create": () => openRolloutCreate(),
  "ro-submit": () => roSubmit(),
  "ro-ring-add": () => roRingAdd(),
  "ro-ring-del": ds => roRingDel(Number(ds.i)),
  "ro-advance": ds => roAdvance(ds.id),
  "ro-delete": ds => roDelete(ds.id),
  "lic-open": () => openLicenseActivate(),
  "lic-submit": () => licSubmit(),
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
  "lib-delete": ds => libDelete(ds.id),
  "lib-filter": ds => { S.libMount = ds.f; paintLibChips(); paintTracks(); },
  "lib-goto-nodes": () => { closeModal(); setView("nodes"); },
  "track-play": ds => openTrackPlay(ds.id),
  "tp-pick": ds => { TP.sel = ds.id; paintTrackPlay(); },
  "tp-goto-nodes": () => { closeModal(); setView("nodes"); },
  "track-play-go": () => trackPlayGo(),
  "play-stop": ds => playStop(ds.id)
});

/* ================= CHANGE MAP ================= */
const CHG = {
  "node-sel": (el, ds) => { if (el.checked) S.sel.add(ds.id); else S.sel.delete(ds.id); paintBulkBar(); paintNodeTable(); },
  "node-sel-all": el => { const list = filteredNodes(); list.forEach(n => { if (el.checked) S.sel.add(n.node_id); else S.sel.delete(n.node_id); }); paintNodes(); },
  "os-pkg": (el, ds) => { if (el.checked) OSC.sel.add(ds.name); else OSC.sel.delete(ds.name); repaintOs(); },
  "os-pkg-all": el => { const pkgs = (OSC.state && OSC.state.packages) || []; pkgs.forEach(p => { if (el.checked) OSC.sel.add(p.name); else OSC.sel.delete(p.name); }); repaintOs(); },
  "cfg-bom": () => paintCfgDrift(),
  "cfg-dac": () => paintCfgDacHint(),
  "ver-lc": (el, ds) => verLifecycle(ds, el.value),
  "dl-bom": el => { S.jobBom = el.value; paintDlForm(); },
  "dl-node": (el, ds) => { if (el.checked) S.jobSel.add(ds.id); else S.jobSel.delete(ds.id); paintDlForm(); },
  "dl-node-all": el => { S.nodes.filter(n => n.authorized).forEach(n => { if (el.checked) S.jobSel.add(n.node_id); else S.jobSel.delete(n.node_id); }); paintDlForm(); },
  "ro-pct": () => paintRoSum(),
  "lic-pfile": el => fileToText(el, txt => { const t = $("#lic-payload"); if (t) t.value = txt; }),
  "lic-sfile": el => fileToText(el, txt => { const t = $("#lic-sig"); if (t) t.value = txt; }),
  "gpg-file": el => fileToText(el, txt => { const t = $("#gpg-armored"); if (t) t.value = txt; }),
  "bi-file": el => fileToText(el, txt => { const t = $("#bi-json"); if (t) t.value = txt; })
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
  if (v === "overview") paintOverviewNodes();
  else if (v === "nodes") paintNodeTable();
  else if (v === "library") { paintMounts(); paintTracks(); }
  else if (v === "catalog") { paintApps(); paintBoms(); }
  else if (v === "deliver") paintRollouts();
  else if (v === "keys") paintLicNodes();
  else if (v === "audit") paintAudit();
}
function boot() {
  const nav = $("#nav");
  if (nav) nav.innerHTML = NAV.map(n => `<button class="nav-item" data-act="nav" data-view="${n.id}">${icon(n.icon, 17)}<span>${esc(n.name)}</span></button>`).join("");
  document.addEventListener("click", onClick);
  document.addEventListener("change", onChange);
  document.addEventListener("input", onInput);
  document.addEventListener("keydown", onKey);
  setView("overview");
  poll("global", globalTick, 4000, true);
}
boot();
