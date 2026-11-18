"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const assets = path.join(__dirname, "assets");
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };

// Small DOM mock: no browser, real network, production service or credentials.
function harness(saved) {
  const elements = new Map(), listeners = new Map();
  const h = { calls: [], events: [], actions: [], mounts: [], nodes: [], license: { effective_seats: 3 }, mobile: false,
    profile: saved || { display_name: "", onboarding_step: 1, onboarding_complete: false, legacy: false },
    session: { configured: false, authenticated: false, secure_context: true, can_setup: true } };
  class Element {
    constructor(tag = "div", attrs = {}, parent = null) {
      this.tagName = tag; this.attrs = attrs; this.parent = parent; this.children = []; this.dataset = {}; this.value = attrs.value || "";
      this.disabled = "disabled" in attrs; this.hidden = "hidden" in attrs; this.inert = false; this.textContent = "";
      for (const [key, value] of Object.entries(attrs)) if (key.startsWith("data-")) this.dataset[key.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = value;
      const classes = new Set((attrs.class || "").split(" "));
      this.classList = { add: c => classes.add(c), remove: c => classes.delete(c), contains: c => classes.has(c), toggle(c, on) { on ??= !classes.has(c); if (on) classes.add(c); else classes.delete(c); } };
      if (attrs.id) elements.set("#" + attrs.id, this);
    }
    set innerHTML(html) {
      for (const child of this.children) if (child.attrs.id && elements.get("#" + child.attrs.id) === child) elements.delete("#" + child.attrs.id);
      this.children = []; this.html = html;
      for (const match of html.matchAll(/<([a-z][\w-]*)\b([^>]*)>/gi)) {
        const attrs = {};
        for (const attr of match[2].matchAll(/([\w-]+)(?:="([^"]*)"|='([^']*)')?/g)) attrs[attr[1]] = attr[2] ?? attr[3] ?? "";
        this.children.push(new Element(match[1], attrs, this));
      }
    }
    get innerHTML() { return this.html || ""; }
    setAttribute(k, v) { this.attrs[k] = String(v); }
    removeAttribute(k) { delete this.attrs[k]; }
    hasAttribute(k) { return k in this.attrs; }
    contains(el) { return el === this || el?.parent === this; }
    focus() { document.activeElement = this; }
    scrollIntoView() {}
    matches(s) {
      if (s === "[hidden]") return this.hidden;
      if (s.startsWith("#")) return this.attrs.id === s.slice(1);
      const attr = s.match(/^\[([^=\]]+)(?:=['"]?([^'"\]]+)['"]?)?\]$/);
      if (attr) return this.hasAttribute(attr[1]) && (!attr[2] || this.attrs[attr[1]] === attr[2]);
      if (s === "button:not(:disabled)") return this.tagName === "button" && !this.disabled;
      if (s === "a[href]") return this.tagName === "a" && this.hasAttribute("href");
      return s === this.tagName;
    }
    closest(s) { return s.split(/,\s*/).some(x => this.matches(x)) ? this : this.parent?.closest(s) || null; }
    querySelectorAll(s) { return this.children.filter(el => s.split(/,\s*/).some(x => el.matches(x))); }
    querySelector(s) { return this.querySelectorAll(s)[0] || null; }
  }
  const document = {
    hidden: false, activeElement: null,
    addEventListener(name, fn) { if (!listeners.has(name)) listeners.set(name, []); listeners.get(name).push(fn); },
    dispatchEvent(event) { h.events.push(event); (listeners.get(event.type) || []).forEach(fn => fn(event)); },
    querySelector(s) {
      if (elements.has(s)) return elements.get(s);
      const match = s.match(/^(#[\w-]+) (.+)$/); if (match) return elements.get(match[1])?.querySelector(match[2]) || null;
      return null;
    },
    querySelectorAll(s) { return [...new Set([...elements.values()].flatMap(el => [el, ...el.children]))].filter(el => s.split(/,\s*/).some(x => el.matches(x))); }
  };
  document.body = new Element("body");
  for (const id of ["nav", "sidebar", "mobile-nav-toggle", "mobile-nav-close", "nav-backdrop", "onboarding-root", "view", "modal-root", "drawer-root"]) new Element("div", { id });
  const main = new Element(); elements.set("#app > .main", main);
  const context = {
    console, URL, URLSearchParams, AbortController, document,
    CustomEvent: class { constructor(type, opts) { this.type = type; this.detail = opts.detail; } },
    MutationObserver: class { observe() {} disconnect() {} },
    location: { origin: "http://localhost", protocol: "http:", host: "localhost" }, navigator: {},
    setTimeout: () => 1, clearTimeout() {}, setInterval: () => 1, clearInterval() {},
    matchMedia: () => ({ get matches() { return h.mobile; }, addEventListener(_, fn) { h.resize = fn; } }),
    __api: async (method, route, body) => {
      h.calls.push({ method, route, body: body ? JSON.parse(JSON.stringify(body)) : undefined });
      if (h.override) { const result = await h.override(method, route, body); if (result !== undefined) return result; }
      if (method === "GET") {
        if (route === "/settings/profile") return { ...h.profile };
        if (route === "/settings/session") return { ...h.session };
        if (route === "/mounts") return h.mounts;
        if (route === "/nodes") return h.nodes;
        if (route === "/license") return h.license;
        if (route.startsWith("/library/browse?")) return { items: [], total: 0 };
      }
      if (method === "POST" && route === "/settings/setup") {
        assert.ok(body.display_name); assert.ok(body.password.length >= 12);
        h.session.configured = h.session.authenticated = true;
        h.profile = { ...h.profile, display_name: body.display_name, onboarding_step: 2 };
        return { ...h.session };
      }
      if (method === "POST" && route === "/settings/login") { h.session.authenticated = true; return { ...h.session }; }
      if (method === "POST" && route === "/settings/logout") { h.session.authenticated = false; return {}; }
      if (method === "PATCH" && route === "/settings/profile") { assert.ok(h.session.authenticated); h.profile.display_name = body.display_name; return { ...h.profile }; }
      if (method === "PATCH" && route === "/settings/onboarding") { assert.ok(h.session.authenticated); h.profile.onboarding_step = body.step; h.profile.onboarding_complete = body.complete; return { ...h.profile }; }
      throw new Error("Unexpected mock route: " + method + " " + route);
    },
    __toast: (...args) => h.actions.push(["toast", ...args]),
    __view: view => {
      h.actions.push(["view", view]);
      vm.runInContext(`S.view=${JSON.stringify(view)}; renderSidebarNav();`, context);
      const html = view === "settings" ? vm.runInContext("shellSettings()", context) : "";
      elements.get("#view").innerHTML = html;
    },
    __wizard: () => h.actions.push(["wizard"])
  };
  context.window = context; vm.createContext(context);
  for (const file of ["app.js", "settings.js", "experience.js"]) vm.runInContext(fs.readFileSync(path.join(assets, file), "utf8"), context, { filename: file });
  vm.runInContext("req=(method, route, opts)=>__api(method, route, opts?.body ? JSON.parse(opts.body) : undefined); toast=__toast; setView=__view; openWizard=__wizard; if(!NAV.some(n=>n.id==='settings')) NAV.push({id:'settings',name:'设置',icon:'settings'}); renderSidebarNav();", context);
  h.run = code => vm.runInContext(code, context); h.elements = elements; h.listeners = listeners; h.document = document;
  h.input = (id, value) => { const el = elements.get("#" + id) || new Element("input", { id }); el.value = value; return el; };
  h.admin = () => { h.session.configured = h.session.authenticated = true; };
  h.state = () => JSON.parse(h.run("JSON.stringify(XP.profile)"));
  return h;
}
const profile = step => ({ display_name: "测试管理员", onboarding_step: step, onboarding_complete: false, legacy: false });

test("navigation and page headings use home and operation log labels without changing routes", () => {
  const h = harness();
  assert.equal(h.run("NAV.find(n => n.id === 'overview').name"), "主页");
  assert.equal(h.run("NAV.find(n => n.id === 'audit').name"), "操作日志");
  assert.match(h.run("shellOverview()"), /id="ov-greeting">主页<\/h1>/);
  assert.match(h.run("shellAudit()"), /操作日志/);
  assert.doesNotMatch(h.elements.get("#nav").innerHTML, /听歌首页|审计/);
});

test("settings is last, collapses independently, and openSettings expands/activates its child", () => {
  const h = harness();
  assert.equal(h.run("SETTINGS_SECTIONS[0][0]"), "profile");
  assert.match(h.elements.get("#nav").innerHTML, /aria-expanded="false"/);
  h.run("ACT['xp-settings-toggle']()"); assert.equal(h.run("XP.settingsOpen"), true);
  h.run("ACT['xp-settings-toggle']()"); assert.equal(h.run("XP.settingsOpen"), false);
  h.run("openSettings('folders')");
  const html = h.elements.get("#nav").innerHTML;
  assert.ok(html.indexOf('data-view="settings"') > html.indexOf('data-view="audit"'));
  assert.match(html, /data-section="folders" aria-current="page"/);
  assert.equal(h.run("XP.settingsOpen"), true);
  h.run("ACT['xp-settings-toggle']()"); assert.equal(h.run("XP.settingsOpen"), false);
  assert.doesNotMatch(h.run("shellSettings()"), /<nav|settings-nav-item/);
});

test("mobile drawer toggles aria, inert state, overlay and Escape without changing ordinary nav", () => {
  const h = harness(); h.mobile = true; h.run("experienceInit(); toggleExperienceNav(true)");
  assert.equal(h.elements.get("#mobile-nav-toggle").attrs["aria-expanded"], "true");
  assert.equal(h.elements.get("#nav-backdrop").hidden, false);
  assert.equal(h.elements.get("#app > .main").inert, true);
  let prevented = 0; const event = { key: "Escape", preventDefault() { prevented++; }, stopImmediatePropagation() {} };
  h.listeners.get("keydown").at(-1)(event);
  assert.equal(prevented, 1); assert.equal(h.run("XP.mobileOpen"), false);
  assert.equal(h.elements.get("#mobile-nav-toggle").attrs["aria-expanded"], "false");
  assert.equal(h.elements.get("#sidebar").inert, true);
  h.mobile = false; h.resize(); assert.equal(h.elements.get("#sidebar").inert, false);
  assert.match(h.elements.get("#nav").innerHTML, /data-act="nav"/);
});

test("initial setup validates name, minimum password and confirmation; persists step 2", async () => {
  const h = harness(); await h.run("bootstrapExperience()");
  for (const values of [["", "test-password-only", "test-password-only"], ["测试", "short", "short"], ["测试", "test-password-only", "different"]]) {
    h.input("onboarding-name", values[0]); h.input("onboarding-password", values[1]); h.input("onboarding-password-confirm", values[2]);
    assert.equal(await h.run("ACT['xp-auth']()"), false);
  }
  assert.ok(h.calls.every(c => c.method === "GET"));
  h.input("onboarding-name", "测试管理员"); h.input("onboarding-password", "test-password-only"); h.input("onboarding-password-confirm", "test-password-only");
  assert.equal(await h.run("ACT['xp-auth']()"), true);
  assert.equal(h.state().onboarding_step, 2); assert.equal(h.run("S.view"), "settings"); assert.equal(h.run("SETTINGS.section"), "folders");
  const request = h.calls.find(c => c.route === "/settings/setup");
  assert.deepEqual(request.body, { display_name: "测试管理员", password: "test-password-only" });
  assert.equal(h.run("SETTINGS.authBusy"), false);
});

test("settings auth also includes setup name/confirmation but legacy login only sends password", async () => {
  const h = harness(); h.run("openSettings('profile')"); await h.run("settingsRefreshSession()");
  assert.match(h.elements.get("#settings-admin").innerHTML, /settings-name/);
  assert.match(h.elements.get("#settings-admin").innerHTML, /settings-password-confirm/);
  h.session.configured = true; h.profile = { ...profile(4), legacy: true, onboarding_complete: true };
  await h.run("settingsRefreshSession()");
  assert.doesNotMatch(h.elements.get("#settings-admin").innerHTML, /settings-name|settings-password-confirm/);
  h.input("settings-password", "test-password-only"); await h.run("settingsAuthenticate()");
  assert.deepEqual(h.calls.find(c => c.route === "/settings/login").body, { password: "test-password-only" });
  assert.ok(!h.calls.some(c => c.route === "/settings/setup"));
});

test("insecure remote HTTP and non-local first setup never submit credentials", async () => {
  for (const session of [{ secure_context: false, can_setup: false }, { secure_context: true, can_setup: false }]) {
    const h = harness(); Object.assign(h.session, session); await h.run("bootstrapExperience()");
    assert.doesNotMatch(h.elements.get("#onboarding-root").innerHTML, /id="onboarding-password"/);
    h.input("onboarding-name", "测试"); h.input("onboarding-password", "test-password-only"); h.input("onboarding-password-confirm", "test-password-only");
    assert.equal(await h.run("ACT['xp-auth']()"), false);
    assert.ok(h.calls.every(c => c.method === "GET"));
  }
});

test("refresh restores saved step and step 2 embeds actual folders shell and actions", async () => {
  const h = harness(profile(2)); h.admin(); await h.run("bootstrapExperience()");
  assert.equal(h.run("SETTINGS.section"), "folders");
  const html = h.elements.get("#view").innerHTML;
  assert.match(html, /settings-local-add/); assert.match(html, /settings-nas-add/); assert.match(html, /settings-sources/);
  assert.equal(await h.run("experiencePersistStep(3)"), true);
  assert.equal(h.profile.onboarding_step, 3); assert.equal(h.run("S.view"), "nodes");
  const restored = harness(h.profile); restored.admin(); await restored.run("bootstrapExperience()");
  assert.equal(restored.run("S.view"), "nodes");
  restored.run("ACT['xp-add-node']()"); assert.ok(restored.actions.some(a => a[0] === "wizard"));
  assert.ok(!restored.calls.some(c => c.method !== "GET"));
  restored.run("ACT['xp-authorization']()"); assert.equal(restored.run("SETTINGS.section"), "authorization");
  await restored.run("ACT['xp-resume']()"); assert.equal(restored.run("S.view"), "nodes");
});

test("configured incomplete instance logs in and resumes without forced password setup", async () => {
  const h = harness(profile(3)); h.session.configured = true; await h.run("bootstrapExperience()");
  assert.equal(h.run("experienceStep()"), 1);
  assert.match(h.elements.get("#onboarding-root").innerHTML, /已有管理员密码/);
  h.input("onboarding-password", "test-password-only"); await h.run("ACT['xp-auth']()");
  assert.equal(h.run("experienceStep()"), 3); assert.equal(h.run("S.view"), "nodes");
  assert.ok(!h.calls.some(c => c.route === "/settings/setup"));
});

test("legacy complete instance prompts for name but never forces setup or onboarding", async () => {
  const h = harness({ ...profile(4), display_name: "", legacy: true, onboarding_complete: true }); h.session.configured = true;
  await h.run("bootstrapExperience()");
  assert.match(h.elements.get("#onboarding-root").innerHTML, /补充一个称呼/);
  assert.equal(h.run("XP.active"), false);
  assert.ok(!h.actions.some(a => a[0] === "view"));
  assert.ok(h.calls.every(c => c.method === "GET"));
});

test("name save is authenticated, emits profile-updated and preserves progress", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()");
  h.input("settings-display-name", " 新称呼 ");
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), true);
  assert.equal(h.state().display_name, "新称呼"); assert.equal(h.state().onboarding_step, 3);
  assert.equal(h.events.filter(e => e.type === "profile-updated").at(-1).detail.display_name, "新称呼");
  h.input("settings-display-name", " "); assert.equal(await h.run("experienceSaveName('settings-display-name')"), false);
  await h.run("settingsLogout()"); h.input("settings-display-name", "拒绝保存");
  const count = h.calls.filter(c => c.method === "PATCH").length;
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), false);
  await h.run("experiencePersistStep(4)");
  assert.equal(h.calls.filter(c => c.method === "PATCH").length, count);
});

