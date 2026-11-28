"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const assets = path.join(__dirname, "assets");
const index = fs.readFileSync(path.join(assets, "index.html"), "utf8");
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
const flush = () => new Promise(resolve => setImmediate(resolve));
const deviceKey = id => id + "::hw:1";
const deviceQuery = key => "?device_key=" + encodeURIComponent(key);
const deviceRoute = (id, action, key = deviceKey(id)) => `/nodes/${encodeURIComponent(id)}/${action}${deviceQuery(key)}`;

// Preserve DOM identity to catch stale writes to detached controls.
function makeDOM() {
  let document;
  const decode = text => String(text).replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");
  class Element {
    constructor(tag = "div", attrs = "") {
      this.tagName = tag.toUpperCase(); this.children = []; this.parentElement = null;
      this.dataset = {}; this.attributes = {}; this.value = ""; this.textContent = "";
      this.disabled = false; this.scrollTop = 0; this.htmlWrites = 0; this.height = 112.2;
      this.style = { setProperty: (name, value) => { this.style[name] = value; } };
      const classes = new Set();
      this.classList = {
        add: (...names) => names.forEach(name => classes.add(name)),
        remove: (...names) => names.forEach(name => classes.delete(name)),
        contains: name => classes.has(name),
        toggle: (name, force) => {
          const on = force === undefined ? !classes.has(name) : !!force;
          if (on) classes.add(name); else classes.delete(name);
          return on;
        }
      };
      for (const match of attrs.matchAll(/([^\s=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g)) {
        this.setAttribute(match[1], decode(match[2] ?? match[3] ?? match[4] ?? ""));
      }
    }
    setAttribute(name, value) {
      this.attributes[name] = String(value);
      if (name.startsWith("data-")) this.dataset[name.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = String(value);
      else if (name === "class") this.classList.add(...String(value).split(/\s+/));
      else if (name === "disabled") this.disabled = true;
      else this[name] = String(value);
    }
    getAttribute(name) { return this.attributes[name] ?? null; }
    get innerHTML() { return this._html || ""; }
    set innerHTML(html) {
      this._html = String(html); this.htmlWrites++;
      for (const child of this.children) child.parentElement = null;
      this.children = [];
      const stack = [this];
      for (const token of this._html.matchAll(/<!--[\s\S]*?-->|<![^>]*>|<\/?([\w-]+)\b([^>]*)>/g)) {
        if (!token[1]) continue;
        const tag = token[1].toLowerCase();
        if (token[0].startsWith("</")) {
          for (let i = stack.length - 1; i > 0; i--) {
            if (stack[i].tagName === tag.toUpperCase()) { stack.length = i; break; }
          }
        } else {
          const child = new Element(tag, token[2]);
          child.parentElement = stack[stack.length - 1]; child.parentElement.children.push(child);
          if (!["input", "img", "br", "hr", "meta", "link", "source", "area", "wbr"].includes(tag) && !/\/\s*>$/.test(token[0])) stack.push(child);
        }
      }
      for (const select of [this, ...this.querySelectorAll("select")].filter(el => el.tagName === "SELECT")) {
        select.value = (select.children.find(c => "selected" in c.attributes) || select.children[0] || {}).value || "";
      }
    }
    get firstElementChild() { return this.children[0] || null; }
    get isConnected() { return this === document || !!this.parentElement?.isConnected; }
    contains(el) { return el === this || this.children.some(c => c.contains(el)); }
    matches(selector) {
      return selector.split(/,\s*/).some(s => {
        if (s.startsWith("#")) return this.id === s.slice(1);
        if (s.startsWith(".")) return this.classList.contains(s.slice(1));
        const attr = s.match(/^\[([^=\]]+)(?:=['"]?([^'"\]]+)['"]?)?\]$/);
        return attr ? attr[1] in this.attributes && (attr[2] === undefined || this.attributes[attr[1]] === attr[2]) : this.tagName.toLowerCase() === s;
      });
    }
    querySelectorAll(selector) {
      const all = this.children.flatMap(c => [c, ...c.querySelectorAll("*")]);
      if (selector === "*") return all;
      const parts = selector.split(/\s+(?![^[]*\])/);
      return all.filter(el => {
        if (!el.matches(parts[parts.length - 1])) return false;
        let ancestor = el.parentElement;
        for (let i = parts.length - 2; i >= 0; i--) {
          while (ancestor && !ancestor.matches(parts[i])) ancestor = ancestor.parentElement;
          if (!ancestor) return false;
          ancestor = ancestor.parentElement;
        }
        return true;
      });
    }
    querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
    closest(selector) { return this.matches(selector) ? this : this.parentElement?.closest(selector) || null; }
    focus() { document.activeElement = this; }
    blur() { if (document.activeElement === this) document.activeElement = null; }
    getBoundingClientRect() { return { height: this.height }; }
  }
  document = new Element("document"); document.hidden = false; document.activeElement = null;
  document.innerHTML = index; document.documentElement = document.querySelector("html");
  document.getElementById = id => document.querySelector("#" + id);
  const listeners = new Map();
  document.addEventListener = (name, fn) => { if (!listeners.has(name)) listeners.set(name, []); listeners.get(name).push(fn); };
  document.dispatch = (name, event) => { for (const fn of listeners.get(name) || []) fn(event); };
  return document;
}

function harness(t) {
  const h = { calls: [], unexpected: [], actions: [], nodes: [], devices: [], plays: {}, queues: {}, intervals: new Map(), observers: [], now: 100000 };
  const document = makeDOM(); let timer = 0;
  const context = {
    console, URL, URLSearchParams, AbortController, document,
    navigator: {}, location: { protocol: "http:", host: "localhost" },
    renderSidebarNav() {}, bootstrapExperience() {},
    setTimeout: () => ++timer, clearTimeout() {},
    setInterval: (fn, ms) => { const id = ++timer; h.intervals.set(id, { fn, ms }); return id; },
    clearInterval: id => h.intervals.delete(id),
    ResizeObserver: class {
      constructor(fn) { this.fn = fn; h.observers.push(this); }
      observe(target) { this.target = target; this.fn([{ target }]); }
    },
    __now: () => h.now,
    __toast: (...args) => h.actions.push(args),
    __request: async (method, route, opts) => {
      const body = opts?.body ? JSON.parse(opts.body) : undefined;
      h.calls.push({ method, route, body });
      if (h.override) { const result = await h.override(method, route, body); if (result !== undefined) return result; }
      const [pathname, query = ""] = route.split("?");
      if (method === "GET") {
        if (pathname === "/nodes") return h.nodes;
        if (pathname === "/playback/devices") return h.devices;
        if (pathname === "/tracks" || pathname === "/playlists") return [];
        if (pathname.startsWith("/tracks/")) return { id: decodeURIComponent(pathname.slice(8)), cover_hash: "cover-" + pathname.slice(8) };
        if (/^\/playlists\/[^/]+$/.test(pathname)) return { id: decodeURIComponent(pathname.slice(11)), name: "Playlist", items: [] };
        if (pathname === "/listening") return { listen_ms: 3000, track_count: 1, album_count: 1, artist_count: 1, unmeasured_plays: 0, genres: [], artists: [], recent_albums: [] };
        const match = pathname.match(/^\/nodes\/([^/]+)\/(play|queue)$/);
        if (match) {
          const id = decodeURIComponent(match[1]), key = new URLSearchParams(query).get("device_key");
          return key
            ? match[2] === "play" ? h.plays[key] || { connected: false } : h.queues[key] || { tracks: [], position: 0 }
            : assert.fail("playback reads must carry device_key");
        }
        if (pathname.endsWith("/stop")) assert.fail("stop must use POST");
      }
      const write = pathname.match(/^\/nodes\/([^/]+)\/(play|stop|pause|resume|seek|volume|sleep|queue|queue\/(prev|next|mode|jump|enqueue|batch))$/);
      if (write && ["POST", "PUT", "DELETE"].includes(method)) {
        const action = write[2], key = new URLSearchParams(query).get("device_key");
        if (["pause", "resume", "stop", "queue/prev", "queue/next", "queue/jump"].includes(action) || (action === "sleep" && method === "DELETE")) assert.ok(key, action + " must carry device_key");
        else assert.ok(body?.device_key, action + " must carry device_key");
        if (action === "queue/jump") assert.equal(body?.device_key, undefined, "queue jump reads its target from the query");
        return {};
      }
      if (method === "POST" && /^\/playlists\/[^/]+\/push$/.test(pathname)) return {};
      h.unexpected.push({ method, route });
      throw new Error("Unexpected request: " + method + " " + route);
    }
  };
  context.window = context; vm.createContext(context);
  for (const file of ["i18n.js", "i18n-app.js", "i18n-features.js", "i18n-settings.js", "i18n-errors.js", "app.js", "settings.js"]) vm.runInContext(fs.readFileSync(path.join(assets, file), "utf8"), context);
  vm.runInContext(fs.readFileSync(path.join(assets, "terminal-center.js"), "utf8"), context);
  vm.runInContext(fs.readFileSync(path.join(assets, "library-browser.js"), "utf8"), context);
  vm.runInContext(`req = __request; toast = __toast; Date.now = __now;
    refreshOverview = async () => {}; refreshLibrary = async () => {}; refreshLibraryLive = async () => {};
    refreshSettings = async () => {}; refreshTerminalCenter = async () => {}; refreshAudit = async () => {};
    shellLibrary = () => '<section id="library-test-view"></section>';
    shellSettings = () => '<section id="settings-test-view"></section>';
    shellAudit = () => '<section id="audit-test-view"></section>';
    globalTick = async () => {};`, context);
  h.run = source => vm.runInContext(source, context);
  h.json = source => JSON.parse(h.run(`JSON.stringify(${source})`));
  h.el = id => document.getElementById(id);
  h.document = document;
  h.node = (id, status = "online") => ({ node_id: id, alias: "终端 " + id, status, authorized: true });
  h.device = (id, changes = {}) => ({ node_id: id, device_key: deviceKey(id), hw: "hw:1", name: "设备 " + id, enabled: true, present: true, online: true, ...changes });
  h.play = (changes = {}) => ({ connected: true, playing: true, state: "playing", track_id: "song-a", session_id: "session-a", title: "Live title", artist: "Live artist", album: "Live album", duration_ms: 180000, rate: 48000, pts: 480000, volume: 60, ...changes });
  h.init = async () => {
    if (!h.devices.length) h.devices = h.nodes.filter(n => n.status === "online").map(n => h.device(n.node_id));
    h.run("initPlayerBar()"); await h.run("refreshPlayerBar()");
  };
  h.tick = async (ms = 1000) => {
    h.now += ms; h.intervals.get(h.run("polls.get('playback')")).fn(); await flush();
  };
  h.live = async (id = "a") => {
    h.nodes = [h.node(id), h.node("b")]; h.devices = [h.device(id), h.device("b")];
    h.plays[deviceKey(id)] = h.play(); h.plays[deviceKey("b")] = h.play({ title: "Target B", track_id: "song-b", session_id: "session-b" });
    h.queues[deviceKey(id)] = { tracks: ["song-a", "song-b"], position: 0 }; h.queues[deviceKey("b")] = { tracks: ["song-b"], position: 0 };
    await h.init();
  };
  h.posts = () => h.calls.filter(c => c.method === "POST");
  h.count = route => h.calls.filter(c => c.route === route).length;
  h.countPath = route => h.calls.filter(c => c.route.split("?")[0] === route).length;
  h.paths = () => h.calls.map(c => c.route.split("?")[0]);
  t.after(() => assert.deepEqual(h.unexpected, [], "all requests must use the mocked API boundary"));
  return h;
}

function assertDisabled(h) {
  for (const id of ["pb-prev", "pb-next", "pb-pause", "pb-seek", "pb-volume"]) assert.equal(h.el(id).disabled, true, id);
  assert.equal(h.run("playerBarConnected()"), false);
}
function beginControl(h, kind, value) {
  const id = kind === "volume" ? "pb-volume" : "pb-seek";
  h.el(id).value = String(value);
  h.run(`onInput({target: $('#${id}')})`);
  return h.run(`CHG['${id}']($('#${id}'))`);
}

test("language changes preserve live playback controls, pending work and selected metadata", async t => {
  const h = harness(t); await h.live();
  h.run("setView('settings'); playerBarBeginRange($('#pb-seek')); PB.volPending=1");
  const seek = h.el("pb-seek"), bar = h.el("player-bar"), timer = h.run("polls.get('playback')");
  seek.value = "45000"; seek.focus();
  const state = h.json("[PB.deviceKey, PB.seq, PB.seekContext, PB.dragging, PB.volPending, S.play['a::hw:1'].track_id, S.play['a::hw:1'].session_id]");
  const calls = h.calls.length;
  for (const locale of ["en", "zh-TW", "zh-CN"]) {
    h.run(`I18n.setPreference('${locale}'); appLanguageChanged()`);
    assert.equal(h.el("player-bar"), bar); assert.equal(h.el("pb-seek"), seek);
    assert.equal(seek.value, "45000"); assert.equal(h.document.activeElement, seek);
    assert.equal(h.run("polls.get('playback')"), timer); assert.equal(h.calls.length, calls);
    assert.deepEqual(h.json("[PB.deviceKey, PB.seq, PB.seekContext, PB.dragging, PB.volPending, S.play['a::hw:1'].track_id, S.play['a::hw:1'].session_id]"), state);
    assert.equal(h.el("pb-title").textContent, "Live title");
    assert.equal(h.el("pb-pause").getAttribute("aria-label"), h.run("appT('m183')"));
  }
});

test("asynchronous server language updates translate the already mounted home without navigation", async t => {
  const h = harness(t); await h.live(); h.run("setView('overview')");
  const view = h.el("view"), filter = h.el("ov-stat-node"), root = h.el("player-bar");
  const epoch = h.run("OV.epoch"), count = h.calls.length, timer = h.run("polls.get('view:main')");
  h.run("I18n.setDefault('en'); appLanguageChanged()");
  assert.equal(h.el("view"), view); assert.equal(h.el("ov-stat-node"), filter); assert.equal(h.el("player-bar"), root);
  assert.equal(h.el("ov-greeting").textContent, "Home");
  assert.equal(h.document.querySelector(".ov-scope").textContent, "ZAT pushes only · Listening across terminals");
  assert.equal(h.run("OV.epoch"), epoch); assert.equal(h.calls.length, count);
  assert.equal(h.run("polls.get('view:main')"), timer);
});

test("index owns a single player root beside view; every view shell excludes it", async t => {
  const h = harness(t), root = h.el("player-bar-root"), view = h.el("view");
  assert.equal(h.document.querySelectorAll("#player-bar-root").length, 1);
  assert.equal(root.parentElement, view.parentElement);
  assert.equal(view.contains(root), false);
  await h.live();
  for (const v of ["overview", "library", "nodes", "audit", "settings"]) {
    const shell = h.run(`shellFor('${v}')`);
    assert.doesNotMatch(shell, /id="(?:player-bar(?:-root)?|pb-[^"]+|ov-player[^"]*)"/);
    h.run(`setView('${v}')`);
    assert.equal(h.document.querySelectorAll("#player-bar").length, 1);
    assert.equal(h.el("player-bar-root"), root);
    assert.equal(root.htmlWrites, 1, "navigation must not remount the dock");
  }
  for (const el of root.querySelectorAll("[id]")) assert.equal(root.querySelectorAll("#" + el.id).length, 1, el.id);
});

test("boot mounts immediately, measures responsive height, and installs one global playback timer", async t => {
  const h = harness(t); h.nodes = [h.node("a")]; h.devices = [h.device("a")]; h.plays[deviceKey("a")] = h.play();
  h.run("boot()"); await h.run("refreshPlayerBar()");
  assert.equal(h.run("PB.deviceKey"), deviceKey("a"));
  assert.equal(h.count(deviceRoute("a", "play")), 1);
  const timer = h.run("polls.get('playback')");
  assert.equal(h.intervals.get(timer).ms, 1000);
  assert.equal(h.run("polls.has('player:poll')"), false);
  assert.equal(h.observers.length, 1);
  assert.equal(h.observers[0].target, h.el("player-bar"));
  assert.equal(h.document.documentElement.style["--player-bar-height"], "113px");
  h.el("player-bar").height = 164.1; h.observers[0].fn([{ target: h.el("player-bar") }]);
  assert.equal(h.document.documentElement.style["--player-bar-height"], "165px");
});

test("boot pointer events capture seek context, preserve it through release, and cancel abandoned drags", async t => {
  const h = harness(t); h.nodes = [h.node("a")]; h.devices = [h.device("a")]; h.plays[deviceKey("a")] = h.play();
  h.run("boot()"); await h.run("refreshPlayerBar()");
  const seek = h.el("pb-seek"); seek.focus();
  h.document.dispatch("pointerdown", { target: seek });
  assert.equal(h.run("PB.dragging"), true);
  assert.deepEqual(h.json("PB.seekContext"), { deviceKey: "a::hw:1", nodeId: "a", track: "song-a", session: "session-a" });
  h.document.dispatch("pointerup", { target: seek });
  assert.equal(h.run("PB.dragging"), false); assert.equal(h.run("PB.seekContext.session"), "session-a");
  seek.value = "23000"; await h.run("CHG['pb-seek']($('#pb-seek'))");
  assert.deepEqual(h.posts()[0].body, { position_ms: 23000, device_key: "a::hw:1" });
  h.document.dispatch("pointerdown", { target: seek });
  h.document.dispatch("pointercancel", { target: seek });
  assert.equal(h.run("PB.dragging"), false); assert.equal(h.run("PB.seekContext"), null); assert.equal(h.document.activeElement, null);
  h.document.dispatch("pointerdown", { target: seek });
  h.document.dispatch("focusout", { target: seek });
  assert.equal(h.run("PB.dragging"), false); assert.equal(h.run("PB.seekContext"), null);
  assert.equal(h.posts().length, 1, "cancel/focusout must never issue a seek");
});

test("real navigation preserves dock DOM, target, focus, drag context and pending state in every view", async t => {
  const h = harness(t); await h.live();
  const bar = h.el("player-bar"), seek = h.el("pb-seek"), volume = h.el("pb-volume"), term = h.el("pb-term-wrap");
  seek.focus(); seek.value = "45000"; volume.value = "24";
  h.run("playerBarBeginRange($('#pb-seek')); playerBarBeginRange($('#pb-volume')); PB.seekPending=1; PB.volPending=2");
  const state = h.json("PB"), timer = h.run("polls.get('playback')");
  for (const v of ["library", "nodes", "audit", "settings", "overview"]) {
    h.run(`setView('${v}')`); await h.tick();
    assert.equal(h.run("S.view"), v === "nodes" ? "settings" : v);
    assert.equal(h.el("player-bar"), bar); assert.equal(h.el("pb-seek"), seek); assert.equal(h.el("pb-term-wrap"), term);
    for (const field of ["deviceKey", "nodeId", "chosen", "seq", "dragging", "volDrag", "seekContext", "seekPending", "volPending"]) assert.deepEqual(h.json(`PB.${field}`), state[field], `${v}: ${field}`);
    assert.equal(h.document.activeElement, seek); assert.equal(seek.value, "45000"); assert.equal(volume.value, "24");
    assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.el("pb-node-label").textContent, "设备 a"); assert.equal(h.run("polls.get('playback')"), timer);
  }
});

test("statistical terminal/date filters and playback target are independent in both directions", async t => {
  const h = harness(t); await h.live();
  h.run("setView('overview')"); await flush();
  await h.run("CHG['ov-stat-node']({value:'stats-only'})");
  h.run("ACT['ov-days']({days:'30'})"); await flush();
  assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.run("PB.seq"), 0);
  assert.ok(h.calls.some(c => c.route === "/listening?days=30&node_id=stats-only"));
  const data = h.json("OV.data"), before = h.count("/listening?days=30&node_id=stats-only");
  await h.run("ACT['pb-device-pick']({deviceKey:'b::hw:1'})");
  assert.equal(h.run("OV.node"), "stats-only"); assert.equal(h.run("OV.days"), 30); assert.deepEqual(h.json("OV.data"), data);
  assert.equal(h.count("/listening?days=30&node_id=stats-only"), before);
  h.run("S.nodes=[{node_id:'stats-list'}]; cleanupOverview()"); await h.run("refreshPlayerBar()");
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.deepEqual(h.json("S.nodes"), [{ node_id: "stats-list" }]);
  assert.deepEqual(h.json("PB.nodes.map(n=>n.node_id)"), ["a", "b"]);
});

