package aptsrc

import (
	"path/filepath"
	"testing"

	"hifihub/server/internal/domain"
	"hifihub/server/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func TestCodename(t *testing.T) {
	cases := map[string]string{
		"Debian GNU/Linux 12 (bookworm)": "bookworm",
		"Raspbian GNU/Linux 13 (trixie)": "trixie",
		"Moode Audio Player 9.3.7":       "",
		"debian13":                       "",
		"":                               "",
	}
	for in, want := range cases {
		if got := Codename(in); got != want {
			t.Fatalf("Codename(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSeedIdempotentAndSpecs(t *testing.T) {
	s := newSvc(t)
	if err := s.Seed(); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(); err != nil {
		t.Fatal(err)
	}
	all, err := s.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("seed list: %v %d", err, len(all))
	}
	if all[0].ID != "upmpdcli" {
		t.Fatalf("seed id: %s", all[0].ID)
	}
	for _, d := range []string{"Debian GNU/Linux 12 (bookworm)", "Raspbian GNU/Linux 13 (trixie)", "moode"} {
		specs, err := s.SpecsFor(d)
		if err != nil || len(specs) != 1 {
			t.Fatalf("specs for %q: %v %d", d, err, len(specs))
		}
		if specs[0].Suite != "" {
			t.Fatalf("suite must stay empty for agent-side resolution: %+v", specs[0])
		}
	}
	specs, err := s.SpecsFor("Ubuntu 24.04 LTS (noble)")
	if err != nil || len(specs) != 0 {
		t.Fatalf("noble must not match: %v %d", err, len(specs))
	}
}

func TestCreateValidation(t *testing.T) {
	s := newSvc(t)
	if _, err := s.Create(domain.AptSource{Name: "x", URL: "ftp://bad"}); err == nil {
		t.Fatal("ftp url accepted")
	}
	if _, err := s.Create(domain.AptSource{Name: "x", URL: "http://ok/", Components: "main contrib"}); err == nil {
		t.Fatal("whitespace components accepted")
	}
	if _, err := s.Create(domain.AptSource{ID: "Bad_ID", Name: "x", URL: "http://ok/"}); err == nil {
		t.Fatal("bad id accepted")
	}
	a, err := s.Create(domain.AptSource{Name: "ok", URL: "http://ok/"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Components != "main" {
		t.Fatalf("default components: %q", a.Components)
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(a.ID); err == nil {
		t.Fatal("double delete should fail")
	}
}