"use strict";

const $ = (id) => document.getElementById(id);
let catalog = [];
let services = [];
let nodeSecret = "";
let lastIdentity = null;
let gateBlockMsg = "";
let lastPlayback = null;
let lastQueue = null;
let npDevices = [];
let npSeeking = false;
let npVoluming = false;
let npDevSig = "";

function esc(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

async function api(path, method = "GET", body) {
  const opt = { method, headers: {} };
  // Mutations carry the local-UI marker (CSRF-safe: browsers cannot set it
  // cross-origin). The loopback-bootstrapped secret is added when known; behind
  // the server reverse proxy the proxy injects X-Node-Secret instead.
  if (method !== "GET") {
    opt.headers["X-Local-UI"] = "1";
    if (nodeSecret) opt.headers["X-Node-Secret"] = nodeSecret;
  }
  if (body !== undefined) {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const res = await fetch(path, opt);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const e = new Error(data.error || ("HTTP " + res.status));
    e.status = res.status;
    throw e;
  }
  return data;
}

function renderBanner() {
  const b = $("banner");
  const id = lastIdentity || {};
  let msg = gateBlockMsg;
  if (!msg) {
    if (id.allow_local === false) {
      msg = "本地控制已禁用（ALLOW_LOCAL=false）。请通过服务端控制台操作。";
    } else if (id.playing && id.play_protect) {
      msg = "播放保护已启用：终端正在播放，安装/升级/电源等操作已被拦截。请先在「正在播放」卡片中停止。";
    }
  }
  if (msg) {
    b.textContent = msg;
    b.className = "banner warn";
    b.hidden = false;
  } else {
    b.hidden = true;
  }
}

function applyGates(id) {
  const blocked = id.allow_local === false || (id.playing && id.play_protect);
  ["btn-os-check", "btn-os-upgrade", "btn-netdiag"].forEach((bid) => {
    const el = $(bid);
    if (el) el.disabled = blocked;
  });
}

function toast(msg, kind = "") {
  const t = $("toast");
  t.textContent = msg;
  t.className = "toast " + kind;
  t.hidden = false;
  clearTimeout(t._timer);
  t._timer = setTimeout(() => (t.hidden = true), 3500);
}

function fmtUptime(sec) {
  sec = Math.floor(sec || 0);
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
  return (d ? d + "天 " : "") + h + "时 " + m + "分";
}

function setBar(id, pct, hot) {
  const el = $(id);
  el.style.width = Math.max(0, Math.min(100, pct)) + "%";
  el.className = pct >= (hot || 90) ? "hot" : "";
}

function renderIdentity(id) {
  lastIdentity = id;
  $("alias").textContent = id.alias || "终端";
  $("arch").textContent = id.arch || "";
  $("agentver").textContent = "Agent " + (id.agent_version || "");
  const sl = $("serverlink");
  sl.textContent = id.server_reachable ? "服务端已连接" : "服务端离线";
  sl.className = "pill " + (id.server_reachable ? "ok" : "bad");
  const sy = $("syslink");
  sy.textContent = id.system_available ? "系统控制就绪" : "系统控制不可用";
  sy.className = "pill " + (id.system_available ? "ok" : "bad");
  $("btn-reboot").disabled = !id.allow_power;
  $("btn-shutdown").disabled = !id.allow_power;
  $("si-ip").textContent = id.ip || "–";
  $("si-node").textContent = id.node_id || "–";
  $("si-node").title = id.node_id || "";
  applyGates(id);
  renderBanner();
}

function renderMetrics(m) {
  $("m-cpu").textContent = (m.cpu_percent || 0).toFixed(0) + "%";
  setBar("bar-cpu", m.cpu_percent || 0);
  $("m-mem").textContent = (m.mem_percent || 0).toFixed(0) + "%";
  setBar("bar-mem", m.mem_percent || 0);
  $("m-temp").textContent = (m.temp_c || 0).toFixed(1) + "°C";
  setBar("bar-temp", (m.temp_c || 0), 75);
  $("m-disk").textContent = (m.disk_percent || 0).toFixed(0) + "%";
  setBar("bar-disk", m.disk_percent || 0);
  $("m-load").textContent = (m.load || []).map((x) => x.toFixed(2)).join(" ");
  $("m-uptime").textContent = fmtUptime(m.uptime_sec);
  $("si-distro").textContent = m.distro || "–";
  $("si-kernel").textContent = m.kernel || "–";
}

function statusText(s) {
  if (!s.installed) return "未安装";
  return s.running ? "运行中" : "已停用";
}

function renderServices(list) {
  const wrap = $("cards");
  wrap.innerHTML = "";
  list.filter((s) => s.primary).forEach((s) => {
    const card = document.createElement("div");
    card.className = "card";
    const dotClass = !s.installed ? "" : s.running ? "on" : "off";
    const proto = s.protocol ? `<span class="badge">${s.protocol.toUpperCase()}</span>` : "";
    const sysBadge = s.source === "system" ? `<span class="badge sys">系统已有</span>` : "";
    const helpers = (s.helpers || []).length ? `<span>组件 <b>${s.helpers.join(", ")}</b></span>` : "";
    const proc = s.process
      ? `<span>进程 <b>${s.process}${(s.pids || []).length ? " (PID " + s.pids.join(", ") + ")" : ""}</b></span>`
      : "";
    const unit = s.installed ? `<span>单元 <b>${s.unit || "–"}</b></span>` : "";
    const ver = s.version ? `<span>版本 <b>${s.version}</b></span>` : "";
    let actions;
    if (!s.installed) {
      actions = `<button class="btn primary" data-act="install">安装</button>`;
    } else {
      // Button follows the live state: running offers 停用, stopped offers 启用.
      const toggle = s.running
        ? `<button class="btn warn" data-act="disable">停用</button>`
        : `<button class="btn good" data-act="enable"${s.unit ? "" : " disabled title=\"缺少 systemd 服务单元，无法启动\""}>启用</button>`;
      if (s.source === "system") {
        // Pre-existing system service: only toggle it. Never overwrite its config
        // or remove the OS package — those belong to the terminal image.
        actions = toggle;
      } else {
        actions = `${toggle}
          <button class="btn" data-act="config">配置</button>
          <button class="btn danger" data-act="uninstall">卸载</button>`;
      }
    }
    card.innerHTML = `
      <div class="card-top">
        <div class="card-title"><span class="dot ${dotClass}"></span><h3>${s.name}</h3></div>
        <div class="card-badges">${proto}${sysBadge}</div>
      </div>
      <div class="card-desc">${s.description || ""}</div>
      <div class="card-meta">
        <span>状态 <b>${statusText(s)}</b></span>
        ${ver}
        ${proc}
        ${unit}
        ${s.occupies_dac ? "<span>占用 DAC</span>" : ""}
        ${helpers}
      </div>
      <div class="card-actions">${actions}</div>`;
    card.querySelectorAll("button[data-act]").forEach((b) => {
      b.addEventListener("click", () => onAction(s, b.dataset.act));
    });
    wrap.appendChild(card);
  });
}

function renderDAC(holders) {
  const panel = $("dac-panel");
  if (!holders || !holders.length) { panel.hidden = true; return; }
  panel.hidden = false;
  $("dac-list").innerHTML = holders
    .map((h) => `<div class="dac-item"><span><b>${h.process || "?"}</b> (PID ${h.pid})</span><span>${h.device}</span></div>`)
    .join("");
}

function renderDevices(devs) {
  npDevices = devs || [];
  const panel = $("dev-panel");
  if (!devs || !devs.length) { panel.hidden = true; return; }
  panel.hidden = false;
  $("dev-list").innerHTML = devs
    .map((d) => `<div class="dev-item"><span>${d.name}</span><b>${d.hw}</b></div>`)
    .join("");
}

async function loadStatus() {
  try {
    const st = await api("local/api/status");
    renderIdentity(st.identity);
    renderMetrics(st.metrics);
    services = st.services || [];
    renderServices(st.services);
    renderDAC(st.dac);
    renderDevices(st.devices);
  } catch (e) {
    // keep last good render; surface transient errors quietly
  }
}

// --- modal ---
let modalOk = null;
function openModal(title, bodyHTML, onOk) {
  $("modal-title").textContent = title;
  $("modal-body").innerHTML = bodyHTML;
  modalOk = onOk;
  $("modal-ok").hidden = false;
  $("modal").hidden = false;
}
function closeModal() { $("modal").hidden = true; modalOk = null; }
$("modal-cancel").addEventListener("click", closeModal);
$("modal-ok").addEventListener("click", async () => {
  if (!modalOk) return closeModal();
  $("modal-ok").disabled = true;
  try { await modalOk(); closeModal(); }
  catch (e) {
    if (e.status === 403) { gateBlockMsg = e.message; renderBanner(); }
    toast(e.message, "err");
  }
  finally { $("modal-ok").disabled = false; }
});

function fieldsHTML(svc, withVersion) {
  let html = "";
  if (withVersion) {
    const entry = catalog.find((c) => c.app_id === svc.app_id);
    const versions = (entry && entry.versions) || [];
    const opts = versions.length
      ? versions.map((v) => `<option value="${v}">${v}</option>`).join("")
      : `<option value="">最新可用</option>`;
    html += `<div class="field"><label>版本</label><select id="f-version">${opts}</select>
      <div class="help">由服务端应用仓库提供</div></div>`;
  }
  (svc.config_fields || []).forEach((f) => {
    const val = (svc.config && svc.config[f.key]) || f.default || "";
    html += `<div class="field"><label>${f.label}</label>
      <input id="f-${f.key}" value="${val}" placeholder="${f.placeholder || ""}">
      ${f.help ? `<div class="help">${f.help}</div>` : ""}</div>`;
  });
  return html || `<div class="help">该服务无需额外配置。</div>`;
}

function collectConfig(svc) {
  const cfg = {};
  (svc.config_fields || []).forEach((f) => {
    const el = $("f-" + f.key);
    if (el) cfg[f.key] = el.value;
  });
  return cfg;
}

async function ensureCatalog() {
  try { catalog = (await api("local/api/catalog")) || []; }
  catch (e) { catalog = []; }
}

// The "+" entry: pick a distributable service (catalog is already filtered to
// this terminal's arch), then install it or reconfigure an installed one.
async function openSvcPicker() {
  await ensureCatalog();
  const rows = catalog.map((c) => {
    const svc = services.find((s) => s.app_id === c.app_id);
    const installed = !!(svc && svc.installed);
    const nv = (c.versions || []).length;
    const arch = (lastIdentity && lastIdentity.arch) || "本机架构";
    const sub = installed
      ? `已安装 · ${svc.version || "系统自带"} · ${svc.running ? "运行中" : "已停用"} · ${nv} 个可下发版本（${arch}）`
      : `${nv} 个可用版本（${arch}）`;
    // System-owned services keep their OS config: never offer our template here.
    const action = installed && svc.source === "system"
      ? `<span class="badge sys">系统已有</span>`
      : `<button class="btn ${installed ? "" : "primary"}" data-pick="${c.app_id}" data-mode="${installed ? "config" : "install"}">${installed ? "配置" : "安装"}</button>`;
    return `<div class="pick-row"><div><b>${c.name}</b><div class="sub">${sub}</div></div>${action}</div>`;
  }).join("");
  openModal("添加 / 配置音频服务",
    rows || `<div class="hint">服务端暂无适用于本机架构的可下发制品。</div>`, null);
  $("modal-ok").hidden = true;
  $("modal-body").querySelectorAll("button[data-pick]").forEach((b) => {
    b.addEventListener("click", () => {
      const id = b.dataset.pick;
      const svc = services.find((s) => s.app_id === id) ||
        { app_id: id, name: (catalog.find((c) => c.app_id === id) || {}).name || id };
      closeModal();
      onAction(svc, b.dataset.mode);
    });
  });
}
$("btn-add-svc").addEventListener("click", openSvcPicker);

async function onAction(svc, act) {
  try {
    if (act === "install") {
      await ensureCatalog();
      openModal("安装 " + svc.name, fieldsHTML(svc, true), async () => {
        const verEl = $("f-version");
        const version = verEl ? verEl.value : "";
        await api(`local/api/services/${svc.app_id}/install`, "POST", { version, config: collectConfig(svc) });
        gateBlockMsg = "";
        toast(svc.name + " 安装完成", "ok");
        loadStatus();
      });
    } else if (act === "config") {
      openModal("配置 " + svc.name, fieldsHTML(svc, false), async () => {
        await api(`local/api/services/${svc.app_id}/config`, "POST", { config: collectConfig(svc) });
        gateBlockMsg = "";
        toast("配置已应用", "ok");
        loadStatus();
      });
    } else if (act === "enable") {
      await api(`local/api/services/${svc.app_id}/enable`, "POST");
      gateBlockMsg = "";
      toast(svc.name + " 已启用", "ok"); loadStatus();
    } else if (act === "disable") {
      await api(`local/api/services/${svc.app_id}/disable`, "POST");
      gateBlockMsg = "";
      toast(svc.name + " 已停用", "ok"); loadStatus();
    } else if (act === "uninstall") {
      if (!confirm(`确认卸载 ${svc.name}？`)) return;
      await api(`local/api/services/${svc.app_id}/uninstall`, "POST");
      gateBlockMsg = "";
      toast(svc.name + " 已卸载", "ok"); loadStatus();
    }
  } catch (e) {
    if (e.status === 403) { gateBlockMsg = e.message; renderBanner(); }
    toast(e.message, "err");
  }
}

async function power(action) {
  const label = action === "reboot" ? "重启" : "关机";
  if (!confirm(`确认${label}本终端？正在进行的播放会中断。`)) return;
  try {
    await api("local/api/power", "POST", { action });
    gateBlockMsg = "";
    toast(label + "指令已发送", "ok");
  } catch (e) {
    if (e.status === 403) { gateBlockMsg = e.message; renderBanner(); }
    toast(e.message, "err");
  }
}
$("btn-reboot").addEventListener("click", () => power("reboot"));
$("btn-shutdown").addEventListener("click", () => power("shutdown"));

// --- 系统更新 ---
function renderOSResult(res) {
  const log = $("os-log");
  log.textContent = res.log || "";
  log.hidden = !res.log;
  const data = res.data || {};
  const pkgs = data.packages || [];
  const summary = $("os-summary");
  if (res.status === "err" && !pkgs.length) {
    summary.textContent = "操作失败：" + (res.log || "未知错误").split("\n")[0];
  } else {
    const when = data.last_check ? new Date(data.last_check * 1000).toLocaleString() : "";
    summary.innerHTML = `可升级 <b>${pkgs.length}</b> 个软件包` +
      (data.reboot_required ? ' · <b class="warn-text">需要重启</b>' : "") +
      (when ? ` · ${esc(when)}` : "");
  }
  $("os-packages").innerHTML = pkgs.map((p) =>
    `<label class="os-pkg"><input type="checkbox" data-pkg="${esc(p.name)}" checked>` +
    `<span class="os-pkg-name">${esc(p.name)}</span>` +
    `<span class="os-pkg-ver">${esc(p.current)} → <b>${esc(p.candidate)}</b></span></label>`
  ).join("");
}

async function osCheck() {
  const btn = $("btn-os-check");
  btn.disabled = true;
  $("os-summary").textContent = "正在检查更新…";
  try {
    const res = await api("local/api/os/check", "POST");
    gateBlockMsg = "";
    renderOSResult(res);
    toast(res.status === "ok" ? "检查完成" : "检查失败", res.status === "ok" ? "ok" : "err");
  } catch (e) {
    if (e.status === 403) { gateBlockMsg = e.message; renderBanner(); }
    $("os-summary").textContent = e.message;
    toast(e.message, "err");
  } finally {
    btn.disabled = false;
    if (lastIdentity) applyGates(lastIdentity);
    loadStatus();
  }
}

async function osUpgrade() {
  const checked = Array.from(document.querySelectorAll("#os-packages input[data-pkg]:checked"))
    .map((el) => el.dataset.pkg);
  const scope = checked.length ? `选中的 ${checked.length} 个软件包` : "全部可升级软件包";
  if (!confirm(`确认升级${scope}？升级期间请勿断电。`)) return;
  const btn = $("btn-os-upgrade");
  btn.disabled = true;
  $("os-summary").textContent = "正在升级…";
  try {
    const res = await api("local/api/os/upgrade", "POST", { packages: checked });
    gateBlockMsg = "";
    renderOSResult(res);
    toast(res.status === "ok" ? "升级完成" : "升级失败", res.status === "ok" ? "ok" : "err");
  } catch (e) {
    if (e.status === 403) { gateBlockMsg = e.message; renderBanner(); }
    $("os-summary").textContent = e.message;
    toast(e.message, "err");
  } finally {
    btn.disabled = false;
    if (lastIdentity) applyGates(lastIdentity);
    loadStatus();
  }
}
$("btn-os-check").addEventListener("click", osCheck);
$("btn-os-upgrade").addEventListener("click", osUpgrade);

// --- 网络诊断 ---
const NET_LABELS = { dns: "DNS 解析", tcp: "TCP 连接", mdns: "mDNS 发现", ssdp: "SSDP 发现", multicast: "组播加入" };
async function netdiag() {
  const btn = $("btn-netdiag");
  btn.disabled = true;
  $("net-results").innerHTML = '<div class="hint">正在诊断…</div>';
  try {
    const res = await api("local/api/netdiag", "POST");
    gateBlockMsg = "";
    const results = (res.data && res.data.results) || [];
    $("net-results").innerHTML = results.map((p) =>
      `<div class="net-item ${p.ok ? "ok" : "bad"}">` +
      `<span class="net-name">${esc(NET_LABELS[p.name] || p.name)}</span>` +
      `<span class="net-detail">${esc(p.detail)}</span>` +
      `<span class="net-ms">${p.ms}ms</span>` +
      `<span class="net-flag">${p.ok ? "正常" : "失败"}</span></div>`
    ).join("") || '<div class="hint">无诊断结果。</div>';
    toast("诊断完成", "ok");
  } catch (e) {
    if (e.status === 403) { gateBlockMsg = e.message; renderBanner(); }
    $("net-results").innerHTML = `<div class="hint">${esc(e.message)}</div>`;
    toast(e.message, "err");
  } finally {
    btn.disabled = false;
    if (lastIdentity) applyGates(lastIdentity);
  }
}
$("btn-netdiag").addEventListener("click", netdiag);

// --- 正在播放 (ZAT) ---
function fmtTime(ms) {
  ms = Math.max(0, Math.floor(ms || 0));
  const s = Math.floor(ms / 1000);
  return Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0");
}

function npStateLabel(st) {
  switch (st) {
    case "playing": return "播放中";
    case "paused": return "已暂停";
    case "ready": return "就绪";
    case "stopped": return "已停止";
    case "authenticated": return "已连接";
    default: return "空闲";
  }
}

function renderPlaybackDevices(p, blocked) {
  const sel = $("np-device");
  const cur = p.pref_device || p.device || p.alsa_device || "default";
  const sig = npDevices.map((d) => d.hw).join("|");
  if (sig !== npDevSig) {
    npDevSig = sig;
    sel.innerHTML = `<option value="default">默认 (default)</option>` +
      npDevices.map((d) => `<option value="${esc(d.hw)}">${esc(d.name)} · ${esc(d.hw)}</option>`).join("");
  }
  if (cur !== "default" && !Array.from(sel.options).some((o) => o.value === cur)) {
    const o = document.createElement("option");
    o.value = cur; o.textContent = cur;
    sel.appendChild(o);
  }
  if (document.activeElement !== sel) sel.value = cur;
  sel.disabled = blocked;
}

function renderPlayback(p) {
  lastPlayback = p;
  // Playback controls bypass play-protection (PROTOCOL.md §11); only ALLOW_LOCAL
  // disables them. Recompute every control's disabled state each render so a
  // prior gate never leaves a stale flag.
  const blocked = !!(lastIdentity && lastIdentity.allow_local === false);
  $("np-panel").classList.toggle("playing", !!p.playing);

  const conn = $("np-conn");
  conn.textContent = p.connected ? "ZAT 已连接" : "ZAT 未连接";
  conn.className = "pill " + (p.connected ? "ok" : "bad");

  const st = $("np-state");
  st.textContent = npStateLabel(p.state);
  st.className = "np-state " + (p.playing ? "on" : p.state === "paused" ? "pause" : "");

  $("np-title").textContent = p.title || (p.connected ? "等待播放" : "未连接");
  $("np-artist").textContent = p.artist || "";
  $("np-album").textContent = p.album || "";
  const fmt = [];
  if (p.codec) fmt.push(String(p.codec).toUpperCase());
  if (p.rate) fmt.push((p.rate / 1000).toFixed(1).replace(/\.0$/, "") + " kHz");
  if (p.channels) fmt.push(p.channels === 2 ? "立体声" : p.channels + " 声道");
  $("np-format").textContent = fmt.join(" · ");

  const rate = p.rate || 44100;
  const posMS = p.pts ? (p.pts / rate) * 1000 : 0;
  const durMS = p.duration_ms || 0;
  const seek = $("np-seek");
  if (!npSeeking) {
    seek.max = durMS > 0 ? Math.round(durMS) : Math.max(Math.round(posMS), 1);
    seek.value = Math.min(Math.round(posMS), Number(seek.max));
    seek.disabled = blocked || !p.connected || durMS <= 0;
  }
  $("np-pos").textContent = fmtTime(posMS);
  $("np-dur").textContent = durMS > 0 ? fmtTime(durMS) : "--:--";

  $("np-buffer").style.width = Math.max(0, Math.min(100, (p.buffer_ms || 0) / 5)) + "%";
  $("np-buffer-ms").textContent = (p.buffer_ms || 0) + " ms";

  const volVal = p.volume == null ? 100 : p.volume;
  const vol = $("np-volume");
  if (!npVoluming) vol.value = volVal;
  vol.disabled = blocked;
  $("np-volume-val").textContent = volVal + "%";

  const active = p.state === "playing" || p.state === "paused";
  const pauseBtn = $("np-pause");
  pauseBtn.textContent = p.state === "paused" ? "继续" : "暂停";
  pauseBtn.disabled = blocked || !p.connected || !active;
  $("np-stop").disabled = blocked || !p.connected || !(active || p.state === "ready");

  renderPlaybackDevices(p, blocked);
  renderQueue();
}

// The playlist lives on the server; the terminal only shows position and drives
// prev/next through it.
function renderQueue() {
  const q = lastQueue;
  const len = q && q.tracks ? q.tracks.length : 0;
  const pos = q ? q.position || 0 : 0;
  const blocked = !!(lastIdentity && lastIdentity.allow_local === false);
  const conn = !!(lastPlayback && lastPlayback.connected);
  $("np-queue").textContent = len ? `第 ${pos + 1} / ${len} 首` : "";
  const prev = $("np-prev"), next = $("np-next");
  prev.disabled = blocked || !conn || len === 0;
  next.disabled = blocked || !conn || len === 0 || pos >= len - 1;
  prev.title = len && pos > 0 ? q.tracks[pos - 1].title || "" : "";
  next.title = len && pos < len - 1 ? q.tracks[pos + 1].title || "" : "";
}

async function loadQueue() {
  try { lastQueue = await api("local/api/playback/queue"); }
  catch (e) { lastQueue = null; }
  renderQueue();
}

async function loadPlayback() {
  try { renderPlayback(await api("local/api/playback")); }
  catch (e) { /* keep last good render; transient errors stay quiet */ }
}

async function npPost(path, body, okMsg) {
  try {
    const r = await api("local/api/playback/" + path, "POST", body);
    gateBlockMsg = "";
    if (okMsg) toast(okMsg, "ok");
    loadPlayback();
    return r;
  } catch (e) {
    if (e.status === 403) { gateBlockMsg = e.message; renderBanner(); }
    toast(e.message, "err");
    loadPlayback();
    return null;
  }
}

$("np-pause").addEventListener("click", async () => {
  const r = await npPost("pause");
  if (r) toast(r.paused ? "已暂停" : "继续播放", "ok");
});
$("np-stop").addEventListener("click", () => npPost("stop", undefined, "已停止"));
$("np-prev").addEventListener("click", async () => { await npPost("prev", undefined, "播放上一首"); loadQueue(); });
$("np-next").addEventListener("click", async () => { await npPost("next", undefined, "播放下一首"); loadQueue(); });

const npSeek = $("np-seek");
npSeek.addEventListener("input", () => {
  npSeeking = true;
  $("np-pos").textContent = fmtTime(parseInt(npSeek.value, 10));
});
npSeek.addEventListener("change", () => {
  npSeeking = false;
  const rate = (lastPlayback && lastPlayback.rate) || 44100;
  npPost("seek", { pts: Math.round((parseInt(npSeek.value, 10) / 1000) * rate) });
});

const npVol = $("np-volume");
npVol.addEventListener("input", () => {
  npVoluming = true;
  $("np-volume-val").textContent = npVol.value + "%";
});
npVol.addEventListener("change", () => {
  npVoluming = false;
  npPost("volume", { volume: parseInt(npVol.value, 10) });
});

$("np-device").addEventListener("change", (ev) => {
  npPost("device", { device: ev.target.value }, "输出设备已设置（下次播放生效）");
});

async function loadSecret() {
  try {
    const r = await api("local/api/secret");
    nodeSecret = r.secret || "";
  } catch (e) {
    // Remote/proxied: the server injects X-Node-Secret; leave it empty here.
    nodeSecret = "";
  }
}

loadSecret();
loadStatus();
loadPlayback();
loadQueue();
setInterval(loadStatus, 3000);
setInterval(loadPlayback, 1000);
setInterval(loadQueue, 3000);