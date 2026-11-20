"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const assets = path.join(__dirname, "assets");
function deferred() { let resolve; const promise = new Promise(r => { resolve = r; }); return { promise, resolve }; }
function harness() {
  const elements = new Map();
  const element = () => ({ innerHTML: "", textContent: "", dataset: {}, contains: () => false, classList: { toggle() {}, add() {}, remove() {} }, setAttribute() {}, querySelectorAll: () => [] });
  for (const id of ["terminal-center", "tc-access", "tc-errors", "tc-workspace", "nd-chips", "nd-bulk", "nd-table", "tc-seats", "tc-task-filters", "tc-task-list", "tc-task-pages", "drawer-root", "modal-root"]) elements.set("#" + id, element());
  const h = { elements, calls: [], nodes: [], jobs: [], rollouts: [], commands: {}, apps: {}, statuses: {}, authenticated: false, actions: [] };
  const context = {
    console, URL, URLSearchParams, AbortController, setTimeout: () => 1, clearTimeout() {}, setInterval: () => 1, clearInterval() {},
    document: { hidden: false, activeElement: null, addEventListener() {}, querySelector: s => elements.get(s) || null, querySelectorAll: () => [] },
    navigator: {}, location: { protocol: "http:", host: "localhost" },
    __api: async (method, route, body) => {
      h.calls.push({ method, route, body });
      if (h.override) { const value = await h.override(method, route, body); if (value !== undefined) return value; }
      assert.equal(method, "GET");
      if (route === "/nodes") return h.nodes;
      if (route === "/jobs") return h.jobs;
      if (route === "/rollouts") return h.rollouts;
      if (route === "/license") return { effective_seats: 3, builtin_seats: 3 };
      if (route === "/settings/session") return { authenticated: h.authenticated };
      if (route === "/update/fleet") return { nodes: [] };
      const match = route.match(/^\/nodes\/([^/]+)(?:\/(.+))?$/);
      if (match) {
        const id = decodeURIComponent(match[1]);
        if (!match[2]) return h.nodes.find(n => n.node_id === id);
        if (match[2] === "commands") return h.commands[id] || [];
        if (match[2] === "apps") return h.apps[id] || [];
        if (match[2] === "local/api/status") return h.statuses[id];
        if (match[2] === "play") return { connected: false };
      }
      throw new Error("Unexpected route: " + route);
    },
    __toast: (...args) => h.actions.push(args),
    __drawer: (title, sub, body, foot, opts) => { h.drawer = { title, body, foot, opts }; },
    __player: id => h.actions.push(["player", id]),
    __view: view => h.actions.push(["view", view])
  };
  context.window = context;
  vm.createContext(context);
  for (const file of ["i18n.js", "i18n-app.js", "i18n-features.js", "i18n-errors.js", "app.js"]) vm.runInContext(fs.readFileSync(path.join(assets, file), "utf8"), context);
  vm.runInContext(fs.readFileSync(path.join(assets, "terminal-center.js"), "utf8"), context);
  vm.runInContext(`req = (method, route, opts) => __api(method, route, opts?.body ? JSON.parse(opts.body) : undefined); toast = __toast; S.view = 'nodes'; openDrawer = (title, sub, body, foot, opts) => { drawerCtx = opts.ctx; __drawer(title, sub, body, foot, opts); }; setDrawerBody = html => __drawer('', '', html, '', {}); openPlayer = __player;`, context);
  h.run = source => vm.runInContext(source, context);
  h.context = context;
  h.node = (id, status = "offline") => ({ node_id: id, alias: "终端 " + id, status, arch: "arm64", distro: "debian", playing: false, authorized: true });
  return h;
}

