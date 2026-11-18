"use strict";

const LIB_CATEGORIES = [["artists", "艺术家"], ["albums", "专辑"], ["tracks", "曲目"], ["genres", "风格"], ["years", "年份"], ["composers", "作曲家"], ["folders", "文件目录"]];
const LB = { states: {}, page: null, busy: false, request: 0, searchTimer: null };
function lbState() {
  const category = S.libTab || "albums";
  if (!LB.states[category]) LB.states[category] = { frames: [{ label: (LIB_CATEGORIES.find(c => c[0] === category) || [category, category])[1], query: { category, page: 1, page_size: 24, field: "all", sort: "name", direction: "asc", artist_mode: "album", q: "" } }] };
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
    artists: [["all", "名字 / 相关专辑"], ["artist", "艺术家名字"]],
    albums: [["all", "专辑 / 艺术家"], ["album", "专辑名"], ["artist", "艺术家"]],
    tracks: [["all", "全部元数据"], ["title", "曲目名"], ["artist", "艺术家"], ["album", "专辑"], ["genre", "风格"], ["composer", "作曲家"], ["year", "年份"], ["path", "文件路径"]],
    genres: [["genre", "风格名称"]], years: [["year", "年代 / 年份"]], composers: [["composer", "作曲家名字"]], folders: [["path", "目录 / 文件路径"], ["title", "曲目名"]]
  };
  const categoryFields = fields[q.category] || [];
  if (!auxiliary && !categoryFields.some(([key]) => key === q.field)) q.field = categoryFields[0][0];
  el.innerHTML = `<div class="sec-head"><div class="sec-title">${icon("music", 18)} 音乐库</div><div class="spacer"></div><button class="btn ghost sm" data-act="lib-refresh">${icon("refresh", 14)} 刷新</button></div>
    <nav class="lb-tabs" aria-label="曲库分类">${LIB_CATEGORIES.concat([["favorites", "我喜欢的"], ["playlists", "播放列表"]]).map(([id, label]) => `<button class="chip ${id === S.libTab ? "active" : ""}" data-act="lib-tab" data-f="${id}" aria-pressed="${id === S.libTab}">${label}</button>`).join("")}</nav>
    ${auxiliary ? `<div class="f-hint lb-note">${S.libTab === "favorites" ? "曲目和专辑分别收藏，保存在服务端；文件不可用时仍保留收藏。" : "播放列表保存在服务端，可跨终端推送。"}</div>` : `
    <nav class="lb-crumbs" aria-label="浏览路径">${lbState().frames.map((f, i) => `<button class="btn ghost xs" data-act="lb-back" data-index="${i}" ${i === lbState().frames.length - 1 ? "disabled" : ""}>${esc(f.label)}</button>`).join('<span aria-hidden="true">/</span>')}</nav>
    <div class="lb-toolbar">
      <label>搜索范围<select class="inp" data-change="lb-field" aria-label="搜索范围">${lbOptions(categoryFields, q.field)}</select></label>
      <label class="lb-query">搜索<input class="inp" type="search" id="lb-search" value="${esc(q.q)}" placeholder="${q.category === "years" ? "如 1980、1985 或 1980–1989" : "搜索当前分类…"}" aria-label="搜索音乐库"></label>
      <button class="btn" data-act="lb-search">搜索</button>
      <label>排序<select class="inp" data-change="lb-sort" aria-label="曲库排序">${lbOptions([["name", "名称"], ["year", "年份"], ["added", "添加时间"], ["duration", "时长"]], q.sort)}</select></label>
      <button class="btn ghost" data-act="lb-direction" aria-label="切换排序方向">${q.direction === "asc" ? "升序 ↑" : "降序 ↓"}</button>
      ${S.libTab === "artists" ? `<label>艺术家口径<select class="inp" data-change="lb-artist-mode" aria-label="艺术家口径">${lbOptions([["album", "专辑艺术家"], ["track", "曲目艺术家"]], q.artist_mode)}</select></label>` : ""}
      ${q.category === "folders" && q.mount_id ? `<label class="lb-recursive"><input type="checkbox" data-change="lb-recursive" ${q.recursive ? "checked" : ""}>搜索子目录</label>` : ""}
    </div>
    ${q.category === "artists" ? `<div class="lb-initials" aria-label="艺术家首字母">${["", ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0-9", "#"].map(initial => `<button class="chip ${String(q.initial || "") === initial ? "active" : ""}" data-act="lb-initial" data-initial="${initial}" aria-pressed="${String(q.initial || "") === initial}">${initial || "全部"}</button>`).join("")}</div><div class="f-hint lb-note">中文按拼音首字母归类；数字与其他字符单列。缺失标签显示为未知。</div>` : ""}
    ${q.category === "years" ? '<div class="f-hint lb-note">按十年年代分组，点击时间段查看专辑；缺失年份单列为“未知年份”。</div>' : ""}
    ${q.category === "tracks" && Object.hasOwn(q, "album") && q.artist_mode === "album" && typeof favoriteAlbumButton === "function" ? `<div class="lb-note">${favoriteAlbumButton(q.artist, q.album)}</div>` : ""}
    <div class="lb-summary"><span id="lb-total" class="txt-faint"></span><div class="spacer"></div><button class="btn sm" data-act="lb-all" data-mode="playlist" ${LB.busy ? "disabled" : ""}>全部结果加入列表</button><button class="btn primary sm" data-act="lb-all" data-mode="play" ${LB.busy ? "disabled" : ""}>全部结果推送 / 排队</button></div>`}`;
}
async function refreshLibraryBrowser() {
  if (!$("#lb-header")) return;
  clearTimeout(LB.searchTimer);
  const seq = ++LB.request, tab = S.libTab;
  LB.busy = true; LB.page = null;
  paintLibraryBrowserHeader();
  const q = lbFrame().query;
  $("#global-search").value = q.q || "";
  S.search = q.q || "";
  $("#lb-body").innerHTML = empty("refresh", "正在加载…", "", true);
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
    LB.busy = false;
    $("#lb-body").innerHTML = empty("alert", "加载失败：" + e.message, '<button class="btn" data-act="lib-refresh">重试</button>', true);
  }
}
function paintLibraryBrowserItems() {
  const body = $("#lb-body"); if (!body) return;
  if (S.libTab === "playlists") {
    const list = S.playlists.filter(p => hit(p.name));
    body.innerHTML = '<div class="acts mb8"><button class="btn primary sm" data-act="pl-new">新建播放列表</button></div>' + (list.length ? `<div class="q-list">${list.map(plRow).join("")}</div>` : empty("layers", "还没有匹配的播放列表", "", true));
    return;
  }
  if (S.libTab === "favorites") { paintFavoriteLibrary(); return; }
  if (!LB.page) return;
  const p = LB.page;
  $("#lb-total").textContent = `${p.total} 项 · ${p.total_tracks} 首曲目（批量操作包含全部页，仅选择可解码曲目）`;
  body.innerHTML = p.items.length ? `<div class="${lbFrame().query.category === "albums" ? "lb-albums" : "lb-list"}">${p.items.map(lbItem).join("")}</div>` : empty("music", "没有匹配的音乐；可调整搜索，或到设置中添加音乐文件夹并扫描。", '<button class="btn ghost sm" data-act="settings-open" data-section="folders">音乐文件夹设置</button>', true);
  const count = Math.max(1, Math.ceil(p.total / p.page_size));
  $("#lb-pages").innerHTML = `<div class="lb-pagination"><span>第 ${p.page} / ${count} 页</span><button class="btn sm" data-act="lb-page" data-page="${p.page - 1}" ${p.page <= 1 ? "disabled" : ""}>上一页</button><button class="btn sm" data-act="lb-page" data-page="${p.page + 1}" ${p.page >= count ? "disabled" : ""}>下一页</button><label>每页 <select class="inp" data-change="lb-size" aria-label="每页数量">${lbOptions([[24, "24"], [48, "48"], [96, "96"]], p.page_size)}</select></label></div>`;
}
function lbItem(item, index) {
  const t = item.track, name = item.name || ({ artist: "未知艺术家", album: "未知专辑", genre: "未知风格", composer: "未知作曲家", year: "未知年份" }[item.kind]) || "未命名";
  const detail = t ? [t.artist || "未知艺术家", t.album || "未知专辑", t.year || "未知年份", t.genre, t.composer, t.codec, fmtDur(t.duration_ms)].filter(Boolean).join(" · ") : [item.artist, `${item.track_count} 首`, item.album_count ? `${item.album_count} 张专辑` : "", item.kind === "year" ? "" : item.year || ""].filter(Boolean).join(" · ");
  const actions = `<div class="lb-item-acts">${item.kind === "album" && lbFrame().query.artist_mode === "album" && typeof favoriteAlbumButton === "function" ? favoriteAlbumButton(item.artist, item.album ?? item.value) : ""}<button class="btn xs ghost" data-act="lb-pick" data-index="${index}" data-mode="playlist" title="加入播放列表">加列表</button><button class="btn xs primary" data-act="lb-pick" data-index="${index}" data-mode="play" ${t && !t.decodable ? "disabled" : ""}>推送 / 排队</button></div>`;
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
  if (!ids.length) { toast("没有可解码的匹配曲目", "warn"); return; }
  if (mode === "playlist") await openPlPicker(ids);
  else await openPushPicker({ trackIDs: ids, track: item && item.track, label: item ? item.name : "全部匹配结果" });
}
Object.assign(ACT, {
  "lb-open": ds => {
    if (LB.busy) return;
    const item = LB.page.items[Number(ds.index)];
    if (!item || item.track) return;
    lbState().frames.push({ label: item.name || "未知", query: lbItemQuery(item) });
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
