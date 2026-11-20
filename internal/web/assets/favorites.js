"use strict";

const fvT = (key, params) => I18n.t("features." + key, params);
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
  return `<div class="fav-player"><button class="btn ghost fav-heart" id="pb-favorite" data-act="favorite-current" aria-label="${esc(fvT("favLikeCurrent"))}" aria-pressed="false" title="${esc(fvT("favStartPlayback"))}" disabled>${icon("heart", 19)}</button><details id="pb-favorite-menu"><summary aria-label="${esc(fvT("favAlbumOptions"))}" title="${esc(fvT("favAlbumOptions"))}">⌄</summary><div><button class="btn ghost sm" id="pb-favorite-album" data-act="favorite-current-album" disabled>${esc(fvT("favSaveAlbumCurrent"))}</button><button class="btn ghost sm" data-act="favorites-open">${esc(fvT("favView"))}</button></div></details></div>`;
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
  const label = fvT(state?.track ? "favUnlikeCurrent" : "favLikeCurrent");
  button.setAttribute("aria-label", label);
  button.title = trackBusy ? fvT("favSaving") : !trackID ? fvT("favStartPlayback") : FV.stateError ? fvT(FV.stateError) : !state ? fvT("favReadingState") : label;
  if (album) {
    const identity = { kind: "album", artist: state?.artist || "", album: state?.album_name || "" };
    album.disabled = !ready || FV.pending.has(favoriteKey(identity));
    album.dataset.id = trackID; album.dataset.artist = identity.artist; album.dataset.album = identity.album;
    album.dataset.saved = String(!!state?.album);
    album.textContent = fvT(state?.album ? "favRemoveAlbumCurrent" : "favSaveAlbumCurrent");
  }
  const summary = $("#pb-favorite-menu summary"), view = $("#pb-favorite-menu [data-act=favorites-open]");
  if (summary) { summary.setAttribute("aria-label", fvT("favAlbumOptions")); summary.title = fvT("favAlbumOptions"); }
  if (view) view.textContent = fvT("favView");
  if (trackID && !FV.stateBusy && Date.now() - FV.stateAt > 10000) refreshPlayerFavorite(trackID);
}
async function refreshPlayerFavorite(trackID) {
  const seq = ++FV.stateSeq, revision = FV.revision;
  FV.stateBusy = true; FV.stateAt = Date.now();
  try {
    const state = await api("GET", "/favorites/state?track_id=" + enc(trackID));
    if (seq !== FV.stateSeq || trackID !== FV.current || revision !== FV.revision) return;
    if (!state || typeof state.track !== "boolean" || typeof state.album !== "boolean" || typeof state.artist !== "string" || typeof state.album_name !== "string") throw new Error(fvT("favIncompleteState"));
    FV.state = state; FV.stateError = "";
  } catch (e) {
    if (seq !== FV.stateSeq || trackID !== FV.current || revision !== FV.revision) return;
    FV.state = null; FV.stateError = e.status === 404 ? "favNotInLibrary" : "favStateFailed";
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
    if (result?.saved !== saved) throw new Error(fvT("favUnconfirmed"));
    FV.revision++; FV.stateSeq++; FV.stateBusy = false; FV.stateAt = 0; FV.state = null;
    toast(fvT(saved ? (identity.kind === "track" ? "favTrackSaved" : "favAlbumSaved") : "favRemoved"), "ok");
    if (S.view === "overview") refreshFavoritePreview();
    if (S.view === "library" && S.libTab === "favorites") refreshLibraryBrowser();
  } catch (e) { toast(e.message || fvT("favSaveFailed"), "err"); }
  finally { FV.pending.delete(key); paintPlayerFavorite(FV.current); }
}
function favoriteAlbumButton(artist, album) {
  return `<button class="btn ghost sm" data-act="favorite-album" data-artist="${esc(artist || "")}" data-album="${esc(album || "")}">${icon("heart", 14)} ${esc(fvT("favSaveAlbum"))}</button>`;
}
function favoriteGreeting() {
  const name = typeof XP !== "undefined" ? XP.profile?.display_name || "" : "";
  const hour = new Date().getHours(), hello = fvT(hour < 6 ? "favLateNight" : hour < 12 ? "favMorning" : hour < 18 ? "favAfternoon" : "favEvening");
  const greeting = $("#ov-greeting"); if (greeting) greeting.textContent = name ? fvT("favGreeting", { greeting: hello, name }) : fvT("favHome");
  return name ? fvT("favNamedMusic", { name }) : fvT("favMusic");
}
function favoriteTabs(kind, action) {
  return `<div class="chips" aria-label="${esc(fvT("favTypes"))}">${[["track", fvT("tracks")], ["album", fvT("albums")]].map(([id, name]) => `<button class="chip ${id === kind ? "active" : ""}" data-act="${action}" data-kind="${id}" aria-pressed="${id === kind}">${esc(name)}</button>`).join("")}</div>`;
}
function paintFavoritePreview() {
  const title = favoriteGreeting(), root = $("#ov-favorites"); if (!root) return;
  const page = FV.preview;
  root.innerHTML = secHead(icon("heart", 17) + " " + esc(title), `<button class="btn ghost xs" data-act="favorites-open" data-kind="${FV.previewKind}">${esc(fvT("favViewAll"))} ${icon("chevR", 12)}</button>`) +
    `<div class="fav-heading">${favoriteTabs(FV.previewKind, "favorite-preview-kind")}<span class="hint">${esc(fvT("favIndependent"))}</span></div>` +
    (FV.previewError ? `<div class="fav-note" role="status">${esc(fvT(FV.previewError))} <button class="btn ghost xs" data-act="favorite-preview-refresh">${esc(fvT("retry"))}</button></div>` : !page ? empty("heart", fvT("favLoading"), "", true) :
      page.items.length ? `<div class="fav-preview">${page.items.map((item, index) => favoriteRow(item, index, "preview")).join("")}</div>` : empty("heart", fvT(FV.previewKind === "track" ? "favEmptyTracks" : "favEmptyAlbums"), "", true));
}
async function refreshFavoritePreview() {
  const root = $("#ov-favorites"); if (!root || S.view !== "overview") return;
  const seq = ++FV.previewSeq, kind = FV.previewKind, revision = FV.revision;
  try {
    const page = await api("GET", `/favorites?kind=${kind}&page=1&page_size=6`);
    if (seq !== FV.previewSeq || root !== $("#ov-favorites") || S.view !== "overview" || kind !== FV.previewKind || revision !== FV.revision) return;
    if (!Array.isArray(page?.items)) throw new Error(fvT("favIncompleteResponse"));
    FV.preview = page; FV.previewError = "";
  } catch (e) {
    if (seq !== FV.previewSeq || root !== $("#ov-favorites") || S.view !== "overview" || kind !== FV.previewKind || revision !== FV.revision) return;
    FV.previewError = "favPreviewFailed";
  }
  paintFavoritePreview();
}
function favoriteRow(item, index, scope) {
  const attrs = `data-index="${index}" data-scope="${scope}"`, title = item.kind === "track" ? item.title || fvT("unknownTrack") : item.album || fvT("unknownAlbum");
  const detail = item.kind === "album" ? fvT("favAlbumDetail", { artist: item.artist || fvT("unknownArtist"), count: I18n.number(item.track_count) }) : fvT("favTrackDetail", { artist: item.artist || fvT("unknownArtist"), album: item.album || fvT("unknownAlbum") });
  return `<article class="fav-row">${coverArt(item.album, item.artist, "thumb", 20, item.cover_hash)}<div class="fav-info">${item.kind === "album" && !item.missing ? `<button class="fav-name" data-act="favorite-album-open" ${attrs}>${esc(title)}</button>` : `<strong>${esc(title)}</strong>`}<span>${esc(detail)}</span>${item.missing ? `<span class="fav-missing">${esc(fvT("favMissing"))}</span>` : !item.decodable ? `<span class="hint">${esc(fvT("favNoDecodable"))}</span>` : ""}</div><div class="fav-actions"><button class="btn ghost xs" data-act="favorite-play" ${attrs} ${item.missing || !item.decodable ? "disabled" : ""} aria-label="${esc(fvT("favPlayLabel", { title }))}">${icon("play", 14)}</button><button class="btn ghost xs fav-heart is-favorite" data-act="favorite-remove" ${attrs} aria-label="${esc(fvT("favRemoveLabel", { title }))}" aria-pressed="true">${icon("heart", 15)}</button></div></article>`;
}
async function refreshFavoriteLibrary(seq) {
  const root = $("#lb-body"), q = { ...lbFrame().query }, kind = FV.kind;
  try {
    const page = await api("GET", "/favorites?" + new URLSearchParams({ kind, page: q.page, page_size: q.page_size, q: q.q }));
    if (seq !== LB.request || root !== $("#lb-body") || S.view !== "library" || S.libTab !== "favorites" || kind !== FV.kind) return;
    if (!Array.isArray(page?.items)) throw new Error(fvT("favIncompleteResponse"));
    const last = Math.max(1, Math.ceil(page.total / page.page_size));
    if (q.page > last) { lbFrame().query.page = last; return refreshLibraryBrowser(); }
    FV.page = page; LB.busy = false;
    paintLibraryBrowserHeader(); paintFavoriteLibrary();
  } catch (e) {
    if (seq !== LB.request || root !== $("#lb-body") || S.view !== "library" || S.libTab !== "favorites") return;
    LB.busy = false; FV.page = null;
    root.innerHTML = empty("alert", fvT("favLoadFailed", { message: e.message }), `<button class="btn ghost" data-act="lib-refresh">${esc(fvT("retry"))}</button>`, true);
  }
}
function paintFavoriteLibrary() {
  const root = $("#lb-body"), p = FV.page; if (!root || !p) return;
  root.innerHTML = `<div class="fav-heading">${favoriteTabs(FV.kind, "favorite-library-kind")}<label class="fav-query">${esc(fvT("favSearch"))}<input class="inp" type="search" id="fav-search" value="${esc(lbFrame().query.q)}" placeholder="${esc(fvT("favSearchPlaceholder"))}"></label><button class="btn sm" data-act="favorite-search">${esc(fvT("search"))}</button><span class="hint">${esc(fvT("favCount", { count: I18n.number(p.total) }))}</span></div>` +
    (p.items.length ? `<div class="fav-list">${p.items.map((item, index) => favoriteRow(item, index, "library")).join("")}</div>` : empty("heart", fvT(lbFrame().query.q ? "favNoMatches" : "favEmpty"), "", true));
  const pages = Math.max(1, Math.ceil(p.total / p.page_size));
  $("#lb-pages").innerHTML = `<div class="lb-pagination"><span>${esc(fvT("pageItems", { page: I18n.number(p.page), pages: I18n.number(pages), count: I18n.number(p.total) }))}</span><button class="btn sm" data-act="lb-page" data-page="${p.page - 1}" ${p.page <= 1 ? "disabled" : ""}>${esc(fvT("previousPage"))}</button><button class="btn sm" data-act="lb-page" data-page="${p.page + 1}" ${p.page >= pages ? "disabled" : ""}>${esc(fvT("nextPage"))}</button><label>${esc(fvT("perPage"))} <select class="inp" data-change="lb-size" aria-label="${esc(fvT("pageSize"))}">${lbOptions([24, 48, 96].map(n => [n, I18n.number(n)]), p.page_size)}</select></label></div>`;
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
    if (!ids?.length) { toast(fvT("favNoPlayable"), "warn"); return; }
    await openPushPicker({ trackIDs: ids, track, label: item.kind === "track" ? item.title : item.album || fvT("unknownAlbum") });
  } catch (e) { toast(e.message || fvT("favReadTrackFailed"), "err"); }
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
    state.frames.push({ get label() { return item.album || fvT("unknownAlbum"); }, query: { ...state.frames[0].query, category: "tracks", artist_mode: "album", artist: item.artist || "", album: item.album || "", page: 1, q: "" } });
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