test("task filter paints respect focus by default but force translated labels without changing filters", () => {
  const h = harness(), filters = h.elements.get("#tc-task-filters"), focused = { value: "removed" };
  h.context.document.activeElement = focused;
  filters.contains = el => el === focused;
  filters.innerHTML = "focused draft";
  h.run(`S.nodes=[{node_id:'a',alias:'Alias <kept>'}]; S.search='keep search'; S.sel.add('a');
    TC.taskNode='removed'; TC.taskType='agent'; TC.taskState='pending'; TC.page=3;`);
  const before = h.run("JSON.stringify([S,TC])");
  for (const [locale, nodeLabel, typeLabel, stateLabel] of [
    ["en", "Terminal", "Type", "Status"], ["zh-TW", "終端", "類型", "狀態"], ["zh-CN", "终端", "类型", "状态"]
  ]) {
    const html = filters.innerHTML;
    h.run(`I18n.setPreference(${JSON.stringify(locale)}); paintTerminalFilters(); paintTerminalFilters(false)`);
    assert.equal(filters.innerHTML, html, "ordinary refresh must not replace focused filters");
    h.run("paintTerminalFilters(true)");
    for (const label of [nodeLabel, typeLabel, stateLabel]) assert.ok(filters.innerHTML.includes(`<span>${label}</span>`));
    assert.ok(filters.innerHTML.includes(h.run("tcT('tcTaskFilter',{field:tcT('status')})")));
    assert.ok(filters.innerHTML.includes(h.run("tcT('tcPersistentNote')")));
    assert.match(filters.innerHTML, /<option value="removed" selected>removed<\/option>/);
    assert.match(filters.innerHTML, /<option value="agent" selected>/);
    assert.match(filters.innerHTML, /<option value="pending" selected>/);
    assert.match(filters.innerHTML, /Alias &lt;kept&gt;/);
    assert.equal((filters.innerHTML.match(/ selected>/g) || []).length, 3);
    assert.equal(h.run("JSON.stringify([S,TC])"), before);
    assert.equal(h.run("S.sel.has('a')"), true);
    assert.equal(h.context.document.activeElement, focused);
    assert.deepEqual(h.calls, []); assert.deepEqual(h.actions, []);
  }
  h.context.document.activeElement = null; filters.innerHTML = "unfocused stale labels";
  h.run("paintTerminalFilters()");
  assert.match(filters.innerHTML, /任务状态/);
  h.elements.delete("#tc-task-filters");
  assert.doesNotThrow(() => h.run("paintTerminalFilters(); paintTerminalFilters(true)"));
});

