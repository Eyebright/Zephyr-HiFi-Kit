"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
function deferred() { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; }
function harness() {
  const elements = new Map();
  const element = () => ({ innerHTML: "", textContent: "", value: "", dataset: {}, disabled: false, attrs: {}, classList: { toggle() {}, add() {}, remove() {} }, setAttribute(k, v) { this.attrs[k] = v; }, querySelectorAll: () => [] });
  for (const id of ["view", "pb-favorite", "pb-favorite-album", "pb-favorite-menu", "ov-greeting", "ov-favorites", "lb-header", "lb-body", "lb-pages", "global-search"]) elements.set("#" + id, element());
  const h = { elements, calls: [], messages: [], events: {}, pages: {}, states: {}, pushed: [] };
  const context = {
    console, URL, URLSearchParams, AbortController,
    setTimeout: () => 1, clearTimeout() {}, setInterval: () => 1, clearInterval() {},
    document: { querySelector: s => elements.get(s) || null, querySelectorAll: () => [], addEventListener: (name, cb) => { h.events[name] = cb; } },
    location: { protocol: "http:", host: "localhost" }, navigator: {},
    __api: async (method, route, body) => {
      h.calls.push({ method, route, body });
      if (h.override) { const result = await h.override(method, route, body); if (result !== undefined) return result; }
      if (route.startsWith("/favorites/state?")) return h.states[new URLSearchParams(route.split("?")[1]).get("track_id")] || { track: false, album: false, artist: "专辑艺术家", album_name: "专辑" };
      if (method !== "GET") return { saved: method === "PUT" };
      if (route.startsWith("/favorites?")) { const q = new URLSearchParams(route.split("?")[1]); return h.pages[q.get("kind")] || { items: [], total: 0, page: Number(q.get("page")), page_size: Number(q.get("page_size")) }; }
      throw new Error("Unexpected route " + route);
    },
    __toast: (...args) => h.messages.push(args), __push: value => h.pushed.push(value)
  };
  context.window = context;
  vm.createContext(context);
  for (const name of ["app.js", "library-browser.js", "favorites.js"]) vm.runInContext(fs.readFileSync(path.join(__dirname, "assets", name), "utf8"), context);
  vm.runInContext("req=(method,route,opts)=>__api(method,route,opts?.body ? JSON.parse(opts.body) : undefined); toast=__toast; openPushPicker=__push; const XP={profile:{display_name:'小林'}};", context);
  h.run = source => vm.runInContext(source, context);
  return h;
}
function item(id, kind = "track") { return { kind, track_id: id, title: "歌曲 " + id, artist: "艺术家", album: "专辑", missing: false, decodable: true, track_count: 1 }; }

test("player has a distinct heart and a separate album action", () => {
  const h = harness(), html = h.run("playerBarShell()");
  assert.match(html, /id="pb-favorite"/); assert.match(html, /favorite-current-album/);
  assert.equal(h.run("new Set(NAV.map(n=>n.icon)).size===NAV.length"), true);
  assert.equal(h.run("NAV.at(-1).id"), "settings");
});
test("heart displays server state without writing or needing a cached catalog", async () => {
  const h = harness(); h.states.a = { track: true, album: false, artist: "专辑艺术家", album_name: "专辑" };
  h.run("FV.current='a'"); await h.run("refreshPlayerFavorite('a')");
  assert.equal(h.elements.get("#pb-favorite").attrs["aria-pressed"], "true");
  assert.equal(h.elements.get("#pb-favorite").disabled, false);
  assert.equal(h.elements.get("#pb-favorite-album").dataset.saved, "false");
  assert.ok(h.calls.every(c => c.method === "GET"));
});
test("click snapshots the original track even after playback changes", async () => {
  const h = harness(), pending = deferred();
  h.override = (method) => method === "PUT" ? pending.promise : undefined;
  const work = h.run("ACT['favorite-current']({id:'original',saved:'false'})");
  h.run("PB.id='other-terminal'; FV.current='next-track'");
  pending.resolve({ saved: true }); await work;
  assert.deepEqual(h.calls.filter(c => c.method === "PUT").map(c => c.body.track_id), ["original"]);
  assert.equal(h.run("FV.current"), "next-track");
});
test("rapid duplicate clicks are coalesced and track favorites never like the album", async () => {
  const h = harness(), pending = deferred();
  h.override = method => method === "PUT" ? pending.promise : undefined;
  const first = h.run("ACT['favorite-current']({id:'a',saved:'false'})");
  await h.run("ACT['favorite-current']({id:'a',saved:'false'})");
  assert.equal(h.calls.filter(c => c.method === "PUT").length, 1);
  assert.equal(h.calls.find(c => c.method === "PUT").body.kind, "track");
  pending.resolve({ saved: true }); await first;
});
test("failed write never toggles the heart or claims success", async () => {
  const h = harness();
  h.run("FV.current='a'; FV.state={track:false,album:false,artist:'',album_name:''}; FV.stateAt=Date.now()");
  h.override = method => { if (method === "PUT") throw new Error("not saved"); };
  await h.run("ACT['favorite-current']({id:'a',saved:'false'})");
  assert.equal(h.elements.get("#pb-favorite").attrs["aria-pressed"], "false");
  assert.ok(h.messages.some(m => m[1] === "err"));
  assert.ok(h.messages.every(m => m[1] !== "ok"));
});
test("late state from a previous track cannot overwrite the current track", async () => {
  const h = harness(), pending = deferred();
  h.override = (_, route) => route.includes("track_id=old") ? pending.promise : undefined;
  h.run("FV.current='old'"); const old = h.run("refreshPlayerFavorite('old')");
  h.run("FV.current='new'"); await h.run("refreshPlayerFavorite('new')");
  pending.resolve({ track: true, album: true, artist: "old", album_name: "old" }); await old;
  assert.equal(h.run("FV.state.artist"), "专辑艺术家");
  assert.equal(h.elements.get("#pb-favorite").dataset.id, "new");
});
test("external or unavailable tracks stay disabled and titles are not guessed", async () => {
  const h = harness(); h.override = () => { const error = new Error("missing"); error.status = 404; throw error; };
  h.run("FV.current='unknown'"); await h.run("refreshPlayerFavorite('unknown')");
  assert.equal(h.elements.get("#pb-favorite").disabled, true);
  assert.match(h.elements.get("#pb-favorite").title, /不在曲库/);
  assert.ok(h.calls.every(c => c.method === "GET"));
});
test("removing a missing favorite uses its stored identity without resolving the file", async () => {
  const h = harness();
  h.run("S.view='library'; S.libTab='favorites'; FV.page={items:[{kind:'track',track_id:'gone',title:'旧曲目',missing:true}]}; refreshLibraryBrowser=async()=>{}");
  await h.run("ACT['favorite-remove']({scope:'library',index:'0'})");
  assert.equal(h.calls.length, 1);
  assert.equal(h.calls[0].method, 'DELETE'); assert.equal(h.calls[0].body.track_id, 'gone');
});

