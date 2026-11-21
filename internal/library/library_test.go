package library

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/mount"
	"hifihub/server/internal/store"
)

type fakeRunner struct {
	calls []string
	err   error
	onRun func(context.Context, string, ...string) (string, error)
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.onRun != nil {
		return f.onRun(ctx, name, args...)
	}
	return "", f.err
}

func newService(t *testing.T, run mount.Runner, root string) (*Service, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	credDir := t.TempDir()
	return New(st, mount.NewManager(run, credDir), root), st
}

func TestAddMountValidatesAndComputesTarget(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	if _, err := s.AddMount(AddMountRequest{Host: "a/b", Share: "music"}); err == nil {
		t.Fatal("expected invalid host rejection")
	}
	m, err := s.AddMount(AddMountRequest{Host: "nas.local", Share: "music", Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("AddMount: %v", err)
	}
	if runtime.GOOS == "windows" {
		if m.Target != "//nas.local/music" {
			t.Fatalf("windows target must be the UNC share: %q", m.Target)
		}
	} else if !strings.HasPrefix(m.Target, "/mnt/zephyr/") {
		t.Fatalf("target not under root: %q", m.Target)
	}
	if m.Status != StatusUnmounted || m.Version != mount.DefaultVersion {
		t.Fatalf("unexpected mount: %+v", m)
	}
	// Password is persisted internally but never serialized.
	got, err := s.GetMount(m.ID)
	if err != nil || got.Password != "p" {
		t.Fatalf("password not stored: %+v %v", got, err)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "p\"") || strings.Contains(string(b), "password") {
		t.Fatalf("password leaked in JSON: %s", b)
	}
}

func TestAddMountReusesSameSourceAcrossPlatforms(t *testing.T) {
	run := &fakeRunner{}
	s, _ := newService(t, run, t.TempDir())
	first, err := s.AddMount(AddMountRequest{Host: "NAS.local", Share: "music", Username: "original", Password: "synthetic-original"})
	if err != nil || first.Reused {
		t.Fatalf("new source: %+v %v", first, err)
	}
	for _, host := range []string{"nas.local", "NAS.LOCAL", " NaS.Local "} {
		got, err := s.AddMount(AddMountRequest{Host: host, Share: "music", Username: "replacement", Password: "synthetic-replacement"})
		if err != nil || !got.Reused {
			t.Fatalf("reuse %q: %+v %v", host, got, err)
		}
		got.Reused = false
		if got != first {
			t.Fatalf("duplicate changed existing configuration: before=%+v after=%+v", first, got)
		}
	}
	for _, req := range []AddMountRequest{
		{Host: "nas.local", Share: "Music"},
		{Host: "nas.local", Share: "other"},
		{Host: "other.local", Share: "music"},
	} {
		got, err := s.AddMount(req)
		if err != nil || got.Reused || got.ID == first.ID {
			t.Fatalf("distinct source must get a new ID: %+v %v", got, err)
		}
	}
	mounts, err := s.ListMounts()
	if err != nil || len(mounts) != 4 || len(run.calls) != 0 {
		t.Fatalf("registration must not mount or create duplicate rows: mounts=%+v calls=%v err=%v", mounts, run.calls, err)
	}
}

func TestSameSourceCaseRules(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name string
		a, b Mount
		want bool
	}{
		{"DNS host folds case", Mount{Kind: "smb", Host: "NAS.local", Share: "music"}, Mount{Kind: "smb", Host: "nas.LOCAL", Share: "music"}, true},
		{"share preserves case", Mount{Kind: "smb", Host: "nas", Share: "Music"}, Mount{Kind: "smb", Host: "nas", Share: "music"}, false},
		{"local display label is not identity", Mount{Kind: "local", Target: root, Share: "old"}, Mount{Kind: "local", Target: root, Share: "new"}, true},
		{"local path preserves case on every OS", Mount{Kind: "local", Target: filepath.Join(root, "Music")}, Mount{Kind: "local", Target: filepath.Join(root, "music")}, false},
		{"different local path", Mount{Kind: "local", Target: filepath.Join(root, "one")}, Mount{Kind: "local", Target: filepath.Join(root, "two")}, false},
		{"kind is part of identity", Mount{Kind: "local", Target: root}, Mount{Kind: "smb", Target: root}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sameSource(test.a, test.b); got != test.want {
				t.Fatalf("sameSource = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMountUnmountLifecycle(t *testing.T) {
	run := &fakeRunner{}
	s, _ := newService(t, run, "/mnt/zephyr")
	m, err := s.AddMount(AddMountRequest{Host: "nas", Share: "music", Username: "u", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m, err = s.Mount(ctx, m.ID)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if m.Status != StatusMounted || m.MountedAt == 0 {
		t.Fatalf("not mounted: %+v", m)
	}
	if len(run.calls) != 1 || !strings.HasPrefix(run.calls[0], "mount -t cifs //nas/music ") {
		t.Fatalf("unexpected mount call: %v", run.calls)
	}
	if !strings.Contains(run.calls[0], "credentials=") {
		t.Fatalf("credential file not referenced: %v", run.calls)
	}
	if strings.Contains(run.calls[0], "secret") {
		t.Fatalf("password on command line: %v", run.calls)
	}
	m, err = s.Unmount(ctx, m.ID)
	if err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	if m.Status != StatusUnmounted {
		t.Fatalf("not unmounted: %+v", m)
	}
	if len(run.calls) != 2 || !strings.HasPrefix(run.calls[1], "umount ") {
		t.Fatalf("unexpected umount call: %v", run.calls)
	}
}

func TestMountErrorRecordsDetail(t *testing.T) {
	run := &fakeRunner{err: os.ErrPermission}
	s, _ := newService(t, run, "/mnt/zephyr")
	m, _ := s.AddMount(AddMountRequest{Host: "nas", Share: "music"})
	m, err := s.Mount(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if m.Status != StatusError || m.Detail == "" {
		t.Fatalf("error not recorded: %+v", m)
	}
}

// insertMounted registers a mount whose target is a real temp directory so Scan
// can walk actual files. It bypasses AddMount's Linux-path validation.
func insertMounted(t *testing.T, s *Service, target string) Mount {
	t.Helper()
	m := Mount{
		ID: "mnt-test", Host: "nas", Share: "music", Target: target,
		Version: mount.DefaultVersion, Status: StatusMounted, CreatedAt: 1,
	}
	_, err := s.db.Exec(`INSERT INTO mounts
		(id,host,share,target,username,password,domain,version,writable,status,detail,created_at,mounted_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Host, m.Share, m.Target, "", "", "", m.Version, 0, m.Status, "", m.CreatedAt, 0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func buildWAV(t *testing.T, path string) {
	t.Helper()
	pcm := make([]byte, 4*2*2) // 4 frames, stereo, 16-bit
	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+len(pcm)))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1)
	binary.LittleEndian.PutUint16(hdr[22:24], 2)
	binary.LittleEndian.PutUint32(hdr[24:28], 44100)
	binary.LittleEndian.PutUint32(hdr[28:32], 44100*4)
	binary.LittleEndian.PutUint16(hdr[32:34], 4)
	binary.LittleEndian.PutUint16(hdr[34:36], 16)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(len(pcm)))
	if err := os.WriteFile(path, append(hdr[:], pcm...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildFLAC(t *testing.T, path string) {
	t.Helper()
	head := make([]byte, 8+18)
	copy(head[0:4], "fLaC")
	head[4] = 0x00 // STREAMINFO, last block
	head[5], head[6], head[7] = 0, 0, 18
	if err := os.WriteFile(path, head, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanCataloguesAudio(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	target := t.TempDir()
	m := insertMounted(t, s, target)

	album := filepath.Join(target, "Artist", "Album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	buildWAV(t, filepath.Join(album, "01 Track.wav"))
	buildFLAC(t, filepath.Join(album, "02 Song.flac"))
	if err := os.WriteFile(filepath.Join(album, "cover.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := s.Scan(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 tracks, got %d", n)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 2 {
		t.Fatalf("ListTracks: %v len=%d", err, len(tracks))
	}
	byTitle := map[string]Track{}
	for _, tr := range tracks {
		byTitle[tr.Title] = tr
	}
	wav, ok := byTitle["01 Track"]
	if !ok {
		t.Fatalf("missing wav track: %+v", tracks)
	}
	if wav.Codec != "pcm_s16le" || wav.Rate != 44100 || wav.Channels != 2 || wav.Bits != 16 || !wav.Decodable {
		t.Fatalf("wav probe wrong: %+v", wav)
	}
	if wav.Artist != "" || wav.Album != "" {
		t.Fatalf("metadata wrong: %+v", wav)
	}
	if wav.RelPath != "Artist/Album/01 Track.wav" {
		t.Fatalf("rel path wrong: %q", wav.RelPath)
	}
	if _, err := os.Stat(wav.Path); err != nil {
		t.Fatalf("absolute path not stored: %v", err)
	}
	flac := byTitle["02 Song"]
	if flac.Codec != "flac" || !flac.Decodable {
		t.Fatalf("flac probe wrong: %+v", flac)
	}
	// GetTrack round-trips and rescan refreshes (idempotent count).
	if _, err := s.GetTrack(wav.ID); err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if n2, err := s.Scan(context.Background(), m.ID); err != nil || n2 != 2 {
		t.Fatalf("rescan: %d %v", n2, err)
	}
}

func TestScanFlatAndTwoLevelMeta(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	target := t.TempDir()
	m := insertMounted(t, s, target)
	buildWAV(t, filepath.Join(target, "loose.wav"))
	sub := filepath.Join(target, "AlbumOnly")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	buildWAV(t, filepath.Join(sub, "song.wav"))
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, _ := s.ListTracks(m.ID)
	for _, tr := range tracks {
		switch tr.Title {
		case "loose":
			if tr.Album != "" || tr.Artist != "" {
				t.Fatalf("flat meta wrong: %+v", tr)
			}
		case "song":
			if tr.Album != "" || tr.Artist != "" {
				t.Fatalf("two-level meta wrong: %+v", tr)
			}
		}
	}
}

func buildFLACCover(t *testing.T, path string, art []byte) {
	t.Helper()
	put32 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint32(b, uint32(v)) }
	pic := put32(nil, 3) // front cover
	pic = put32(pic, len("image/png"))
	pic = append(pic, "image/png"...)
	pic = put32(pic, 0) // description
	pic = put32(pic, 100)
	pic = put32(pic, 100)
	pic = put32(pic, 24)
	pic = put32(pic, 0)
	pic = put32(pic, len(art))
	pic = append(pic, art...)
	block := func(typ byte, last bool, body []byte) []byte {
		h := []byte{typ, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
		if last {
			h[0] |= 0x80
		}
		return append(h, body...)
	}
	buf := []byte("fLaC")
	buf = append(buf, block(0, false, make([]byte, 18))...)
	buf = append(buf, block(6, true, pic)...)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanExtractsAndPrunesCovers(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "/mnt/zephyr")
	target := t.TempDir()
	m := insertMounted(t, s, target)
	album := filepath.Join(target, "Artist", "Album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	art := maintenancePNG(t, 75)
	buildFLACCover(t, filepath.Join(album, "song.flac"), art)
	buildWAV(t, filepath.Join(album, "plain.wav"))

	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	tracks, _ := s.ListTracks(m.ID)
	var hash string
	for _, tr := range tracks {
		if tr.Title == "song" {
			hash = tr.CoverHash
		} else if tr.CoverHash != "" {
			t.Fatalf("wav must not carry a cover: %+v", tr)
		}
	}
	if hash == "" {
		t.Fatalf("flac cover not extracted: %+v", tracks)
	}
	mime, data, err := s.GetCover(hash)
	if err != nil || mime != "image/png" || string(data) != string(art) {
		t.Fatalf("GetCover: %q %v %v", mime, data, err)
	}
	// Rescan refreshes tracks but the content-addressed cover stays single.
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM covers`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("covers after rescan: %d %v", n, err)
	}
	if err := s.DeleteMount(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM covers`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("covers not pruned on delete: %d %v", n, err)
	}
	if _, _, err := s.GetCover(hash); err == nil {
		t.Fatal("cover still present after prune")
	}
}

func TestDeleteMountRemovesTracks(t *testing.T) {
	run := &fakeRunner{}
	s, _ := newService(t, run, "/mnt/zephyr")
	target := t.TempDir()
	m := insertMounted(t, s, target)
	buildWAV(t, filepath.Join(target, "a.wav"))
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMount(context.Background(), m.ID); err != nil {
		t.Fatalf("DeleteMount: %v", err)
	}
	if _, err := s.GetMount(m.ID); err == nil {
		t.Fatal("mount still present")
	}
	if tracks, _ := s.ListTracks(m.ID); len(tracks) != 0 {
		t.Fatalf("tracks not removed: %d", len(tracks))
	}
}

func TestDeleteMountedUnmounts(t *testing.T) {
	run := &fakeRunner{}
	s, _ := newService(t, run, "/mnt/zephyr")
	// Linux-style target so Unmount's absolute-path validation passes on every host.
	m := Mount{ID: "mnt-x", Host: "nas", Share: "music", Target: "/mnt/zephyr/mnt-x",
		Version: mount.DefaultVersion, Status: StatusMounted, CreatedAt: 1}
	if _, err := s.db.Exec(`INSERT INTO mounts
		(id,host,share,target,username,password,domain,version,writable,status,detail,created_at,mounted_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Host, m.Share, m.Target, "", "", "", m.Version, 0, m.Status, "", m.CreatedAt, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMount(context.Background(), m.ID); err != nil {
		t.Fatalf("DeleteMount: %v", err)
	}
	if len(run.calls) != 1 || run.calls[0] != "umount /mnt/zephyr/mnt-x" {
		t.Fatalf("expected umount on delete: %v", run.calls)
	}
}

func addWAVTags(t *testing.T, path string, fields ...[2]string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info := []byte("INFO")
	for _, field := range fields {
		info = append(info, field[0]...)
		info = binary.LittleEndian.AppendUint32(info, uint32(len(field[1])))
		info = append(info, field[1]...)
		if len(field[1])%2 != 0 {
			info = append(info, 0)
		}
	}
	b = append(b, "LIST"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(info)))
	b = append(b, info...)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanPreservesIdentityAndFirstAddedTime(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "/mnt/zephyr")
	target := t.TempDir()
	m := insertMounted(t, s, target)
	for _, name := range []string{"known.wav", "legacy.wav", "removed.wav"} {
		p := filepath.Join(target, name)
		buildWAV(t, p)
		if err := os.Chtimes(p, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now().Unix()
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	end := time.Now().Unix()
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 3 {
		t.Fatalf("initial tracks: %+v, %v", tracks, err)
	}
	before := map[string]Track{}
	for _, tr := range tracks {
		if tr.AddedAt < start || tr.AddedAt > end {
			t.Fatalf("first added time must be discovery time, not file mtime: %+v", tr)
		}
		before[tr.RelPath] = tr
	}
	knownAt := start - 86400
	if _, err := st.DB.Exec(`UPDATE tracks SET added_at=?,scanned_at=1 WHERE id=?`, knownAt, before["known.wav"].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE tracks SET added_at=0,scanned_at=1 WHERE id=?`, before["legacy.wav"].ID); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreatePlaylist("rescan references")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{before["known.wav"].ID, before["legacy.wav"].ID, before["removed.wav"].ID}
	if _, err := s.AddPlaylistItems(p.ID, ids); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(target, "removed.wav")); err != nil {
		t.Fatal(err)
	}
	buildWAV(t, filepath.Join(target, "new.wav"))
	addWAVTags(t, filepath.Join(target, "known.wav"), [2]string{"INAM", "Tagged title"}, [2]string{"IGNR", "Jazz"})
	start = time.Now().Unix()
	if n, err := s.Scan(context.Background(), m.ID); err != nil || n != 3 {
		t.Fatalf("rescan: %d, %v", n, err)
	}
	end = time.Now().Unix()
	after, err := s.ListTracks(m.ID)
	if err != nil || len(after) != 3 {
		t.Fatalf("rescanned tracks: %+v, %v", after, err)
	}
	var newTrack Track
	for _, tr := range after {
		switch tr.RelPath {
		case "known.wav":
			if tr.ID != before[tr.RelPath].ID || tr.AddedAt != knownAt || tr.Title != "Tagged title" || tr.Genre != "Jazz" {
				t.Fatalf("existing identity/time or refreshed metadata: %+v", tr)
			}
		case "legacy.wav":
			if tr.ID != before[tr.RelPath].ID || tr.AddedAt != 0 {
				t.Fatalf("legacy unknown time must remain zero: %+v", tr)
			}
		case "new.wav":
			newTrack = tr
			if tr.ID == "" || tr.AddedAt < start || tr.AddedAt > end {
				t.Fatalf("new file first added time: %+v", tr)
			}
		default:
			t.Fatalf("unexpected path: %+v", tr)
		}
		if tr.RelPath == "legacy.wav" {
			if tr.ScannedAt != 1 {
				t.Fatalf("unchanged track must retain its metadata scan time: %+v", tr)
			}
		} else if tr.ScannedAt < start || tr.ScannedAt > end {
			t.Fatalf("changed track scan time must refresh independently: %+v", tr)
		}
	}
	if newTrack.ID == "" {
		t.Fatal("new track absent")
	}
	if _, err := s.GetTrack(before["removed.wav"].ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("removed file still resolves: %v", err)
	}
	d, err := s.GetPlaylist(p.ID)
	if err != nil || len(d.Items) != 3 {
		t.Fatalf("playlist after rescan: %+v, %v", d, err)
	}
	for i, item := range d.Items {
		if item.TrackID != ids[i] || item.Missing != (i == 2) {
			t.Fatalf("playlist identity/resolution changed: %+v", d.Items)
		}
	}
	if d.Items[0].Title != "Tagged title" {
		t.Fatalf("playlist metadata not refreshed: %+v", d.Items)
	}
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTrack(newTrack.ID)
	if err != nil || got.AddedAt != newTrack.AddedAt {
		t.Fatalf("new file identity/time lost on next scan: %+v, %v", got, err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil || fields["added_at"] == nil || fields["genre"] == nil {
		t.Fatalf("new fields missing from JSON: %s, %v", b, err)
	}
	// A file absent for multiple scans still recovers its playlist reference.
	buildWAV(t, filepath.Join(target, "removed.wav"))
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.GetTrack(before["removed.wav"].ID)
	if err != nil || recovered.AddedAt != before["removed.wav"].AddedAt {
		t.Fatalf("missing-file identity/time lost: %+v %v", recovered, err)
	}
	d, err = s.GetPlaylist(p.ID)
	if err != nil || d.Items[2].Missing || d.Items[2].TrackID != ids[2] {
		t.Fatalf("returned file did not recover playlist: %+v %v", d, err)
	}
}

func TestScanFailurePreservesCatalog(t *testing.T) {
	for _, mode := range []string{"missing root", "canceled", "probe permission failure", "insert failure", "statistics failure"} {
		t.Run(mode, func(t *testing.T) {
			s, st := newService(t, &fakeRunner{}, t.TempDir())
			target := t.TempDir()
			m := addMountedLocalSource(t, s, target)
			buildWAV(t, filepath.Join(target, "a.wav"))
			buildWAV(t, filepath.Join(target, "b.wav"))
			assertScanChanges(t, s, m.ID, false, ScanResult{Count: 2, Added: 2})
			// Make attempted writes observable even when two scans share a second.
			if _, err := st.DB.Exec(`UPDATE mounts SET last_scan=1 WHERE id=?`, m.ID); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(target, "a.wav")); err != nil {
				t.Fatal(err)
			}
			addWAVTags(t, filepath.Join(target, "b.wav"), [2]string{"INAM", "Changed"})
			buildWAV(t, filepath.Join(target, "c.wav"))
			ctx := context.Background()
			switch mode {
			case "missing root":
				if _, err := st.DB.Exec(`UPDATE mounts SET target=? WHERE id=?`, filepath.Join(target, "missing"), m.ID); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "probe permission failure":
				p := filepath.Join(target, "b.wav")
				if err := os.Chmod(p, 0); err != nil {
					t.Skipf("cannot deny file reads: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
				if f, err := os.Open(p); err == nil {
					f.Close()
					t.Skip("host permits reads despite chmod (Windows or privileged user)")
				}
			case "insert failure":
				if _, err := st.DB.Exec(`CREATE TRIGGER fail_rescan BEFORE INSERT ON tracks
					WHEN NEW.rel_path='b.wav' BEGIN SELECT RAISE(ABORT, 'test insertion failure'); END`); err != nil {
					t.Fatal(err)
				}
			case "statistics failure":
				if _, err := st.DB.Exec(`CREATE TRIGGER fail_scan_statistics BEFORE UPDATE OF last_scan ON mounts
					BEGIN SELECT RAISE(ABORT, 'test statistics failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotSourceCatalog(t, s, m.ID)
			result, err := s.ScanChanges(ctx, m.ID, false)
			if err == nil || result != (ScanResult{}) {
				t.Fatalf("expected failed scan with no partial result: %+v, %v", result, err)
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error lost: %v", err)
			}
			assertSourceCatalogUnchanged(t, s, m.ID, before)
		})
	}
}

// Include cached covers and historical identities, not only visible track rows.
// Every query is against newService's disposable test database.
type sourceCatalogSnapshot struct {
	Mount      Mount
	Tracks     []Track
	Identities map[string]backupTrackIdentity
	Covers     map[string]string
}

func snapshotSourceCatalog(t *testing.T, s *Service, id string) sourceCatalogSnapshot {
	t.Helper()
	var out sourceCatalogSnapshot
	var err error
	out.Mount, err = s.GetMount(id)
	if err != nil {
		t.Fatal(err)
	}
	out.Tracks, err = s.ListTracks(id)
	if err != nil {
		t.Fatal(err)
	}
	out.Identities = make(map[string]backupTrackIdentity)
	rows, err := s.db.Query(`SELECT id,mount_id,rel_path,added_at FROM track_identities WHERE mount_id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v backupTrackIdentity
		if err := rows.Scan(&v.ID, &v.MountID, &v.RelPath, &v.AddedAt); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		out.Identities[v.RelPath] = v
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		t.Fatal(rowErr)
	}
	rows, err = s.db.Query(`SELECT hash,mime,data,created_at FROM covers`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out.Covers = make(map[string]string)
	for rows.Next() {
		var hash, mime string
		var data []byte
		var createdAt int64
		if err := rows.Scan(&hash, &mime, &data, &createdAt); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal([]any{mime, data, createdAt})
		if err != nil {
			t.Fatal(err)
		}
		out.Covers[hash] = string(encoded)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertSourceCatalogUnchanged(t *testing.T, s *Service, id string, before sourceCatalogSnapshot) {
	t.Helper()
	after := snapshotSourceCatalog(t, s, id)
	if before.Mount != after.Mount {
		t.Error("failed operation changed mount state or last successful scan statistics")
	}
	if !reflect.DeepEqual(before.Tracks, after.Tracks) {
		t.Error("failed operation changed track rows")
	}
	if !reflect.DeepEqual(before.Identities, after.Identities) {
		t.Error("failed operation changed historical track identities")
	}
	if !reflect.DeepEqual(before.Covers, after.Covers) {
		t.Errorf("failed operation changed cover cache: %d rows before, %d rows after", len(before.Covers), len(after.Covers))
	}
}

func TestSMBScanCheckFailurePreservesCatalog(t *testing.T) {
	for _, mode := range []string{"disconnected before scan", "disconnected after scan", "canceled after scan", "file removed after scan"} {
		t.Run(mode, func(t *testing.T) {
			run := &fakeRunner{}
			s, st := newService(t, run, t.TempDir())
			target := t.TempDir()
			m := insertMounted(t, s, target)
			p := filepath.Join(target, "song.wav")
			buildWAV(t, p)
			assertScanChanges(t, s, m.ID, false, ScanResult{Count: 1, Added: 1})
			wantCall := "findmnt --noheadings --types cifs --mountpoint " + target
			if !reflect.DeepEqual(run.calls, []string{wantCall, wantCall}) {
				t.Fatalf("SMB scans must check before and after walking: %v", run.calls)
			}
			if _, err := st.DB.Exec(`UPDATE mounts SET last_scan=1 WHERE id=?`, m.ID); err != nil {
				t.Fatal(err)
			}
			before := snapshotSourceCatalog(t, s, m.ID)
			if mode == "disconnected before scan" {
				// An externally unmounted share looks like an empty local directory.
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			} else {
				addWAVTags(t, p, [2]string{"INAM", "Modified before failure"})
				buildWAV(t, filepath.Join(target, "new.wav"))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			disconnected := errors.New("synthetic disconnected share")
			checks := 0
			run.onRun = func(_ context.Context, name string, args ...string) (string, error) {
				if name+" "+strings.Join(args, " ") != wantCall {
					t.Fatalf("unexpected SMB scan command: %s %v", name, args)
				}
				checks++
				if mode == "disconnected before scan" || (mode == "disconnected after scan" && checks == 2) {
					return "", disconnected
				}
				if checks == 2 {
					switch mode {
					case "canceled after scan":
						cancel()
					case "file removed after scan":
						if err := os.Remove(p); err != nil {
							t.Fatal(err)
						}
					}
				}
				return "", nil
			}
			result, err := s.ScanChanges(ctx, m.ID, false)
			if err == nil || result != (ScanResult{}) {
				t.Fatalf("failed scan returned partial success: %+v %v", result, err)
			}
			wantChecks := 2
			if mode == "disconnected before scan" {
				wantChecks = 1
			}
			if checks != wantChecks {
				t.Fatalf("check calls = %d, want %d", checks, wantChecks)
			}
			if strings.HasPrefix(mode, "disconnected") && !errors.Is(err, disconnected) {
				t.Fatalf("connection error lost: %v", err)
			}
			if mode == "canceled after scan" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error lost: %v", err)
			}
			assertSourceCatalogUnchanged(t, s, m.ID, before)
		})
	}
}

func TestFailedScanDoesNotWriteNewCovers(t *testing.T) {
	for _, mode := range []string{"connection failure", "canceled", "cover write failure", "transaction failure"} {
		t.Run(mode, func(t *testing.T) {
			run := &fakeRunner{}
			s, st := newService(t, run, t.TempDir())
			target := t.TempDir()
			m := insertMounted(t, s, target)
			buildWAV(t, filepath.Join(target, "known.wav"))
			assertScanChanges(t, s, m.ID, false, ScanResult{Count: 1, Added: 1})
			before := snapshotSourceCatalog(t, s, m.ID)
			buildFLACCover(t, filepath.Join(target, "new-cover.flac"), maintenancePNG(t, 93))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			checks := 0
			run.onRun = func(_ context.Context, name string, _ ...string) (string, error) {
				if name != "findmnt" {
					t.Fatalf("unexpected scan command: %s", name)
				}
				checks++
				if checks == 2 {
					switch mode {
					case "connection failure":
						return "", errors.New("synthetic disconnect after cover read")
					case "canceled":
						cancel()
					}
				}
				return "", nil
			}
			if mode == "cover write failure" {
				if _, err := st.DB.Exec(`CREATE TRIGGER fail_cover_insert BEFORE INSERT ON covers
					BEGIN SELECT RAISE(ABORT, 'synthetic cover write failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "transaction failure" {
				if _, err := st.DB.Exec(`CREATE TRIGGER fail_scan_cover BEFORE UPDATE OF last_scan ON mounts
					BEGIN SELECT RAISE(ABORT, 'synthetic commit failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if result, err := s.ScanChanges(ctx, m.ID, false); err == nil || result != (ScanResult{}) {
				t.Fatalf("expected failed scan: %+v %v", result, err)
			}
			assertSourceCatalogUnchanged(t, s, m.ID, before)
		})
	}
}

func TestArchiveUnmountFailurePreservesCatalog(t *testing.T) {
	run := &fakeRunner{}
	s, st := newService(t, run, t.TempDir())
	target := t.TempDir()
	m := insertMounted(t, s, target)
	p := filepath.Join(target, "song.wav")
	buildWAV(t, p)
	assertScanChanges(t, s, m.ID, false, ScanResult{Count: 1, Added: 1})
	if _, err := st.DB.Exec(`UPDATE mounts SET username='synthetic-user',password='synthetic-password',domain='synthetic-domain',last_scan=1 WHERE id=?`, m.ID); err != nil {
		t.Fatal(err)
	}
	before := snapshotSourceCatalog(t, s, m.ID)
	run.calls = nil
	run.err = errors.New("synthetic unmount failure")
	if _, err := s.ArchiveMount(context.Background(), m.ID); !errors.Is(err, run.err) {
		t.Fatalf("archive swallowed unmount failure: %v", err)
	}
	if !reflect.DeepEqual(run.calls, []string{"umount " + target}) {
		t.Fatalf("wrong archive command: %v", run.calls)
	}
	assertSourceCatalogUnchanged(t, s, m.ID, before)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("failed archive removed the original file: %v", err)
	}
}

func TestScanMetadataAndMalformedTagFallback(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, "/mnt/zephyr")
	target := t.TempDir()
	m := insertMounted(t, s, target)
	album := filepath.Join(target, "Folder Artist", "Folder Album")
	if err := os.MkdirAll(album, 0o700); err != nil {
		t.Fatal(err)
	}
	wav := filepath.Join(album, "tagged.wav")
	buildWAV(t, wav)
	addWAVTags(t, wav, [2]string{"INAM", "Tagged WAV\x00"}, [2]string{"IART", "WAV Artist"},
		[2]string{"IPRD", "WAV Album"}, [2]string{"IGNR", " "}, [2]string{"IGNR", "Jazz"}, [2]string{"IGNR", "Rock"})
	partial := filepath.Join(album, "partial.wav")
	buildWAV(t, partial)
	addWAVTags(t, partial, [2]string{"IART", "\x00 "}, [2]string{"IPRD", "Tagged Album"})
	bad := filepath.Join(album, "broken.wav")
	buildWAV(t, bad)
	b, err := os.ReadFile(bad)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, "LIST\x0c\x00\x00\x00INFOIGNR\xff\xff\xff\xff"...)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
	if err := os.WriteFile(bad, b, 0o600); err != nil {
		t.Fatal(err)
	}
	flac := filepath.Join(album, "tagged.flac")
	buildFLACCover(t, flac, make([]byte, 128*1024))
	b, err = os.ReadFile(flac)
	if err != nil {
		t.Fatal(err)
	}
	b[26] &^= 0x80 // the picture is followed by a Vorbis comment block
	comment := binary.LittleEndian.AppendUint32(nil, 0)
	entries := []string{"TITLE=Tagged FLAC", "ARTIST=FLAC Artist", "ALBUM=FLAC Album", "GENRE=", "GENRE=Classical", "GENRE=Rock",
		"ALBUMARTIST=Album Artist", "COMPOSER=Bach, Johann Sebastian", "COMPOSER=Mozart", "DATE=1999-02-03", "DISCNUMBER=2/4", "TRACKNUMBER=7/12"}
	comment = binary.LittleEndian.AppendUint32(comment, uint32(len(entries)))
	for _, entry := range entries {
		comment = binary.LittleEndian.AppendUint32(comment, uint32(len(entry)))
		comment = append(comment, entry...)
	}
	b = append(b, 0x84, byte(len(comment)>>16), byte(len(comment)>>8), byte(len(comment)))
	b = append(b, comment...)
	if err := os.WriteFile(flac, b, 0o600); err != nil {
		t.Fatal(err)
	}
	badFLAC := filepath.Join(album, "broken.flac")
	buildFLAC(t, badFLAC)
	f, err := os.OpenFile(badFLAC, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.Write([]byte{0x84, 0xff, 0xff, 0xff})
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("write malformed FLAC: %v, %v", writeErr, closeErr)
	}
	if n, err := s.Scan(context.Background(), m.ID); err != nil || n != 5 {
		t.Fatalf("scan with tags: %d, %v", n, err)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range tracks {
		wantTitle, wantArtist, wantAlbum, wantGenre := "broken", "", "", ""
		switch filepath.Base(tr.Path) {
		case "tagged.wav":
			wantTitle, wantArtist, wantAlbum, wantGenre = "Tagged WAV", "WAV Artist", "WAV Album", "Jazz; Rock"
		case "tagged.flac":
			wantTitle, wantArtist, wantAlbum, wantGenre = "Tagged FLAC", "FLAC Artist", "FLAC Album", "Classical; Rock"
			if tr.AlbumArtist != "Album Artist" || tr.Composer != "Bach, Johann Sebastian; Mozart" || tr.Year != 1999 || tr.DiscNumber != 2 || tr.TrackNumber != 7 {
				t.Fatalf("extended FLAC scan fields: %+v", tr)
			}
		case "partial.wav":
			wantTitle, wantAlbum = "partial", "Tagged Album"
		}
		if tr.Title != wantTitle || tr.Artist != wantArtist || tr.Album != wantAlbum || tr.Genre != wantGenre || !tr.Decodable {
			t.Fatalf("metadata/fallback or identification wrong: %+v", tr)
		}
	}
}

func TestPlaybackPathDerivesCurrentPathAndRequiresMountedSource(t *testing.T) {
	s, _ := maintenanceTestService(t)
	root := t.TempDir()
	buildWAV(t, filepath.Join(root, "song.wav"))
	m := insertMounted(t, s, root)
	if _, err := s.Scan(t.Context(), m.ID); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks: %+v %v", tracks, err)
	}
	tr := tracks[0]
	maintenanceExec(t, s.db, `UPDATE tracks SET path='/attacker/private.wav' WHERE id=?`, tr.ID)
	p, err := s.PlaybackPath(tr.ID)
	want, resolveErr := filepath.EvalSymlinks(filepath.Join(root, "song.wav"))
	if err != nil || resolveErr != nil || p != want {
		t.Fatalf("playback path: %q %v", p, err)
	}
	for _, rel := range []string{"../escape.wav", "/escape.wav", "C:escape.wav", `\\host\share\escape.wav`, "."} {
		maintenanceExec(t, s.db, `UPDATE tracks SET rel_path=? WHERE id=?`, rel, tr.ID)
		if _, err := s.PlaybackPath(tr.ID); err == nil {
			t.Fatalf("accepted playback relative path %q", rel)
		}
	}
	maintenanceExec(t, s.db, `UPDATE tracks SET rel_path='song.wav' WHERE id=?`, tr.ID)
	maintenanceExec(t, s.db, `UPDATE mounts SET status='unmounted' WHERE id=?`, m.ID)
	if _, err := s.PlaybackPath(tr.ID); !errors.Is(err, ErrNotMounted) {
		t.Fatalf("unmounted playback: %v", err)
	}
}

func TestPlaybackPathRejectsChangedSymlinks(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			s, _ := maintenanceTestService(t)
			root, outside := t.TempDir(), t.TempDir()
			inside := filepath.Join(root, "album")
			if err := os.Mkdir(inside, 0o700); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(inside, "song.wav")
			buildWAV(t, p)
			buildWAV(t, filepath.Join(outside, "song.wav"))
			m := insertMounted(t, s, root)
			if _, err := s.Scan(t.Context(), m.ID); err != nil {
				t.Fatal(err)
			}
			tracks, err := s.ListTracks(m.ID)
			if err != nil || len(tracks) != 1 {
				t.Fatalf("tracks: %+v %v", tracks, err)
			}
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			link, target := p, filepath.Join(outside, "song.wav")
			if directory {
				if err := os.Remove(inside); err != nil {
					t.Fatal(err)
				}
				link, target = inside, outside
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink creation unavailable: %v", err)
			}
			if _, err := s.PlaybackPath(tracks[0].ID); err == nil {
				t.Fatal("changed symlink escaped source at playback")
			}
		})
	}
}

func TestPruneCoversPreservesHistoryReferences(t *testing.T) {
	for _, action := range []string{"rescan", "delete mount"} {
		t.Run(action, func(t *testing.T) {
			s, st := newService(t, &fakeRunner{}, "/mnt/zephyr")
			target := t.TempDir()
			m := insertMounted(t, s, target)
			p := filepath.Join(target, "history.flac")
			buildFLACCover(t, p, maintenancePNG(t, 125))
			if _, err := s.Scan(context.Background(), m.ID); err != nil {
				t.Fatal(err)
			}
			tracks, err := s.ListTracks(m.ID)
			if err != nil || len(tracks) != 1 || tracks[0].CoverHash == "" {
				t.Fatalf("cover track: %+v, %v", tracks, err)
			}
			tr := tracks[0]
			if _, err := st.DB.Exec(`INSERT INTO play_history(id,node_id,track_id,started_at,cover_hash) VALUES('history','node',?,1,?)`, tr.ID, tr.CoverHash); err != nil {
				t.Fatal(err)
			}
			if _, err := st.DB.Exec(`INSERT INTO covers(hash,mime,data,created_at) VALUES('orphan','image/png',?,1)`, []byte("orphan")); err != nil {
				t.Fatal(err)
			}
			if action == "rescan" {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if n, err := s.Scan(context.Background(), m.ID); err != nil || n != 0 {
					t.Fatalf("rescan after removal: %d, %v", n, err)
				}
			} else if err := s.DeleteMount(context.Background(), m.ID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.GetCover(tr.CoverHash); err != nil {
				t.Fatalf("history cover pruned: %v", err)
			}
			if _, _, err := s.GetCover("orphan"); !errors.Is(err, catalog.ErrNotFound) {
				t.Fatalf("orphan not pruned: %v", err)
			}
			if _, err := st.DB.Exec(`DELETE FROM play_history`); err != nil {
				t.Fatal(err)
			}
			s.pruneCovers()
			if _, _, err := s.GetCover(tr.CoverHash); !errors.Is(err, catalog.ErrNotFound) {
				t.Fatalf("unreferenced history cover not pruned: %v", err)
			}
		})
	}
}

func TestScanStoresSidecarCover(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	root := t.TempDir()
	buildWAV(t, filepath.Join(root, "a.wav"))
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "folder.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := s.AddMount(AddMountRequest{Kind: "local", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Mount(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScanChanges(context.Background(), m.ID, true); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 || tracks[0].CoverHash == "" {
		t.Fatalf("tracks=%+v err=%v", tracks, err)
	}
	mime, data, err := s.GetCover(tracks[0].CoverHash)
	if err != nil || mime != "image/png" || len(data) != buf.Len() {
		t.Fatalf("cover mime=%q len=%d err=%v", mime, len(data), err)
	}
}

func TestScanBackfillsCoversForUnchangedTracks(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	root := t.TempDir()
	buildWAV(t, filepath.Join(root, "a.wav"))
	m, err := s.AddMount(AddMountRequest{Kind: "local", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Mount(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScanChanges(context.Background(), m.ID, true); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 || tracks[0].CoverHash != "" {
		t.Fatalf("initial tracks=%+v err=%v", tracks, err)
	}
	// Art arrives after the first scan; the audio file itself never changes.
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(root, "Artwork", "COVER.png")
	if err := os.MkdirAll(filepath.Dir(art), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(art, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := s.ScanChanges(context.Background(), m.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Unchanged != 1 || res.CoversFilled != 1 {
		t.Fatalf("scan result = %+v", res)
	}
	tracks, err = s.ListTracks(m.ID)
	if err != nil || len(tracks) != 1 || tracks[0].CoverHash == "" {
		t.Fatalf("backfilled tracks=%+v err=%v", tracks, err)
	}
	res, err = s.ScanChanges(context.Background(), m.ID, false)
	if err != nil || res.CoversFilled != 0 {
		t.Fatalf("second scan result = %+v err=%v", res, err)
	}
}
