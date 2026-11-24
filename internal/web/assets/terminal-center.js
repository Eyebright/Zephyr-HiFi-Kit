"use strict";

const tcT = (key, params) => I18n.t("features." + key, params);
const TC = {
  tab: "nodes", epoch: 0, busy: null, admin: false, loading: true,
  errors: [], commands: [], nodeErrors: {}, taskNode: "", taskType: "all", taskState: "all", page: 1,
  detail: null, detailTab: "overview", fleetError: "", devEdit: null
};
function tcTime(value) {
  const time = ts(value);
  return time ? I18n.date(new Date(time * 1000), { dateStyle: "medium", timeStyle: "medium" }) : "—";
}
function tcAgo(value) {
  const time = ts(value); if (!time) return "—";
  const seconds = Math.max(0, Math.floor(Date.now() / 1000) - time);
  const [size, unit] = seconds < 60 ? [1, "second"] : seconds < 3600 ? [60, "minute"] : seconds < 86400 ? [3600, "hour"] : [86400, "day"];
  return I18n.relative(-Math.floor(seconds / size), unit);
}
function tcRolloutLabel(state) {
  const key = { done: "done", failed: "failed", running: "active", pending: "tcAwaitingAdvance" }[state];
  return key ? tcT(key) : state || tcT("tcStateUnknown");
}
function cleanupTerminalCenter() { TC.epoch++; TC.busy = null; TC.loading = true; stopPoll("tc:upgrades"); }
function shellNodes() {
  return `<section id="terminal-center">
    <div class="page-head"><div><h1>${esc(tcT("tcTitle"))}</h1><p class="ph-sub">${esc(tcT("tcSubtitle"))}</p></div><div class="ph-actions"><button class="btn ghost sm" data-act="tc-advanced">${icon("sliders", 15)} ${esc(tcT("tcAdvanced"))}</button></div></div>
    <nav class="tc-tabs" aria-label="${esc(tcT("tcWorkspace"))}">${[["nodes", tcT("node")], ["tasks", tcT("taskRecords")]].map(([tab, name]) => `<button class="btn ghost ${TC.tab === tab ? "active" : ""}" data-act="tc-tab" data-tab="${tab}" aria-pressed="${TC.tab === tab}">${esc(name)}</button>`).join("")}</nav>
    <div id="tc-access"></div><div id="tc-errors" role="status"></div><div id="tc-workspace">${tcWorkspace()}</div>
  </section>`;
}
function terminalCenterLanguageChanged() {
  const root = $("#terminal-center"); if (!root) return;
  const next = document.createElement("div"); next.innerHTML = shellNodes();
  for (const selector of [".page-head", ".tc-tabs"]) root.querySelector(selector)?.replaceWith(next.querySelector(selector));
  const add = root.querySelector("[data-act='wz-open']");
  if (add) add.innerHTML = icon("plus", 14) + " " + esc(tcT("tcAdd"));
  paintTerminalFilters(true);
  paintTerminalCenter();
}
function tcWorkspace() {
  if (TC.tab === "tasks") return `<section class="card tc-tasks"><div class="tc-task-filters toolbar" id="tc-task-filters"></div><div id="tc-task-list"></div><div id="tc-task-pages" class="tc-pages"></div></section>`;
  const upgradable = S.nodes.filter(n => tcOutdated(n) && n.status === "online" && n.authorized !== false).length;
  return `<section class="card"><div class="sec-head tc-node-head"><div class="chips" id="nd-chips"></div><div class="spacer"></div><span id="tc-seats" class="hint"></span>${TC.admin && upgradable ? `<button class="btn ghost sm" data-act="node-upgrade-all">${icon("refresh", 14)} ${esc(tcT("tcUpgradeAll"))} (${esc(I18n.number(upgradable))})</button>` : ""}<button class="btn primary sm" data-act="wz-open">${icon("plus", 14)} ${esc(tcT("tcAdd"))}</button></div><div id="nd-bulk"></div><div id="nd-table"></div></section>`;
}
function tcActive() {
  return S.view === "nodes" || (S.view === "settings" && typeof SETTINGS !== "undefined" && SETTINGS.section === "devices");
}
function tcHere(root, epoch) {
  return $("#terminal-center") === root && TC.epoch === epoch && (S.view === "nodes" || S.view === "settings");
}
async function refreshTerminalCenter() {
  const root = $("#terminal-center"), epoch = TC.epoch;
  if (!root || !tcHere(root, epoch)) return;
  if (TC.busy) return TC.busy;
  const work = (async () => {
    const responses = await Promise.allSettled([api("GET", "/nodes"), api("GET", "/jobs"), api("GET", "/rollouts"), api("GET", "/license"), api("GET", "/settings/session"), api("GET", "/update/fleet")]);
    if (!tcHere(root, epoch)) return;
    TC.errors = [];
    if (responses[0].status === "rejected") {
      TC.loading = false; TC.errors.push({ key: "tcNodesRefreshFailed", params: { message: responses[0].reason.message } }); paintTerminalCenter(); return;
    }
    S.nodes = responses[0].value || [];
    for (const id of S.sel) if (!S.nodes.some(n => n.node_id === id)) S.sel.delete(id);
    for (const [i, key, nameKey] of [[1, "jobs", "jobType"], [2, "rollouts", "rolloutRecords"]]) {
      if (responses[i].status === "fulfilled") S[key] = responses[i].value || [];
      else TC.errors.push({ key: "tcRecordsRefreshFailed", params: { get name() { return tcT(nameKey); }, message: responses[i].reason.message } });
    }
    S.license = responses[3].status === "fulfilled" ? responses[3].value : null;
    TC.admin = responses[4].status === "fulfilled" && responses[4].value.authenticated === true;
    if (responses[4].status === "rejected") TC.errors.push({ key: "tcSessionUnavailable" });
    TC.fleetError = responses[5].status === "rejected" ? responses[5].reason.message : "";
    S.upd.fleet = responses[5].status === "fulfilled" ? responses[5].value : null;
    TC.loading = false;
    paintTerminalCenter();
    let cursor = 0;
    const results = [];
    await Promise.all(Array.from({ length: Math.min(4, S.nodes.length) }, async () => {
      while (cursor < S.nodes.length && tcHere(root, epoch)) {
        const n = S.nodes[cursor++], id = n.node_id;
        const values = await Promise.allSettled([
          api("GET", `/nodes/${enc(id)}/apps`), api("GET", `/nodes/${enc(id)}/commands`),
          n.status === "online" && TC.admin ? api("GET", `/nodes/${enc(id)}/local/api/status`) : Promise.resolve(null),
          api("GET", `/nodes/${enc(id)}/devices`)
        ]);
        results.push({ id, values });
      }
    }));
    if (!tcHere(root, epoch)) return;
    const previousCommands = TC.commands;
    TC.commands = []; TC.nodeErrors = {};
    for (const { id, values } of results) {
      const [apps, commands, live, devs] = values;
      if (apps.status === "fulfilled") S.nodeApps[id] = Array.isArray(apps.value) ? apps.value : apps.value?.apps || [];
      else { S.nodeApps[id] = null; TC.nodeErrors[id] = "tcAppsReadFailed"; }
      S.nodeSvc[id] = live.status === "fulfilled" && Array.isArray(live.value?.services) ? live.value.services : null;
      S.nodeDevs[id] = devs.status === "fulfilled" && Array.isArray(devs.value?.devices) ? devs.value.devices : null;
      if (commands.status === "fulfilled") TC.commands.push(...(commands.value || []));
      else {
        TC.commands.push(...previousCommands.filter(c => c.node_id === id));
        TC.errors.push({ key: "tcCommandsRefreshFailed", params: { name: tcNodeName(id) } });
      }
    }
    paintTerminalCenter();
    if ($("#dl-rollouts")) paintRollouts();
  })();
  TC.busy = work;
  try { await work; } finally { if (TC.busy === work) TC.busy = null; }
}
function paintTerminalCenter() {
  if (!$("#terminal-center")) return;
  const access = $("#tc-access"), errors = $("#tc-errors");
  if (access) access.innerHTML = TC.admin ? "" : `<div class="tc-readonly"><span>${icon("lock", 14)} ${esc(tcT("tcReadOnly"))}</span><button class="btn ghost xs" data-act="tc-login">${esc(tcT("tcLoginSettings"))}</button></div>`;
  if (errors) errors.innerHTML = TC.errors.length ? `<div class="banner warn"><div>${TC.errors.map(e => esc(tcT(e.key, e.params))).join("<br>")}</div><button class="btn ghost xs" data-act="refresh-view">${esc(tcT("reload"))}</button></div>` : "";
  if (TC.tab === "nodes") paintNodes(); else { paintTerminalFilters(); paintTerminalTasks(); }
}
function setTerminalTab(tab) {
  if (!["nodes", "tasks"].includes(tab)) return;
  TC.tab = tab;
  const workspace = $("#tc-workspace");
  if (!workspace) { setView("nodes"); return; }
  workspace.innerHTML = tcWorkspace();
  $$("[data-act=tc-tab]").forEach(el => { el.classList.toggle("active", el.dataset.tab === tab); el.setAttribute("aria-pressed", String(el.dataset.tab === tab)); });
  paintTerminalCenter();
}
function showTerminalTasks(id = "") {
  closeModal(); closeDrawer(); TC.taskNode = id; TC.page = 1;
  if (!tcActive()) { TC.tab = "tasks"; setView("nodes"); }
  else setTerminalTab("tasks");
  return refreshTerminalCenter();
}
function paintTerminalSearch() { TC.page = 1; if (TC.tab === "tasks") paintTerminalTasks(); else { paintBulkBar(); paintNodeTable(); } }
function tcNodeName(id) { const n = S.nodes.find(n => n.node_id === id); return n?.alias || shortId(id); }
function tcFleetNode(n) { return S.upd.fleet?.nodes?.find(x => x.node_id === n.node_id); }
function tcTargetVersion() { return S.upd.fleet?.target || ""; }
function tcOutdated(n) { const target = tcTargetVersion(); return !!(target && n.agent_version && n.agent_version !== target); }
function tcFlavorLabel(n) { return tcT(n.build_flavor === "audio" ? "tcFlavorAudio" : "tcFlavorManagement"); }
function tcUpgradeBlocked(n) { return !TC.admin || n.status !== "online" || n.authorized === false || (n.playing && n.play_protect); }
function tcUpgradeButton(n, cls = "primary sm") {
  if (!tcOutdated(n)) return "";
  const reason = !TC.admin ? tcT("tcReadOnly") : n.status !== "online" ? tcT("tcNodeNotOnline") : n.authorized === false ? tcT("unauthorized") : n.playing && n.play_protect ? tcT("tcUpgradePlaying") : "";
  return `<button class="btn ${cls}" data-act="node-upgrade" data-id="${esc(n.node_id)}" ${tcUpgradeBlocked(n) ? "disabled" : ""} title="${esc(reason)}">${icon("refresh", 13)} ${esc(tcT("tcUpgrade"))}</button>`;
}
function filteredNodes() {
  let nodes = S.nodes.filter(n => S.nodeFilter === "all" || (S.nodeFilter === "playing" ? n.status === "online" && n.playing : n.status === S.nodeFilter));
  nodes = nodes.filter(n => hit(n.alias, n.node_id, n.group, n.ip, n.arch, n.distro, ...(S.nodeApps[n.node_id] || []).map(a => a.app_id)));
  const { key, dir } = S.nodeSort;
  return nodes.sort((a, b) => String(a[key] ?? "").localeCompare(String(b[key] ?? ""), "zh", { numeric: true }) * dir);
}
function paintNodes() { paintNodeChips(); paintBulkBar(); paintNodeTable(); }
function paintNodeChips() {
  const el = $("#nd-chips"); if (!el) return;
  const counts = { all: S.nodes.length, online: S.nodes.filter(n => n.status === "online").length, offline: S.nodes.filter(n => n.status === "offline").length, playing: S.nodes.filter(n => n.status === "online" && n.playing).length };
  el.innerHTML = ["all", "online", "offline", "playing"].map(f => `<button class="chip ${S.nodeFilter === f ? "active" : ""}" data-act="nd-filter" data-f="${f}" aria-pressed="${S.nodeFilter === f}">${esc(tcT(f))}<span class="cnt">${esc(I18n.number(counts[f]))}</span></button>`).join("");
  const seats = $("#tc-seats");
  if (seats) seats.textContent = S.license ? tcT("tcSeats", { count: I18n.number(S.nodes.filter(n => n.authorized).length), seats: I18n.number(S.license.effective_seats ?? S.license.seats ?? S.license.builtin_seats ?? 3) }) : tcT("tcLicenseUnread");
}
function paintBulkBar() {
  const el = $("#nd-bulk"); if (!el) return;
  if (!S.sel.size) { el.innerHTML = ""; return; }
  const visible = filteredNodes().filter(n => S.sel.has(n.node_id)).length;
  const upgradable = S.nodes.filter(n => S.sel.has(n.node_id) && tcOutdated(n) && n.status === "online" && n.authorized !== false).length;
  el.innerHTML = `<div class="bulk-bar tc-bulk"><span class="b-cnt">${esc(tcT(visible < S.sel.size ? "tcSelectedHidden" : "tcSelected", { count: I18n.number(S.sel.size), hidden: I18n.number(S.sel.size - visible) }))}</span>
    ${upgradable ? `<button class="btn primary xs" data-act="node-upgrade-selected">${icon("refresh", 13)} ${esc(tcT("tcUpgradeSelected"))}</button>` : ""}
    <button class="btn ${upgradable ? "ghost" : "primary"} xs" data-act="tc-install">${icon("download", 13)} ${esc(tcT("installUpdateServices"))}</button>
    <details class="tc-more"><summary>${esc(tcT("tcMore"))}</summary><div><button class="btn ghost xs" data-act="bulk-auth">${esc(tcT("tcBulkAuth"))}</button><button class="btn ghost xs" data-act="bulk-revoke">${esc(tcT("tcBulkRevoke"))}</button><button class="btn ghost danger xs" data-act="bulk-delete">${esc(tcT("tcBulkDelete"))}</button></div></details>
    <span class="spacer"></span><button class="btn xs ghost" data-act="nd-clear-sel">${esc(tcT("tcClearSelection"))}</button></div>`;
}
function tcServiceSummary(n) {
  const live = S.nodeSvc[n.node_id], apps = S.nodeApps[n.node_id];
  if (n.status === "online" && Array.isArray(live)) {
    const installed = live.filter(s => s.installed);
    return `<div>${esc(installed.length ? installed.map(s => s.name).join(" / ") : tcT("tcNoServices"))}</div><div class="sub">${esc(tcT("tcServiceCounts", { installed: I18n.number(installed.length), running: I18n.number(installed.filter(s => s.running).length) }))}</div>`;
  }
  const status = tcT(n.status !== "online" ? "tcNodeNotOnline" : TC.nodeErrors[n.node_id] || (TC.admin ? "tcLiveNotLoaded" : "tcLiveLogin"));
  return `<div>${esc(apps?.length ? apps.map(a => a.app_id).join(" / ") : tcT("tcInstallUnknown"))}</div><div class="sub">${esc(apps?.length ? tcT("tcHistoryStatus", { status }) : status)}</div>`;
}
function tcNodeIndicator(n) {
  const fleet = S.upd.fleet?.nodes?.find(x => x.node_id === n.node_id);
  const latest = terminalTaskRecords().filter(t => t.nodes.includes(n.node_id) && t.kind !== "rollout")[0];
  if (latest?.group === "failed") return badge(tcT("tcLatestFailed"), "err");
  if (fleet && !["converged", "unsupported"].includes(fleet.state)) return fleetStateBadge(fleet.state);
  if (latest && ["pending", "active", "waiting", "attention"].includes(latest.group)) return badge(latest.label, "info");
  return `<span class="sub">${esc(tcT(TC.errors.length ? "tcTasksNotSynced" : TC.fleetError ? "tcAgentNotLoaded" : "tcNoPending"))}</span>`;
}
function nodeRow(n) {
  const id = n.node_id;
  return `<tr class="${S.sel.has(id) ? "selected" : ""}"><td><label class="check"><input type="checkbox" data-change="node-sel" data-id="${esc(id)}" aria-label="${esc(tcT("tcSelectNode", { name: n.alias || id }))}" ${S.sel.has(id) ? "checked" : ""}><span class="box"></span></label></td>
    <td><button class="tc-name" data-act="node-open" data-id="${esc(id)}">${esc(n.alias || shortId(id))}</button><div class="sub mono">${esc(n.ip || tcT("tcIPNotReported"))}</div><div class="sub">${esc([n.distro, n.arch].filter(Boolean).join(" · "))}</div></td>
    <td>${statusBadge(n.status)}<div class="sub">${n.status === "online" ? n.playing ? esc(tcT("playing")) + (n.active_proto ? " · " + esc(n.active_proto) : "") : esc(tcT("tcIdle")) : esc(tcT("tcPlaybackUnknown"))}</div></td>
    <td><div class="mono">${esc(n.agent_version || tcT("unknown"))}</div>${tcOutdated(n) ? `<div class="sub">→ ${esc(tcTargetVersion())}</div>` : `<div class="sub">${esc(tcT("tcUpToDate"))}</div>`}<div class="sub">${esc(tcFlavorLabel(n))}</div></td>
    <td class="tc-service-cell">${tcServiceSummary(n)}</td><td>${tcNodeIndicator(n)}<div class="sub">${esc(tcT(n.authorized ? "authorized" : "unauthorized"))}</div></td>
    <td><div class="acts">${tcUpgradeButton(n, "xs")}<button class="btn ghost xs" data-act="tc-install" data-id="${esc(id)}">${esc(tcT("installUpdate"))}</button><button class="btn ghost xs" data-act="node-open" data-id="${esc(id)}">${esc(tcT("details"))}</button><button class="btn ghost danger xs" data-act="node-delete" data-id="${esc(id)}" title="${esc(tcT("tcDelete"))}">${icon("trash", 12)} ${esc(tcT("tcDelete"))}</button></div></td></tr>${tcDevStrip(n)}`;
}
function tcDeviceName(d) {
  return d.name || d.reported_name || (d.hw === "default" ? appT("m262") : d.hw || "—");
}
function tcDeviceTable(n, devices, opts = {}) {
  const id = n.node_id, editable = !!opts.editable;
  const rows = devices.map(d => {
    const deviceKey = playbackDeviceKey(d) || id + "::" + d.hw;
    const label = tcDeviceName(d);
    const playable = n.status === "online" && !!d.enabled && !!d.present && !!d.online;
    const editing = editable && TC.devEdit && TC.devEdit.id === id && TC.devEdit.hw === d.hw;
    const states = [];
    if (d.dsp_enabled) states.push(badge(tcT("tcDevEQ"), "info"));
    if (playable) states.push(badge(tcT("tcDevReady"), "ok"));
    else {
      if (!d.enabled) states.push(badge(tcT("tcDevDisabled"), "muted"));
      if (!d.present) states.push(badge(tcT("tcDevAbsent"), "muted"));
      if (!d.online) states.push(badge(tcT("offline"), "muted"));
      if (n.status !== "online") states.push(badge(tcT("tcNodeNotOnline"), "muted"));
    }
    const nameCell = editing
      ? `<div class="tc-dev-edit"><input class="inp sm tc-dev-input" data-id="${esc(id)}" data-hw="${esc(d.hw)}" value="${esc(TC.devEdit.value)}" maxlength="40" aria-label="${esc(tcT("tcDevRename"))}"><button class="btn primary xs" data-act="tc-dev-rename-save" data-id="${esc(id)}" data-hw="${esc(d.hw)}" title="${esc(appT("m858"))}">${icon("check", 12)}</button><button class="btn ghost xs" data-act="tc-dev-rename-cancel" title="${esc(appT("m063"))}">${icon("x", 12)}</button></div>`
      : `<div class="tc-dev-name-line"><span class="tc-dev-name">${esc(tcDeviceName(d))}</span>${editable && TC.admin ? `<button class="btn ghost xs" data-act="tc-dev-rename" data-id="${esc(id)}" data-hw="${esc(d.hw)}" title="${esc(tcT("tcDevRename"))}">${icon("edit", 12)} ${esc(tcT("tcDevRename"))}</button>` : ""}</div>`;
    const activateCell = editable ? `<td><label class="switch sm" title="${esc(tcT("tcDevActivate"))}"><input type="checkbox" data-change="tc-dev-act" data-id="${esc(id)}" data-hw="${esc(d.hw)}" ${d.enabled ? "checked" : ""} ${TC.admin ? "" : "disabled"} aria-label="${esc(tcT("tcDevActivate"))} ${esc(label)}"><span class="track"></span></label></td>` : "";
    const playCell = editable ? "" : `<td>${playable ? terminalPlaySummary(opts.playStates?.[deviceKey]) : ""}</td>`;
    return `<tr class="tc-device-row ${d.enabled ? "on" : ""} ${d.present ? "" : "absent"}">${activateCell}<td>${nameCell}</td><td class="mono tc-dev-key">${esc(deviceKey)}</td><td><div class="tc-dev-states">${states.join("")}</div>${editable && TC.admin ? `<select class="inp xs tc-dev-dsd" data-change="tc-dev-dsd" data-id="${esc(id)}" data-hw="${esc(d.hw)}" aria-label="${esc(tcT("tcDsdMode"))}" title="${esc(tcT("tcDsdMode"))}"><option value="pcm" ${d.dsd_mode !== "dop" ? "selected" : ""}>${esc(tcT("tcDsdPcm"))}</option><option value="dop" ${d.dsd_mode === "dop" ? "selected" : ""}>${esc(tcT("tcDsdDop"))}</option></select>` : ""}</td>${playCell}</tr>`;
  }).join("");
  return `<div class="tc-device-table-wrap"><table class="tbl tc-device-table"><tbody>${rows}</tbody></table></div>`;
}
function tcDevStrip(n) {
  const id = n.node_id, devs = S.nodeDevs[id];
  if (!Array.isArray(devs)) return `<tr class="tc-dev-row"><td colspan="7"><div class="tc-dev-empty"><span class="lbl">${esc(tcT("tcPlayDevices"))}</span><span class="sub">${esc(tcT("tcDevsNotLoaded"))}</span></div></td></tr>`;
  if (!devs.length) return `<tr class="tc-dev-row"><td colspan="7"><div class="tc-dev-empty"><span class="lbl">${esc(tcT("tcPlayDevices"))}</span><span class="sub">${esc(tcT("tcNoDevs"))}</span></div></td></tr>`;
  return `<tr class="tc-dev-row"><td colspan="7">${tcDeviceTable(n, devs, { editable: true })}</td></tr>`;
}
function paintNodeTable() {
  const el = $("#nd-table"); if (!el) return;
  const active = document.activeElement;
  const focus = active && active.classList && active.classList.contains("tc-dev-input") && TC.devEdit ? {
    id: TC.devEdit.id, hw: TC.devEdit.hw, value: active.value,
    start: active.selectionStart, end: active.selectionEnd, direction: active.selectionDirection
  } : null;
  if (focus) TC.devEdit.value = focus.value;
  const list = filteredNodes(), all = list.length > 0 && list.every(n => S.sel.has(n.node_id));
  if (TC.loading) { el.innerHTML = empty("refresh", tcT("tcLoadingNodes"), "", true); return; }
  if (!list.length) { el.innerHTML = empty("server", tcT(S.nodes.length ? "tcNoMatches" : TC.errors.length ? "tcNodesUnavailable" : "tcNoNodes"), "", true); return; }
  el.innerHTML = `<div class="tbl-wrap"><table class="tbl tc-node-table"><thead><tr><th><label class="check"><input type="checkbox" data-change="node-sel-all" aria-label="${esc(tcT("tcSelectAll"))}" ${all ? "checked" : ""}><span class="box"></span></label></th><th><button class="tc-sort" data-act="nd-sort" data-key="alias">${esc(tcT("node"))} ${S.nodeSort.dir > 0 ? "↑" : "↓"}</button></th><th>${esc(tcT("tcConnectionPlayback"))}</th><th>${esc(tcT("tcAgentVersion"))}</th><th>${esc(tcT("installedServices"))}</th><th>${esc(tcT("tcTaskLicense"))}</th><th>${esc(tcT("tcActions"))}</th></tr></thead><tbody>${list.map(nodeRow).join("")}</tbody></table></div>`;
  const select = $("[data-change=node-sel-all]"); if (select) select.indeterminate = !all && list.some(n => S.sel.has(n.node_id));
  if (focus && TC.devEdit && TC.devEdit.id === focus.id && TC.devEdit.hw === focus.hw) {
    const next = Array.from(el.querySelectorAll(".tc-dev-input")).find(x => x.dataset.id === focus.id && x.dataset.hw === focus.hw);
    if (next) {
      next.value = TC.devEdit.value;
      next.focus({ preventScroll: true });
      if (typeof focus.start === "number" && typeof next.setSelectionRange === "function") {
        try { next.setSelectionRange(focus.start, focus.end, focus.direction); } catch { /* Unsupported input type. */ }
      }
    }
  }
}

