"use strict";

const I18n = (() => {
  const locales = ["zh-CN", "zh-TW", "en"];
  const preferences = ["server", "browser", ...locales];
  const catalog = new Map();
  const storageKey = "zephyr-ui-language";
  let preference = "server", defaultLocale = "zh-CN";
  try {
    const saved = globalThis.localStorage?.getItem(storageKey);
    if (preferences.includes(saved)) preference = saved;
  } catch {}

  function browserLocale() {
    const languages = globalThis.navigator?.languages || [globalThis.navigator?.language];
    for (const language of languages) {
      const tag = String(language || "").toLowerCase();
      if (/^en(?:-|$)/.test(tag)) return "en";
      if (/^zh(?:-|$)/.test(tag)) return /(?:^|-)(?:hant|tw|hk|mo)(?:-|$)/.test(tag) ? "zh-TW" : "zh-CN";
    }
    return defaultLocale;
  }
  function currentLocale() {
    return preference === "server" ? defaultLocale : preference === "browser" ? browserLocale() : preference;
  }
  function register(namespace, entries) {
    for (const [key, messages] of Object.entries(entries)) {
      catalog.set(namespace + "." + key, messages);
    }
  }
  function t(key, params = {}) {
    const messages = catalog.get(key);
    const template = messages?.[locales.indexOf(currentLocale())] || messages?.[0] || key;
    return template.replace(/\{(\w+)\}/g, (token, name) => Object.hasOwn(params, name) ? String(params[name] ?? "") : token);
  }
  function apply(root = globalThis.document) {
    const doc = globalThis.document;
    if (doc?.documentElement) doc.documentElement.lang = currentLocale();
    if (!root?.querySelectorAll) return;
    for (const el of root.querySelectorAll("[data-i18n]")) el.textContent = t(el.getAttribute("data-i18n"));
    for (const attr of ["title", "placeholder", "aria-label", "aria-description", "content"]) {
      for (const el of root.querySelectorAll("[data-i18n-" + attr + "]")) el.setAttribute(attr, t(el.getAttribute("data-i18n-" + attr)));
    }
  }
  function notify(previousLocale) {
    apply();
    if (typeof globalThis.CustomEvent === "function") globalThis.document?.dispatchEvent?.(new CustomEvent("language-changed", {
      detail: { locale: currentLocale(), previousLocale, preference, defaultLocale }
    }));
  }
  function setPreference(value) {
    if (!preferences.includes(value)) throw new RangeError("Unsupported language preference");
    const previousLocale = currentLocale(), changed = preference !== value;
    preference = value;
    let saved = true;
    try {
      if (!globalThis.localStorage) saved = false;
      else globalThis.localStorage.setItem(storageKey, value);
    } catch { saved = false; }
    if (changed) notify(previousLocale);
    return saved;
  }
  function setDefault(value) {
    if (!locales.includes(value)) throw new RangeError("Unsupported default language");
    if (value === defaultLocale) return;
    const previousLocale = currentLocale();
    defaultLocale = value;
    notify(previousLocale);
  }
  function message(data, fallback = "") {
    const key = data?.message_code || (data?.code ? "errors." + data.code : "");
    const raw = typeof data?.error === "string" ? data.error : typeof data?.message === "string" ? data.message : fallback;
    const known = catalog.has(key), summary = known ? t(key, data.message_params || {}) : t("core.requestFailed");
    const generic = /(?:^errors\.(?:bad_request|internal_error|bad_gateway|service_unavailable|conflict|artifact_rejected|command_invalid|local_source_invalid|update_policy_invalid)$|_failed$)/.test(key);
    const diagnostic = data?.detail || ((!known || generic) ? raw : "");
    return diagnostic && diagnostic !== summary ? summary + "\n" + t("core.diagnostic", { detail: diagnostic }) : summary;
  }
  function detail(value, fallbackKey, params = {}) {
    if (value && typeof value === "object") {
      const key = value.detail_code || value.reason_code || value.message_code;
      if (catalog.has(key)) return t(key, value.detail_params || value.reason_params || value.message_params || {});
      value = value.detail || value.reason || value.message || "";
    }
    if (fallbackKey && catalog.has(fallbackKey)) {
      const summary = t(fallbackKey, params);
      return value ? summary + " · " + t("core.diagnostic", { detail: value }) : summary;
    }
    return String(value ?? "");
  }
  function date(value, options = { dateStyle: "medium", timeStyle: "medium" }) {
    const d = value instanceof Date ? value : new Date(value);
    return Number.isNaN(d.getTime()) ? "—" : new Intl.DateTimeFormat(currentLocale(), options).format(d);
  }
  register("core", {
    title: ["Zephyr HiFi Kit · 运维控制台", "Zephyr HiFi Kit · 管理控制台", "Zephyr HiFi Kit · Management Console"],
    description: ["Zephyr HiFi Kit（和风HiFi音乐播放套件）运维控制台 — 主页、音乐库、终端中心、设置与操作日志", "Zephyr HiFi Kit（和風HiFi音樂播放套件）管理控制台 — 首頁、音樂庫、終端中心、設定與操作記錄", "Zephyr HiFi Kit management console — Home, Library, Terminals, Settings and Activity"],
    brand: ["和风HiFi音乐播放套件", "和風HiFi音樂播放套件", "Your music, everywhere"],
    closeNavigation: ["关闭导航", "關閉導覽", "Close navigation"],
    navigation: ["主导航", "主導覽", "Main navigation"],
    openNavigation: ["打开主导航", "開啟主導覽", "Open navigation"],
    connecting: ["正在连接服务器…", "正在連線至伺服器…", "Connecting to server…"],
    searchPlaceholder: ["搜索当前视图…（按 / 聚焦）", "搜尋目前畫面…（按 / 聚焦）", "Search this view… (press /)"],
    search: ["全局搜索", "全域搜尋", "Global search"],
    connection: ["服务器连接状态", "伺服器連線狀態", "Server connection"],
    connected: ["已连接", "已連線", "Connected"],
    profile: ["管理员与偏好", "管理員與偏好設定", "Administrator & preferences"],
    administrator: ["管理员", "管理員", "Administrator"],
    alerts: ["告警中心", "警示中心", "Alerts"],
    requestFailed: ["请求失败", "請求失敗", "Request failed"],
    diagnostic: ["原始诊断：{detail}", "原始診斷：{detail}", "Original diagnostic: {detail}"]
  });
  return {
    register, t, apply, setPreference, setDefault, message, detail, date,
    get locale() { return currentLocale(); },
    get preference() { return preference; },
    get defaultLocale() { return defaultLocale; },
    has: key => catalog.has(key),
    number: (value, options) => new Intl.NumberFormat(currentLocale(), options).format(value),
    relative: (value, unit) => new Intl.RelativeTimeFormat(currentLocale(), { numeric: "auto" }).format(value, unit)
  };
})();

if (typeof document !== "undefined") {
  document.addEventListener("DOMContentLoaded", () => I18n.apply());
  document.addEventListener("language-changed", () => {
    if (typeof appLanguageChanged === "function") appLanguageChanged();
    if (typeof experienceLanguageChanged === "function") experienceLanguageChanged();
  });
}