test("same-target refresh returns one shared promise and reads discovery only every five seconds", async t => {
  const h = harness(t); await h.live(); h.calls.length = 0;
  const pending = deferred(); h.override = (method, route) => route === deviceRoute("a", "play") ? pending.promise : undefined;
  const first = h.run("refreshPlayerBar()"), second = h.run("refreshPlayerBar()");
  assert.equal(first, second); assert.equal(h.count(deviceRoute("a", "play")), 1); assert.equal(h.count(deviceRoute("a", "queue")), 1);
  assert.equal(h.count("/nodes"), 0); assert.equal(h.count("/playback/devices"), 0);
  pending.resolve(h.play()); await first; h.override = null;
  for (let i = 0; i < 4; i++) await h.tick();
  assert.equal(h.count("/nodes"), 0); assert.equal(h.count("/playback/devices"), 0); assert.equal(h.count(deviceRoute("a", "play")), 5);
  await h.tick(); assert.equal(h.count("/nodes"), 1); assert.equal(h.count("/playback/devices"), 1); assert.equal(h.count(deviceRoute("a", "play")), 6);
  assert.ok(h.calls.every(c => ["/nodes", "/playback/devices", deviceRoute("a", "play"), deviceRoute("a", "queue")].includes(c.route)));
});

test("hidden tabs receive initial paint but pause subsequent playback ticks and resume on visibility", async t => {
  const h = harness(t); h.document.hidden = true; await h.live();
  assert.equal(h.count(deviceRoute("a", "play")), 1);
  h.calls.length = 0; await h.tick(6000); await h.tick(); assert.equal(h.calls.length, 0);
  h.document.hidden = false; await h.tick();
  assert.deepEqual(h.calls.map(c => c.route), ["/playback/devices", "/nodes", deviceRoute("a", "play"), deviceRoute("a", "queue")]);
  assert.equal(h.el("pb-title").textContent, "Live title");
});

