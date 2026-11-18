"use strict";

const TC = {
  tab: "nodes", epoch: 0, busy: null, admin: false, loading: true,
  errors: [], commands: [], nodeErrors: {}, taskNode: "", taskType: "all", taskState: "all", page: 1,
  detail: null, detailTab: "overview", fleetError: ""
};

function cleanupTerminalCenter() { TC.epoch++; TC.busy = null; TC.loading = true; }
function shellNodes() {
  return `<section id="terminal-center">
    <div class="tc-heading"><div><h1>终端中心</h1><p class="hint">选择终端，安装服务，追踪每一次变更。</p></div>
      <button class="btn ghost sm" data-act="tc-advanced">${icon("sliders", 15)} 高级维护</button></div>
    <nav class="tc-tabs" aria-label="终端中心工作区">${[["nodes", "终端"], ["tasks", "任务记录"]].map(([tab, name]) => `<button class="btn ghost ${TC.tab === tab ? "active" : ""}" data-act="tc-tab" data-tab="${tab}" aria-pressed="${TC.tab === tab}">${name}</button>`).join("")}</nav>
    <div id="tc-access"></div><div id="tc-errors" role="status"></div><div id="tc-workspace">${tcWorkspace()}</div>
  </section>`;
}
function tcWorkspace() {
  if (TC.tab === "tasks") return `<section class="card tc-tasks"><div class="tc-task-filters" id="tc-task-filters"></div><div id="tc-task-list"></div><div id="tc-task-pages" class="tc-pages"></div></section>`;
  return `<section class="card"><div class="sec-head tc-node-head"><div class="chips" id="nd-chips"></div><div class="spacer"></div><span id="tc-seats" class="hint"></span><button class="btn primary sm" data-act="wz-open">${icon("plus", 14)} 添加终端</button></div><div id="nd-bulk"></div><div id="nd-table"></div></section>`;
}
function tcHere(root, epoch) { return S.view === "nodes" && $("#terminal-center") === root && TC.epoch === epoch; }
async function refreshTerminalCenter() {
  const root = $("#terminal-center"), epoch = TC.epoch;
  if (!root || S.view !== "nodes") return;
  if (TC.busy) return TC.busy;
  const work = (async () => {
    const responses = await Promise.allSettled([api("GET", "/nodes"), api("GET", "/jobs"), api("GET", "/rollouts"), api("GET", "/license"), api("GET", "/settings/session"), api("GET", "/update/fleet")]);
    if (!tcHere(root, epoch)) return;
    TC.errors = [];
    if (responses[0].status === "rejected") {
      TC.loading = false; TC.errors.push("终端列表刷新失败，未更新此前数据：" + responses[0].reason.message); paintTerminalCenter(); return;
    }
    S.nodes = responses[0].value || [];
    for (const id of S.sel) if (!S.nodes.some(n => n.node_id === id)) S.sel.delete(id);
    for (const [i, key, name] of [[1, "jobs", "部署作业"], [2, "rollouts", "灰度记录"]]) {
      if (responses[i].status === "fulfilled") S[key] = responses[i].value || [];
      else TC.errors.push(name + "刷新失败，以下可能包含旧记录：" + responses[i].reason.message);
    }
    S.license = responses[3].status === "fulfilled" ? responses[3].value : null;
    TC.admin = responses[4].status === "fulfilled" && responses[4].value.authenticated === true;
    if (responses[4].status === "rejected") TC.errors.push("管理员会话状态暂不可用，管理操作需重新验证。");
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
          n.status === "online" && TC.admin ? api("GET", `/nodes/${enc(id)}/local/api/status`) : Promise.resolve(null)
        ]);
        results.push({ id, values });
      }
    }));
    if (!tcHere(root, epoch)) return;
    const previousCommands = TC.commands;
    TC.commands = []; TC.nodeErrors = {};
    for (const { id, values } of results) {
      const [apps, commands, live] = values;
      if (apps.status === "fulfilled") S.nodeApps[id] = Array.isArray(apps.value) ? apps.value : apps.value?.apps || [];
      else { S.nodeApps[id] = null; TC.nodeErrors[id] = "应用上报读取失败"; }
      S.nodeSvc[id] = live.status === "fulfilled" && Array.isArray(live.value?.services) ? live.value.services : null;
      if (commands.status === "fulfilled") TC.commands.push(...(commands.value || []));
      else {
        TC.commands.push(...previousCommands.filter(c => c.node_id === id));
        TC.errors.push(`${tcNodeName(id)}：命令记录刷新失败，未更新此前结果`);
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
  if (access) access.innerHTML = TC.admin ? "" : `<div class="tc-readonly"><span>${icon("lock", 14)} 只读浏览 · 安装与维护需要管理员登录</span><button class="btn ghost xs" data-act="tc-login">前往设置登录</button></div>`;
  if (errors) errors.innerHTML = TC.errors.length ? `<div class="banner warn"><div>${TC.errors.map(esc).join("<br>")}</div><button class="btn ghost xs" data-act="refresh-view">重新读取</button></div>` : "";
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
  if (S.view !== "nodes") { TC.tab = "tasks"; setView("nodes"); }
  else setTerminalTab("tasks");
  return refreshTerminalCenter();
}
function paintTerminalSearch() { TC.page = 1; if (TC.tab === "tasks") paintTerminalTasks(); else { paintBulkBar(); paintNodeTable(); } }
function tcNodeName(id) { const n = S.nodes.find(n => n.node_id === id); return n?.alias || shortId(id); }
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
  el.innerHTML = [["all", "全部"], ["online", "在线"], ["offline", "离线"], ["playing", "播放中"]].map(([f, label]) => `<button class="chip ${S.nodeFilter === f ? "active" : ""}" data-act="nd-filter" data-f="${f}" aria-pressed="${S.nodeFilter === f}">${label}<span class="cnt">${counts[f]}</span></button>`).join("");
  const seats = $("#tc-seats");
  if (seats) seats.textContent = S.license ? `授权 ${S.nodes.filter(n => n.authorized).length} / ${S.license.effective_seats ?? S.license.seats ?? S.license.builtin_seats ?? 3} 台` : "授权状态未读取";
}
function paintBulkBar() {
  const el = $("#nd-bulk"); if (!el) return;
  if (!S.sel.size) { el.innerHTML = ""; return; }
  const visible = filteredNodes().filter(n => S.sel.has(n.node_id)).length;
  el.innerHTML = `<div class="bulk-bar tc-bulk"><span class="b-cnt">已选 ${S.sel.size} 台${visible < S.sel.size ? `（${S.sel.size - visible} 台不在当前筛选中）` : ""}</span>
    <button class="btn primary xs" data-act="tc-install">${icon("download", 13)} 安装 / 更新服务</button>
    <details class="tc-more"><summary>更多操作</summary><div><button class="btn ghost xs" data-act="bulk-auth">批量授权</button><button class="btn ghost xs" data-act="bulk-revoke">批量撤销授权</button><button class="btn ghost danger xs" data-act="bulk-delete">批量注销</button></div></details>
    <span class="spacer"></span><button class="btn xs ghost" data-act="nd-clear-sel">取消选择</button></div>`;
}
function tcServiceSummary(n) {
  const live = S.nodeSvc[n.node_id], apps = S.nodeApps[n.node_id];
  if (n.status === "online" && Array.isArray(live)) {
    const installed = live.filter(s => s.installed);
    return `<div>${installed.length ? esc(installed.map(s => s.name).join("、")) : "未检测到已装服务"}</div><div class="sub">${installed.length} 项已安装 · ${installed.filter(s => s.running).length} 项运行中</div>`;
  }
  return `<div>${apps?.length ? esc(apps.map(a => a.app_id).join("、")) : "安装状态未知"}</div><div class="sub">${apps?.length ? "历史上报 · " : ""}${n.status !== "online" ? "终端未在线" : TC.nodeErrors[n.node_id] || (TC.admin ? "实时状态未获取" : "实时检测需登录")}</div>`;
}
function tcNodeIndicator(n) {
  const fleet = S.upd.fleet?.nodes?.find(x => x.node_id === n.node_id);
  const latest = terminalTaskRecords().filter(t => t.nodes.includes(n.node_id) && t.kind !== "rollout")[0];
  if (latest?.group === "failed") return badge("最近任务失败", "err");
  if (fleet && !["converged", "unsupported"].includes(fleet.state)) return fleetStateBadge(fleet.state);
  if (latest && ["pending", "active", "waiting", "attention"].includes(latest.group)) return badge(latest.label, "info");
  return `<span class="sub">${TC.errors.length ? "任务状态未完全同步" : TC.fleetError ? "Agent 更新状态未获取" : "无待处理任务"}</span>`;
}
function nodeRow(n) {
  const id = n.node_id;
  return `<tr class="${S.sel.has(id) ? "selected" : ""}"><td><label class="check"><input type="checkbox" data-change="node-sel" data-id="${esc(id)}" aria-label="选择 ${esc(n.alias || id)}" ${S.sel.has(id) ? "checked" : ""}><span class="box"></span></label></td>
    <td><button class="tc-name" data-act="node-open" data-id="${esc(id)}">${esc(n.alias || shortId(id))}</button><div class="sub mono">${esc(n.ip || "IP 未上报")}</div><div class="sub">${esc([n.distro, n.arch].filter(Boolean).join(" · "))}</div></td>
    <td>${statusBadge(n.status)}<div class="sub">${n.status === "online" ? n.playing ? "播放中" + (n.active_proto ? " · " + esc(n.active_proto) : "") : "空闲" : "播放状态未知"}</div></td>
    <td class="tc-service-cell">${tcServiceSummary(n)}</td><td>${tcNodeIndicator(n)}<div class="sub">${n.authorized ? "已授权" : "未授权"}</div></td>
    <td><div class="acts"><button class="btn xs" data-act="tc-install" data-id="${esc(id)}">安装 / 更新</button><button class="btn ghost xs" data-act="node-open" data-id="${esc(id)}">详情</button></div></td></tr>`;
}
function paintNodeTable() {
  const el = $("#nd-table"); if (!el) return;
  const list = filteredNodes(), all = list.length > 0 && list.every(n => S.sel.has(n.node_id));
  if (TC.loading) { el.innerHTML = empty("refresh", "正在读取终端…", "", true); return; }
  if (!list.length) { el.innerHTML = empty("server", S.nodes.length ? "没有匹配的终端" : TC.errors.length ? "终端数据不可用，请重新读取" : "还没有终端，添加终端后即可安装服务", "", true); return; }
  el.innerHTML = `<div class="tbl-wrap"><table class="tbl tc-node-table"><thead><tr><th><label class="check"><input type="checkbox" data-change="node-sel-all" aria-label="选择当前筛选的全部终端" ${all ? "checked" : ""}><span class="box"></span></label></th><th><button class="tc-sort" data-act="nd-sort" data-key="alias">终端 ${S.nodeSort.dir > 0 ? "↑" : "↓"}</button></th><th>连接 / 播放</th><th>已装服务</th><th>任务 / 授权</th><th>操作</th></tr></thead><tbody>${list.map(nodeRow).join("")}</tbody></table></div>`;
  const select = $("[data-change=node-sel-all]"); if (select) select.indeterminate = !all && list.some(n => S.sel.has(n.node_id));
}

