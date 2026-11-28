"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const assets = path.join(__dirname, "assets");
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };

// Small DOM mock: no browser, real network, production service or credentials.
function harness(saved, { storage = new Map(), storageFailures = {} } = {}) {
  const elements = new Map(), listeners = new Map();
  const h = { calls: [], events: [], actions: [], mounts: [], nodes: [], license: { effective_seats: 3 }, mobile: false,
    storage, storageFailures, storageCalls: [], defaultLanguage: "zh-CN",
    profile: saved ? { ...saved } : { display_name: "", onboarding_step: 1, onboarding_complete: false, legacy: false },
    session: { configured: false, authenticated: false, secure_context: true, can_setup: true } };
  const localStorage = {};
  for (const method of ["getItem", "setItem", "removeItem"]) localStorage[method] = (key, value) => {
    h.storageCalls.push({ method, key, ...(method === "setItem" ? { value: String(value) } : {}) });
    if (h.storageFailures[method]) throw new Error("mock localStorage " + method + " unavailable");
    if (method === "getItem") return h.storage.get(key) ?? null;
    if (method === "setItem") h.storage.set(key, String(value));
    else if (method === "removeItem") h.storage.delete(key);
  };
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
    getAttribute(k) { return this.attrs[k] ?? null; }
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
  const header = new Element("header");
  header.innerHTML = fs.readFileSync(path.join(assets, "index.html"), "utf8").match(/<header class="topbar">([\s\S]*?)<\/header>/)[1];
  elements.get("#xp-account-label").textContent = "管理员";
  const main = new Element(); elements.set("#app > .main", main);
  const context = {
    console, URL, URLSearchParams, AbortController, document, localStorage,
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
        if (route === "/settings/language") return { default_language: h.defaultLanguage };
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
      if (method === "PATCH" && route === "/settings/language") { assert.ok(h.session.authenticated); h.defaultLanguage = body.default_language; return { default_language: h.defaultLanguage }; }
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
  for (const file of ["i18n.js", "i18n-app.js", "i18n-settings.js", "i18n-errors.js", "app.js", "settings.js", "experience.js"]) vm.runInContext(fs.readFileSync(path.join(assets, file), "utf8"), context, { filename: file });
  vm.runInContext("req=(method, route, opts)=>__api(method, route, opts?.body ? JSON.parse(opts.body) : undefined); toast=__toast; setView=__view; openWizard=__wizard; if(!NAV.some(n=>n.id==='settings')) NAV.push({id:'settings',name:'设置',icon:'settings'}); renderSidebarNav();", context);
  h.run = code => vm.runInContext(code, context); h.elements = elements; h.listeners = listeners; h.document = document;
  h.input = (id, value) => { const el = elements.get("#" + id) || new Element("input", { id }); el.value = value; return el; };
  h.admin = () => { h.session.configured = h.session.authenticated = true; };
  h.state = () => JSON.parse(h.run("JSON.stringify(XP.profile)"));
  return h;
}
const profile = step => ({ display_name: "测试管理员", onboarding_step: step, onboarding_complete: false, legacy: false });
const dismissedKey = "zephyr-onboarding-dismissed";
const onboarding = h => h.elements.get("#onboarding-root");
const onboardingButton = (h, action) => onboarding(h).children.find(el => el.dataset.act === action);
const writes = h => h.calls.filter(c => c.method !== "GET");
function assertDismissed(h) {
  assert.equal(h.run("XP.dismissed"), true);
  assert.equal(h.run("XP.active"), false);
  assert.equal(onboarding(h).hidden, true);
  assert.equal(onboarding(h).innerHTML, "");
}
function assertThreeSteps(html, done = false) {
  const progress = html.match(/<ol class="xp-progress"[^>]*>([\s\S]*?)<\/ol>/);
  assert.ok(progress, "three-step progress must remain visible");
  assert.equal((progress[1].match(/<li\b/g) || []).length, 3);
  for (const [i, label] of ["管理员与安全", "添加音乐源", "连接播放终端"].entries()) assert.ok(progress[1].includes(`<span>${i + 1}</span>${label}`));
  assert.doesNotMatch(html, /<span>4<\/span>|第\s*4\s*步|4\.\s*完成/);
  if (done) {
    assert.equal((progress[1].match(/class="done"/g) || []).length, 3);
    assert.doesNotMatch(progress[1], /aria-current="step"/);
  }
}

test("navigation and page headings use home and operation log labels without changing routes", () => {
  const h = harness();
  assert.equal(h.run("NAV.find(n => n.id === 'overview').name"), "主页");
  assert.equal(h.run("NAV.some(n => n.id === 'audit')"), false);
  assert.match(h.run("shellOverview()"), /id="ov-greeting">主页<\/h1>/);
  assert.match(h.run("shellAudit()"), /操作日志/);
  assert.doesNotMatch(h.elements.get("#nav").innerHTML, /听歌首页|审计/);
  assert.doesNotMatch(h.elements.get("#nav").innerHTML, /data-view="audit"/);
});

test("settings is one pinned nav item and renders every section on a single page", () => {
  const h = harness();
  assert.equal(h.run("SETTINGS_SECTIONS[0][0]"), "profile");
  const html = h.elements.get("#nav").innerHTML;
  assert.match(html, /class="nav-bottom"/);
  assert.ok(html.indexOf('data-act="settings-open"') > html.indexOf('data-tab="playlists"'));
  assert.doesNotMatch(html, /sidebar-settings/);
  h.run("openSettings('folders')");
  const after = h.elements.get("#nav").innerHTML;
  assert.match(after, /class="nav-item active" data-act="settings-open"/);
  const shell = h.run("shellSettings()");
  assert.match(shell, /settings-anchors/);
  for (const id of ["profile", "authorization", "folders", "packages", "maintenance", "about"]) assert.ok(shell.includes("settings-block-" + id), id);
  assert.equal(h.run("SETTINGS_SECTIONS.at(-1)[0]"), "about");
  assert.doesNotMatch(after, /data-section="(?:accounts|protocols)"|音乐服务账号|协议说明/);
  h.run("openSettings('about')");
  assert.equal(h.run("SETTINGS.section"), "about");
  assert.match(h.run("shellSettings()"), /Zephyr HiFi Kit/);
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
  const h = harness(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  for (const values of [["", "test-password-only", "test-password-only"], ["测试", "short", "short"], ["测试", "test-password-only", "different"]]) {
    h.input("onboarding-name", values[0]); h.input("onboarding-password", values[1]); h.input("onboarding-password-confirm", values[2]);
    assert.equal(await h.run("ACT['xp-auth']()"), false);
  }
  assert.ok(h.calls.every(c => c.method === "GET"));
  h.input("onboarding-name", "测试管理员"); h.input("onboarding-password", "test-password-only"); h.input("onboarding-password-confirm", "test-password-only");
  assert.equal(await h.run("ACT['xp-auth']()"), true);
  assert.equal(h.state().onboarding_step, 2); assert.equal(h.run("S.view"), "settings"); assert.equal(h.run("SETTINGS.section"), "folders");
  assert.equal(h.elements.get("#xp-account-label").textContent, "测试管理员");
  const request = h.calls.find(c => c.route === "/settings/setup");
  assert.deepEqual(request.body, { display_name: "测试管理员", password: "test-password-only" });
  assert.equal(h.run("SETTINGS.authBusy"), false);
});

test("settings auth also includes setup name/confirmation but legacy login only sends password", async () => {
  const h = harness(); h.run("openSettings('profile')"); await h.run("settingsRefreshSession()");
  assert.match(h.elements.get("#settings-admin").innerHTML, /settings-name/);
  assert.match(h.elements.get("#settings-admin").innerHTML, /settings-password-confirm/);
  h.session.configured = true; h.profile = { ...profile(3), legacy: true, onboarding_complete: true };
  await h.run("settingsRefreshSession()");
  assert.doesNotMatch(h.elements.get("#settings-admin").innerHTML, /settings-name|settings-password-confirm/);
  h.input("settings-password", "test-password-only"); await h.run("settingsAuthenticate()");
  assert.deepEqual(h.calls.find(c => c.route === "/settings/login").body, { password: "test-password-only" });
  assert.ok(!h.calls.some(c => c.route === "/settings/setup"));
});

test("insecure remote HTTP and non-local first setup never submit credentials", async () => {
  for (const session of [{ secure_context: false, can_setup: false }, { secure_context: true, can_setup: false }]) {
    const h = harness(); Object.assign(h.session, session); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
    assert.doesNotMatch(h.elements.get("#onboarding-root").innerHTML, /id="onboarding-password"/);
    h.input("onboarding-name", "测试"); h.input("onboarding-password", "test-password-only"); h.input("onboarding-password-confirm", "test-password-only");
    assert.equal(await h.run("ACT['xp-auth']()"), false);
    assert.ok(h.calls.every(c => c.method === "GET"));
  }
});

test("refresh offers resume to saved step and step 2 embeds actual folders shell and actions", async () => {
  const h = harness(profile(2)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  assert.equal(h.run("SETTINGS.section"), "folders");
  const html = h.elements.get("#view").innerHTML;
  assert.match(html, /settings-local-add/); assert.match(html, /settings-nas-add/); assert.match(html, /settings-sources/);
  assert.equal(await h.run("experiencePersistStep(3)"), true);
  assert.equal(h.profile.onboarding_step, 3); assert.equal(h.run("S.view"), "settings");
  const restored = harness(h.profile); restored.admin(); await restored.run("bootstrapExperience()");
  assert.equal(restored.run("XP.stage"), "welcome");
  assert.match(restored.elements.get("#onboarding-root").innerHTML, /继续配置/);
  assert.ok(!restored.actions.some(a => a[0] === "view"));
  await restored.run("ACT['xp-resume']()");
  assert.equal(restored.run("S.view"), "settings");
  restored.run("ACT['xp-add-node']()"); assert.ok(restored.actions.some(a => a[0] === "wizard"));
  assert.ok(!restored.calls.some(c => c.method !== "GET"));
  restored.run("ACT['xp-authorization']()"); assert.equal(restored.run("SETTINGS.section"), "authorization");
  await restored.run("ACT['xp-resume']()"); assert.equal(restored.run("S.view"), "settings");
});

test("configured incomplete instance logs in and resumes without forced password setup", async () => {
  const h = harness(profile(3)); h.session.configured = true; await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  assert.equal(h.run("experienceStep()"), 1);
  assert.match(h.elements.get("#onboarding-root").innerHTML, /已有管理员密码/);
  h.input("onboarding-password", "test-password-only"); await h.run("ACT['xp-auth']()");
  assert.equal(h.run("experienceStep()"), 3); assert.equal(h.run("S.view"), "settings");
  assert.ok(!h.calls.some(c => c.route === "/settings/setup"));
});

test("legacy complete instance prompts for name but never forces setup or onboarding", async () => {
  const h = harness({ ...profile(3), display_name: "", legacy: true, onboarding_complete: true }); h.session.configured = true;
  await h.run("bootstrapExperience()");
  assert.match(h.elements.get("#onboarding-root").innerHTML, /补充一个称呼/);
  assert.equal(h.run("XP.active"), false);
  assert.ok(!h.actions.some(a => a[0] === "view"));
  assert.ok(h.calls.every(c => c.method === "GET"));
});

test("account label falls back when no name is saved and loads a saved name while locked", async () => {
  for (const display_name of ["", "   ", "测试称呼"]) {
    const h = harness({ ...profile(1), display_name });
    assert.equal(h.elements.get("#xp-account-label").textContent, "管理员");
    await h.run("bootstrapExperience()");
    assert.equal(h.elements.get("#xp-account-label").textContent, display_name.trim() || "管理员");
    const button = h.elements.get("#xp-account");
    assert.equal(button.attrs["aria-label"], display_name.trim() ? "测试称呼 · 管理员与偏好" : "管理员与偏好");
    assert.equal(button.attrs.title, display_name.trim() || "管理员与偏好");
    assert.equal(h.run("settingsAdmin()"), false);
    h.run("ACT['settings-open']({section:'profile'})");
    assert.equal(h.run("S.view"), "settings");
    assert.equal(h.run("SETTINGS.section"), "profile");
    assert.equal(button.dataset.act, "settings-open");
    assert.equal(button.dataset.section, "profile");
  }
});

test("profile refresh updates the account label without duplicating unchanged profile events", async () => {
  const h = harness(profile(3)); await h.run("bootstrapExperience()");
  h.profile.display_name = "刷新称呼";
  await h.run("refreshExperienceProfile()");
  assert.equal(h.elements.get("#xp-account-label").textContent, "刷新称呼");
  const count = h.events.filter(e => e.type === "profile-updated").length;
  await h.run("refreshExperienceProfile()");
  assert.equal(h.events.filter(e => e.type === "profile-updated").length, count);
});

test("failed or unconfirmed name saves leave the account label unchanged", async () => {
  for (const failure of ["write", "read", "unconfirmed"]) {
    const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()");
    h.input("settings-display-name", "未确认称呼");
    h.override = (method, route) => {
      if (route !== "/settings/profile") return;
      if ((failure === "write" && method === "PATCH") || (failure === "read" && method === "GET")) throw new Error("test failure");
      if (failure === "unconfirmed" && method === "GET") return profile(3);
    };
    assert.equal(await h.run("experienceSaveName('settings-display-name')"), false);
    assert.equal(h.elements.get("#xp-account-label").textContent, "测试管理员");
    assert.equal(h.elements.get("#xp-account").attrs.title, "测试管理员");
  }
});

test("a late profile refresh cannot overwrite the newly saved account label", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()");
  const pending = deferred();
  h.override = (method, route) => method === "GET" && route === "/settings/profile" ? pending.promise : undefined;
  const refresh = h.run("refreshExperienceProfile()");
  h.override = null; h.input("settings-display-name", "新称呼");
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), true);
  pending.resolve(profile(3)); await refresh;
  assert.equal(h.elements.get("#xp-account-label").textContent, "新称呼");
  assert.equal(h.elements.get("#xp-account").attrs["aria-label"], "新称呼 · 管理员与偏好");
});