test("background playback reads selected playback and track metadata, never the whole library or listening statistics", async t => {
  const h = harness(t); await h.live();
  for (const view of ["library", "nodes", "audit", "settings"]) {
    h.run(`setView('${view}')`); await h.tick();
  }
  assert.ok(h.calls.every(c => c.method === "GET" && ["/nodes", "/playback/devices", deviceRoute("a", "play"), deviceRoute("a", "queue"), "/tracks/song-a"].includes(c.route)));
  assert.equal(h.count("/tracks/song-a"), 1);
  assert.match(h.el("pb-cover").innerHTML, /cover-song-a/);
  assert.equal(h.run("S.tracks.length"), 0);
  assert.equal(h.el("pb-title").textContent, "Live title"); assert.equal(h.el("pb-sub").textContent, "Live artist · Live album");
  assert.equal(h.el("pb-total").textContent, "3:00"); assert.equal(h.el("pb-seek").value, "10000");
});

test("late discovery from an old target cannot replace nodes or the explicit new target", async t => {
  const h = harness(t), pending = deferred();
  h.nodes = [h.node("a"), h.node("b")]; h.devices = [h.device("a"), h.device("b")];
  h.plays[deviceKey("a")] = h.play({ title: "Target A" }); h.plays[deviceKey("b")] = h.play({ title: "Target B" });
  await h.init();
  let reads = 0; h.override = (_, route) => route === "/nodes" && ++reads === 1 ? pending.promise : undefined;
  h.run("PB.nodesAt=0"); const old = h.run("refreshPlayerBar()");
  await flush();
  assert.equal(h.run("PB.deviceKey"), deviceKey("a"));
  await h.run("selectPlayerBar('b::hw:1')");
  pending.resolve([h.node("obsolete")]); await old;
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.deepEqual(h.json("PB.nodes.map(n=>n.node_id)"), ["a", "b"]);
  assert.equal(h.el("pb-title").textContent, "Target B"); assert.equal(h.count("/nodes/obsolete/play"), 0);
});