test("progress write failure and unconfirmed response cannot skip or complete", async () => {
  const h = harness(profile(2)); h.admin(); await h.run("bootstrapExperience()");
  h.override = (method, route) => { if (method === "PATCH" && route === "/settings/onboarding") throw new Error("test write failed"); };
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  assert.equal(h.state().onboarding_step, 2); assert.equal(h.run("XP.busy"), null);
  h.override = (method, route) => method === "PATCH" && route === "/settings/onboarding" ? {} : undefined;
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  assert.equal(h.state().onboarding_step, 2);
  assert.equal(await h.run("experiencePersistStep(4, true)"), false);
  assert.equal(h.state().onboarding_complete, false);
});

test("all created-admin progress changes require auth and nonempty name", async () => {
  const h = harness(profile(2)); h.session.configured = true; await h.run("bootstrapExperience()");
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  h.admin(); await h.run("settingsRefreshSession()"); h.run("XP.profile.display_name=' '");
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  assert.ok(h.calls.every(c => c.method === "GET"));
});

test("summary is real, partial reads disable completion, and finish persists before home", async () => {
  const h = harness(profile(4)); h.admin(); h.mounts = [{ kind: "local", status: "mounted" }, { kind: "smb", status: "unmounted" }]; h.nodes = [{ status: "offline", authorized: false }];
  h.override = (_, route) => { if (route === "/nodes") throw new Error("test offline"); };
  await h.run("bootstrapExperience()"); await h.run("experienceRefreshSummary()");
  assert.equal(h.run("XP.summary"), null); assert.equal(await h.run("experiencePersistStep(4,true)"), false);
  h.override = null; await h.run("experienceRefreshSummary()");
  assert.match(h.run("experienceSummaryContent()"), /2 个 · 本地 1 \/ NAS 1/);
  assert.match(h.run("experienceSummaryContent()"), /1 台 · 在线 0 \/ 已授权 0/);
  assert.equal(await h.run("experiencePersistStep(4,true)"), true);
  assert.equal(h.profile.onboarding_complete, true); assert.equal(h.run("S.view"), "overview");
  assert.equal(h.elements.get("#onboarding-root").hidden, true);
});

