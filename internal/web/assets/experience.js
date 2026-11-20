"use strict";

const XP = {
  profile: null, active: false, stage: "welcome", dismissed: false, loading: false, error: "", busy: null,
  epoch: 0, profileSeq: 0, bootstrapSeq: 0, summarySeq: 0, summary: null,
  authenticated: false, settingsOpen: false, mobileOpen: false,
  route: "", navEpoch: 0, initialized: false, media: null,
  languageSeq: 0, languageEditSeq: 0, languageBusy: false, languageLoading: false,
  languageLoaded: false, languageDraft: null, languageDirty: false, languageNotice: "", languageNoticeKind: "info",
  preferenceNotice: "", preferenceNoticeKind: "info"
};
const EXPERIENCE_STEPS = ["管理员与安全", "添加音乐源", "连接播放终端"];
// Preserve the exported array interface while resolving labels at render time.
EXPERIENCE_STEPS.forEach((source, index) => Object.defineProperty(EXPERIENCE_STEPS, index, { get: () => settingsT(source), enumerable: true }));
Object.assign(ICONS, {
  user: '<circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0116 0v2"/>',
  home: '<path d="M3 10l9-7 9 7v11H3V10z"/><path d="M9 21v-8h6v8"/>',
  library: '<path d="M4 4v16M9 4v16M14 4v16M18 4l3 16"/>',
  settings: '<path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3"/><circle cx="15" cy="17" r="3"/>'
});