test("late play and queue from a previous target cannot overwrite data or release a newer single-flight", async t => {
  const h = harness(t); await h.live();
  const oldPlay = deferred(), oldQueue = deferred(), newPlay = deferred();
  h.override = (_, route) => ({ [deviceRoute("a", "play")]: oldPlay.promise, [deviceRoute("a", "queue")]: oldQueue.promise, [deviceRoute("b", "play")]: newPlay.promise })[route];
  const old = h.run("refreshPlayerBar()"), current = h.run("selectPlayerBar('b::hw:1')"); await flush();
  const busy = h.run("PB.busy");
  oldPlay.resolve(h.play({ title: "Stale response" })); oldQueue.resolve({ tracks: ["stale"], position: 0 }); await old;
  assert.equal(h.run("PB.busy"), busy); assert.equal(h.run("refreshPlayerBar()"), busy);
  assert.equal(h.run("S.play['a::hw:1'].title"), "Live title"); assert.deepEqual(h.json("S.nodeQueue['a::hw:1'].tracks"), ["song-a", "song-b"]);
  newPlay.resolve(h.plays[deviceKey("b")]); await current;
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.equal(h.el("pb-title").textContent, "Target B"); assert.equal(h.run("PB.busy"), null);
});

test("initial selection prefers an available device and never silently changes an explicit empty target", async t => {
  const h = harness(t); h.nodes = [h.node("off", "offline"), h.node("a")]; h.plays[deviceKey("a")] = h.play(); await h.init();
  assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.count("/nodes/off/play"), 0);
  await h.run("selectPlayerBar('')"); h.calls.length = 0; await h.tick(6000);
  assert.equal(h.run("PB.deviceKey"), ""); assert.equal(h.run("PB.chosen"), true);
  assert.match(h.el("pb-state").textContent, /未选择/); assertDisabled(h);
  assert.deepEqual(h.calls.map(c => c.route), ["/playback/devices", "/nodes"]);
});

test("no available devices stays disabled and discovery can later select the first device", async t => {
  const h = harness(t); await h.init(); assertDisabled(h);
  assert.match(h.el("pb-state").textContent, /暂无可用播放设备/); assert.equal(h.run("PB.chosen"), false);
  h.nodes = [h.node("a")]; h.devices = [h.device("a")]; h.plays[deviceKey("a")] = h.play(); await h.tick(5000);
  assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.run("playerBarConnected()"), true);
});

test("offline terminals expose no devices and never trigger playback reads", async t => {
  const h = harness(t); h.nodes = [h.node("a", "offline"), h.node("b", "offline")]; await h.init();
  assert.equal(h.run("PB.deviceKey"), ""); assertDisabled(h);
  assert.deepEqual(h.calls.map(c => c.route), ["/playback/devices", "/nodes"]);
});

test("offline, removed and reconnected targets retain identity without falling back to another available device", async t => {
  const h = harness(t); await h.live();
  for (const [nodes, devices, status] of [
    [[h.node("a", "offline"), h.node("b")], [h.device("a", { online: false }), h.device("b")], /离线/],
    [[h.node("b")], [h.device("b")], /已移除/],
    [[], [], /已移除/]
  ]) {
    h.nodes = nodes; h.devices = devices; h.calls.length = 0; await h.tick(5000);
    assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.match(h.el("pb-state").textContent, status); assertDisabled(h);
    assert.deepEqual(h.calls.map(c => c.route), ["/playback/devices", "/nodes"]);
  }
  h.nodes = [h.node("b"), h.node("a")]; h.devices = [h.device("b"), h.device("a")]; h.plays[deviceKey("a")] = h.play({ title: "Reconnected" }); await h.tick(5000);
  assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.el("pb-title").textContent, "Reconnected"); assert.equal(h.run("playerBarConnected()"), true);
});

test("device discovery failure shows an error rather than an empty list and can recover", async t => {
  const h = harness(t);
  h.override = (_, route) => { if (route === "/playback/devices") throw new Error("unavailable"); };
  await h.init(); assertDisabled(h);
  assert.match(h.el("pb-state").textContent, /播放设备加载失败/);
  assert.doesNotMatch(h.el("pb-state").textContent, /暂无可用播放设备/); assert.equal(h.run("PB.chosen"), false);
  assert.equal(h.run("PB.busy"), null);
  h.override = null; h.nodes = [h.node("a")]; h.devices = [h.device("a")]; h.plays[deviceKey("a")] = h.play(); await h.tick(5000);
  assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.run("playerBarConnected()"), true);
});

test("node metadata failure does not block available-device playback and recovers on the next discovery tick", async t => {
  const h = harness(t); await h.live(); h.calls.length = 0;
  h.override = (_, route) => { if (route === "/nodes") throw new Error("metadata failed"); };
  await h.tick(5000); assert.equal(h.run("playerBarConnected()"), true); assert.equal(h.run("PB.deviceKey"), deviceKey("a"));
  assert.equal(h.count(deviceRoute("a", "play")), 1);
  await h.tick(); assert.equal(h.count("/nodes"), 1, "failed discovery must still be throttled");
  h.override = null; await h.tick(4000); assert.equal(h.run("playerBarConnected()"), true);
});

test("failed play fetch and a disconnected ZAT cannot leave stale controls enabled or switch targets", async t => {
  const h = harness(t); await h.live();
  h.override = (_, route) => { if (route === deviceRoute("a", "play")) throw new Error("play failed"); };
  await h.tick(); assertDisabled(h); assert.match(h.el("pb-state").textContent, /播放状态暂不可用/);
  assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.run("S.play['a::hw:1'].connected"), false);
  h.override = null; h.plays[deviceKey("a")] = h.play({ connected: false }); await h.tick();
  assertDisabled(h); assert.match(h.el("pb-state").textContent, /ZAT 未连接/);
  h.plays[deviceKey("a")] = h.play(); await h.tick(); assert.equal(h.run("playerBarConnected()"), true);
});

test("a failed queue fetch clears stale queue controls without hiding valid playback", async t => {
  const h = harness(t); await h.live();
  h.override = (_, route) => { if (route === deviceRoute("a", "queue")) throw new Error("queue failed"); };
  await h.tick();
  assert.equal(h.el("pb-prev").disabled, true); assert.equal(h.el("pb-next").disabled, true);
  assert.equal(h.el("pb-pause").disabled, false); assert.equal(h.run("S.nodeQueue['a::hw:1']"), null);
  assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.el("pb-title").textContent, "Live title");
  h.override = null; await h.tick(); assert.equal(h.el("pb-next").disabled, false);
});

test("focused or dragged ranges and focused terminal select are not overwritten by live paint", async t => {
  const h = harness(t); await h.live();
  for (const id of ["pb-seek", "pb-volume"]) {
    const el = h.el(id); el.focus(); el.value = "user-input"; const writes = el.htmlWrites;
    h.run("paintPlayerBar()"); assert.equal(el.value, "user-input", id); assert.equal(el.htmlWrites, writes, id); el.blur();
  }
  h.el("pb-seek").value = "31000"; h.run("onInput({target: $('#pb-seek')})");
  h.el("pb-volume").value = "31"; h.run("onInput({target: $('#pb-volume')})");
  await h.tick();
  assert.equal(h.el("pb-seek").value, "31000"); assert.equal(h.el("pb-elapsed").textContent, "0:31");
  assert.equal(h.el("pb-volume").value, "31"); assert.equal(h.el("pb-volume-value").textContent, "31");
  assert.deepEqual(h.json("PB.seekContext"), { deviceKey: "a::hw:1", nodeId: "a", track: "song-a", session: "session-a" });
});

for (const changed of ["track_id", "session_id"]) {
  test(`seek captures ${changed} at drag start and refuses to seek after it changes`, async t => {
    const h = harness(t); await h.live();
    h.run("playerBarBeginRange($('#pb-seek'))");
    h.plays[deviceKey("a")] = h.play({ [changed]: "changed" }); await h.tick();
    h.run("onInput({target: $('#pb-seek')})");
    h.el("pb-seek").value = "50000"; await h.run("CHG['pb-seek']($('#pb-seek'))");
    assert.equal(h.posts().length, 0); assert.equal(h.run("PB.seekPending"), 0);
    assert.equal(h.run("PB.dragging"), false); assert.equal(h.run("PB.seekContext"), null);
    assert.match(h.actions[0][0], /曲目已切换/);
  });
}