test("logout invalidates in-flight progress without falsely completing or navigating", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()");
  const pending = deferred(); h.override = (method, route) => method === "PATCH" && route === "/settings/onboarding" ? pending.promise : undefined;
  const work = h.run("experiencePersistStep(4)");
  assert.equal(h.run("!!XP.busy"), true);
  await h.run("settingsLogout()");
  pending.resolve({}); assert.equal(await work, false);
  assert.equal(h.state().onboarding_step, 3); assert.equal(h.run("experienceStep()"), 1); assert.equal(h.run("XP.busy"), null);
});

test("logout failure keeps local controls locked, never claims server logout", async () => {
  const h = harness(profile(2)); h.admin(); await h.run("bootstrapExperience()");
  h.override = (_, route) => { if (route === "/settings/logout") throw new Error("test disconnect"); };
  await h.run("settingsLogout()");
  assert.equal(h.run("settingsAdmin()"), false); assert.equal(h.run("SETTINGS.authBusy"), false);
  assert.match(h.run("SETTINGS.sessionError"), /无法确认服务端会话已退出/);
  assert.ok(!h.actions.some(a => a[1] === "管理员已锁定"));
});

test("late bootstrap and mutation responses do not steal a newer route", async () => {
  const h = harness(profile(2)); h.admin(); const pending = deferred();
  h.override = (_, route) => route === "/settings/profile" ? pending.promise : undefined;
  const boot = h.run("bootstrapExperience()"); h.run("setView('audit')"); pending.resolve(profile(2)); await boot;
  assert.equal(h.run("S.view"), "audit");
  h.override = null; await h.run("experienceResume()");
  const save = deferred(); h.override = (method, route) => method === "PATCH" && route === "/settings/onboarding" ? save.promise : undefined;
  const work = h.run("experiencePersistStep(3)"); h.run("setView('library')"); h.profile.onboarding_step = 3; save.resolve({}); await work;
  assert.equal(h.run("S.view"), "library"); assert.equal(h.state().onboarding_step, 3);
});