test("terminal language changes retain the workspace and repaint cached tabs without requests or action replay", async t => {
  for (const tab of ["nodes", "tasks"]) await t.test(tab, () => {
    const h = harness(), root = h.elements.get("#terminal-center"), workspace = h.elements.get("#tc-workspace");
    h.run(`TC.tab=${JSON.stringify(tab)}; TC.loading=false; TC.epoch=9; TC.busy={pending:true};
      TC.taskNode='a'; TC.taskType='agent'; TC.taskState='pending'; TC.page=2;
      TC.detail={node_id:'a'}; TC.detailTab='services'; TC.errors=[{key:'tcSessionUnavailable'}];
      S.nodes=[{node_id:'a',alias:'Keep alias',status:'offline',authorized:true}];
      S.sel.add('a'); S.search='Keep'; S.nodeFilter='offline'; S.nodeSort={key:'alias',dir:-1};
      S.play={node_id:'a',position:42}; S.nodeQueue.a=[{id:'queued'}];
      TC.commands=Array.from({length:31},(_,i)=>({id:'c'+i,node_id:'a',type:'agent-upgrade',status:'pending',created_at:i+1}));
      window.filterPaints=[]; const filtersForLanguageTest=paintTerminalFilters;
      paintTerminalFilters=(force)=>{ filterPaints.push(force); return filtersForLanguageTest(force); };`);
    const parts = new Map();
    const part = (selector, innerHTML) => ({ innerHTML, replaceWith(next) { assert.ok(next); parts.set(selector, part(selector, next.innerHTML)); } });
    for (const selector of [".tc-heading", ".tc-tabs"]) parts.set(selector, part(selector, "old labels"));
    const add = { innerHTML: "old add label" };
    root.querySelector = selector => selector === "[data-act='wz-open']" ? tab === "nodes" ? add : null : parts.get(selector) || null;
    // Only extract the two replaceable shell fragments, not a simulated browser DOM.
    h.context.document.createElement = tag => {
      assert.equal(tag, "div");
      return { innerHTML: "", querySelector(selector) {
        const html = selector === ".tc-heading" ? this.innerHTML.match(/<div class="tc-heading">([\s\S]*?)<nav class="tc-tabs"/)[1]
          : this.innerHTML.match(/<nav class="tc-tabs"[\s\S]*?<\/nav>/)[0];
        return { innerHTML: html };
      } };
    };
    for (const node of [root, workspace]) Object.defineProperty(node, "innerHTML", { get: () => "retained workspace", set() { assert.fail("translation must not replace terminal roots"); } });
    if (tab === "nodes") h.elements.delete("#tc-task-filters");
    else {
      const focused = { value: "a" }; h.context.document.activeElement = focused;
      h.elements.get("#tc-task-filters").contains = el => el === focused;
    }
    const state = h.run("JSON.stringify([S,TC])"), busy = h.run("TC.busy"), commands = h.run("TC.commands");
    for (const [locale, title, addLabel, taskLabel] of [
      ["en", "Terminal center", "Add terminal", "Task history"],
      ["zh-TW", "終端中心", "新增終端", "工作紀錄"],
      ["zh-CN", "终端中心", "添加终端", "任务记录"]
    ]) {
      h.run(`filterPaints.length=0; I18n.setPreference(${JSON.stringify(locale)}); terminalCenterLanguageChanged()`);
      assert.ok(parts.get(".tc-heading").innerHTML.includes(title));
      assert.ok(parts.get(".tc-tabs").innerHTML.includes(taskLabel));
      assert.ok(parts.get(".tc-tabs").innerHTML.includes(`data-tab="${tab}" aria-pressed="true"`));
      assert.equal(h.elements.get("#terminal-center"), root); assert.equal(h.elements.get("#tc-workspace"), workspace);
      assert.deepEqual(Array.from(h.run("filterPaints")), tab === "tasks" ? [true, undefined] : [true]);
      if (tab === "nodes") {
        assert.ok(add.innerHTML.includes(addLabel));
        assert.match(h.elements.get("#nd-table").innerHTML, /Keep alias/);
        assert.match(h.elements.get("#nd-chips").innerHTML, /data-f="offline" aria-pressed="true"/);
      } else {
        assert.ok(h.elements.get("#tc-task-filters").innerHTML.includes(h.run("tcT('tcAllNodes')")), "forced paint updates even focused filters");
        assert.ok(h.elements.get("#tc-task-list").innerHTML.includes(h.run("tcT('tcAwaitingReport')")));
        assert.equal((h.elements.get("#tc-task-list").innerHTML.match(/data-act="tc-task-open"/g) || []).length, 6);
      }
      assert.ok(h.elements.get("#tc-access").innerHTML.includes(h.run("tcT('tcReadOnly')")));
      assert.ok(h.elements.get("#tc-errors").innerHTML.includes(h.run("tcT('tcSessionUnavailable')")));
      assert.equal(h.run("JSON.stringify([S,TC])"), state);
      assert.equal(h.run("TC.busy"), busy); assert.equal(h.run("TC.commands"), commands);
      assert.equal(h.run("S.sel.has('a')"), true);
      assert.deepEqual(h.calls, []); assert.deepEqual(h.actions, []);
    }
    h.elements.delete("#terminal-center"); h.context.document.createElement = () => assert.fail("missing root must be a no-op");
    assert.doesNotThrow(() => h.run("terminalCenterLanguageChanged()"));
  });
});

test("navigation contains one terminal entry, with no old deployment selectors", () => {
  const h = harness();
  assert.equal(h.run("NAV.filter(x => x.id === 'nodes' && x.name === '终端中心').length"), 1);
  assert.equal(h.run("NAV.some(x => x.id === 'deliver')"), false);
  const shell = h.run("shellNodes()");
  assert.match(shell, /终端中心工作区/);
  assert.doesNotMatch(shell, /dl-bom|dl-nodes|nd-stats/);
  assert.equal(h.run("'jobSel' in S || 'aptCmds' in S"), false);
});