test("name save is authenticated, emits profile-updated and preserves progress", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()");
  h.input("settings-display-name", " 新称呼 ");
  assert.equal(h.elements.get("#xp-account-label").textContent, "测试管理员");
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), true);
  assert.equal(h.elements.get("#xp-account-label").textContent, "新称呼");
  assert.equal(h.elements.get("#xp-account").attrs.title, "新称呼");
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
  const h = harness(profile(2)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  h.override = (method, route) => { if (method === "PATCH" && route === "/settings/onboarding") throw new Error("test write failed"); };
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  assert.equal(h.state().onboarding_step, 2); assert.equal(h.run("XP.busy"), null);
  h.override = (method, route) => method === "PATCH" && route === "/settings/onboarding" ? {} : undefined;
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  assert.equal(h.state().onboarding_step, 2);
  assert.equal(await h.run("experiencePersistStep(3, true)"), false);
  assert.equal(h.state().onboarding_complete, false);
});

test("all created-admin progress changes require auth and nonempty name", async () => {
  const h = harness(profile(2)); h.session.configured = true; await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  h.admin(); await h.run("settingsRefreshSession()"); h.run("XP.profile.display_name=' '");
  assert.equal(await h.run("experiencePersistStep(3)"), false);
  assert.ok(h.calls.every(c => c.method === "GET"));
});

test("summary is real and non-blocking, and finish persists before home", async () => {
  const h = harness(profile(3)); h.admin(); h.mounts = [{ kind: "local", status: "mounted" }, { kind: "smb", status: "unmounted" }]; h.nodes = [{ status: "offline", authorized: false }];
  h.override = (_, route) => { if (route === "/nodes") throw new Error("test offline"); };
  await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  assert.equal(h.run("XP.summary"), null);
  const review = h.elements.get("#onboarding-root").children.find(el => el.dataset.act === "xp-review");
  assert.equal(review.disabled, false);
  h.override = null; await h.run("experienceRefreshSummary()");
  assert.match(h.run("experienceSummaryContent()"), /2 个 · 本地 1 \/ NAS 1/);
  assert.match(h.run("experienceSummaryContent()"), /1 台 · 在线 0 \/ 已授权 0/);
  assert.equal(h.run("ACT['xp-review']()"), true);
  assert.equal(await h.run("ACT['xp-finish']()"), true);
  assert.equal(h.profile.onboarding_complete, true); assert.equal(h.run("S.view"), "overview");
  assert.equal(h.elements.get("#onboarding-root").hidden, true);
});

test("finish completes at step 3 without waiting for a readable summary", async () => {
  const h = harness(profile(3)); h.admin();
  h.override = (_, route) => { if (route === "/nodes") throw new Error("test offline"); };
  await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  assert.equal(h.run("XP.summary"), null);
  assert.equal(h.run("ACT['xp-review']()"), true);
  assert.equal(h.elements.get("#onboarding-root").children.find(el => el.dataset.act === "xp-finish").disabled, false);
  assert.equal(await h.run("ACT['xp-finish']()"), true);
  assert.equal(h.profile.onboarding_step, 3); assert.equal(h.profile.onboarding_complete, true);
  assert.equal(h.run("S.view"), "overview");
});

test("logout invalidates in-flight progress without falsely completing or navigating", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  const pending = deferred(); h.override = (method, route) => method === "PATCH" && route === "/settings/onboarding" ? pending.promise : undefined;
  const work = h.run("experiencePersistStep(3)");
  assert.equal(h.run("!!XP.busy"), true);
  await h.run("settingsLogout()");
  pending.resolve({}); assert.equal(await work, false);
  assert.equal(h.state().onboarding_step, 3); assert.equal(h.run("experienceStep()"), 1); assert.equal(h.run("XP.busy"), null);
});