test("concurrent next clicks serialize, disable controls and release after failure", async () => {
  const h = harness(profile(2)); h.admin(); await h.run("bootstrapExperience()"); const pending = deferred();
  h.override = (method, route) => method === "PATCH" && route === "/settings/onboarding" ? pending.promise : undefined;
  const work = h.run("experiencePersistStep(3)");
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  assert.equal(h.calls.filter(c => c.method === "PATCH").length, 1);
  const next = h.elements.get("#onboarding-root").children.find(el => el.dataset.act === "xp-step" && el.dataset.step === "3");
  assert.equal(next.disabled, true);
  pending.reject(new Error("test failure")); await work;
  assert.equal(h.run("XP.busy"), null); assert.equal(next.disabled, false);
});

test("bootstrap read failure exposes retry without creating an admin or completing", async () => {
  const h = harness(); h.override = (_, route) => { if (route === "/settings/profile") throw new Error("test unavailable"); };
  await h.run("bootstrapExperience()");
  assert.equal(h.run("XP.profile"), null); assert.equal(h.run("XP.loading"), false);
  assert.match(h.elements.get("#onboarding-root").innerHTML, /重新检查引导状态/);
  assert.ok(h.calls.every(c => c.method === "GET"));
});

test("collapsing settings preserves typed onboarding fields and keyboard focus", async () => {
  const h = harness(); await h.run("bootstrapExperience()");
  const name = h.input("onboarding-name", "尚未提交的姓名");
  const toggle = h.elements.get("#nav").children.find(el => el.dataset.act === "xp-settings-toggle");
  toggle.focus(); h.run("ACT['xp-settings-toggle']()");
  assert.equal(h.elements.get("#onboarding-name"), name);
  assert.equal(name.value, "尚未提交的姓名");
  assert.equal(h.document.activeElement.dataset.act, "xp-settings-toggle");
});

