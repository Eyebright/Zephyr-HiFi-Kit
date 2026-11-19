package library

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hifihub/server/internal/store"
)

func TestSettingsProfileInitializationAndRestart(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "legacy"}[configured], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.db")
			st, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			s := New(st, nil, "")
			want := InitialSettingsProfile(configured)
			if p, err := s.InitializeSettingsProfile(configured); err != nil || p != want {
				t.Fatalf("initial: %+v %v", p, err)
			}
			if p, err := s.InitializeSettingsProfile(!configured); err != nil || p != want {
				t.Fatalf("initialization changed existing state: %+v %v", p, err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			st, err = store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			s = New(st, nil, "")
			if p, err := s.InitializeSettingsProfile(true); err != nil || p != want {
				t.Fatalf("restart classified new instance as legacy: %+v %v", p, err)
			}
			p, err := s.CompleteSettingsSetup("  管理员  ")
			if err != nil || p.DisplayName != "管理员" || p.OnboardingStep != 2 || p.OnboardingComplete || p.Legacy {
				t.Fatalf("setup: %+v %v", p, err)
			}
			if p, err := s.UpdateSettingsOnboarding(3, true); err != nil || !p.OnboardingComplete {
				t.Fatalf("complete: %+v %v", p, err)
			}
			if p, err := s.UpdateSettingsOnboarding(1, false); err != nil || p.OnboardingStep != 1 || p.OnboardingComplete {
				t.Fatalf("revisit: %+v %v", p, err)
			}
		})
	}
}

func TestSettingsDisplayNameValidation(t *testing.T) {
	for _, name := range []string{"", " \u3000 ", strings.Repeat("界", 41), "a\x00b", "a\x7fb", "a\u0085b", "\nName", "Name\t", string([]byte{0xff})} {
		if _, err := NormalizeSettingsDisplayName(name); !errors.Is(err, ErrSettingsDisplayName) {
			t.Errorf("accepted invalid name %q: %v", name, err)
		}
	}
	for _, name := range []string{"A", strings.Repeat("界", 40), "Zoë 王", "<script>alert('x')</script>", "\U0001F3B5"} {
		if got, err := NormalizeSettingsDisplayName(" \u3000" + name + " "); err != nil || got != name {
			t.Errorf("valid name %q: %q %v", name, got, err)
		}
	}
}

func TestSettingsProfileCorruptionFailsClosed(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "")
	for _, value := range []string{
		"broken", "null", `{}`, `{"display_name":"","onboarding_step":1}`,
		`{"display_name":"","onboarding_step":0,"onboarding_complete":false,"legacy":false}`,
		`{"display_name":"","onboarding_step":2,"onboarding_complete":false,"legacy":false}`,
		`{"display_name":"A","onboarding_step":2,"onboarding_complete":true,"legacy":false}`,
		`{"display_name":" A ","onboarding_step":2,"onboarding_complete":false,"legacy":false}`,
		`{"display_name":"A\u0000","onboarding_step":2,"onboarding_complete":false,"legacy":false}`,
		`{"display_name":"A","onboarding_step":2,"onboarding_complete":false,"legacy":false,"password":"not-allowed"}`,
		`{"display_name":"A","onboarding_step":2,"onboarding_complete":false,"legacy":false} {}`,
		`{"display_name":"A","onboarding_step":5,"onboarding_complete":false,"legacy":false}`,
		`{"display_name":"A","onboarding_step":3,"onboarding_complete":false,"legacy":false} trailing`,
	} {
		if _, err := st.DB.Exec(`INSERT INTO server_state(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsProfileKey, value); err != nil {
			t.Fatal(err)
		}
		if _, err := s.InitializeSettingsProfile(true); !errors.Is(err, ErrSettingsProfileCorrupt) {
			t.Fatalf("initialized corrupt profile %q: %v", value, err)
		}
		if _, err := s.UpdateSettingsDisplayName("Repair"); !errors.Is(err, ErrSettingsProfileCorrupt) {
			t.Fatalf("overwrote corrupt profile: %v", err)
		}
		var after string
		if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key=?`, settingsProfileKey).Scan(&after); err != nil || after != value {
			t.Fatalf("corrupt state changed: %q %v", after, err)
		}
	}
}