test("persisted commands survive a fresh UI context and do not need online terminals", async () => {
  for (let i = 0; i < 2; i++) {
    const h = harness(); h.nodes = [h.node("a")];
    h.commands.a = [{ id: "c1", node_id: "a", type: "apt-install", status: "done", result_status: "ok", created_at: 10, params: { packages: ["mpd"] } }];
    await h.run("refreshTerminalCenter()");
    assert.equal(h.run("terminalTaskRecords().length"), 1);
    assert.equal(h.run("terminalTaskRecords()[0].group"), "done");
    assert.ok(h.calls.some(c => c.route === "/nodes/a/commands"));
    assert.ok(h.calls.every(c => c.method === "GET"));
    assert.ok(!h.calls.some(c => c.route.endsWith("/local/api/status")));
  }
});

test("task normalization retains distinct backend types, unknown results and raw states", () => {
  const h = harness();
  h.run(`S.jobs = [{id:'j', node_id:'a', bom_id:'audio', state:'waiting_idle', phases:[], created_at:1}];
    S.rollouts = [{id:'r', name:'灰度', status:'running', created_at:2, rings:[{nodes:['a'], jobs:['j']}]}];
    TC.commands = [{id:'c', node_id:'a', type:'agent-upgrade', status:'done', created_at:3}, {id:'p', node_id:'a', type:'apt-source', status:'pending', created_at:4}];`);
  const records = JSON.parse(h.run("JSON.stringify(terminalTaskRecords())"));
  assert.deepEqual(records.map(r => r.kind), ["command", "command", "rollout", "job"]);
  assert.equal(records[0].label, "待回报");
  assert.equal(records[1].group, "attention");
  assert.equal(records[1].label, "结果未知");
  assert.equal(records[3].group, "waiting");
  assert.equal(records[3].state, "waiting_idle");
  assert.doesNotMatch(records[3].title, /undefined/);
});

test("terminal, type, status and search task filters combine", () => {
  const h = harness();
  h.run(`S.nodes = [{node_id:'a',alias:'客厅'}, {node_id:'b',alias:'书房'}]; TC.commands = [
    {id:'a1',node_id:'a',type:'apt-install',status:'done',result_status:'err',created_at:1,params:{packages:['mpd']}},
    {id:'b1',node_id:'b',type:'apt-install',status:'done',result_status:'ok',created_at:2},
    {id:'a2',node_id:'a',type:'agent-upgrade',status:'pending',created_at:3}];
    TC.taskNode='a'; TC.taskType='apt'; TC.taskState='failed'; S.search='客厅';`);
  assert.equal(h.run("filteredTerminalTasks()[0].id"), "a1");
  h.run("S.search='no match'");
  assert.equal(h.run("filteredTerminalTasks().length"), 0);
});

test("selection remains explicit across filters and hidden selections are disclosed", () => {
  const h = harness();
  h.run(`S.nodes = [{node_id:'a', alias:'A', status:'online'}, {node_id:'b', alias:'B', status:'offline'}]; S.sel.add('b'); ACT['nd-filter']({f:'online'});`);
  assert.match(h.elements.get("#nd-bulk").innerHTML, /1 台不在当前筛选中/);
  h.run("CHG['node-sel-all']({checked:true})");
  assert.equal(h.run("S.sel.size"), 2);
  h.run("CHG['node-sel-all']({checked:false})");
  assert.equal(h.run("S.sel.has('b') && !S.sel.has('a')"), true);
});

test("refresh prunes removed selections without selecting newly discovered terminals", async () => {
  const h = harness(); h.nodes = [h.node("a"), h.node("b")];
  h.run("S.sel.add('a'); S.sel.add('deleted')");
  await h.run("refreshTerminalCenter()");
  assert.equal(h.run("S.sel.size"), 1);
  assert.equal(h.run("S.sel.has('a')"), true);
});

test("late center response cannot overwrite a different view or a new center instance", async () => {
  const h = harness(), pending = deferred();
  h.nodes = [h.node("a")];
  h.override = (_, route) => route === "/nodes" ? pending.promise : undefined;
  const work = h.run("refreshTerminalCenter()");
  h.run("cleanupTerminalCenter(); S.view='library'; S.nodes=[{node_id:'keep'}]");
  pending.resolve(h.nodes); await work;
  assert.equal(h.run("S.nodes[0].node_id"), "keep");
  assert.equal(h.run("TC.busy"), null);
});

