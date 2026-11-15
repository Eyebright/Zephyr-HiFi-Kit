package library

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hifihub/server/internal/mount"
	"hifihub/server/internal/store"
)

type fakeRunner struct {
	calls []string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
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
	if !strings.HasPrefix(m.Target, "/mnt/zephyr/") {
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
	if wav.Artist != "Artist" || wav.Album != "Album" {
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
	// GetTrack round-trips and rescan replaces (idempotent count).
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
			if tr.Album != "AlbumOnly" || tr.Artist != "" {
				t.Fatalf("two-level meta wrong: %+v", tr)
			}
		}
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
