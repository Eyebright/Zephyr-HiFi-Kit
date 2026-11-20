"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const source = fs.readFileSync(path.join(__dirname, "assets", "terminal-install.js"), "utf8");
const clone = value => value === undefined ? undefined : JSON.parse(JSON.stringify(value));
const noSources = "没有适用于该终端发行版的第三方 APT 源（发行版自带软件包无需额外源）";
const error = (message, status, code) => Object.assign(new Error(message), { status, code });
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function node(id) {
  return { node_id: id, alias: "终端 " + id, arch: "arm64", distro: "bookworm", status: "online", authorized: true, playing: false, play_protect: true };
}
function status(id) {
  return { identity: { node_id: id, arch: "arm64", system_available: true, playing: false, play_protect: true },
    metrics: { distro: "Debian GNU/Linux 12 (bookworm)", arch: "arm64" },
    services: [{ app_id: "mpd", installed: false }, { app_id: "roonbridge", installed: false }] };
}
function artifact(app = "roonbridge", extra = {}) {
  return { id: app + "-v1", app_id: app, version: "1.0", arch: "arm64", format: "deb", distro_support: ["bookworm"],
    signed: true, signature: "repo/" + app + ".deb.sig", artifact_path: "repo/" + app + ".deb", sha256: "a".repeat(64),
    deps_pkgs: [], deps_apps: [], units: [app + ".service"], ...extra };
}
function harness() {
  const dom = { body: { innerHTML: "" }, foot: { innerHTML: "" }, onClose: null, title: "", open: false };
  const h = { dom, calls: [], confirms: [], toasts: [], paints: [], sessionChecks: 0, admin: true, sessionAdmin: true,
    nodes: [node("a"), node("b")], apps: { a: [], b: [] }, statuses: { a: status("a"), b: status("b") },
    apt: [{ app: { app_id: "mpd", name: "Music Player Daemon", upstream: "https://www.musicpd.org" },
      version: { id: "mpd-apt", app_id: "mpd", version: "apt", arch: "all", format: "apt", apt_package: "mpd", deps_pkgs: [], distro_support: ["bookworm"] } }],
    boms: [{ bom_id: "audio", version: 1, status: "published", apps: [{ app_id: "roonbridge", version: "1.0" }], variables: {}, units_enabled: [] }],
    versions: { roonbridge: [artifact()] }, refreshes: 0, taskViews: 0, confirm: true, noSources: false };
  h.latest = clone(h.boms[0]);
  h.close = () => {
    const cb = dom.onClose;
    dom.onClose = null; dom.open = false; dom.body.innerHTML = dom.foot.innerHTML = "";
    if (cb) cb();
  };
  h.otherModal = () => sandbox.openModal("另一个窗口", "其他内容", "其他按钮");
  const escape = value => String(value ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
  const sandbox = {
    ACT: {}, CHG: {}, S: { sel: new Set(["b"]), aptCmds: ["unchanged"] },
    api: async (method, url, body) => {
      h.calls.push({ method, url, body: clone(body) });
      if (h.intercept) {
        const override = await h.intercept(method, url, body);
        if (override !== undefined) return clone(override);
      }
      if (method === "GET") {
        if (url === "/nodes") return clone(h.nodes);
        if (url === "/apt-apps") return clone(h.apt);
        if (url === "/boms?status=published") return clone(h.boms);
        if (url === "/boms/audio") return clone(h.latest);
        let match = url.match(/^\/nodes\/([^/]+)\/(apps|local\/api\/status)$/);
        if (match) return clone(match[2] === "apps" ? h.apps[decodeURIComponent(match[1])] : h.statuses[decodeURIComponent(match[1])]);
        match = url.match(/^\/apps\/([^/]+)\/versions$/);
        if (match) return clone(h.versions[decodeURIComponent(match[1])] || []);
        throw error("未模拟的读取 " + url, 404);
      }
      if (method === "POST") {
        if (h.noSources && url.endsWith("/apt-sources/apply")) throw error(noSources, 400, "apt_sources_empty");
        if (url === "/jobs" || /\/nodes\/[^/]+\/(commands|apt-sources\/apply)$/.test(url)) return { id: "task-" + h.calls.length, status: "pending" };
      }
      throw new Error("禁止未声明的 API：" + method + " " + url);
    },
    esc: escape, enc: encodeURIComponent, icon: () => "", badge: (label, kind) => `<span class="${kind}">${escape(label)}</span>`,
    empty: () => "", kvRow: (label, value) => `<div class="kv-row"><span>${escape(label)}</span><span>${value}</span></div>`,
    openModal: (title, body, foot, opts = {}) => {
      h.close(); dom.open = true; dom.title = title; dom.body.innerHTML = body; dom.foot.innerHTML = foot; dom.onClose = opts.onClose;
    },
    closeModal: h.close,
    setModalBody: html => { assert.ok(dom.open, "closed modal must not be painted"); dom.body.innerHTML = html; h.paints.push(["body", html]); },
    setModalFoot: html => { assert.ok(dom.open, "closed modal must not be painted"); dom.foot.innerHTML = html; h.paints.push(["foot", html]); },
    confirmDlg: async opts => { h.confirms.push(clone(opts)); return h.onConfirm ? h.onConfirm(opts) : h.confirm; },
    toast: (...args) => h.toasts.push(args),
    settingsAdmin: () => h.admin,
    settingsRefreshSession: async () => { h.sessionChecks++; h.admin = h.sessionAdmin; if (h.onSession) await h.onSession(); },
    refreshTerminalCenter: async () => { h.refreshes++; }, showTerminalTasks: () => { h.taskViews++; }
  };
  sandbox.window = sandbox;
  vm.createContext(sandbox);
  for (const file of ["i18n.js", "i18n-features.js", "i18n-errors.js"]) vm.runInContext(fs.readFileSync(path.join(__dirname, "assets", file), "utf8"), sandbox);
  vm.runInContext(source, sandbox, { filename: "terminal-install.js" });
  h.context = sandbox;
  h.open = ids => sandbox.openTerminalInstall(ids);
  h.action = name => sandbox.ACT["ti-" + name]();
  h.body = () => dom.body.innerHTML;
  h.foot = () => dom.foot.innerHTML;
  h.posts = () => h.calls.filter(c => c.method !== "GET");
  h.disabled = () => /data-act="ti-submit"[^>]*\bdisabled\b/.test(h.foot());
  h.select = async (kind = "apt", id) => {
    const values = [...h.body().matchAll(/<option value="([^"]+)"/g)].map(m => m[1].replace(/&quot;/g, '"').replace(/&amp;/g, "&").replace(/&#39;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">"));
    const key = values.find(value => { const parts = JSON.parse(value); return parts[0] === kind && (!id || parts[1] === id); });
    assert.ok(key, "source option exists");
    await sandbox.CHG["ti-source"]({ value: key });
  };
  return h;
}

async function ready(h, kind = "apt", ids = ["a"]) { await h.open(ids); await h.select(kind); }

test("only exposes the installer entry point and namespaced handlers", () => {
  const h = harness();
  assert.equal(typeof h.context.openTerminalInstall, "function");
  assert.ok(Object.keys(h.context.ACT).every(k => k.startsWith("ti-")));
  assert.deepEqual(Object.keys(h.context.CHG), ["ti-source"]);
  assert.equal(h.context.preview, undefined);
});

test("takes a deduplicated caller snapshot; reads only selected terminals and never defaults to S.sel", async () => {
  const h = harness(), ids = ["a", "a"];
  const pending = h.open(ids); ids.push("b"); h.context.S.sel.add("unselected");
  await pending;
  assert.deepEqual(h.calls.filter(c => c.url.includes("/nodes/")).map(c => c.url).sort(), ["/nodes/a/apps", "/nodes/a/local/api/status"]);
  assert.ok(h.calls.every(c => c.method === "GET"));
  assert.match(h.body(), /目标固定为 1 台/);
  assert.doesNotMatch(h.body(), /type="checkbox"/);
  await h.action("submit"); assert.equal(h.posts().length, 0);
  await h.select(); await h.action("submit");
  assert.deepEqual(h.posts().map(c => c.url), ["/nodes/a/apt-sources/apply", "/nodes/a/commands"]);
  assert.deepEqual(h.context.S.aptCmds, ["unchanged"]);
  assert.match(h.confirms[0].message, /仅提交以上 1 台/);
  assert.equal(h.refreshes, 1);
  await h.action("tasks"); assert.equal(h.taskViews, 1); assert.equal(h.dom.open, false);
});

test("empty or invalid target arrays do not open or fetch", async () => {
  const h = harness();
  for (const value of [[], undefined, new Set(["a"]), [""], [null]]) await h.open(value);
  assert.equal(h.calls.length, 0); assert.equal(h.dom.open, false);
});

test("APT options use app/version metadata and disclose candidate rather than precise version", async () => {
  const h = harness(); h.apt[0].version.deps_pkgs = ["libasound2"];
  await ready(h);
  assert.match(h.body(), /Music Player Daemon/);
  assert.match(h.body(), /包 mpd/); assert.match(h.body(), /依赖 libasound2/);
  assert.match(h.body(), /来源 APT 发行版签名仓库/);
  assert.match(h.body(), /发行版候选（精确版本未知）/);
  assert.equal(h.disabled(), false);
});

test("platform and state blockers never infer Linux compatibility from matching CPU architecture", async t => {
  const cases = [
    ["offline", h => { h.nodes[0].status = "offline"; }, /终端离线/],
    ["unauthorized", h => { h.nodes[0].authorized = false; }, /终端未授权/],
    ["system capability false", h => { h.statuses.a.identity.system_available = false; }, /系统服务管理不可用/],
    ["missing system capability", h => { delete h.statuses.a.identity.system_available; }, /系统服务管理不可用/],
    ["Windows same arch", h => { h.nodes[0].distro = "Windows 11"; h.statuses.a.metrics.distro = "Windows 11"; }, /不能仅凭 CPU 架构安装/],
    ["Android arm64", h => { h.nodes[0].distro = "Android"; }, /不能仅凭 CPU 架构安装/],
    ["live Fedora overrides stale Debian", h => { h.statuses.a.metrics.distro = "Fedora Linux 42"; }, /不是已确认的/],
    ["wrong arch", h => { h.apt[0].version.arch = "amd64"; }, /制品架构 amd64 不适配 arm64/],
    ["identity arch mismatch", h => { h.statuses.a.identity.arch = "amd64"; }, /架构报告不一致/],
    ["metrics arch mismatch", h => { h.statuses.a.metrics.arch = "amd64"; }, /架构报告不一致/],
    ["identity mismatch", h => { h.statuses.a.identity.node_id = "b"; }, /终端身份与目标不一致/],
    ["unsupported distro", h => { h.apt[0].version.distro_support = ["jammy"]; }, /发行版不在制品支持范围/],
    ["stale distro codename", h => { h.statuses.a.metrics.distro = "Debian GNU/Linux 13 (trixie)"; }, /发行版不在制品支持范围/],
    ["glibc cannot be guessed", h => { h.apt[0].version.glibc_min = "2.36"; }, /API 未提供 glibc 版本/],
    ["playing", h => { h.nodes[0].playing = true; }, /播放保护/],
    ["live playing", h => { h.statuses.a.identity.playing = true; }, /播放保护/],
    ["playback unknown", h => { delete h.statuses.a.identity.playing; }, /播放状态未确认/],
    ["malformed status", h => { h.statuses.a = {}; }, /状态响应不完整/],
    ["missing node", h => { h.nodes = [node("b")]; }, /已不存在/],
    ["revoked version", h => { h.apt[0].version.lifecycle = "revoked"; }, /已禁用或撤销/],
    ["invalid package", h => { h.apt[0].version.apt_package = "mpd;reboot"; }, /包名不合法/]
  ];
  for (const [name, alter, expected] of cases) await t.test(name, async () => {
    const h = harness(); alter(h); await ready(h);
    assert.match(h.body(), expected); assert.ok(h.disabled());
    await h.action("submit"); assert.equal(h.posts().length, 0);
  });
});

test("read failures and unknown inventory cannot be presented as not installed", async t => {
  for (const endpoint of ["/nodes/a/apps", "/nodes/a/local/api/status", "/nodes"]) await t.test(endpoint, async () => {
    const h = harness(); h.intercept = (method, url) => { if (url === endpoint) throw error("离线", 502); };
    await ready(h); assert.ok(h.disabled()); assert.match(h.body(), /读取失败/);
    if (endpoint !== "/nodes") {
      assert.match(h.body(), /状态未知（不能判定未安装）/);
      assert.doesNotMatch(h.body(), /未安装（本地服务状态）/);
    }
    await h.action("submit"); assert.equal(h.posts().length, 0);
  });
});

test("source system, dependencies and unknown sources prevent takeover", async t => {
  await t.test("system primary", async () => {
    const h = harness(); h.statuses.a.services[0] = { app_id: "mpd", installed: true, source: "system", version: "0.23" };
    await ready(h); assert.match(h.body(), /系统自带服务.*禁止接管更新/); assert.ok(h.disabled());
  });
  await t.test("system dependency", async () => {
    const h = harness(); h.apt[0].app.app_id = "upmpdcli"; h.apt[0].version.apt_package = "upmpdcli"; h.apt[0].version.deps_pkgs = ["mpd"];
    h.statuses.a.services[0] = { app_id: "mpd", installed: true, source: "system", version: "0.23" };
    await ready(h); assert.match(h.body(), /mpd：系统自带服务/); assert.ok(h.disabled());
  });
  await t.test("unknown source", async () => {
    const h = harness(); h.statuses.a.services[0] = { app_id: "mpd", installed: true, version: "0.23" };
    await ready(h); assert.match(h.body(), /来源未知/); assert.ok(h.disabled());
  });
  await t.test("installed APT candidate not known", async () => {
    const h = harness(); h.statuses.a.services[0] = { app_id: "mpd", installed: true, source: "server", version: "0.23" };
    await ready(h); assert.match(h.body(), /无法确认发行版候选是否更新，不自动重装/); assert.ok(h.disabled());
  });
});

test("mixed targets disable the entire round rather than silently skip invalid targets", async () => {
  const h = harness(); h.nodes[1].authorized = false;
  await ready(h, "apt", ["a", "b"]);
  assert.match(h.body(), /终端 a/); assert.match(h.body(), /终端 b/);
  assert.match(h.body(), /整体禁止执行，不会静默跳过/); assert.ok(h.disabled());
  await h.action("submit"); assert.equal(h.posts().length, 0);
});

test("read-only preview works while locked; submission revalidates administrator session", async () => {
  const h = harness(); h.admin = h.sessionAdmin = false;
  await ready(h); assert.match(h.body(), /Music Player Daemon/); assert.ok(h.disabled());
  await h.action("submit"); assert.equal(h.posts().length, 0); assert.equal(h.confirms.length, 0);
  await h.action("unlock"); assert.match(h.toasts.at(-1)[0], /设置页解锁/);
  h.sessionAdmin = true; await h.action("refresh"); assert.equal(h.disabled(), false);
  h.sessionAdmin = false; await h.action("submit"); assert.equal(h.posts().length, 0);
  assert.match(h.body(), /会话未确认或已失效/);
});

test("cancelled confirmation performs no writes and allows a later deliberate submission", async () => {
  const h = harness(); h.confirm = false; await ready(h); await h.action("submit");
  assert.equal(h.posts().length, 0); assert.equal(h.disabled(), false);
  h.confirm = true; await h.action("submit"); assert.equal(h.posts().length, 2);
});

test("preflight detects changed terminal status and option snapshot before confirming", async t => {
  for (const [name, change] of [
    ["authorization", h => { h.nodes[0].authorized = false; }],
    ["packages", h => { h.apt[0].version.deps_pkgs = ["new-dependency"]; }],
    ["source removed", h => { h.apt = []; }],
    ["installed state", h => { h.statuses.a.services[0] = { app_id: "mpd", installed: true, source: "system" }; }]
  ]) await t.test(name, async () => {
    const h = harness(); await ready(h); change(h); await h.action("submit");
    assert.equal(h.posts().length, 0); assert.equal(h.confirms.length, 0); assert.match(h.body(), /已变化/);
  });
});

test("confirmation-time status or session changes abort before any write", async t => {
  await t.test("playing begins", async () => {
    const h = harness(); await ready(h);
    h.onConfirm = () => { h.statuses.a.identity.playing = true; return true; };
    await h.action("submit"); assert.equal(h.posts().length, 0); assert.match(h.body(), /播放保护/);
  });
  await t.test("session expires", async () => {
    const h = harness(); await ready(h);
    h.onConfirm = () => { h.sessionAdmin = false; return true; };
    await h.action("submit"); assert.equal(h.posts().length, 0); assert.match(h.body(), /会话未确认或已失效/);
  });
});

test("busy guards double click and option changes while confirming", async () => {
  const h = harness(), entered = deferred(), confirm = deferred(); await ready(h);
  h.onConfirm = () => { entered.resolve(); return confirm.promise; };
  const submitting = h.action("submit"); await entered.promise;
  await h.action("submit"); await h.context.CHG["ti-source"]({ value: "changed" }); await h.action("refresh");
  assert.equal(h.confirms.length, 1); confirm.resolve(true); await submitting;
  assert.equal(h.posts().length, 2); assert.equal(h.posts()[1].body.params.packages[0], "mpd");
  await h.action("submit"); await h.action("refresh"); assert.equal(h.posts().length, 2);
});

test("APT posts sources then packages per node; only the precise no-extra-source 400 is ignored", async t => {
  await t.test("specific 400 is safe", async () => {
    const h = harness(); h.noSources = true; await ready(h); await h.action("submit");
    assert.equal(h.posts().length, 2);
    assert.deepEqual(h.posts()[1].body, { type: "apt-install", params: { packages: ["mpd"] } });
    assert.match(h.body(), /提交成功，等待终端执行/);
  });
  for (const failure of [error("其他错误：无需额外源？", 400), error(noSources, 400), error(noSources, 403, "apt_sources_empty"), error("源请求超时", undefined)]) await t.test(failure.message + failure.status, async () => {
    const h = harness(); h.intercept = (method, url) => { if (method === "POST" && url.endsWith("/apt-sources/apply")) throw failure; };
    await ready(h); await h.action("submit"); await h.action("submit");
    assert.equal(h.posts().length, 1); assert.match(h.body(), /不会自动重试/);
  });
});

test("APT no-source handling uses the stable code in every UI language", async () => {
  for (const locale of ["zh-CN", "zh-TW", "en"]) {
    const h = harness();
    vm.runInContext(`I18n.setPreference(${JSON.stringify(locale)})`, h.context);
    h.intercept = (method, url) => { if (method === "POST" && url.endsWith("/apt-sources/apply")) throw error("No external APT sources", 400, "apt_sources_empty"); };
    await ready(h); await h.action("submit");
    assert.equal(h.posts().length, 2, locale);
    assert.equal(h.posts()[1].body.type, "apt-install");
  }
});

test("per-terminal errors preserve preceding tasks, continue confirmed targets and never retry", async () => {
  const h = harness(); await ready(h, "apt", ["a", "b"]);
  h.intercept = (method, url) => { if (method === "POST" && url === "/nodes/a/commands") throw error("连接中断", 502); };
  await h.action("submit");
  assert.deepEqual(h.posts().map(p => p.url), ["/nodes/a/apt-sources/apply", "/nodes/a/commands", "/nodes/b/apt-sources/apply", "/nodes/b/commands"]);
  assert.match(h.body(), /提交结果不明：连接中断/); assert.match(h.body(), /已有前置任务已提交/); assert.match(h.body(), /提交成功/);
  await h.action("submit"); assert.equal(h.posts().length, 4);
});

test("a response without task ID is unknown and must not trigger follow-on installation", async () => {
  const h = harness(); h.intercept = method => method === "POST" ? {} : undefined;
  await ready(h); await h.action("submit");
  assert.equal(h.posts().length, 1); assert.match(h.body(), /未返回任务 ID/);
});

test("BOM preview reads real versions and submits only the selected published BOM", async () => {
  const h = harness(); await ready(h, "bom");
  assert.ok(h.calls.some(c => c.url === "/apps/roonbridge/versions"));
  assert.ok(h.calls.every(c => c.method === "GET"));
  assert.match(h.body(), /audio@1/); assert.match(h.body(), /roonbridge@1.0/); assert.match(h.body(), /不能持久锁定此版本/);
  assert.equal(h.disabled(), false); await h.action("submit");
  assert.deepEqual(h.posts(), [{ method: "POST", url: "/jobs", body: { node_id: "a", bom_id: "audio", bom_version: 1, kind: "provision" } }]);
});

test("BOM compatibility rejects unsigned, unsupported, mismatched and unsafe templates", async t => {
  const cases = [
    ["unsigned", h => { h.versions.roonbridge[0].signed = false; }, /不能绕过签名交付/],
    ["missing signature", h => { h.versions.roonbridge[0].signature = ""; }, /签名归档/],
    ["missing exact arch", h => { h.versions.roonbridge[0].arch = "amd64"; }, /缺少所选版本与终端架构/],
    ["all cannot resolve in BOM Agent", h => { h.versions.roonbridge[0].arch = "all"; }, /不能交付 all 架构制品/],
    ["wrong OS format", h => { h.versions.roonbridge[0].format = "exe"; }, /不支持的 Linux 交付格式/],
    ["wrong file suffix", h => { h.versions.roonbridge[0].artifact_path = "repo/roonbridge.exe"; }, /格式声明与归档文件名不符/],
    ["Windows archive marker", h => { h.versions.roonbridge[0].artifact_path = "repo/roonbridge_windows_arm64.deb"; }, /制品路径标识为非 Linux 包/],
    ["generic archive is not Linux evidence", h => { h.versions.roonbridge[0].format = "tar.bz2"; h.versions.roonbridge[0].distro_support = []; }, /不能仅凭 CPU 架构判定兼容/],
    ["glibc not reported", h => { h.versions.roonbridge[0].glibc_min = "2.31"; }, /无法验证/],
    ["APT cannot use archive executor", h => { h.versions.roonbridge[0].format = "apt"; }, /请改用 APT 服务来源/],
    ["new draft would override selected version", h => { h.latest.version = 2; h.latest.status = "draft"; }, /最新模板与所选已发布版本不同/],
    ["playing", h => { h.statuses.a.identity.playing = true; }, /本轮不安装/],
    ["system service", h => { h.statuses.a.services[1] = { app_id: "roonbridge", installed: true, source: "system", version: "0.9" }; }, /禁止接管更新/],
    ["same version", h => { h.statuses.a.services[1] = { app_id: "roonbridge", installed: true, source: "server", version: "1.0" }; }, /所有目标版本均已安装，无需安装/]
  ];
  for (const [name, alter, expected] of cases) await t.test(name, async () => {
    const h = harness(); alter(h); await ready(h, "bom"); assert.ok(h.disabled()); assert.match(h.body(), expected);
    await h.action("submit"); assert.equal(h.posts().length, 0);
  });
});

test("same-version components in a mixed BOM prevent unnecessary whole-template reinstallation", async () => {
  const h = harness(); h.boms[0].apps.push({ app_id: "networkaudiod", version: "1.0" }); h.latest = clone(h.boms[0]);
  h.versions.networkaudiod = [artifact("networkaudiod")];
  h.statuses.a.services[1] = { app_id: "roonbridge", installed: true, source: "server", version: "1.0" };
  await ready(h, "bom"); assert.ok(h.disabled()); assert.match(h.body(), /禁止不必要重装/);
});

test("closing or replacing modal discards late initial responses", async t => {
  for (const replace of [false, true]) await t.test(String(replace), async () => {
    const h = harness(), entered = deferred(), gate = deferred();
    h.intercept = (method, url) => { if (url === "/nodes") { entered.resolve(); return gate.promise; } };
    const opening = h.open(["a"]); await entered.promise;
    if (replace) h.otherModal(); else h.close();
    const paints = h.paints.length; gate.resolve(h.nodes); await opening;
    assert.equal(h.paints.length, paints); assert.equal(h.body(), replace ? "其他内容" : ""); assert.equal(h.posts().length, 0);
  });
});

test("new install epoch wins over old metadata response", async () => {
  const h = harness(); await h.open(["a"]);
  const gate = deferred(), entered = deferred();
  h.intercept = (method, url) => { if (url === "/apps/roonbridge/versions") { entered.resolve(); return gate.promise; } };
  const choosing = h.select("bom"); await entered.promise;
  await h.open(["b"]); const body = h.body(); gate.resolve(h.versions.roonbridge); await choosing;
  assert.equal(h.body(), body); assert.match(body, /终端 b/); assert.doesNotMatch(body, /终端 a/);
});

test("closing during confirmation prevents writes even if confirmation later resolves true", async () => {
  const h = harness(); await ready(h);
  h.onConfirm = () => { h.otherModal(); return true; };
  await h.action("submit"); assert.equal(h.posts().length, 0); assert.equal(h.body(), "其他内容");
});

test("closing during a source request does not submit install or other targets and cannot overwrite replacement modal", async () => {
  const h = harness(), entered = deferred(), gate = deferred(); await ready(h, "apt", ["a", "b"]);
  h.intercept = (method, url) => { if (method === "POST" && url.endsWith("/apt-sources/apply")) { entered.resolve(); return gate.promise; } };
  const pending = h.action("submit"); await entered.promise; h.otherModal(); const paints = h.paints.length;
  gate.resolve({ id: "source-accepted" }); await pending;
  assert.equal(h.posts().length, 1); assert.equal(h.paints.length, paints); assert.equal(h.body(), "其他内容");
});

test("read errors for one catalog do not invent options or prevent safe use of another source", async () => {
  const h = harness(); h.intercept = (method, url) => { if (url === "/apt-apps") throw error("目录暂不可用", 503); };
  await ready(h, "bom"); assert.match(h.body(), /APT 服务读取失败/); assert.equal(h.disabled(), false);
});

test("untrusted labels and errors are escaped in modal output", async () => {
  const h = harness(); h.nodes[0].alias = '<img src=x onerror="evil()">'; h.apt[0].app.name = "<script>evil()</script>";
  await ready(h); assert.doesNotMatch(h.body(), /<img|<script>/); assert.match(h.body(), /&lt;script&gt;/);
});
