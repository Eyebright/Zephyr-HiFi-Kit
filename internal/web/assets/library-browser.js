"use strict";

const lbT = (key, params) => I18n.t("features." + key, params);
const LIB_CATEGORIES = [["artists", "artists"], ["albums", "albums"], ["tracks", "tracks"], ["genres", "genres"], ["years", "years"], ["composers", "composers"], ["folders", "libFolders"]].map(([id, key]) => Object.defineProperty([id], 1, { get: () => lbT(key), enumerable: true }));
const LB = { states: {}, page: null, error: null, busy: false, request: 0, searchTimer: null };
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
      ${S.libTab === "artists" ? `<label>${esc(lbT("libArtistMode"))}<select class="inp" data-change="lb-artist-mode" aria-label="${esc(lbT("libArtistMode"))}">${lbOptions([["album", lbT("libAlbumArtistMode")], ["track", lbT("libTrackArtistMode")]], q.artist_mode)}</select></label>` : ""}
      ${q.category === "folders" && q.mount_id ? `<label class="lb-recursive"><input type="checkbox" data-change="lb-recursive" ${q.recursive ? "checked" : ""}>${esc(lbT("libRecursive"))}</label>` : ""}
    </div>
    ${q.category === "artists" ? `<div class="lb-initials" aria-label="${esc(lbT("libInitials"))}">${["", ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0-9", "#"].map(initial => `<button class="chip ${String(q.initial || "") === initial ? "active" : ""}" data-act="lb-initial" data-initial="${initial}" aria-pressed="${String(q.initial || "") === initial}">${initial || esc(lbT("all"))}</button>`).join("")}</div><div class="f-hint lb-note">${esc(lbT("libInitialsNote"))}</div>` : ""}
    ${q.category === "years" ? `<div class="f-hint lb-note">${esc(lbT("libYearsNote"))}</div>` : ""}
    ${q.category === "tracks" && Object.hasOwn(q, "album") && q.artist_mode === "album" && typeof favoriteAlbumButton === "function" ? `<div class="lb-note">${favoriteAlbumButton(q.artist, q.album)}</div>` : ""}
    <div class="lb-summary"><span id="lb-total" class="txt-faint"></span><div class="spacer"></div><button class="btn sm" data-act="lb-all" data-mode="playlist" ${LB.busy ? "disabled" : ""}>${esc(lbT("libAllToPlaylist"))}</button><button class="btn primary sm" data-act="lb-all" data-mode="play" ${LB.busy ? "disabled" : ""}>${esc(lbT("libAllPush"))}</button></div>`}`;
}
async function refreshLibraryBrowser() {
  if (!$("#lb-header")) return;
  clearTimeout(LB.searchTimer);
  const seq = ++LB.request, tab = S.libTab;
  LB.busy = true; LB.page = null; LB.error = null;
  paintLibraryBrowserHeader();
  const q = lbFrame().query;
  $("#global-search").value = q.q || "";
  S.search = q.q || "";
  $("#lb-body").innerHTML = empty("refresh", lbT("loading"), "", true);
  $("#lb-pages").innerHTML = "";
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
      if (q.page > last) { q.page = last; return refreshLibraryBrowser(); }
    }
    LB.busy = false;
    paintLibraryBrowserHeader(); paintLibraryBrowserItems();
  } catch (e) {
    if (seq !== LB.request || S.view !== "library") return;
    LB.busy = false; LB.error = e;
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
  if (!LB.page) return;
  const p = LB.page;
  $("#lb-total").textContent = lbT("libTotal", { count: I18n.number(p.total), tracks: I18n.number(p.total_tracks) });
  body.innerHTML = p.items.length ? `<div class="${lbFrame().query.category === "albums" ? "lb-albums" : "lb-list"}">${p.items.map(lbItem).join("")}</div>` : empty("music", lbT("libNoMusic"), `<button class="btn ghost sm" data-act="settings-open" data-section="folders">${esc(lbT("libFolderSettings"))}</button>`, true);
  const count = Math.max(1, Math.ceil(p.total / p.page_size));
  $("#lb-pages").innerHTML = `<div class="lb-pagination"><span>${esc(lbT("page", { page: I18n.number(p.page), pages: I18n.number(count) }))}</span><button class="btn sm" data-act="lb-page" data-page="${p.page - 1}" ${p.page <= 1 ? "disabled" : ""}>${esc(lbT("previousPage"))}</button><button class="btn sm" data-act="lb-page" data-page="${p.page + 1}" ${p.page >= count ? "disabled" : ""}>${esc(lbT("nextPage"))}</button><label>${esc(lbT("perPage"))} <select class="inp" data-change="lb-size" aria-label="${esc(lbT("pageSize"))}">${lbOptions([24, 48, 96].map(n => [n, I18n.number(n)]), p.page_size)}</select></label></div>`;
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
  if (item.kind === "album") return `<article class="lb-album"><button class="lb-album-open" data-act="lb-open" data-index="${index}">${coverArt(name, item.artist, "lb-art", 30, item.cover_hash)}<strong>${esc(name)}</strong><span>${esc(detail)}</span></button>${actions}</article>`;
  return `<article class="lb-item">${t ? coverArt(t.album, t.artist, "thumb", 20, t.cover_hash) : `<span class="lb-item-icon">${esc(item.initial || "") || icon(item.kind === "folder" || item.kind === "source" ? "folder" : "music", 19)}</span>`}<div class="lb-item-info">${t ? `<strong>${esc(t.title || name)}</strong>` : `<button class="lb-name" data-act="lb-open" data-index="${index}">${esc(name)}</button>`}<span class="txt-faint">${esc(detail)}</span>${t ? `<span class="txt-faint mono" title="${esc(t.rel_path)}">${esc(t.rel_path)}</span>` : ""}</div>${actions}</article>`;
}
function lbItemQuery(item) {
  const q = { ...lbFrame().query, category: "tracks", page: 1, q: "", initial: "", field: "all" };
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
  clearTimeout(LB.searchTimer); S.libTab = tab;
  await refreshLibraryBrowser();
}
function libraryBrowserSearch(text) {
  lbFrame().query.q = text; lbFrame().query.page = 1;
  if (S.libTab === "playlists") { paintLibraryBrowserItems(); return; }
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
  "lb-page": ds => { if (LB.busy) return; lbFrame().query.page = Number(ds.page); return refreshLibraryBrowser(); },
  "lb-direction": () => { const q = lbFrame().query; q.direction = q.direction === "asc" ? "desc" : "asc"; q.page = 1; return refreshLibraryBrowser(); },
  "lb-initial": ds => { const q = lbFrame().query; q.initial = ds.initial; q.page = 1; return refreshLibraryBrowser(); },
  "lb-search": () => { lbFrame().query.q = $("#lb-search").value; lbFrame().query.page = 1; return refreshLibraryBrowser(); },
  "lb-pick": ds => { if (!LB.busy) return lbPick(LB.page.items[Number(ds.index)], ds.mode); },
  "lb-all": ds => { if (!LB.busy && LB.page) return lbPick(null, ds.mode); }
});
Object.assign(CHG, {
  "lb-field": el => { lbFrame().query.field = el.value; lbFrame().query.page = 1; return refreshLibraryBrowser(); },
  "lb-sort": el => { lbFrame().query.sort = el.value; lbFrame().query.page = 1; return refreshLibraryBrowser(); },
  "lb-size": el => { lbFrame().query.page_size = Number(el.value); lbFrame().query.page = 1; return refreshLibraryBrowser(); },
  "lb-recursive": el => { lbFrame().query.recursive = el.checked; lbFrame().query.page = 1; return refreshLibraryBrowser(); },
  "lb-artist-mode": el => { const state = lbState(); state.frames.splice(1); state.frames[0].query.artist_mode = el.value; state.frames[0].query.page = 1; return refreshLibraryBrowser(); }
});
document.addEventListener("keydown", e => { if (e.target.id === "lb-search" && e.key === "Enter") { e.preventDefault(); ACT["lb-search"](); } });
document.addEventListener("input", e => { if (e.target.id === "lb-search") lbFrame().query.q = e.target.value; });
