"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const assets = path.join(__dirname, "assets");
const deferred = () => { let resolve; const promise = new Promise(r => { resolve = r; }); return { promise, resolve }; };
const response = data => ({ ok: true, status: 200, json: async () => data, text: async () => JSON.stringify(data) });

// Isolated DOM and fetch doubles: never starts a browser or contacts a server.
function harness() {
  const elements = new Map();
  const element = (textContent = "") => ({ textContent, innerHTML: "", dataset: {}, querySelectorAll: () => [], setAttribute() {}, removeAttribute() {} });
  const h = { elements, calls: [], metrics: { version: "9.8.7-test+build.42" } };
  const context = {
    console, URL, URLSearchParams, AbortController,
    setTimeout: () => 1, clearTimeout() {}, setInterval: () => 1, clearInterval() {},
    MutationObserver: class { observe() {} disconnect() {} },
    document: { hidden: false, addEventListener() {}, querySelector: s => elements.get(s) || null, querySelectorAll: () => [] },
    navigator: {}, location: { protocol: "http:", host: "localhost" },
    fetch: async (route, opts = {}) => {
      h.calls.push({ method: opts.method || "GET", route });
      if (h.fetchHandler) return h.fetchHandler(route, opts);
      assert.equal(opts.method || "GET", "GET");
      if (route === "/api/v1/settings/session") return response({ configured: true, authenticated: false, can_setup: false, secure_context: true });
      if (route === "/metrics/server") return h.metricsHandler ? h.metricsHandler() : response(h.metrics);
      throw new Error("Unexpected mock request: " + route);
    },
    __view: view => {
      vm.runInContext(`S.view=${JSON.stringify(view)}`, context);
      elements.clear();
      if (view !== "settings") return;
      h.html = vm.runInContext("shellSettings()", context);
      for (const match of h.html.matchAll(/\bid="([^"]+)"/g)) elements.set("#" + match[1], element());
      if (elements.has("#settings-about-version")) elements.get("#settings-about-version").textContent = "未获取";
    }
  };
  context.window = context;
  vm.createContext(context);
  for (const file of ["app.js", "settings.js"]) vm.runInContext(fs.readFileSync(path.join(assets, file), "utf8"), context, { filename: file });
  vm.runInContext("setView=__view", context);
  h.run = code => vm.runInContext(code, context);
  h.open = section => h.run(`openSettings(${JSON.stringify(section)})`);
  h.version = () => elements.get("#settings-about-version").textContent;
  return h;
}

test("about is last and removed account/protocol sections fall back safely", () => {
  const h = harness();
  const sections = JSON.parse(h.run("JSON.stringify(SETTINGS_SECTIONS.map(s => s[0]))"));
  assert.deepEqual(sections, ["profile", "authorization", "folders", "packages", "maintenance", "about"]);
  assert.equal(h.run("typeof settingsAccountsShell"), "undefined");
  assert.equal(h.run("typeof settingsProtocolsShell"), "undefined");
  for (const section of ["accounts", "protocols"]) {
    h.open(section);
    assert.equal(h.run("SETTINGS.section"), "profile");
    assert.doesNotMatch(h.html, /音乐服务账号|协议说明|settings-local-add/);
  }
  h.open("about");
  assert.match(h.html, /关于我们/);
  assert.match(h.html, /id="settings-about-version"[^>]*>未获取<\/strong>/);
  assert.equal(h.calls.length, 0);
});