test("seek without captured context or with a disabled/disconnected control never writes", async t => {
  const h = harness(t); await h.live();
  await h.run("CHG['pb-seek']($('#pb-seek'))"); assert.equal(h.posts().length, 0);
  h.el("pb-seek").disabled = true; h.run("playerBarBeginRange($('#pb-seek'))");
  assert.equal(h.run("PB.seekContext"), null); await h.run("CHG['pb-seek']($('#pb-seek'))");
  h.plays[deviceKey("a")] = h.play({ connected: false }); await h.tick();
  await beginControl(h, "seek", 45000); await beginControl(h, "volume", 40);
  assert.equal(h.posts().length, 0);
});

test("a seek started before navigation still targets the captured track session after navigation", async t => {
  const h = harness(t); await h.live(); h.el("pb-seek").value = "45000";
  h.run("playerBarBeginRange($('#pb-seek')); setView('library')");
  await h.run("CHG['pb-seek']($('#pb-seek'))");
  assert.deepEqual(h.posts(), [{ method: "POST", route: "/nodes/a/seek", body: { position_ms: 45000, device_key: "a::hw:1" } }]);
  assert.equal(h.run("S.view"), "library"); assert.equal(h.run("PB.seekPending"), 0);
});

for (const [kind, value, field, body] of [
  ["seek", 42000, "seekPending", { position_ms: 42000 }],
  ["volume", 35, "volPending", { volume: 35 }]
]) {
  test(`${kind} routes to the selected terminal and its pending completion survives every view`, async t => {
    const h = harness(t); await h.live("room/a b"); const pending = deferred();
    h.override = (method, route) => method === "POST" && route.endsWith("/" + kind) ? pending.promise : undefined;
    const work = beginControl(h, kind, value);
    assert.equal(h.run(`PB.${field}`), 1);
    assert.deepEqual(h.posts(), [{ method: "POST", route: `/nodes/${encodeURIComponent("room/a b")}/${kind}`, body: { ...body, device_key: deviceKey("room/a b") } }]);
    const el = h.el("pb-" + kind);
    for (const view of ["library", "nodes", "audit", "settings", "overview"]) {
      h.run(`setView('${view}')`); await h.tick();
      assert.equal(h.run(`PB.${field}`), 1); assert.equal(el.value, String(value)); assert.equal(el.isConnected, true);
    }
    pending.resolve({}); await work;
    assert.equal(h.run(`PB.${field}`), 0);
    assert.equal(h.run("PB.deviceKey"), deviceKey("room/a b"));
  });

  test(`late ${kind} completion cannot decrement the new target's pending request or refresh it`, async t => {
    const h = harness(t); await h.live(); const oldRequest = deferred(), newRequest = deferred();
    h.override = (method, route) => method === "POST" ? (route === `/nodes/a/${kind}` ? oldRequest.promise : newRequest.promise) : undefined;
    const oldWork = beginControl(h, kind, value); const oldSeq = h.run("PB.seq");
    await h.run("selectPlayerBar('b::hw:1')");
    assert.equal(h.run("PB.seq"), oldSeq + 1); assert.equal(h.run(`PB.${field}`), 0);
    assert.equal(h.run("PB.seekContext"), null); assert.equal(h.run("PB.dragging || PB.volDrag"), false);
    const newWork = beginControl(h, kind, value); const count = h.calls.length;
    oldRequest.resolve({}); await oldWork;
    assert.equal(h.run(`PB.${field}`), 1); assert.equal(h.calls.length, count);
    assert.equal(h.run("PB.deviceKey"), deviceKey("b"));
    newRequest.resolve({}); await newWork; assert.equal(h.run(`PB.${field}`), 0);
    assert.deepEqual(h.posts().map(c => c.route), [`/nodes/a/${kind}`, `/nodes/b/${kind}`]);
  });

  test(`${kind} request failure reports the error and clears only its own pending state`, async t => {
    const h = harness(t); await h.live();
    h.override = method => { if (method === "POST") throw new Error(kind + " rejected"); };
    await beginControl(h, kind, value);
    assert.equal(h.run(`PB.${field}`), 0);
    assert.ok(h.actions.some(a => a[0] === kind + " rejected" && a[1] === "err"));
    assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.run("playerBarConnected()"), true);
  });
}

test("selecting the same target shares in-flight refresh without clearing drag or pending state", async t => {
  const h = harness(t); await h.live(); const pending = deferred();
  h.run("playerBarBeginRange($('#pb-seek')); PB.volPending=1");
  h.override = (_, route) => route === deviceRoute("a", "play") ? pending.promise : undefined;
  const first = h.run("refreshPlayerBar()"), seq = h.run("PB.seq"), context = h.json("PB.seekContext");
  assert.equal(h.run("selectPlayerBar('a::hw:1')"), first);
  assert.equal(h.run("PB.seq"), seq); assert.equal(h.run("PB.dragging"), true); assert.deepEqual(h.json("PB.seekContext"), context);
  assert.equal(h.run("PB.volPending"), 1);
  pending.resolve(h.play()); await first;
});

test("volume clamps to the accepted range without starting a new track", async t => {
  const h = harness(t); await h.live();
  await beginControl(h, "volume", 150); await beginControl(h, "volume", -20);
  assert.deepEqual(h.posts().map(c => [c.route, c.body]), [["/nodes/a/volume", { volume: 100, device_key: deviceKey("a") }], ["/nodes/a/volume", { volume: 0, device_key: deviceKey("a") }]]);
  assert.equal(h.run("S.play['a::hw:1'].track_id"), "song-a");
});

test("transport pause/resume and previous/next actions use the dock's selected terminal in other views", async t => {
  const h = harness(t); await h.live(); h.run("setView('audit')");
  assert.equal(h.el("pb-pause").getAttribute("aria-label"), "暂停");
  await h.run("ACT['play-pause']($('#pb-pause').dataset)");
  h.plays[deviceKey("a")] = h.play({ playing: false, state: "paused" }); await h.tick();
  assert.equal(h.el("pb-pause").disabled, false); assert.equal(h.el("pb-pause").getAttribute("aria-label"), "继续播放");
  await h.run("ACT['play-pause']($('#pb-pause').dataset)");
  await h.run("ACT['queue-prev']($('#pb-prev').dataset)"); await h.run("ACT['queue-next']($('#pb-next').dataset)");
  assert.deepEqual(h.posts().map(c => c.route), [deviceRoute("a", "pause"), deviceRoute("a", "resume"), deviceRoute("a", "queue/prev"), deviceRoute("a", "queue/next")]);
  assert.equal(h.run("S.view"), "audit"); assert.equal(h.count("/tracks"), 0);
  h.queues[deviceKey("a")] = { tracks: ["song-a", "song-b"], position: 1 }; await h.tick();
  assert.equal(h.el("pb-prev").disabled, false); assert.equal(h.el("pb-next").disabled, true);
  h.plays[deviceKey("a")] = h.play({ playing: false, state: "idle" }); await h.tick();
  assert.equal(h.el("pb-pause").disabled, true); assert.equal(h.el("pb-seek").disabled, true); assert.equal(h.el("pb-volume").disabled, true);
  const before = h.posts().length; await h.run("playPause(PB.deviceKey)"); assert.equal(h.posts().length, before);
});

test("terminal detail opens the selected console in place; closing it keeps the dock and its poll alive", async t => {
  const h = harness(t); await h.live(); h.run("setView('nodes'); openDrawer('Terminal', '', '', '', {ctx:{kind:'node',id:'b'}})");
  const bar = h.el("player-bar"), timer = h.run("polls.get('playback')");
  await h.run("ACT['tc-device-player']({id:'b',deviceKey:'b::hw:1'})");
  assert.equal(h.run("S.view"), "settings"); assert.equal(h.run("drawerCtx"), null); assert.equal(h.el("drawer-root").firstElementChild, null);
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.equal(h.run("PLY.deviceKey"), deviceKey("b")); assert.ok(h.el("ply-body"));
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.equal(h.run("polls.has('player:poll')"), false);
  h.run("closeModal()"); assert.equal(h.el("ply-body"), null); assert.equal(h.run("PLY.deviceKey"), "");
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.equal(h.el("player-bar"), bar); assert.equal(h.run("polls.get('playback')"), timer);
  h.plays[deviceKey("b")] = h.play({ title: "Still playing" }); await h.tick(); assert.equal(h.el("pb-title").textContent, "Still playing");
});

