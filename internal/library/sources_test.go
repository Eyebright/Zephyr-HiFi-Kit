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
	if _, err := s.AddMount(AddMountRequest{Kind: "local", Path: root}); err == nil {
		t.Fatal("duplicate local source accepted")
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