test("logout failure keeps local controls locked, never claims server logout", async () => {
  const h = harness(profile(2)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
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
  h.override = null; await h.run("ACT['xp-resume']()");
  const save = deferred(); h.override = (method, route) => method === "PATCH" && route === "/settings/onboarding" ? save.promise : undefined;
  const work = h.run("experiencePersistStep(3)"); h.run("setView('library')"); h.profile.onboarding_step = 3; save.resolve({}); await work;
  assert.equal(h.run("S.view"), "library"); assert.equal(h.state().onboarding_step, 3);
});

test("concurrent next clicks serialize, disable controls and release after failure", async () => {
  const h = harness(profile(2)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()"); const pending = deferred();
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

test("collapsing playlists preserves typed onboarding fields and keyboard focus", async () => {
  const h = harness(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  const name = h.input("onboarding-name", "尚未提交的姓名");
  const toggle = h.elements.get("#nav").children.find(el => el.dataset.act === "xp-playlists-toggle");
  toggle.focus(); h.run("ACT['xp-playlists-toggle']()");
  assert.equal(h.elements.get("#onboarding-name"), name);
  assert.equal(name.value, "尚未提交的姓名");
  assert.equal(h.document.activeElement.dataset.act, "xp-playlists-toggle");
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
  const label = h.elements.get("#xp-account-label");
  Object.defineProperty(label, "innerHTML", { set() { assert.fail("account name must be inserted as text"); } });
  const name = '<img src=x onerror="bad">';
  h.input("settings-display-name", name);
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), true);
  assert.doesNotMatch(h.run("experienceProfileContent()"), /<img/);
  assert.match(h.run("experienceProfileContent()"), /&lt;img/);
  assert.equal(label.textContent, name);
  assert.equal(h.elements.get("#xp-account").attrs.title, name);
  const longName = "𠮷".repeat(40);
  h.input("settings-display-name", longName);
  assert.equal(await h.run("experienceSaveName('settings-display-name')"), true);
  assert.equal(label.textContent, longName);
  assert.equal(h.elements.get("#xp-account").attrs.title, longName);
});

test("invalid complete-at-step-two profile is rejected instead of hiding onboarding", async () => {
  const h = harness({ ...profile(2), onboarding_complete: true }); h.admin();
  await h.run("bootstrapExperience()");
  assert.equal(h.run("XP.profile"), null);
  assert.match(h.elements.get("#onboarding-root").innerHTML, /不一致/);
});

test("first bootstrap shows only welcome and starts the password form on explicit resume", async () => {
  const h = harness();
  assert.equal(h.run("XP.stage"), "welcome");
  await h.run("bootstrapExperience()");
  const html = onboarding(h).innerHTML;
  assert.equal(onboarding(h).hidden, false);
  assert.match(html, /欢迎使用 Zephyr HiFi Kit/);
  assert.match(html, /只需简单3步完成配置就可开始体验/);
  assert.match(html, /开始配置/);
  assert.match(html, /跳过，我自己配置/);
  assert.doesNotMatch(html, /<form\b|<input\b|xp-auth|xp-review|xp-finish|继续配置/);
  assertThreeSteps(html);
  assert.ok(!h.actions.some(a => a[0] === "view"));
  assert.deepEqual(h.calls.map(c => [c.method, c.route]).sort(), [["GET", "/settings/language"], ["GET", "/settings/profile"], ["GET", "/settings/session"]]);
  assert.deepEqual(h.storageCalls.filter(c => c.key === dismissedKey), [{ method: "getItem", key: dismissedKey }]);
  await h.run("ACT['xp-resume']()");
  assert.equal(h.run("XP.stage"), "steps");
  assert.equal(h.run("experienceStep()"), 1);
  assert.match(onboarding(h).innerHTML, /1\. 管理员与安全/);
  assert.match(onboarding(h).innerHTML, /data-experience-form="xp-auth"/);
  assert.ok(h.elements.has("#onboarding-name"));
  assert.ok(h.elements.has("#onboarding-password"));
  assert.ok(h.elements.has("#onboarding-password-confirm"));
  assert.equal(writes(h).length, 0);
});

test("saved progress remains on welcome without route or summary fetch until resume", async t => {
  for (const step of [2, 3]) await t.test(`saved step ${step}`, async () => {
    const h = harness(profile(step)); h.admin(); h.run("setView('audit')");
    h.actions.length = 0;
    await h.run("bootstrapExperience()");
    assert.equal(h.run("XP.stage"), "welcome");
    assert.equal(h.run("S.view"), "audit");
    assert.match(onboarding(h).innerHTML, new RegExp(`已保存到第 ${step} 步`));
    assert.match(onboarding(h).innerHTML, /继续配置不会清除已有设置/);
    assert.doesNotMatch(onboarding(h).innerHTML, /<form\b|<input\b|xp-review|xp-finish/);
    assert.ok(!h.calls.some(c => ["/mounts", "/nodes", "/license"].includes(c.route)));
    assert.ok(!h.actions.some(a => a[0] === "view"));
    await h.run("ACT['xp-resume']()");
    assert.equal(h.run("XP.stage"), "steps");
    assert.equal(h.run("experienceStep()"), step);
    assert.equal(h.run("S.view"), "settings");
    if (step === 2) assert.equal(h.run("SETTINGS.section"), "folders");
    assert.deepEqual(h.state(), profile(step));
    assert.equal(writes(h).length, 0);
  });
});

test("skip at welcome, every step and completion is local only, survives refresh and resumes progress", async t => {
  const cases = [
    { label: "fresh welcome", step: 1, stage: "welcome", authenticated: false },
    { label: "saved welcome", step: 3, stage: "welcome", authenticated: true },
    { label: "first setup", step: 1, stage: "steps", authenticated: false },
    { label: "locked saved progress", step: 3, stage: "steps", authenticated: false },
    { label: "identity", step: 1, stage: "steps", authenticated: true },
    { label: "sources", step: 2, stage: "steps", authenticated: true },
    { label: "terminals", step: 3, stage: "steps", authenticated: true },
    { label: "completion", step: 3, stage: "complete", authenticated: true }
  ];
  for (const { label, step, stage, authenticated } of cases) await t.test(label, async () => {
    const storage = new Map([["unrelated-preference", "preserve"]]);
    const h = harness(step === 1 && !authenticated ? undefined : profile(step), { storage });
    if (authenticated) h.admin(); else h.session.configured = step > 1;
    await h.run("bootstrapExperience()");
    if (stage !== "welcome") await h.run("ACT['xp-resume']()");
    if (stage === "complete") assert.equal(h.run("ACT['xp-review']()"), true);
    const saved = h.state(), server = { ...h.profile }, session = h.run("JSON.stringify(SETTINGS.session)");
    const serverSession = { ...h.session }, events = h.events.length, callCount = h.calls.length;
    const storageStart = h.storageCalls.length;
    assert.equal(onboardingButton(h, "xp-skip").disabled, false);
    assert.equal(h.run("ACT['xp-skip']()"), true);
    assertDismissed(h);
    assert.equal(h.run("S.view"), "overview");
    assert.equal(h.run("XP.stage"), "welcome");
    assert.deepEqual(h.state(), saved);
    assert.deepEqual(h.profile, server);
    assert.equal(h.run("JSON.stringify(SETTINGS.session)"), session);
    assert.deepEqual(h.session, serverSession);
    assert.equal(h.run("settingsAdmin()"), authenticated);
    assert.equal(h.calls.length, callCount, "skip must not send any server request");
    assert.equal(h.events.length, events, "skip must not announce a changed profile");
    assert.deepEqual(h.storageCalls.slice(storageStart), [{ method: "setItem", key: dismissedKey, value: "1" }]);
    assert.equal(storage.get("unrelated-preference"), "preserve");
    assert.equal(writes(h).length, 0);

    const restored = harness(h.profile, { storage }); Object.assign(restored.session, h.session);
    await restored.run("bootstrapExperience()");
    assertDismissed(restored);
    assert.deepEqual(restored.state(), saved);
    assert.ok(!restored.actions.some(a => a[0] === "view"));
    assert.deepEqual(restored.storageCalls.filter(c => c.key === dismissedKey), [{ method: "getItem", key: dismissedKey }]);
    restored.run("ACT['settings-open']({section:'profile'})");
    assert.match(restored.elements.get("#view").innerHTML, /继续使用引导/);
    await restored.run("ACT['xp-resume']()");
    assert.equal(restored.run("XP.dismissed"), false);
    assert.equal(restored.run("XP.active"), true);
    assert.equal(restored.run("XP.stage"), "steps");
    assert.equal(onboarding(restored).hidden, false);
    assert.equal(restored.run("experienceStep()"), authenticated ? step : 1);
    assert.deepEqual(restored.state(), saved);
    assert.equal(restored.run("settingsAdmin()"), authenticated);
    assert.equal(storage.has(dismissedKey), false);
    assert.equal(storage.get("unrelated-preference"), "preserve");
    assert.deepEqual(restored.storageCalls.at(-1), { method: "removeItem", key: dismissedKey });
    assert.equal(writes(restored).length, 0);
  });
});

test("only the exact dismissed preference is recognized and init reads it once", async t => {
  for (const value of [null, "0", "true", "1"]) await t.test(String(value), async () => {
    const storage = new Map(value === null ? [] : [[dismissedKey, value]]);
    const h = harness(profile(2), { storage }); h.admin();
    await h.run("bootstrapExperience()"); await h.run("bootstrapExperience()"); h.run("experienceInit()");
    assert.equal(h.run("XP.dismissed"), value === "1");
    assert.equal(onboarding(h).hidden, value === "1");
    assert.equal(h.storageCalls.filter(c => c.method === "getItem" && c.key === dismissedKey).length, 1);
    assert.equal(writes(h).length, 0);
  });
});

test("manual settings setup or login after skip never reopens onboarding", async t => {
  for (const setup of [true, false]) await t.test(setup ? "setup" : "login", async () => {
    const h = harness(setup ? undefined : profile(3)); h.session.configured = !setup;
    await h.run("bootstrapExperience()");
    assert.equal(h.run("ACT['xp-skip']()"), true);
    h.run("ACT['settings-open']({section:'profile'})"); await h.run("settingsRefreshSession()");
    if (setup) { h.input("settings-name", "手动设置称呼"); h.input("settings-password-confirm", "test-password-only"); }
    h.input("settings-password", "test-password-only");
    const actionStart = h.actions.length;
    assert.equal(await h.run("settingsAuthenticate()"), true);
    assertDismissed(h);
    assert.equal(h.run("S.view"), "settings");
    assert.equal(h.run("SETTINGS.section"), "profile");
    assert.ok(!h.actions.slice(actionStart).some(a => a[0] === "view"));
    assert.equal(h.state().onboarding_complete, false);
    assert.equal(h.state().onboarding_step, setup ? 2 : 3);
    assert.equal(h.storage.get(dismissedKey), "1");
    assert.deepEqual(writes(h).map(c => [c.method, c.route]), [["POST", setup ? "/settings/setup" : "/settings/login"]]);
    await h.run("ACT['xp-resume']()");
    assert.equal(h.run("experienceStep()"), setup ? 2 : 3);
    assert.equal(h.storage.has(dismissedKey), false);
  });
});

test("skip refuses while a progress write, authentication or bootstrap is busy", async t => {
  for (const operation of ["progress", "authentication", "bootstrap"]) await t.test(operation, async () => {
    const h = harness(profile(2));
    if (operation !== "authentication") h.admin(); else h.session.configured = true;
    await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
    const pending = deferred();
    const route = operation === "progress" ? "/settings/onboarding" : operation === "authentication" ? "/settings/login" : "/settings/profile";
    h.override = (_, requestRoute) => requestRoute === route ? pending.promise : undefined;
    if (operation === "authentication") h.input("onboarding-password", "test-password-only");
    const work = h.run(operation === "progress" ? "ACT['xp-step']({step:'3'})" : operation === "authentication" ? "ACT['xp-auth']()" : "bootstrapExperience()");
    assert.equal(h.run("!!XP.busy || SETTINGS.authBusy || XP.loading"), true);
    const saved = h.state(), session = h.run("JSON.stringify(SETTINGS.session)"), count = h.calls.length, storageCount = h.storageCalls.length, view = h.run("S.view");
    if (operation !== "bootstrap") assert.equal(onboardingButton(h, "xp-skip").disabled, true);
    assert.equal(h.run("ACT['xp-skip']()"), false);
    assert.deepEqual(h.state(), saved);
    assert.equal(h.run("JSON.stringify(SETTINGS.session)"), session);
    assert.equal(h.run("S.view"), view);
    assert.equal(h.run("XP.dismissed"), false);
    assert.equal(h.run("XP.active"), true);
    assert.equal(h.storageCalls.length, storageCount);
    assert.equal(h.calls.length, count);
    h.override = null; pending.reject(new Error("test pending operation failed")); await work;
    assert.equal(h.run("!!XP.busy || SETTINGS.authBusy || XP.loading"), false);
    assert.equal(h.run("ACT['xp-skip']()"), true);
    assertDismissed(h);
  });
});

test("late summary success, summary error and profile refresh cannot undo skip", async t => {
  for (const operation of ["summary-success", "summary-error", "profile"]) await t.test(operation, async () => {
    const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
    const pending = deferred();
    h.override = (_, route) => route === (operation === "profile" ? "/settings/profile" : "/nodes") ? pending.promise : undefined;
    const work = h.run(operation === "profile" ? "refreshExperienceProfile()" : "ACT['xp-summary-refresh']()");
    assert.equal(h.run("ACT['xp-skip']()"), true);
    const actionCount = h.actions.length, eventCount = h.events.length;
    if (operation === "summary-error") pending.reject(new Error("late summary failure"));
    else pending.resolve(operation === "profile" ? { ...profile(3), display_name: "过期资料" } : [{ status: "online", authorized: true }]);
    await work;
    assertDismissed(h);
    assert.equal(h.run("XP.summary"), null);
    assert.equal(h.run("XP.error"), "");
    assert.deepEqual(h.state(), profile(3));
    assert.equal(h.elements.get("#xp-account-label").textContent, "测试管理员");
    assert.equal(h.actions.length, actionCount);
    assert.equal(h.events.length, eventCount);
    assert.equal(h.run("S.view"), "overview");
    h.override = null;
    await h.run("refreshExperienceProfile()"); await h.run("bootstrapExperience()");
    const count = h.calls.length; await h.run("ACT['xp-summary-refresh']()");
    assert.equal(h.calls.length, count);
    assertDismissed(h);
    assert.equal(writes(h).length, 0);
  });
});

test("localStorage write failures keep skip and resume effective for this visit and warn", async t => {
  for (const method of ["setItem", "removeItem"]) await t.test(method, async () => {
    const storage = new Map(method === "removeItem" ? [[dismissedKey, "1"]] : []);
    const h = harness(profile(2), { storage, storageFailures: { [method]: true } }); h.admin();
    await h.run("bootstrapExperience()");
    if (method === "setItem") {
      await h.run("ACT['xp-resume']()");
      assert.equal(h.run("ACT['xp-skip']()"), true);
      assertDismissed(h);
      assert.equal(storage.has(dismissedKey), false);
      await h.run("bootstrapExperience()"); await h.run("experienceAuthenticated(false)");
      assertDismissed(h);
      assert.equal(h.run("S.view"), "overview");
    } else {
      assertDismissed(h);
      await h.run("ACT['xp-resume']()");
      assert.equal(h.run("XP.dismissed"), false);
      assert.equal(h.run("XP.active"), true);
      assert.equal(h.run("XP.stage"), "steps");
      assert.equal(h.run("experienceStep()"), 2);
      assert.equal(onboarding(h).hidden, false);
      assert.equal(storage.get(dismissedKey), "1");
    }
    assert.ok(h.actions.some(a => a[0] === "toast" && a[2] === "warn" && /浏览器.*记住.*本次操作仍有效/.test(a[1])));
    assert.deepEqual(h.state(), profile(2));
    assert.equal(writes(h).length, 0);
  });
});

test("localStorage read failure degrades to welcome, permits skipping and explains unavailable preferences", async () => {
  const h = harness(profile(2), { storage: new Map([[dismissedKey, "1"]]), storageFailures: { getItem: true } }); h.admin();
  await h.run("bootstrapExperience()");
  const initialWarnings = h.actions.filter(a => a[0] === "toast" && a[2] === "warn");
  assert.equal(h.run("XP.stage"), "welcome");
  assert.equal(h.run("XP.loading"), false);
  assert.match(onboarding(h).innerHTML, /继续配置/);
  assert.equal(h.run("ACT['xp-skip']()"), true);
  assertDismissed(h);
  await h.run("bootstrapExperience()"); assertDismissed(h);
  assert.deepEqual(h.state(), profile(2));
  assert.equal(writes(h).length, 0);
  assert.ok(initialWarnings.some(a => /浏览器.*(?:读取|记住)|(?:读取|记住).*引导/.test(a[1])), "failed preference reads must warn instead of silently forgetting the saved dismissal");
});

test("unavailable localStorage reads and writes still keep this visit dismissed", async () => {
  const h = harness(profile(3), { storageFailures: { getItem: true, setItem: true, removeItem: true } }); h.admin();
  await h.run("bootstrapExperience()");
  assert.equal(h.run("ACT['xp-skip']()"), true);
  await h.run("refreshExperienceProfile()"); await h.run("experienceAuthenticated(false)"); await h.run("bootstrapExperience()");
  assertDismissed(h);
  assert.ok(h.actions.some(a => a[0] === "toast" && a[2] === "warn" && /记住.*本次操作仍有效/.test(a[1])));
  await h.run("ACT['xp-resume']()");
  assert.equal(h.run("XP.dismissed"), false);
  assert.equal(h.run("experienceStep()"), 3);
  assert.deepEqual(h.state(), profile(3));
  assert.equal(writes(h).length, 0);
});

test("review is presentation only with exactly three done progress items and completion copy", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  assert.match(onboarding(h).innerHTML, /3\. 连接播放终端/);
  assert.equal(onboardingButton(h, "xp-review").disabled, false);
  assert.equal(onboardingButton(h, "xp-finish"), undefined);
  const count = h.calls.length, events = h.events.length, saved = h.state();
  assert.equal(h.run("ACT['xp-review']()"), true);
  assert.equal(h.run("XP.stage"), "complete");
  assert.equal(h.calls.length, count);
  assert.equal(h.events.length, events);
  assert.deepEqual(h.state(), saved);
  assert.deepEqual(h.profile, saved);
  assert.equal(h.run("S.view"), "settings");
  const html = onboarding(h).innerHTML;
  assertThreeSteps(html, true);
  assert.match(html, /如沐和风，音乐随您而行！/);
  assert.match(html, /配置引导已走完，随时可以在设置中继续调整。/);
  assert.match(html, /开始聆听/);
  assert.match(html, /点击开始聆听后结束引导并进入主页，不会自动播放音乐。/);
  assert.doesNotMatch(html, /<form\b|<input\b|data-act="xp-review"|data-act="xp-step"/);
  assert.equal(onboardingButton(h, "xp-finish").disabled, false);
  assert.equal(h.run("ACT['xp-review']()"), false);
  assert.equal(h.calls.length, count);
  assert.equal(writes(h).length, 0);
  await h.run("ACT['xp-resume']()");
  assert.equal(h.run("XP.stage"), "steps");
  assert.equal(h.run("experienceStep()"), 3);
  assert.ok(onboardingButton(h, "xp-review"));
  assert.equal(onboardingButton(h, "xp-finish"), undefined);
  assert.deepEqual(h.state(), saved);
  assert.equal(writes(h).length, 0);
});

test("review requires unlocked active step 3 and finish requires the complete presentation", async t => {
  const cases = [
    { label: "welcome", step: 3, prepare: "" },
    { label: "step 1", step: 1, prepare: "await ACT['xp-resume']()" },
    { label: "step 2", step: 2, prepare: "await ACT['xp-resume']()" },
    { label: "dismissed", step: 3, prepare: "await ACT['xp-resume'](); ACT['xp-skip']()" },
    { label: "locked", step: 3, prepare: "await ACT['xp-resume'](); SETTINGS.session.authenticated=false; experienceSessionChanged()" },
    { label: "mutation busy", step: 3, prepare: "await ACT['xp-resume'](); XP.busy={}" },
    { label: "auth busy", step: 3, prepare: "await ACT['xp-resume'](); SETTINGS.authBusy=true" }
  ];
  for (const { label, step, prepare } of cases) await t.test(label, async () => {
    const h = harness(profile(step)); h.admin(); await h.run("bootstrapExperience()");
    await h.run(`(async () => { ${prepare} })()`);
    const count = h.calls.length, saved = h.state(), stage = h.run("XP.stage");
    assert.equal(h.run("ACT['xp-review']()"), false);
    assert.equal(await h.run("ACT['xp-finish']()"), false);
    assert.equal(h.run("XP.stage"), stage);
    assert.deepEqual(h.state(), saved);
    assert.equal(h.calls.length, count);
    assert.equal(writes(h).length, 0);
  });
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  const count = h.calls.length;
  assert.equal(await h.run("ACT['xp-finish']()"), false, "step 3 is not itself a completion confirmation");
  assert.equal(h.calls.length, count);
  assert.equal(h.run("XP.stage"), "steps");
});

test("finish waits for PATCH and confirmed profile read, serializes clicks and only then navigates home", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()");
  assert.equal(h.run("ACT['xp-review']()"), true);
  const patch = deferred(), read = deferred(), readStarted = deferred();
  h.override = (method, route) => {
    if (method === "PATCH" && route === "/settings/onboarding") return patch.promise;
    if (method === "GET" && route === "/settings/profile") { readStarted.resolve(); return read.promise; }
  };
  const callStart = h.calls.length, actionStart = h.actions.length;
  const finish = h.run("ACT['xp-finish']()");
  assert.equal(h.run("!!XP.busy"), true);
  assert.equal(onboardingButton(h, "xp-finish").disabled, true);
  assert.equal(onboardingButton(h, "xp-resume").disabled, true);
  assert.equal(onboardingButton(h, "xp-skip").disabled, true);
  assert.equal(await h.run("ACT['xp-finish']()"), false);
  assert.equal(h.run("ACT['xp-skip']()"), false);
  assert.equal(await h.run("ACT['xp-resume']()"), false);
  assert.equal(writes(h).length, 1);
  assert.deepEqual(writes(h)[0], { method: "PATCH", route: "/settings/onboarding", body: { step: 3, complete: true } });
  assert.equal(h.run("S.view"), "settings");
  assert.equal(h.state().onboarding_complete, false);
  patch.resolve({}); await readStarted.promise;
  assert.equal(h.run("XP.stage"), "complete");
  assert.equal(h.state().onboarding_complete, false);
  assert.equal(onboarding(h).hidden, false);
  assert.ok(!h.actions.slice(actionStart).some(a => a[0] === "view"));
  assert.equal(await h.run("ACT['xp-finish']()"), false);
  h.profile.onboarding_complete = true; read.resolve({ ...h.profile });
  assert.equal(await finish, true);
  assert.equal(h.run("XP.busy"), null);
  assert.equal(h.state().onboarding_complete, true);
  assert.equal(h.run("S.view"), "overview");
  assert.equal(onboarding(h).hidden, true);
  assert.deepEqual(h.calls.slice(callStart).map(c => [c.method, c.route]), [["PATCH", "/settings/onboarding"], ["GET", "/settings/profile"]]);
  assert.deepEqual(h.actions.slice(actionStart).filter(a => a[0] === "view"), [["view", "overview"]]);
  const count = h.calls.length;
  assert.equal(await h.run("ACT['xp-finish']()"), false);
  assert.equal(h.calls.length, count);
  assert.equal(h.storage.has(dismissedKey), false, "completion is server progress, not a local dismissal");
});

test("finish failures and unconfirmed reads retain completion card and saved client progress", async t => {
  for (const failure of ["write", "read", "unconfirmed", "wrong-step"]) await t.test(failure, async () => {
    const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()"); h.run("ACT['xp-review']()");
    const actionStart = h.actions.length;
    h.override = (method, route) => {
      if (failure === "write" && method === "PATCH" && route === "/settings/onboarding") throw new Error("test completion write failure");
      if (method === "GET" && route === "/settings/profile") {
        if (failure === "read") throw new Error("test completion read failure");
        if (failure === "unconfirmed") return profile(3);
        if (failure === "wrong-step") return profile(2);
      }
    };
    assert.equal(await h.run("ACT['xp-finish']()"), false);
    assert.equal(h.run("XP.busy"), null);
    assert.equal(h.run("XP.stage"), "complete");
    assert.equal(h.run("XP.active"), true);
    assert.deepEqual(h.state(), profile(3));
    assert.equal(h.run("S.view"), "settings");
    assert.equal(onboarding(h).hidden, false);
    assert.match(onboarding(h).innerHTML, /如沐和风，音乐随您而行！/);
    assert.match(h.elements.get("#onboarding-error").innerHTML, /test completion|未确认进度保存/);
    assert.equal(onboardingButton(h, "xp-finish").disabled, false);
    assert.equal(writes(h).length, 1);
    assert.ok(!h.actions.slice(actionStart).some(a => a[0] === "view" || (a[0] === "toast" && a[2] === "ok")));
    h.override = null;
    assert.equal(await h.run("ACT['xp-finish']()"), true, "completion may be retried after failure");
    assert.equal(h.run("S.view"), "overview");
  });
});

test("refreshing an unsubmitted completion card preserves server step 3 and returns through welcome", async () => {
  const storage = new Map(), h = harness(profile(3), { storage }); h.admin();
  await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()"); h.run("ACT['xp-review']()");
  assert.deepEqual(h.profile, profile(3));
  assert.equal(writes(h).length, 0);
  const restored = harness(h.profile, { storage }); restored.admin(); await restored.run("bootstrapExperience()");
  assert.equal(restored.run("XP.stage"), "welcome");
  assert.match(onboarding(restored).innerHTML, /已保存到第 3 步/);
  assert.doesNotMatch(onboarding(restored).innerHTML, /如沐和风|xp-finish/);
  assert.ok(!restored.actions.some(a => a[0] === "view"));
  assert.equal(await restored.run("ACT['xp-finish']()"), false);
  await restored.run("ACT['xp-resume']()");
  assert.equal(restored.run("XP.stage"), "steps");
  assert.equal(restored.run("experienceStep()"), 3);
  assert.equal(onboardingButton(restored, "xp-finish"), undefined);
  assert.ok(onboardingButton(restored, "xp-review"));
  assert.equal(writes(restored).length, 0);
});

test("locking or losing authentication exits completion to step 1 and blocks finish", async t => {
  for (const reason of ["logout", "session-expired", "session-unavailable", "401"]) await t.test(reason, async () => {
    const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()"); h.run("ACT['xp-review']()");
    if (reason === "logout") await h.run("settingsLogout()");
    if (reason === "session-expired") { h.session.authenticated = false; await h.run("settingsRefreshSession()"); }
    if (reason === "session-unavailable") {
      h.override = (_, route) => { if (route === "/settings/session") throw new Error("test unavailable session"); };
      await h.run("settingsRefreshSession()");
    }
    if (reason === "401") {
      h.override = (method, route) => { if (method === "PATCH" && route === "/settings/onboarding") throw Object.assign(new Error("test expired session"), { status: 401 }); };
      assert.equal(await h.run("ACT['xp-finish']()"), false);
      assert.match(h.run("XP.error"), /管理员会话已失效/);
    }
    assert.equal(h.run("settingsAdmin()"), false);
    assert.equal(h.run("XP.stage"), "steps");
    assert.equal(h.run("experienceStep()"), 1);
    assert.match(onboarding(h).innerHTML, /1\. 管理员与安全/);
    assert.equal(onboardingButton(h, "xp-finish"), undefined);
    assert.deepEqual(h.state(), profile(3));
    const count = h.calls.length;
    assert.equal(await h.run("ACT['xp-finish']()"), false);
    assert.equal(h.run("ACT['xp-review']()"), false);
    assert.equal(h.calls.length, count);
    assert.equal(h.run("S.view"), "settings");
  });
});

test("logout invalidates a pending completion response without falsely completing or navigating", async () => {
  const h = harness(profile(3)); h.admin(); await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()"); h.run("ACT['xp-review']()");
  const pending = deferred();
  h.override = (method, route) => method === "PATCH" && route === "/settings/onboarding" ? pending.promise : undefined;
  const work = h.run("ACT['xp-finish']()");
  await h.run("settingsLogout()");
  assert.equal(h.run("XP.stage"), "steps");
  assert.equal(h.run("experienceStep()"), 1);
  const count = h.calls.length;
  pending.resolve({ ...profile(3), onboarding_complete: true });
  assert.equal(await work, false);
  assert.equal(h.calls.length, count, "invalidated completion must not perform its readback");
  assert.equal(h.run("XP.busy"), null);
  assert.deepEqual(h.state(), profile(3));
  assert.equal(h.run("S.view"), "settings");
  assert.equal(onboardingButton(h, "xp-finish"), undefined);
  assert.ok(!h.actions.some(a => a[0] === "toast" && /设置完成/.test(a[1])));
});

test("completion summary excludes archived sources and explicitly describes missing or unusable resources", async t => {
  const cases = [
    { label: "empty", mounts: [], nodes: [], sources: "0 个 · 本地 0 / NAS 0", terminals: "0 台 · 在线 0 / 已授权 0", warnings: ["音乐源待添加", "播放终端待连接"] },
    { label: "archived only", mounts: [{ kind: "local", status: "archived" }, { kind: "smb", status: "archived" }], nodes: [], sources: "0 个 · 本地 0 / NAS 0", warnings: ["音乐源待添加", "播放终端待连接"] },
    { label: "archived mixed with live sources", mounts: [{ kind: "local", status: "archived" }, { kind: "smb", status: "archived" }, { kind: "local", status: "mounted" }, { kind: "smb", status: "unmounted" }], nodes: [{ status: "online", authorized: true }], sources: "2 个 · 本地 1 / NAS 1", available: "1 个 · 扫描中 0", warnings: [] },
    { label: "unconnected and scanning sources", mounts: [{ kind: "local", status: "scanning" }, { kind: "smb", status: "unmounted" }], nodes: [{ status: "online", authorized: true }], sources: "2 个 · 本地 1 / NAS 1", available: "0 个 · 扫描中 1", warnings: ["音乐源待连接"] },
    { label: "authorized but offline", mounts: [{ kind: "local", status: "mounted" }], nodes: [{ status: "offline", authorized: true }], terminals: "1 台 · 在线 0 / 已授权 1", warnings: ["暂无在线且已授权的播放终端"] },
    { label: "online but unauthorized", mounts: [{ kind: "local", status: "mounted" }], nodes: [{ status: "online", authorized: false }], terminals: "1 台 · 在线 1 / 已授权 0", warnings: ["暂无在线且已授权的播放终端"] },
    { label: "online and authorized counts belong to different nodes", mounts: [{ kind: "local", status: "mounted" }], nodes: [{ status: "online", authorized: false }, { status: "offline", authorized: true }], terminals: "2 台 · 在线 1 / 已授权 1", warnings: ["暂无在线且已授权的播放终端"] },
    { label: "ready source and terminal", mounts: [{ kind: "local", status: "mounted" }], nodes: [{ status: "online", authorized: true }], sources: "1 个 · 本地 1 / NAS 0", terminals: "1 台 · 在线 1 / 已授权 1", warnings: [] }
  ];
  const allWarnings = ["音乐源待添加", "音乐源待连接", "播放终端待连接", "暂无在线且已授权的播放终端"];
  for (const example of cases) await t.test(example.label, async () => {
    const h = harness(profile(3)); h.admin(); h.mounts = example.mounts; h.nodes = example.nodes;
    await h.run("bootstrapExperience()"); await h.run("ACT['xp-resume']()"); h.run("ACT['xp-review']()");
    const html = h.run("experienceSummaryContent()");
    // The flat DOM mock does not retain nested innerHTML; verify the card's rendered markup too.
    assert.ok(onboarding(h).innerHTML.includes(html));
    if (example.sources) assert.ok(html.includes(`<dt>音乐源</dt><dd>${example.sources}</dd>`));
    if (example.available) assert.ok(html.includes(`<dt>可用音乐源</dt><dd>${example.available}</dd>`));
    if (example.terminals) assert.ok(html.includes(`<dt>播放终端</dt><dd>${example.terminals}</dd>`));
    for (const warning of allWarnings) assert.equal(html.includes(warning), example.warnings.includes(warning), warning);
    if (example.warnings.length) assert.match(html, /结束引导后可从音乐文件夹或音频设备继续配置/);
    assert.doesNotMatch(html, /全部就绪|所有.*已配置|自动播放|自动安装/);
    assert.equal(onboardingButton(h, "xp-finish").disabled, false, "missing resources must remain non-blocking");
    assert.equal(writes(h).length, 0);
  });
});

const languageKey = "zephyr-ui-language";
async function languageHarness({ authenticated = false, ...options } = {}) {
  const h = harness(undefined, options);
  h.session.configured = true;
  if (authenticated) h.admin();
  await h.run("settingsRefreshSession()");
  await h.run("experienceRefreshLanguage()");
  // Mount the standalone card, without the full settings page's browser DOM replacement.
  h.elements.get("#view").innerHTML = h.run("experienceLanguageCard()");
  h.run("experienceLanguageFeedback()");
  return h;
}
function languageDraft(h, locale) {
  h.input("server-default-language", locale);
  h.run("CHG['xp-default-language']($('#server-default-language'))");
}
function assertLanguageLock(h, busy, authenticated = true) {
  assert.equal(h.run("XP.languageBusy"), busy);
  assert.equal(h.run("SETTINGS.busy.has('language')"), busy);
  const html = h.run("experienceLanguageCard()");
  for (const [selector, disabled] of [
    ['id="server-default-language"', busy || !authenticated],
    ['data-act="xp-language-save"', busy || !authenticated],
    ['data-act="xp-language-refresh"', busy],
    ['id="ui-language-preference"', false]
  ]) {
    const tag = html.match(new RegExp(`<(?:select|button)\\b[^>]*${selector}[^>]*>`));
    assert.ok(tag, selector + " must be rendered");
    assert.equal(/\sdisabled(?:\s|>)/.test(tag[0]), disabled, selector);
  }
}

test("public language preferences switch all three locales and browser/server modes without server writes", async () => {
  const h = await languageHarness();
  h.run("navigator.languages=['zh-HK', 'en-US']");
  const calls = h.calls.length, storageStart = h.storageCalls.length;
  const choices = [["en", "en", "Home"], ["zh-TW", "zh-TW", "主頁"], ["zh-CN", "zh-CN", "主页"], ["browser", "zh-TW", "主頁"], ["server", "zh-CN", "主页"]];
  for (const [preference, locale, home] of choices) {
    h.input("ui-language-preference", preference);
    h.run("CHG['xp-language-preference']($('#ui-language-preference'))");
    assert.equal(h.run("I18n.preference"), preference);
    assert.equal(h.run("I18n.locale"), locale);
    assert.equal(h.elements.get("#ui-language-preference").value, preference);
    assert.equal(h.run("NAV.find(n => n.id === 'overview').name"), home);
    assert.ok(h.elements.get("#nav").innerHTML.includes(`<span>${home}</span>`));
    assert.equal(h.storage.get(languageKey), preference);
    assert.equal(h.run("XP.preferenceNoticeKind"), "info");
    assert.equal(h.run("settingsAdmin()"), false);
    assert.equal(h.run("I18n.defaultLocale"), "zh-CN");
    assert.equal(h.defaultLanguage, "zh-CN");
    assert.equal(h.calls.length, calls, "preference changes must not trigger any request");
  }
  assert.deepEqual(h.storageCalls.slice(storageStart), choices.map(([value]) => ({ method: "setItem", key: languageKey, value })));
  assert.equal(h.events.filter(e => e.type === "language-changed").length, choices.length);
  assertLanguageLock(h, false, false);
  h.input("server-default-language", "en");
  assert.equal(await h.run("ACT['xp-language-save']()"), false);
  assert.equal(h.calls.length, calls);
  assert.equal(writes(h).length, 0);
});

test("failed localStorage setItem still applies each language preference for this visit and warns", async t => {
  for (const locale of ["zh-CN", "zh-TW", "en"]) await t.test(locale, async () => {
    const previous = locale === "en" ? "zh-TW" : "en";
    const storage = new Map([[languageKey, previous], ["unrelated-preference", "preserve"]]);
    const h = await languageHarness({ storage, storageFailures: { setItem: true } });
    const calls = h.calls.length, storageStart = h.storageCalls.length, actionStart = h.actions.length;
    h.input("ui-language-preference", locale);
    assert.doesNotThrow(() => h.run("CHG['xp-language-preference']($('#ui-language-preference'))"));
    assert.equal(h.run("I18n.preference"), locale);
    assert.equal(h.run("I18n.locale"), locale);
    assert.equal(h.elements.get("#ui-language-preference").value, locale);
    assert.equal(h.run("XP.preferenceNoticeKind"), "warn");
    assert.match(h.run("XP.preferenceNotice"), /无法保存语言偏好.*本次切换仍已生效/);
    assert.equal(h.actions.slice(actionStart).filter(a => a[0] === "toast" && a[2] === "warn").length, 1);
    assert.ok(!h.actions.slice(actionStart).some(a => a[0] === "toast" && a[2] === "ok"));
    assert.equal(h.events.filter(e => e.type === "language-changed").at(-1).detail.locale, locale);
    assert.equal(storage.get(languageKey), previous);
    assert.equal(storage.get("unrelated-preference"), "preserve");
    assert.deepEqual(h.storageCalls.slice(storageStart), [{ method: "setItem", key: languageKey, value: locale }]);
    assert.equal(h.run("I18n.defaultLocale"), "zh-CN");
    assert.equal(h.defaultLanguage, "zh-CN");
    assert.equal(h.calls.length, calls);
    assert.equal(writes(h).length, 0);
  });
});

test("administrator language save waits for PATCH then GET confirmation and serializes both phases", async () => {
  const h = await languageHarness({ authenticated: true });
  languageDraft(h, "zh-TW");
  const patch = deferred(), read = deferred(), readStarted = deferred();
  h.override = (method, route) => {
    if (route !== "/settings/language") return;
    if (method === "PATCH") return patch.promise;
    if (method === "GET") { readStarted.resolve(); return read.promise; }
  };
  const calls = h.calls.length, actionStart = h.actions.length, events = h.events.length, storageStart = h.storageCalls.length;
  const editSeq = h.run("XP.languageEditSeq");
  const save = h.run("ACT['xp-language-save']()");
  for (const phase of ["PATCH", "GET"]) {
    if (phase === "GET") { patch.resolve({ default_language: "zh-TW" }); await readStarted.promise; }
    assertLanguageLock(h, true);
    assert.equal(h.run("I18n.defaultLocale"), "zh-CN", phase + " alone cannot apply the default");
    assert.equal(h.run("I18n.locale"), "zh-CN");
    assert.equal(h.run("XP.languageDirty"), true);
    assert.equal(h.run("XP.languageNotice"), "");
    assert.equal(h.events.length, events);
    assert.ok(!h.actions.slice(actionStart).some(a => a[0] === "toast" && a[2] === "ok"));
    const count = h.calls.length;
    assert.equal(await h.run("ACT['xp-language-save']()"), false);
    await h.run("ACT['xp-language-refresh']()");
    h.run("CHG['xp-default-language']({value:'en'})");
    assert.equal(h.run("XP.languageDraft"), "zh-TW");
    assert.equal(h.run("XP.languageEditSeq"), editSeq);
    assert.equal(h.calls.length, count, "busy save must block duplicate writes and refreshes");
  }
  read.resolve({ default_language: "zh-TW" });
  assert.equal(await save, true);
  assertLanguageLock(h, false);
  assert.equal(h.run("I18n.defaultLocale"), "zh-TW");
  assert.equal(h.run("I18n.locale"), "zh-TW");
  assert.equal(h.run("I18n.preference"), "server");
  assert.equal(h.run("XP.languageDraft"), "zh-TW");
  assert.equal(h.elements.get("#server-default-language").value, "zh-TW");
  assert.equal(h.run("XP.languageDirty"), false);
  assert.equal(h.run("XP.languageLoaded"), true);
  assert.equal(h.run("XP.languageNotice"), "服务端默认语言已保存。");
  assert.equal(h.run("XP.languageNoticeKind"), "info");
  assert.equal(h.actions.slice(actionStart).filter(a => a[0] === "toast" && a[2] === "ok").length, 1);
  assert.equal(h.events.filter(e => e.type === "language-changed").at(-1).detail.defaultLocale, "zh-TW");
  assert.deepEqual(h.calls.slice(calls), [
    { method: "PATCH", route: "/settings/language", body: { default_language: "zh-TW" } },
    { method: "GET", route: "/settings/language", body: undefined }
  ]);
  assert.deepEqual(h.storageCalls.slice(storageStart), [], "server default save must not overwrite browser preference");
});

test("failed or mismatched language saves never announce success or apply an unconfirmed default and can retry", async t => {
  for (const failure of ["write", "read", "mismatch", "invalid-response"]) await t.test(failure, async () => {
    const h = await languageHarness({ authenticated: true });
    languageDraft(h, "zh-TW");
    const calls = h.calls.length, actionStart = h.actions.length, events = h.events.length;
    h.override = (method, route) => {
      if (route !== "/settings/language") return;
      if ((failure === "write" && method === "PATCH") || (failure === "read" && method === "GET")) throw new Error("test language save unavailable");
      if (method === "PATCH") return { default_language: "zh-TW" };
      if (method === "GET") return { default_language: failure === "invalid-response" ? "fr" : "en" };
    };
    assert.equal(await h.run("ACT['xp-language-save']()"), false);
    assertLanguageLock(h, false);
    assert.equal(h.run("I18n.defaultLocale"), "zh-CN");
    assert.equal(h.run("I18n.locale"), "zh-CN");
    assert.equal(h.run("XP.languageDraft"), "zh-TW");
    assert.equal(h.run("XP.languageDirty"), true);
    assert.equal(h.run("XP.languageNoticeKind"), "err");
    assert.ok(h.run("XP.languageNotice").length > 0);
    assert.ok(h.actions.slice(actionStart).some(a => a[0] === "toast" && a[2] === "err"));
    assert.ok(!h.actions.slice(actionStart).some(a => a[0] === "toast" && a[2] === "ok"));
    assert.equal(h.events.length, events);
    assert.deepEqual(h.calls.slice(calls).map(c => [c.method, c.route]), failure === "write" ? [["PATCH", "/settings/language"]] : [["PATCH", "/settings/language"], ["GET", "/settings/language"]]);
    h.override = null;
    assert.equal(await h.run("ACT['xp-language-save']()"), true);
    assert.equal(h.defaultLanguage, "zh-TW");
    assert.equal(h.run("I18n.defaultLocale"), "zh-TW");
    assert.equal(h.run("XP.languageDirty"), false);
    assertLanguageLock(h, false);
  });
});

test("late language refresh successes and errors cannot overwrite a newer refresh or confirmed save", async t => {
  for (const newer of ["refresh", "save"]) for (const outcome of ["success", "error"]) await t.test(`${newer}, stale ${outcome}`, async () => {
    const h = await languageHarness({ authenticated: true });
    const pending = deferred();
    h.override = (method, route) => method === "GET" && route === "/settings/language" ? pending.promise : undefined;
    const old = h.run("ACT['xp-language-refresh']()");
    assert.equal(h.run("XP.languageLoading"), true);
    h.override = null;
    if (newer === "refresh") { h.defaultLanguage = "en"; await h.run("ACT['xp-language-refresh']()"); }
    else { languageDraft(h, "en"); assert.equal(await h.run("ACT['xp-language-save']()"), true); }
    assert.equal(h.run("XP.languageLoading"), false);
    const notice = h.run("XP.languageNotice"), events = h.events.length, actions = h.actions.length, calls = h.calls.length;
    if (outcome === "success") pending.resolve({ default_language: "zh-TW" });
    else pending.reject(new Error("test stale language read failed"));
    await old;
    assert.equal(h.run("I18n.defaultLocale"), "en");
    assert.equal(h.run("I18n.locale"), "en");
    assert.equal(h.run("XP.languageDraft"), "en");
    assert.equal(h.elements.get("#server-default-language").value, "en");
    assert.equal(h.run("XP.languageDirty"), false);
    assert.equal(h.run("XP.languageLoading"), false);
    assert.equal(h.run("XP.languageNotice"), notice);
    assert.equal(h.run("XP.languageNoticeKind"), "info");
    assert.equal(h.events.length, events);
    assert.equal(h.actions.length, actions);
    assert.equal(h.calls.length, calls);
  });
});

test("language refresh preserves dirty drafts edited before or during the read", async t => {
  for (const timing of ["before", "during"]) await t.test(timing, async () => {
    const h = await languageHarness({ authenticated: true });
    const pending = deferred();
    h.override = (method, route) => method === "GET" && route === "/settings/language" ? pending.promise : undefined;
    if (timing === "before") languageDraft(h, "zh-TW");
    const read = h.run("ACT['xp-language-refresh']()");
    if (timing === "during") languageDraft(h, "zh-TW");
    const editSeq = h.run("XP.languageEditSeq");
    assert.equal(h.run("XP.languageDirty"), true);
    pending.resolve({ default_language: "en" }); await read;
    assert.equal(h.run("I18n.defaultLocale"), "en", "public confirmed default may refresh independently of the draft");
    assert.equal(h.run("I18n.locale"), "en");
    assert.equal(h.run("XP.languageDraft"), "zh-TW");
    assert.equal(h.elements.get("#server-default-language").value, "zh-TW");
    assert.equal(h.run("XP.languageDirty"), true);
    assert.equal(h.run("XP.languageEditSeq"), editSeq);
    assert.equal(h.run("XP.languageLoading"), false);
    assert.equal(writes(h).length, 0);
  });
});

test("language save 401 at either PATCH or confirmation locks the session and prevents further writes", async t => {
  for (const phase of ["PATCH", "GET"]) await t.test(phase, async () => {
    const h = await languageHarness({ authenticated: true });
    languageDraft(h, "zh-TW");
    const epoch = h.run("XP.epoch"), actionStart = h.actions.length, calls = h.calls.length;
    h.override = (method, route) => {
      if (route !== "/settings/language") return;
      if (method === phase) throw Object.assign(new Error("test expired language session"), { status: 401 });
      if (method === "PATCH") return { default_language: "zh-TW" };
    };
    assert.equal(await h.run("ACT['xp-language-save']()"), false);
    assert.equal(h.run("settingsAdmin()"), false);
    assert.equal(h.run("SETTINGS.session.authenticated"), false);
    assert.equal(h.run("XP.authenticated"), false);
    assert.ok(h.run("XP.epoch") > epoch);
    assertLanguageLock(h, false, false);
    assert.equal(h.run("I18n.defaultLocale"), "zh-CN");
    assert.equal(h.run("I18n.locale"), "zh-CN");
    assert.equal(h.run("XP.languageDraft"), "zh-TW");
    assert.equal(h.run("XP.languageDirty"), true);
    assert.equal(h.run("XP.languageNoticeKind"), "err");
    assert.match(h.run("XP.languageNotice"), /管理员会话已失效/);
    assert.ok(h.actions.slice(actionStart).some(a => a[0] === "toast" && a[2] === "err"));
    assert.ok(!h.actions.slice(actionStart).some(a => a[0] === "toast" && a[2] === "ok"));
    assert.deepEqual(h.calls.slice(calls).map(c => [c.method, c.route]), phase === "PATCH" ? [["PATCH", "/settings/language"]] : [["PATCH", "/settings/language"], ["GET", "/settings/language"]]);
    const count = h.calls.length;
    assert.equal(await h.run("ACT['xp-language-save']()"), false);
    h.run("CHG['xp-default-language']({value:'en'})");
    assert.equal(h.run("XP.languageDraft"), "zh-TW");
    h.input("ui-language-preference", "en");
    h.run("CHG['xp-language-preference']($('#ui-language-preference'))");
    assert.equal(h.run("I18n.locale"), "en", "locking administration must not block local preferences");
    assert.equal(h.calls.length, count);
  });
});

test("session changes during either language save phase discard old results even after reauthentication", async t => {
  for (const phase of ["PATCH", "GET"]) for (const change of ["logout", "expired", "reauthenticate"]) await t.test(`${phase}, ${change}`, async () => {
    const h = await languageHarness({ authenticated: true });
    languageDraft(h, "zh-TW");
    const patch = deferred(), read = deferred(), readStarted = deferred();
    h.override = (method, route) => {
      if (route !== "/settings/language") return;
      if (method === "PATCH") return patch.promise;
      if (method === "GET") { readStarted.resolve(); return read.promise; }
    };
    const epoch = h.run("XP.epoch"), authSeq = h.run("SETTINGS.authSeq");
    const save = h.run("ACT['xp-language-save']()");
    if (phase === "GET") { patch.resolve({ default_language: "zh-TW" }); await readStarted.promise; }
    if (change === "logout") await h.run("settingsLogout()");
    if (change === "expired") { h.session.authenticated = false; await h.run("settingsRefreshSession()"); }
    if (change === "reauthenticate") {
      h.input("settings-password", "test-password-only");
      assert.equal(await h.run("settingsAuthenticate()"), true);
      assert.equal(h.run("XP.epoch"), epoch, "reauthentication must also invalidate via authSeq, without requiring logout");
      assert.ok(h.run("SETTINGS.authSeq") > authSeq);
    }
    const authenticated = change === "reauthenticate";
    assert.equal(h.run("settingsAdmin()"), authenticated);
    assertLanguageLock(h, true, authenticated);
    const count = h.calls.length, actionStart = h.actions.length, events = h.events.length;
    assert.equal(await h.run("ACT['xp-language-save']()"), false);
    if (phase === "PATCH") patch.resolve({ default_language: "zh-TW" });
    else read.resolve({ default_language: "zh-TW" });
    assert.equal(await save, false);
    assert.equal(h.calls.length, count, "invalidated PATCH must not start a confirmation GET");
    assertLanguageLock(h, false, authenticated);
    assert.equal(h.run("I18n.defaultLocale"), "zh-CN");
    assert.equal(h.run("I18n.locale"), "zh-CN");
    assert.equal(h.run("XP.languageDraft"), "zh-TW");
    assert.equal(h.run("XP.languageDirty"), true);
    assert.equal(h.run("XP.languageNoticeKind"), "warn");
    assert.match(h.run("XP.languageNotice"), /会话已变化.*保存结果未确认/);
    assert.ok(!h.actions.slice(actionStart).some(a => a[0] === "toast" && a[2] === "ok"));
    assert.equal(h.events.length, events);
  });
});

test("language changes repaint cached library and audit views without remounting or requesting data", async t => {
  for (const route of ["library", "audit"]) await t.test(route, () => {
    const h = harness();
    h.run(fs.readFileSync(path.join(assets, "i18n-features.js"), "utf8"));
    h.run(fs.readFileSync(path.join(assets, "library-browser.js"), "utf8"));
    h.run(`S.view=${JSON.stringify(route)}; S.search='keep'; S.sel.add('selected');
      S.play={node_id:'playing',position:42}; S.nodeQueue.playing=[{id:'queued'}];
      S.auditFilter='settings.update'; S.audit=[{action:'settings.update',actor:'keep',target:'cached-target',at:1,result:'ok'}];
      LB.request=7; lbFrame().query.page=2; lbFrame().query.q='keep';
      LB.page={page:2,page_size:24,total:25,total_tracks:25,items:[{kind:'album',name:'Cached album',value:'Cached album',artist:'Keep artist',track_count:1}]};`);
    const view = h.elements.get("#view");
    view.innerHTML = h.run(route === "library" ? "shellLibraryBrowser()" : "shellAudit()");
    // The flat mock cannot resolve class selectors; supply just the audit title.
    const title = h.input("language-audit-title", ""); h.elements.set("#view .sec-title", title);
    const originalHTML = view.innerHTML;
    Object.defineProperty(view, "innerHTML", { get: () => originalHTML, set() { assert.fail("language changes must not remount #view"); } });
    const state = h.run("JSON.stringify([S,lbState().frames.map(frame=>frame.query),LB.page,LB.request,LB.busy,LB.error,LB.searchTimer])"), page = h.run("LB.page"), calls = h.calls.length, actions = h.actions.length;
    for (const locale of ["en", "zh-TW", "zh-CN"]) {
      h.run(`I18n.setPreference(${JSON.stringify(locale)})`);
      assert.equal(h.elements.get("#view"), view);
      assert.equal(h.run("JSON.stringify([S,lbState().frames.map(frame=>frame.query),LB.page,LB.request,LB.busy,LB.error,LB.searchTimer])"), state);
      assert.equal(h.run("LB.page"), page);
      assert.equal(h.run("S.sel.has('selected')"), true);
      if (route === "library") {
        assert.ok(h.elements.get("#lb-header").innerHTML.includes(h.run("lbT('libTitle')")));
        assert.match(h.elements.get("#lb-body").innerHTML, /Cached album/);
        assert.match(h.elements.get("#lb-pages").innerHTML, /data-offset="0"/);
      } else {
        assert.ok(title.innerHTML.includes(h.run("appT('m068')")));
        assert.match(h.elements.get("#au-chips").innerHTML, /class="chip active" data-act="audit-filter" data-f="settings.update"/);
        assert.match(h.elements.get("#au-table").innerHTML, /cached-target/);
      }
      assert.equal(h.calls.length, calls, "cached language paints must not even issue GETs");
      assert.equal(h.actions.length, actions, "no navigation, playback or action replay");
    }
  });
});

test("experience delegates settings translation inside the existing view/onboarding preservation boundary", () => {
  const h = harness(), view = h.elements.get("#view"), root = onboarding(h);
  h.run(`setView('settings'); SETTINGS.section='devices'; S.search='unsaved filter'; S.sel.add('keep');
    window.languagePaints=[];
    const preserveForLanguageTest=experiencePreserveForms;
    experiencePreserveForms=(roots,paint)=>{ window.languageRoots=roots; languagePaints.push('preserve'); preserveForLanguageTest(roots,paint); languagePaints.push('restored'); };
    const settingsForLanguageTest=settingsLanguageChanged;
    settingsLanguageChanged=()=>{ languagePaints.push('settings'); if (SETTINGS.section==='devices') terminalCenterLanguageChanged(); };
    function terminalCenterLanguageChanged() { languagePaints.push('terminal'); }
    const onboardingForLanguageTest=experiencePaintOnboarding;
    experiencePaintOnboarding=()=>{ languagePaints.push('onboarding'); onboardingForLanguageTest(); };`);
  h.actions.length = 0;
  Object.defineProperty(view, "innerHTML", { set() { assert.fail("settings translation must retain #view"); } });
  h.run("I18n.setPreference('en')");
  assert.deepEqual(Array.from(h.run("languageRoots")), [view, root]);
  assert.deepEqual(Array.from(h.run("languagePaints")), ["preserve", "settings", "terminal", "onboarding", "restored"]);
  assert.equal(h.run("S.view"), "settings"); assert.equal(h.run("SETTINGS.section"), "devices");
  assert.equal(h.run("S.search"), "unsaved filter");
  assert.equal(h.run("S.sel.has('keep')"), true);
  assert.equal(h.elements.get("#sidebar").getAttribute("aria-label"), h.run("settingsT('主导航')"));
  assert.match(h.elements.get("#nav").innerHTML, /Settings/);
  assert.deepEqual(h.calls, []); assert.deepEqual(h.actions, []);
});

test("form preservation keeps original controls, files, notices and selection while translating attributes", () => {
  const h = harness(); h.run("I18n.setPreference('en')");
  const root = h.elements.get("#view");
  const control = (id, tag, type, value) => Object.assign(h.input(id, value), { id, tagName: tag, type, readOnly: false });
  const text = control("draft", "INPUT", "text", "unsaved name");
  text.setAttribute("title", "stale title"); text.selectionStart = 2; text.selectionEnd = 8; text.selectionDirection = "backward";
  text.setSelectionRange = (...range) => { text.restoredSelection = range; };
  const select = control("choice", "SELECT", "select-one", "keep");
  const check = control("check", "INPUT", "checkbox", "on"); check.checked = true;
  const file = control("settings-restore-file", "INPUT", "file", "C:\\fakepath\\backup.json");
  const files = [{ name: "backup.json", size: 12, text() { assert.fail("translation must not read the backup"); } }]; file.files = files;
  Object.defineProperty(file, "value", { get: () => "C:\\fakepath\\backup.json", set() { assert.fail("file input value must not be assigned"); } });
  const note = h.input("settings-restore-file-note", "");
  const notice = h.input("pending-error", ""); notice.id = "pending-error"; notice.innerHTML = "existing error";
  const originals = [text, select, check, file]; let controls = originals.slice(), notices = [notice];
  // Only model the replacement/selection surface used by experiencePreserveForms.
  root.contains = el => controls.includes(el);
  root.querySelectorAll = selector => selector === "input, select, textarea" ? controls : selector.includes("[role=") || selector === "[id]" ? notices : [];
  Object.defineProperty(text, "isConnected", { get: () => controls.includes(text) }); text.focus();
  h.document.paintLanguageForms = () => {
    controls = originals.map((old, index) => {
      const next = control("fresh-" + index, old.tagName, old.type, "default"); next.id = old.id;
      next.setAttribute("aria-label", "Translated control"); next.setAttribute("placeholder", "Translated placeholder");
      next.disabled = old === check; next.readOnly = old === text;
      if (old === select) next.innerHTML = '<option value="keep">Keep selection</option>';
      next.replaceWith = el => { controls[index] = el; };
      return next;
    });
    notices = [{ id: notice.id, replaceWith: el => { notices[0] = el; } }];
    h.run("window.scrollY=900");
  };
  h.run("window.scrollX=12; window.scrollY=34; window.scrollTo=(x,y)=>{window.scrollX=x;window.scrollY=y;}; experiencePreserveForms([null,$('#view')],document.paintLanguageForms)");
  assert.deepEqual(controls, originals); assert.equal(notices[0], notice);
  assert.equal(text.value, "unsaved name"); assert.equal(select.value, "keep"); assert.equal(check.checked, true);
  assert.equal(file.files, files); assert.match(note.textContent, /backup.json/);
  assert.equal(text.getAttribute("aria-label"), "Translated control");
  assert.equal(text.getAttribute("placeholder"), "Translated placeholder"); assert.equal(text.hasAttribute("title"), false);
  assert.equal(text.readOnly, true); assert.equal(check.disabled, true);
  assert.match(select.innerHTML, /Keep selection/);
  assert.equal(h.document.activeElement, text); assert.deepEqual(text.restoredSelection, [2, 8, "backward"]);
  assert.equal(h.run("window.scrollX"), 12); assert.equal(h.run("window.scrollY"), 34);
  assert.deepEqual(h.calls, []); assert.deepEqual(h.actions, []);
});

// A structural stub, not an HTML parser: model only child moves and selectors
// needed by settingsLanguageChanged. Real browser DOM identity is tested separately.
function languageTree(selector, children = []) {
  const node = { selector, children: [], dataset: {}, innerHTML: "", textContent: "",
    get childNodes() { return this.children; },
    querySelector(s) { return this.children.find(child => child.selector === s) || this.children.map(child => child.querySelector(s)).find(Boolean) || null; },
    querySelectorAll(s) {
      const out = [];
      const walk = n => { for (const child of n.children) { if (child.selector === s) out.push(child); walk(child); } };
      walk(this); return out;
    },
    setAttribute(name, value) { this[name] = value; },
    replaceWith(next) {
      assert.ok(next, "replacement must exist");
      const parent = this.parentElement;
      if (next.parentElement) next.parentElement.children.splice(next.parentElement.children.indexOf(next), 1);
      parent.children.splice(parent.children.indexOf(this), 1, next); next.parentElement = parent; this.parentElement = null;
    },
    replaceChildren(...next) {
      for (const child of this.children) child.parentElement = null;
      this.children = [];
      for (const child of next) {
        if (child.parentElement) child.parentElement.children.splice(child.parentElement.children.indexOf(child), 1);
        child.parentElement = this; this.children.push(child);
      }
    }
  };
  node.replaceChildren(...children); return node;
}

test("settings translation retains embedded async roots and repaints cached authorization, packages and maintenance", async t => {
  for (const section of ["authorization", "packages", "maintenance"]) await t.test(section, async () => {
    const h = harness();
    h.run(`S.view='settings'; SETTINGS.section=${JSON.stringify(section)}; SETTINGS.epoch=11; SETTINGS.refreshSeq=4;
      SETTINGS.session={authenticated:true}; SETTINGS.busy.add('maintenance');
      S.license={status:'none',builtin_seats:3}; S.keys=[]; S.apps=[]; S.boms=[];
      S.upd.server={state:'idle',current:'1.2.3',latest:'1.2.4'};
      SETTINGS.maintenance={counts:{tracks:17,mounts:2,playlists:3,history:4,covers:5,audit:6}};`);
    const fixture = () => {
      const content = section === "authorization" ? [languageTree("note"), languageTree("grid", [languageTree("#lic-card"), languageTree("#gpg-card")]), languageTree("banner")]
        : section === "packages" ? [languageTree("note"), languageTree("sources"), languageTree("apps", [languageTree(".sec-head"), languageTree("#cat-apps")]), languageTree("#bom-report"), languageTree("boms", [languageTree(".sec-head"), languageTree("#cat-boms")])]
          : [languageTree("update", [languageTree("#upd-body", [languageTree("#upd-server")])]), languageTree("#settings-maintenance-counts")];
      return languageTree("#settings-page", [languageTree(".page-head"), languageTree("#settings-admin"), languageTree(".settings-anchors"), languageTree(".settings-section-heading"), languageTree("#settings-content-" + section, content)]);
    };
    const page = fixture(), query = h.document.querySelector, queryAll = h.document.querySelectorAll;
    const strip = selector => selector.startsWith("#settings-page ") ? selector.slice("#settings-page ".length) : null;
    h.document.querySelector = selector => { if (selector === "#settings-page") return page; return page.querySelector(selector.replace(/^#settings-page /, "")) || query(selector); };
    h.document.querySelectorAll = selector => { const inner = strip(selector); if (inner === null) return queryAll(selector); const node = page.querySelector(inner); return node ? [node] : []; };
    const retainedIDs = section === "authorization" ? ["#lic-card", "#gpg-card"] : section === "packages" ? ["#cat-apps", "#cat-boms", "#bom-report"] : ["#upd-body", "#upd-server"];
    const retained = retainedIDs.map(id => page.querySelector(id));
    const admin = page.querySelector("#settings-admin"), content = page.querySelector("#settings-content-" + section);
    const snapshot = h.run("JSON.stringify([S,SETTINGS.epoch,SETTINGS.refreshSeq,SETTINGS.session,Array.from(SETTINGS.busy),SETTINGS.maintenance])");
    for (const locale of ["en", "zh-TW", "zh-CN"]) {
      const oldHeading = page.querySelector(".page-head"), oldSectionHeading = page.querySelector(".settings-section-heading");
      const oldPackageHeads = section === "packages" ? retained.slice(0, 2).map(node => node.parentElement.querySelector(".sec-head")) : [];
      let next;
      h.document.createElement = tag => { assert.equal(tag, "div"); next = fixture(); return next; };
      h.run(`I18n.setPreference(${JSON.stringify(locale)})`);
      assert.ok(next.innerHTML.includes(h.run("settingsT('刷新状态')")), "real shell is generated in the selected language");
      assert.notEqual(page.querySelector(".page-head"), oldHeading);
      assert.notEqual(page.querySelector(".settings-section-heading"), oldSectionHeading);
      assert.equal(page.querySelector("#settings-content-" + section), content);
      assert.equal(page.querySelector("#settings-admin"), admin);
      assert.equal(admin["aria-label"], h.run("settingsT('管理员解锁')"));
      for (const [index, id] of retainedIDs.entries()) assert.equal(page.querySelector(id), retained[index], id + " must survive a language repaint");
      for (const [index, old] of oldPackageHeads.entries()) assert.notEqual(retained[index].parentElement.querySelector(".sec-head"), old);
      const painted = section === "maintenance" ? page.querySelector("#settings-maintenance-counts").innerHTML : retained[0].innerHTML;
      const label = section === "maintenance" ? h.run("settingsT('音乐源')") : h.run(section === "authorization" ? "appT('m430')" : "appT('m375')");
      assert.ok(painted.includes(label), "cached content must be painted, not just the headings");
      if (section === "maintenance") { assert.match(painted, />2</); assert.doesNotMatch(painted, />17</); assert.match(page.querySelector("#upd-server").innerHTML, /1\.2\.3/); }
      assert.equal(h.run("JSON.stringify([S,SETTINGS.epoch,SETTINGS.refreshSeq,SETTINGS.session,Array.from(SETTINGS.busy),SETTINGS.maintenance])"), snapshot);
      assert.deepEqual(h.calls, []); assert.deepEqual(h.actions, []);
    }
    const pending = deferred(), target = retained[0];
    const response = pending.promise.then(value => { target.innerHTML = value; });
    h.run("I18n.setPreference('en')"); pending.resolve("late response"); await response;
    assert.equal(page.querySelector(retainedIDs[0]), target);
    assert.equal(page.querySelector(retainedIDs[0]).innerHTML, "late response");
    assert.deepEqual(h.calls, []); assert.deepEqual(h.actions, []);
  });
});

test("assets order keeps experience between settings and terminal-center", () => {
  const html = fs.readFileSync(path.join(assets, "index.html"), "utf8");
  assert.ok(html.indexOf('src="/settings.js"') < html.indexOf('src="/experience.js"'));
  assert.ok(html.indexOf('src="/experience.js"') < html.indexOf('src="/terminal-center.js"'));
  assert.match(html, /id="onboarding-root"/); assert.match(html, /aria-controls="sidebar" aria-expanded="false"/);
});

test("new albums sort by the latest added track, cache groups and cap at 45", () => {
  const h = harness();
  h.run(`S.tracks = [
    { artist: 'A', album: 'Old', added_at: 5, duration_ms: 1 },
    { artist: 'A', album: 'Old', added_at: 100, duration_ms: 1 },
    { artist: 'B', album: 'Newest', added_at: 200, duration_ms: 1 },
    { artist: 'C', album: 'Mixed', added_at: 0, duration_ms: 1 },
    { artist: 'C', album: 'Mixed', added_at: 150, duration_ms: 1 },
    { artist: 'D', album: 'Unknown', added_at: 0, duration_ms: 1 }
  ];`);
  const order = h.run("JSON.stringify(ovAddedAlbums(albumGroups()).filter(x => x.added > 0).map(x => [x.g.album, x.added]))");
  assert.equal(order, JSON.stringify([["Newest", 200], ["Mixed", 150], ["Old", 100]]));
  assert.equal(h.run("albumGroups() === albumGroups()"), true, "album grouping must be cached");
  assert.equal(h.run("ovAddedAlbums(albumGroups()) === ovAddedAlbums(albumGroups())"), true, "added ordering must be cached");
  assert.equal(h.run("NEW_ALBUM_LIMIT"), 45);
});
