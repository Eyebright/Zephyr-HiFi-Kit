"use strict";

ICONS.heart = '<path d="M20.8 4.6a5.5 5.5 0 00-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 00-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 000-7.8z"/>';
const FV = {
  kind: "track", previewKind: "track", preview: null, previewError: "", previewSeq: 0,
  page: null, revision: 0, pending: new Set(), current: "", state: null, stateAt: 0, stateSeq: 0, stateBusy: false, stateError: ""
};
function favoriteIdentity(item) {
  return item.kind === "track" ? { kind: "track", track_id: item.track_id } : { kind: "album", artist: item.artist || "", album: item.album || "" };
}
function favoriteKey(item) { return JSON.stringify(favoriteIdentity(item)); }
function favoritePlayerShell() {
  return `<div class="fav-player"><button class="btn ghost fav-heart" id="pb-favorite" data-act="favorite-current" aria-label="喜欢当前曲目" aria-pressed="false" title="开始播放曲库曲目后可收藏" disabled>${icon("heart", 19)}</button><details id="pb-favorite-menu"><summary aria-label="专辑收藏选项" title="专辑收藏选项">⌄</summary><div><button class="btn ghost sm" id="pb-favorite-album" data-act="favorite-current-album" disabled>收藏当前专辑</button><button class="btn ghost sm" data-act="favorites-open">查看我喜欢的音乐</button></div></details></div>`;
}
function paintPlayerFavorite(trackID) {
  if (trackID !== FV.current) {
    FV.current = trackID; FV.state = null; FV.stateAt = 0; FV.stateBusy = false; FV.stateError = ""; FV.stateSeq++;
    const menu = $("#pb-favorite-menu"); if (menu) menu.open = false;
  }
  const button = $("#pb-favorite"), album = $("#pb-favorite-album");
  if (!button) return;
  const state = FV.state, ready = !!(trackID && state);
  const trackBusy = FV.pending.has(favoriteKey({ kind: "track", track_id: trackID }));
  button.disabled = !ready || trackBusy;
  button.dataset.id = trackID; button.dataset.saved = String(!!state?.track);
  button.setAttribute("aria-pressed", String(!!state?.track));
  button.classList.toggle("is-favorite", !!state?.track);
  const label = state?.track ? "取消喜欢当前曲目" : "喜欢当前曲目";
  button.setAttribute("aria-label", label);
  button.title = trackBusy ? "正在保存…" : !trackID ? "开始播放曲库曲目后可收藏" : FV.stateError || (!state ? "正在读取收藏状态…" : label);
  if (album) {
    const identity = { kind: "album", artist: state?.artist || "", album: state?.album_name || "" };
    album.disabled = !ready || FV.pending.has(favoriteKey(identity));
    album.dataset.id = trackID; album.dataset.artist = identity.artist; album.dataset.album = identity.album;
    album.dataset.saved = String(!!state?.album);
    album.textContent = state?.album ? "取消收藏当前专辑" : "收藏当前专辑";
  }
  if (trackID && !FV.stateBusy && Date.now() - FV.stateAt > 10000) refreshPlayerFavorite(trackID);
}
async function refreshPlayerFavorite(trackID) {
  const seq = ++FV.stateSeq, revision = FV.revision;
  FV.stateBusy = true; FV.stateAt = Date.now();
  try {
    const state = await api("GET", "/favorites/state?track_id=" + enc(trackID));
    if (seq !== FV.stateSeq || trackID !== FV.current || revision !== FV.revision) return;
    if (!state || typeof state.track !== "boolean" || typeof state.album !== "boolean" || typeof state.artist !== "string" || typeof state.album_name !== "string") throw new Error("收藏状态不完整");
    FV.state = state; FV.stateError = "";
  } catch (e) {
    if (seq !== FV.stateSeq || trackID !== FV.current || revision !== FV.revision) return;
    FV.state = null; FV.stateError = e.status === 404 ? "当前曲目不在曲库中，不能收藏" : "收藏状态读取失败，稍后自动重试";
  } finally {
    if (seq === FV.stateSeq) { FV.stateBusy = false; paintPlayerFavorite(FV.current); }
  }
}
async function changeFavorite(identity, saved) {
  const key = favoriteKey(identity);
  if (FV.pending.has(key)) return;
  FV.pending.add(key); paintPlayerFavorite(FV.current);
  try {
    const result = await api(saved ? "PUT" : "DELETE", "/favorites", identity);
    if (result?.saved !== saved) throw new Error("未确认收藏已保存，请刷新后重试");
    FV.revision++; FV.stateSeq++; FV.stateBusy = false; FV.stateAt = 0; FV.state = null;
    toast(saved ? (identity.kind === "track" ? "已加入我喜欢的曲目" : "已加入我喜欢的专辑") : "已取消收藏", "ok");
    if (S.view === "overview") refreshFavoritePreview();
    if (S.view === "library" && S.libTab === "favorites") refreshLibraryBrowser();
  } catch (e) { toast(e.message || "收藏未保存，请重试", "err"); }
  finally { FV.pending.delete(key); paintPlayerFavorite(FV.current); }
}
function favoriteAlbumButton(artist, album) {
  return `<button class="btn ghost sm" data-act="favorite-album" data-artist="${esc(artist || "")}" data-album="${esc(album || "")}">${icon("heart", 14)} 收藏专辑</button>`;
}
function favoriteGreeting() {
  const name = typeof XP !== "undefined" ? XP.profile?.display_name || "" : "";
  const hour = new Date().getHours(), hello = hour < 6 ? "夜深了" : hour < 12 ? "早上好" : hour < 18 ? "下午好" : "晚上好";
  const greeting = $("#ov-greeting"); if (greeting) greeting.textContent = name ? `${hello}，${name}` : "主页";
  return name ? `${name} 喜欢的音乐` : "我喜欢的音乐";
}
function favoriteTabs(kind, action) {
  return `<div class="chips" aria-label="收藏类型">${[["track", "曲目"], ["album", "专辑"]].map(([id, name]) => `<button class="chip ${id === kind ? "active" : ""}" data-act="${action}" data-kind="${id}" aria-pressed="${id === kind}">${name}</button>`).join("")}</div>`;
}
function paintFavoritePreview() {
  const title = favoriteGreeting(), root = $("#ov-favorites"); if (!root) return;
  const page = FV.preview;
  root.innerHTML = secHead(icon("heart", 17) + " " + esc(title), `<button class="btn ghost xs" data-act="favorites-open" data-kind="${FV.previewKind}">查看全部 ${icon("chevR", 12)}</button>`) +
    `<div class="fav-heading">${favoriteTabs(FV.previewKind, "favorite-preview-kind")}<span class="hint">收藏与播放终端无关</span></div>` +
    (FV.previewError ? `<div class="fav-note" role="status">${esc(FV.previewError)} <button class="btn ghost xs" data-act="favorite-preview-refresh">重试</button></div>` : !page ? empty("heart", "正在读取喜欢的音乐…", "", true) :
      page.items.length ? `<div class="fav-preview">${page.items.map((item, index) => favoriteRow(item, index, "preview")).join("")}</div>` : empty("heart", FV.previewKind === "track" ? "播放时点亮爱心，把喜欢的曲目留在这里。" : "在专辑页面或播放栏菜单中收藏整张专辑。", "", true));
}
async function refreshFavoritePreview() {
  const root = $("#ov-favorites"); if (!root || S.view !== "overview") return;
  const seq = ++FV.previewSeq, kind = FV.previewKind, revision = FV.revision;
  try {
    const page = await api("GET", `/favorites?kind=${kind}&page=1&page_size=6`);
    if (seq !== FV.previewSeq || root !== $("#ov-favorites") || S.view !== "overview" || kind !== FV.previewKind || revision !== FV.revision) return;
    if (!Array.isArray(page?.items)) throw new Error("收藏响应不完整");
    FV.preview = page; FV.previewError = "";
  } catch (e) {
    if (seq !== FV.previewSeq || root !== $("#ov-favorites") || S.view !== "overview" || kind !== FV.previewKind || revision !== FV.revision) return;
    FV.previewError = "喜欢的音乐暂时无法读取，不影响其他播放功能。";
  }
  paintFavoritePreview();
}
function favoriteRow(item, index, scope) {
  const attrs = `data-index="${index}" data-scope="${scope}"`, title = item.kind === "track" ? item.title || "未知曲目" : item.album || "未知专辑";
  const detail = item.kind === "album" ? `${item.artist || "未知艺术家"} · ${item.track_count} 首` : `${item.artist || "未知艺术家"} · ${item.album || "未知专辑"}`;
  return `<article class="fav-row">${coverArt(item.album, item.artist, "thumb", 20, item.cover_hash)}<div class="fav-info">${item.kind === "album" && !item.missing ? `<button class="fav-name" data-act="favorite-album-open" ${attrs}>${esc(title)}</button>` : `<strong>${esc(title)}</strong>`}<span>${esc(detail)}</span>${item.missing ? '<span class="fav-missing">文件不可用 · 收藏仍保留</span>' : !item.decodable ? '<span class="hint">当前无可解码曲目</span>' : ""}</div><div class="fav-actions"><button class="btn ghost xs" data-act="favorite-play" ${attrs} ${item.missing || !item.decodable ? "disabled" : ""} aria-label="播放或排队 ${esc(title)}">${icon("play", 14)}</button><button class="btn ghost xs fav-heart is-favorite" data-act="favorite-remove" ${attrs} aria-label="取消收藏 ${esc(title)}" aria-pressed="true">${icon("heart", 15)}</button></div></article>`;
}
async function refreshFavoriteLibrary(seq) {
  const root = $("#lb-body"), q = { ...lbFrame().query }, kind = FV.kind;
  try {
    const page = await api("GET", "/favorites?" + new URLSearchParams({ kind, page: q.page, page_size: q.page_size, q: q.q }));
    if (seq !== LB.request || root !== $("#lb-body") || S.view !== "library" || S.libTab !== "favorites" || kind !== FV.kind) return;
    if (!Array.isArray(page?.items)) throw new Error("收藏响应不完整");
    const last = Math.max(1, Math.ceil(page.total / page.page_size));
    if (q.page > last) { lbFrame().query.page = last; return refreshLibraryBrowser(); }
    FV.page = page; LB.busy = false;
    paintLibraryBrowserHeader(); paintFavoriteLibrary();
  } catch (e) {
    if (seq !== LB.request || root !== $("#lb-body") || S.view !== "library" || S.libTab !== "favorites") return;
    LB.busy = false; FV.page = null;
    root.innerHTML = empty("alert", "收藏读取失败：" + e.message, '<button class="btn ghost" data-act="lib-refresh">重试</button>', true);
  }
}
function paintFavoriteLibrary() {
  const root = $("#lb-body"), p = FV.page; if (!root || !p) return;
  root.innerHTML = `<div class="fav-heading">${favoriteTabs(FV.kind, "favorite-library-kind")}<label class="fav-query">搜索收藏<input class="inp" type="search" id="fav-search" value="${esc(lbFrame().query.q)}" placeholder="曲目、专辑或艺术家"></label><button class="btn sm" data-act="favorite-search">搜索</button><span class="hint">${p.total} 项收藏</span></div>` +
    (p.items.length ? `<div class="fav-list">${p.items.map((item, index) => favoriteRow(item, index, "library")).join("")}</div>` : empty("heart", lbFrame().query.q ? "没有匹配的收藏" : "还没有收藏，播放时点亮爱心或收藏整张专辑。", "", true));
  const pages = Math.max(1, Math.ceil(p.total / p.page_size));
  $("#lb-pages").innerHTML = `<div class="lb-pagination"><span>第 ${p.page} / ${pages} 页 · ${p.total} 项</span><button class="btn sm" data-act="lb-page" data-page="${p.page - 1}" ${p.page <= 1 ? "disabled" : ""}>上一页</button><button class="btn sm" data-act="lb-page" data-page="${p.page + 1}" ${p.page >= pages ? "disabled" : ""}>下一页</button><label>每页 <select class="inp" data-change="lb-size" aria-label="每页数量">${lbOptions([[24, "24"], [48, "48"], [96, "96"]], p.page_size)}</select></label></div>`;
}
function favoriteRecord(ds) {
  if (ds.scope === "library" && (S.view !== "library" || S.libTab !== "favorites" || LB.busy)) return null;
  if (ds.scope === "preview" && (S.view !== "overview" || FV.previewError)) return null;
  return (ds.scope === "preview" ? FV.preview : FV.page)?.items[Number(ds.index)] || null;
}
async function playFavorite(item) {
  if (!item || item.missing || !item.decodable) return;
  const root = $("#view"), view = S.view, tab = S.libTab, epoch = LB.request, overviewEpoch = OV.epoch;
  try {
    let track, ids;
    if (item.kind === "track") { track = await api("GET", "/tracks/" + enc(item.track_id)); ids = track.decodable ? [track.id] : []; }
    else {
      const result = await api("GET", "/library/select?" + new URLSearchParams({ category: "tracks", artist_mode: "album", artist: item.artist || "", album: item.album || "" }));
      ids = result.track_ids;
    }
    if (root !== $("#view") || view !== S.view || tab !== S.libTab || epoch !== LB.request || overviewEpoch !== OV.epoch) return;
    if (!ids?.length) { toast("收藏仍保留，但当前没有可播放文件", "warn"); return; }
    await openPushPicker({ trackIDs: ids, track, label: item.kind === "track" ? item.title : item.album || "未知专辑" });
  } catch (e) { toast(e.message || "无法读取收藏曲目", "err"); }
}
Object.assign(ACT, {
  "favorite-current": ds => ds.id ? changeFavorite({ kind: "track", track_id: ds.id }, ds.saved !== "true") : undefined,
  "favorite-current-album": ds => {
    if (!ds.id) return;
    const menu = $("#pb-favorite-menu"); if (menu) menu.open = false;
    return changeFavorite({ kind: "album", artist: ds.artist || "", album: ds.album || "" }, ds.saved !== "true");
  },
  "favorite-album": ds => changeFavorite({ kind: "album", artist: ds.artist || "", album: ds.album || "" }, true),
  "favorite-remove": ds => { const item = favoriteRecord(ds); if (item) return changeFavorite(favoriteIdentity(item), false); },
  "favorite-play": ds => playFavorite(favoriteRecord(ds)),
  "favorite-album-open": ds => {
    const item = favoriteRecord(ds); if (!item || item.missing) return;
    S.libTab = "albums";
    const state = lbState(); state.frames.splice(1);
    state.frames.push({ label: item.album || "未知专辑", query: { ...state.frames[0].query, category: "tracks", artist_mode: "album", artist: item.artist || "", album: item.album || "", page: 1, q: "" } });
    setView("library");
  },
  "favorites-open": ds => { FV.kind = ds.kind === "album" ? "album" : "track"; S.libTab = "favorites"; lbFrame().query.page = 1; setView("library"); },
  "favorite-preview-kind": ds => { if (!["track", "album"].includes(ds.kind)) return; FV.previewKind = ds.kind; FV.preview = null; FV.previewError = ""; paintFavoritePreview(); return refreshFavoritePreview(); },
  "favorite-preview-refresh": refreshFavoritePreview,
  "favorite-library-kind": ds => { if (!["track", "album"].includes(ds.kind)) return; FV.kind = ds.kind; lbFrame().query.page = 1; return refreshLibraryBrowser(); },
  "favorite-search": () => { lbFrame().query.q = $("#fav-search").value; lbFrame().query.page = 1; return refreshLibraryBrowser(); }
});
document.addEventListener("profile-updated", paintFavoritePreview);
document.addEventListener("keydown", e => { if (e.target.id === "fav-search" && e.key === "Enter") { e.preventDefault(); ACT["favorite-search"](); } });
