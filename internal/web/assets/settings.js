"use strict";

let settingsTranslationIndex;
const settingsRenderedMessages = new Map();
function settingsTranslations() {
  if (!settingsTranslationIndex && typeof SETTINGS_I18N_ENTRIES !== "undefined") {
    const reverse = new Map();
    for (const [source, translations] of Object.entries(SETTINGS_I18N_ENTRIES)) {
      for (const text of [source, ...translations]) reverse.set(text, source);
    }
    const sources = Object.keys(SETTINGS_I18N_ENTRIES).filter(key => !key.includes("{")).sort((a, b) => b.length - a.length);
    settingsTranslationIndex = { reverse, pattern: new RegExp(sources.map(key => key.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|"), "g") };
  }
  return settingsTranslationIndex;
}
function settingsT(text, values) {
  const rendered = values === undefined ? settingsRenderedMessages.get(text) : null;
  const source = rendered?.source || settingsTranslations()?.reverse.get(text) || text;
  values = values || rendered?.values || {};
  const result = typeof I18n !== "undefined" && settingsTranslations()?.reverse.has(source)
    ? I18n.t("settings." + source, values)
    : String(source).replace(/\{(\w+)\}/g, (match, key) => Object.prototype.hasOwnProperty.call(values, key) ? String(values[key]) : match);
  if (Object.keys(values).length && settingsTranslations()?.reverse.has(source)) {
    if (settingsRenderedMessages.size >= 200) settingsRenderedMessages.delete(settingsRenderedMessages.keys().next().value);
    settingsRenderedMessages.set(result, { source, values });
  }
  return result;
}
// Interpolation values are data, not translation sources.
function settingsHTML(parts, ...values) {
  const index = settingsTranslations();
  return parts.reduce((html, part, i) => html + (index ? part.replace(index.pattern, source => esc(settingsT(source))) : part) + (i < values.length ? values[i] : ""), "");
}
function settingsToast(message, kind) { toast(settingsT(message), kind); }
function settingsBadge(label, kind) { return badge(settingsT(label), kind); }
function settingsNumber(value, options) { return typeof I18n !== "undefined" ? I18n.number(value, options) : Number(value).toLocaleString(); }
function settingsDate(value, options) { return typeof I18n !== "undefined" ? I18n.date(value, options) : new Date(value).toLocaleString(); }
function settingsTranslatedList(items, columns) {
  return items.map(item => {
    for (const column of columns) {
      const source = item[column];
      Object.defineProperty(item, column, { get: () => settingsT(source), enumerable: true });
    }
    return item;
  });
}

const SETTINGS = {
  section: "profile", session: null, sessionError: "", authBusy: false,
  epoch: 0, refreshSeq: 0, sessionSeq: 0, authSeq: 0, busy: new Set(), mounts: [], counts: new Map(),
  maintenance: null, observer: null
};
const SETTINGS_SECTIONS = settingsTranslatedList([
  ["profile", "管理员与偏好", "user", "管理员称呼、会话安全与首次使用引导"],
  ["authorization", "授权信息", "shield", "许可证、信任根与终端席位"],
  ["folders", "音乐文件夹", "folder", "服务端本地目录与 NAS 音乐源"],
  ["packages", "软件包与源", "box", "应用版本、软件包、签名与安装源；安装执行见终端中心"],
  ["maintenance", "备份与维护", "database", "服务端自升级、音乐数据备份、恢复与操作日志清理"],
  ["about", "关于我们", "info", "产品介绍、服务端版本、许可与第三方实现来源"]
], [1, 3]);
const SETTINGS_LEGACY_MANAGE = new Set([
  "lic-open", "lic-submit", "key-import", "key-import-submit", "key-trust", "node-auth", "node-revoke", "goto-add-node",
  "app-create", "app-create-submit", "app-upload", "app-import-local", "app-import-local-submit", "up-submit", "ver-ban",
  "bom-new", "bom-edit", "bom-import", "bom-import-submit", "bom-publish", "bom-rollback", "bom-save-ed", "bom-publish-ed",
  "bom-validate", "bom-validate-ed", "apt-src-add", "apt-src-del",
  "upd-check", "upd-apply", "upd-auto",
  "node-config", "node-os", "node-net", "node-console", "cfg-save", "cfg-delete", "cfg-auth", "cfg-revoke",
  "play-stop", "play-pause", "queue-prev", "queue-next", "queue-jump", "queue-remove", "queue-clear", "track-enqueue-go"
]);

function settingsAdmin() { return SETTINGS.session?.authenticated === true; }
function settingsHere(section) { return S.view === "settings" && !!$("#settings-page") && (!section || SETTINGS.section === section); }
function settingsNote(text, kind = "info") { return settingsHTML`<div class="settings-note ${esc(kind)}">${icon(kind === "err" || kind === "warn" ? "alert" : "info", 16)}<span data-settings-message="${esc(encodeURIComponent(text))}">${esc(settingsT(text))}</span></div>`; }
function settingsError(id, message) { const el = $("#" + id); if (el) el.innerHTML = message ? settingsNote(message, "err") : ""; }
function settingsButton(action, label, opts = {}) {
  return settingsHTML`<button type="button" class="btn ${opts.className || "ghost sm"}" data-act="${esc(action)}"${opts.id ? settingsHTML` data-id="${esc(opts.id)}"` : ""}${opts.legacy ? settingsHTML` data-legacy="${esc(opts.legacy)}"` : ""}${opts.kind ? settingsHTML` data-kind="${esc(opts.kind)}"` : ""}${opts.admin ? " data-settings-admin" : ""}${opts.task ? settingsHTML` data-settings-task="${esc(opts.task)}"` : ""}${opts.blocked ? ' data-settings-blocked="true"' : ""}${opts.blocked || (opts.admin && !settingsAdmin()) || SETTINGS.busy.has(opts.task) ? " disabled" : ""}>${opts.icon ? icon(opts.icon, 14) : ""}${esc(settingsT(label))}</button>`;
}
function settingsRequireAdmin() {
  if (settingsAdmin()) return true;
  settingsToast("请先在设置页解锁管理员；只读浏览不受影响。", "warn");
  $("#settings-password")?.focus();
  return false;
}
function settingsLegacyManage(el) {
  return SETTINGS_LEGACY_MANAGE.has(el.dataset.act) || SETTINGS_LEGACY_MANAGE.has(el.dataset.change) || ["ver-lc", "node-device", "ply-device"].includes(el.dataset.change);
}
function settingsApplyAccess() {
  if (!settingsHere()) return;
  const roots = [$("#settings-page"), $("#modal-root"), $("#drawer-root")].filter(Boolean);
  for (const root of roots) {
    root.querySelectorAll("[data-settings-admin]").forEach(el => {
      const disabled = !settingsAdmin() || el.dataset.settingsBlocked === "true" || SETTINGS.busy.has(el.dataset.settingsTask);
      if (el.disabled !== disabled) el.disabled = disabled;
      const title = !settingsAdmin() ? settingsT("请先解锁管理员") : "";
      if (el.title !== title) el.title = title;
    });
    root.querySelectorAll("button[data-act], select[data-change], input[data-change='upd-auto']").forEach(el => {
      if (!settingsLegacyManage(el)) return;
      if (!settingsAdmin() && !el.disabled) {
        el.dataset.settingsLocked = "true";
        el.disabled = true;
        el.setAttribute("aria-description", settingsT("请先解锁管理员"));
      } else if (settingsAdmin() && el.dataset.settingsLocked === "true") {
        delete el.dataset.settingsLocked;
        el.disabled = false;
        el.removeAttribute("aria-description");
      }
    });
  }
}
function settingsObserve() {
  if (!SETTINGS.observer) SETTINGS.observer = new MutationObserver(settingsApplyAccess);
  SETTINGS.observer.disconnect();
  [$("#settings-page"), $("#modal-root"), $("#drawer-root")].filter(Boolean).forEach(el => SETTINGS.observer.observe(el, { childList: true, subtree: true }));
  settingsApplyAccess();
}
function shellSettings(languageOnly = false) {
  if (!languageOnly) SETTINGS.epoch++;
  const section = SETTINGS_SECTIONS.find(x => x[0] === SETTINGS.section) || SETTINGS_SECTIONS[0];
  SETTINGS.section = section[0];
  return settingsHTML`<div id="settings-page" class="settings-page">
    <header class="settings-heading"><div><span class="settings-eyebrow">Zephyr HiFi Kit / 设置</span><h1>设置</h1><p>音乐来源、播放能力与服务端管理，集中在这里。</p></div>${settingsButton("settings-refresh", "刷新状态", { icon: "refresh" })}</header>
    <div class="settings-layout"><div class="settings-main"><section id="settings-admin" class="card settings-admin" aria-label="管理员解锁">${settingsNote("正在检查管理员会话…")}</section>
      <header class="settings-section-heading"><h2>${icon(section[2], 20)}${esc(section[1])}</h2><p>${esc(section[3])}</p></header>
      <div id="settings-section-error" role="status" aria-live="polite"></div>
      <div id="settings-content">${settingsSectionShell()}</div>
    </div></div></div>`;
}
function openSettings(section = "profile") {
  SETTINGS.section = SETTINGS_SECTIONS.some(x => x[0] === section) ? section : "profile";
  if (typeof experienceOpenSettings === "function") experienceOpenSettings();
  setView("settings");
  if (typeof renderSidebarNav === "function") renderSidebarNav();
}
function settingsSectionShell() {
  switch (SETTINGS.section) {
    case "profile": return typeof experienceProfileShell === "function" ? experienceProfileShell() : settingsNote("管理员资料界面尚未加载，请刷新页面。", "warn");
    case "authorization": return settingsNote("服务端内置 3 台终端席位，可用许可证扩容。管理员登录与许可证授权相互独立；这里只导入验证用公钥，不生成、上传或导出签发私钥。") + shellKeys();
    case "packages": return settingsNote("这里只维护应用版本、软件包、签名与安装源；安装执行请前往终端中心。当前交付支持 Linux DEB、APT 与受支持的 Linux 归档包，架构相同不代表操作系统兼容：Windows/macOS 文件不能当作 Linux 可安装包。") + settingsHTML`<section class="card mb12"><div class="settings-card-head"><h3>${icon("globe", 17)} 安装源管理</h3>${settingsButton("apt-sources", "安装源管理", { icon: "globe" })}</div><p>可只读浏览 APT 安装源，添加或删除源需解锁管理员。应用名仍打开原版本与入库流程；BOM 高级管理保留在下方，不在这里执行终端安装。</p></section>` + shellCatalog();
    case "about": return settingsAboutShell();
    case "maintenance": return settingsMaintenanceShell();
    default: return settingsFoldersShell();
  }
}
function settingsValidDisplayName(name) { return !!name && Array.from(name).length <= 40 && !/[\u0000-\u001f\u007f-\u009f]/.test(name); }
function settingsAuthFields(setup, prefix = "settings") {
  const disabled = SETTINGS.authBusy ? " disabled" : "";
  return settingsHTML`${setup ? settingsHTML`<label class="field" for="${prefix}-name"><span>管理员姓名 / 称呼</span><input class="inp" id="${prefix}-name" name="display_name" autocomplete="name" maxlength="80" required${disabled}></label>` : ""}<label class="field" for="${prefix}-password"><span>${esc(settingsT(setup ? "设置管理员密码（至少 12 字符）" : "管理员密码"))}</span><input class="inp" id="${prefix}-password" name="password" type="password" autocomplete="${setup ? "new-password" : "current-password"}"${setup ? ' minlength="12"' : ""} required${disabled}></label>${setup ? settingsHTML`<label class="field" for="${prefix}-password-confirm"><span>确认管理员密码</span><input class="inp" id="${prefix}-password-confirm" name="password_confirm" type="password" autocomplete="new-password" minlength="12" required${disabled}></label>` : ""}`;
}
function settingsPaintSession() {
  const el = $("#settings-admin"); if (!el) return;
  const session = SETTINGS.session, signature = JSON.stringify([session, SETTINGS.sessionError, SETTINGS.authBusy, typeof I18n !== "undefined" ? I18n.locale : "zh-CN"]);
  if (el.dataset.signature === signature) return;
  el.dataset.signature = signature;
  if (!session) {
    el.innerHTML = settingsHTML`<h3>${icon("lock", 17)} 管理员解锁</h3>${settingsNote(SETTINGS.sessionError || "正在检查管理员会话…", SETTINGS.sessionError ? "err" : "info")}<p>会话状态未确认，管理操作保持锁定；授权摘要、音乐源和软件包仍可浏览。</p>${settingsButton("settings-session-refresh", "重新检查")}`;
    return;
  }
  if (session.authenticated) {
    el.innerHTML = settingsHTML`<div class="settings-card-head"><h3>${icon("unlock", 17)} 管理员已解锁</h3>${settingsBadge("会话已认证", "ok")}</div><p>身份由服务端 HttpOnly Cookie 管理，不在浏览器存储密码。离开共享电脑前请锁定。</p>${settingsButton("settings-logout", SETTINGS.authBusy ? "正在锁定…" : "锁定管理员", { blocked: SETTINGS.authBusy, icon: "lock" })}`;
    return;
  }
  const setup = !session.configured, canSubmit = session.secure_context && (!setup || session.can_setup);
  el.innerHTML = settingsHTML`<div class="settings-card-head"><h3>${icon("lock", 17)} 管理员解锁</h3>${settingsBadge("只读浏览", "warn")}</div>
    <p>${esc(settingsT(setup ? "首次必须在服务端本机 localhost 设置管理员密码，至少 12 个字符。远程访问不能完成首次设置。" : "输入管理员密码以管理音乐源、授权与维护。不会读取已有密码、保存到本地存储或写入日志。"))}</p>
    ${!session.secure_context ? settingsNote("远程明文 HTTP 不能登录。请使用 HTTPS，或在服务端本机通过 localhost 访问。", "warn") : setup && !session.can_setup ? settingsNote("尚未设置密码，请先在服务端 localhost 完成首次设置。", "warn") : ""}
    ${canSubmit ? settingsHTML`<form data-settings-form="settings-auth-submit" class="settings-auth-form${setup ? " settings-setup-form" : ""}">${settingsAuthFields(setup)}${settingsButton("settings-auth-submit", SETTINGS.authBusy ? "正在验证…" : setup ? "设置并验证会话" : "解锁管理员", { className: "primary", blocked: SETTINGS.authBusy })}</form>` : ""}
    <div id="settings-auth-error" role="status" aria-live="polite"></div><p class="settings-small">未解锁仍可浏览摘要与能力；管理操作由服务端再次校验。</p>`;
}
async function settingsRefreshSession(duringAuth = false) {
  if (SETTINGS.authBusy && !duringAuth) return SETTINGS.session;
  const seq = ++SETTINGS.sessionSeq;
  try {
    const session = await api("GET", "/settings/session");
    if (seq !== SETTINGS.sessionSeq) return;
    if (!session || !["configured", "authenticated", "can_setup", "secure_context"].every(k => typeof session[k] === "boolean")) throw new Error("管理员会话响应不完整");
    SETTINGS.session = session; SETTINGS.sessionError = "";
  } catch (e) {
    if (seq !== SETTINGS.sessionSeq) return;
    SETTINGS.session = null;
    SETTINGS.sessionError = e.status === 404 ? "管理员接口尚不可用，请确认服务端已更新。" : "无法确认管理员会话，请检查连接后重试。";
  }
  if (settingsHere()) { settingsPaintSession(); settingsApplyAccess(); }
  if (typeof experienceSessionChanged === "function") experienceSessionChanged();
  return SETTINGS.session;
}
async function settingsAuthenticate(options = {}) {
  if (SETTINGS.authBusy) return false;
  const prefix = options.prefix === "onboarding" ? "onboarding" : "settings";
  const session = SETTINGS.session, input = $("#" + prefix + "-password");
  if (!input || !session || !session.secure_context || (!session.configured && !session.can_setup)) return false;
  const setup = !session.configured, name = $("#" + prefix + "-name")?.value.trim() || "";
  let password = input.value, confirmation = $("#" + prefix + "-password-confirm")?.value || "";
  input.value = "";
  const confirmInput = $("#" + prefix + "-password-confirm"); if (confirmInput) confirmInput.value = "";
  const fail = message => { settingsError(prefix + "-auth-error", message); settingsToast(message, "err"); };
  if (!password || (setup && (!settingsValidDisplayName(name) || Array.from(password).length < 12 || password !== confirmation))) {
    password = confirmation = ""; fail("首次设置需填写 1–40 字符姓名、至少 12 字符密码并保持两次密码一致；登录需填写密码。"); return false;
  }
  confirmation = "";
  const seq = ++SETTINGS.authSeq, epoch = SETTINGS.epoch, view = S.view;
  if (typeof experienceAuthenticationStarted === "function") experienceAuthenticationStarted();
  SETTINGS.sessionSeq++; // Invalidate any pre-login session read.
  SETTINGS.authBusy = true; settingsPaintSession();
  if (typeof experienceApplyAccess === "function") experienceApplyAccess();
  let failure = "", authenticated = false;
  try {
    await api("POST", setup ? "/settings/setup" : "/settings/login", setup ? { display_name: name, password } : { password });
    password = "";
    await settingsRefreshSession(true);
    if (seq !== SETTINGS.authSeq) return false;
    authenticated = settingsAdmin();
    if (!authenticated) failure = "请求已完成，但未确认已认证会话。请检查浏览器 Cookie 设置，重新登录后继续。";
    if (authenticated && typeof experienceAuthenticated === "function") await experienceAuthenticated(setup, epoch === SETTINGS.epoch && view === S.view);
  } catch (e) {
    failure = e.status === 401 ? "密码不正确或会话未获授权。" : e.status === 403 ? "当前访问不允许登录或首次设置，请使用 HTTPS / 服务端 localhost。" : e.status === 429 ? "尝试次数过多，请稍后再试。" : "管理员验证或资料读取失败，请重新检查状态后继续。";
    if (seq === SETTINGS.authSeq) await settingsRefreshSession(true);
  } finally {
    password = "";
    if (seq === SETTINGS.authSeq) {
      SETTINGS.authBusy = false; settingsPaintSession(); settingsApplyAccess();
      if (typeof experienceApplyAccess === "function") experienceApplyAccess();
    }
  }
  if (seq !== SETTINGS.authSeq) return false;
  if (failure) { fail(failure); return false; }
  settingsToast("管理员会话已解锁", "ok");
  await refreshSettings();
  return authenticated;
}
async function settingsLogout() {
  if (SETTINGS.authBusy) return;
  SETTINGS.authSeq++; SETTINGS.sessionSeq++;
  SETTINGS.authBusy = true;
  SETTINGS.session = SETTINGS.session ? { ...SETTINGS.session, authenticated: false } : null;
  if (typeof experienceSessionChanged === "function") experienceSessionChanged(true);
  settingsPaintSession(); settingsApplyAccess();
  try {
    await api("POST", "/settings/logout", {});
    await settingsRefreshSession(true);
    if (SETTINGS.session && !settingsAdmin()) settingsToast("管理员已锁定", "ok");
    else throw new Error("会话未退出");
  } catch (e) {
    SETTINGS.sessionSeq++; SETTINGS.session = null;
    SETTINGS.sessionError = "锁定失败，无法确认服务端会话已退出，请重新检查并重试锁定。";
    settingsToast(SETTINGS.sessionError, "err");
  } finally {
    SETTINGS.authBusy = false; settingsPaintSession(); settingsApplyAccess();
    if (typeof experienceSessionChanged === "function") experienceSessionChanged();
  }
}
async function settingsFailure(e, target) {
  if (e.status === 401) {
    SETTINGS.session = null;
    await settingsRefreshSession();
  }
  const message = e.status === 401 ? "管理员会话已失效，请重新解锁后重试。" : e.message || "操作失败，请重试。";
  settingsError(target, message); settingsToast(message, "err");
}
async function refreshSettings() {
  if (!settingsHere()) return;
  settingsObserve();
  const epoch = SETTINGS.epoch, seq = ++SETTINGS.refreshSeq, section = SETTINGS.section;
  const current = () => settingsHere(section) && epoch === SETTINGS.epoch && seq === SETTINGS.refreshSeq;
  await settingsRefreshSession();
  if (!current()) return;
  settingsError("settings-section-error", "");
  try {
    if (section === "profile" && typeof refreshExperienceProfile === "function") await refreshExperienceProfile(current);
    else if (section === "authorization") await refreshKeys();
    else if (section === "packages") await refreshCatalog();
    else if (section === "about") await settingsRefreshAbout(current);
    else if (section === "folders") await settingsRefreshFolders(current);
    else if (section === "maintenance") {
      // Register before awaiting so a late response cannot restart a stopped view poll.
      poll("view:server-update", () => { if (settingsHere("maintenance")) return refreshUpdateLive(); }, 4000);
      await Promise.all([settingsRefreshMaintenance(current), refreshUpdate()]);
    }
  } catch (e) {
    if (current()) await settingsFailure(e, "settings-section-error");
  }
  if (current()) settingsApplyAccess();
}

function settingsFoldersShell() {
  return settingsHTML`<section class="card"><h3>${icon("plus", 16)} 添加音乐源</h3><p>本地指运行音乐服务的主机，不是当前浏览器所在电脑。目录只建立索引，不上传原音频。</p>
    <form data-settings-form="settings-local-add" class="settings-source-add"><fieldset data-settings-admin data-settings-task="local" ${settingsAdmin() ? "" : "disabled"}><label class="field" for="settings-local-path"><span>服务端绝对目录</span><input id="settings-local-path" class="inp mono" placeholder="例如 /srv/music 或 D:\\Music" required></label>${settingsButton("settings-local-add", "添加本地目录", { icon: "plus", admin: true, task: "local", className: "primary" })}</fieldset></form>
    <div class="settings-card-foot"><span>音乐存放在 NAS 或 SMB 共享？</span>${settingsButton("settings-nas-add", "添加 NAS 共享", { icon: "folder", admin: true })}</div><div id="settings-folder-error" role="status" aria-live="polite"></div></section>
    <section class="card"><div class="settings-card-head"><h3>已配置音乐源</h3><span id="settings-source-count" class="settings-small">正在读取…</span></div><div id="settings-sources">${settingsNote("正在读取音乐源…")}</div><div id="settings-count-note"></div><p class="settings-small">索引保存在本服务端：重复添加同一地址会复用已有曲目，默认“更新扫描”只重读新增或变化的文件。停用或移除并保留索引后可重新连接；离线与扫描失败不会清空索引。跨服务端请使用音乐 JSON 备份恢复，原音频始终保留在目录中。</p></section>
    <section id="settings-folder-editor" class="card hidden" aria-label="编辑音乐源"></section>`;
}
function settingsMountKind(m) { return m.kind === "local" ? "local" : "smb"; }
function settingsMountLabel(m) { return settingsMountKind(m) === "local" ? m.target : settingsHTML`${m.host}/${m.share}`; }
async function settingsRefreshFolders(current) {
  const [mounts, browse] = await Promise.allSettled([
    api("GET", "/mounts"), api("GET", "/library/browse?category=folders&page_size=96")
  ]);
  if (!current()) return;
  if (mounts.status === "rejected") throw mounts.reason;
  if (!Array.isArray(mounts.value)) throw new Error("音乐源响应格式不正确");
  SETTINGS.mounts = mounts.value; S.mounts = mounts.value;
  SETTINGS.counts = new Map();
  if (browse.status === "fulfilled" && Array.isArray(browse.value?.items)) {
    const items = browse.value.items.slice();
    let partial = false;
    const pages = Math.ceil((Number(browse.value.total) || SETTINGS.mounts.length) / 96);
    for (let page = 2; page <= pages; page++) {
      try {
        const more = await api("GET", `/library/browse?category=folders&page_size=96&page=${page}`);
        if (!current()) return;
        if (Array.isArray(more?.items)) items.push(...more.items); else partial = true;
      } catch (e) { partial = true; }
    }
    if (!current()) return;
    for (const item of items) {
      if (item.kind === "source" && item.mount_id && typeof item.track_count === "number" && Number.isFinite(item.track_count) && item.track_count >= 0) SETTINGS.counts.set(item.mount_id, item.track_count);
    }
    const note = $("#settings-count-note"); if (note) note.innerHTML = partial ? settingsNote("部分源计数读取失败，以 — 标记；可刷新重试，不影响源管理。", "warn") : "";
  } else {
    const el = $("#settings-count-note"); if (el) el.innerHTML = settingsNote("目录曲目计数暂不可用，源管理仍可使用；不会退回全量曲目请求。", "warn");
  }
  settingsPaintSources();
}
function settingsPaintSources() {
  const el = $("#settings-sources"); if (!el) return;
  const archivedCount = SETTINGS.mounts.filter(m => m.status === "archived").length;
  const count = $("#settings-source-count"); if (count) count.textContent = settingsT("{configured} 个已配置 · {archived} 个保留索引", { configured: settingsNumber(SETTINGS.mounts.length - archivedCount), archived: settingsNumber(archivedCount) });
  if (!SETTINGS.mounts.length) { el.innerHTML = settingsNote("还没有音乐源。先添加本地目录或 NAS 共享，然后更新扫描。"); return; }
  el.innerHTML = settingsHTML`<div class="settings-source-list">${SETTINGS.mounts.map(m => {
    const local = settingsMountKind(m) === "local", mounted = m.status === "mounted", archived = m.status === "archived";
    const task = "mount:" + m.id, scanning = m.status === "scanning", busy = SETTINGS.busy.has(task) || scanning;
    const options = { id: m.id, admin: true, task, blocked: scanning };
    const status = scanning ? "扫描中" : archived ? "已移除 · 索引保留" : mounted ? local ? "可用" : "已挂载" : m.status === "unmounted" ? local ? "已停用" : "未挂载" : m.status === "error" ? "连接异常" : m.status || "未知";
    const scan = m.scan_result || {};
    const lastScan = Number.isFinite(m.last_scan) && m.last_scan > 0 ? settingsDate(m.last_scan * 1000, { dateStyle: "medium", timeStyle: "medium" }) : settingsT("尚无记录");
    return settingsHTML`<article class="settings-source"><div class="settings-source-top">${icon(local ? "folder" : "server", 20)}<div class="settings-source-title"><strong>${esc(local ? m.share || settingsMountLabel(m) : settingsMountLabel(m))}</strong><span class="mono">${esc(m.target || "—")}</span></div>${settingsBadge(local ? "本地目录" : "NAS / SMB", local ? "info" : "violet")}</div>
      <div class="settings-source-meta">${settingsBadge(busy ? "操作进行中" : status, busy ? "info" : mounted ? "ok" : "warn")}<span>${esc(SETTINGS.counts.has(m.id) ? settingsT("{count} 首已索引", { count: settingsNumber(SETTINGS.counts.get(m.id)) }) : settingsT("曲目数 —"))}</span>${local ? "" : settingsHTML`<span>SMB ${esc(m.version || "3.0")} · ${esc(settingsT(m.writable ? "可写" : "只读"))}</span>`}</div>
      <p class="settings-small">${esc(settingsT("上次成功扫描：{date}", { date: lastScan }))}${esc(m.last_scan > 0 ? settingsT(" · 新增 {added} / 更新 {updated} / 未变 {unchanged} / 缺失 {removed}", { added: settingsNumber(scan.added ?? 0), updated: settingsNumber(scan.updated ?? 0), unchanged: settingsNumber(scan.unchanged ?? 0), removed: settingsNumber(scan.removed ?? 0) }) : settingsT(" · 旧索引仍保留，首次更新会补齐文件指纹"))}</p>
      ${archived ? settingsHTML`<p class="settings-small">重新添加同一地址或连接即可复用索引；NAS 凭据已移除，需要认证时请先编辑源。</p>` : ""}
      ${m.detail ? settingsHTML`<p class="settings-source-detail">${esc(m.detail)}</p>` : ""}
      <div class="settings-source-actions">${settingsButton("settings-mount-action", scanning ? "扫描中…" : "更新扫描", { ...options, icon: "refresh", legacy: "lib-scan", blocked: scanning || !mounted })}
        ${settingsButton("settings-mount-action", "全量重扫…", { ...options, legacy: "lib-scan-full", blocked: scanning || !mounted })}
        ${settingsButton("settings-mount-action", archived ? "重新连接" : mounted ? local ? "停用目录" : "卸载共享" : local ? "启用目录" : "挂载共享", { ...options, legacy: mounted ? "lib-unmount" : "lib-mount" })}
        ${settingsButton("settings-mount-test", "连接检测", { ...options, blocked: scanning || (!local && !mounted) })}${settingsButton("settings-mount-edit", "编辑源", { ...options, icon: "edit" })}
        ${archived ? "" : settingsButton("settings-mount-action", "移除并保留索引…", { ...options, legacy: "lib-archive" })}
        ${settingsButton("settings-mount-action", "彻底删除索引…", { ...options, legacy: "lib-delete", className: "danger sm" })}</div><div class="settings-test-result" data-settings-test-result="${esc(m.id)}" role="status"></div></article>`;
  }).join("")}</div>`;
  settingsApplyAccess();
}
function settingsAbsolutePath(path) { return /^(?:\/(?!\/)|[a-z]:[\\/]|\\\\[^\\]+\\[^\\]+)/i.test(path); }
async function settingsLocalAdd() {
  const input = $("#settings-local-path");
  if (!input || SETTINGS.busy.has("local") || !settingsRequireAdmin()) return;
  const path = input.value.trim();
  if (!settingsAbsolutePath(path)) { settingsError("settings-folder-error", "请输入服务端绝对目录，如 /srv/music 或 D:\\Music；不要填写浏览器本机的相对路径。"); return; }
  SETTINGS.busy.add("local"); settingsApplyAccess(); settingsError("settings-folder-error", "");
  try {
    const result = await api("POST", "/mounts", { kind: "local", path });
    input.value = ""; settingsToast(result?.reused ? "已复用同源索引；启用后只需更新扫描，无需重新建立曲库。" : "本地音乐源已添加，请先启用目录再更新扫描。", "ok"); await refreshSettings();
  } catch (e) { await settingsFailure(e, "settings-folder-error"); }
  finally { SETTINGS.busy.delete("local"); settingsApplyAccess(); }
}
async function settingsMountAction(ds) {
  if (!["lib-scan", "lib-scan-full", "lib-mount", "lib-unmount", "lib-archive", "lib-delete"].includes(ds.legacy) || !settingsRequireAdmin()) return;
  const task = "mount:" + ds.id;
  if (SETTINGS.busy.has(task)) return;
  SETTINGS.busy.add(task); settingsPaintSources();
  try { await ACT[ds.legacy]({ id: ds.id }); }
  finally { SETTINGS.busy.delete(task); if (settingsHere("folders")) await refreshSettings(); }
}
async function settingsMountTest(id) {
  const task = "mount:" + id;
  if (SETTINGS.busy.has(task) || !settingsRequireAdmin()) return;
  SETTINGS.busy.add(task); settingsApplyAccess();
  try {
    const result = await api("POST", `/mounts/${enc(id)}/test`, {});
    if (result?.id !== id) throw new Error("连接检测未返回成功确认，请刷新后重试。");
    const target = Array.from(document.querySelectorAll("[data-settings-test-result]")).find(el => el.dataset.settingsTestResult === id);
    const message = "目录可读取。检测未更改挂载状态，也未扫描或清除现有曲目。";
    if (target) target.innerHTML = settingsNote(message);
    settingsToast("连接检测请求已完成", "ok");
  } catch (e) { await settingsFailure(e, "settings-folder-error"); }
  finally { SETTINGS.busy.delete(task); settingsApplyAccess(); }
}
function settingsEditMount(id, languageOnly = false) {
  if (!languageOnly && (!settingsRequireAdmin() || SETTINGS.busy.has("mount:" + id))) return;
  const mount = SETTINGS.mounts.find(m => m.id === id), el = $("#settings-folder-editor");
  if (!mount || !el) return;
  const local = settingsMountKind(mount) === "local";
  const field = (name, label, value = "", type = "text") => settingsHTML`<label class="field" for="settings-edit-${name}"><span>${esc(settingsT(label))}</span><input id="settings-edit-${name}" class="inp" type="${type}"${type === "password" ? ' autocomplete="new-password"' : settingsHTML` value="${esc(value)}"`}></label>`;
  const encoding = mount.text_encoding || "auto";
  const encodingField = settingsHTML`<label class="field" for="settings-edit-encoding"><span>${esc(settingsT("标签/文件名编码"))}</span><select class="inp" id="settings-edit-encoding">${[["auto", "自动（推荐）"], ["utf-8", "UTF-8"], ["gbk", "简体中文（GBK）"], ["big5", "繁体中文（Big5）"], ["shift_jis", "日文（Shift-JIS）"], ["windows-1251", "俄文（Windows-1251）"]].map(([v, label]) => settingsHTML`<option value="${v}"${v === encoding ? " selected" : ""}>${esc(settingsT(label))}</option>`).join("")}</select><div class="f-hint">${esc(settingsT("编码变更后请执行全量重扫以更新已索引的元数据。"))}</div></label>`;
  el.classList.remove("hidden");
  el.innerHTML = settingsHTML`<div class="settings-card-head"><h3>${esc(settingsT("编辑{source}", { source: settingsT(local ? "本地目录" : " NAS 共享") }))}</h3>${settingsButton("settings-mount-edit-cancel", "关闭")}</div><p>当前服务端不允许将现有索引迁到其他目录：更换实际路径或共享位置请添加新源并重扫。更改 NAS 凭据或选项前请先卸载；密码留空保留，不会读取已保存密码。</p><form data-settings-form="settings-mount-save" data-id="${esc(id)}"><fieldset data-settings-admin data-settings-task="edit:${esc(id)}"><div class="settings-form-grid">${local ? field("path", "服务端绝对目录（位置不可修改）", mount.target) + field("label", "音乐源名称", mount.share) : field("host", "主机", mount.host) + field("share", "共享名", mount.share) + field("username", "用户名（空为 guest）", mount.username) + field("password", "密码（留空保留）", "", "password") + field("domain", "域", mount.domain) + settingsHTML`<label class="field" for="settings-edit-version"><span>SMB 版本</span><select class="inp" id="settings-edit-version">${["3.0", "3.1.1", "2.1", "2.0", "1.0"].map(v => settingsHTML`<option${v === (mount.version || "3.0") ? " selected" : ""}>${v}</option>`).join("")}</select></label>`}${encodingField}</div>${local ? "" : settingsHTML`<label class="settings-checkbox"><input id="settings-edit-writable" type="checkbox" ${mount.writable ? "checked" : ""}>允许写入共享（曲库仅读取；默认只读）</label>`}<div id="settings-edit-error" role="status"></div>${settingsButton("settings-mount-save", "保存音乐源", { id, admin: true, task: "edit:" + id, className: "primary", icon: "check" })}</fieldset></form>`;
  for (const id of ["settings-edit-path", "settings-edit-host", "settings-edit-share"]) { const input = $("#" + id); if (input) input.readOnly = true; }
  settingsApplyAccess();
  if (!languageOnly) { el.scrollIntoView({ behavior: "smooth", block: "nearest" }); $(local ? "#settings-edit-label" : "#settings-edit-username")?.focus(); }
}
async function settingsSaveMount(id) {
  const mount = SETTINGS.mounts.find(m => m.id === id), task = "edit:" + id;
  if (!mount || SETTINGS.busy.has(task) || !settingsRequireAdmin()) return;
  const local = settingsMountKind(mount) === "local", read = key => $("#settings-edit-" + key)?.value || "";
  const encoding = read("encoding");
  const body = local ? { kind: "local", path: read("path").trim(), share: read("label").trim(), text_encoding: encoding } : { kind: "smb", host: read("host").trim(), share: read("share").trim(), username: read("username").trim(), password: read("password"), domain: read("domain").trim(), version: read("version"), text_encoding: encoding, writable: !!$("#settings-edit-writable")?.checked };
  const password = $("#settings-edit-password"); if (password) password.value = "";
  if ((local && !settingsAbsolutePath(body.path)) || (!local && (!body.host || !body.share || /[/\\\s]/.test(body.host + body.share)))) {
    if (!local) body.password = "";
    settingsError("settings-edit-error", local ? "请输入服务端绝对目录。" : "请填写主机及单段共享名，不能包含斜杠或空格。"); return;
  }
  SETTINGS.busy.add(task); settingsApplyAccess(); settingsError("settings-edit-error", "");
  try {
    await api("PATCH", `/mounts/${enc(id)}`, body);
    settingsToast("音乐源配置已保存。", "ok");
    settingsCloseEditor(); await refreshSettings();
  } catch (e) { await settingsFailure(e, "settings-edit-error"); }
  finally { if (!local) body.password = ""; SETTINGS.busy.delete(task); settingsApplyAccess(); }
}
function settingsCloseEditor() {
  const el = $("#settings-folder-editor"); if (el) { el.innerHTML = ""; el.classList.add("hidden"); }
}

function settingsAboutShell() {
  const components = [
    ["QPlay", "当前尚未提供 QPlay 组件，不包含腾讯官方实现。"],
    ["MPD", "Music Player Daemon 开源项目，可从项目公开发布或 Linux APT 软件源获取。"],
    ["AirPlay", "shairport-sync / nqptp 开源项目，可从项目公开发布或支持的软件源获取。"],
    ["UPnP / OpenHome", "upmpdcli、MPD、gmrender 开源项目，可从项目公开发布或 Linux APT 软件源获取；gmrender 仅用于 UPnP。"],
    ["HQPlayer NAA", "Signalyst networkaudiod 厂商组件，公开获取渠道为厂商官方发布；非开源，受厂商许可约束。"],
    ["ZAT", "Zephyr HiFi Kit 自有音频传输实现，随本项目源代码公开，采用 MIT 许可。"]
  ];
  return settingsHTML`<section class="card"><h3>${icon("music", 17)} Zephyr HiFi Kit</h3><p>和风HiFi音乐播放套件</p><p>让音乐回归聆听，让管理化繁为简。</p><p>面向家庭与小型商用 Hi-Fi 场景的音乐服务与终端推送平台，集中管理本地 / NAS 音乐库、ZAT 音频推流与播放终端。</p>
      <p>服务端版本：<strong id="settings-about-version" class="mono" role="status" aria-live="polite">未获取</strong></p><p class="settings-small">版本由当前服务端报告；未获取时可点击“刷新状态”重试。</p>
      <p><a href="https://www.zephyr.hk" target="_blank" rel="noopener noreferrer">官方网站</a> · <a href="https://github.com/Eyebright/Zephyr-HiFi-Kit" target="_blank" rel="noopener noreferrer">源代码仓库</a></p></section>
    <section class="card" aria-labelledby="settings-component-title"><h3 id="settings-component-title">${icon("speaker", 17)} 组件来源与获取渠道</h3><p>开源项目、厂商公开发布及自有实现的来源如下，未提供的组件另行注明。</p><dl class="settings-component-list">${components.map(([name, source]) => settingsHTML`<div><dt>${esc(name)}</dt><dd>${esc(settingsT(source))}</dd></div>`).join("")}</dl></section>
    <section class="card settings-disclaimer" aria-labelledby="settings-disclaimer-title"><h3 id="settings-disclaimer-title">${icon("info", 17)} 免责声明</h3><ul class="settings-disclaimer-list">
      <li><strong>许可与归属：</strong>本项目采用 <a href="https://github.com/Eyebright/Zephyr-HiFi-Kit/blob/main/LICENSE" target="_blank" rel="noopener noreferrer">MIT 许可</a>，复制或分发时须保留版权声明及许可声明。第三方组件遵循各自许可，项目、产品名、协议名及商标归各自权利人所有。</li>
      <li><strong>非官方关联：</strong>名称引用仅用于说明来源与兼容性，不表示获得 Apple、腾讯、Signalyst 或其他权利人的官方授权、认证、背书或合作。</li>
      <li><strong>支持边界：</strong>列出来源不表示任何服务已安装或正在运行，也不保证所有协议能力可用；实际支持取决于组件版本、构建、设备与配置。</li>
      <li><strong>无担保与责任限制：</strong>在适用法律允许的范围内，软件按“原样”提供，不作任何明示或默示保证；作者及版权持有人不承担因使用软件产生的索赔、损害或其他责任。</li>
      <li><strong>合法使用：</strong>公开可获取不等于开源或可自由再分发；请遵守组件许可及适用法律，仅使用合法授权的音源与服务，并自行妥善备份数据。</li>
    </ul><p class="settings-small">Copyright (c) 2026 Zephyr HiFi Kit contributors</p></section>`;
}
async function settingsRefreshAbout(current) {
  let version = "未获取";
  try {
    const metrics = await rootJson("/metrics/server");
    if (typeof metrics?.version === "string" && metrics.version.trim()) version = metrics.version.trim();
  } catch (e) { /* Keep the about page readable when server metrics are unavailable. */ }
  if (!current()) return;
  const el = $("#settings-about-version"); if (el) { el.textContent = version === "未获取" ? settingsT(version) : version; el.dataset.version = version === "未获取" ? "" : version; }
}

function settingsMaintenanceShell() {
  return settingsHTML`<section class="card" aria-label="服务端自升级"><p>这里只管理服务端自升级；检查、应用更新与自动升级开关均需管理员解锁。终端 Agent 升级策略请前往终端中心的高级维护。</p><div id="upd-body" data-update-scope="server"><div id="upd-server"></div></div></section>
    <section class="card"><h3>音乐数据概况</h3><div id="settings-maintenance-counts" class="settings-count-grid">${settingsNote("解锁管理员后读取维护统计。")}</div></section>
    <div id="settings-maintenance-error" role="status" aria-live="polite"></div>
    <section class="card"><h3>${icon("download", 17)} 音乐数据备份</h3><p>下载音乐 JSON：包括源的非凭据配置、曲库、封面、播放列表与播放历史。不包含账号凭据、终端身份、许可证、签发私钥或原音频。</p><p class="settings-small">备份中可能包含音乐目录、曲目及收听记录，请妥善保管。</p>${settingsButton("settings-backup", "下载音乐 JSON 备份", { admin: true, task: "maintenance", icon: "download", className: "primary" })}</section>
    <section class="card settings-danger-zone"><h3>${icon("upload", 17)} 从备份恢复</h3><p>恢复会覆盖当前音乐数据。建议先下载备份；不会恢复凭据或原音频，恢复后请检查目录路径、NAS 凭据并按需重扫。恢复前请停止播放并卸载所有 SMB 共享。</p><fieldset data-settings-admin data-settings-task="maintenance" ${settingsAdmin() ? "" : "disabled"}><label class="field" for="settings-restore-file"><span>选择音乐 JSON 备份（最大 64 MiB）</span><input id="settings-restore-file" class="inp" type="file" accept=".json,application/json" data-change="settings-restore-file"></label><p id="settings-restore-file-note" class="settings-small" role="status">只在你选择文件并点击恢复后读取内容。</p>${settingsButton("settings-restore", "恢复并覆盖音乐数据…", { admin: true, task: "maintenance", className: "danger" })}</fieldset></section>
    <section class="card"><h3>清理音乐数据</h3><div class="settings-maintenance-row"><div><strong>孤立封面</strong><p>清理不再被曲库或播放历史引用的封面缓存，不删除原音频。</p></div>${settingsButton("settings-cleanup", "清理孤立封面…", { admin: true, task: "maintenance", kind: "orphan-covers" })}</div><div class="settings-maintenance-row"><div><strong>曲库索引</strong><p>清空曲库索引，不删除原音频、播放历史或播放列表。列表中的曲目可能显示缺失，需重扫源。播放中拒绝清理。</p></div>${settingsButton("settings-cleanup", "清空曲库索引…", { admin: true, task: "maintenance", kind: "catalog", className: "danger sm" })}</div></section>
    <section class="card"><h3>${icon("audit", 17)} 操作日志清理</h3><p>仅清理所选日期之前的数据库操作日志，不清理进程文件日志。截止日期必须早于今天；截止点为浏览器本地时区该日 00:00，服务端会再次校验。</p><fieldset data-settings-admin data-settings-task="maintenance" ${settingsAdmin() ? "" : "disabled"}><label class="field settings-date-field" for="settings-audit-before"><span>保留此日期及之后的操作日志</span><input id="settings-audit-before" class="inp" type="date" required></label>${settingsButton("settings-cleanup", "按日期清理操作日志…", { admin: true, task: "maintenance", kind: "audit", className: "danger sm" })}</fieldset></section>`;
}
async function settingsRefreshMaintenance(current) {
  if (!settingsAdmin()) {
    SETTINGS.maintenance = null;
    const el = $("#settings-maintenance-counts"); if (el) el.innerHTML = settingsNote("维护统计、备份、恢复与清理均需管理员解锁。");
    return;
  }
  const result = await api("GET", "/settings/maintenance");
  if (!current()) return;
  SETTINGS.maintenance = result;
  settingsPaintMaintenance();
}
function settingsPaintMaintenance() {
  const result = SETTINGS.maintenance;
  if (!result || !settingsAdmin()) {
    const el = $("#settings-maintenance-counts"); if (el) el.innerHTML = settingsNote("维护统计、备份、恢复与清理均需管理员解锁。");
    return;
  }
  const counts = result?.counts || result, el = $("#settings-maintenance-counts"); if (!el) return;
  el.innerHTML = [["tracks", "曲目"], ["mounts", "音乐源"], ["playlists", "播放列表"], ["history", "历史记录"], ["covers", "封面"], ["audit", "操作日志"]].map(([key, label]) => settingsHTML`<div class="settings-count"><span>${esc(settingsT(label))}</span><strong>${typeof counts?.[key] === "number" && Number.isFinite(counts[key]) && counts[key] >= 0 ? esc(settingsNumber(counts[key])) : "—"}</strong></div>`).join("");
}
async function settingsEnsureIdle() {
  const nodes = await api("GET", "/nodes");
  if (!Array.isArray(nodes)) throw new Error("无法检查终端播放状态，请稍后重试。");
  if (nodes.some(n => n.playing)) throw new Error("有终端正在播放，已取消操作。请停止播放后重试；服务端会再次校验。");
}
async function settingsBackup() {
  if (SETTINGS.busy.has("maintenance") || !settingsRequireAdmin()) return;
  SETTINGS.busy.add("maintenance"); settingsApplyAccess(); settingsError("settings-maintenance-error", "");
  try {
    const response = await fetch(API_BASE + "/settings/backup", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: "{}" });
    if (!response.ok) { const error = new Error(settingsT("备份失败（HTTP {status}），未生成下载文件。", { status: response.status })); error.status = response.status; throw error; }
    if (!/application\/(?:json|octet-stream)/i.test(response.headers.get("Content-Type") || "")) throw new Error("备份响应不是音乐 JSON 文件，已取消下载。");
    const blob = await response.blob();
    if (!blob.size) throw new Error("服务端返回了空备份，已取消下载。");
    const url = URL.createObjectURL(blob), link = document.createElement("a");
    link.href = url; link.download = `zephyr-music-${new Date().toISOString().slice(0, 10)}.json`;
    document.body.appendChild(link); link.click(); link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 60000);
    settingsToast("音乐备份已生成并交给浏览器下载。", "ok"); await refreshSettings();
  } catch (e) { await settingsFailure(e, "settings-maintenance-error"); }
  finally { SETTINGS.busy.delete("maintenance"); settingsApplyAccess(); }
}
function settingsRestoreFileChanged(input) {
  const file = input.files?.[0], note = $("#settings-restore-file-note");
  if (note) note.textContent = !file ? settingsT("只在你选择文件并点击恢复后读取内容。") : file.size > 64 * 1024 * 1024 ? settingsT("文件超过 64 MiB，请选择更小的音乐 JSON 备份。") : settingsT("{name} · {size}；尚未读取内容。", { name: file.name, size: fmtBytes(file.size) });
}
async function settingsRestore() {
  if (SETTINGS.busy.has("maintenance") || !settingsRequireAdmin()) return;
  const input = $("#settings-restore-file"), file = input?.files?.[0];
  if (!file || !file.size || file.size > 64 * 1024 * 1024) { settingsError("settings-maintenance-error", "请选择非空音乐 JSON 备份，文件最大 64 MiB。"); return; }
  SETTINGS.busy.add("maintenance"); settingsApplyAccess(); settingsError("settings-maintenance-error", "");
  let backup;
  try {
    try { backup = JSON.parse(await file.text()); } catch (e) { throw new Error("无法读取或解析 JSON 备份，请检查文件；当前数据未更改。"); }
    if (!backup || typeof backup !== "object" || Array.isArray(backup)) throw new Error("备份必须是音乐 JSON 对象；版本与字段由服务端进一步校验。");
    const confirmed = await confirmDlg({ title: settingsT("覆盖音乐数据？"), message: settingsT("将用 {name} 覆盖当前音乐源配置、曲库、封面、播放列表和历史。凭据与原音频不在备份内。请确认已备份当前音乐数据，且所有终端已停止播放。", { name: file.name }), okText: settingsT("恢复并覆盖") });
    if (!confirmed || !input.isConnected || !settingsRequireAdmin()) return;
    await settingsEnsureIdle();
    const result = await api("POST", "/settings/restore", { confirm: "RESTORE", backup });
    if (result?.restored !== true) throw new Error("服务端未返回恢复成功确认，请刷新状态核实后再决定是否重试。");
    input.value = ""; settingsRestoreFileChanged(input);
    settingsToast("音乐数据已恢复，请检查源路径与 NAS 凭据。", "ok"); await refreshSettings();
  } catch (e) { await settingsFailure(e, "settings-maintenance-error"); }
  finally { backup = null; SETTINGS.busy.delete("maintenance"); settingsApplyAccess(); }
}
async function settingsCleanup(kind) {
  if (!["orphan-covers", "catalog", "audit"].includes(kind) || SETTINGS.busy.has("maintenance") || !settingsRequireAdmin()) return;
  let before = 0, date = "";
  if (kind === "audit") {
    date = $("#settings-audit-before")?.value || "";
    before = Math.floor(new Date(date + "T00:00:00").getTime() / 1000);
    const now = new Date(), today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() / 1000;
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(before) || before <= 0 || before >= today) { settingsError("settings-maintenance-error", "请选择早于今天的有效操作日志截止日期。"); return; }
  }
  const messages = {
    "orphan-covers": ["清理孤立封面？", "只删除不再被引用的封面缓存，不删除原音频。"],
    catalog: ["清空曲库索引？", "将清空曲库索引，不删除原音频、播放历史或播放列表。列表中的曲目可能暂时缺失，需重新扫描音乐源。播放中拒绝执行。"],
    audit: ["清理操作日志？", settingsT("将删除 {date} 00:00（浏览器本地时区）之前的数据库操作日志，无法撤销。不清理进程文件日志。", { date })]
  };
  const epoch = SETTINGS.epoch;
  SETTINGS.busy.add("maintenance"); settingsApplyAccess(); settingsError("settings-maintenance-error", "");
  try {
    if (!await confirmDlg({ title: settingsT(messages[kind][0]), message: settingsT(messages[kind][1]), okText: settingsT("确认清理") })) return;
    if (!settingsHere("maintenance") || epoch !== SETTINGS.epoch || !settingsRequireAdmin()) return;
    if (kind === "catalog") await settingsEnsureIdle();
    const result = await api("POST", "/settings/cleanup", { kind, before, confirm: "CLEAN" });
    if (result?.kind !== kind || typeof result.deleted !== "number" || result.deleted < 0) throw new Error("服务端未返回清理结果，请刷新状态核实后重试。");
    settingsToast(settingsT("清理已完成：删除 {count} 条记录", { count: settingsNumber(result.deleted) }), "ok"); await refreshSettings();
  } catch (e) { await settingsFailure(e, "settings-maintenance-error"); }
  finally { SETTINGS.busy.delete("maintenance"); settingsApplyAccess(); }
}