func TestSettingsProfileLegacyFourStepMigration(t *testing.T) {
	for _, tc := range []struct {
		value    string
		step     int
		complete bool
	}{
		{`{"display_name":"A","onboarding_step":4,"onboarding_complete":true,"legacy":false}`, 3, true},
		{`{"display_name":"A","onboarding_step":4,"onboarding_complete":false,"legacy":false}`, 3, false},
		{`{"display_name":"","onboarding_step":4,"onboarding_complete":true,"legacy":true}`, 3, true},
	} {
		p, err := decodeSettingsProfile(tc.value)
		if err != nil || p.OnboardingStep != tc.step || p.OnboardingComplete != tc.complete {
			t.Fatalf("legacy step 4 must fold into step 3: %+v %v", p, err)
		}
	}
	if _, err := (SettingsProfile{DisplayName: "A"}).WithOnboarding(4, false); !errors.Is(err, ErrSettingsOnboarding) {
		t.Fatalf("step 4 must be rejected: %v", err)
	}
	if _, err := (SettingsProfile{DisplayName: "A"}).WithOnboarding(2, true); !errors.Is(err, ErrSettingsOnboarding) {
		t.Fatalf("completion outside step 3 must be rejected: %v", err)
	}
}

func TestSettingsProfileValidationAndRollback(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "")
	if _, err := s.InitializeSettingsProfile(false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSettingsOnboarding(2, false); !errors.Is(err, ErrSettingsOnboarding) {
		t.Fatal("advanced without name", err)
	}
	before, err := s.CompleteSettingsSetup("Admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		step     int
		complete bool
	}{{0, false}, {4, false}, {5, false}, {1, true}, {2, true}} {
		if _, err := s.UpdateSettingsOnboarding(test.step, test.complete); !errors.Is(err, ErrSettingsOnboarding) {
			t.Fatalf("invalid onboarding accepted: %+v %v", test, err)
		}
	}
	if _, err := s.UpdateSettingsDisplayName(" "); !errors.Is(err, ErrSettingsDisplayName) {
		t.Fatal("invalid name accepted", err)
	}
	if _, err := st.DB.Exec(`CREATE TRIGGER reject_profile_update BEFORE UPDATE ON server_state
		WHEN NEW.key='settings.profile.v1' AND NEW.value<>OLD.value
		BEGIN SELECT RAISE(ABORT, 'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSettingsDisplayName("Other"); err == nil {
		t.Fatal("failed write reported success")
	}
	if after, err := s.GetSettingsProfile(); err != nil || after != before {
		t.Fatalf("failed writes changed profile: %+v %v", after, err)
	}
}

func TestSettingsProfileConcurrentUpdatesPreserveFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, nil, "")
	if _, err := s.InitializeSettingsProfile(false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteSettingsSetup("Before"); err != nil {
		t.Fatal(err)
	}
	other, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	s2 := New(other, nil, "")
	for i := 0; i < 12; i++ {
		if _, err := s.CompleteSettingsSetup("Before"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make(chan error, 3)
		start := make(chan struct{})
		for _, run := range []func() error{
			func() error { _, err := s.UpdateSettingsDisplayName("After"); return err },
			func() error { _, err := s2.UpdateSettingsOnboarding(3, true); return err },
			func() error { _, err := s2.InitializeSettingsProfile(true); return err },
		} {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; errs <- run() }()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		p, err := s.GetSettingsProfile()
		if err != nil || p.DisplayName != "After" || p.OnboardingStep != 3 || !p.OnboardingComplete || p.Legacy {
			t.Fatalf("lost update: %+v %v", p, err)
		}
	}
	var count int
	if err := st.DB.QueryRow(`SELECT count(*) FROM server_state WHERE key LIKE 'settings.%'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("profile must use one key: %d %v", count, err)
	}
	p, err := s.GetSettingsProfile()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil || strings.Contains(string(data), "password") {
		t.Fatal("profile must be non-secret", err)
	}
}
