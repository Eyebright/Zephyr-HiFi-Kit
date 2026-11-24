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
  const h = { elements, calls: [], nodes: [], jobs: [], rollouts: [], commands: {}, apps: {}, devices: {}, statuses: {}, authenticated: false, actions: [], playerRefreshes: 0 };
  const context = {
    console, URL, URLSearchParams, AbortController, setTimeout: () => 1, clearTimeout() {}, setInterval: () => 1, clearInterval() {},
    document: { hidden: false, activeElement: null, addEventListener() {}, getElementById: () => null, querySelector: s => elements.get(s) || null, querySelectorAll: () => [] },
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
      const [pathname, query = ""] = route.split("?");
      const match = pathname.match(/^\/nodes\/([^/]+)(?:\/(.+))?$/);
      if (match) {
        const id = decodeURIComponent(match[1]);
        if (!match[2]) return h.nodes.find(n => n.node_id === id);
        if (match[2] === "commands") return h.commands[id] || [];
        if (match[2] === "apps") return h.apps[id] || [];
        if (match[2] === "devices") return { devices: h.devices[id] || [] };
        if (match[2] === "local/api/status") return h.statuses[id];
        if (match[2] === "play") return { connected: false, device_key: new URLSearchParams(query).get("device_key") };
      }
      throw new Error("Unexpected route: " + route);
    },
    __toast: (...args) => h.actions.push(args),
    __drawer: (title, sub, body, foot, opts) => { h.drawer = { title, body, foot, opts }; },
    __player: id => h.actions.push(["player", id]),
    __refreshPlayer: () => { h.playerRefreshes++; },
    __view: view => h.actions.push(["view", view])
  };
  context.window = context;
  vm.createContext(context);
  for (const file of ["i18n.js", "i18n-app.js", "i18n-features.js", "i18n-errors.js", "app.js"]) vm.runInContext(fs.readFileSync(path.join(assets, file), "utf8"), context);
  vm.runInContext(fs.readFileSync(path.join(assets, "terminal-center.js"), "utf8"), context);
  vm.runInContext(`req = (method, route, opts) => __api(method, route, opts?.body ? JSON.parse(opts.body) : undefined); toast = __toast; S.view = 'nodes'; openDrawer = (title, sub, body, foot, opts) => { drawerCtx = opts.ctx; __drawer(title, sub, body, foot, opts); }; setDrawerBody = html => __drawer('', '', html, '', {}); openPlayer = __player; refreshPlayerBar = async () => { __refreshPlayer(); };`, context);
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
    for (const selector of [".page-head", ".tc-tabs"]) parts.set(selector, part(selector, "old labels"));
    const add = { innerHTML: "old add label" };
    root.querySelector = selector => selector === "[data-act='wz-open']" ? tab === "nodes" ? add : null : parts.get(selector) || null;
    // Only extract the two replaceable shell fragments, not a simulated browser DOM.
    h.context.document.createElement = tag => {
      assert.equal(tag, "div");
      return { innerHTML: "", querySelector(selector) {
        const html = selector === ".page-head" ? this.innerHTML.match(/<div class="page-head">([\s\S]*?)<nav class="tc-tabs"/)[1]
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
      ["en", "Audio devices", "Add terminal", "Task history"],
      ["zh-TW", "音訊裝置", "新增終端", "工作紀錄"],
      ["zh-CN", "音频设备", "添加终端", "任务记录"]
    ]) {
      h.run(`filterPaints.length=0; I18n.setPreference(${JSON.stringify(locale)}); terminalCenterLanguageChanged()`);
      assert.ok(parts.get(".page-head").innerHTML.includes(title));
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

test("audio devices live in settings, with no standalone terminal nav or old deployment selectors", () => {
  const h = harness();
  assert.equal(h.run("NAV.some(x => x.id === 'nodes')"), false);
  assert.equal(h.run("NAV.some(x => x.id === 'deliver')"), false);
  const shell = h.run("shellNodes()");
  assert.match(shell, /音频设备工作区/);
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

test("settings audio devices load even after scroll-spy changes section before refresh starts", async () => {
  const h = harness(); h.nodes = [h.node("a")];
  h.run("SETTINGS={section:'folders'}; S.view='settings'");
  await h.run("refreshTerminalCenter()");
  assert.equal(h.run("S.nodes[0].node_id"), "a");
  assert.equal(h.run("TC.loading"), false);
  assert.match(h.elements.get("#nd-table").innerHTML, /终端 a/);
});

test("late center response survives settings scroll-spy moving to another section", async () => {
  const h = harness(), pending = deferred();
  h.nodes = [h.node("a")];
  h.run("SETTINGS={section:'devices'}; S.view='settings'");
  h.override = (_, route) => route === "/nodes" ? pending.promise : undefined;
  const work = h.run("refreshTerminalCenter()");
  h.run("SETTINGS.section='folders'");
  pending.resolve(h.nodes); await work;
  assert.equal(h.run("S.nodes[0].node_id"), "a");
  assert.equal(h.run("TC.loading"), false);
  assert.match(h.elements.get("#nd-table").innerHTML, /终端 a/);
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

test("maintenance drawer no longer offers raw ALSA or DAC pickers", () => {
  const h = harness();
  h.run(`TC.detailTab='maintenance'; S.nodePlay.a={connected:true, devices:["hw:2,0"], device_info:[{hw:"hw:2,0", name:"Headphones"}], pref_device:"hw:2,0"}`);
  const html = h.run("nodeDrawerBody({node_id:'a',status:'online',dac_device:'hw:2,0'}, [])");
  assert.doesNotMatch(html, /node-play-dev|cfg-dac/);
  assert.match(html, /播放设备的激活与命名/);
});

test("terminal rows render playback devices as a headerless table with activate and naming", () => {
  const h = harness();
  h.run(`TC.admin=true; S.nodeDevs.a=[{hw:"hw:3,0",name:"客厅 DAC",reported_name:"AUDIO",enabled:true,present:true,online:true},{hw:"hw:2,0",name:"",reported_name:"Headphones",enabled:false,present:false,online:false}]`);
  const html = h.run("nodeRow({node_id:'a',alias:'moode',status:'online'})");
  assert.match(html, /tc-dev-row/);
  assert.match(html, /<table class="tbl tc-device-table"><tbody>/);
  assert.doesNotMatch(html, /<thead>/);
  assert.doesNotMatch(html, /data-act="tc-device-player"/);
  assert.equal((html.match(/<tr class="tc-device-row/g) || []).length, 2);
  assert.match(html, /客厅 DAC/);
  assert.match(html, /data-change="tc-dev-act" data-id="a" data-hw="hw:3,0" checked/);
  assert.match(html, /a::hw:3,0/);
  assert.match(html, /a::hw:2,0/);
  assert.match(html, /Headphones/);
  assert.match(html, /未上报/);
  assert.match(html, /data-act="tc-dev-rename" data-id="a" data-hw="hw:2,0"/);
  assert.match(html, /自定义名称/);
  assert.match(html, /data-act="node-delete" data-id="a"/);
  h.run("TC.devEdit={id:'a',hw:'hw:2,0',value:'x'}");
  const editing = h.run("nodeRow({node_id:'a',alias:'moode',status:'online'})");
  assert.match(editing, /tc-dev-input/);
  assert.match(editing, /data-id="a" data-hw="hw:2,0"/);
  assert.match(editing, /data-act="tc-dev-rename-save"/);
});

test("device rename draft survives periodic table repaint with focus and caret", () => {
  const h = harness(), table = h.elements.get("#nd-table");
  h.run(`TC.admin=true; TC.loading=false; S.nodes=[{node_id:'a',alias:'moode',status:'online',authorized:true}];
    S.nodeDevs.a=[{hw:'hw:1,0',name:'旧名称',reported_name:'AUDIO',enabled:true,present:true,online:true}];
    TC.devEdit={id:'a',hw:'hw:1,0',value:''};`);
  const active = {
    classList: { contains: cls => cls === "tc-dev-input" },
    dataset: { id: "a", hw: "hw:1,0" }, value: "新名称", selectionStart: 2, selectionEnd: 2, selectionDirection: "none"
  };
  h.context.document.activeElement = active;
  const restored = {
    dataset: { id: "a", hw: "hw:1,0" }, value: "",
    focus(opts) { h.focus = opts; },
    setSelectionRange(start, end, direction) { h.selection = [start, end, direction]; }
  };
  table.querySelectorAll = selector => selector === ".tc-dev-input" ? [restored] : [];
  h.run("paintNodeTable()");
  assert.equal(h.run("TC.devEdit.value"), "新名称");
  assert.equal(restored.value, "新名称");
  assert.equal(h.focus.preventScroll, true);
  assert.deepEqual(Array.from(h.selection), [2, 2, "none"]);
  assert.match(table.innerHTML, /新名称/);
});

test("typing in the device rename input updates the draft before repaint", () => {
  const h = harness();
  h.run("TC.devEdit={id:'a',hw:'hw:1,0',value:''}");
  h.run("onInput({target:{classList:{contains:cls=>cls==='tc-dev-input'},value:'实时草稿'}})");
  assert.equal(h.run("TC.devEdit.value"), "实时草稿");
});

test("terminal detail reads each playable device summary but never a duplicate queue", async () => {
  const h = harness(); h.nodes = [h.node("a", "online")];
  h.devices.a = [
    { hw: "hw:1,0", name: "Living DAC", enabled: true, present: true, online: true },
    { hw: "hw:2,0", name: "Headphones", enabled: true, present: true, online: true },
    { hw: "hw:3,0", name: "Absent DAC", enabled: true, present: false, online: true }
  ];
  await h.run("openNodeDrawer('a')");
  const reads = h.calls.filter(c => c.route.startsWith("/nodes/a/play?"));
  assert.deepEqual(reads.map(c => new URLSearchParams(c.route.split("?")[1]).get("device_key")), ["a::hw:1,0", "a::hw:2,0"]);
  assert.ok(h.calls.every(c => !c.route.endsWith("/queue")));
  const html = h.run("nodeDrawerBody(TC.detail, [])");
  assert.match(html, /a::hw:1,0/); assert.match(html, /a::hw:2,0/); assert.match(html, /a::hw:3,0/);
  assert.doesNotMatch(html, /播放队列|node-q-prev/);
});

test("late terminal detail response does not reopen or overwrite another drawer", async () => {
  const h = harness(), pending = deferred(); h.nodes = [h.node("a")];
  h.override = (_, route) => route === "/nodes/a" ? pending.promise : undefined;
  const work = h.run("openNodeDrawer('a')");
  h.run("drawerCtx={kind:'different'}"); const before = h.drawer;
  pending.resolve(h.nodes[0]); await work;
  assert.equal(h.drawer, before);
});

test("opening a device player closes terminal detail and passes the explicit device key", () => {
  const h = harness(), root = h.elements.get("#drawer-root");
  root.innerHTML = "terminal detail"; root.firstElementChild = {};
  h.run(`PB.devices=[{node_id:'explicit',device_key:'explicit::hw:3,0',hw:'hw:3,0',name:'Living DAC',enabled:true,present:true,online:true}];
    setView=__view; S.search='keep filter'; S.sel.add('selected'); drawerCtx={kind:'node',id:'explicit'}; drawerOnClose=()=>__toast('drawer-closed');
    ACT['tc-device-player']({id:'explicit',deviceKey:'explicit::hw:3,0'})`);
  assert.equal(h.run("S.view"), "nodes");
  assert.equal(h.run("S.search"), "keep filter"); assert.equal(h.run("S.sel.has('selected')"), true);
  assert.equal(h.run("drawerCtx"), null); assert.equal(root.innerHTML, "");
  assert.deepEqual(h.actions, [["drawer-closed"], ["player", "explicit::hw:3,0"]]);
});

test("terminal device player rejects another terminal's key and unavailable devices", async () => {
  const h = harness();
  h.run(`PB.devices=[{node_id:'a',device_key:'a::hw:1,0',hw:'hw:1,0',enabled:true,present:true,online:true}];`);
  await h.run("tcOpenTerminalPlayer('b','a::hw:1,0')");
  await h.run("tcOpenTerminalPlayer('a','a::hw:9,0')");
  assert.equal(h.actions.some(a => a[0] === "player"), false);
  assert.equal(h.playerRefreshes, 1);
});

test("management actions recheck authentication and never issue writes when logged out", async () => {
  const h = harness();
  await h.run("ACT['node-auth']({id:'a'})");
  assert.ok(h.calls.every(c => c.method === "GET"));
  assert.match(h.actions[0][0], /登录管理员/);
});

test("terminal deletion rechecks admin, confirms the endpoint and honors cancellation", async () => {
  const h = harness();
  h.run(`S.nodes=[{node_id:'a',alias:'客厅终端',status:'online',authorized:true}];
    window.confirmCalls=[]; window.deleteCalls=[];
    confirmDlg=async opts => { window.confirmCalls.push(opts); return false; };
    deleteNodes=async ids => { window.deleteCalls.push(ids); };`);
  await h.run("ACT['node-delete']({id:'a'})");
  assert.deepEqual(JSON.parse(h.run("JSON.stringify(window.confirmCalls)")), []);
  assert.deepEqual(JSON.parse(h.run("JSON.stringify(window.deleteCalls)")), []);
  assert.ok(h.calls.every(c => c.method === "GET"));
  assert.match(h.actions[0][0], /管理员/);

  h.authenticated = true;
  await h.run("ACT['node-delete']({id:'a'})");
  const confirmations = JSON.parse(h.run("JSON.stringify(window.confirmCalls)"));
  assert.equal(confirmations.length, 1);
  assert.equal(confirmations[0].title, "删除终端");
  assert.equal(confirmations[0].okText, "删除终端");
  assert.match(confirmations[0].message, /客厅终端/);
  assert.deepEqual(JSON.parse(h.run("JSON.stringify(window.deleteCalls)")), []);
  assert.ok(h.calls.every(c => c.method === "GET"));

  h.run("confirmDlg=async()=>true");
  await h.run("ACT['node-delete']({id:'a'})");
  assert.deepEqual(JSON.parse(h.run("JSON.stringify(window.deleteCalls)")), [["a"]]);
});

test("terminal deletion uses DELETE and provides the existing restore undo", async () => {
  const h = harness(), node = h.node("a"); h.nodes = [node]; h.authenticated = true;
  h.run(`S.nodes=[${JSON.stringify(node)}]; confirmDlg=async()=>true; refreshCurrent=async()=>{};`);
  h.override = (method, route, body) => {
    if (method === "DELETE" && route === "/nodes/a") { h.nodes = []; return { ok: true, node }; }
    if (method === "POST" && route === "/nodes/restore") { h.nodes = [body.node]; return { ok: true }; }
    return undefined;
  };
  await h.run("ACT['node-delete']({id:'a'})");
  assert.ok(h.calls.some(c => c.method === "DELETE" && c.route === "/nodes/a"));
  const notice = h.actions.find(a => a[1] === "ok" && a[2]?.undo);
  assert.ok(notice, "successful deletion must expose undo");
  await notice[2].undo();
  const restore = h.calls.find(c => c.method === "POST" && c.route === "/nodes/restore");
  assert.deepEqual(JSON.parse(JSON.stringify(restore.body)), { node });
  assert.equal(h.nodes.length, 1);
  assert.equal(h.nodes[0].node_id, "a");
});

test("terminal deletion reports a playing endpoint rejection without undo", async () => {
  const h = harness(); h.nodes = [h.node("a", "online")]; h.authenticated = true;
  h.run("S.nodes=[{node_id:'a',alias:'客厅终端',status:'online',authorized:true}]; confirmDlg=async()=>true; refreshCurrent=async()=>{};");
  h.override = (method, route) => {
    if (method === "DELETE" && route === "/nodes/a") throw new Error("终端正在播放");
    return undefined;
  };
  await h.run("ACT['node-delete']({id:'a'})");
  assert.ok(h.calls.some(c => c.method === "DELETE" && c.route === "/nodes/a"));
  assert.ok(h.actions.some(a => a[1] === "err" && a[0].includes("终端正在播放")));
  assert.equal(h.actions.some(a => a[2]?.undo), false);
});

test("maintenance changes stop after logout or detaching the original control", async () => {
  for (const change of ["upd-policy"]) {
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
    { node_id:'b', alias:'Idle', status:'online', arch:'amd64', distro:'debian', playing:false, authorized:true, agent_version:'0.58', build_flavor:'management' },
    { node_id:'c', alias:'Playing', status:'online', arch:'arm64', distro:'debian', playing:true, play_protect:true, authorized:true, agent_version:'0.4.3', build_flavor:'audio' }
  ];
  S.upd.fleet = { target:'0.58', nodes:[
    { node_id:'a', current:'0.4.3', target:'0.58', state:'pending' },
    { node_id:'b', current:'0.58', target:'0.58', state:'converged' },
    { node_id:'c', current:'0.4.3', target:'0.58', state:'waiting_idle' }
  ] };
  TC.admin = true; TC.loading = false; paintNodeTable();`);
  const html = h.elements.get("#nd-table").innerHTML;
  assert.match(html, /0\.4\.3/);
  assert.match(html, /→ 0\.58/);
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

test("device activation and naming use the registry endpoints", async () => {
  const h = harness(); h.nodes = [h.node("a", "online")];
  h.devices.a = [{ hw: "hw:1,0", name: "", reported_name: "AUDIO", enabled: false, present: true, online: true }];
  h.override = (method, route, body) => {
    if (method === "POST" && route === "/nodes/a/devices/hw%3A1%2C0/activate") return { enabled: body.enabled };
    if (method === "PATCH" && route === "/nodes/a/devices/hw%3A1%2C0") return body;
  };
  await h.run("CHG['tc-dev-act']({checked:true},{id:'a',hw:'hw:1,0'})");
  assert.deepEqual(JSON.parse(JSON.stringify(h.calls.find(c => c.method === "POST").body)), { enabled: true });
  const input = { value: "  Living DAC  " };
  const control = { closest: () => ({ querySelector: () => input }) };
  h.context.control = control;
  await h.run("ACT['tc-dev-rename-save']({id:'a',hw:'hw:1,0'}, control)");
  assert.deepEqual(JSON.parse(JSON.stringify(h.calls.find(c => c.method === "PATCH").body)), { name: "Living DAC" });
});
