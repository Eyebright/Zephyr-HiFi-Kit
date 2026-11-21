"use strict";

/* Dictation for search inputs via the browser Web Speech API. Each input marked
   data-voice gets a microphone button; recognition text is written back into
   the input as it arrives so the normal search debounce/Enter paths apply. */
(function () {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
  const V = { rec: null, input: null, btn: null, active: false, base: "", acc: "", moPending: false };
  const tr = key => (typeof I18n !== "undefined" ? I18n.t("app." + key) : key);
  const MIC = '<svg class="ic" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="2.5" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0014 0"/><path d="M12 18v3.5"/><path d="M8.5 21.5h7"/></svg>';

  function voiceLang() {
    const lang = String(document.documentElement.lang || "zh-CN").toLowerCase();
    if (lang.startsWith("zh-tw") || lang.startsWith("zh-hant")) return "zh-TW";
    if (lang.startsWith("en")) return "en-US";
    return "zh-CN";
  }

  function reset() {
    V.active = false; V.input = null; V.btn = null; V.acc = "";
    document.querySelectorAll(".voice-btn.live").forEach(b => {
      b.classList.remove("live");
      b.title = tr("voiceInput"); b.setAttribute("aria-label", tr("voiceInput"));
    });
  }

  function finish(input) {
    input.dispatchEvent(new Event("change", { bubbles: true }));
    if (typeof ACT === "object") {
      if (input.id === "lb-search") ACT["lb-search"]();
      else if (input.id === "fav-search") ACT["favorite-search"]();
    }
    input.focus();
  }

  function toggle(input, btn) {
    if (V.active && V.input === input) { if (V.rec) V.rec.stop(); return; }
    if (V.rec) { try { V.rec.stop(); } catch (e) { /* already stopped */ } }
    const rec = new SR();
    V.rec = rec; V.input = input; V.btn = btn; V.active = true;
    V.base = input.value.trim(); V.acc = "";
    btn.classList.add("live"); btn.title = tr("voiceListening"); btn.setAttribute("aria-label", tr("voiceListening"));
    rec.lang = voiceLang(); rec.interimResults = true; rec.continuous = false; rec.maxAlternatives = 1;
    rec.onresult = ev => {
      let interim = "";
      for (let i = ev.resultIndex; i < ev.results.length; i++) {
        const r = ev.results[i];
        if (r.isFinal) V.acc += r[0].transcript; else interim += r[0].transcript;
      }
      input.value = [V.base, (V.acc + interim).trim()].filter(Boolean).join(" ");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    };
    rec.onerror = ev => {
      if (ev.error === "not-allowed" || ev.error === "service-not-allowed") toast(tr("voiceDenied"), "err");
      else if (ev.error !== "aborted" && ev.error !== "no-speech") toast(tr("voiceFailed"), "err");
    };
    rec.onend = () => {
      const input = V.input, done = V.active;
      const text = V.acc.trim();
      reset();
      if (done && input) { if (text) finish(input); else input.focus(); }
    };
    try { rec.start(); } catch (e) { reset(); toast(tr("voiceFailed"), "err"); }
  }

  function decorate() {
    document.querySelectorAll("input[data-voice]:not([data-voice-ready])").forEach(input => {
      input.dataset.voiceReady = "1";
      const host = input.parentElement;
      if (!host) return;
      const field = document.createElement("span");
      field.className = "voice-field";
      host.insertBefore(field, input);
      field.appendChild(input);
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "voice-btn";
      btn.setAttribute("aria-label", tr("voiceInput"));
      btn.title = tr("voiceInput");
      btn.innerHTML = MIC;
      btn.addEventListener("click", e => { e.preventDefault(); e.stopPropagation(); toggle(input, btn); });
      field.appendChild(btn);
    });
  }

  function boot() {
    if (!SR) return;
    decorate();
    const observer = new MutationObserver(() => {
      if (V.moPending) return;
      V.moPending = true;
      requestAnimationFrame(() => { V.moPending = false; decorate(); });
    });
    observer.observe(document.body, { childList: true, subtree: true });
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", boot); else boot();
})();