test("about includes product, existing links, MIT terms and qualified third-party sources", () => {
  const h = harness(); h.open("about");
  assert.match(h.html, /Zephyr HiFi Kit/);
  assert.match(h.html, /和风HiFi音乐播放套件/);
  assert.match(h.html, /让音乐回归聆听，让管理化繁为简/);
  assert.match(h.html, /本地 \/ NAS 音乐库、ZAT 音频推流与播放终端/);
  for (const url of ["https://www.zephyr.hk", "https://github.com/Eyebright/Zephyr-HiFi-Kit", "https://github.com/Eyebright/Zephyr-HiFi-Kit/blob/main/LICENSE"]) {
    assert.ok(h.html.includes(`href="${url}" target="_blank" rel="noopener noreferrer"`));
  }
  const license = fs.readFileSync(path.join(__dirname, "../../LICENSE"), "utf8");
  assert.match(license, /MIT License/);
  assert.ok(license.includes("Copyright (c) 2026 Zephyr HiFi Kit contributors"));
  assert.match(h.html, /Copyright \(c\) 2026 Zephyr HiFi Kit contributors/);
  assert.match(h.html, /MIT 许可/);
  assert.match(h.html, /保留版权声明及许可声明/);
  assert.match(h.html, /按“原样”提供，不作任何明示或默示保证/);
  assert.match(h.html, /第三方组件遵循各自许可/);
  assert.match(h.html, /商标归各自权利人所有/);
  assert.match(h.html, /不表示获得 Apple、腾讯、Signalyst 或其他权利人的官方授权、认证、背书或合作/);
  assert.match(h.html, /当前尚未提供 QPlay 组件/);
  assert.match(h.html, /Music Player Daemon 开源项目/);
  assert.match(h.html, /shairport-sync \/ nqptp 开源项目/);
  assert.match(h.html, /upmpdcli、MPD、gmrender 开源项目/);
  assert.match(h.html, /gmrender 仅用于 UPnP/);
  assert.match(h.html, /Signalyst networkaudiod 厂商组件，公开获取渠道为厂商官方发布；非开源/);
  assert.match(h.html, /随本项目源代码公开，采用 MIT 许可/);
  assert.match(h.html, /不表示任何服务已安装或正在运行/);
  assert.match(h.html, /公开可获取不等于开源或可自由再分发/);
  assert.match(h.html, /在适用法律允许的范围内/);
  assert.match(h.html, /仅使用合法授权的音源与服务/);
  assert.doesNotMatch(h.run("settingsAboutShell()"), /<input|尚未接入|暂无可用授权入口|data-act="tc-goto"/);
});

test("about groups all six component sources in one list and makes the disclaimer explicit", () => {
  const h = harness();
  const html = h.run("settingsAboutShell()");
  const sources = html.match(/<section[^>]+aria-labelledby="settings-component-title">[\s\S]*?<\/section>/)?.[0];
  assert.ok(sources);
  assert.deepEqual([...sources.matchAll(/<dt>([^<]+)<\/dt>/g)].map(m => m[1]), ["QPlay", "MPD", "AirPlay", "UPnP / OpenHome", "HQPlayer NAA", "ZAT"]);
  assert.equal((sources.match(/<dd>/g) || []).length, 6);
  assert.equal((html.match(/<section /g) || []).length, 3);
  assert.doesNotMatch(html, /<article|settings-capability|协议能力与来源|前往终端中心/);
  const disclaimer = html.match(/<section class="card settings-disclaimer"[^>]+>[\s\S]*?<\/section>/)?.[0];
  assert.ok(disclaimer);
  assert.match(disclaimer, /<h3 id="settings-disclaimer-title">[^]*?免责声明<\/h3>/);
  for (const heading of ["许可与归属", "非官方关联", "支持边界", "无担保与责任限制", "合法使用"]) {
    assert.ok(disclaimer.includes(`<strong>${heading}：</strong>`));
  }
});

test("about refresh reads the root metrics version while locked, without terminal or music requests", async () => {
  const h = harness(); h.open("about");
  await h.run("refreshSettings()");
  assert.equal(h.version(), h.metrics.version);
  assert.equal(h.run("settingsAdmin()"), false);
  assert.deepEqual(h.calls, [
    { method: "GET", route: "/api/v1/settings/session" },
    { method: "GET", route: "/metrics/server" }
  ]);
  assert.doesNotMatch(fs.readFileSync(path.join(assets, "settings.js"), "utf8"), /0\.4\.3/);
});

test("missing or invalid version clears a prior version instead of reusing cached metrics", async () => {
  for (const metrics of [null, {}, { version: null }, { version: 43 }, { version: "" }, { version: " \n " }, { version: ["1.2.3"] }]) {
    const h = harness(); h.open("about");
    await h.run("refreshSettings()");
    assert.equal(h.version(), h.metrics.version);
    h.run("S.metrics={version:'cached-test-version'}");
    h.metrics = metrics;
    await h.run("ACT['settings-refresh']()");
    assert.equal(h.version(), "未获取");
    assert.equal(h.elements.get("#settings-section-error").innerHTML, "");
  }
});