function terminalTaskRecords() {
  const jobs = S.jobs.map(j => {
    const state = jobState(j), phase = currentPhase(j);
    return { id: j.id, kind: "job", type: "job", nodes: jobNodes(j), title: tcT("tcJobTitle", { kind: JOB_ZH[j.kind] || j.kind || tcT("tcDeploy"), template: j.bom_id || tcT("tcTemplateUnrecorded"), version: j.bom_version ? "@" + j.bom_version : tcT("tcVersionUnrecorded") }), state, label: JOB_ZH[state] || state,
      group: state === "done" ? "done" : state.startsWith("failed") ? "failed" : state === "queued" ? "pending" : state === "waiting_idle" ? "waiting" : ["suspended", "interrupted"].includes(state) ? "attention" : "active", time: j.created_at, detail: j.reason ? I18n.detail(j, "features.tcJobReason") : JOB_ZH[phase.name] || phase.name || "", raw: j };
  });
  const commands = TC.commands.map(c => {
    const finished = c.status === "done", outcome = finished && ["ok", "err"].includes(c.result_status) ? c.result_status : "";
    return { id: c.id, kind: "command", type: c.type.startsWith("apt-") ? "apt" : c.type === "agent-upgrade" ? "agent" : "system", nodes: [c.node_id], title: CMD_ZH[c.type] || c.type,
      state: outcome || c.status, label: tcT(outcome === "ok" ? "done" : outcome === "err" ? "failed" : c.status === "pending" ? "tcAwaitingReport" : "tcResultUnknown"),
      group: outcome === "ok" ? "done" : outcome === "err" ? "failed" : c.status === "pending" ? "pending" : "attention", time: c.created_at,
      detail: c.params?.packages?.join(" / ") || c.params?.version || c.type, raw: c };
  });
  const rollouts = S.rollouts.map(r => ({ id: r.id, kind: "rollout", type: "rollout", nodes: [...new Set((r.rings || []).flatMap(x => x.nodes || []))], title: r.name || r.id, state: r.status,
    label: tcRolloutLabel(r.status), group: r.status === "done" ? "done" : r.status === "failed" ? "failed" : r.status === "pending" ? "pending" : r.status === "running" ? "active" : "attention", time: r.created_at, detail: tcT("tcRingCount", { template: `${r.bom_id}@${r.bom_version}`, count: I18n.number((r.rings || []).length) }), raw: r }));
  return [...jobs, ...commands, ...rollouts].sort((a, b) => ts(b.time) - ts(a.time) || a.id.localeCompare(b.id));
}
function filteredTerminalTasks() {
  return terminalTaskRecords().filter(t => (!TC.taskNode || t.nodes.includes(TC.taskNode)) && (TC.taskType === "all" || t.type === TC.taskType) && (TC.taskState === "all" || t.group === TC.taskState) && hit(t.id, t.title, t.detail, t.label, ...t.nodes.map(tcNodeName), ...t.nodes));
}
function paintTerminalFilters(force = false) {
  const el = $("#tc-task-filters"); if (!el || (!force && el.contains(document.activeElement))) return;
  const nodes = [["", tcT("tcAllNodes")], ...S.nodes.map(n => [n.node_id, n.alias || shortId(n.node_id)])];
  if (TC.taskNode && !nodes.some(([id]) => id === TC.taskNode)) nodes.push([TC.taskNode, tcNodeName(TC.taskNode)]);
  const fields = [["node", tcT("node"), TC.taskNode, nodes], ["type", tcT("tcType"), TC.taskType, [["all", tcT("tcAllTypes")], ["job", tcT("jobType")], ["apt", tcT("tcAPTType")], ["system", tcT("tcSystemType")], ["agent", tcT("tcAgentType")], ["rollout", tcT("tcRolloutType")]]], ["state", tcT("status"), TC.taskState, [["all", tcT("tcAllStates")], ["pending", tcT("tcPending")], ["active", tcT("active")], ["waiting", tcT("tcWaitingIdle")], ["done", tcT("done")], ["failed", tcT("failed")], ["attention", tcT("tcAttention")]]]];
  el.innerHTML = fields.map(([key, label, value, options]) => `<label class="field"><span>${esc(label)}</span><select class="inp" data-change="tc-task-filter" data-key="${key}" aria-label="${esc(tcT("tcTaskFilter", { field: label }))}">${options.map(([id, name]) => `<option value="${esc(id)}" ${id === value ? "selected" : ""}>${esc(name)}</option>`).join("")}</select></label>`).join("") + `<span class="hint">${esc(tcT("tcPersistentNote"))}</span>`;
}
function paintTerminalTasks() {
  const el = $("#tc-task-list"), pages = $("#tc-task-pages"); if (!el || !pages) return;
  if (TC.loading) { el.innerHTML = empty("refresh", tcT("tcLoadingTasks"), "", true); pages.innerHTML = ""; return; }
  const list = filteredTerminalTasks(), count = Math.max(1, Math.ceil(list.length / 25)); TC.page = Math.min(TC.page, count);
  el.innerHTML = list.length ? `<div class="tbl-wrap"><table class="tbl tc-task-table"><thead><tr><th>${esc(tcT("tcTask"))}</th><th>${esc(tcT("node"))}</th><th>${esc(tcT("status"))}</th><th>${esc(tcT("createdAt"))}</th><th>${esc(tcT("tcRecord"))}</th></tr></thead><tbody>${list.slice((TC.page - 1) * 25, TC.page * 25).map(t => `<tr><td><div>${esc(t.title)}</div><div class="sub tc-task-detail">${esc(t.detail)}</div></td><td>${esc(t.nodes.map(tcNodeName).join(" / ") || tcT("unassigned"))}</td><td>${badge(t.label, t.group === "failed" ? "err" : t.group === "done" ? "ok" : "info")}<div class="sub mono">${esc(t.state || "unknown")}</div></td><td class="nowrap">${esc(tcTime(t.time))}</td><td><button class="btn ghost xs" data-act="tc-task-open" data-kind="${t.kind}" data-id="${esc(t.id)}">${esc(tcT("tcDetailsLogs"))}</button></td></tr>`).join("")}</tbody></table></div>` : empty("activity", tcT(TC.errors.length ? "tcNoTasksPartial" : "tcNoTasks"), "", true);
  pages.innerHTML = `<span class="hint">${esc(tcT("tcTaskPages", { count: I18n.number(list.length), page: I18n.number(TC.page), pages: I18n.number(count) }))}</span><span class="spacer"></span><button class="btn ghost sm" data-act="tc-page" data-dir="-1" ${TC.page <= 1 ? "disabled" : ""}>${esc(tcT("previousPage"))}</button><button class="btn ghost sm" data-act="tc-page" data-dir="1" ${TC.page >= count ? "disabled" : ""}>${esc(tcT("nextPage"))}</button>`;
}
function terminalTaskBody(t) {
  let html = `<div class="kv">${kvRow(tcT("tcTaskID"), esc(t.id))}${kvRow(tcT("node"), esc(t.nodes.map(tcNodeName).join(" / ") || tcT("unassigned")))}${kvRow(tcT("status"), esc(`${t.label} · ${t.state || "unknown"}`))}${kvRow(tcT("createdAt"), esc(tcTime(t.time)))}</div>`;
  if (t.kind === "command") {
    const c = t.raw;
    if (c.status === "pending") html += `<div class="banner info mt12">${esc(tcT("tcPendingWarning"))}</div>`;
    if (c.finished_at) html += `<div class="kv mt12">${kvRow(tcT("tcFinishedAt"), esc(tcTime(c.finished_at)))}</div>`;
    html += `<div class="d-sec"><span class="lbl">${esc(tcT("tcParameters"))}</span><pre class="log-box">${esc(JSON.stringify(c.params || {}, null, 2))}</pre></div><div class="d-sec"><span class="lbl">${esc(tcT("tcExecutionLogs"))}</span><pre class="log-box">${esc(c.log || tcT("tcNoLogs"))}</pre></div>`;
    if (c.data) html += `<div class="d-sec"><span class="lbl">${esc(tcT("tcResultData"))}</span><pre class="log-box">${esc(typeof c.data === "string" ? c.data : JSON.stringify(c.data, null, 2))}</pre></div>`;
  } else {
    html += `<div class="d-sec"><span class="lbl">${esc(tcT("tcRingRecords", { detail: t.detail }))}</span>${(t.raw.rings || []).map(r => `<div class="tc-ring"><div>${esc(r.name)} · ${esc(I18n.number(Number(r.percent) / 100, { style: "percent", maximumFractionDigits: 2 }))} · ${esc(tcRolloutLabel(r.status))}</div><p class="hint">${esc(tcT("tcRingNodes", { nodes: (r.nodes || []).map(tcNodeName).join(" / ") || tcT("unassigned") }))}</p><div class="acts">${(r.jobs || []).map(id => `<button class="btn ghost xs" data-act="job-open" data-id="${esc(id)}">${esc(tcT("tcJobLink", { id: shortId(id) }))}</button>`).join("") || `<span class="hint">${esc(tcT("tcNoJobs"))}</span>`}</div></div>`).join("")}</div>`;
  }
  return html;
}
function openTerminalTask(kind, id) {
  if (kind === "job") return openJobDrawer(id);
  const t = terminalTaskRecords().find(x => x.kind === kind && x.id === id); if (!t) return;
  openDrawer(esc(t.title), esc(t.id), terminalTaskBody(t), `<button class="btn ghost" data-act="tc-tasks">${esc(tcT("tcBackTasks"))}</button>`, { ctx: { kind: "tc-task", id }, onClose: () => stopPoll("tc:task") });
  const context = drawerCtx;
  poll("tc:task", async () => {
    if (drawerCtx !== context) return;
    await refreshTerminalCenter();
    if (drawerCtx !== context) return;
    const latest = terminalTaskRecords().find(x => x.kind === kind && x.id === id);
    if (latest) setDrawerBody(terminalTaskBody(latest));
  }, 4000);
}