// Preserve embedded controls and use cached state without replaying requests.
function settingsLanguageChanged() {
  if (!settingsHere()) return;
  const next = document.createElement("div"); next.innerHTML = shellSettings(true);
  for (const selector of [".settings-heading", ".settings-section-heading"]) {
    const current = $("#settings-page " + selector), replacement = next.querySelector(selector);
    if (current && replacement) current.replaceWith(replacement);
  }
  const admin = $("#settings-admin");
  if (admin) { admin.setAttribute("aria-label", settingsT("管理员解锁")); delete admin.dataset.signature; settingsPaintSession(); }
  const content = $("#settings-content"), replacement = next.querySelector("#settings-content");
  if (!content || !replacement) return;
  if (SETTINGS.section === "authorization" || SETTINGS.section === "packages") {
    // Retain embedded roots so pending responses can still update them.
    const count = SETTINGS.section === "packages" ? 2 : 1;
    for (let i = 0; i < count; i++) {
      const old = content.children[i], fresh = replacement.children[0];
      if (old && fresh) old.replaceWith(fresh);
    }
    if (SETTINGS.section === "authorization") { paintLicense(); paintKeys(); }
    else {
      for (const id of ["cat-apps", "cat-boms"]) {
        const old = $("#" + id)?.parentElement.querySelector(".sec-head");
        const fresh = replacement.querySelector("#" + id)?.parentElement.querySelector(".sec-head");
        if (old && fresh) old.replaceWith(fresh);
      }
      paintApps(); paintBoms();
    }
  } else {
    const update = $("#upd-body"), version = $("#settings-about-version")?.dataset.version;
    const editor = $("#settings-folder-editor [data-settings-form='settings-mount-save']")?.dataset.id;
    const countNote = $("#settings-count-note");
    content.replaceChildren(...replacement.childNodes);
    if (SETTINGS.section === "folders") {
      settingsPaintSources();
      if (editor) settingsEditMount(editor, true);
      if (countNote) $("#settings-count-note")?.replaceWith(countNote);
    } else if (SETTINGS.section === "maintenance") {
      if (update) $("#upd-body")?.replaceWith(update);
      paintUpdateServer(); settingsPaintMaintenance();
    } else if (SETTINGS.section === "about" && version) {
      const target = $("#settings-about-version"); if (target) { target.textContent = version; target.dataset.version = version; }
    }
  }
  settingsObserve();
}

