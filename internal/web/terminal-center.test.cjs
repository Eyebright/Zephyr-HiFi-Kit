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
  vm.runInContext(fs.readFileSync(path.join(assets, "app.js"), "utf8"), context);
  vm.runInContext(fs.readFileSync(path.join(assets, "terminal-center.js"), "utf8"), context);
  vm.runInContext(`req = (method, route, opts) => __api(method, route, opts?.body ? JSON.parse(opts.body) : undefined); toast = __toast; S.view = 'nodes'; openDrawer = (title, sub, body, foot, opts) => { drawerCtx = opts.ctx; __drawer(title, sub, body, foot, opts); }; setDrawerBody = html => __drawer('', '', html, '', {}); openPlayer = __player;`, context);
  h.run = source => vm.runInContext(source, context);
  h.context = context;
  h.node = (id, status = "offline") => ({ node_id: id, alias: "终端 " + id, status, arch: "arm64", distro: "debian", playing: false, authorized: true });
  return h;
}

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
