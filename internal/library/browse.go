package library

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/mozillazg/go-pinyin"
	"modernc.org/sqlite"
)

// BrowseQuery uses pointers to distinguish no filter from an unknown (empty/0)
// entity. Directory is a slash-separated path relative to MountID, never an OS
// path. YearFrom/YearTo form an inclusive range and cannot be combined with Year.
// Page is one-based (0 defaults to 1); PageSize defaults to 20 and accepts
// 1..200. Offset, when >0, overrides Page so callers can preserve a position
// while the adaptive page size changes.
type BrowseQuery struct {
	Category, Q, Field, Sort, Direction, ArtistMode, Initial, MountID, Directory string
	Artist, Album, Genre, Composer                                               *string
	Year, YearFrom, YearTo                                                       *int
	Page, PageSize, Offset                                                       int
	Recursive                                                                    bool
}

type BrowsePage struct {
	Category    string       `json:"category"`
	Page        int          `json:"page"`
	PageSize    int          `json:"page_size"`
	Offset      int          `json:"offset"`
	Total       int          `json:"total"`
	TotalTracks int          `json:"total_tracks"`
	Items       []BrowseItem `json:"items"`
}

// Album identity is (Artist, Value); folder identity is (MountID, Directory).
// Other entity values may be empty; year 0 is represented by Value "0".
type BrowseItem struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Value      string `json:"value"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	Year       int    `json:"year"`
	YearFrom   int    `json:"year_from,omitempty"`
	YearTo     int    `json:"year_to,omitempty"`
	CoverHash  string `json:"cover_hash"`
	TrackCount int    `json:"track_count"`
	AlbumCount int    `json:"album_count"`
	DurationMS int64  `json:"duration_ms"`
	MountID    string `json:"mount_id"`
	Directory  string `json:"directory"`
	Initial    string `json:"initial"`
	Track      *Track `json:"track,omitempty"`
}

// Register before any store connection opens. Deterministic SQL functions keep
// old rows sortable without a rescan, materialized keys or client-side paging.
func init() {
	functions := map[string]func(string) string{
		"library_sort":    pinyinKey,
		"library_initial": browseInitial,
		"library_values": func(s string) string {
			values := []string{}
			seen := map[string]bool{}
			for _, v := range strings.Split(s, ";") {
				v = strings.TrimSpace(v)
				key := strings.ToLower(v)
				if v != "" && !seen[key] {
					seen[key] = true
					values = append(values, v)
				}
			}
			if len(values) == 0 {
				values = append(values, "")
			}
			b, _ := json.Marshal(values)
			return string(b)
		},
		"library_dir": func(s string) string {
			d := path.Dir(s)
			if d == "." {
				return ""
			}
			return d
		},
	}
	for name, fn := range functions {
		sqlite.MustRegisterDeterministicScalarFunction(name, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			s, _ := args[0].(string)
			return fn(s), nil
		})
	}
}

func pinyinKey(s string) string {
	a := pinyin.NewArgs()
	a.Fallback = func(r rune, _ pinyin.Args) []string { return []string{string(r)} }
	return strings.ToLower(strings.Join(pinyin.LazyPinyin(strings.TrimSpace(s), a), ""))
}

func browseInitial(s string) string {
	key := pinyinKey(s)
	if key != "" {
		if key[0] >= 'a' && key[0] <= 'z' {
			return strings.ToUpper(key[:1])
		}
		if key[0] >= '0' && key[0] <= '9' {
			return "0-9"
		}
	}
	return "#"
}

func normalizeBrowse(q BrowseQuery) (BrowseQuery, error) {
	if q.Category == "" {
		q.Category = "tracks"
	}
	switch q.Category {
	case "artists", "albums", "tracks", "genres", "years", "composers", "folders":
	default:
		return q, fmt.Errorf("无效分类")
	}
	if q.ArtistMode == "" {
		q.ArtistMode = "album"
	}
	if q.ArtistMode != "album" && q.ArtistMode != "track" {
		return q, fmt.Errorf("无效艺术家模式")
	}
	if q.Field == "" || q.Field == "name" {
		q.Field = map[string]string{"artists": "artist", "albums": "album", "tracks": "title", "genres": "genre", "years": "year", "composers": "composer", "folders": "path"}[q.Category]
	}
	switch q.Field {
	case "all", "title", "artist", "album", "genre", "composer", "year", "path":
	default:
		return q, fmt.Errorf("无效搜索字段")
	}
	if q.Sort == "" {
		q.Sort = "name"
	}
	switch q.Sort {
	case "name", "year", "added", "duration":
	default:
		return q, fmt.Errorf("无效排序字段")
	}
	if q.Direction == "" {
		q.Direction = "asc"
	}
	if q.Direction != "asc" && q.Direction != "desc" {
		return q, fmt.Errorf("无效排序方向")
	}
	q.Initial = strings.ToUpper(q.Initial)
	if q.Initial != "" && q.Initial != "#" && q.Initial != "0-9" && !(len(q.Initial) == 1 && q.Initial[0] >= 'A' && q.Initial[0] <= 'Z') {
		return q, fmt.Errorf("无效首字母")
	}
	if q.Page < 0 {
		return q, fmt.Errorf("页码不能为负数")
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 20
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		return q, fmt.Errorf("每页数量必须在 1..200 之间")
	}
	if q.Offset < 0 {
		return q, fmt.Errorf("偏移量不能为负数")
	}
	if q.Offset > 0 {
		q.Page = q.Offset/q.PageSize + 1
	} else {
		if q.Page-1 > int(^uint(0)>>1)/q.PageSize {
			return q, fmt.Errorf("页码过大")
		}
		q.Offset = (q.Page - 1) * q.PageSize
	}
	if q.Year != nil && *q.Year < 0 {
		return q, fmt.Errorf("年份不能为负数")
	}
	if q.YearFrom != nil || q.YearTo != nil {
		if q.YearFrom == nil || q.YearTo == nil {
			return q, fmt.Errorf("year_from 和 year_to 必须同时提供")
		}
		if q.Year != nil {
			return q, fmt.Errorf("年份范围不能与 year 同时使用")
		}
		if *q.YearFrom < 1 || *q.YearTo > 9999 || *q.YearFrom > *q.YearTo {
			return q, fmt.Errorf("年份范围必须为 1..9999，且 year_from 不大于 year_to")
		}
	}
	q.Directory = strings.ReplaceAll(q.Directory, "\\", "/")
	if strings.HasPrefix(q.Directory, "/") || strings.ContainsAny(q.Directory, ":\x00") {
		return q, fmt.Errorf("目录必须为相对路径")
	}
	for _, part := range strings.Split(q.Directory, "/") {
		if part == ".." {
			return q, fmt.Errorf("目录不能越界")
		}
	}
	if q.Directory != "" {
		q.Directory = path.Clean(q.Directory)
		if q.Directory == "." {
			q.Directory = ""
		}
	}
	if q.Directory != "" && q.MountID == "" {
		return q, fmt.Errorf("目录筛选需要音乐源")
	}
	return q, nil
}

func likeLiteral(s string) string {
	return "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(s) + "%"
}

const albumArtistSQL = `COALESCE(NULLIF(t.album_artist,''),t.artist)`

// browseSQL builds a membership relation (one track per entity), then aggregates
// entirely in SQLite. The same relation drives counts, paging and selection.
func browseSQL(q BrowseQuery, selecting bool) (string, []any) {
	artist := albumArtistSQL
	if q.ArtistMode == "track" {
		artist = "t.artist"
	}
	where := []string{"1=1"}
	args := []any{}
	add := func(expr string, val any) { where = append(where, expr); args = append(args, val) }
	if q.MountID != "" {
		add("t.mount_id=?", q.MountID)
	}
	if q.Artist != nil {
		add(artist+"=?", *q.Artist)
	}
	if q.Album != nil {
		add("t.album=?", *q.Album)
	}
	if q.Year != nil {
		add("t.year=?", *q.Year)
	}
	if q.YearFrom != nil && q.YearTo != nil {
		add("t.year>=?", *q.YearFrom)
		add("t.year<=?", *q.YearTo)
	}
	for _, f := range []struct {
		column string
		value  *string
	}{{"genre", q.Genre}, {"composer", q.Composer}} {
		if f.value != nil {
			add(`EXISTS(SELECT 1 FROM json_each(library_values(t.`+f.column+`)) WHERE value=?)`, *f.value)
		}
	}
	prefix := q.Directory
	if prefix != "" {
		prefix += "/"
	}
	if q.Directory != "" || (q.Category == "folders" && q.MountID != "") {
		if q.Recursive || (q.Category == "folders" && !selecting) {
			if prefix != "" {
				where = append(where, `substr(t.rel_path,1,?)=?`)
				args = append(args, len([]rune(prefix)), prefix)
			}
		} else {
			add(`library_dir(t.rel_path)=?`, q.Directory)
		}
	}
	sources := q.Category == "folders" && q.MountID == ""
	// Named categories search whole entities rather than a subset of their tracks.
	// Other fields retain their track-level search semantics.
	entitySearch := (q.Category == "genres" && q.Field == "genre") || (q.Category == "composers" && q.Field == "composer")
	yearSearch := q.Category == "years" && q.Field == "year"
	sourceSearch := sources && (q.Field == "path" || q.Field == "all")
	if q.Q != "" && !entitySearch && !yearSearch && !sourceSearch {
		fields := map[string]string{"title": "t.title", "artist": artist, "album": "t.album", "genre": "t.genre", "composer": "t.composer", "year": "CAST(t.year AS TEXT)", "path": "t.rel_path"}
		cols := []string{fields[q.Field]}
		if q.Field == "all" {
			cols = []string{"t.title", "t.artist", "t.album_artist", "t.album", "t.genre", "t.composer", "CAST(t.year AS TEXT)", "t.rel_path"}
		}
		search := []string{}
		for _, col := range cols {
			search = append(search, col+` LIKE ? ESCAPE '\'`)
			args = append(args, likeLiteral(q.Q))
		}
		where = append(where, "("+strings.Join(search, " OR ")+")")
	}
	base := `WITH base AS (SELECT t.*, ` + artist + ` AS browse_artist FROM tracks t WHERE ` + strings.Join(where, " AND ") + `), `
	// kind/name/value/artist/album/mount/directory/track identity are separate from
	// metadata used for aggregation, so unknown and duplicate names remain safe.
	identity := ""
	from := "base b"
	switch q.Category {
	case "artists":
		identity = `'artist',b.browse_artist,b.browse_artist,b.browse_artist,'','','',''`
	case "albums":
		identity = `'album',b.album,b.album,b.browse_artist,b.album,'','',''`
	case "genres", "composers":
		kind, col := "genre", "genre"
		if q.Category == "composers" {
			kind, col = "composer", "composer"
		}
		identity = `'` + kind + `',v.value,v.value,'','','','',''`
		from += ` JOIN json_each(library_values(b.` + col + `)) v`
	case "years":
		// Unknown years must remain separate from the 1–9 interval.
		decade := `CAST(MAX(1,b.year/10*10) AS TEXT)||'–'||CAST(b.year/10*10+9 AS TEXT)`
		identity = `'year',CASE WHEN b.year=0 THEN '' ELSE ` + decade + ` END,CASE WHEN b.year=0 THEN '0' ELSE ` + decade + ` END,'','','','',''`
	case "tracks":
		identity = `'track',b.title,b.id,b.artist,b.album,b.mount_id,library_dir(b.rel_path),b.id`
	case "folders":
		if sources {
			identity = `'source',m.share,m.id,'','',m.id,'',''`
			from = `mounts m LEFT JOIN base b ON b.mount_id=m.id`
		} else {
			// substr is character-based, not byte-based (Chinese directories).
			rest := fmt.Sprintf("substr(b.rel_path,%d)", len([]rune(prefix))+1)
			child := `substr(` + rest + `,1,instr(` + rest + `,'/')-1)`
			// Prefix originates in a bound parameter, never interpolated SQL.
			identity = `'folder',` + child + `,?||` + child + `,'','',b.mount_id,?||` + child + `,''`
			args = append(args, prefix, prefix)
			from += ` WHERE instr(` + rest + `,'/')>0`
		}
	}
	columns := `kind,name,value,entity_artist,entity_album,entity_mount,entity_dir,track_id`
	// Common metadata columns allow source rows without tracks (zero aggregates).
	metadata := `b.id,b.cover_hash,b.duration_ms,b.added_at,b.year,b.browse_artist,b.album`
	// SQLite column aliases are declared once on the raw CTE.
	base += `raw(id,cover_hash,duration_ms,added_at,year,browse_artist,album,` + columns + `) AS (`
	raw := `SELECT ` + metadata + `,` + identity + ` FROM ` + from
	if q.Category == "folders" && !sources {
		raw += ` UNION ALL SELECT ` + metadata + `,'track',b.title,b.id,b.artist,b.album,b.mount_id,library_dir(b.rel_path),b.id FROM base b`
		if !q.Recursive {
			raw += ` WHERE library_dir(b.rel_path)=?`
			args = append(args, q.Directory)
		}
	}
	memberWhere := []string{"1=1"}
	if q.Q != "" && entitySearch {
		memberWhere = append(memberWhere, `name LIKE ? ESCAPE '\'`)
		args = append(args, likeLiteral(q.Q))
	}
	if q.Q != "" && yearSearch {
		term := strings.TrimSpace(strings.ReplaceAll(q.Q, "-", "–"))
		if parts := strings.Split(term, "–"); len(parts) == 2 {
			term = strings.TrimSpace(parts[0]) + "–" + strings.TrimSpace(parts[1])
		}
		search := `value LIKE ? ESCAPE '\'`
		args = append(args, likeLiteral(term))
		if year, err := strconv.Atoi(term); err == nil && year >= 1 && year <= 9999 {
			// Match every member of the containing decade, not just this year.
			search = `(` + search + ` OR (year>0 AND year/10=?))`
			args = append(args, year/10)
		}
		memberWhere = append(memberWhere, search)
	}
	if q.Q != "" && sourceSearch {
		// Source path/name searches include empty registered sources.
		search := `(name LIKE ? ESCAPE '\' OR value IN (SELECT id FROM mounts WHERE target LIKE ? ESCAPE '\'))`
		args = append(args, likeLiteral(q.Q), likeLiteral(q.Q))
		if q.Field == "all" {
			search = `(` + search + ` OR id IN (SELECT id FROM base WHERE title LIKE ? ESCAPE '\' OR artist LIKE ? ESCAPE '\' OR album_artist LIKE ? ESCAPE '\' OR album LIKE ? ESCAPE '\' OR genre LIKE ? ESCAPE '\' OR composer LIKE ? ESCAPE '\' OR CAST(year AS TEXT) LIKE ? ESCAPE '\' OR rel_path LIKE ? ESCAPE '\'))`
			for i := 0; i < 8; i++ {
				args = append(args, likeLiteral(q.Q))
			}
		}
		memberWhere = append(memberWhere, search)
	}
	if sources && (q.Artist != nil || q.Album != nil || q.Genre != nil || q.Composer != nil || q.Year != nil || q.YearFrom != nil || q.YearTo != nil || (q.Q != "" && !sourceSearch)) {
		memberWhere = append(memberWhere, "id IS NOT NULL")
	}
	if q.Initial != "" {
		memberWhere = append(memberWhere, `library_initial(name)=?`)
		args = append(args, q.Initial)
	}
	cte := base + raw + `), members AS (SELECT * FROM raw WHERE ` + strings.Join(memberWhere, " AND ") + `), entities AS (
		SELECT kind,name,value,entity_artist,entity_album,entity_mount,entity_dir,track_id,
		COALESCE(MAX(year),0) AS year,COALESCE(MAX(NULLIF(cover_hash,'')),'') AS cover_hash,
		COUNT(id) AS track_count,COUNT(DISTINCT CASE WHEN id IS NOT NULL THEN json_array(browse_artist,album) END) AS album_count,
		COALESCE(SUM(duration_ms),0) AS duration_ms,COALESCE(MAX(added_at),0) AS added_at
		FROM members GROUP BY kind,name,value,entity_artist,entity_album,entity_mount,entity_dir,track_id)`
	return cte, args
}