test("console and dock reuse a pending playback read and every subsequent poll", async t => {
  const h = harness(t); await h.live(); h.run("setView('library')"); h.calls.length = 0;
  const pending = deferred(); h.override = (_, route) => route === deviceRoute("a", "play") ? pending.promise : undefined;
  const refresh = h.run("refreshPlayerBar()"), consoleWork = h.run("openPlayer('a::hw:1')");
  assert.equal(h.count(deviceRoute("a", "play")), 1); assert.equal(h.count(deviceRoute("a", "queue")), 1);
  pending.resolve(h.play()); await Promise.all([refresh, consoleWork]); h.override = null;
  assert.equal(h.run("S.view"), "library"); assert.equal(h.run("polls.has('player:poll')"), false);
  h.calls.length = 0; await h.tick();
  assert.deepEqual(h.calls.map(c => c.route), [deviceRoute("a", "play"), deviceRoute("a", "queue")]);
  assert.equal(h.el("pb-title").textContent, "Live title"); assert.equal(h.el("ply-title").innerHTML, "Live title");
});

test("closing a console before playback resolves never reopens it or clears the global target", async t => {
  const h = harness(t); await h.live(); const pending = deferred();
  h.override = (_, route) => route === deviceRoute("a", "play") ? pending.promise : undefined;
  const work = h.run("openPlayer('a::hw:1')"); const root = h.el("ply-body"); h.run("closeModal()");
  pending.resolve(h.play()); await work;
  assert.equal(h.el("ply-body"), null); assert.equal(root.isConnected, false); assert.equal(h.run("PLY.deviceKey"), "");
  assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.el("pb-title").textContent, "Live title");
});

test("reopening the same target while loading cannot paint the detached earlier console root", async t => {
  const h = harness(t); await h.live(); const pending = deferred();
  h.override = (_, route) => route === deviceRoute("a", "play") ? pending.promise : undefined;
  const first = h.run("openPlayer('a::hw:1')"), firstRoot = h.el("ply-body");
  const second = h.run("openPlayer('a::hw:1')"), secondRoot = h.el("ply-body"); const writes = firstRoot.htmlWrites;
  assert.notEqual(firstRoot, secondRoot); assert.equal(firstRoot.isConnected, false);
  pending.resolve(h.play()); await Promise.all([first, second]);
  assert.equal(h.el("ply-body"), secondRoot); assert.equal(firstRoot.htmlWrites, writes);
  assert.equal(secondRoot.htmlWrites, 1); assert.equal(h.run("PLY.deviceKey"), deviceKey("a"));
  assert.equal(h.run("polls.has('player:poll')"), false);
});

test("late optional console library data cannot repaint a newer same-target modal", async t => {
  const h = harness(t); await h.live(); const pending = deferred(); let reads = 0;
  h.override = (_, route) => route === "/tracks" && ++reads === 1 ? pending.promise : undefined;
  const first = h.run("openPlayer('a::hw:1')"); await flush(); const oldRoot = h.el("ply-body");
  const second = h.run("openPlayer('a::hw:1')"); await second; const currentRoot = h.el("ply-body"), writes = currentRoot.htmlWrites;
  pending.resolve([]); await first;
  assert.equal(h.el("ply-body"), currentRoot); assert.equal(currentRoot.htmlWrites, writes); assert.equal(oldRoot.isConnected, false);
  assert.equal(h.run("PLY.deviceKey"), deviceKey("a"));
});

test("console terminal switch synchronizes the global dock without navigating or adding a timer", async t => {
  const h = harness(t); await h.live(); h.run("setView('settings')"); await h.run("openPlayer('a::hw:1')");
  const timer = h.run("polls.get('playback')");
  await h.run("ACT['ply-device-pick']({deviceKey:'b::hw:1'})");
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.equal(h.run("PLY.deviceKey"), deviceKey("b"));
  assert.equal(h.el("ply-pp").dataset.id, deviceKey("b")); assert.equal(h.el("ply-node-label").textContent, "设备 b");
  assert.equal(h.run("S.view"), "settings"); assert.equal(h.run("polls.get('playback')"), timer); assert.equal(h.run("polls.has('player:poll')"), false);
});

test("offline or removed console targets disable live controls but preserve stop for the known session", async t => {
  const h = harness(t); await h.live(); await h.run("openPlayer('a::hw:1')");
  for (const [nodes, devices] of [
    [[h.node("a", "offline"), h.node("b")], [h.device("a", { online: false }), h.device("b")]],
    [[h.node("b")], [h.device("b")]]
  ]) {
    h.nodes = nodes; h.devices = devices; await h.tick(5000);
    assert.equal(h.run("S.play['a::hw:1'].connected"), true, "exercise stale cached playback, not a new disconnected response");
    assert.equal(h.run("PB.deviceKey"), deviceKey("a")); assert.equal(h.run("PLY.deviceKey"), deviceKey("a")); assertDisabled(h);
    for (const id of ["ply-prev", "ply-next", "ply-pp", "ply-seek", "ply-volume"]) assert.equal(h.el(id).disabled, true, id);
    assert.equal(h.el("ply-stop").disabled, false, "stop must remain available after unplug or deactivation");
    assert.equal(h.el("ply-term-wrap").dataset.disabled, "0");
    assert.match(h.el("ply-term-menu").innerHTML, /b::hw:1/);
    assert.doesNotMatch(h.el("ply-term-menu").innerHTML, /a::hw:1/);
  }
  h.nodes = [h.node("a"), h.node("b")]; h.devices = [h.device("a"), h.device("b")]; await h.tick(5000);
  assert.equal(h.el("ply-pp").disabled, false); assert.equal(h.el("ply-next").disabled, false); assert.equal(h.el("ply-term-wrap").dataset.disabled, "0");
});

test("a status read that finishes after hot-removal preserves the session needed by stop", async t => {
  const h = harness(t); await h.live(); await h.run("openPlayer('a::hw:1')"); const pending = deferred();
  h.override = (_, route) => route === deviceRoute("a", "play") ? pending.promise : undefined;
  const refresh = h.run("refreshPlayerBar()"); await flush();
  h.nodes = [h.node("a", "offline"), h.node("b")]; h.devices = [h.device("b")]; h.run("PB.devices=[]; PB.registry=[];");
  pending.resolve(null); await refresh;
  assert.equal(h.run("S.play['a::hw:1'].connected"), false);
  assert.equal(h.run("S.play['a::hw:1'].playing"), false);
  assert.equal(h.run("S.play['a::hw:1'].track_id"), "song-a");
  assert.equal(h.run("S.play['a::hw:1'].session_id"), "session-a");
  assertDisabled(h);
  assert.equal(h.el("ply-stop").disabled, false, "stop must survive an in-flight status failure");
});

test("stop posts the stable device key without requiring current activation or presence", async t => {
  const h = harness(t); await h.live(); await h.run("openPlayer('a::hw:1')");
  h.nodes = [h.node("a", "offline")]; h.devices = []; await h.tick(5000);
  h.run("syncPlayUI = async () => {}"); h.calls.length = 0; h.actions.length = 0;
  await h.run("playStop('a::hw:1')");
  assert.deepEqual(h.calls, [{ method: "POST", route: deviceRoute("a", "stop"), body: {} }]);
  assert.equal(h.actions.length, 1);
});

test("late playback for an earlier console target cannot replace a newly opened target", async t => {
  const h = harness(t); await h.live(); const pending = deferred();
  h.override = (_, route) => route === deviceRoute("a", "play") ? pending.promise : undefined;
  const oldWork = h.run("openPlayer('a::hw:1')"), oldRoot = h.el("ply-body");
  await h.run("openPlayer('b::hw:1')"); const currentRoot = h.el("ply-body");
  pending.resolve(h.play({ title: "Late A" })); await oldWork;
  assert.equal(h.el("ply-body"), currentRoot); assert.equal(oldRoot.isConnected, false);
  assert.equal(h.run("PLY.deviceKey"), deviceKey("b")); assert.equal(h.run("PB.deviceKey"), deviceKey("b"));
  assert.equal(h.el("ply-title").innerHTML, "Target B"); assert.equal(h.el("pb-title").textContent, "Target B");
  assert.equal(h.el("ply-pp").dataset.id, deviceKey("b")); assert.equal(h.run("polls.has('player:poll')"), false);
});