async function openNodeDrawer(id) {
  TC.detailTab = "overview"; TC.detail = null;
  openDrawer(esc(tcNodeName(id)), esc(id), empty("refresh", tcT("tcLoadingNodes"), "", true), "", { ctx: { kind: "node", id }, onClose: () => { stopPoll("node:poll"); TC.detail = null; } });
  const context = drawerCtx;
  let busy = false;
  const refresh = async () => {
    if (busy || drawerCtx !== context) return;
    busy = true;
    try {
      const n = await api("GET", `/nodes/${enc(id)}`);
      const responses = await Promise.allSettled([api("GET", `/nodes/${enc(id)}/apps`), api("GET", `/nodes/${enc(id)}/devices`), n.status === "online" && TC.admin ? api("GET", `/nodes/${enc(id)}/local/api/status`) : Promise.resolve(null)]);
      if (drawerCtx !== context) return;
      S.nodeApps[id] = responses[0].status === "fulfilled" ? (Array.isArray(responses[0].value) ? responses[0].value : responses[0].value?.apps || []) : null;
      const devices = responses[1].status === "fulfilled" && Array.isArray(responses[1].value?.devices)
        ? responses[1].value.devices.map(d => ({ ...d, node_id: id, device_key: playbackDeviceKey(d) || id + "::" + d.hw }))
        : null;
      S.nodeDevs[id] = devices;
      S.nodeSvc[id] = responses[2].status === "fulfilled" && Array.isArray(responses[2].value?.services) ? responses[2].value.services : null;
      const playable = (devices || []).filter(d => n.status === "online" && d.enabled && d.present && d.online);
      const statuses = await Promise.allSettled(playable.map(d => api("GET", `/nodes/${enc(id)}/play?device_key=${enc(playbackDeviceKey(d))}`)));
      if (drawerCtx !== context) return;
      const keys = new Set((devices || []).map(playbackDeviceKey));
      for (const key of Object.keys(S.nodePlay)) if (playbackNodeID(key) === id && !keys.has(key)) delete S.nodePlay[key];
      playable.forEach((d, i) => { S.nodePlay[playbackDeviceKey(d)] = statuses[i].status === "fulfilled" ? statuses[i].value : null; });
      TC.detail = n;
      setDrawerBody(nodeDrawerBody(n, S.nodeApps[id]));
      setDrawerTitle(esc(n.alias || shortId(id)), esc(id));
      setDrawerFoot(`<button class="btn ghost" data-act="tc-tasks" data-id="${esc(id)}">${esc(tcT("taskRecords"))}</button><div class="spacer"></div><button class="btn primary" data-act="tc-install" data-id="${esc(id)}">${esc(tcT("installUpdateServices"))}</button>`);
    } catch (e) {
      if (drawerCtx === context) setDrawerBody(`<div class="banner err">${esc(tcT("tcNodeDetailsFailed", { message: e.message }))}</div><button class="btn ghost" data-act="node-open" data-id="${esc(id)}">${esc(tcT("reload"))}</button>`);
    } finally { busy = false; }
  };
  await refresh();
  if (drawerCtx === context) poll("node:poll", refresh, 4000);
}
function nodeDrawerBody(n, apps) {
  const id = n.node_id;
  let html = `<nav class="tc-tabs" aria-label="${esc(tcT("tcNodeDetails"))}">${[["overview", tcT("tcOverview")], ["services", tcT("installedServices")], ["maintenance", tcT("tcMaintenance")]].map(([key, name]) => `<button class="btn ghost sm ${TC.detailTab === key ? "active" : ""}" data-act="tc-detail-tab" data-tab="${key}" aria-pressed="${TC.detailTab === key}">${esc(name)}</button>`).join("")}</nav>`;
  if (n.status !== "online") html += `<div class="banner warn">${esc(tcT("tcOfflineWarning", { time: tcAgo(n.last_seen) }))}</div>`;
  if (TC.detailTab === "services") {
    const services = n.status === "online" && Array.isArray(S.nodeSvc[id]) ? S.nodeSvc[id].filter(s => s.installed) : null;
    html += `<p class="hint">${esc(tcT("tcInstalledNote"))}</p>`;
    if (services) return html + (services.length ? `<div class="tbl-wrap"><table class="tbl tc-services"><thead><tr><th>${esc(tcT("tcServiceVersion"))}</th><th>${esc(tcT("tcSource"))}</th><th>${esc(tcT("tcRuntimeStatus"))}</th></tr></thead><tbody>${services.map(s => `<tr><td>${esc(s.name)}<div class="sub">${esc(s.version || tcT("versionUnknown"))} · ${esc(s.protocol || tcT("tcProtocolNotReported"))}</div><div class="sub mono">${esc(s.unit || s.process || "")}</div></td><td>${tcSourceBadge(s.source)}</td><td>${s.running ? badge(tcT("tcRunning"), "ok") : badge(tcT("tcNotRunning"), "muted")}</td></tr>`).join("")}</tbody></table></div>` : empty("box", tcT("tcNoAudioServices"), "", true));
    html += `<div class="banner info">${esc(tcT(TC.admin ? "tcLiveUnavailable" : "tcLiveUnavailableLogin"))}</div>`;
    return html + (apps?.length ? `<div class="tbl-wrap"><table class="tbl tc-services"><thead><tr><th>${esc(tcT("tcReportedApp"))}</th><th>${esc(tcT("tcVersion"))}</th><th>${esc(tcT("tcSource"))}</th><th>${esc(tcT("tcCurrentStatus"))}</th></tr></thead><tbody>${apps.map(a => `<tr><td>${esc(a.app_id)}</td><td>${esc(a.version || tcT("unknown"))}</td><td>${tcSourceBadge(a.source)}</td><td>${esc(tcT("unknown"))}</td></tr>`).join("")}</tbody></table></div>` : empty("box", tcT(apps === null ? "tcHistoryFailed" : "tcNoHistory"), "", true));
  }
  if (TC.detailTab === "maintenance") {
    return html + `<div class="d-sec"><span class="lbl">${esc(tcT("tcDevicesProtection"))}</span><div class="kv">${kvRow(tcT("tcDACHolder"), n.dac_holder_pid ? `PID ${Number(n.dac_holder_pid)}` : esc(tcT("tcNoHolder")))}${kvRow(tcT("tcProtection"), n.play_protect ? badge(tcT("tcEnabled"), "ok") : badge(tcT("tcDisabled"), "warn"))}</div><p class="hint">${esc(tcT("tcDevsManagedNote"))}</p><button class="btn ghost sm" data-act="node-config" data-id="${esc(id)}">${esc(tcT("tcEditConfig"))}</button></div>
      <div class="d-sec"><span class="lbl">${esc(tcT("tcSystemMaintenance"))}</span><div class="kv">${kvRow(tcT("tcAutoOSUpdate"), esc(tcT(n.auto_os_update ? "tcOn" : "tcOff")))}${kvRow(tcT("tcOSWindow"), esc(n.os_window || tcT("tcNotSet")))}${kvRow(tcT("tcAgentVersion"), esc(n.agent_version || tcT("unknown")))}${kvRow(tcT("tcTargetVersion"), esc(tcTargetVersion() || tcT("unknown")))}${kvRow(tcT("tcBuildFlavor"), esc(tcFlavorLabel(n)))}</div><p class="hint">${esc(tcT("tcUpdatesNote"))}</p><div class="acts">${tcUpgradeButton(n, "sm")}<button class="btn ghost sm" data-act="node-os" data-id="${esc(id)}">${esc(tcT("tcOSPackages"))}</button><button class="btn ghost sm" data-act="node-net" data-id="${esc(id)}">${esc(tcT("tcNetwork"))}</button></div></div>
      <details class="tc-advanced-details"><summary>${esc(tcT("tcAdvancedActions"))}</summary><p class="hint">${esc(tcT("tcNativeNote"))}</p><div class="acts"><button class="btn ghost sm" data-act="node-console" data-id="${esc(id)}">${esc(tcT("tcNativeConsole"))}</button><button class="btn ghost sm" data-act="${n.authorized ? "node-revoke" : "node-auth"}" data-id="${esc(id)}">${esc(tcT(n.authorized ? "tcRevoke" : "tcAuthorize"))}</button></div></details>`;
  }
  const fleet = tcFleetNode(n), target = tcTargetVersion(), outdated = tcOutdated(n);
  return html + `<div class="d-sec"><span class="lbl">${esc(tcT("tcAgentVersion"))}</span><div class="kv">${kvRow(tcT("tcAgentVersion"), esc(n.agent_version || tcT("unknown")))}${kvRow(tcT("tcTargetVersion"), esc(target || tcT("unknown")))}${kvRow(tcT("tcBuildFlavor"), esc(tcFlavorLabel(n)))}${kvRow(tcT("status"), fleet ? fleetStateBadge(fleet.state) : badge(tcT(outdated ? "tcUpgrade" : "tcUpToDate"), outdated ? "info" : "ok"))}</div>${outdated ? `<div class="acts mt8">${tcUpgradeButton(n)}</div>` : ""}<p class="hint">${esc(tcT("tcUpgradeAgentNote"))}</p></div><div class="kv">${kvRow(tcT("tcConnection"), statusBadge(n.status))}${kvRow(tcT("tcAuthorization"), authBadge(n))}${kvRow("IP", esc(n.ip || tcT("notReported")))}${kvRow(tcT("tcSystemArch"), esc([n.distro, n.arch].filter(Boolean).join(" · ") || tcT("notReported")))}${kvRow(tcT("tcLastHeartbeat"), esc(tcTime(n.last_seen)))}${kvRow(tcT("tcDesiredTemplate"), n.bom_id ? esc(`${n.bom_id}@${n.bom_version}`) : esc(tcT("tcUnbound")))}${kvRow(tcT("tcGroup"), esc(n.group || tcT("tcUngrouped")))}${n.note ? kvRow(tcT("tcNote"), esc(n.note)) : ""}</div>
    <div class="d-sec"><span class="lbl">${esc(tcT("tcPlayback"))}</span><div id="tc-play-summary">${terminalPlaybackSection(n)}</div></div>
    <div class="d-sec"><span class="lbl">${esc(tcT("tcResources"))}</span><div class="tc-resources"><div><span>CPU</span><strong>${esc(n.cpu == null ? tcT("unknown") : I18n.number(Number(n.cpu) / 100, { style: "percent", minimumFractionDigits: 1, maximumFractionDigits: 1 }))}</strong></div><div><span>${esc(tcT("tcMemory"))}</span><strong>${esc(n.mem_mb == null ? tcT("unknown") : I18n.number(Number(n.mem_mb), { maximumFractionDigits: 0 }) + " MB")}</strong></div><div><span>${esc(tcT("tcTemperature"))}</span><strong>${esc(n.temp_c ? I18n.number(Number(n.temp_c), { minimumFractionDigits: 1, maximumFractionDigits: 1 }) + " °C" : tcT("notReported"))}</strong></div></div></div>`;
}
function tcSourceBadge(source) { return source === "system" ? badge(tcT("sourceSystem"), "info") : ["server", "managed"].includes(source) ? badge(tcT("sourceServer"), "ok") : badge(source || tcT("sourceUnknown"), "muted"); }
function terminalPlaySummary(p) {
  if (!p) return `<span class="hint">${esc(tcT("tcPlaybackFailed"))}</span>`;
  return `<span>${esc(p.connected ? p.title ? p.title : tcT(p.state === "paused" ? "tcPaused" : "tcZATNoTrack") : tcT("tcZATDisconnected"))}</span><span class="hint">${esc([p.artist, p.album].filter(Boolean).join(" · "))}</span>`;
}
function terminalPlaybackSection(n) {
  const id = n.node_id, devices = S.nodeDevs[id];
  if (!Array.isArray(devices)) return empty("box", tcT("tcDevsNotLoaded"), "", true);
  if (!devices.length) return empty("box", tcT("tcNoDevs"), "", true);
  return tcDeviceTable(n, devices, { playStates: S.nodePlay }) + (n.active_proto ? `<p class="hint">${esc(tcT("tcActiveProtocol", { protocol: n.active_proto }))}</p>` : "");
}
async function openTerminalAdvanced() {
  closeDrawer();
  openModal(tcT("tcAdvancedTitle"), `<div id="tc-advanced"><p class="hint">${esc(tcT("tcGlobalMaintenanceNote"))}</p><div class="d-sec"><span class="lbl">${esc(tcT("tcAgentPolicy"))}</span><div id="upd-body" data-update-scope="fleet"><div id="upd-policy"></div><div id="upd-fleet" class="mt16"></div></div></div><details class="tc-advanced-details"><summary>${esc(tcT("tcAdvancedRollouts"))}</summary><p class="hint">${esc(tcT("tcRolloutNote"))}</p><div class="acts"><button class="btn ghost sm" data-act="tc-packages">${esc(tcT("tcManagePackages"))}</button><button class="btn primary sm" data-act="ro-create">${esc(tcT("tcNewRollout"))}</button></div><div id="dl-rollouts"></div></details></div>`, `<button class="btn ghost" data-act="modal-close">${esc(tcT("close"))}</button>`, { onClose: () => stopPoll("tc:advanced") });
  const root = $("#tc-advanced");
  await refreshUpdate();
  if ($("#tc-advanced") !== root) return;
  try {
    const [boms, rollouts] = await Promise.all([api("GET", "/boms?status=published"), api("GET", "/rollouts")]);
    if ($("#tc-advanced") !== root) return;
    S.pubBoms = boms || []; S.rollouts = rollouts || []; paintRollouts();
  } catch (e) { if ($("#tc-advanced") === root) $("#dl-rollouts").innerHTML = `<div class="banner err">${esc(tcT("tcRolloutsFailed", { message: e.message }))}</div>`; }
  if ($("#tc-advanced") === root) poll("tc:advanced", refreshUpdateLive, 4000);
}
async function upgradeTerminalNodes(ids) {
  if (!await terminalRequireAdmin()) return;
  const target = tcTargetVersion();
  const list = (ids || []).map(id => S.nodes.find(n => n.node_id === id)).filter(n => n && tcOutdated(n));
  if (!list.length) { toast(tcT("tcUpToDate"), "info"); return; }
  const names = list.map(n => n.alias || shortId(n.node_id)).join(" / ");
  const flavors = [...new Set(list.map(tcFlavorLabel))].join(" / ");
  const ok = await confirmDlg({
    title: tcT("tcUpgrade"),
    message: tcT("tcUpgradeConfirm", { name: names, current: list[0].agent_version || tcT("unknown"), target, flavor: flavors }),
    okText: tcT("tcUpgrade"), danger: false
  });
  if (!ok) return;
  for (const n of list) {
    try {
      await api("POST", `/nodes/${enc(n.node_id)}/upgrade`);
      toast(tcT("tcUpgradeStarted", { name: n.alias || shortId(n.node_id), version: target }), "ok");
    } catch (e) {
      toast(tcT("tcUpgradeFailed", { message: e.message }), "err");
    }
  }
  await refreshTerminalCenter();
  tcWatchUpgrades();
}
function tcWatchUpgrades() {
  stopPoll("tc:upgrades");
  const pending = () => (S.upd.fleet?.nodes || []).some(r => ["upgrading", "pending"].includes(r.state));
  if (!pending()) return;
  poll("tc:upgrades", async () => {
    await refreshTerminalCenter();
    if (!pending()) stopPoll("tc:upgrades");
  }, 4000);
}
async function terminalRequireAdmin() {
  try { const session = await api("GET", "/settings/session"); TC.admin = session.authenticated === true; }
  catch (e) { TC.admin = false; toast(tcT("tcSessionFailed", { message: e.message }), "err"); return false; }
  if (!TC.admin) { toast(tcT("tcAdminRequired"), "warn"); paintTerminalCenter(); }
  return TC.admin;
}
async function tcOpenTerminalPlayer(id, preferredKey = "") {
  if (!preferredKey || playbackNodeID(preferredKey) !== id) { toast(tcT("tcNoPlayableDevices"), "warn"); return; }
  let device = availablePlaybackDevice(preferredKey);
  if (!device && typeof refreshPlayerBar === "function") {
    try { await refreshPlayerBar(); }
    catch (e) { toast(tcT("tcNoPlayableDevices"), "warn"); return; }
  }
  device = availablePlaybackDevice(preferredKey);
  if (!device || device.node_id !== id) { toast(tcT("tcNoPlayableDevices"), "warn"); return; }
  closeDrawer();
  await openPlayer(preferredKey);
}
Object.assign(ACT, {
  "tc-goto": () => { TC.tab = "nodes"; setView("nodes"); },
  "tc-login": () => openSettings("authorization"),
  "tc-packages": () => openSettings("packages"),
  "tc-tab": ds => setTerminalTab(ds.tab),
  "tc-install": ds => openTerminalInstall(ds.id ? [ds.id] : Array.from(S.sel)),
  "node-upgrade": ds => upgradeTerminalNodes([ds.id]),
  "node-upgrade-selected": () => upgradeTerminalNodes(Array.from(S.sel)),
  "node-upgrade-all": () => upgradeTerminalNodes(S.nodes.filter(n => tcOutdated(n) && n.status === "online" && n.authorized !== false).map(n => n.node_id)),
  "node-delete": async ds => {
    const n = S.nodes.find(x => x.node_id === ds.id);
    const okc = await confirmDlg({ title: tcT("tcDeleteTitle"), message: tcT("tcDeleteMsg", { name: n?.alias || shortId(ds.id) }), okText: tcT("tcDelete") });
    if (okc) await deleteNodes([ds.id]);
  },
  "tc-tasks": ds => showTerminalTasks(ds.id || ""),
  "tc-page": ds => { TC.page = Math.max(1, TC.page + Number(ds.dir)); paintTerminalTasks(); },
  "tc-task-open": ds => openTerminalTask(ds.kind, ds.id),
  "tc-advanced": () => openTerminalAdvanced(),
  "tc-detail-tab": ds => { if (TC.detail && ["overview", "services", "maintenance"].includes(ds.tab)) { TC.detailTab = ds.tab; setDrawerBody(nodeDrawerBody(TC.detail, S.nodeApps[TC.detail.node_id])); } },
  "tc-dev-rename": ds => {
    const d = (S.nodeDevs[ds.id] || []).find(x => x.hw === ds.hw);
    TC.devEdit = { id: ds.id, hw: ds.hw, value: d?.name || "" };
    paintNodeTable();
    const input = Array.from($("#nd-table")?.querySelectorAll(".tc-dev-input") || []).find(x => x.dataset.id === ds.id && x.dataset.hw === ds.hw);
    if (input) input.focus({ preventScroll: true });
  },
  "tc-dev-rename-cancel": () => { TC.devEdit = null; paintNodeTable(); },
  "tc-dev-rename-save": async (ds, el) => {
    const input = el.closest("tr")?.querySelector(".tc-dev-input");
    const name = String(input ? input.value : TC.devEdit?.value || "").trim();
    await api("PATCH", `/nodes/${enc(ds.id)}/devices/${enc(ds.hw)}`, { name });
    TC.devEdit = null;
    toast(tcT("tcDevRenamed"), "ok");
    await refreshTerminalCenter();
  },
  "tc-device-player": ds => tcOpenTerminalPlayer(ds.id, ds.deviceKey || "")
});
CHG["tc-dev-dsd"] = async (el, ds) => {
  await api("PATCH", `/nodes/${enc(ds.id)}/devices/${enc(ds.hw)}/dsd`, { mode: el.value });
  const d = (S.nodeDevs[ds.id] || []).find(x => x.hw === ds.hw);
  if (d) d.dsd_mode = el.value;
  toast(tcT("tcDsdMode") + ": " + tcT(el.value === "dop" ? "tcDsdDop" : "tcDsdPcm"), "ok");
};
CHG["tc-task-filter"] = (el, ds) => { const key = { node: "taskNode", type: "taskType", state: "taskState" }[ds.key]; if (key) { TC[key] = el.value; TC.page = 1; paintTerminalTasks(); } };
for (const action of ["wz-open", "wz-next", "wz-reissue", "wz-auth", "node-console", "node-auth", "node-revoke", "node-delete", "bulk-auth", "bulk-revoke", "bulk-delete", "cfg-save", "cfg-delete", "cfg-auth", "cfg-revoke", "os-check", "os-upgrade", "net-run", "job-advance", "job-rb-complete", "ro-submit", "ro-advance", "ro-delete", "upd-converge", "upd-retry"]) {
  const run = ACT[action];
  ACT[action] = async (...args) => {
    if (tcActive()) {
      const root = $("#terminal-center"), epoch = TC.epoch;
      if (!await terminalRequireAdmin() || !tcHere(root, epoch)) return;
    }
    return run(...args);
  };
}
for (const change of ["upd-policy"]) {
  const run = CHG[change];
  CHG[change] = async (el, ds) => {
    if (tcActive()) {
      const root = $("#terminal-center"), epoch = TC.epoch;
      const allowed = await terminalRequireAdmin();
      if (!tcHere(root, epoch) || !el.isConnected) return;
      if (!allowed) { paintUpdatePolicy(true); return; }
    }
    return run(el, ds);
  };
}