func entityOrder(q BrowseQuery) string {
	key := map[string]string{"name": "library_sort(name)", "year": "year", "added": "added_at", "duration": "duration_ms"}[q.Sort]
	if q.Category == "years" && (q.Sort == "name" || q.Sort == "year") {
		// MAX(year) orders disjoint decades numerically, including short years.
		key = "year"
	}
	return key + " " + q.Direction + `,library_sort(name),name,entity_artist,entity_album,entity_mount,entity_dir,kind,value`
}

// Browse returns one server-side page and snapshot-consistent counts. Only the
// requested page's Track objects are loaded; no full-catalog sort occurs in Go.
func (s *Service) Browse(query BrowseQuery) (BrowsePage, error) {
	q, err := normalizeBrowse(query)
	page := BrowsePage{Category: q.Category, Page: q.Page, PageSize: q.PageSize, Offset: q.Offset, Items: []BrowseItem{}}
	if err != nil {
		return page, err
	}
	cte, args := browseSQL(q, false)
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(cte+` SELECT (SELECT COUNT(*) FROM entities),(SELECT COUNT(DISTINCT id) FROM members)`, args...).Scan(&page.Total, &page.TotalTracks); err != nil {
		return page, err
	}
	pageArgs := append(append([]any{}, args...), q.PageSize, q.Offset)
	rows, err := tx.Query(cte+` SELECT kind,name,value,entity_artist,entity_album,year,cover_hash,track_count,album_count,duration_ms,entity_mount,entity_dir,library_initial(name),track_id FROM entities ORDER BY `+entityOrder(q)+` LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return page, err
	}
	trackIDs := []any{}
	for rows.Next() {
		var item BrowseItem
		var trackID string
		if err := rows.Scan(&item.Kind, &item.Name, &item.Value, &item.Artist, &item.Album, &item.Year, &item.CoverHash, &item.TrackCount, &item.AlbumCount, &item.DurationMS, &item.MountID, &item.Directory, &item.Initial, &trackID); err != nil {
			rows.Close()
			return page, err
		}
		if item.Kind == "year" && item.Year > 0 {
			item.YearFrom = max(1, item.Year/10*10)
			item.YearTo = item.Year/10*10 + 9
		}
		if item.Name == "" {
			item.Name = map[string]string{"artist": "未知艺术家", "album": "未知专辑", "genre": "未知风格", "composer": "未知作曲家", "year": "未知年份", "track": "未知曲目"}[item.Kind]
		}
		if trackID != "" {
			trackIDs = append(trackIDs, trackID)
		}
		page.Items = append(page.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(trackIDs) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(trackIDs)), ",")
		rows, err = tx.Query(`SELECT `+trackCols+` FROM tracks WHERE id IN (`+marks+`)`, trackIDs...)
		if err != nil {
			return page, err
		}
		byID := map[string]*Track{}
		for rows.Next() {
			t, err := scanTrack(rows)
			if err != nil {
				rows.Close()
				return page, err
			}
			byID[t.ID] = &t
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, err
		}
		for i := range page.Items {
			if page.Items[i].Kind == "track" {
				page.Items[i].Track = byID[page.Items[i].Value]
			}
		}
	}
	return page, tx.Commit()
}

// SelectTracks returns all matching decodable IDs, independent of Page. Category
// controls search/initial semantics, not playability. More than 10000 is an error.
// Folder selection is direct-only unless Recursive is true.
func (s *Service) SelectTracks(query BrowseQuery) ([]string, error) {
	q, err := normalizeBrowse(query)
	if err != nil {
		return nil, err
	}
	cte, args := browseSQL(q, true)
	key := map[string]string{"name": "library_sort(t.title)", "year": "t.year", "added": "t.added_at", "duration": "t.duration_ms"}[q.Sort]
	rows, err := s.db.Query(cte+` SELECT t.id FROM tracks t WHERE t.decodable=1 AND t.id IN (SELECT id FROM members)
		ORDER BY `+key+` `+q.Direction+`,library_sort(t.title),t.title,t.artist,t.album,t.mount_id,library_dir(t.rel_path),t.id LIMIT 10001`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		if len(ids) > 10000 {
			return nil, fmt.Errorf("选择超过10000首曲目，请缩小筛选范围")
		}
	}
	return ids, rows.Err()
}