test("switching the dock while the console is open synchronizes its target and command routing", async t => {
  const h = harness(t); await h.live(); await h.run("openPlayer('a::hw:1')");
  await h.run("ACT['pb-device-pick']({deviceKey:'b::hw:1'})");
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.equal(h.run("PLY.deviceKey"), deviceKey("b"));
  assert.equal(h.el("ply-pp").dataset.id, deviceKey("b"));
  assert.equal(h.el("ply-title").innerHTML, "Target B");
  await h.run("ACT['play-pause']($('#ply-pp').dataset)");
  assert.deepEqual(h.posts().map(c => c.route), [deviceRoute("b", "pause")]);
});

test("clearing the global target closes an open console instead of leaving commands bound to the old terminal", async t => {
  const h = harness(t); await h.live(); await h.run("openPlayer('a::hw:1')");
  await h.run("ACT['pb-device-pick']({deviceKey:''})");
  assert.equal(h.run("PB.deviceKey"), ""); assert.equal(h.run("PLY.deviceKey"), "");
  assert.equal(h.el("ply-body"), null); assertDisabled(h);
});

test("current-track metadata uses the library cache or one targeted read per track change", async t => {
  const h = harness(t); h.nodes = [h.node("a")]; h.plays[deviceKey("a")] = h.play();
  h.run("S.tracks=[{id:'song-a',cover_hash:'cached-art'}]"); await h.init();
  assert.equal(h.count("/tracks/song-a"), 0); assert.match(h.el("pb-cover").innerHTML, /cached-art/);
  h.run("S.tracks=[]"); h.plays[deviceKey("a")] = h.play({track_id:"new/song", session_id:"new-session"});
  await h.tick(); await h.tick();
  assert.equal(h.count("/tracks/new%2Fsong"), 1); assert.equal(h.run("PB.track.id"), "new/song");
  assert.equal(h.count("/tracks"), 0); assert.equal(h.run("S.tracks.length"), 0);
});

test("missing current-track metadata leaves live controls available without retrying every tick", async t => {
  const h = harness(t); h.nodes = [h.node("a")]; h.plays[deviceKey("a")] = h.play();
  h.override = (_, route) => { if (route === "/tracks/song-a") throw new Error("track removed"); };
  await h.init(); await h.tick();
  assert.equal(h.count("/tracks/song-a"), 1); assert.equal(h.run("playerBarConnected()"), true);
  assert.equal(h.el("pb-title").textContent, "Live title"); assert.equal(h.el("pb-pause").disabled, false);
  assert.equal(h.run("PB.track"), null);
});

test("late current-track metadata cannot replace a newly selected terminal's cover", async t => {
  const h = harness(t); await h.live(); const pending = deferred();
  h.plays[deviceKey("a")] = h.play({track_id:"slow-track"});
  h.override = (_, route) => route === "/tracks/slow-track" ? pending.promise : undefined;
  const old = h.run("refreshPlayerBar()"); await flush();
  await h.run("selectPlayerBar('b::hw:1')");
  pending.resolve({id:"slow-track",cover_hash:"stale-cover"}); await old;
  assert.equal(h.run("PB.deviceKey"), deviceKey("b")); assert.equal(h.run("PB.trackID"), "song-b");
  assert.match(h.el("pb-cover").innerHTML, /cover-song-b/); assert.doesNotMatch(h.el("pb-cover").innerHTML, /stale-cover/);
});

test("changing view during console loading closes the modal but leaves playback refreshing on that view", async t => {
  const h = harness(t); await h.live(); const pending = deferred();
  h.override = (_, route) => route === deviceRoute("a", "play") ? pending.promise : undefined;
  const work = h.run("openPlayer('a::hw:1')"); h.run("setView('audit')");
  pending.resolve(h.play({ title: "Late but current playback" })); await work;
  assert.equal(h.run("S.view"), "audit"); assert.equal(h.el("ply-body"), null); assert.equal(h.run("PLY.deviceKey"), "");
  assert.equal(h.el("pb-title").textContent, "Late but current playback"); assert.equal(h.run("PB.deviceKey"), deviceKey("a"));
});


test("mode icon menu reflects and updates the per-terminal mode; codec badge only when detected", async t => {
  const h = harness(t);
  h.nodes = [h.node("a")];
  h.plays[deviceKey("a")] = h.play({ mqa: true });
  h.queues[deviceKey("a")] = { tracks: ["song-a"], position: 0, mode: "loop_all" };
  await h.init();

  const badge = h.el("pb-mqa"), wrap = h.el("pb-mode");
  assert.equal(badge.hidden, false);
  assert.match(badge.innerHTML, /MQA/);
  assert.equal(badge.classList.contains("codec-icon"), false, "wrapper must not carry badge styling");
  assert.equal(badge.querySelectorAll(".codec-icon").length, 1, "exactly one framed badge");
  assert.equal(wrap.dataset.disabled, "0");
  assert.match(h.el("pb-mode-btn").getAttribute("aria-label"), /列表循环/);
  assert.equal(wrap.querySelectorAll("[data-mode]").length, 4);
  assert.equal(wrap.querySelector('[data-mode="loop_all"]').classList.contains("active"), true);
  assert.equal(wrap.querySelector('[data-mode="sequence"]').classList.contains("active"), false);

  await h.run("ACT['pb-mode-set']({mode:'shuffle'})");
  const call = h.calls.find(c => c.route === "/nodes/a/queue/mode");
  assert.deepEqual(call.body, { mode: "shuffle", device_key: deviceKey("a") });
  assert.equal(h.run("S.nodeQueue['a::hw:1'].mode"), "shuffle");

  h.run("S.play['a::hw:1'] = { ...S.play['a::hw:1'], mqa: false, dop: false, converted: false }; paintPlayerBar()");
  assert.equal(badge.hidden, true, "MQA icon must stay hidden for non-MQA sources");
});

test("two devices on one terminal isolate status queue volume mode and sleep", async t => {
  const h = harness(t); h.nodes = [h.node("a")];
  const first = "a::hw:1", second = "a::hw:2";
  h.devices = [h.device("a"), h.device("a", { device_key: second, hw: "hw:2", name: "第二设备" })];
  h.plays[first] = h.play({ title: "First DAC", volume: 31, sleep_stop_at: 111 });
  h.plays[second] = h.play({ title: "Second DAC", track_id: "song-b", session_id: "session-b", volume: 72, sleep_stop_at: 222 });
  h.queues[first] = { tracks: ["song-a", "song-b"], position: 0, mode: "loop_all" };
  h.queues[second] = { tracks: ["song-b"], position: 0, mode: "shuffle" };
  await h.init();
  assert.equal(h.run("PB.deviceKey"), first);
  await h.run(`selectPlayerBar('${second}')`);
  assert.equal(h.el("pb-title").textContent, "Second DAC");
  assert.ok(h.calls.some(c => c.route === deviceRoute("a", "play", first)));
  assert.ok(h.calls.some(c => c.route === deviceRoute("a", "play", second)));
  await beginControl(h, "volume", 44);
  await h.run("ACT['pb-mode-set']({mode:'loop_one'})");
  await h.run(`sleepSet('${second}',30)`);
  await h.run(`sleepCancel('${second}')`);
  await h.run(`queueJump('${first}',1)`);
  assert.deepEqual(h.calls.filter(c => ["POST", "PUT", "DELETE"].includes(c.method)).map(c => [c.method, c.route, c.body]), [
    ["POST", "/nodes/a/volume", { volume: 44, device_key: second }],
    ["POST", "/nodes/a/queue/mode", { mode: "loop_one", device_key: second }],
    ["PUT", "/nodes/a/sleep", { minutes: 30, device_key: second }],
    ["DELETE", deviceRoute("a", "sleep", second), {}],
    ["POST", deviceRoute("a", "queue/jump", first), { index: 1 }]
  ]);
  assert.equal(h.run(`S.play['${first}'].volume`), 31);
  assert.equal(h.run(`S.play['${first}'].sleep_stop_at`), 111);
  assert.equal(h.run(`S.nodeQueue['${first}'].mode`), "loop_all");
});

test("queue jump and playlist load send the target in the query", async t => {
  const h = harness(t); await h.live();
  await h.run("queueJump('a::hw:1',1)");
  assert.deepEqual(h.posts().at(-1), { method: "POST", route: deviceRoute("a", "queue/jump"), body: { index: 1 } });
  h.override = (_, route) => route === "/playlists/pl" ? { id: "pl", name: "Road", items: [{ track_id: "song-a", missing: false }, { track_id: "song-b", missing: false }] } : undefined;
  await h.run("openPlayer('a::hw:1')");
  await h.run("plyLoadPlaylist('pl')");
  assert.deepEqual(h.posts().at(-1), { method: "POST", route: deviceRoute("a", "queue/jump"), body: { index: 0 } });
});