function terminalTaskRecords() {
  const jobs = S.jobs.map(j => {
    const state = jobState(j), phase = currentPhase(j);
    return { id: j.id, kind: "job", type: "job", nodes: jobNodes(j), title: `${JOB_ZH[j.kind] || j.kind || "部署"} · ${j.bom_id || "模板未记录"}${j.bom_version ? "@" + j.bom_version : "（版本未记录）"}`, state, label: JOB_ZH[state] || state,
      group: state === "done" ? "done" : state.startsWith("failed") ? "failed" : state === "queued" ? "pending" : state === "waiting_idle" ? "waiting" : ["suspended", "interrupted"].includes(state) ? "attention" : "active", time: j.created_at, detail: j.reason || JOB_ZH[phase.name] || phase.name || "", raw: j };
  });
  const commands = TC.commands.map(c => {
    const finished = c.status === "done", outcome = finished && ["ok", "err"].includes(c.result_status) ? c.result_status : "";
    return { id: c.id, kind: "command", type: c.type.startsWith("apt-") ? "apt" : c.type === "agent-upgrade" ? "agent" : "system", nodes: [c.node_id], title: CMD_ZH[c.type] || c.type,
      state: outcome || c.status, label: outcome === "ok" ? "已完成" : outcome === "err" ? "失败" : c.status === "pending" ? "待回报" : "结果未知",
      group: outcome === "ok" ? "done" : outcome === "err" ? "failed" : c.status === "pending" ? "pending" : "attention", time: c.created_at,
      detail: c.params?.packages?.join("、") || c.params?.version || c.type, raw: c };
  });
  const rollouts = S.rollouts.map(r => ({ id: r.id, kind: "rollout", type: "rollout", nodes: [...new Set((r.rings || []).flatMap(x => x.nodes || []))], title: r.name || r.id, state: r.status,
    label: ({ done: "已完成", failed: "失败", running: "进行中", pending: "待推进" })[r.status] || r.status || "状态未知", group: r.status === "done" ? "done" : r.status === "failed" ? "failed" : r.status === "pending" ? "pending" : r.status === "running" ? "active" : "attention", time: r.created_at, detail: `${r.bom_id}@${r.bom_version} · ${(r.rings || []).length} 个环`, raw: r }));
  return [...jobs, ...commands, ...rollouts].sort((a, b) => ts(b.time) - ts(a.time) || a.id.localeCompare(b.id));
}
function filteredTerminalTasks() {
  return terminalTaskRecords().filter(t => (!TC.taskNode || t.nodes.includes(TC.taskNode)) && (TC.taskType === "all" || t.type === TC.taskType) && (TC.taskState === "all" || t.group === TC.taskState) && hit(t.id, t.title, t.detail, t.label, ...t.nodes.map(tcNodeName), ...t.nodes));
}
function paintTerminalFilters() {
  const el = $("#tc-task-filters"); if (!el || el.contains(document.activeElement)) return;
  const nodes = [["", "全部终端"], ...S.nodes.map(n => [n.node_id, n.alias || shortId(n.node_id)])];
  if (TC.taskNode && !nodes.some(([id]) => id === TC.taskNode)) nodes.push([TC.taskNode, tcNodeName(TC.taskNode)]);
  const fields = [["node", "终端", TC.taskNode, nodes], ["type", "类型", TC.taskType, [["all", "全部类型"], ["job", "部署作业"], ["apt", "APT 服务 / 源"], ["system", "系统维护 / 诊断"], ["agent", "Agent 更新"], ["rollout", "灰度发布"]]], ["state", "状态", TC.taskState, [["all", "全部状态"], ["pending", "待处理 / 待回报"], ["active", "进行中"], ["waiting", "等待空闲"], ["done", "已完成"], ["failed", "失败"], ["attention", "挂起 / 中断 / 未知"]]]];
  el.innerHTML = fields.map(([key, label, value, options]) => `<label class="field"><span>${label}</span><select class="inp" data-change="tc-task-filter" data-key="${key}" aria-label="任务${label}">${options.map(([id, name]) => `<option value="${esc(id)}" ${id === value ? "selected" : ""}>${esc(name)}</option>`).join("")}</select></label>`).join("") + `<span class="hint">服务器持久记录 · 待回报不代表正在执行</span>`;
}
function paintTerminalTasks() {
  const el = $("#tc-task-list"), pages = $("#tc-task-pages"); if (!el || !pages) return;
  if (TC.loading) { el.innerHTML = empty("refresh", "正在读取任务记录…", "", true); pages.innerHTML = ""; return; }
  const list = filteredTerminalTasks(), count = Math.max(1, Math.ceil(list.length / 25)); TC.page = Math.min(TC.page, count);
  el.innerHTML = list.length ? `<div class="tbl-wrap"><table class="tbl tc-task-table"><thead><tr><th>任务</th><th>终端</th><th>状态</th><th>创建时间</th><th>记录</th></tr></thead><tbody>${list.slice((TC.page - 1) * 25, TC.page * 25).map(t => `<tr><td><div>${esc(t.title)}</div><div class="sub tc-task-detail">${esc(t.detail)}</div></td><td>${esc(t.nodes.map(tcNodeName).join("、") || "未分配")}</td><td>${badge(t.label, t.group === "failed" ? "err" : t.group === "done" ? "ok" : "info")}<div class="sub mono">${esc(t.state || "unknown")}</div></td><td class="nowrap">${fmtTime(t.time)}</td><td><button class="btn ghost xs" data-act="tc-task-open" data-kind="${t.kind}" data-id="${esc(t.id)}">详情 / 日志</button></td></tr>`).join("")}</tbody></table></div>` : empty("activity", TC.errors.length ? "当前没有可显示的记录；部分数据读取失败" : "没有匹配的任务记录", "", true);
  pages.innerHTML = `<span class="hint">${list.length} 条记录 · 第 ${TC.page} / ${count} 页</span><span class="spacer"></span><button class="btn ghost sm" data-act="tc-page" data-dir="-1" ${TC.page <= 1 ? "disabled" : ""}>上一页</button><button class="btn ghost sm" data-act="tc-page" data-dir="1" ${TC.page >= count ? "disabled" : ""}>下一页</button>`;
}
function terminalTaskBody(t) {
  let html = `<div class="kv">${kvRow("任务 ID", esc(t.id))}${kvRow("终端", esc(t.nodes.map(tcNodeName).join("、") || "未分配"))}${kvRow("状态", esc(`${t.label} · ${t.state || "unknown"}`))}${kvRow("创建时间", fmtTime(t.time))}</div>`;
  if (t.kind === "command") {
    const c = t.raw;
    if (c.status === "pending") html += `<div class="banner info mt12">服务器已保存命令，尚未收到完成回报；不能据此判断终端是否正在执行。请先核实记录，不要重复下发。</div>`;
    if (c.finished_at) html += `<div class="kv mt12">${kvRow("结束时间", fmtTime(c.finished_at))}</div>`;
    html += `<div class="d-sec"><span class="lbl">参数</span><pre class="log-box">${esc(JSON.stringify(c.params || {}, null, 2))}</pre></div><div class="d-sec"><span class="lbl">执行日志</span><pre class="log-box">${esc(c.log || "尚无日志回报")}</pre></div>`;
    if (c.data) html += `<div class="d-sec"><span class="lbl">结果数据</span><pre class="log-box">${esc(typeof c.data === "string" ? c.data : JSON.stringify(c.data, null, 2))}</pre></div>`;
  } else {
    html += `<div class="d-sec"><span class="lbl">逐环记录 · ${esc(t.detail)}</span>${(t.raw.rings || []).map(r => `<div class="tc-ring"><div>${esc(r.name)} · ${r.percent}% · ${esc(r.status)}</div><p class="hint">终端：${esc((r.nodes || []).map(tcNodeName).join("、") || "未分配")}</p><div class="acts">${(r.jobs || []).map(id => `<button class="btn ghost xs" data-act="job-open" data-id="${esc(id)}">作业 ${esc(shortId(id))}</button>`).join("") || '<span class="hint">尚无作业</span>'}</div></div>`).join("")}</div>`;
  }
  return html;
}
function openTerminalTask(kind, id) {
  if (kind === "job") return openJobDrawer(id);
  const t = terminalTaskRecords().find(x => x.kind === kind && x.id === id); if (!t) return;
  openDrawer(esc(t.title), esc(t.id), terminalTaskBody(t), `<button class="btn ghost" data-act="tc-tasks">返回任务记录</button>`, { ctx: { kind: "tc-task", id }, onClose: () => stopPoll("tc:task") });
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
  openDrawer(esc(tcNodeName(id)), esc(id), empty("refresh", "正在读取终端…", "", true), "", { ctx: { kind: "node", id }, onClose: () => { stopPoll("node:poll"); TC.detail = null; } });
  const context = drawerCtx;
  let busy = false;
  const refresh = async () => {
    if (busy || drawerCtx !== context) return;
    busy = true;
    try {
      const n = await api("GET", `/nodes/${enc(id)}`);
      const responses = await Promise.allSettled([api("GET", `/nodes/${enc(id)}/apps`), api("GET", `/nodes/${enc(id)}/play`), n.status === "online" && TC.admin ? api("GET", `/nodes/${enc(id)}/local/api/status`) : Promise.resolve(null)]);
      if (drawerCtx !== context) return;
      S.nodeApps[id] = responses[0].status === "fulfilled" ? (Array.isArray(responses[0].value) ? responses[0].value : responses[0].value?.apps || []) : null;
      S.nodePlay[id] = responses[1].status === "fulfilled" ? responses[1].value : null;
      S.nodeSvc[id] = responses[2].status === "fulfilled" && Array.isArray(responses[2].value?.services) ? responses[2].value.services : null;
      TC.detail = n;
      if (!$("#node-play-dev") || document.activeElement !== $("#node-play-dev")) setDrawerBody(nodeDrawerBody(n, S.nodeApps[id]));
      setDrawerTitle(esc(n.alias || shortId(id)), esc(id));
      setDrawerFoot(`<button class="btn ghost" data-act="tc-tasks" data-id="${esc(id)}">任务记录</button><div class="spacer"></div><button class="btn primary" data-act="tc-install" data-id="${esc(id)}">安装 / 更新服务</button>`);
    } catch (e) {
      if (drawerCtx === context) setDrawerBody(`<div class="banner err">终端详情读取失败：${esc(e.message)}</div><button class="btn ghost" data-act="node-open" data-id="${esc(id)}">重新读取</button>`);
    } finally { busy = false; }
  };
  await refresh();
  if (drawerCtx === context) poll("node:poll", refresh, 4000);
}
function nodeDrawerBody(n, apps) {
  const id = n.node_id;
  let html = `<nav class="tc-tabs" aria-label="终端详情">${[["overview", "概况"], ["services", "已装服务"], ["maintenance", "设备与维护"]].map(([key, name]) => `<button class="btn ghost sm ${TC.detailTab === key ? "active" : ""}" data-act="tc-detail-tab" data-tab="${key}" aria-pressed="${TC.detailTab === key}">${name}</button>`).join("")}</nav>`;
  if (n.status !== "online") html += `<div class="banner warn">终端未在线 · 以下为最近上报数据，不能确认当前安装与运行状态。最近心跳 ${ago(n.last_seen)}</div>`;
  if (TC.detailTab === "services") {
    const services = n.status === "online" && Array.isArray(S.nodeSvc[id]) ? S.nodeSvc[id].filter(s => s.installed) : null;
    html += `<p class="hint">只展示已安装服务。系统已有服务不自动接管、升级或卸载；安装与运行状态分别展示。</p>`;
    if (services) return html + (services.length ? `<div class="tbl-wrap"><table class="tbl tc-services"><thead><tr><th>服务 / 版本</th><th>来源</th><th>运行状态</th></tr></thead><tbody>${services.map(s => `<tr><td>${esc(s.name)}<div class="sub">${esc(s.version || "版本未知")} · ${esc(s.protocol || "协议未上报")}</div><div class="sub mono">${esc(s.unit || s.process || "")}</div></td><td>${tcSourceBadge(s.source)}</td><td>${s.running ? badge("运行中", "ok") : badge("未运行", "muted")}</td></tr>`).join("")}</tbody></table></div>` : empty("box", "当前未检测到已装音频服务", "", true));
    html += `<div class="banner info">实时检测不可用${TC.admin ? "" : "（需要管理员登录）"}，下方仅为服务端保存的历史上报，不代表当前状态。</div>`;
    return html + (apps?.length ? `<div class="tbl-wrap"><table class="tbl tc-services"><thead><tr><th>上报应用</th><th>版本</th><th>来源</th><th>当前状态</th></tr></thead><tbody>${apps.map(a => `<tr><td>${esc(a.app_id)}</td><td>${esc(a.version || "未知")}</td><td>${tcSourceBadge(a.source)}</td><td>未知</td></tr>`).join("")}</tbody></table></div>` : empty("box", apps === null ? "历史上报读取失败" : "暂无历史上报，安装状态未知", "", true));
  }
  if (TC.detailTab === "maintenance") {
    const p = S.nodePlay[id] || {}, { devs, cur } = nodePlayDevs(p);
    return html + `<div class="d-sec"><span class="lbl">设备与播放保护</span><div class="kv">${kvRow("DAC 仲裁", esc(n.dac_arbitration || "未知"))}${kvRow("DAC 设备", esc(n.dac_device || "未上报"))}${kvRow("DAC 占用", n.dac_holder_pid ? `PID ${Number(n.dac_holder_pid)}` : "未报告占用")}${kvRow("播放保护", n.play_protect ? badge("已开启", "ok") : badge("未开启", "warn"))}</div><label class="field mt12">ALSA 输出设备<select id="node-play-dev" class="inp mono" data-change="node-device" data-id="${esc(id)}" ${p.connected && TC.admin ? "" : "disabled"}>${nodePlayOpts(devs, cur)}</select></label><p class="hint">终端上报的输出设备；变更后下次播放生效。</p><button class="btn ghost sm" data-act="node-config" data-id="${esc(id)}">编辑配置与保护</button></div>
      <div class="d-sec"><span class="lbl">系统维护</span><div class="kv">${kvRow("自动系统更新", n.auto_os_update ? "开启" : "关闭")}${kvRow("系统更新窗口", esc(n.os_window || "未设置"))}${kvRow("Agent 版本", esc(n.agent_version || "未知"))}</div><p class="hint">系统软件包升级与终端 Agent 更新为两条独立流程。</p><div class="acts"><button class="btn ghost sm" data-act="node-os" data-id="${esc(id)}">系统软件包更新</button><button class="btn ghost sm" data-act="node-net" data-id="${esc(id)}">网络诊断</button></div></div>
      <details class="tc-advanced-details"><summary>高级操作</summary><p class="hint">原生控制台提供设备侧能力；Agent 更新策略位于终端中心高级维护。</p><div class="acts"><button class="btn ghost sm" data-act="node-console" data-id="${esc(id)}">打开原生控制台</button><button class="btn ghost sm" data-act="${n.authorized ? "node-revoke" : "node-auth"}" data-id="${esc(id)}">${n.authorized ? "撤销授权" : "授权终端"}</button></div></details>`;
  }
  return html + `<div class="kv">${kvRow("连接", statusBadge(n.status))}${kvRow("授权", authBadge(n))}${kvRow("IP", esc(n.ip || "未上报"))}${kvRow("系统 / 架构", esc([n.distro, n.arch].filter(Boolean).join(" · ") || "未上报"))}${kvRow("最近心跳", fmtTime(n.last_seen))}${kvRow("期望部署模板", n.bom_id ? esc(`${n.bom_id}@${n.bom_version}`) : "未绑定")}${kvRow("分组", esc(n.group || "未分组"))}${n.note ? kvRow("备注", esc(n.note)) : ""}</div>
    <div class="d-sec"><span class="lbl">播放概况</span><div id="tc-play-summary">${terminalPlaySummary(n, S.nodePlay[id])}</div><button class="btn ghost sm mt8" data-act="tc-player" data-id="${esc(id)}">打开该终端播放器</button></div>
    <div class="d-sec"><span class="lbl">最近上报资源</span><div class="tc-resources"><div><span>CPU</span><strong>${n.cpu == null ? "未知" : Number(n.cpu).toFixed(1) + "%"}</strong></div><div><span>内存占用</span><strong>${n.mem_mb == null ? "未知" : Number(n.mem_mb).toFixed(0) + " MB"}</strong></div><div><span>温度</span><strong>${n.temp_c ? Number(n.temp_c).toFixed(1) + " °C" : "未上报"}</strong></div></div></div>`;
}
function tcSourceBadge(source) { return source === "system" ? badge("系统已有", "info") : ["server", "managed"].includes(source) ? badge("服务端下发", "ok") : badge(source || "来源未知", "muted"); }
function terminalPlaySummary(n, p) {
  if (!p) return `<span class="hint">播放状态读取失败</span>`;
  return `<div>${p.connected ? p.title ? esc(p.title) : p.state === "paused" ? "已暂停" : "ZAT 已连接 · 未报告曲目" : "ZAT 未连接"}</div><div class="hint">${esc([p.artist, p.album].filter(Boolean).join(" · "))}</div>${n.active_proto ? `<div class="hint">终端活动协议：${esc(n.active_proto)}</div>` : ""}`;
}
function paintNodePlay(id) { const el = $("#tc-play-summary"); if (el && TC.detail?.node_id === id) el.innerHTML = terminalPlaySummary(TC.detail, S.nodePlay[id]); }
async function openTerminalAdvanced() {
  closeDrawer();
  openModal("终端高级维护", `<div id="tc-advanced"><p class="hint">此处为全局终端维护，不使用列表勾选范围。系统软件包更新在单台终端的「设备与维护」中。</p><div class="d-sec"><span class="lbl">终端 Agent 更新策略</span><div id="upd-body" data-update-scope="fleet"><div id="upd-policy"></div><div id="upd-fleet" class="mt16"></div></div></div><details class="tc-advanced-details"><summary>高级灰度发布</summary><p class="hint">灰度按后端规则分环分配终端；创建前请核对范围。逐环推进与下发结果保留在任务记录中。</p><div class="acts"><button class="btn ghost sm" data-act="tc-packages">管理部署模板与签名包</button><button class="btn primary sm" data-act="ro-create">新建灰度</button></div><div id="dl-rollouts"></div></details></div>`, `<button class="btn ghost" data-act="modal-close">关闭</button>`, { onClose: () => stopPoll("tc:advanced") });
  const root = $("#tc-advanced");
  await refreshUpdate();
  if ($("#tc-advanced") !== root) return;
  try {
    const [boms, rollouts] = await Promise.all([api("GET", "/boms?status=published"), api("GET", "/rollouts")]);
    if ($("#tc-advanced") !== root) return;
    S.pubBoms = boms || []; S.rollouts = rollouts || []; paintRollouts();
  } catch (e) { if ($("#tc-advanced") === root) $("#dl-rollouts").innerHTML = `<div class="banner err">灰度数据读取失败：${esc(e.message)}</div>`; }
  if ($("#tc-advanced") === root) poll("tc:advanced", refreshUpdateLive, 4000);
}
async function terminalRequireAdmin() {
  try { const session = await api("GET", "/settings/session"); TC.admin = session.authenticated === true; }
  catch (e) { TC.admin = false; toast("无法验证管理员会话：" + e.message, "err"); return false; }
  if (!TC.admin) { toast("请先在右上角设置中登录管理员，再执行终端管理操作。", "warn"); paintTerminalCenter(); }
  return TC.admin;
}
Object.assign(ACT, {
  "tc-goto": () => { TC.tab = "nodes"; setView("nodes"); },
  "tc-login": () => openSettings("authorization"),
  "tc-packages": () => openSettings("packages"),
  "tc-tab": ds => setTerminalTab(ds.tab),
  "tc-install": ds => openTerminalInstall(ds.id ? [ds.id] : Array.from(S.sel)),
  "tc-tasks": ds => showTerminalTasks(ds.id || ""),
  "tc-page": ds => { TC.page = Math.max(1, TC.page + Number(ds.dir)); paintTerminalTasks(); },
  "tc-task-open": ds => openTerminalTask(ds.kind, ds.id),
  "tc-advanced": () => openTerminalAdvanced(),
  "tc-detail-tab": ds => { if (TC.detail && ["overview", "services", "maintenance"].includes(ds.tab)) { TC.detailTab = ds.tab; setDrawerBody(nodeDrawerBody(TC.detail, S.nodeApps[TC.detail.node_id])); } },
  "tc-player": ds => { closeDrawer(); return openPlayer(ds.id); }
});
CHG["tc-task-filter"] = (el, ds) => { const key = { node: "taskNode", type: "taskType", state: "taskState" }[ds.key]; if (key) { TC[key] = el.value; TC.page = 1; paintTerminalTasks(); } };
for (const action of ["wz-open", "wz-next", "wz-reissue", "wz-auth", "node-console", "node-auth", "node-revoke", "bulk-auth", "bulk-revoke", "bulk-delete", "cfg-save", "cfg-delete", "cfg-auth", "cfg-revoke", "os-check", "os-upgrade", "net-run", "job-advance", "job-rb-complete", "ro-submit", "ro-advance", "ro-delete", "upd-converge", "upd-retry"]) {
  const run = ACT[action];
  ACT[action] = async (...args) => {
    if (S.view === "nodes") {
      const root = $("#terminal-center"), epoch = TC.epoch;
      if (!await terminalRequireAdmin() || !tcHere(root, epoch)) return;
    }
    return run(...args);
  };
}
for (const change of ["upd-policy", "node-device"]) {
  const run = CHG[change];
  CHG[change] = async (el, ds) => {
    if (S.view === "nodes") {
      const root = $("#terminal-center"), epoch = TC.epoch;
      const allowed = await terminalRequireAdmin();
      if (!tcHere(root, epoch) || !el.isConnected) return;
      if (!allowed) {
        if (change === "upd-policy") paintUpdatePolicy(true);
        else el.value = nodePlayDevs(S.nodePlay[ds.id] || {}).cur;
        return;
      }
    }
    return run(el, ds);
  };
}