test("selecting the current ordinary mobile route still closes the drawer", () => {
  const h = harness(); h.mobile = true; h.run("experienceInit(); toggleExperienceNav(true)");
  const current = h.elements.get("#nav").children.find(el => el.dataset.act === "nav");
  h.listeners.get("click").at(-1)({ target: current });
  assert.equal(h.run("XP.mobileOpen"), false);
  assert.equal(h.elements.get("#app > .main").inert, false);
});

test("late pre-logout session response cannot unlock controls again", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()");
  const pending = deferred();
  h.override = (_, route) => route === "/settings/session" ? pending.promise : undefined;
  const refresh = h.run("settingsRefreshSession()");
  h.override = null; await h.run("settingsLogout()");
  pending.resolve({ configured: true, authenticated: true, secure_context: true, can_setup: false }); await refresh;
  assert.equal(h.run("settingsAdmin()"), false); assert.equal(h.run("experienceStep()"), 1);
});

test("names follow the backend 40 Unicode character limit and render as escaped text", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()");
  h.input("settings-display-name", "界".repeat(41));
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), false);
  h.input("settings-display-name", "invalid\u0000name");
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), false);
  const name = '<img src=x onerror="bad">';
  h.input("settings-display-name", name);
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), true);
  assert.doesNotMatch(h.run("experienceProfileContent()"), /<img/);
  assert.match(h.run("experienceProfileContent()"), /&lt;img/);
});

test("invalid complete-at-step-two profile is rejected instead of hiding onboarding", async () => {
  const h = harness({ ...profile(2), onboarding_complete: true }); h.admin();
  await h.run("bootstrapExperience()");
  assert.equal(h.run("XP.profile"), null);
  assert.match(h.elements.get("#onboarding-root").innerHTML, /不一致/);
});

test("assets order keeps experience between settings and terminal-center", () => {
  const html = fs.readFileSync(path.join(assets, "index.html"), "utf8");
  assert.ok(html.indexOf('src="/settings.js"') < html.indexOf('src="/experience.js"'));
  assert.ok(html.indexOf('src="/experience.js"') < html.indexOf('src="/terminal-center.js"'));
  assert.match(html, /id="onboarding-root"/); assert.match(html, /aria-controls="sidebar" aria-expanded="false"/);
});