test("partial command read failures retain prior records and surface staleness", async () => {
  const h = harness(); h.nodes = [h.node("a")];
  h.run("TC.commands=[{id:'old',node_id:'a',type:'netdiag',status:'pending',created_at:1}]");
  h.override = (_, route) => { if (route === "/nodes/a/commands") throw new Error("temporarily offline"); };
  await h.run("refreshTerminalCenter()");
  assert.equal(h.run("TC.commands[0].id"), "old");
  assert.match(h.elements.get("#tc-errors").innerHTML, /命令记录刷新失败/);
});

test("unavailable service status is unknown, never uninstalled or current enabled state", () => {
  const h = harness();
  h.run("TC.detailTab='services'; S.nodeSvc.a=null; S.nodeApps.a=[]");
  const html = h.run("nodeDrawerBody({node_id:'a',status:'offline'}, [])");
  assert.match(html, /安装状态未知/);
  assert.doesNotMatch(html, /当前未检测到已装音频服务/);
  const summary = h.run("tcServiceSummary({node_id:'a',status:'offline'})");
  assert.match(summary, /安装状态未知/);
});

test("services drawer shows only installed services and separates source and running state", () => {
  const h = harness();
  h.run(`TC.detailTab='services'; S.nodeSvc.a=[{name:'系统 MPD',installed:true,running:false,source:'system'},{name:'ZAT',installed:true,running:true,source:'server'},{name:'未安装项目',installed:false}];`);
  const html = h.run("nodeDrawerBody({node_id:'a',status:'online'}, [])");
  assert.match(html, /系统已有/); assert.match(html, /服务端下发/); assert.match(html, /未运行/);
  assert.doesNotMatch(html, /未安装项目/);
});

test("terminal detail reads playback summary but never a duplicate queue", async () => {
  const h = harness(); h.nodes = [h.node("a")];
  await h.run("openNodeDrawer('a')");
  assert.ok(h.calls.some(c => c.route === "/nodes/a/play"));
  assert.ok(h.calls.every(c => !c.route.endsWith("/queue")));
  const html = h.run("nodeDrawerBody(TC.detail, [])");
  assert.match(html, /打开该终端播放器/); assert.doesNotMatch(html, /播放队列|node-q-prev/);
});

test("late terminal detail response does not reopen or overwrite another drawer", async () => {
  const h = harness(), pending = deferred(); h.nodes = [h.node("a")];
  h.override = (_, route) => route === "/nodes/a" ? pending.promise : undefined;
  const work = h.run("openNodeDrawer('a')");
  h.run("drawerCtx={kind:'different'}"); const before = h.drawer;
  pending.resolve(h.nodes[0]); await work;
  assert.equal(h.drawer, before);
});

test("opening player closes terminal detail in place and passes the explicit terminal to openPlayer", () => {
  const h = harness(), root = h.elements.get("#drawer-root");
  root.innerHTML = "terminal detail"; root.firstElementChild = {};
  h.run("setView=__view; S.search='keep filter'; S.sel.add('selected'); drawerCtx={kind:'node',id:'explicit'}; drawerOnClose=()=>__toast('drawer-closed'); ACT['tc-player']({id:'explicit'})");
  assert.equal(h.run("S.view"), "nodes");
  assert.equal(h.run("S.search"), "keep filter"); assert.equal(h.run("S.sel.has('selected')"), true);
  assert.equal(h.run("drawerCtx"), null); assert.equal(root.innerHTML, "");
  assert.deepEqual(h.actions, [["drawer-closed"], ["player", "explicit"]]);
});

test("management actions recheck authentication and never issue writes when logged out", async () => {
  const h = harness();
  await h.run("ACT['node-auth']({id:'a'})");
  assert.ok(h.calls.every(c => c.method === "GET"));
  assert.match(h.actions[0][0], /登录管理员/);
});

test("maintenance changes stop after logout or detaching the original control", async () => {
  for (const change of ["upd-policy", "node-device"]) {
    const h = harness();
    await h.run(`CHG['${change}']({isConnected:true}, {id:'a'})`);
    assert.ok(h.calls.every(c => c.method === "GET"));
    h.authenticated = true;
    await h.run(`CHG['${change}']({isConnected:false}, {id:'a'})`);
    assert.ok(h.calls.every(c => c.method === "GET"));
  }
});

