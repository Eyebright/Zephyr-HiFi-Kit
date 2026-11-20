package library

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"hifihub/server/internal/store"
)

func TestSettingsLanguageDefaultAndPersistence(t *testing.T) {
	for _, language := range []string{"zh-CN", "zh-TW", "en"} {
		t.Run(language, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "language.db")
			st, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { st.Close() })
			s := New(st, nil, "")
			if got, err := s.GetSettingsLanguage(); err != nil || got != "zh-CN" {
				t.Fatalf("unset language: %q %v", got, err)
			}
			var count int
			if err := st.DB.QueryRow(`SELECT count(*) FROM server_state WHERE key=?`, settingsLanguageKey).Scan(&count); err != nil || count != 0 {
				t.Fatalf("public read wrote a default: %d %v", count, err)
			}
			if _, err := s.InitializeSettingsProfile(false); err != nil {
				t.Fatal(err)
			}
			profile, err := s.CompleteSettingsSetup("Administrator")
			if err != nil {
				t.Fatal(err)
			}
			var before string
			if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key=?`, settingsProfileKey).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if got, err := s.UpdateSettingsLanguage(language); err != nil || got != language {
				t.Fatalf("save language: %q %v", got, err)
			}
			var after, value string
			if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key=?`, settingsProfileKey).Scan(&after); err != nil || after != before {
				t.Fatalf("language changed profile bytes: %q %v", after, err)
			}
			if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key=?`, settingsLanguageKey).Scan(&value); err != nil || value != language {
				t.Fatalf("language must use its own plain-string key: %q %v", value, err)
			}
			if got, err := s.GetSettingsLanguage(); err != nil || got != language {
				t.Fatalf("read saved language: %q %v", got, err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			st, err = store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			s = New(st, nil, "")
			if got, err := s.GetSettingsLanguage(); err != nil || got != language {
				t.Fatalf("restart lost language: %q %v", got, err)
			}
			if got, err := s.GetSettingsProfile(); err != nil || got != profile {
				t.Fatalf("restart broke strict profile decoding: %+v %v", got, err)
			}
			if _, err := s.UpdateSettingsOnboarding(3, true); err != nil {
				t.Fatal(err)
			}
			if _, err := s.UpdateSettingsDisplayName("Renamed"); err != nil {
				t.Fatal(err)
			}
			if got, err := s.GetSettingsLanguage(); err != nil || got != language {
				t.Fatalf("profile update changed language: %q %v", got, err)
			}
		})
	}
}

func TestSettingsLanguageRejectsInvalidValues(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "unset", true: "saved"}[existing], func(t *testing.T) {
			s, st := newService(t, &fakeRunner{}, "")
			want, wantCount := "zh-CN", 0
			if existing {
				if _, err := s.UpdateSettingsLanguage("zh-TW"); err != nil {
					t.Fatal(err)
				}
				want, wantCount = "zh-TW", 1
			}
			for _, value := range []string{"", "zh", "zh-cn", "zh-tw", "ZH-CN", "EN", "en-US", "fr", " en", "en ", "en\n", "en\x00", string([]byte{0xff}), strings.Repeat("a", 8192)} {
				if err := ValidateSettingsLanguage(value); !errors.Is(err, ErrSettingsLanguage) {
					t.Fatalf("validated invalid language %q: %v", value, err)
				}
				if _, err := s.UpdateSettingsLanguage(value); !errors.Is(err, ErrSettingsLanguage) {
					t.Fatalf("saved invalid language %q: %v", value, err)
				}
				if got, err := s.GetSettingsLanguage(); err != nil || got != want {
					t.Fatalf("invalid input changed language: %q %v", got, err)
				}
				var count int
				if err := st.DB.QueryRow(`SELECT count(*) FROM server_state WHERE key=?`, settingsLanguageKey).Scan(&count); err != nil || count != wantCount {
					t.Fatalf("invalid input changed persistence: %d %v", count, err)
				}
			}
		})
	}
}

func TestSettingsLanguageCorruptionFailsClosed(t *testing.T) {
	s, st := newService(t, &fakeRunner{}, "")
	for _, value := range []string{"", "broken", "null", "{}", `"en"`, `{"default_language":"en"}`, "zh-cn", "en ", "en\x00", string([]byte{0xff})} {
		if _, err := st.DB.Exec(`INSERT INTO server_state(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsLanguageKey, value); err != nil {
			t.Fatal(err)
		}
		if got, err := s.GetSettingsLanguage(); !errors.Is(err, ErrSettingsLanguageCorrupt) || got != "" {
			t.Fatalf("corruption returned a default for %q: %q %v", value, got, err)
		}
		if _, err := s.UpdateSettingsLanguage("en"); !errors.Is(err, ErrSettingsLanguageCorrupt) {
			t.Fatalf("corrupt state overwritten: %v", err)
		}
		var after string
		if err := st.DB.QueryRow(`SELECT value FROM server_state WHERE key=?`, settingsLanguageKey).Scan(&after); err != nil || after != value {
			t.Fatalf("corrupt bytes changed: %q %v", after, err)
		}
	}
}

func TestSettingsLanguageStorageErrors(t *testing.T) {
	for _, mode := range []string{"closed", "missing-table", "insert", "update"} {
		t.Run(mode, func(t *testing.T) {
			s, st := newService(t, &fakeRunner{}, "")
			want := "zh-CN"
			switch mode {
			case "closed":
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
			case "missing-table":
				if _, err := st.DB.Exec(`DROP TABLE server_state`); err != nil {
					t.Fatal(err)
				}
			case "insert":
				if _, err := st.DB.Exec(`CREATE TRIGGER reject_language_insert BEFORE INSERT ON server_state
					WHEN NEW.key='settings.language.v1' BEGIN SELECT RAISE(ABORT, 'synthetic insert failure'); END`); err != nil {
					t.Fatal(err)
				}
			case "update":
				if _, err := s.UpdateSettingsLanguage("en"); err != nil {
					t.Fatal(err)
				}
				want = "en"
				if _, err := st.DB.Exec(`CREATE TRIGGER reject_language_update BEFORE UPDATE ON server_state
					WHEN NEW.key='settings.language.v1' AND NEW.value<>OLD.value
					BEGIN SELECT RAISE(ABORT, 'synthetic update failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := s.UpdateSettingsLanguage("zh-TW"); err == nil || got != "" || errors.Is(err, ErrSettingsLanguage) || errors.Is(err, ErrSettingsLanguageCorrupt) {
				t.Fatalf("write error not propagated: %q %v", got, err)
			}
			got, err := s.GetSettingsLanguage()
			if mode == "closed" || mode == "missing-table" {
				if err == nil || got != "" {
					t.Fatalf("read error became a default: %q %v", got, err)
				}
			} else if err != nil || got != want {
				t.Fatalf("failed write changed language: %q %v", got, err)
			}
		})
	}
}
