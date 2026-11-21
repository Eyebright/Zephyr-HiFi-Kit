"use strict";

const lbT = (key, params) => I18n.t("features." + key, params);
const LIB_CATEGORIES = [["artists", "artists"], ["albums", "albums"], ["tracks", "tracks"], ["genres", "genres"], ["years", "years"], ["composers", "composers"], ["folders", "libFolders"]].map(([id, key]) => Object.defineProperty([id], 1, { get: () => lbT(key), enumerable: true }));
const LB = { states: {}, page: null, error: null, busy: false, request: 0, searchTimer: null, sel: new Set(),
  artist: null, artistMode: "", artistAt: 0, artistPage: 1, artistLoading: false, artistError: null,
  restoreAnchor: null, trackView: (() => { try { return localStorage.getItem("lbTrackView") === "list" ? "list" : "tiles"; } catch (e) { return "tiles"; } })() };
const LB_PAGE_SIZES = { artists: 27, albums: 28, tracks: 24 };
function lbPageSize(category) { return LB_PAGE_SIZES[category] || 24; }
const LB_ARTIST_TTL = 5 * 60 * 1000;
function lbState() {
  const category = S.libTab || "albums";
  if (!LB.states[category]) LB.states[category] = { frames: [{ get label() { return String((LIB_CATEGORIES.find(c => c[0] === category) || [category, category])[1]); }, query: { category, page: 1, page_size: 24, field: "all", sort: "name", direction: "asc", artist_mode: "album", q: "" } }] };
  return LB.states[category];
}
function lbFrame() { return lbState().frames.at(-1); }
function shellLibraryBrowser() {
  return `<section class="card lb-browser"><div id="lb-header"></div><div id="lb-body"></div><div id="lb-pages"></div></section>`;
}
function lbOptions(options, value) {
  return options.map(([id, label]) => `<option value="${esc(id)}" ${String(value) === String(id) ? "selected" : ""}>${esc(label)}</option>`).join("");
}
function paintLibraryBrowserHeader() {
  const el = $("#lb-header"); if (!el) return;
  const frame = lbFrame(), q = frame.query, auxiliary = S.libTab === "playlists" || S.libTab === "favorites";
  const fields = {
    artists: [["all", lbT("libNameAlbums")], ["artist", lbT("libArtistName")]],
    albums: [["all", lbT("libAlbumArtist")], ["album", lbT("libAlbumName")], ["artist", lbT("artists")]],
    tracks: [["all", lbT("libAllMetadata")], ["title", lbT("libTrackName")], ["artist", lbT("artists")], ["album", lbT("albums")], ["genre", lbT("genres")], ["composer", lbT("composers")], ["year", lbT("years")], ["path", lbT("libFilePath")]],
    genres: [["genre", lbT("libGenreName")]], years: [["year", lbT("libDecadeYear")]], composers: [["composer", lbT("libComposerName")]], folders: [["path", lbT("libFolderPath")], ["title", lbT("libTrackName")]]
  };
  const categoryFields = fields[q.category] || [];
  if (!auxiliary && !categoryFields.some(([key]) => key === q.field)) q.field = categoryFields[0][0];
  el.innerHTML = `<div class="sec-head"><div class="sec-title">${icon("music", 18)} ${esc(lbT("libTitle"))}</div><div class="spacer"></div><button class="btn ghost sm" data-act="lib-refresh">${icon("refresh", 14)} ${esc(lbT("refresh"))}</button></div>
    <nav class="lb-tabs" aria-label="${esc(lbT("libCategories"))}">${LIB_CATEGORIES.concat([["favorites", lbT("favorites")], ["playlists", lbT("playlists")]]).map(([id, label]) => `<button class="chip ${id === S.libTab ? "active" : ""}" data-act="lib-tab" data-f="${id}" aria-pressed="${id === S.libTab}">${esc(label)}</button>`).join("")}</nav>
    ${auxiliary ? `<div class="f-hint lb-note">${esc(lbT(S.libTab === "favorites" ? "libFavoritesNote" : "libPlaylistsNote"))}</div>` : `
    <nav class="lb-crumbs" aria-label="${esc(lbT("libBrowsePath"))}">${lbState().frames.map((f, i) => `<button class="btn ghost xs" data-act="lb-back" data-index="${i}" ${i === lbState().frames.length - 1 ? "disabled" : ""}>${esc(f.label)}</button>`).join('<span aria-hidden="true">/</span>')}</nav>
    <div class="lb-toolbar">
      <label>${esc(lbT("libSearchScope"))}<select class="inp" data-change="lb-field" aria-label="${esc(lbT("libSearchScope"))}">${lbOptions(categoryFields, q.field)}</select></label>
      <label class="lb-query">${esc(lbT("search"))}<input class="inp" type="search" id="lb-search" value="${esc(q.q)}" placeholder="${esc(lbT(q.category === "years" ? "libYearPlaceholder" : "libSearchPlaceholder"))}" aria-label="${esc(lbT("libSearch"))}"></label>
      <button class="btn" data-act="lb-search">${esc(lbT("search"))}</button>
      <label>${esc(lbT("libSort"))}<select class="inp" data-change="lb-sort" aria-label="${esc(lbT("libSortLabel"))}">${lbOptions([["name", lbT("libName")], ["year", lbT("years")], ["added", lbT("libAdded")], ["duration", lbT("libDuration")]], q.sort)}</select></label>
      <button class="btn ghost" data-act="lb-direction" aria-label="${esc(lbT("libDirection"))}">${esc(lbT(q.direction === "asc" ? "libAscending" : "libDescending"))}</button>
      ${q.category === "tracks" ? `<div class="lb-view-toggle" role="group" aria-label="${esc(lbT("libView"))}"><button class="btn ghost xs ${LB.trackView === "tiles" ? "active" : ""}" data-act="lb-track-view" data-view="tiles" aria-pressed="${LB.trackView === "tiles"}" title="${esc(lbT("libViewTiles"))}">${icon("grid", 13)}</button><button class="btn ghost xs ${LB.trackView === "list" ? "active" : ""}" data-act="lb-track-view" data-view="list" aria-pressed="${LB.trackView === "list"}" title="${esc(lbT("libViewList"))}">${icon("listView", 13)}</button></div>` : ""}
      ${S.libTab === "artists" ? `<label>${esc(lbT("libArtistMode"))}<select class="inp" data-change="lb-artist-mode" aria-label="${esc(lbT("libArtistMode"))}">${lbOptions([["album", lbT("libAlbumArtistMode")], ["track", lbT("libTrackArtistMode")]], q.artist_mode)}</select></label>` : ""}
      ${q.category === "folders" && q.mount_id ? `<label class="lb-recursive"><input type="checkbox" data-change="lb-recursive" ${q.recursive ? "checked" : ""}>${esc(lbT("libRecursive"))}</label>` : ""}
    </div>
    ${q.category === "artists" && lbState().frames.length === 1 ? lbArtistInitials(q) : ""}
    ${q.category === "years" ? `<div class="f-hint lb-note">${esc(lbT("libYearsNote"))}</div>` : ""}
    ${q.category === "tracks" && Object.hasOwn(q, "album") && q.artist_mode === "album" && typeof favoriteAlbumButton === "function" ? `<div class="lb-note">${favoriteAlbumButton(q.artist, q.album)}</div>` : ""}
    <div class="lb-summary"><span id="lb-total" class="txt-faint"></span><div class="spacer"></div><button class="btn sm" data-act="lb-all" data-mode="playlist" ${LB.busy ? "disabled" : ""}>${esc(lbT("libAllToPlaylist"))}</button><button class="btn primary sm" data-act="lb-all" data-mode="play" ${LB.busy ? "disabled" : ""}>${esc(lbT("libAllPush"))}</button></div>`}`;
}
function lbArtistInitials(q) {
  const counts = new Map(((LB.artist && LB.artist.initials) || []).map(x => [x.key, x.count]));
  const keys = ["", ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0-9", "#"];
  return `<div class="lb-initials lb-index-tiles" aria-label="${esc(lbT("libInitials"))}">${keys.map(initial => {
    const count = initial ? counts.get(initial) : 0;
    return `<button class="lb-index-tile ${String(q.initial || "") === initial ? "active" : ""}" data-act="lb-initial" data-initial="${initial}" aria-pressed="${String(q.initial || "") === initial}"><b>${initial || esc(lbT("all"))}</b>${count ? `<span>${I18n.number(count)}</span>` : ""}</button>`;
  }).join("")}</div><div class="f-hint lb-note">${esc(lbT("libArtistIndexNote"))}</div>`;
}
function lbArtistFiltered() {
  const q = lbFrame().query, initial = q.initial || "", text = String(q.q || "").trim().toLowerCase();
  return ((LB.artist && LB.artist.items) || []).filter(a => {
    if (initial && a.initial !== initial) return false;
    if (!text) return true;
    return String(a.name || "").toLowerCase().includes(text) || String(a.sort_key || "").includes(text);
  });
}
function lbArtistTile(a) {
  const name = a.name || lbT("unknownArtist");
  const mono = a.first_char && a.first_char !== "#" ? a.first_char : (a.initial || "?");
  return `<button class="lb-artist-tile" data-act="lb-artist-open" data-name="${esc(a.name)}"><span class="lb-artist-mono" aria-hidden="true">${esc(mono)}</span><strong class="truncate">${esc(name)}</strong><span class="hint">${esc(lbT("trackCount", { count: I18n.number(a.track_count) }))}</span></button>`;
}
function lbRestoreAnchor() {
  if (LB.restoreAnchor == null || !LB.page) return;
  const q = lbFrame().query, idx = LB.restoreAnchor - (q.offset || 0);
  LB.restoreAnchor = null;
  const items = document.querySelectorAll("#lb-body .lb-album, #lb-body .lb-item, #lb-body .lb-track-tile");
  const el = items[idx]; if (el && el.scrollIntoView) el.scrollIntoView({ block: "nearest" });
}
async function refreshLibraryBrowser() {
  if (!$("#lb-header")) return;
  clearTimeout(LB.searchTimer);
  const seq = ++LB.request, tab = S.libTab;
  LB.busy = true; LB.page = null; LB.error = null;
  paintLibraryBrowserHeader();
  const q = lbFrame().query;
  q.page_size = lbPageSize(q.category);
  $("#global-search").value = q.q || "";
  S.search = q.q || "";
  $("#lb-body").innerHTML = empty("refresh", lbT("loading"), "", true);
  $("#lb-pages").innerHTML = "";
  if (tab === "artists" && lbFrame().query.category === "artists") return refreshArtistIndex(seq);
  if (tab === "favorites") return refreshFavoriteLibrary(seq);
  try {
    let result;
    if (tab === "playlists") result = await api("GET", "/playlists");
    else result = await api("GET", "/library/browse?" + new URLSearchParams(q));
    if (seq !== LB.request || S.view !== "library" || S.libTab !== tab) return;
    if (tab === "playlists") S.playlists = result || [];
    else {
      LB.page = result;
      const last = Math.max(1, Math.ceil(result.total / result.page_size));
      if ((q.offset || 0) >= result.total && result.total > 0) {
        q.offset = (last - 1) * result.page_size; q.page = 1;
        return refreshLibraryBrowser();
      }
    }
    LB.busy = false;
    paintLibraryBrowserHeader(); paintLibraryBrowserItems();
  } catch (e) {
    if (seq !== LB.request || S.view !== "library") return;
    LB.busy = false; LB.error = e;
    libraryBrowserLanguageChanged();
  }
}
async function refreshArtistIndex(seq) {
  const mode = lbFrame().query.artist_mode || "album";
  if (LB.artist && LB.artistMode === mode && Date.now() - LB.artistAt < LB_ARTIST_TTL) {
    LB.busy = false; LB.artistPage = 1;
    paintLibraryBrowserHeader(); paintLibraryBrowserItems();
    return;
  }
  LB.artistLoading = true; LB.artistError = null;
  try {
    const data = await api("GET", "/library/artist-index?artist_mode=" + encodeURIComponent(mode));
    if (seq !== LB.request || S.view !== "library" || S.libTab !== "artists") return;
    if (!data || !Array.isArray(data.items)) throw new Error("invalid artist index");
    LB.artist = data; LB.artistMode = mode; LB.artistAt = Date.now(); LB.artistPage = 1;
    LB.artistLoading = false; LB.busy = false;
    paintLibraryBrowserHeader(); paintLibraryBrowserItems();
  } catch (e) {
    if (seq !== LB.request || S.view !== "library") return;
    LB.artistLoading = false; LB.busy = false; LB.artistError = e;
    libraryBrowserLanguageChanged();
  }
}

function libraryBrowserLanguageChanged() {
  paintLibraryBrowserHeader();
  paintLibraryBrowserItems();
  const body = $("#lb-body");
  if (body && LB.busy) body.innerHTML = empty("refresh", lbT("loading"), "", true);
  else if (body && LB.error) body.innerHTML = empty("alert", lbT("loadFailed", { message: I18n.message(LB.error.data, LB.error.rawMessage || LB.error.message) }), `<button class="btn" data-act="lib-refresh">${esc(lbT("retry"))}</button>`, true);
}
function paintLibraryBrowserItems() {
  const body = $("#lb-body"); if (!body) return;
  if (S.libTab === "playlists") {
    const list = S.playlists.filter(p => hit(p.name));
    body.innerHTML = `<div class="acts mb8"><button class="btn primary sm" data-act="pl-new">${esc(lbT("libNewPlaylist"))}</button></div>` + (list.length ? `<div class="q-list">${list.map(plRow).join("")}</div>` : empty("layers", lbT("libNoPlaylists"), "", true));
    return;
  }
  if (S.libTab === "favorites") { paintFavoriteLibrary(); return; }
  if (S.libTab === "artists" && lbFrame().query.category === "artists") { paintArtistIndex(); return; }
  if (!LB.page) return;
  const p = LB.page;
  $("#lb-total").textContent = lbT("libTotal", { count: I18n.number(p.total), tracks: I18n.number(p.total_tracks) });
  const cat = lbFrame().query.category;
  const tiles = cat === "tracks" && LB.trackView === "tiles";
  const container = cat === "albums" ? "lb-albums" : tiles ? "lb-tracks" : "lb-list";
  const items = tiles ? p.items.map((it, i) => lbTrackTile(it, i)).join("") : p.items.map(lbItem).join("");
  body.innerHTML = `${lbBulkBar()}` + (p.items.length ? `<div class="${container}">${items}</div>` : empty("music", lbT("libNoMusic"), `<button class="btn ghost sm" data-act="settings-open" data-section="folders">${esc(lbT("libFolderSettings"))}</button>`, true));
  $("#lb-pages").innerHTML = lbServerPager(p);
  lbRestoreAnchor();
}
function paintArtistIndex() {
  const body = $("#lb-body"); if (!body) return;
  if (LB.artistLoading) { body.innerHTML = empty("refresh", lbT("loading"), "", true); $("#lb-pages").innerHTML = ""; return; }
  if (LB.artistError) { body.innerHTML = empty("alert", lbT("loadFailed", { message: I18n.message(LB.artistError.data, LB.artistError.rawMessage || LB.artistError.message) }), `<button class="btn" data-act="lib-refresh">${esc(lbT("retry"))}</button>`, true); $("#lb-pages").innerHTML = ""; return; }
  if (!LB.artist) return;
  const items = lbArtistFiltered();
  const size = lbPageSize("artists");
  const pages = Math.max(1, Math.ceil(items.length / size));
  if (LB.artistPage > pages) LB.artistPage = pages;
  const start = (LB.artistPage - 1) * size, slice = items.slice(start, start + size);
  $("#lb-total").textContent = lbT("libArtistTotal", { count: I18n.number(LB.artist.total), matched: I18n.number(items.length) });
  body.innerHTML = slice.length ? `<div class="lb-artist-grid">${slice.map(lbArtistTile).join("")}</div>` : empty("music", lbT("libNoArtists"), "", true);
  $("#lb-pages").innerHTML = lbArtistPager(LB.artistPage, pages, items.length, size);
}

function lbServerPager(p) {
  const size = p.page_size, pages = Math.max(1, Math.ceil(p.total / size));
  const offset = p.offset || (p.page - 1) * size, page = Math.floor(offset / size) + 1;
  const from = p.total ? offset + 1 : 0, to = Math.min(offset + size, p.total);
  const prev = Math.max(0, offset - size), next = Math.min((pages - 1) * size, offset + size);
  return `<div class="lb-pagination"><span class="lb-range">${esc(lbT("libPageRange", { from: I18n.number(from), to: I18n.number(to), total: I18n.number(p.total) }))}</span><div class="spacer"></div><button class="btn sm" data-act="lb-page" data-offset="0" ${page <= 1 ? "disabled" : ""}>${esc(lbT("firstPage"))}</button><button class="btn sm" data-act="lb-page" data-offset="${prev}" ${page <= 1 ? "disabled" : ""}>${esc(lbT("previousPage"))}</button><span class="lb-jump">${esc(lbT("pageNumber"))} <input class="inp" id="lb-page-num" type="number" min="1" max="${pages}" value="${page}" aria-label="${esc(lbT("jumpToPage"))}"> <span>${esc(lbT("pageOf", { pages: I18n.number(pages) }))}</span></span><button class="btn sm" data-act="lb-page-jump">${esc(lbT("jumpToPage"))}</button><button class="btn sm" data-act="lb-page" data-offset="${next}" ${page >= pages ? "disabled" : ""}>${esc(lbT("nextPage"))}</button><button class="btn sm" data-act="lb-page" data-offset="${(pages - 1) * size}" ${page >= pages ? "disabled" : ""}>${esc(lbT("lastPage"))}</button></div>`;
}
function lbArtistPager(page, pages, total, size) {
  const from = total ? (page - 1) * size + 1 : 0, to = Math.min(page * size, total);
  return `<div class="lb-pagination"><span class="lb-range">${esc(lbT("libPageRange", { from: I18n.number(from), to: I18n.number(to), total: I18n.number(total) }))}</span><div class="spacer"></div><button class="btn sm" data-act="lb-artist-page" data-page="1" ${page <= 1 ? "disabled" : ""}>${esc(lbT("firstPage"))}</button><button class="btn sm" data-act="lb-artist-page" data-page="${page - 1}" ${page <= 1 ? "disabled" : ""}>${esc(lbT("previousPage"))}</button><span class="lb-jump">${esc(lbT("pageNumber"))} <input class="inp" id="lb-artist-page-num" type="number" min="1" max="${pages}" value="${page}" aria-label="${esc(lbT("jumpToPage"))}"> <span>${esc(lbT("pageOf", { pages: I18n.number(pages) }))}</span></span><button class="btn sm" data-act="lb-artist-jump">${esc(lbT("jumpToPage"))}</button><button class="btn sm" data-act="lb-artist-page" data-page="${page + 1}" ${page >= pages ? "disabled" : ""}>${esc(lbT("nextPage"))}</button><button class="btn sm" data-act="lb-artist-page" data-page="${pages}" ${page >= pages ? "disabled" : ""}>${esc(lbT("lastPage"))}</button></div>`;
}
function lbTrackTile(item, index) {
  const t = item.track;
  if (!t) return "";
  const check = t.decodable ? `<label class="check lb-check" title="${esc(lbT("libSelectTrack"))}"><input type="checkbox" data-change="lb-select" data-id="${esc(t.id)}" aria-label="${esc(lbT("libSelectTrack"))}" ${LB.sel.has(t.id) ? "checked" : ""}><span class="box"></span></label>` : `<span class="lb-check-space"></span>`;
  const badges = [t.mqa ? (typeof codecMark === "function" ? codecMark("mqa", "MQA") : "MQA") : "", t.codec === "dsd" ? (typeof codecMark === "function" ? codecMark("dsd", "DSD") : "DSD") : ""].filter(Boolean).join(" ");
  return `<article class="lb-track-tile ${LB.sel.has(t.id) ? "selected" : ""}">${check}<button class="lb-track-art" data-act="lb-pick" data-index="${index}" data-mode="play" ${t.decodable ? "" : "disabled"} aria-label="${esc(lbT("libPush"))}">${coverArt(t.album, t.artist, "track-art", 28, t.cover_hash)}<span class="lb-track-play">${icon("play", 14)}</span></button><div class="lb-track-info"><strong class="truncate" title="${esc(t.title || lbT("unknownTrack"))}">${esc(t.title || lbT("unknownTrack"))}</strong><span class="hint truncate">${esc([t.artist || lbT("unknownArtist"), t.album || lbT("unknownAlbum")].join(" · "))}</span><span class="hint mono">${esc([t.codec, fmtDur(t.duration_ms)].filter(Boolean).join(" · "))}</span>${badges ? `<span class="lb-track-badges">${badges}</span>` : ""}</div><div class="lb-track-acts"><button class="btn xs ghost" data-act="lb-pick" data-index="${index}" data-mode="playlist" title="${esc(lbT("libAddPlaylist"))}">${esc(lbT("libAddPlaylistShort"))}</button><button class="btn xs primary" data-act="lb-pick" data-index="${index}" data-mode="play" ${t.decodable ? "" : "disabled"}>${esc(lbT("libPush"))}</button></div></article>`;
}

function lbBulkBar() {
  const n = LB.sel.size, target = PB && PB.id ? (S.nodes.find(x => x.node_id === PB.id)?.alias || shortId(PB.id)) : "";
  return `<div class="lb-bulk"><span class="b-cnt">${esc(lbT("libSelectedCount", { count: I18n.number(n) }))}</span>${target ? `<span class="hint">${esc(lbT("libQueueTarget", { target }))}</span>` : `<span class="hint warn">${esc(lbT("libQueuePickTerminal"))}</span>`}<span class="spacer"></span><button class="btn ghost xs" data-act="lb-bulk-page">${esc(lbT("libSelectPage"))}</button><button class="btn ghost xs" data-act="lb-bulk-filtered">${esc(lbT("libSelectFiltered"))}</button>${n ? `<button class="btn xs" data-act="lb-bulk-enqueue">${esc(lbT("libEnqueue"))}</button><button class="btn xs" data-act="lb-bulk-next">${esc(lbT("libPlayNext"))}</button><button class="btn ghost xs" data-act="lb-bulk-replace">${esc(lbT("libReplaceQueue"))}</button><button class="btn ghost xs" data-act="lb-bulk-playlist">${esc(lbT("libAddPlaylistShort"))}</button><button class="btn ghost xs" data-act="lb-bulk-clear">${esc(lbT("tcClearSelection"))}</button>` : ""}</div>`;
}
function lbBulkTarget() {
  if (!PB || !PB.id) { toast(lbT("libQueuePickTerminal"), "warn"); return ""; }
  return PB.id;
}
async function lbBulkQueue(next, replace) {
  const id = lbBulkTarget(); if (!id || !LB.sel.size) return;
  const ids = Array.from(LB.sel);
  try {
    if (replace) await api("PUT", `/nodes/${enc(id)}/queue`, { track_ids: ids, position: 0 });
    else await api("POST", `/nodes/${enc(id)}/queue/batch`, { track_ids: ids, next: !!next });
    toast(lbT(replace ? "libQueueReplaced" : next ? "libQueuedNext" : "libQueued", { count: I18n.number(ids.length) }), "ok");
    await refreshPlayerBar();
  } catch (e) { toast(e.message, "err"); }
}
function lbSelectPage() {
  (LB.page?.items || []).forEach(item => { if (item.track && item.track.decodable) LB.sel.add(item.track.id); });
  paintLibraryBrowserItems();
}
async function lbSelectFiltered() {
  try {
    const q = { ...lbFrame().query };
    if (q.category === "folders") q.recursive = true;
    const result = await api("GET", "/library/select?" + new URLSearchParams(q));
    (result.track_ids || []).forEach(id => LB.sel.add(id));
    toast(lbT("libSelectedFiltered", { count: I18n.number(LB.sel.size) }), "ok");
    paintLibraryBrowserItems();
  } catch (e) { toast(e.message, "err"); }
}
function lbItemName(item) {
  const key = { artist: "unknownArtist", album: "unknownAlbum", genre: "unknownGenre", composer: "unknownComposer", year: "unknownYear", track: "unknownTrack" }[item.kind];
  // Empty entity values identify server-generated placeholders, not user metadata.
  const missing = item.kind === "year" ? item.year === 0 : item.kind === "track" ? item.track && !item.track.title : item.value === "";
  return missing && key ? lbT(key) : item.name || lbT(key || "unnamed");
}
function lbItem(item, index) {
  const t = item.track, name = lbItemName(item);
  const detail = t ? [t.artist || lbT("unknownArtist"), t.album || lbT("unknownAlbum"), t.year || lbT("unknownYear"), t.genre, t.composer, t.codec, fmtDur(t.duration_ms)].filter(Boolean).join(" · ") : [item.artist, lbT("trackCount", { count: I18n.number(item.track_count) }), item.album_count ? lbT("albumCount", { count: I18n.number(item.album_count) }) : "", item.kind === "year" ? "" : item.year || ""].filter(Boolean).join(" · ");
  const actions = `<div class="lb-item-acts">${item.kind === "album" && lbFrame().query.artist_mode === "album" && typeof favoriteAlbumButton === "function" ? favoriteAlbumButton(item.artist, item.album ?? item.value) : ""}<button class="btn xs ghost" data-act="lb-pick" data-index="${index}" data-mode="playlist" title="${esc(lbT("libAddPlaylist"))}">${esc(lbT("libAddPlaylistShort"))}</button><button class="btn xs primary" data-act="lb-pick" data-index="${index}" data-mode="play" ${t && !t.decodable ? "disabled" : ""}>${esc(lbT("libPush"))}</button></div>`;
  const check = t && t.decodable ? `<label class="check lb-check" title="${esc(lbT("libSelectTrack"))}"><input type="checkbox" data-change="lb-select" data-id="${esc(t.id)}" aria-label="${esc(lbT("libSelectTrack"))}" ${LB.sel.has(t.id) ? "checked" : ""}><span class="box"></span></label>` : `<span class="lb-check-space"></span>`;
  if (item.kind === "album") return `<article class="lb-album"><button class="lb-album-open" data-act="lb-open" data-index="${index}">${coverArt(name, item.artist, "lb-art", 30, item.cover_hash)}<strong>${esc(name)}</strong><span>${esc(detail)}</span></button>${actions}</article>`;
  return `<article class="lb-item ${LB.sel.has(t && t.id) ? "selected" : ""}">${check}${t ? coverArt(t.album, t.artist, "thumb", 20, t.cover_hash) : `<span class="lb-item-icon">${esc(item.initial || "") || icon(item.kind === "folder" || item.kind === "source" ? "folder" : "music", 19)}</span>`}<div class="lb-item-info">${t ? `<strong>${esc(t.title || name)}</strong>` : `<button class="lb-name" data-act="lb-open" data-index="${index}">${esc(name)}</button>`}<span class="txt-faint">${t && (t.mqa || t.codec === "dsd") ? `<span class="badge gold">${t.mqa ? "MQA" : "DSD"}</span> ` : ""}${esc(detail)}</span>${t ? `<span class="txt-faint mono" title="${esc(t.rel_path)}">${esc(t.rel_path)}</span>` : ""}</div>${actions}</article>`;
}
function lbItemQuery(item) {
  const q = { ...lbFrame().query, category: "tracks", page: 1, offset: 0, q: "", initial: "", field: "all" };
  switch (item.kind) {
    case "artist": q.artist = item.value; q.category = "albums"; break;
    case "album": q.artist = item.artist || ""; q.album = item.album ?? item.value; break;
    case "genre": q.genre = item.value; q.category = "albums"; break;
    case "year":
      if (item.year_from) {
        q.year_from = item.year_from; q.year_to = item.year_to;
      } else {
        q.year = 0;
      }
      q.category = "albums";
      break;
    case "composer": q.composer = item.value; q.category = "albums"; break;
    case "source": case "folder": q.category = "folders"; q.mount_id = item.mount_id; q.directory = item.directory || ""; q.recursive = false; break;
  }
  return q;
}
async function libraryBrowserTab(tab) {
  if (!LIB_CATEGORIES.some(c => c[0] === tab) && !["playlists", "favorites"].includes(tab)) return;
  clearTimeout(LB.searchTimer); S.libTab = tab; LB.sel.clear();
  await refreshLibraryBrowser();
}
function libraryBrowserSearch(text) {
  lbFrame().query.q = text; lbFrame().query.page = 1;
  if (S.libTab === "playlists") { paintLibraryBrowserItems(); return; }
  if (S.libTab === "artists" && lbFrame().query.category === "artists" && LB.artist) { LB.artistPage = 1; paintLibraryBrowserItems(); return; }
  clearTimeout(LB.searchTimer);
  LB.searchTimer = setTimeout(() => refreshLibraryBrowser(), 300);
}
async function lbPick(item, mode) {
  let ids;
  if (item && item.track) ids = [item.track.id];
  else {
    const q = item ? lbItemQuery(item) : { ...lbFrame().query };
    if (q.category === "folders") q.recursive = true;
    const result = await api("GET", "/library/select?" + new URLSearchParams(q));
    ids = result.track_ids;
  }
  if (!ids.length) { toast(lbT("libNoDecodable"), "warn"); return; }
  if (mode === "playlist") await openPlPicker(ids);
  else await openPushPicker({ trackIDs: ids, track: item && item.track, label: item ? lbItemName(item) : lbT("libAllResults") });
}
Object.assign(ACT, {
  "lb-open": ds => {
    if (LB.busy) return;
    const item = LB.page.items[Number(ds.index)];
    if (!item || item.track) return;
    lbState().frames.push({ get label() { return lbItemName(item); }, query: lbItemQuery(item) });
    return refreshLibraryBrowser();
  },
  "lb-back": ds => { lbState().frames.splice(Number(ds.index) + 1); return refreshLibraryBrowser(); },
  "lb-page": ds => { if (LB.busy) return; const q = lbFrame().query; q.offset = Math.max(0, Number(ds.offset) || 0); q.page = 1; LB.restoreAnchor = q.offset; return refreshLibraryBrowser(); },
  "lb-direction": () => { const q = lbFrame().query; q.direction = q.direction === "asc" ? "desc" : "asc"; q.page = 1; q.offset = 0; return refreshLibraryBrowser(); },
  "lb-initial": ds => {
    const q = lbFrame().query; q.initial = ds.initial; q.page = 1; q.offset = 0;
    if (S.libTab === "artists" && q.category === "artists" && LB.artist) { LB.artistPage = 1; paintLibraryBrowserHeader(); paintLibraryBrowserItems(); return; }
    return refreshLibraryBrowser();
  },
  "lb-artist-page": ds => { LB.artistPage = Math.max(1, Number(ds.page) || 1); paintLibraryBrowserItems(); },
  "lb-artist-jump": () => {
    const items = lbArtistFiltered(), pages = Math.max(1, Math.ceil(items.length / lbPageSize("artists")));
    const n = Math.max(1, Math.min(pages, Number($("#lb-artist-page-num")?.value) || 1));
    LB.artistPage = n; paintLibraryBrowserItems();
  },
  "lb-track-view": ds => {
    LB.trackView = ds.view === "list" ? "list" : "tiles";
    try { localStorage.setItem("lbTrackView", LB.trackView); } catch (e) {}
    paintLibraryBrowserHeader(); paintLibraryBrowserItems();
  },
  "lb-page-jump": () => {
    if (!LB.page) return;
    const pages = Math.max(1, Math.ceil(LB.page.total / LB.page.page_size));
    const n = Math.max(1, Math.min(pages, Number($("#lb-page-num")?.value) || 1));
    const q = lbFrame().query; q.offset = (n - 1) * LB.page.page_size; q.page = 1; LB.restoreAnchor = q.offset;
    refreshLibraryBrowser();
  },
  "lb-artist-open": ds => { const q = lbItemQuery({ kind: "artist", value: ds.name || "" }); lbState().frames.push({ label: ds.name || lbT("unknownArtist"), query: q }); return refreshLibraryBrowser(); },
  "lb-search": () => {
    lbFrame().query.q = $("#lb-search").value; lbFrame().query.page = 1; lbFrame().query.offset = 0;
    if (S.libTab === "artists" && lbFrame().query.category === "artists" && LB.artist) { LB.artistPage = 1; paintLibraryBrowserItems(); return; }
    return refreshLibraryBrowser();
  },
  "lb-pick": ds => { if (!LB.busy) return lbPick(LB.page.items[Number(ds.index)], ds.mode); },
  "lb-all": ds => { if (!LB.busy && LB.page) return lbPick(null, ds.mode); },
  "lb-bulk-page": () => { if (!LB.busy) lbSelectPage(); },
  "lb-bulk-filtered": () => { if (!LB.busy) return lbSelectFiltered(); },
  "lb-bulk-enqueue": () => lbBulkQueue(false, false),
  "lb-bulk-next": () => lbBulkQueue(true, false),
  "lb-bulk-replace": () => lbBulkQueue(false, true),
  "lb-bulk-playlist": () => { if (LB.sel.size) return openPlPicker(Array.from(LB.sel)); },
  "lb-bulk-clear": () => { LB.sel.clear(); paintLibraryBrowserItems(); }
});
Object.assign(CHG, {
  "lb-select": el => { const id = el.dataset.id; if (!id) return; if (el.checked) LB.sel.add(id); else LB.sel.delete(id); paintLibraryBrowserItems(); },
  "lb-field": el => { lbFrame().query.field = el.value; lbFrame().query.page = 1; lbFrame().query.offset = 0; return refreshLibraryBrowser(); },
  "lb-sort": el => { lbFrame().query.sort = el.value; lbFrame().query.page = 1; lbFrame().query.offset = 0; return refreshLibraryBrowser(); },

  "lb-recursive": el => { lbFrame().query.recursive = el.checked; lbFrame().query.page = 1; lbFrame().query.offset = 0; return refreshLibraryBrowser(); },
  "lb-artist-mode": el => { const state = lbState(); state.frames.splice(1); state.frames[0].query.artist_mode = el.value; state.frames[0].query.page = 1; state.frames[0].query.offset = 0; LB.artistAt = 0; LB.artistPage = 1; return refreshLibraryBrowser(); }
});
document.addEventListener("keydown", e => {
  if (e.target.id === "lb-search" && e.key === "Enter") { e.preventDefault(); ACT["lb-search"](); }
  if (e.target.id === "lb-page-num" && e.key === "Enter") { e.preventDefault(); ACT["lb-page-jump"](); }
  if (e.target.id === "lb-artist-page-num" && e.key === "Enter") { e.preventDefault(); ACT["lb-artist-jump"](); }
});
document.addEventListener("input", e => { if (e.target.id === "lb-search") lbFrame().query.q = e.target.value; });
