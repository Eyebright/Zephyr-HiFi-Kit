package library

import (
	"context"
	"errors"
	"testing"
)

func TestReconnectRestoresOnlyMountedSources(t *testing.T) {
	run := &fakeRunner{}
	s, st := newService(t, run, t.TempDir())
	m1, err := s.AddMount(AddMountRequest{Host: "nas", Share: "music", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := s.AddMount(AddMountRequest{Host: "nas", Share: "other", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE mounts SET status=? WHERE id=?`, StatusMounted, m1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE mounts SET status=? WHERE id=?`, StatusUnmounted, m2.ID); err != nil {
		t.Fatal(err)
	}
	attempted, restored, failures := s.Reconnect(context.Background())
	if attempted != 1 || restored != 1 || len(failures) != 0 {
		t.Fatalf("attempted=%d restored=%d failures=%v", attempted, restored, failures)
	}
	if len(run.calls) != 1 || run.calls[0] == "" {
		t.Fatalf("mount calls = %v", run.calls)
	}
}

func TestReconnectReportsFailureWithoutSecret(t *testing.T) {
	run := &fakeRunner{err: errors.New("access denied")}
	s, st := newService(t, run, t.TempDir())
	m, err := s.AddMount(AddMountRequest{Host: "nas", Share: "music", Username: "u", Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`UPDATE mounts SET status=? WHERE id=?`, StatusMounted, m.ID); err != nil {
		t.Fatal(err)
	}
	attempted, restored, failures := s.Reconnect(context.Background())
	if attempted != 1 || restored != 0 || len(failures) != 1 {
		t.Fatalf("attempted=%d restored=%d failures=%v", attempted, restored, failures)
	}
	if contains(failures[0], "s3cret") {
		t.Fatal("password leaked into reconnect error")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
