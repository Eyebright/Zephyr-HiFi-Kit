"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const assets = path.join(__dirname, "assets");
const dictionaries = ["i18n-app.js", "i18n-settings.js", "i18n-features.js", "i18n-errors.js"];

function harness({ saved, languages = ["en-US"], brokenStorage = false } = {}) {
  const storage = new Map(saved === undefined ? [] : [["zephyr-ui-language", saved]]);
  const listeners = new Map(), events = [];
  const context = {
    navigator: { languages },
    localStorage: {
      getItem(key) { if (brokenStorage) throw new Error("blocked"); return storage.get(key) ?? null; },
      setItem(key, value) { if (brokenStorage) throw new Error("blocked"); storage.set(key, value); }
    },
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
    document: {
      documentElement: { lang: "zh-CN" }, querySelectorAll: () => [],
      addEventListener(type, fn) { if (!listeners.has(type)) listeners.set(type, []); listeners.get(type).push(fn); },
      dispatchEvent(event) { events.push(event); for (const fn of listeners.get(event.type) || []) fn(event); }
    }
  };
  vm.createContext(context);
  vm.runInContext(fs.readFileSync(path.join(assets, "i18n.js"), "utf8"), context);
  return { context, storage, events, run: source => vm.runInContext(source, context) };
}

test("defaults to server Chinese and keeps explicit browser choices independent", () => {
  const h = harness();
  assert.equal(h.run("I18n.locale"), "zh-CN");
  h.run("I18n.setDefault('en')");
  assert.equal(h.run("I18n.locale"), "en");
  assert.equal(h.context.document.documentElement.lang, "en");
  assert.equal(h.run("I18n.setPreference('zh-TW')"), true);
  h.run("I18n.setDefault('zh-CN')");
  assert.equal(h.run("I18n.locale"), "zh-TW");
  assert.equal(h.storage.get("zephyr-ui-language"), "zh-TW");
  assert.equal(harness({ saved: h.storage.get("zephyr-ui-language") }).run("I18n.locale"), "zh-TW");
  h.run("I18n.setPreference('server')");
  assert.equal(h.run("I18n.locale"), "zh-CN");
});

test("browser matching handles regional Chinese, English and unsupported languages", () => {
  for (const [languages, expected] of [
    [["zh-HK"], "zh-TW"], [["zh-Hant"], "zh-TW"], [["zh-MO"], "zh-TW"],
    [["zh-Hans-SG"], "zh-CN"], [["en-GB"], "en"], [["de-DE", "en"], "en"], [["fr"], "zh-CN"]
  ]) assert.equal(harness({ saved: "browser", languages }).run("I18n.locale"), expected);
  const h = harness({ saved: "browser", languages: ["fr"] });
  h.run("I18n.setDefault('zh-TW')");
  assert.equal(h.run("I18n.locale"), "zh-TW");
});

test("storage failure preserves the current-session selection", () => {
  const h = harness({ brokenStorage: true });
  assert.equal(h.run("I18n.locale"), "zh-CN");
  assert.equal(h.run("I18n.setPreference('en')"), false);
  assert.equal(h.run("I18n.locale"), "en");
  assert.equal(h.events.length, 1);
  assert.equal(h.events[0].type, "language-changed");
});

test("invalid persisted preferences are ignored and invalid writes rejected", () => {
  const h = harness({ saved: "javascript:alert(1)" });
  assert.equal(h.run("I18n.preference"), "server");
  assert.throws(() => h.run("I18n.setDefault('fr')"), /Unsupported/);
  assert.throws(() => h.run("I18n.setPreference('__proto__')"), /Unsupported/);
  assert.equal(h.run("I18n.locale"), "zh-CN");
});

test("translation preserves untrusted parameters as text without recursively interpolating them", () => {
  const h = harness();
  h.run("I18n.register('test', { line: ['你好 {name}，{count} 首', '你好 {name}，{count} 首', 'Hello {name}, {count} tracks'] })");
  assert.equal(h.run("I18n.t('test.line', {name: '<img onerror=alert(1)>{count}', count: 0})"), "你好 <img onerror=alert(1)>{count}，0 首");
  h.run("I18n.setPreference('en')");
  assert.equal(h.run("I18n.t('test.line', {name: '音乐随心而动', count: 2})"), "Hello 音乐随心而动, 2 tracks");
  assert.equal(h.run("I18n.t('missing.key')"), "missing.key");
});

test("formatters follow locale without changing source timestamps or values", () => {
  const h = harness({ saved: "en" });
  assert.equal(h.run("I18n.number(12345.6)"), new Intl.NumberFormat("en").format(12345.6));
  assert.equal(h.run("I18n.relative(-2, 'minute')"), "2 minutes ago");
  assert.equal(h.run("I18n.date('invalid')"), "—");
  const iso = "2026-11-20T12:00:00Z", options = { year: "numeric", month: "long", day: "numeric", timeZone: "UTC" };
  assert.equal(h.run(`I18n.date(${JSON.stringify(iso)}, ${JSON.stringify(options)})`), new Intl.DateTimeFormat("en", options).format(new Date(iso)));
});

