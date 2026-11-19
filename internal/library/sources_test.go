package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hifihub/server/internal/catalog"
)

func TestLocalSourceLifecycleNeverCallsSMBOrDeletesFiles(t *testing.T) {
	run := &fakeRunner{}
	s, _ := newService(t, run, t.TempDir())
	root := t.TempDir()
	file := filepath.Join(root, "song.wav")
	buildWAV(t, file)
	addWAVTags(t, file, [2]string{"ICRD", "2005-02-03"}, [2]string{"ITRK", "3/10"}, [2]string{"IMUS", "Bach, Johann Sebastian"})
	m, err := s.AddMount(AddMountRequest{Kind: "local", Path: root, Username: "ignored", Password: "not-stored"})
	if err != nil || m.Kind != "local" || m.Host != "localhost" || m.Status != StatusUnmounted || m.Password != "" {
		t.Fatalf("local source: %+v %v", m, err)
	}
	if reused, err := s.AddMount(AddMountRequest{Kind: "local", Path: root, Share: "another label"}); err != nil || reused.ID != m.ID || !reused.Reused || reused.Share != m.Share {
		t.Fatalf("duplicate local source must reuse the existing source: %+v %v", reused, err)
	}
	if _, err := s.Scan(context.Background(), m.ID); !errors.Is(err, ErrNotMounted) {
		t.Fatalf("unmounted scan: %v", err)
	}
	if tested, err := s.TestMount(context.Background(), m.ID); err != nil || tested.Status != StatusUnmounted {
		t.Fatalf("read-only test: %+v %v", tested, err)
	}
	m, err = s.Mount(context.Background(), m.ID)
	if err != nil || m.Status != StatusMounted {
		t.Fatalf("mount: %+v %v", m, err)
	}
	if n, err := s.Scan(context.Background(), m.ID); err != nil || n != 1 {
		t.Fatalf("scan: %d %v", n, err)
	}
	before, err := s.ListTracks(m.ID)
	if err != nil || len(before) != 1 || before[0].Year != 2005 || before[0].TrackNumber != 3 || before[0].Composer != "Bach, Johann Sebastian" {
		t.Fatalf("extended scan fields: %+v %v", before, err)
	}
	m, err = s.UpdateMount(m.ID, AddMountRequest{Share: "重命名音乐源"})
	if err != nil || m.Share != "重命名音乐源" || m.Kind != "local" {
		t.Fatalf("rename: %+v %v", m, err)
	}
	if _, err := s.Scan(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ListTracks(m.ID)
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].AddedAt != before[0].AddedAt {
		t.Fatalf("rename changed identity: %+v %+v", before, after)
	}
	if _, err := s.UpdateMount(m.ID, AddMountRequest{Kind: "local", Path: t.TempDir()}); err == nil {
		t.Fatal("physical source reassignment accepted")
	}
	m, err = s.Unmount(context.Background(), m.ID)
	if err != nil || m.Status != StatusUnmounted {
		t.Fatalf("unmount: %+v %v", m, err)
	}
	if _, err := s.Scan(context.Background(), m.ID); !errors.Is(err, ErrNotMounted) {
		t.Fatalf("unmounted rescan: %v", err)
	}
	kept, _ := s.ListTracks(m.ID)
	if !reflect.DeepEqual(after, kept) {
		t.Fatal("rejected scan changed catalog")
	}
	if _, err := s.Mount(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMount(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("original file removed: %v", err)
	}
	if _, err := s.GetTrack(before[0].ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("source catalog not removed: %v", err)
	}
	if len(run.calls) != 0 {
		t.Fatalf("local operation called SMB: %v", run.calls)
	}
}

func addMountedLocalSource(t *testing.T, s *Service, root string) Mount {
	t.Helper()
	m, err := s.AddMount(AddMountRequest{Kind: "local", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	m, err = s.Mount(context.Background(), m.ID)
	if err != nil || m.Status != StatusMounted {
		t.Fatalf("mount local source: %+v %v", m, err)
	}
	return m
}

func assertScanChanges(t *testing.T, s *Service, id string, full bool, want ScanResult) {
	t.Helper()
	start := time.Now().Unix()
	got, err := s.ScanChanges(context.Background(), id, full)
	if err != nil || got != want {
		t.Fatalf("ScanChanges(full=%v): %+v %v, want %+v", full, got, err, want)
	}
	m, err := s.GetMount(id)
	if err != nil || m.ScanResult != want || m.LastScan < start || m.LastScan > time.Now().Unix() {
		t.Fatalf("successful scan statistics not persisted: %+v %v", m, err)
	}
}

func sourceTracksByPath(t *testing.T, s *Service, id string) map[string]Track {
	t.Helper()
	tracks, err := s.ListTracks(id)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]Track, len(tracks))
	for _, tr := range tracks {
		out[tr.RelPath] = tr
	}
	return out
}

func TestLocalScanChangesIncrementalAndFull(t *testing.T) {
	run := &fakeRunner{}
	s, st := newService(t, run, t.TempDir())
	root := t.TempDir()
	writeTagged := func(name, title string, mtime time.Time) {
		t.Helper()
		p := filepath.Join(root, name)
		buildWAV(t, p)
		addWAVTags(t, p, [2]string{"INAM", title})
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	mtime := time.Unix(1700000000, 123456700)
	writeTagged("same-size.wav", "Original", mtime)
	writeTagged("size-change.wav", "Original", mtime)
	writeTagged("removed.wav", "Original", mtime)
	m := addMountedLocalSource(t, s, root)
	assertScanChanges(t, s, m.ID, false, ScanResult{Count: 3, Added: 3})
	for _, tr := range sourceTracksByPath(t, s, m.ID) {
		info, err := os.Stat(tr.Path)
		if err != nil || tr.MtimeNS == 0 || tr.MtimeNS != info.ModTime().UnixNano() {
			t.Fatalf("file mtime not persisted: %+v %v", tr, err)
		}
	}
	// Deliberately stale catalog metadata proves an update scan does not re-probe
	// unchanged files or rewrite even their ScannedAt/AddedAt fields.
	if _, err := st.DB.Exec(`UPDATE tracks SET title='catalog-only',scanned_at=1,added_at=42 WHERE mount_id=?`, m.ID); err != nil {
		t.Fatal(err)
	}
	before := sourceTracksByPath(t, s, m.ID)
	assertScanChanges(t, s, m.ID, false, ScanResult{Count: 3, Unchanged: 3})
	if after := sourceTracksByPath(t, s, m.ID); !reflect.DeepEqual(before, after) {
		t.Fatalf("unchanged files were rewritten: before=%+v after=%+v", before, after)
	}
	// Changing only mtime (not size) must invalidate cached metadata.
	writeTagged("same-size.wav", "Modified", mtime.Add(2*time.Second))
	info, err := os.Stat(filepath.Join(root, "same-size.wav"))
	if err != nil || info.Size() != before["same-size.wav"].Size || info.ModTime().UnixNano() == before["same-size.wav"].MtimeNS {
		t.Fatalf("same-size fixture does not isolate mtime: %v %v", info, err)
	}
	// Changing size with the exact old mtime must also invalidate the cache.
	writeTagged("size-change.wav", "A much longer title", time.Unix(0, before["size-change.wav"].MtimeNS))
	info, err = os.Stat(filepath.Join(root, "size-change.wav"))
	if err != nil || info.Size() == before["size-change.wav"].Size || info.ModTime().UnixNano() != before["size-change.wav"].MtimeNS {
		t.Fatalf("size-change fixture does not isolate size: %v %v", info, err)
	}
	if err := os.Remove(filepath.Join(root, "removed.wav")); err != nil {
		t.Fatal(err)
	}
	writeTagged("new.wav", "New song", mtime)
	assertScanChanges(t, s, m.ID, false, ScanResult{Count: 3, Added: 1, Updated: 2, Removed: 1})
	after := sourceTracksByPath(t, s, m.ID)
	for name, title := range map[string]string{"same-size.wav": "Modified", "size-change.wav": "A much longer title"} {
		got := after[name]
		if got.Title != title || got.ID != before[name].ID || got.AddedAt != before[name].AddedAt || got.ScannedAt <= 1 {
			t.Fatalf("updated metadata or identity wrong: %+v", got)
		}
	}
	if _, ok := after["removed.wav"]; ok {
		t.Fatal("removed file still indexed")
	}
	if after["new.wav"].ID == "" || after["new.wav"].Title != "New song" {
		t.Fatalf("new file missing: %+v", after)
	}
	// Legacy rows with unknown mtime must be read even when their size matches.
	if _, err := st.DB.Exec(`UPDATE tracks SET mtime_ns=0,title='legacy-stale' WHERE id=?`, after["same-size.wav"].ID); err != nil {
		t.Fatal(err)
	}
	assertScanChanges(t, s, m.ID, false, ScanResult{Count: 3, Updated: 1, Unchanged: 2})
	legacy := sourceTracksByPath(t, s, m.ID)["same-size.wav"]
	if legacy.Title != "Modified" || legacy.MtimeNS == 0 || legacy.ID != after["same-size.wav"].ID || legacy.AddedAt != after["same-size.wav"].AddedAt {
		t.Fatalf("legacy mtime did not force refresh: %+v", legacy)
	}
	if _, err := st.DB.Exec(`UPDATE tracks SET title='stale-full',scanned_at=1 WHERE mount_id=?`, m.ID); err != nil {
		t.Fatal(err)
	}
	fullBefore := sourceTracksByPath(t, s, m.ID)
	assertScanChanges(t, s, m.ID, true, ScanResult{Count: 3, Updated: 3})
	for name, tr := range sourceTracksByPath(t, s, m.ID) {
		old := fullBefore[name]
		if tr.Title != after[name].Title || tr.ID != old.ID || tr.AddedAt != old.AddedAt || tr.MtimeNS != old.MtimeNS || tr.ScannedAt <= 1 {
			t.Fatalf("full scan did not repair metadata while preserving identity: old=%+v new=%+v", old, tr)
		}
	}
	if len(run.calls) != 0 {
		t.Fatalf("local scans invoked SMB backend: %v", run.calls)
	}
}

func TestArchiveSourceAndReAddPreserveIndex(t *testing.T) {
	for _, kind := range []string{"local", "smb"} {
		t.Run(kind, func(t *testing.T) {
			run := &fakeRunner{}
			s, st := newService(t, run, t.TempDir())
			root := t.TempDir()
			file := filepath.Join(root, "song.wav")
			buildWAV(t, file)
			var m Mount
			var req AddMountRequest
			if kind == "local" {
				m = addMountedLocalSource(t, s, root)
				req = AddMountRequest{Kind: "local", Path: root}
			} else {
				// The fake CIFS manager uses a temp directory, never a real UNC share.
				m = insertMounted(t, s, root)
				if _, err := st.DB.Exec(`UPDATE mounts SET username='synthetic-user',password='synthetic-password',domain='synthetic-domain',mounted_at=1 WHERE id=?`, m.ID); err != nil {
					t.Fatal(err)
				}
				req = AddMountRequest{Host: "NAS", Share: "music", Username: "new-user", Password: "synthetic-new-password", Domain: "new-domain"}
			}
			assertScanChanges(t, s, m.ID, false, ScanResult{Count: 1, Added: 1})
			before := sourceTracksByPath(t, s, m.ID)
			previous, err := s.GetMount(m.ID)
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.CreatePlaylist("archived source references")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.AddPlaylistItems(p.ID, []string{before["song.wav"].ID}); err != nil {
				t.Fatal(err)
			}
			run.calls = nil
			archived, err := s.ArchiveMount(context.Background(), m.ID)
			if err != nil || archived.ID != m.ID || archived.Status != StatusArchived || archived.Username != "" || archived.Password != "" || archived.Domain != "" || archived.MountedAt != 0 {
				t.Fatalf("archive: %+v %v", archived, err)
			}
			if archived.LastScan != previous.LastScan || archived.ScanResult != previous.ScanResult || !reflect.DeepEqual(before, sourceTracksByPath(t, s, m.ID)) {
				t.Fatal("archive changed index or previous scan statistics")
			}
			if kind == "smb" {
				if !reflect.DeepEqual(run.calls, []string{"umount " + root}) {
					t.Fatalf("archive must unmount SMB: %v", run.calls)
				}
			} else if len(run.calls) != 0 {
				t.Fatalf("local archive invoked SMB: %v", run.calls)
			}
			if _, err := s.PlaybackPath(before["song.wav"].ID); !errors.Is(err, ErrNotMounted) {
				t.Fatalf("archived source remained playable: %v", err)
			}
			if _, err := s.ScanChanges(context.Background(), m.ID, false); !errors.Is(err, ErrNotMounted) {
				t.Fatalf("archived source remained scannable: %v", err)
			}
			reused, err := s.AddMount(req)
			if err != nil || !reused.Reused || reused.ID != m.ID || reused.Target != previous.Target || reused.CreatedAt != previous.CreatedAt || reused.Status != StatusUnmounted {
				t.Fatalf("archive re-add did not reuse source: %+v %v", reused, err)
			}
			if reused.Username != req.Username || reused.Password != req.Password || reused.Domain != req.Domain {
				t.Fatal("re-add did not replace archived credentials")
			}
			if reused.LastScan != previous.LastScan || reused.ScanResult != previous.ScanResult || !reflect.DeepEqual(before, sourceTracksByPath(t, s, m.ID)) {
				t.Fatal("re-add changed index, mtime or previous scan statistics")
			}
			if _, err := s.Mount(context.Background(), m.ID); err != nil {
				t.Fatal(err)
			}
			assertScanChanges(t, s, m.ID, false, ScanResult{Count: 1, Unchanged: 1})
			if !reflect.DeepEqual(before, sourceTracksByPath(t, s, m.ID)) {
				t.Fatal("re-mounted source lost unchanged track identity or metadata")
			}
			playlist, err := s.GetPlaylist(p.ID)
			if err != nil || len(playlist.Items) != 1 || playlist.Items[0].Missing || playlist.Items[0].TrackID != before["song.wav"].ID {
				t.Fatalf("playlist reference lost: %+v %v", playlist, err)
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("original WAV removed: %v", err)
			}
		})
	}
}

func TestLocalSourceValidationAndErrors(t *testing.T) {
	s, _ := newService(t, &fakeRunner{}, t.TempDir())
	file := filepath.Join(t.TempDir(), "file.wav")
	buildWAV(t, file)
	for _, req := range []AddMountRequest{
		{Kind: "ftp", Path: t.TempDir()}, {Kind: "local", Path: "relative"}, {Kind: "local", Path: file},
		{Kind: "local", Path: filepath.Join(t.TempDir(), "missing")}, {Kind: "local", Path: t.TempDir(), Share: "bad\nname"},
	} {
		if _, err := s.AddMount(req); err == nil {
			t.Fatalf("invalid request accepted: %+v", req)
		}
	}
	root := t.TempDir()
	m, err := s.AddMount(AddMountRequest{Kind: "local", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	m, err = s.Mount(context.Background(), m.ID)
	if err != nil || m.Status != StatusError {
		t.Fatalf("missing mount should record error: %+v %v", m, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("local mount recreated a missing directory: %v", err)
	}
	if _, err := s.TestMount(context.Background(), m.ID); err == nil {
		t.Fatal("missing directory test succeeded")
	}
}

func TestMountUpdatePasswordPreservationAndSafeSMBTest(t *testing.T) {
	run := &fakeRunner{}
	s, _ := newService(t, run, t.TempDir())
	m, err := s.AddMount(AddMountRequest{Host: "nas", Share: "music", Username: "user", Password: "secret"})
	if err != nil || m.Kind != "smb" {
		t.Fatalf("default kind: %+v %v", m, err)
	}
	m, err = s.UpdateMount(m.ID, AddMountRequest{Host: "nas", Share: "music", Username: "new-user"})
	if err != nil || m.Password != "secret" || m.Username != "new-user" {
		t.Fatalf("preserve password: %+v %v", m, err)
	}
	m, err = s.UpdateMount(m.ID, AddMountRequest{Host: "nas", Share: "music", Username: "new-user", Password: "replacement"})
	if err != nil || m.Password != "replacement" {
		t.Fatalf("replace password: %+v %v", m, err)
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "replacement") || strings.Contains(string(b), "password") {
		t.Fatalf("credential exposed: %s", b)
	}
	if _, err := s.TestMount(context.Background(), m.ID); !errors.Is(err, ErrNotMounted) {
		t.Fatalf("unmounted SMB test: %v", err)
	}
	if len(run.calls) != 0 {
		t.Fatalf("test changed SMB state: %v", run.calls)
	}
	if _, err := s.UpdateMount(m.ID, AddMountRequest{Host: "other", Share: "music"}); err == nil {
		t.Fatal("source reassigned")
	}
	if _, err := s.db.Exec(`UPDATE mounts SET status='mounted' WHERE id=?`, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateMount(m.ID, AddMountRequest{Host: "nas", Share: "music", Username: "changed"}); err == nil {
		t.Fatal("mounted SMB credentials changed")
	}
}

func TestScanDoesNotFollowSymlinks(t *testing.T) {
	run := &fakeRunner{}
	s, _ := newService(t, run, t.TempDir())
	root, outside := t.TempDir(), t.TempDir()
	buildWAV(t, filepath.Join(root, "inside.wav"))
	buildWAV(t, filepath.Join(outside, "outside.wav"))
	if err := os.Symlink(filepath.Join(outside, "outside.wav"), filepath.Join(root, "escape.wav")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape-dir")); err != nil {
		t.Fatal(err)
	}
	m, err := s.AddMount(AddMountRequest{Kind: "local", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Mount(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Scan(context.Background(), m.ID); err != nil || n != 1 {
		t.Fatalf("scan followed symlinks: %d %v", n, err)
	}
	tracks, _ := s.ListTracks(m.ID)
	if tracks[0].RelPath != "inside.wav" {
		t.Fatalf("outside track: %+v", tracks)
	}
}