test("network, HTTP and JSON errors show unavailable and refresh can recover", async () => {
  for (const handler of [
    () => { throw new Error("mock network failure"); },
    () => ({ ok: false, status: 503 }),
    () => ({ ok: true, json: async () => { throw new SyntaxError("mock invalid JSON"); } })
  ]) {
    const h = harness(); h.open("about");
    await h.run("refreshSettings()");
    h.metricsHandler = handler;
    await h.run("ACT['settings-refresh']()");
    assert.equal(h.version(), "未获取");
    assert.match(h.html, /免责声明/);
    h.metricsHandler = null;
    h.metrics = { version: "  10.2.1-rc.2  " };
    await h.run("ACT['settings-refresh']()");
    assert.equal(h.version(), "10.2.1-rc.2");
  }
});

test("server version is inserted as text, not HTML", async () => {
  const h = harness(); h.open("about");
  const el = h.elements.get("#settings-about-version");
  Object.defineProperty(el, "innerHTML", { set() { assert.fail("version must not be inserted as HTML"); } });
  h.metrics = { version: '<img src=x onerror="bad">' };
  await h.run("refreshSettings()");
  assert.equal(h.version(), h.metrics.version);
});

test("late metrics success or failure cannot replace a newer refresh", async () => {
  for (const ok of [true, false]) {
    const h = harness(); h.open("about");
    const started = deferred(), pending = deferred();
    h.metricsHandler = () => { started.resolve(); return pending.promise; };
    const old = h.run("refreshSettings()"); await started.promise;
    h.metricsHandler = null; h.metrics = { version: "new-test-version" };
    await h.run("refreshSettings()");
    pending.resolve(ok ? response({ version: "old-test-version" }) : { ok: false, status: 503 });
    await old;
    assert.equal(h.version(), "new-test-version");
  }
});

test("late metrics cannot update a departed view or a newly rendered about page", async () => {
  for (const returnToAbout of [false, true]) {
    const h = harness(); h.open("about");
    const started = deferred(), pending = deferred();
    h.metricsHandler = () => { started.resolve(); return pending.promise; };
    const el = h.elements.get("#settings-about-version");
    const old = h.run("refreshSettings()"); await started.promise;
    h.run("setView('audit')");
    if (returnToAbout) h.open("about");
    pending.resolve(response({ version: "stale-test-version" })); await old;
    assert.equal(el.textContent, "未获取");
    if (returnToAbout) assert.equal(h.version(), "未获取");
  }
});

test("music sources show retained indexes and scan statistics with gated actions", () => {
  const h = harness(); h.open("folders");
  h.run(`SETTINGS.mounts = [
    {id:'active',kind:'local',target:'D:/Music',share:'Music',status:'mounted',last_scan:1700000000,scan_result:{added:1,updated:2,unchanged:3,removed:4}},
    {id:'saved',kind:'smb',target:'//nas/music',host:'nas',share:'music',status:'archived'},
    {id:'unsafe',kind:'local',target:'<img src=x>',share:'<script>bad</script>',status:'unmounted'}
  ]; SETTINGS.counts.set('active',6); settingsPaintSources()`);
  const html = h.elements.get("#settings-sources").innerHTML;
  assert.equal(h.elements.get("#settings-source-count").textContent, "2 个已配置 · 1 个保留索引");
  assert.match(html, /新增 1 \/ 更新 2 \/ 未变 3 \/ 缺失 4/);
  assert.match(html, /上次成功扫描/);
  assert.match(html, /重新连接/);
  assert.match(html, /NAS 凭据已移除/);
  assert.match(html, /移除并保留索引/);
  assert.match(html, /彻底删除索引/);
  assert.match(html, /data-legacy="lib-scan-full"/);
  assert.match(html, /&lt;script&gt;bad&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>bad|<img src=x>/);
  for (const button of html.matchAll(/<button[^>]*>/g)) assert.match(button[0], / disabled/);
  h.run("SETTINGS.session={authenticated:true};settingsPaintSources()");
  const enabled = h.elements.get("#settings-sources").innerHTML;
  assert.doesNotMatch(enabled.match(/<button[^>]*data-id="active"[^>]*data-legacy="lib-scan"[^>]*>/)[0], / disabled/);
  assert.match(enabled.match(/<button[^>]*data-id="saved"[^>]*data-legacy="lib-scan"[^>]*>/)[0], / disabled/);
});