Object.assign(ACT, {
  "settings-open": ds => openSettings(ds.section || "profile"),
  "settings-refresh": () => refreshSettings(),
  "settings-session-refresh": () => settingsRefreshSession(),
  "settings-auth-submit": () => settingsAuthenticate(),
  "settings-logout": () => settingsLogout(),
  "settings-local-add": () => settingsLocalAdd(),
  "settings-nas-add": () => { if (settingsRequireAdmin()) openMountAdd(); },
  "settings-mount-action": ds => settingsMountAction(ds),
  "settings-mount-test": ds => settingsMountTest(ds.id),
  "settings-mount-edit": ds => settingsEditMount(ds.id),
  "settings-mount-edit-cancel": () => settingsCloseEditor(),
  "settings-mount-save": ds => settingsSaveMount(ds.id),
  "settings-backup": () => settingsBackup(),
  "settings-restore": () => settingsRestore(),
  "settings-cleanup": ds => settingsCleanup(ds.kind)
});
Object.assign(CHG, {
  "settings-restore-file": el => settingsRestoreFileChanged(el)
});
function settingsCaptureAccess(event) {
  if (!settingsHere() || settingsAdmin()) return;
  const el = event.target.closest("[data-act], [data-change]");
  if (!el || !el.closest("#settings-page, #modal-root, #drawer-root")) return;
  if (!settingsLegacyManage(el) && !el.closest("[data-settings-admin]")) return;
  event.preventDefault(); event.stopImmediatePropagation();
  settingsRequireAdmin(); settingsApplyAccess();
}
document.addEventListener("click", settingsCaptureAccess, true);
document.addEventListener("change", settingsCaptureAccess, true);
document.addEventListener("submit", event => {
  const form = event.target.closest("[data-settings-form]");
  if (!form || !form.dataset.settingsForm.startsWith("settings-")) return;
  event.preventDefault();
  const action = ACT[form.dataset.settingsForm];
  if (action) Promise.resolve().then(() => action(form.dataset)).catch(e => settingsFailure(e, "settings-section-error"));
});