test("album action uses effective album identity and never creates track favorites", async () => {
  const h = harness(); await h.run("ACT['favorite-current-album']({id:'a',artist:'合辑艺术家',album:'合辑',saved:'false'})");
  const body = h.calls.find(c => c.method === "PUT").body;
  assert.equal(body.kind, "album"); assert.equal(body.artist, "合辑艺术家"); assert.equal(body.album, "合辑");
  assert.equal(body.track_id, undefined);
});
test("missing favorites retain escaped metadata and cannot be played", async () => {
  const h = harness();
  const html = h.run("favoriteRow({kind:'track',track_id:'a',title:'<script>bad</script>',artist:'<b>',album:'x',missing:true,decodable:false},0,'library')");
  assert.match(html, /&lt;script&gt;/); assert.doesNotMatch(html, /<script>/); assert.match(html, /文件不可用/); assert.match(html, /favorite-play[^>]*disabled/);
  await h.run("playFavorite({kind:'track',track_id:'a',missing:true,decodable:false})"); assert.equal(h.calls.length, 0);
});
test("personalized greeting uses text, never HTML", () => {
  const h = harness(); h.run("XP.profile.display_name='<img src=x>'; paintFavoritePreview()");
  assert.match(h.elements.get("#ov-greeting").textContent, /<img src=x>/);
  assert.match(h.elements.get("#ov-favorites").innerHTML, /&lt;img src=x&gt;/);
  assert.doesNotMatch(h.elements.get("#ov-favorites").innerHTML, /<img src=x>/);
});
test("favorites are a library tab with paged server reads and no removed history entry", async () => {
  const h = harness(); h.pages.track = { items: [item("a")], total: 49, page: 2, page_size: 24 };
  h.run("S.view='library'; S.libTab='favorites'; lbFrame().query.page=2"); await h.run("refreshLibraryBrowser()");
  assert.match(h.elements.get("#lb-header").innerHTML, /我喜欢的/);
  assert.doesNotMatch(h.elements.get("#lb-header").innerHTML, /最近播放/);
  assert.match(h.elements.get("#lb-pages").innerHTML, /第 2 \/ 3 页/);
  assert.ok(h.calls.some(c => c.route.includes("page=2")));
});
test("empty final page redirects to the last populated page", async () => {
  const h = harness(); h.run("S.view='library'; S.libTab='favorites'; lbFrame().query.page=2");
  h.override = (_, route) => route.startsWith("/favorites?") ? { items: [], total: 0, page: route.includes("page=2") ? 2 : 1, page_size: 24 } : undefined;
  await h.run("refreshLibraryBrowser()");
  assert.equal(h.run("lbFrame().query.page"), 1); assert.equal(h.calls.length, 2);
});
test("late preview response cannot replace a different favorite type", async () => {
  const h = harness(), pending = deferred();
  h.override = (_, route) => route.includes("kind=track") ? pending.promise : undefined;
  const old = h.run("refreshFavoritePreview()");
  await h.run("ACT['favorite-preview-kind']({kind:'album'})");
  pending.resolve({ items: [item("old")], total: 1, page: 1, page_size: 6 }); await old;
  assert.equal(h.run("FV.preview.items.length"), 0);
  assert.equal(h.run("FV.previewKind"), "album");
});
test("navigation away and back discards a pending favorite playback picker", async () => {
  const h = harness(), pending = deferred();
  h.override = () => pending.promise;
  const work = h.run("playFavorite({kind:'track',track_id:'a',title:'A',missing:false,decodable:true})");
  h.run("cleanupOverview()");
  pending.resolve({id:'a',decodable:true}); await work;
  assert.equal(h.pushed.length, 0);
});

test("late paginated response cannot repaint a different library tab", async () => {
  const h = harness(), pending = deferred(); h.override = () => pending.promise;
  h.run("S.view='library'; S.libTab='favorites'"); const work = h.run("refreshLibraryBrowser()");
  h.run("S.libTab='albums'; LB.request++"); const before = h.elements.get("#lb-body").innerHTML;
  pending.resolve({ items: [item("old")], total: 1, page: 1, page_size: 24 }); await work;
  assert.equal(h.elements.get("#lb-body").innerHTML, before);
});