test("mute remembers volume separately for each device", async t => {
  const h = harness(t); h.nodes = [h.node("a")];
  const first = "a::hw:1", second = "a::hw:2";
  h.devices = [h.device("a"), h.device("a", { device_key: second, hw: "hw:2", name: "第二设备" })];
  h.plays[first] = h.play({ volume: 64 }); h.plays[second] = h.play({ volume: 27 });
  await h.init();
  await h.run("hotkeyMute()");
  h.plays[first] = h.play({ volume: 0 }); await h.run(`selectPlayerBar('${second}')`);
  h.plays[second] = h.play({ volume: 0 });
  await h.run("hotkeyMute()");
  await h.run("hotkeyMute()");
  assert.deepEqual(h.posts().map(c => [c.route, c.body.volume]), [
    ["/nodes/a/volume", 0],
    ["/nodes/a/volume", 0],
    ["/nodes/a/volume", 27]
  ]);
});

test("keyboard shortcuts route seek volume mute and queue to the selected device", async t => {
  const h = harness(t); await h.live();
  await h.run("selectPlayerBar('b::hw:1')");
  await h.run("hotkeyVolume(5)");
  await h.run("hotkeyMute()");
  await h.run("hotkeySeek(10)");
  await h.run("hotkeyMove('next')");
  const posts = h.posts();
  assert.ok(posts.some(c => c.route === "/nodes/b/volume" && c.body.volume === 65));
  assert.ok(posts.some(c => c.route === "/nodes/b/volume" && c.body.volume === 0));
  assert.ok(posts.some(c => c.route === "/nodes/b/seek" && c.body.device_key === "b::hw:1"));
  assert.ok(posts.some(c => c.route === deviceRoute("b", "queue/next")));
});

test("full console add reorder remove clear and load keep the device key", async t => {
  const h = harness(t); await h.live(); await h.run("openPlayer('a::hw:1')");
  h.run("S.tracks=[{id:'song-c',title:'C',decodable:true}]; PLY.addSel.add('song-c')");
  await h.run("plyAddGo()");
  h.run("S.nodeQueue['a::hw:1']={tracks:[{track_id:'song-a'},{track_id:'song-b'}],position:0}");
  await h.run("plyQueueMove(0,1)");
  h.run("S.nodeQueue['a::hw:1']={tracks:[{track_id:'song-a'},{track_id:'song-b'}],position:0}");
  await h.run("queueRemove('a::hw:1',1)");
  await h.run("queueClear('a::hw:1')");
  h.override = (_, route) => route === "/playlists/pl" ? { id: "pl", name: "Road", items: [{ track_id: "song-a", missing: false }] } : undefined;
  await h.run("plyLoadPlaylist('pl')");
  const writes = h.calls.filter(c => ["POST", "PUT"].includes(c.method) && c.route.startsWith("/nodes/a/"));
  assert.ok(writes.every(c => c.body.device_key === "a::hw:1" || new URLSearchParams(c.route.split("?")[1] || "").get("device_key") === "a::hw:1"));
  assert.ok(writes.some(c => c.route === "/nodes/a/queue/enqueue" && c.body.track_id === "song-c"));
  assert.ok(writes.some(c => c.route === "/nodes/a/queue" && c.body.track_ids.join(",") === "song-b,song-a"));
  assert.ok(writes.some(c => c.route === "/nodes/a/queue" && c.body.track_ids.join(",") === "song-a"));
  assert.ok(writes.some(c => c.route === "/nodes/a/queue" && c.body.track_ids.length === 0));
  assert.ok(writes.some(c => c.route === deviceRoute("a", "queue/jump") && c.body.index === 0));
});

test("library track and album push use only activated online devices", async t => {
  const h = harness(t); await h.live(); h.run("S.view='library'");
  await h.run("openPushPicker({trackIDs:['song-a'],track:{id:'song-a',title:'A',decodable:true}})");
  assert.equal(h.run("TP.sel"), "a::hw:1");
  await h.run("ACT['track-play-go']()");
  assert.ok(h.posts().some(c => c.route === "/nodes/a/play" && c.body.device_key === "a::hw:1"));
  h.calls.length = 0;
  h.override = (_, route) => route.startsWith("/library/select?") ? { track_ids: ["song-a", "song-b"] } : undefined;
  await h.run("lbPick({kind:'album',artist:'Artist',album:'Album',value:'Album'},'play')");
  await h.run("ACT['track-enqueue-go']()");
  assert.ok(h.posts().some(c => c.route === "/nodes/a/queue/batch" && c.body.device_key === "a::hw:1"));
  h.calls.length = 0; h.devices = []; await h.run("refreshPlayerBar()");
  await h.run("openPushPicker({trackIDs:['song-a'],track:{id:'song-a',decodable:true}})");
  await h.run("ACT['track-play-go']()");
  assert.equal(h.posts().length, 0);
  assert.equal(h.el("tp-go").disabled, true);
});

test("per-device EQ panel loads, edits, saves and bypasses bit-perfect independently", async t => {
  const h = harness(t); await h.live();
  const presets = [
    { id: "flat", name_zh: "平直", name_en: "Flat", preamp: 0, bands: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0] },
    { id: "bass", name_zh: "低音增强", name_en: "Bass", preamp: -3, bands: [6, 5, 4, 2, 0, 0, 0, 0, 0, 0] }
  ];
  const cfgs = {
    a: { enabled: false, preset: "flat", preamp: 0, bands: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0], bypass: false },
    b: { enabled: true, preset: "bass", preamp: -3, bands: [6, 5, 4, 2, 0, 0, 0, 0, 0, 0], bypass: true }
  };
  h.override = (method, route, body) => {
    if (route === "/dsp/presets") return presets;
    const m = route.match(/^\/nodes\/([^/]+)\/devices\/([^/]+)\/dsp$/);
    if (m) {
      const id = decodeURIComponent(m[1]);
      if (method === "GET") return cfgs[id];
      if (method === "PUT") { cfgs[id] = { ...cfgs[id], ...body }; return { ok: true }; }
    }
    return undefined;
  };
  await h.run("openPlayer('a::hw:1')");
  h.run("PLY.eqOpen=true");
  await h.run("loadPlayerEQ()");
  assert.ok(h.el("ply-eq").innerHTML.includes("eq-bands"));
  assert.match(h.el("ply-eq").innerHTML, /EQ 调节/);
  assert.doesNotMatch(h.el("ply-eq").innerHTML, /ply-dsd|eq-dsd/);
  assert.equal((h.el("ply-eq").innerHTML.match(/data-change="ply-eq-band"/g) || []).length, 16);
  assert.equal(h.run("PLY.eq.cfg.bands.length"), 16, "legacy ten-band curves are padded");
  assert.deepEqual(h.json("PLY.eq.cfg.bands.slice(10)"), [0, 0, 0, 0, 0, 0]);
  assert.equal(h.run("PLY.eq.cfg.preset"), "flat");
  h.run("CHG['ply-eq-band']({value:'6',dataset:{band:'0'}})");
  assert.equal(h.run("PLY.eq.cfg.bands[0]"), 6);
  assert.equal(h.run("PLY.eq.cfg.preset"), "custom");
  await h.run("savePlayerEQ()");
  assert.equal(cfgs.a.bands[0], 6);
  assert.equal(cfgs.b.bands[0], 6, "device b must keep its own curve");
  assert.equal(cfgs.b.preset, "bass");
  h.run("PLY.eq.presets=PLY.eq.presets.slice(0,1); CHG['ply-eq-preset']({value:'flat'})");
  assert.equal(h.run("PLY.eq.cfg.bands.length"), 16, "preset selection always renders sixteen bands");
  await h.run("openPlayer('b::hw:1')");
  await flush();
  assert.equal(h.run("PLY.eq.cfg.preset"), "bass");
  assert.equal(h.run("PLY.eq.cfg.enabled"), true);
  assert.ok(h.el("ply-eq").innerHTML.includes("位完美"), "bypass banner shown");
  assert.equal(h.el("ply-eq-toggle").disabled, true, "bit-perfect session disables EQ switch");
  assert.equal(h.el("ply-eq-toggle").getAttribute("aria-checked"), "true");
});