test("maintenance changes stop after navigation during the session check", async () => {
  const h = harness(), pending = deferred();
  h.override = (_, route) => route === "/settings/session" ? pending.promise : undefined;
  const work = h.run("CHG['upd-policy']({isConnected:true}, {})");
  h.run("cleanupTerminalCenter(); S.view='library'");
  pending.resolve({authenticated:true}); await work;
  assert.ok(h.calls.every(c => c.method === "GET"));
});

test("task detail escapes terminal logs and preserves real pending semantics", () => {
  const h = harness();
  h.run(`TC.commands=[{id:'c',node_id:'a',type:'apt-install',status:'pending',created_at:1,log:'<script>alert(1)</script>',params:{packages:['mpd']}}]`);
  const html = h.run("terminalTaskBody(terminalTaskRecords()[0])");
  assert.match(html, /&lt;script&gt;/); assert.doesNotMatch(html, /<script>/);
  assert.match(html, /不能据此判断终端是否正在执行/);
});

test("pagination shows all matching stored task records without dropping older ones", () => {
  const h = harness();
  h.run("TC.loading=false; TC.commands=Array.from({length:31},(_,i)=>({id:'c'+i,node_id:'a',type:'netdiag',status:'pending',created_at:i})); paintTerminalTasks()");
  assert.equal((h.elements.get("#tc-task-list").innerHTML.match(/data-act="tc-task-open"/g) || []).length, 25);
  h.run("TC.page=2; paintTerminalTasks()");
  assert.equal((h.elements.get("#tc-task-list").innerHTML.match(/data-act="tc-task-open"/g) || []).length, 6);
  assert.match(h.elements.get("#tc-task-pages").innerHTML, /31 条记录/);
});

test("node table surfaces target version, build flavor and direct upgrade", () => {
  const h = harness();
  h.run(`S.nodes = [
    { node_id:'a', alias:'Pi', status:'online', arch:'arm64', distro:'debian', playing:false, authorized:true, agent_version:'0.4.3', build_flavor:'audio' },
    { node_id:'b', alias:'Idle', status:'online', arch:'amd64', distro:'debian', playing:false, authorized:true, agent_version:'0.57', build_flavor:'management' },
    { node_id:'c', alias:'Playing', status:'online', arch:'arm64', distro:'debian', playing:true, play_protect:true, authorized:true, agent_version:'0.4.3', build_flavor:'audio' }
  ];
  S.upd.fleet = { target:'0.57', nodes:[
    { node_id:'a', current:'0.4.3', target:'0.57', state:'pending' },
    { node_id:'b', current:'0.57', target:'0.57', state:'converged' },
    { node_id:'c', current:'0.4.3', target:'0.57', state:'waiting_idle' }
  ] };
  TC.admin = true; TC.loading = false; paintNodeTable();`);
  const html = h.elements.get("#nd-table").innerHTML;
  assert.match(html, /0\.4\.3/);
  assert.match(html, /→ 0\.57/);
  assert.match(html, /音频版（ALSA 播放）/);
  assert.match(html, /管理版（不含 ALSA 播放）/);
  assert.match(html, /data-act="node-upgrade" data-id="a"/);
  assert.match(html, /data-act="node-upgrade" data-id="c" disabled/);
  h.run("I18n.setPreference('en'); paintNodeTable();");
  const en = h.elements.get("#nd-table").innerHTML;
  assert.match(en, /Audio build \(ALSA playback\)/);
  assert.match(en, /Upgrade Agent/);
  assert.match(en, /0\.4\.3/);
});

test("server device selector shows ALSA card names with hardware ids", () => {
  const h = harness();
  const html = h.run(`nodePlayOpts(["default","hw:3,0"], "hw:3,0", new Map([["hw:3,0","AUDIO"]]))`);
  assert.match(html, /AUDIO · hw:3,0/);
  assert.match(html, /value="hw:3,0" selected/);
  const dvHtml = h.run(`(() => { const dv = nodePlayDevs({devices:["hw:0,0"], device_info:[{hw:"hw:0,0", name:"b1"}], pref_device:"hw:0,0"}); return nodePlayOpts(dv.devs, dv.cur, dv.names); })()`);
  assert.match(dvHtml, /b1 · hw:0,0/);
});
