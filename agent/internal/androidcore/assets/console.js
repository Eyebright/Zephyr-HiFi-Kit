"use strict";

const $ = (id) => document.getElementById(id);
const model = {
  identity: {},
  playback: {},
  queue: null,
  queueError: "",
  stateBusy: false,
  queueBusy: false,
  controlBusy: false,
  seeking: false,
  volumeBusy: false,
  coverSrc: "",
  queueSignature: "",
  lastPosition: -1,
  lyrics: null,
  lyricsTrackID: "",
  lyricsSignature: "",
  lyricsBusy: false,
  lyricsError: "",
  lyricIndex: -1,
  lyricClock: { ms: 0, at: 0, playing: false },
  lyricFrame: 0,
  paneManual: false,
  paneAutoPending: true,
  sideRatio: 0.59,
  toastTimer: 0
};

function formatTime(ms) {
  const total = Math.max(0, Math.floor((Number(ms) || 0) / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return h ? `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}` : `${m}:${String(s).padStart(2, "0")}`;
}

function playbackLabel(state) {
  return ({ playing: "正在播放", paused: "已暂停", ready: "准备就绪", idle: "等待播放", stopped: "已停止", error: "播放异常" })[state] || "等待播放";
}

function queueModeLabel(mode) {
  return ({ sequence: "顺序播放", loop_all: "循环全部", loop_one: "单曲循环", shuffle: "随机播放" })[mode] || "顺序播放";
}

async function api(path, method = "GET", body) {
  const options = { method, cache: "no-store" };
  if (body !== undefined) {
    options.headers = { "Content-Type": "application/json" };
    options.body = JSON.stringify(body);
  }
  const response = await fetch(path, options);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

function toast(message, kind = "") {
  const node = $("toast");
  node.textContent = message;
  node.className = `toast show ${kind}`;
  clearTimeout(model.toastTimer);
  model.toastTimer = setTimeout(() => { node.className = "toast"; }, 3200);
}

function addBadge(parent, text, kind) {
  const node = document.createElement("span");
  node.className = `badge ${kind || ""}`;
  node.textContent = text;
  parent.appendChild(node);
}

function coverURL(base, hash, size) {
  if (!base || !/^[a-f0-9]{16,128}$/i.test(String(hash || ""))) return "";
  try {
    const url = new URL(String(base), window.location.origin);
    if (url.protocol !== "http:" && url.protocol !== "https:") return "";
    url.pathname = `/api/v1/covers/${encodeURIComponent(hash)}/${size}`;
    url.search = "";
    url.hash = "";
    return url.toString();
  } catch (_) {
    return "";
  }
}

function currentQueueTrack() {
  const q = model.queue;
  if (!q || !Array.isArray(q.tracks) || !q.tracks.length) return null;
  const pos = Math.max(0, Math.min(Number(q.position) || 0, q.tracks.length - 1));
  return q.tracks[pos] || null;
}

function renderArtwork() {
  const p = model.playback;
  const track = currentQueueTrack();
  const hash = track && p.track_id && track.track_id === p.track_id ? track.cover_hash : "";
  const src = coverURL(model.identity.server_url, hash, 640);
  if (src === model.coverSrc) return;
  model.coverSrc = src;
  const image = $("cover");
  image.hidden = true;
  image.onload = null;
  image.onerror = null;
  image.removeAttribute("src");
  if (!src) return;
  image.onload = () => {
    if (model.coverSrc === src) image.hidden = false;
  };
  image.onerror = () => {
    if (model.coverSrc === src) {
      image.hidden = true;
      image.removeAttribute("src");
    }
  };
  image.referrerPolicy = "no-referrer";
  image.src = src;
}

function lyricElapsed() {
  const clock = model.lyricClock;
  const total = Math.max(0, Number(model.playback.duration_ms) || 0);
  let ms = clock.ms + (clock.playing ? performance.now() - clock.at : 0);
  if (total > 0) ms = Math.min(ms, total);
  return Math.max(0, ms);
}

function syncLyricClock(elapsed, playing) {
  model.lyricClock = { ms: elapsed, at: performance.now(), playing };
}

// centerInList scrolls a list so the item sits at the vertical center of the
// visible area. Viewport-relative rects are used because offsetTop is relative
// to the nearest positioned ancestor (not the list), which previously pushed
// the highlight off-screen under the pane chrome.
function centerInList(list, item) {
  const lr = list.getBoundingClientRect();
  const ir = item.getBoundingClientRect();
  const delta = (ir.top + ir.height / 2) - (lr.top + lr.height / 2);
  const max = Math.max(0, list.scrollHeight - list.clientHeight);
  const top = Math.max(0, Math.min(max, list.scrollTop + delta));
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  list.scrollTo({ top, behavior: reduce ? "auto" : "smooth" });
}

function updateLyricHighlight(force) {
  const lyrics = model.lyrics;
  if (!lyrics || !lyrics.synced || !Array.isArray(lyrics.lines) || !lyrics.lines.length) return;
  const ms = lyricElapsed();
  const lines = lyrics.lines;
  let index = -1;
  for (let i = 0; i < lines.length; i++) {
    if ((Number(lines[i].time_ms) || 0) <= ms) index = i;
    else break;
  }
  const list = $("lyrics-list");
  if (index !== model.lyricIndex || force) {
    model.lyricIndex = index;
    Array.from(list.children).forEach((node, i) => {
      node.classList.toggle("active", i === index);
      node.classList.toggle("past", i < index);
      if (i !== index) node.style.removeProperty("--fill");
    });
    const cur = list.children[index];
    if (cur) centerInList(list, cur);
  }
  // Karaoke fill: sweep the active line from its start time toward the next
  // line's time, updated every animation frame while playing.
  const active = list.children[index];
  if (active) {
    const start = Number(lines[index].time_ms) || 0;
    const end = index + 1 < lines.length ? (Number(lines[index + 1].time_ms) || start + 4000) : start + 4000;
    const span = Math.max(500, end - start);
    const prog = Math.max(0, Math.min(1, (ms - start) / span));
    active.style.setProperty("--fill", (prog * 100).toFixed(1) + "%");
  }
}

function scheduleLyrics() {
  if (model.lyricFrame || document.hidden) return;
  if (model.lyrics && model.lyrics.synced && model.playback.state === "playing") {
    model.lyricFrame = requestAnimationFrame(tickLyrics);
  }
}

function tickLyrics() {
  model.lyricFrame = 0;
  if (document.hidden) return;
  updateLyricHighlight(false);
  scheduleLyrics();
}

function renderLyrics() {
  const list = $("lyrics-list");
  const empty = $("lyrics-empty");
  const status = $("lyrics-status");
  const footer = $("lyrics-footer");
  const title = $("lyrics-empty-title");
  const detail = $("lyrics-empty-detail");
  const trackID = model.playback.track_id || "";
  const lyrics = model.lyrics;
  model.lyricIndex = -1;
  if (!trackID) {
    status.textContent = "等待播放";
    title.textContent = "暂无歌词";
    detail.textContent = "播放曲目后，这里会显示服务端缓存的内嵌歌词或同名 LRC 歌词";
    footer.textContent = "歌词由服务端统一缓存 · 随播放进度同步";
    list.replaceChildren();
    empty.hidden = false;
    return;
  }
  if (model.lyricsError) {
    status.textContent = "读取失败";
    title.textContent = "无法读取歌词";
    detail.textContent = model.lyricsError;
    footer.textContent = "歌词读取失败不影响音频播放";
    list.replaceChildren();
    empty.hidden = false;
    return;
  }
  if (model.lyricsBusy && !lyrics) {
    status.textContent = "读取中";
    title.textContent = "正在读取歌词";
    detail.textContent = "服务端正在提取内嵌歌词或同名 LRC 文件";
    list.replaceChildren();
    empty.hidden = false;
    return;
  }
  if (!lyrics || lyrics.source === "none" || !Array.isArray(lyrics.lines) || !lyrics.lines.length) {
    status.textContent = "暂无歌词";
    title.textContent = "暂无歌词";
    detail.textContent = "可在音频中内嵌歌词，或放置同名 .lrc 文件后重新播放";
    footer.textContent = "歌词由服务端统一缓存";
    list.replaceChildren();
    empty.hidden = false;
    return;
  }
  empty.hidden = true;
  status.textContent = lyrics.synced ? "LRC 同步" : "纯文本";
  footer.textContent = lyrics.source === "embedded" ? "内嵌歌词 · 服务端缓存" : lyrics.source === "sidecar" ? "同名 LRC 文件 · 服务端缓存" : "服务端缓存歌词";
  const signature = JSON.stringify([lyrics.track_id, lyrics.source, lyrics.synced, lyrics.lines]);
  if (signature !== model.lyricsSignature) {
    model.lyricsSignature = signature;
    list.replaceChildren();
    lyrics.lines.forEach((line, index) => {
      const node = document.createElement("div");
      node.className = "lyric-line" + (line.text ? "" : " blank");
      node.dataset.index = String(index);
      node.textContent = line.text || "♪";
      list.appendChild(node);
    });
  }
  if (lyrics.synced) updateLyricHighlight(true);
  else Array.from(list.children).forEach(node => node.classList.remove("active", "past"));
}

async function loadLyrics(trackID) {
  if (model.lyricsBusy) return;
  model.lyricsBusy = true;
  renderLyrics();
  try {
    const lyrics = await api("/api/lyrics");
    if (model.lyricsTrackID === trackID) {
      model.lyrics = lyrics;
      model.lyricsError = "";
    }
  } catch (error) {
    if (model.lyricsTrackID === trackID) model.lyricsError = error.message || "服务端暂时不可用";
  } finally {
    model.lyricsBusy = false;
    if (model.lyricsTrackID === trackID) { renderLyrics(); autoSelectPane(); }
    else if (model.lyricsTrackID) loadLyrics(model.lyricsTrackID);
  }
}

function renderConnection() {
  const p = model.playback;
  const id = model.identity;
  const node = $("connection");
  node.className = "status-pill";
  let label = "连接中";
  let cls = "";
  let short = "连接中";
  if (p.connected) {
    cls = "online"; label = "ZAT 已连接"; short = "已连接";
  } else if (id.registered === false) {
    cls = "offline"; label = "等待注册"; short = "待注册";
  } else if (id.server_reachable === false) {
    cls = "offline"; label = "服务端离线"; short = "离线";
  }
  if (cls) node.classList.add(cls);
  node.querySelector("span").textContent = label;
  const chip = $("link-chip");
  chip.className = "link-chip" + (cls ? " " + cls : "");
  $("link-chip-text").textContent = short;
  $("terminal-name").textContent = id.alias || "Android 终端";
  $("server-state").textContent = id.server_reachable ? "已连接" : "离线";
}

function renderPlayback() {
  const p = model.playback;
  const connected = !!p.connected;
  const playing = connected && p.state === "playing";
  const paused = connected && p.state === "paused";
  const active = playing || paused;
  const hasTrack = !!(p.track_id || p.title);
  const total = Math.max(0, Number(p.duration_ms) || 0);
  const rate = Number(p.rate) || 0;
  const elapsed = Math.min(total || Number.MAX_SAFE_INTEGER, rate ? (Number(p.pts) || 0) / rate * 1000 : 0);
  syncLyricClock(elapsed, playing);
  updateLyricHighlight(false);
  scheduleLyrics();

  renderConnection();
  $("playback-state").textContent = playbackLabel(p.state);
  $("title").textContent = hasTrack ? (p.title || "未命名曲目") : "等待服务端推送音乐";
  $("artist").textContent = hasTrack ? (p.artist || "未知艺术家") : "从服务端播放列表选择一首曲目";
  $("album").textContent = hasTrack ? (p.album || "") : "";
  $("format").textContent = hasTrack ? [p.codec || "—", rate ? `${(rate / 1000).toFixed(1)} kHz` : "—", p.channels ? `${p.channels} ch` : "—"].join("  ·  ") : "";
  $("badges").replaceChildren();
  if (p.bit_perfect) addBadge($("badges"), "BIT PERFECT", "green");
  if (p.mqa) addBadge($("badges"), "MQA", "gold");
  if (p.dop) addBadge($("badges"), p.dsd_rate ? `DSD${p.dsd_rate}` : "DoP", "gold");
  if (p.detail) addBadge($("badges"), p.detail, "cyan");

  const seek = $("seek");
  seek.max = String(Math.round(total));
  seek.disabled = model.controlBusy || !(connected && active && rate > 0 && total > 0);
  if (!model.seeking) {
    seek.value = String(Math.round(elapsed));
    $("elapsed").textContent = formatTime(elapsed);
  }
  $("duration").textContent = formatTime(total);

  const play = $("play-pause");
  play.disabled = model.controlBusy || !active;
  $("play-icon").setAttribute("href", playing ? "#i-pause" : "#i-play");
  $("play-label").textContent = playing ? "暂停" : "继续";
  play.setAttribute("aria-label", playing ? "暂停" : "继续");
  play.title = playing ? "暂停" : "继续";

  const q = model.queue;
  const tracks = q && Array.isArray(q.tracks) ? q.tracks : [];
  const pos = q ? Math.max(0, Number(q.position) || 0) : 0;
  const mode = q && q.mode;
  $("prev").disabled = model.controlBusy || !connected || !tracks.length;
  $("next").disabled = model.controlBusy || !connected || !tracks.length || (mode === "sequence" && pos >= tracks.length - 1);
  $("stop").disabled = model.controlBusy || !connected || (!hasTrack && !p.session_id);
  $("prev").title = pos > 0 && tracks[pos - 1] ? `上一首：${tracks[pos - 1].title || tracks[pos - 1].track_id}` : "重新播放当前曲目";
  $("next").title = pos < tracks.length - 1 && tracks[pos + 1] ? `下一首：${tracks[pos + 1].title || tracks[pos + 1].track_id}` : "下一首";

  const volume = $("volume");
  volume.disabled = !connected || !!(p.mqa || p.dop);
  if (!model.volumeBusy) {
    volume.value = String(p.volume != null ? p.volume : 100);
    $("volume-value").textContent = `${p.volume != null ? p.volume : 100}%`;
  }
  $("output").textContent = p.alsa_device && p.alsa_device !== "default" ? `AAudio · ${p.alsa_device}` : "AAudio 默认输出";
  $("buffer").textContent = `${p.buffer_ms || 0} ms`;
  $("queue-position").textContent = tracks.length ? `${pos + 1} / ${tracks.length}` : "—";
  renderArtwork();
}

function makeQueueThumb(track) {
  const node = document.createElement("span");
  node.className = "queue-art";
  const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  icon.setAttribute("class", "icon");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", "#i-disc");
  icon.appendChild(use);
  node.appendChild(icon);
  const src = coverURL(model.identity.server_url, track.cover_hash, 96);
  if (src) {
    const image = document.createElement("img");
    image.alt = "";
    image.loading = "lazy";
    image.referrerPolicy = "no-referrer";
    image.onerror = () => { image.remove(); };
    image.src = src;
    node.appendChild(image);
  }
  return node;
}

function renderQueue() {
  const q = model.queue;
  const tracks = q && Array.isArray(q.tracks) ? q.tracks : [];
  const pos = q ? Math.max(0, Number(q.position) || 0) : 0;
  const signature = JSON.stringify([q && q.updated_at, pos, q && q.mode, tracks.map(t => [t.track_id, t.title, t.cover_hash, t.missing])]);
  const list = $("queue-list");
  if (signature !== model.queueSignature) {
    model.queueSignature = signature;
    list.replaceChildren();
    tracks.forEach((track, index) => {
      const row = document.createElement("button");
      row.type = "button";
      row.className = `queue-row${index === pos ? " current" : ""}`;
      row.disabled = !!track.missing || !model.playback.connected || model.controlBusy;
      row.setAttribute("aria-label", `播放第 ${index + 1} 首：${track.title || track.track_id}`);
      row.appendChild(makeQueueThumb(track));
      const number = document.createElement("span");
      number.className = "queue-index";
      number.textContent = String(index + 1);
      row.appendChild(number);
      const info = document.createElement("span");
      info.className = "queue-info";
      const title = document.createElement("span");
      title.className = "queue-title";
      title.textContent = track.title || track.track_id;
      const sub = document.createElement("span");
      sub.className = "queue-sub";
      sub.textContent = [track.artist, track.album].filter(Boolean).join(" · ") || (track.missing ? "曲目已丢失" : "—");
      info.append(title, sub);
      row.appendChild(info);
      const duration = document.createElement("span");
      duration.className = "queue-duration";
      duration.textContent = track.duration_ms ? formatTime(track.duration_ms) : "—";
      row.appendChild(duration);
      row.addEventListener("click", () => jumpTo(index));
      list.appendChild(row);
    });
  } else {
    Array.from(list.children).forEach((row, index) => { row.disabled = !!tracks[index].missing || !model.playback.connected || model.controlBusy; });
  }

  $("queue-count").textContent = q ? String(tracks.length) : "—";
  const empty = $("queue-empty");
  empty.hidden = tracks.length > 0;
  if (!tracks.length) {
    empty.querySelector("strong").textContent = model.queueError ? "无法读取播放列表" : "播放列表为空";
    empty.querySelector("span").textContent = model.queueError || "在服务端控制台选择音乐后，这里会同步显示";
  }
  document.querySelector(".queue-footer").textContent = tracks.length ? `${queueModeLabel(q.mode)} · 播放列表保存在服务端 · 点击曲目可直接播放` : "播放列表保存在服务端";
  if (q && pos !== model.lastPosition) {
    model.lastPosition = pos;
    const current = list.children[pos];
    if (current) centerInList(list, current);
  }
}

async function loadState() {
  if (model.stateBusy || document.hidden) return;
  model.stateBusy = true;
  try {
    const state = await api("/api/state");
    model.identity = state.identity || {};
    model.playback = state.playback || {};
    const trackID = model.playback.track_id || "";
    if (trackID !== model.lyricsTrackID) {
      model.lyricsTrackID = trackID;
      model.lyrics = null;
      model.lyricsSignature = "";
      model.lyricsError = "";
      model.lyricIndex = -1;
      model.paneManual = false;
      model.paneAutoPending = true;
      if (trackID) loadLyrics(trackID);
      else { renderLyrics(); autoSelectPane(); }
    }
    renderPlayback();
    renderQueue();
  } catch (error) {
    renderConnection();
  } finally {
    model.stateBusy = false;
  }
}

async function loadQueue() {
  if (model.queueBusy || document.hidden) return;
  model.queueBusy = true;
  try {
    model.queue = await api("/api/queue");
    model.queueError = "";
  } catch (error) {
    model.queueError = error.message || "服务端暂时不可用";
  } finally {
    model.queueBusy = false;
    renderPlayback();
    renderQueue();
  }
}

async function control(path, message, body = {}) {
  if (model.controlBusy) return;
  model.controlBusy = true;
  renderPlayback();
  renderQueue();
  try {
    await api(path, "POST", body);
    if (message) toast(message);
    await Promise.all([loadState(), loadQueue()]);
  } catch (error) {
    toast(error.message || "操作失败", "error");
  } finally {
    model.controlBusy = false;
    renderPlayback();
    renderQueue();
  }
}

async function jumpTo(index) {
  await control("/api/jump", `开始播放第 ${index + 1} 首`, { index });
}

$("play-pause").addEventListener("click", () => {
  const playing = model.playback.state === "playing";
  control("/api/pause", playing ? "已暂停" : "继续播放");
});
$("prev").addEventListener("click", () => control("/api/prev", "已切换到上一首"));
$("next").addEventListener("click", () => control("/api/next", "已切换到下一首"));
$("stop").addEventListener("click", () => control("/api/stop", "播放已停止"));

$("seek").addEventListener("input", () => {
  model.seeking = true;
  $("elapsed").textContent = formatTime(Number($("seek").value));
});
$("seek").addEventListener("change", async () => {
  const rate = Number(model.playback.rate) || 0;
  const pts = rate ? Math.round(Number($("seek").value) * rate / 1000) : 0;
  model.seeking = false;
  try {
    await api("/api/seek", "POST", { pts });
    await loadState();
  } catch (error) {
    toast(error.message || "定位失败", "error");
    await loadState();
  }
});

$("volume").addEventListener("input", () => {
  model.volumeBusy = true;
  $("volume-value").textContent = `${$("volume").value}%`;
});
$("volume").addEventListener("change", async () => {
  const value = Number($("volume").value);
  try {
    await api("/api/volume", "POST", { volume: value });
  } catch (error) {
    toast(error.message || "音量设置失败", "error");
  } finally {
    model.volumeBusy = false;
    await loadState();
  }
});

function setPane(name) {
  const lyrics = name !== "queue";
  $("lyrics-pane").hidden = !lyrics;
  $("queue-pane").hidden = lyrics;
  $("tab-lyrics").classList.toggle("active", lyrics);
  $("tab-queue").classList.toggle("active", !lyrics);
  $("tab-lyrics").setAttribute("aria-selected", String(lyrics));
  $("tab-queue").setAttribute("aria-selected", String(!lyrics));
  try { localStorage.setItem("zat-pane", lyrics ? "lyrics" : "queue"); } catch (_) {}
  if (lyrics) updateLyricHighlight(true);
}

// hasLyrics reports whether the current track has displayable lyric lines.
function hasLyrics() {
  const l = model.lyrics;
  return !!(l && l.source !== "none" && Array.isArray(l.lines) && l.lines.length);
}

// autoSelectPane shows lyrics when the track has them, otherwise the playlist.
// It runs on track change / lyrics arrival and yields to a manual tab pick until
// the next track change.
function autoSelectPane() {
  if (model.paneManual || !model.paneAutoPending) return;
  model.paneAutoPending = false;
  setPane(hasLyrics() ? "lyrics" : "queue");
}

$("tab-lyrics").addEventListener("click", () => { model.paneManual = true; setPane("lyrics"); });
$("tab-queue").addEventListener("click", () => { model.paneManual = true; setPane("queue"); });
let savedPane = "lyrics";
try { savedPane = localStorage.getItem("zat-pane") === "queue" ? "queue" : "lyrics"; } catch (_) {}
setPane(savedPane);

// Draggable divider between the player and the side pane (landscape). The split
// ratio is persisted so the preferred width survives reloads.
function applySideRatio(r) {
  model.sideRatio = Math.max(0.3, Math.min(0.75, r));
  const con = document.querySelector(".console");
  const side = model.sideRatio;
  con.style.gridTemplateColumns = `minmax(0, ${(1 - side).toFixed(3)}fr) 14px minmax(0, ${side.toFixed(3)}fr)`;
}
try {
  const saved = parseFloat(localStorage.getItem("zat-side-ratio"));
  if (saved >= 0.3 && saved <= 0.75) model.sideRatio = saved;
} catch (_) {}
applySideRatio(model.sideRatio);
(function initResizer() {
  const handle = $("pane-resizer");
  const consoleEl = document.querySelector(".console");
  if (!handle || !consoleEl) return;
  let dragging = false;
  const move = (clientX) => {
    const rect = consoleEl.getBoundingClientRect();
    if (rect.width <= 0) return;
    applySideRatio(1 - (clientX - rect.left) / rect.width);
  };
  handle.addEventListener("pointerdown", (e) => {
    dragging = true;
    handle.setPointerCapture(e.pointerId);
    e.preventDefault();
  });
  handle.addEventListener("pointermove", (e) => { if (dragging) move(e.clientX); });
  const end = (e) => {
    if (!dragging) return;
    dragging = false;
    try { localStorage.setItem("zat-side-ratio", model.sideRatio.toFixed(3)); } catch (_) {}
  };
  handle.addEventListener("pointerup", end);
  handle.addEventListener("pointercancel", end);
})();

renderPlayback();
renderQueue();
renderLyrics();
Promise.all([loadState(), loadQueue()]);
setInterval(loadState, 1000);
setInterval(loadQueue, 3000);
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) {
    loadState();
    loadQueue();
    scheduleLyrics();
  }
});