test("only marked UI elements are translated, not arbitrary user content", () => {
  const h = harness({ saved: "en" }), user = { textContent: "管理员" };
  const title = { textContent: "管理员", getAttribute: () => "core.administrator" };
  const placeholder = { getAttribute: () => "core.search", setAttribute(name, value) { this[name] = value; } };
  h.context.document.querySelectorAll = selector => selector === "[data-i18n]" ? [title] : selector === "[data-i18n-placeholder]" ? [placeholder] : [];
  h.run("I18n.apply()");
  assert.equal(title.textContent, "Administrator");
  assert.equal(placeholder.placeholder, "Global search");
  assert.equal(user.textContent, "管理员");
});

test("message codes select translations while unknown diagnostics stay intact", () => {
  const h = harness({ saved: "en" });
  h.run("I18n.register('errors', { denied: ['操作被拒绝', '操作遭拒絕', 'Operation denied'] })");
  assert.equal(h.run("I18n.message({message_code:'errors.denied',error:'原始诊断'})"), "Operation denied");
  assert.match(h.run("I18n.message({error:'设备 音乐房 <offline>'})"), /Original diagnostic: 设备 音乐房 <offline>/);
  assert.equal(h.run("I18n.detail('歌曲名称')"), "歌曲名称");
  h.run("I18n.register('errors', { bad_request: ['请求无效', '請求無效', 'Invalid request'] })");
  assert.match(h.run("I18n.message({message_code:'errors.bad_request',error:'路径 /音乐 无效'})"), /Invalid request\nOriginal diagnostic: 路径 \/音乐 无效/);
  assert.match(h.run("I18n.message({message_code:'errors.denied',detail:'external stderr'})"), /Operation denied\nOriginal diagnostic: external stderr/);
});

test("request errors keep non-JSON and nested diagnostics visible", async () => {
  const h = harness({ saved: "en" });
  h.context.window = h.context;
  h.context.document.querySelector = () => null;
  for (const file of ["i18n-app.js", "app.js"]) vm.runInContext(fs.readFileSync(path.join(assets, file), "utf8"), h.context);
  for (const text of ["disk full 原始 stderr", JSON.stringify({ error: { message: "disk full 原始 stderr" } })]) {
    h.context.fetch = async () => ({ ok: false, status: 502, text: async () => text });
    await assert.rejects(h.run("req('GET','/fixture')"), error => {
      assert.equal(error.status, 502);
      assert.equal(error.rawMessage, "disk full 原始 stderr");
      assert.match(error.message, /Original diagnostic: disk full 原始 stderr/);
      return true;
    });
  }
});

test("Android installation instructions use real newlines in every locale", () => {
  const h = harness();
  vm.runInContext(fs.readFileSync(path.join(assets, "i18n-app.js"), "utf8"), h.context);
  for (const locale of ["zh-CN", "zh-TW", "en"]) {
    h.run(`I18n.setPreference('${locale}')`);
    const text = h.run("I18n.t('app.m209',{id:'artifact.apk',value:'server-address',value2:'test-placeholder'})");
    assert.equal(text.split("\n").length, 3);
    assert.ok(!text.includes("\\n"));
  }
});

test("all catalogs have three nonempty translations and identical interpolation parameters", () => {
  const all = new Map();
  const context = vm.createContext({ I18n: { register(namespace, entries) {
    for (const [key, variants] of Object.entries(entries)) {
      const full = namespace + "." + key;
      assert.ok(!all.has(full), "duplicate key: " + full);
      assert.equal(variants.length, 3, full);
      const placeholders = value => [...new Set([...value.matchAll(/\{(\w+)\}/g)].map(m => m[1]))].sort();
      for (const variant of variants) {
        assert.equal(typeof variant, "string", full);
        assert.ok(variant.trim(), "empty translation: " + full);
        assert.deepEqual(placeholders(variant), placeholders(variants[0]), "placeholder mismatch: " + full);
      }
      all.set(full, variants);
    }
  } } });
  for (const file of dictionaries) vm.runInContext(fs.readFileSync(path.join(assets, file), "utf8"), context, { filename: file });
  assert.ok(all.size > 100, "catalogs must cover the whole console");
  for (const file of ["app.js", "settings.js", "experience.js", "library-browser.js", "favorites.js", "terminal-center.js", "terminal-install.js"]) {
    const source = fs.readFileSync(path.join(assets, file), "utf8");
    for (const match of source.matchAll(/I18n\.t\(\s*["']([^"']+)["']\s*[,)]/g)) assert.ok(all.has(match[1]) || match[1].startsWith("core."), file + " missing " + match[1]);
    for (const match of source.matchAll(/app[TH]\(\s*["']([^"']+)["']\s*[,)]/g)) assert.ok(all.has("app." + match[1]), file + " missing app." + match[1]);
    for (const match of source.matchAll(/(?:lb|fv|tc|ti)T\(\s*["']([^"']+)["']\s*[,)]/g)) assert.ok(all.has("features." + match[1]), file + " missing features." + match[1]);
  }
});