function renderSidebarNav(languageOnly = false) {
  const route = S.view + (S.view === "settings" ? ":" + SETTINGS.section : "");
  const changed = !languageOnly && route !== XP.route;
  if (changed) {
    XP.route = route; XP.navEpoch++;
    if (S.view === "settings") XP.settingsOpen = true;
    if (XP.mobileOpen) toggleExperienceNav(false);
  }
  const nav = $("#nav"); if (!nav) return;
  const focused = nav.contains(document.activeElement) ? { ...document.activeElement.dataset } : null;
  const items = [...NAV.filter(n => n.id !== "settings"), ...NAV.filter(n => n.id === "settings")];
  nav.innerHTML = items.map(n => {
    const active = S.view === n.id;
    if (n.id !== "settings") return settingsHTML`<button type="button" class="nav-item${active ? " active" : ""}" data-act="nav" data-view="${esc(n.id)}"${active ? ' aria-current="page"' : ""}>${icon(n.icon, 17)}<span>${esc(n.name)}</span></button>`;
    return settingsHTML`<div class="xp-settings-group"><button type="button" class="nav-item xp-settings-toggle${active ? " active" : ""}" data-act="xp-settings-toggle" data-view="settings" aria-expanded="${XP.settingsOpen}" aria-controls="sidebar-settings">${icon(n.icon, 17)}<span>${esc(n.name)}</span>${icon("chevR", 14, "xp-chevron")}</button><div id="sidebar-settings" class="xp-settings-children"${XP.settingsOpen ? "" : " hidden"}>${SETTINGS_SECTIONS.map(([id, name, ic]) => settingsHTML`<button type="button" class="xp-settings-link${active && SETTINGS.section === id ? " active" : ""}" data-act="settings-open" data-section="${esc(id)}"${active && SETTINGS.section === id ? ' aria-current="page"' : ""}>${icon(ic, 15)}<span>${esc(name)}</span></button>`).join("")}</div></div>`;
  }).join("");
  if (focused) Array.from(nav.querySelectorAll("button")).find(el => el.dataset.act === focused.act && el.dataset.view === focused.view && el.dataset.section === focused.section)?.focus();
  if (changed) experiencePaintOnboarding();
}
function experienceOpenSettings() { XP.settingsOpen = true; toggleExperienceNav(false); }
function toggleExperienceNav(open = !XP.mobileOpen) {
  XP.mobileOpen = !!open && !!XP.media?.matches;
  document.body.classList.toggle("xp-nav-open", XP.mobileOpen);
  const toggle = $("#mobile-nav-toggle"), sidebar = $("#sidebar"), shade = $("#nav-backdrop"), main = $("#app > .main");
  toggle?.setAttribute("aria-expanded", String(XP.mobileOpen));
  if (shade) shade.hidden = !XP.mobileOpen;
  if (sidebar) { sidebar.inert = !!XP.media?.matches && !XP.mobileOpen; sidebar.setAttribute("aria-label", settingsT("主导航")); }
  if (main) main.inert = XP.mobileOpen;
  if (XP.mobileOpen) $("#mobile-nav-close")?.focus();
  else if (sidebar?.contains(document.activeElement)) toggle?.focus();
}
function experienceNavKey(event) {
  if (!XP.mobileOpen) return;
  if (event.key === "Escape") { event.preventDefault(); event.stopImmediatePropagation(); toggleExperienceNav(false); }
  if (event.key === "Tab") {
    const focusable = $$("button:not(:disabled), a[href]", $("#sidebar")).filter(el => !el.closest("[hidden]"));
    const first = focusable[0], last = focusable[focusable.length - 1];
    if (event.shiftKey && (document.activeElement === first || !$("#sidebar").contains(document.activeElement))) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  }
}
function experienceInit() {
  if (XP.initialized) return;
  XP.initialized = true;
  try { XP.dismissed = localStorage.getItem("zephyr-onboarding-dismissed") === "1"; }
  catch { settingsToast("浏览器无法读取引导显示选择，仍可开始配置或跳过。", "warn"); }
  XP.media = window.matchMedia("(max-width: 980px)");
  const resize = () => toggleExperienceNav(false);
  if (XP.media.addEventListener) XP.media.addEventListener("change", resize);
  else XP.media.addListener(resize);
  document.addEventListener("keydown", experienceNavKey, true);
  document.addEventListener("click", event => {
    const item = event.target.closest("[data-act]");
    if (XP.mobileOpen && item?.dataset.act === "nav" && item.closest("#nav")) toggleExperienceNav(false);
  }, true);
  toggleExperienceNav(false);
}
function experienceValidateProfile(value) {
  if (!value || typeof value.display_name !== "string" || typeof value.onboarding_complete !== "boolean" || typeof value.legacy !== "boolean" || !Number.isInteger(value.onboarding_step) || value.onboarding_step < 1 || value.onboarding_step > 3) throw new Error("管理员资料或引导进度响应不完整，请检查服务端版本后重试。");
  if ((value.onboarding_complete && value.onboarding_step !== 3) || (value.onboarding_step > 1 && !value.display_name.trim() && !(value.legacy && value.onboarding_complete))) throw new Error("管理员资料与引导进度不一致，请重新检查，不能跳过姓名设置。");
  return { display_name: value.display_name, onboarding_step: value.onboarding_step, onboarding_complete: value.onboarding_complete, legacy: value.legacy };
}
function experiencePaintAccount() {
  const name = XP.profile?.display_name.trim() || "", label = $("#xp-account-label"), button = $("#xp-account");
  if (label) label.textContent = name || settingsT("管理员");
  button?.setAttribute("aria-label", name ? settingsT("{name} · 管理员与偏好", { name }) : settingsT("管理员与偏好"));
  button?.setAttribute("title", name || settingsT("管理员与偏好"));
}
function experienceSetProfile(profile) {
  const changed = JSON.stringify(XP.profile) !== JSON.stringify(profile);
  XP.profile = profile;
  experiencePaintAccount();
  if (changed) document.dispatchEvent(new CustomEvent("profile-updated", { detail: { ...profile } }));
}
async function refreshExperienceProfile(current = () => true) {
  if (XP.busy) return;
  const seq = ++XP.profileSeq, epoch = XP.epoch;
  const profile = experienceValidateProfile(await api("GET", "/settings/profile"));
  if (!current() || seq !== XP.profileSeq || epoch !== XP.epoch || XP.busy) return;
  const input = $("#settings-display-name"), dirty = input && input.value !== (XP.profile?.display_name || "");
  experienceSetProfile(profile);
  const el = $("#settings-profile-content");
  if (el) {
    experiencePreserveForms([el], () => { el.innerHTML = experienceProfileContent(); });
    if (input?.isConnected && !dirty) input.value = profile.display_name;
  }
  experiencePaintOnboarding(); experienceApplyAccess();
  return profile;
}
async function bootstrapExperience() {
  experienceInit();
  // Independent of profile/session loading: failure must never block login or setup.
  void experienceRefreshLanguage();
  if (XP.busy || SETTINGS.authBusy) return;
  const seq = ++XP.bootstrapSeq, navEpoch = XP.navEpoch;
  XP.loading = true; XP.error = ""; experiencePaintOnboarding();
  try {
    const [profile] = await Promise.all([api("GET", "/settings/profile").then(experienceValidateProfile), settingsRefreshSession()]);
    if (seq !== XP.bootstrapSeq) return;
    if (!SETTINGS.session) throw new Error(SETTINGS.sessionError || "管理员会话尚未确认。");
    experienceSetProfile(profile); XP.active = !profile.onboarding_complete && !XP.dismissed;
    XP.loading = false;
    if (XP.active && XP.stage === "steps" && navEpoch === XP.navEpoch) experienceShowStep();
    else experiencePaintOnboarding();
  } catch (e) {
    if (seq !== XP.bootstrapSeq) return;
    XP.error = e.message || "无法读取引导状态，请重试。";
  } finally {
    if (seq === XP.bootstrapSeq) { XP.loading = false; experiencePaintOnboarding(); }
  }
}
function experienceSessionChanged(invalidate = false) {
  const authenticated = settingsAdmin();
  const changed = authenticated !== XP.authenticated;
  if (invalidate || (XP.authenticated && !authenticated)) {
    XP.epoch++; XP.profileSeq++; XP.bootstrapSeq++; XP.summarySeq++;
    XP.loading = false; XP.summary = null;
    if (XP.stage === "complete") XP.stage = "steps";
    // Keep the mutation lock until its request settles; late results are discarded.
  }
  XP.authenticated = authenticated;
  if (changed || invalidate || !SETTINGS.session) experiencePaintOnboarding();
  experienceApplyAccess();
}
function experienceAuthenticationStarted() {
  XP.bootstrapSeq++; XP.profileSeq++; XP.loading = false;
}
async function experienceAuthenticated(setup, navigate = true) {
  XP.profileSeq++; XP.bootstrapSeq++;
  const epoch = XP.epoch;
  const profile = experienceValidateProfile(await api("GET", "/settings/profile"));
  if (epoch !== XP.epoch || !settingsAdmin()) return;
  experienceSetProfile(profile); XP.loading = false; XP.error = ""; XP.active = !profile.onboarding_complete && !XP.dismissed;
  if (setup && (profile.onboarding_step !== 2 || !profile.display_name.trim())) {
    XP.error = "管理员已创建，但服务端尚未确认第 2 步进度；请重新检查状态，不要重复设置密码。";
    experiencePaintOnboarding(); return;
  }
  if (XP.active && XP.stage === "steps" && navigate) experienceShowStep(); else experiencePaintOnboarding();
}
function experienceStep() {
  if (!XP.profile || !SETTINGS.session?.configured || !settingsAdmin() || !XP.profile.display_name.trim()) return 1;
  return XP.profile.onboarding_step;
}
function experienceButton(action, label, options = {}) {
  return settingsHTML`<button type="button" class="btn ${options.primary ? "primary" : "ghost"} sm" data-act="${action}"${options.step ? settingsHTML` data-step="${options.step}"` : ""}${options.admin ? " data-xp-admin" : ""}${options.lock === false ? "" : " data-xp-lock"}${options.disabled || (options.admin && !settingsAdmin()) || (options.lock !== false && (XP.busy || SETTINGS.authBusy)) ? " disabled" : ""}>${esc(settingsT(label))}</button>`;
}
function experienceProfileShell() { return settingsHTML`<div id="settings-profile-content">${experienceProfileContent()}</div>`; }
function experienceProfileContent() {
  return settingsHTML`<section class="card"><h3>${icon("user", 17)} 怎么称呼你？</h3><p>用于首页问候和管理员显示；不是新的登录账号。密码仅用于管理员解锁。</p>${!XP.profile ? settingsNote("正在读取管理员资料…") : settingsHTML`<form data-experience-form="xp-profile-save"><label class="field" for="settings-display-name"><span>管理员姓名 / 称呼（必填）</span><input class="inp" id="settings-display-name" name="display_name" autocomplete="name" maxlength="80" required value="${esc(XP.profile.display_name)}" data-xp-admin data-xp-lock${!settingsAdmin() || XP.busy ? " disabled" : ""}></label>${experienceButton("xp-profile-save", "保存称呼", { primary: true, admin: true })}</form>`}<div id="settings-profile-error" role="status" aria-live="polite"></div><p class="settings-small">语言立即生效，无需重启。此浏览器可覆盖服务端默认语言；音乐来源、授权和服务接入可从侧栏分别配置。</p>${XP.profile && !XP.profile.onboarding_complete ? experienceButton("xp-resume", "继续使用引导") : ""}</section>${experienceLanguageCard()}`;
}
function experienceLanguageOptions(value, preference = false) {
  const options = preference ? [["server", settingsT("服务端默认")], ["browser", settingsT("跟随浏览器")]] : [];
  options.push(["zh-CN", "简体中文"], ["zh-TW", "繁體中文"], ["en", "English"]);
  return options.map(([locale, label]) => `<option value="${locale}"${value === locale ? " selected" : ""}>${esc(label)}</option>`).join("");
}
function experienceBrowserLanguage(id = "ui-language-preference") {
  const preference = typeof I18n !== "undefined" ? I18n.preference : "server";
  return settingsHTML`<div class="xp-browser-language"><label class="field" for="${id}"><span>此浏览器语言</span><select class="inp" id="${id}" data-change="xp-language-preference">${experienceLanguageOptions(preference, true)}</select></label><div data-xp-language-feedback role="status" aria-live="polite">${XP.preferenceNotice ? settingsNote(XP.preferenceNotice, XP.preferenceNoticeKind) : ""}</div></div>`;
}
function experienceLanguageCard() {
  const locale = XP.languageDraft || (typeof I18n !== "undefined" ? I18n.defaultLocale : "zh-CN");
  return settingsHTML`<section class="card settings-language-card" id="settings-language-card"><h3>${icon("globe", 17)} 语言</h3><p>浏览器语言对未登录访客也有效，仅影响此浏览器。服务端默认语言仅管理员可保存。</p>${experienceBrowserLanguage()}<div class="settings-language-server"><label class="field" for="server-default-language"><span>服务端默认语言</span><select class="inp" id="server-default-language" data-change="xp-default-language" data-settings-admin data-settings-task="language"${!settingsAdmin() || XP.languageBusy ? " disabled" : ""}>${experienceLanguageOptions(locale)}</select></label>${settingsButton("xp-language-save", XP.languageBusy ? "正在保存…" : "保存默认语言", { admin: true, task: "language", className: "primary", blocked: XP.languageBusy })}${settingsButton("xp-language-refresh", "重新检查默认语言", { blocked: XP.languageBusy })}</div><div id="settings-language-status" role="status" aria-live="polite">${XP.languageLoading ? settingsNote("正在读取默认语言…") : XP.languageNotice ? settingsNote(XP.languageNotice, XP.languageNoticeKind) : ""}</div></section>`;
}
function experienceLanguageFeedback() {
  for (const select of $$("[data-change='xp-language-preference']")) select.value = typeof I18n !== "undefined" ? I18n.preference : "server";
  for (const el of $$("[data-xp-language-feedback]")) el.innerHTML = XP.preferenceNotice ? settingsNote(XP.preferenceNotice, XP.preferenceNoticeKind) : "";
  const status = $("#settings-language-status");
  if (status) status.innerHTML = XP.languageLoading ? settingsNote("正在读取默认语言…") : XP.languageNotice ? settingsNote(XP.languageNotice, XP.languageNoticeKind) : "";
  const select = $("#server-default-language");
  if (select && XP.languageDraft) select.value = XP.languageDraft;
  const save = $("[data-act='xp-language-save']");
  if (save) { save.textContent = settingsT(XP.languageBusy ? "正在保存…" : "保存默认语言"); save.dataset.settingsBlocked = String(XP.languageBusy); }
  const refresh = $("[data-act='xp-language-refresh']"); if (refresh) refresh.disabled = XP.languageBusy;
  settingsApplyAccess();
}
function experiencePreferenceChanged(input) {
  if (!["server", "browser", "zh-CN", "zh-TW", "en"].includes(input.value) || typeof I18n === "undefined") return;
  const saved = I18n.setPreference(input.value);
  XP.preferenceNotice = saved ? "浏览器语言已应用并保存。" : "浏览器无法保存语言偏好，本次切换仍已生效；关闭页面后可能恢复原设置。";
  XP.preferenceNoticeKind = saved ? "info" : "warn";
  experienceLanguageFeedback(); settingsToast(XP.preferenceNotice, saved ? "ok" : "warn");
}
function experienceValidateLanguage(value) {
  if (!value || !["zh-CN", "zh-TW", "en"].includes(value.default_language)) throw new Error("默认语言响应不完整，请检查服务端版本。");
  return value.default_language;
}
async function experienceRefreshLanguage() {
  if (XP.languageBusy || typeof I18n === "undefined") return;
  const seq = ++XP.languageSeq, editSeq = XP.languageEditSeq;
  XP.languageLoading = true; experienceLanguageFeedback();
  try {
    const locale = experienceValidateLanguage(await api("GET", "/settings/language"));
    if (seq !== XP.languageSeq || XP.languageBusy) return;
    XP.languageLoaded = true;
    if (editSeq === XP.languageEditSeq && !XP.languageDirty) XP.languageDraft = locale;
    XP.languageNotice = ""; XP.languageNoticeKind = "info";
    I18n.setDefault(locale);
  } catch {
    if (seq !== XP.languageSeq) return;
    XP.languageNotice = "无法读取服务端默认语言，当前语言与登录不受影响。可重新检查或稍后重试。"; XP.languageNoticeKind = "warn";
  } finally {
    if (seq === XP.languageSeq) { XP.languageLoading = false; experienceLanguageFeedback(); }
  }
}
async function experienceSaveLanguage() {
  if (XP.languageBusy || !settingsRequireAdmin() || typeof I18n === "undefined") return false;
  const input = $("#server-default-language"), locale = input?.value;
  if (!["zh-CN", "zh-TW", "en"].includes(locale)) return false;
  const seq = ++XP.languageSeq, epoch = XP.epoch, authSeq = SETTINGS.authSeq, editSeq = XP.languageEditSeq;
  const current = () => seq === XP.languageSeq && epoch === XP.epoch && authSeq === SETTINGS.authSeq && settingsAdmin();
  XP.languageBusy = true; XP.languageLoading = false; XP.languageDraft = locale; XP.languageNotice = "";
  SETTINGS.busy.add("language"); experienceLanguageFeedback();
  try {
    await api("PATCH", "/settings/language", { default_language: locale });
    if (!current()) return false;
    const confirmed = experienceValidateLanguage(await api("GET", "/settings/language"));
    if (!current()) return false;
    if (confirmed !== locale) throw new Error("服务端未确认默认语言已保存，请重新检查后重试。");
    XP.languageLoaded = true;
    if (editSeq === XP.languageEditSeq) { XP.languageDraft = confirmed; XP.languageDirty = false; }
    XP.languageNotice = "服务端默认语言已保存。"; XP.languageNoticeKind = "info";
    I18n.setDefault(confirmed);
    settingsToast(XP.languageNotice, "ok");
    return true;
  } catch (error) {
    if (!current()) return false;
    XP.languageNotice = error.status === 401 ? "管理员会话已失效，请重新解锁后重试。" : settingsTranslations()?.reverse.has(error.message) ? error.message : "默认语言保存失败，请重新检查后重试。";
    XP.languageNoticeKind = "err";
    settingsToast(XP.languageNotice, "err");
    if (error.status === 401) {
      SETTINGS.session = SETTINGS.session ? { ...SETTINGS.session, authenticated: false } : null;
      experienceSessionChanged(true); settingsPaintSession();
    }
    return false;
  } finally {
    if (seq === XP.languageSeq) {
      if (!current() && XP.languageNoticeKind !== "err") { XP.languageNotice = "管理员会话已变化，默认语言保存结果未确认。请重新检查。"; XP.languageNoticeKind = "warn"; }
      XP.languageBusy = false; SETTINGS.busy.delete("language"); experienceLanguageFeedback();
    }
  }
}
function experienceAuthContent() {
  const session = SETTINGS.session;
  if (!session) return settingsNote(SETTINGS.sessionError || "正在确认管理员会话…", "warn") + experienceButton("xp-retry", "重新检查状态", { lock: false });
  if (session.authenticated) return settingsHTML`<p>请确认首页使用的称呼。已有管理员无需重新设置密码。</p><form data-experience-form="xp-identity-next"><label class="field" for="onboarding-display-name"><span>管理员姓名 / 称呼（必填）</span><input class="inp" id="onboarding-display-name" name="display_name" autocomplete="name" maxlength="80" required value="${esc(XP.profile?.display_name || "")}" data-xp-admin data-xp-lock></label>${experienceButton("xp-identity-next", "保存并继续", { primary: true, admin: true })}</form>`;
  const setup = !session.configured;
  return settingsHTML`<p>${esc(settingsT(setup ? "先为你的音乐空间设置管理员姓名与密码。首次设置仅允许在服务端本机 localhost 完成，密码至少 12 个字符。" : "已保存引导进度。使用已有管理员密码解锁后继续，不会重设密码。"))}</p>${!session.secure_context ? settingsNote("远程明文 HTTP 不能登录。请改用 HTTPS 或服务端本机 localhost。", "warn") : setup && !session.can_setup ? settingsNote("首次设置必须在服务端本机通过 localhost 完成，远程访问不能创建管理员。", "warn") : settingsHTML`<form class="xp-auth-form" data-experience-form="xp-auth">${settingsAuthFields(setup, "onboarding")}${experienceButton("xp-auth", setup ? "创建管理员并继续" : "登录并继续", { primary: true })}</form>`}<div id="onboarding-auth-error" role="status" aria-live="polite"></div>`;
}
function experienceErrorContent() { return XP.error ? settingsNote(XP.error, "err") + experienceButton("xp-retry", "重新检查已保存状态", { lock: false }) : ""; }
function experienceProgress(step, complete = false) {
  return settingsHTML`<ol class="xp-progress" aria-label="三步引导进度">${EXPERIENCE_STEPS.map((label, index) => settingsHTML`<li class="${complete || index + 1 < step ? "done" : index + 1 === step ? "active" : ""}"${!complete && index + 1 === step ? ' aria-current="step"' : ""}><span>${esc(settingsNumber(index + 1))}</span>${esc(label)}</li>`).join("")}</ol>`;
}
function experiencePaintOnboarding() {
  const root = $("#onboarding-root"); if (!root) return;
  root.hidden = false;
  if (XP.loading) { root.innerHTML = settingsHTML`<div class="xp-onboarding card">${settingsNote("正在读取管理员资料与首次使用进度…")}</div>`; return; }
  if (!XP.profile) { root.innerHTML = XP.error ? settingsHTML`<div class="xp-onboarding card">${settingsNote(XP.error, "err")}${experienceButton("xp-retry", "重新检查引导状态", { lock: false })}</div>` : ""; root.hidden = !XP.error; return; }
  if (XP.profile.onboarding_complete) {
    root.hidden = !!XP.profile.display_name.trim() && !XP.error;
    root.innerHTML = settingsHTML`${experienceErrorContent()}${!XP.profile.display_name.trim() ? settingsHTML`<div class="xp-profile-prompt card"><div><strong>欢迎回来，补充一个称呼吧</strong><p>原有实例已就绪，无需重新设置密码或重走引导。</p></div><button type="button" class="btn ghost sm" data-act="settings-open" data-section="profile">补充管理员姓名</button></div>` : ""}`;
    return;
  }
  if (!XP.active) { root.hidden = true; root.innerHTML = ""; return; }
  if (XP.stage === "welcome") {
    const continuing = SETTINGS.session?.configured || XP.profile.onboarding_step > 1;
    root.innerHTML = settingsHTML`<section class="xp-onboarding xp-intro card settings-page" aria-labelledby="onboarding-title"><span class="settings-eyebrow">Zephyr HiFi Kit</span><h2 tabindex="-1" id="onboarding-title">欢迎使用 Zephyr HiFi Kit</h2><p class="xp-intro-lead">只需简单3步完成配置就可开始体验</p>${experienceBrowserLanguage("onboarding-language-preference")}${experienceProgress(0)}${continuing ? settingsHTML`<p>${esc(settingsT("已保存到第 {step} 步，继续配置不会清除已有设置。", { step: settingsNumber(XP.profile.onboarding_step) }))}</p>` : ""}<div class="xp-intro-actions">${experienceButton("xp-resume", continuing ? "继续配置" : "开始配置", { primary: true })}${experienceButton("xp-skip", "跳过，我自己配置")}</div><p class="settings-small">可随时从设置 → 管理员与偏好继续使用引导。</p><div id="onboarding-error" role="status" aria-live="polite">${experienceErrorContent()}</div></section>`;
    experienceApplyAccess(); return;
  }
  const step = experienceStep();
  if (XP.stage === "complete" && step === 3) {
    root.innerHTML = settingsHTML`<section class="xp-onboarding xp-intro card settings-page" aria-labelledby="onboarding-title"><span class="settings-eyebrow">Zephyr HiFi Kit</span><h2 tabindex="-1" id="onboarding-title">如沐和风，音乐随您而行！</h2><p class="xp-intro-lead">配置引导已走完，随时可以在设置中继续调整。</p>${experienceProgress(3, true)}<div id="onboarding-summary">${experienceSummaryContent()}</div><div id="onboarding-error" role="status" aria-live="polite">${experienceErrorContent()}</div><div class="xp-intro-actions">${experienceButton("xp-finish", "开始聆听", { primary: true, admin: true })}${experienceButton("xp-resume", "返回配置", { admin: true })}${experienceButton("xp-skip", "跳过，我自己配置")}</div><p class="settings-small">点击开始聆听后结束引导并进入主页，不会自动播放音乐。</p></section>`;
    experienceApplyAccess(); return;
  }
  const inPlace = step === 2 ? S.view === "settings" && SETTINGS.section === "folders" : step === 3 ? S.view === "nodes" : true;
  let content = "";
  if (step === 1) content = experienceBrowserLanguage("onboarding-language-preference") + experienceAuthContent();
  if (step === 2) content = settingsHTML`<p>在下方添加服务端本地目录或 NAS 共享，启用 / 挂载后检测连接并扫描。这里直接复用音乐文件夹管理，不上传原音频。</p><div class="xp-step-actions">${experienceButton("xp-folders", inPlace ? "刷新音乐源状态" : "打开音乐文件夹", { lock: false })}</div>${inPlace ? "" : settingsNote("音乐源控件位于设置 → 音乐文件夹。可随时返回引导，已保存的进度不会丢失。")}`;
  if (step === 3) content = settingsHTML`<p>使用现有添加终端向导生成注册令牌，在你选择的目标设备上手动安装。注册、上线与授权是不同状态；此引导不会自动安装软件或执行终端命令。</p><div class="xp-step-actions">${experienceButton("xp-add-node", "添加播放终端…", { primary: true, admin: true })}${experienceButton("xp-nodes", "查看终端与真实状态", { lock: false })}${experienceButton("xp-authorization", "查看授权与席位", { lock: false })}</div><p class="settings-small">添加向导关闭后，可在终端中心查看详情、手动授权和配置 DAC。未配置的项目可稍后在侧栏补充，不影响完成设置。</p><div id="onboarding-summary">${experienceSummaryContent()}</div>${experienceButton("xp-summary-refresh", "刷新真实摘要", { lock: false })}`;
  root.innerHTML = settingsHTML`<section class="xp-onboarding${step === 1 ? " xp-identity" : ""} card settings-page" aria-label="首次使用引导"><div class="xp-onboarding-head"><div><span class="settings-eyebrow">Zephyr HiFi Kit</span><h2 tabindex="-1" id="onboarding-title">${esc(settingsNumber(step))}. ${esc(EXPERIENCE_STEPS[step - 1])}</h2></div><span class="settings-small">进度保存在服务端</span></div>${experienceProgress(step)}<div class="xp-step-body">${content}</div><div id="onboarding-error" role="status" aria-live="polite">${experienceErrorContent()}</div><footer class="xp-onboarding-footer">${step > 1 ? experienceButton("xp-step", "上一步", { step: step - 1, admin: true }) : ""}${!inPlace ? experienceButton("xp-resume", "返回引导") : ""}${experienceButton("xp-skip", "跳过，我自己配置")}<span class="spacer"></span>${step === 2 ? experienceButton("xp-step", "稍后配置", { step: step + 1, admin: true }) + experienceButton("xp-step", "下一步", { step: step + 1, primary: true, admin: true }) : step === 3 ? experienceButton("xp-review", "完成配置", { primary: true, admin: true }) : ""}</footer></section>`;
  experienceApplyAccess();
}
function experienceApplyAccess() {
  for (const el of $$("[data-xp-admin], [data-xp-lock]")) {
    const blocked = (el.hasAttribute("data-xp-admin") && !settingsAdmin()) || (el.hasAttribute("data-xp-lock") && (!!XP.busy || SETTINGS.authBusy));
    el.disabled = !!blocked;
  }
  const form = $("#onboarding-root [data-experience-form='xp-auth']");
  form?.querySelectorAll("input, button").forEach(el => { el.disabled = SETTINGS.authBusy || !!XP.busy; });
}
function experienceShowStep() {
  if (!XP.profile || XP.profile.onboarding_complete || XP.dismissed) { experiencePaintOnboarding(); return; }
  XP.active = true; XP.stage = "steps";
  const step = experienceStep();
  if (step === 2 && (S.view !== "settings" || SETTINGS.section !== "folders")) openSettings("folders");
  if (step === 3 && S.view !== "nodes") setView("nodes");
  experiencePaintOnboarding();
  $("#onboarding-title")?.focus();
  $("#onboarding-root")?.scrollIntoView({ block: "start", behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
  if (step === 3) return experienceRefreshSummary();
}
function experienceRememberDismissed(dismissed) {
  XP.dismissed = dismissed;
  try {
    if (dismissed) localStorage.setItem("zephyr-onboarding-dismissed", "1");
    else localStorage.removeItem("zephyr-onboarding-dismissed");
  } catch { settingsToast("浏览器未能记住引导显示选择，本次操作仍有效。", "warn"); }
}
function experienceSkip() {
  if (XP.busy || SETTINGS.authBusy || XP.loading || !XP.profile || XP.profile.onboarding_complete) return false;
  XP.bootstrapSeq++; XP.profileSeq++; XP.summarySeq++;
  XP.active = false; XP.stage = "welcome"; XP.summary = null; XP.error = "";
  experienceRememberDismissed(true);
  setView("overview"); experiencePaintOnboarding();
  const heading = $("#ov-greeting");
  heading?.setAttribute("tabindex", "-1"); heading?.focus();
  return true;
}
async function experienceResume() {
  if (XP.busy || SETTINGS.authBusy || XP.loading) return false;
  const navEpoch = XP.navEpoch;
  if (!XP.profile || XP.error || !SETTINGS.session) await bootstrapExperience();
  if (!XP.profile || XP.error || !SETTINGS.session || XP.profile.onboarding_complete || navEpoch !== XP.navEpoch) return false;
  experienceRememberDismissed(false);
  return experienceShowStep();
}
function experienceReview() {
  if (XP.busy || SETTINGS.authBusy || !XP.active || XP.stage !== "steps" || experienceStep() !== 3 || !settingsRequireAdmin()) return false;
  XP.stage = "complete";
  experiencePaintOnboarding();
  $("#onboarding-title")?.focus();
  $("#onboarding-root")?.scrollIntoView({ block: "start", behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
  return true;
}
function experienceFailure(error, target = "onboarding-error") {
  const message = error.status === 401 ? "管理员会话已失效，请重新登录后继续。进度未确认保存。" : error.message || "保存失败，请重新检查状态后重试。";
  XP.error = message;
  settingsError(target, message);
  const progressError = $("#onboarding-error"); if (progressError) progressError.innerHTML = experienceErrorContent();
  settingsToast(message, "err");
  if (error.status === 401) {
    SETTINGS.session = SETTINGS.session ? { ...SETTINGS.session, authenticated: false } : null;
    experienceSessionChanged(true); settingsPaintSession(); settingsApplyAccess();
  }
}
async function experienceSaveName(inputId, advance = false) {
  if (XP.busy || SETTINGS.authBusy || !settingsRequireAdmin()) return false;
  const name = $("#" + inputId)?.value.trim() || "";
  const target = advance ? "onboarding-error" : "settings-profile-error";
  if (!settingsValidDisplayName(name)) { settingsError(target, "请填写 1–40 字符的管理员姓名 / 称呼，不能包含控制字符。"); return false; }
  const token = {}, epoch = XP.epoch, navEpoch = XP.navEpoch;
  XP.busy = token; XP.profileSeq++; XP.bootstrapSeq++; XP.error = ""; experienceApplyAccess();
  let saved = false;
  try {
    await api("PATCH", "/settings/profile", { display_name: name });
    if (epoch !== XP.epoch || !settingsAdmin()) return false;
    const profile = experienceValidateProfile(await api("GET", "/settings/profile"));
    if (epoch !== XP.epoch || !settingsAdmin()) return false;
    if (profile.display_name !== name) throw new Error("服务端未确认称呼已保存，请刷新资料后重试。");
    experienceSetProfile(profile); saved = true;
    settingsError(target, ""); settingsToast("管理员称呼已保存", "ok");
  } catch (e) { if (epoch === XP.epoch) experienceFailure(e, target); }
  finally { if (XP.busy === token) XP.busy = null; experienceApplyAccess(); }
  if (saved && advance && navEpoch === XP.navEpoch) return experiencePersistStep(Math.max(2, XP.profile.onboarding_step));
  if (saved) experiencePaintOnboarding();
  return saved;
}
async function experiencePersistStep(step, complete = false) {
  if (XP.busy || SETTINGS.authBusy || !settingsRequireAdmin()) return false;
  const profile = XP.profile, displayed = experienceStep();
  if (!profile || profile.onboarding_complete || !profile.display_name.trim()) { settingsError("onboarding-error", "请先保存管理员姓名。"); return false; }
  if (!Number.isInteger(step) || step < 1 || step > 3 || (Math.abs(step - profile.onboarding_step) > 1) || (complete && (displayed !== 3 || step !== 3))) return false;
  const token = {}, epoch = XP.epoch, navEpoch = XP.navEpoch;
  XP.busy = token; XP.profileSeq++; XP.bootstrapSeq++; XP.summarySeq++; XP.error = ""; experienceApplyAccess();
  let saved = false;
  try {
    await api("PATCH", "/settings/onboarding", { step, complete });
    if (epoch !== XP.epoch || !settingsAdmin()) return false;
    const result = experienceValidateProfile(await api("GET", "/settings/profile"));
    if (epoch !== XP.epoch || !settingsAdmin()) return false;
    if (result.onboarding_step !== step || result.onboarding_complete !== complete || !result.display_name.trim()) throw new Error("服务端未确认进度保存；请重新检查状态，当前不会跳步或完成引导。");
    experienceSetProfile(result); XP.active = !complete && !XP.dismissed; XP.summary = null; saved = true;
  } catch (e) { if (epoch === XP.epoch) experienceFailure(e); }
  finally { if (XP.busy === token) XP.busy = null; experienceApplyAccess(); }
  if (saved) {
    if (navEpoch === XP.navEpoch) {
      if (complete) { settingsToast("设置完成 · 如沐和风，开始聆听", "ok"); setView("overview"); } else await experienceShowStep();
    }
    experiencePaintOnboarding();
  }
  return saved;
}
function experienceSummaryContent() {
  if (!XP.summary) return settingsNote("配置摘要尚未读取，可点击刷新；不影响完成设置。");
  const { nodes, license } = XP.summary;
  const mounts = XP.summary.mounts.filter(m => m.status !== "archived");
  const local = mounts.filter(m => m.kind === "local").length;
  const pending = [];
  if (!mounts.length) pending.push("音乐源待添加");
  else if (!mounts.some(m => m.status === "mounted")) pending.push("音乐源待连接");
  if (!nodes.length) pending.push("播放终端待连接");
  else if (!nodes.some(n => n.status === "online" && n.authorized === true)) pending.push("暂无在线且已授权的播放终端");
  return settingsHTML`<dl class="xp-summary"><div><dt>管理员</dt><dd>${esc(XP.profile.display_name)}</dd></div><div><dt>音乐源</dt><dd>${esc(settingsT("{count} 个 · 本地 {local} / NAS {nas}", { count: settingsNumber(mounts.length), local: settingsNumber(local), nas: settingsNumber(mounts.length - local) }))}</dd></div><div><dt>可用音乐源</dt><dd>${esc(settingsT("{count} 个 · 扫描中 {scanning}", { count: settingsNumber(mounts.filter(m => m.status === "mounted").length), scanning: settingsNumber(mounts.filter(m => m.status === "scanning").length) }))}</dd></div><div><dt>播放终端</dt><dd>${esc(settingsT("{count} 台 · 在线 {online} / 已授权 {authorized}", { count: settingsNumber(nodes.length), online: settingsNumber(nodes.filter(n => n.status === "online").length), authorized: settingsNumber(nodes.filter(n => n.authorized === true).length) }))}</dd></div><div><dt>授权席位</dt><dd>${typeof license.effective_seats === "number" ? esc(settingsNumber(license.effective_seats)) : esc(settingsT("未提供席位数"))}</dd></div></dl>${pending.length ? settingsNote(settingsT("{pending}。结束引导后可从音乐文件夹或终端中心继续配置。", { pending: pending.map(item => settingsT(item)).join(settingsT("；")) }), "warn") : ""}`;
}
async function experienceRefreshSummary() {
  if (!XP.active || experienceStep() !== 3 || XP.busy) return;
  const seq = ++XP.summarySeq, epoch = XP.epoch;
  XP.summary = null; experienceApplyAccess();
  const el = $("#onboarding-summary"); if (el) el.innerHTML = settingsNote("正在读取真实配置摘要…");
  try {
    const [mounts, nodes, license] = await Promise.all([api("GET", "/mounts"), api("GET", "/nodes"), api("GET", "/license")]);
    if (seq !== XP.summarySeq || epoch !== XP.epoch || experienceStep() !== 3) return;
    if (!Array.isArray(mounts) || !Array.isArray(nodes) || !license || typeof license !== "object" || Array.isArray(license)) throw new Error("配置摘要响应不完整，请重试。");
    XP.summary = { mounts, nodes, license }; XP.error = "";
    settingsError("onboarding-error", "");
    const current = $("#onboarding-summary"); if (current) current.innerHTML = experienceSummaryContent();
  } catch (e) {
    if (seq !== XP.summarySeq || epoch !== XP.epoch) return;
    experienceFailure(new Error("无法读取真实配置摘要，请刷新重试。"));
    const current = $("#onboarding-summary"); if (current) current.innerHTML = experienceSummaryContent();
  } finally { if (seq === XP.summarySeq) experienceApplyAccess(); }
}
// In-flight operations retain references to these exact form nodes.
function experiencePreserveForms(roots, paint) {
  roots = roots.filter(Boolean);
  const active = document.activeElement;
  const focusedRoot = roots.find(root => root.contains(active));
  const focus = focusedRoot ? { element: active, id: active.id, data: { ...active.dataset }, tag: active.tagName, start: active.selectionStart, end: active.selectionEnd, direction: active.selectionDirection } : null;
  const controls = roots.flatMap(root => Array.from(root.querySelectorAll("input, select, textarea")).map((el, index) => ({ root, el, index, value: el.value, checked: el.checked })));
  const notices = roots.flatMap(root => Array.from(root.querySelectorAll("[role='status'][id$='-error'], [data-settings-test-result]")).filter(el => el.innerHTML).map(el => ({ root, el })));
  const scrollX = window.scrollX, scrollY = window.scrollY;
  paint();
  for (const { root, el, index, value, checked } of controls) {
    const candidates = Array.from(root.querySelectorAll("input, select, textarea"));
    const next = el.id ? candidates.find(candidate => candidate.id === el.id) : candidates[index];
    if (!next || next === el || next.tagName !== el.tagName || next.type !== el.type) continue;
    for (const name of ["placeholder", "title", "aria-label", "aria-description"]) {
      if (next.hasAttribute(name)) el.setAttribute(name, next.getAttribute(name)); else el.removeAttribute(name);
    }
    el.disabled = next.disabled; el.readOnly = next.readOnly;
    if (el.tagName === "SELECT") el.innerHTML = next.innerHTML;
    if (el.type !== "file") el.value = value;
    if (typeof checked === "boolean") el.checked = checked;
    next.replaceWith(el);
  }
  for (const { root, el } of notices) {
    const next = el.id ? Array.from(root.querySelectorAll("[id]")).find(candidate => candidate.id === el.id) : Array.from(root.querySelectorAll("[data-settings-test-result]")).find(candidate => candidate.dataset.settingsTestResult === el.dataset.settingsTestResult);
    if (next && next !== el) next.replaceWith(el);
  }
  for (const root of roots) for (const el of root.querySelectorAll("[data-settings-message]")) el.textContent = settingsT(decodeURIComponent(el.dataset.settingsMessage));
  experienceLanguageFeedback(); experienceApplyAccess();
  const file = $("#settings-restore-file"); if (file) settingsRestoreFileChanged(file);
  if (focus) {
    const elements = Array.from(focusedRoot.querySelectorAll("[id], button, a, input, select, textarea"));
    const target = focus.element.isConnected ? focus.element : focus.id ? elements.find(el => el.id === focus.id) : elements.find(el => el.tagName === focus.tag && Object.keys(focus.data).length && Object.entries(focus.data).every(([key, value]) => el.dataset[key] === value));
    if (target && !target.disabled) {
      target.focus({ preventScroll: true });
      if (typeof focus.start === "number" && typeof target.setSelectionRange === "function") {
        try { target.setSelectionRange(focus.start, focus.end, focus.direction); } catch { /* Unsupported input type. */ }
      }
    }
  }
  if (typeof window.scrollTo === "function" && (window.scrollX !== scrollX || window.scrollY !== scrollY)) window.scrollTo(scrollX, scrollY);
}
// Keep form nodes and playback state intact during language changes.
function experienceLanguageChanged() {
  experiencePreserveForms([$("#view"), $("#onboarding-root")], () => {
    if (settingsHere()) settingsLanguageChanged();
    else if (S.view === "library") libraryBrowserLanguageChanged();
    else if (S.view === "nodes") terminalCenterLanguageChanged();
    else if (S.view === "audit") {
      const title = $("#view .sec-title"); if (title) title.innerHTML = icon("audit", 17) + " " + appH("m068");
      paintAudit();
    }
    if ($("#onboarding-root")) experiencePaintOnboarding();
  });
  experiencePaintAccount();
  if ($("#sidebar")) $("#sidebar").setAttribute("aria-label", settingsT("主导航"));
  if ($("#nav")) renderSidebarNav(true);
}

Object.assign(CHG, {
  "xp-language-preference": el => experiencePreferenceChanged(el),
  "xp-default-language": el => {
    if (!settingsAdmin() || XP.languageBusy || !["zh-CN", "zh-TW", "en"].includes(el.value)) return;
    XP.languageDraft = el.value; XP.languageDirty = true; XP.languageEditSeq++; XP.languageNotice = ""; experienceLanguageFeedback();
  }
});
Object.assign(ACT, {
  "xp-language-save": () => experienceSaveLanguage(),
  "xp-language-refresh": () => experienceRefreshLanguage(),
  "xp-settings-toggle": () => { XP.settingsOpen = !XP.settingsOpen; renderSidebarNav(); },
  "xp-nav-toggle": () => toggleExperienceNav(),
  "xp-nav-close": () => toggleExperienceNav(false),
  "xp-retry": () => bootstrapExperience(),
  "xp-resume": () => experienceResume(),
  "xp-auth": () => settingsAuthenticate({ prefix: "onboarding" }),
  "xp-profile-save": () => experienceSaveName("settings-display-name"),
  "xp-identity-next": () => experienceSaveName("onboarding-display-name", true),
  "xp-step": ds => experiencePersistStep(Number(ds.step)),
  "xp-skip": () => experienceSkip(),
  "xp-review": () => experienceReview(),
  "xp-finish": () => XP.active && XP.stage === "complete" ? experiencePersistStep(3, true) : false,
  "xp-summary-refresh": () => experienceRefreshSummary(),
  "xp-folders": () => { if (settingsHere("folders")) return refreshSettings(); openSettings("folders"); },
  "xp-nodes": () => setView("nodes"),
  "xp-authorization": () => openSettings("authorization"),
  "xp-add-node": () => { if (settingsRequireAdmin()) { if (S.view !== "nodes") setView("nodes"); openWizard(); } }
});
document.addEventListener("submit", event => {
  const form = event.target.closest("[data-experience-form]"); if (!form) return;
  event.preventDefault();
  const action = ACT[form.dataset.experienceForm];
  if (action) Promise.resolve().then(() => action(form.dataset)).catch(e => experienceFailure(e));
});