test("source scan actions use incremental by default and confirm full repair", async () => {
  const h = harness(); h.open("folders");
  h.fetchHandler = () => response({count:3,added:1,updated:1,unchanged:1,removed:0});
  h.run(`var notices=[], confirmed=false;
    toast=message=>notices.push(message);refreshCurrent=async()=>{};confirmDlg=async()=>confirmed`);
  await h.run("ACT['lib-scan']({id:'source/id'})");
  assert.deepEqual(h.calls, [{method:"POST",route:"/api/v1/mounts/source%2Fid/scan"}]);
  assert.match(h.run("notices[0]"), /新增 1 \/ 更新 1 \/ 未变 1/);
  await h.run("ACT['lib-scan-full']({id:'a'})");
  assert.equal(h.calls.length, 1);
  h.run("confirmed=true");
  await h.run("ACT['lib-scan-full']({id:'a'})");
  assert.equal(h.calls[1].route, "/api/v1/mounts/a/scan?mode=full");
});

test("retaining an index and permanent deletion are separate confirmed requests", async () => {
  const h = harness(); h.open("folders");
  h.fetchHandler = () => response({});
  h.run(`var dialogs=[], confirmed=false;
    toast=()=>{};refreshCurrent=async()=>{};confirmDlg=async options=>{dialogs.push(options);return confirmed}`);
  await h.run("ACT['lib-archive']({id:'a'})");
  await h.run("ACT['lib-delete']({id:'a'})");
  assert.equal(h.calls.length, 0);
  h.run("confirmed=true");
  await h.run("ACT['lib-archive']({id:'a'})");
  await h.run("ACT['lib-delete']({id:'a'})");
  assert.deepEqual(h.calls, [
    {method:"POST",route:"/api/v1/mounts/a/archive"}, {method:"DELETE",route:"/api/v1/mounts/a"}
  ]);
  assert.match(h.run("dialogs[0].message"), /索引、封面、播放列表与历史仍保留/);
  assert.match(h.run("dialogs[1].message"), /同地址下次需重新建立索引/);
});

test("source management actions reject locked, duplicate and unknown operations", async () => {
  const h = harness(); h.open("folders");
  h.run(`var sourceActions=0;toast=()=>{};refreshSettings=async()=>{};
    ACT['lib-scan-full']=async()=>{sourceActions++}`);
  await h.run("settingsMountAction({id:'a',legacy:'lib-scan-full'})");
  assert.equal(h.run("sourceActions"), 0);
  h.run("SETTINGS.session={authenticated:true};SETTINGS.busy.add('mount:a')");
  await h.run("settingsMountAction({id:'a',legacy:'lib-scan-full'})");
  assert.equal(h.run("sourceActions"), 0);
  h.run("SETTINGS.busy.clear()");
  await h.run("settingsMountAction({id:'a',legacy:'unexpected'})");
  await h.run("settingsMountAction({id:'a',legacy:'lib-scan-full'})");
  assert.equal(h.run("sourceActions"), 1);
  assert.equal(h.run("SETTINGS.busy.size"), 0);
});

test("readding a local directory tells the user its index was reused", async () => {
  const h = harness(); h.open("folders");
  h.elements.get("#settings-local-path").value = "D:/Music";
  h.fetchHandler = () => response({id:'existing',reused:true});
  h.run(`SETTINGS.session={authenticated:true};var reuseNotice='';
    toast=m=>{reuseNotice=m};refreshSettings=async()=>{}`);
  await h.run("settingsLocalAdd()");
  assert.match(h.run("reuseNotice"), /复用同源索引/);
  assert.equal(h.elements.get("#settings-local-path").value, "");
  assert.equal(h.run("SETTINGS.busy.size"), 0);
});

test("unrelated and removed settings routes never request about metrics", async () => {
  for (const section of ["profile", "accounts", "protocols"]) {
    const h = harness(); h.open(section); await h.run("refreshSettings()");
    assert.deepEqual(h.calls, [{ method: "GET", route: "/api/v1/settings/session" }]);
  }
});
