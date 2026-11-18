package mount

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func validSpec() Spec {
	return Spec{
		Host:     "nas.local",
		Share:    "music",
		Target:   "/mnt/zephyr/library",
		Username: "zephyr",
		Password: "s3cr3t-p@ss",
		Domain:   "WORKGROUP",
	}
}

func TestSpecValidateOK(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	guest := Spec{Host: "nas", Share: "pub", Target: "/mnt/pub"}
	if err := guest.Validate(); err != nil {
		t.Fatalf("guest spec rejected: %v", err)
	}
}

func TestSpecValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Spec)
	}{
		{"empty host", func(s *Spec) { s.Host = "" }},
		{"host slash", func(s *Spec) { s.Host = "a/b" }},
		{"host comma", func(s *Spec) { s.Host = "a,b" }},
		{"host space", func(s *Spec) { s.Host = "a b" }},
		{"empty share", func(s *Spec) { s.Share = "" }},
		{"share backslash", func(s *Spec) { s.Share = "a\\b" }},
		{"relative target", func(s *Spec) { s.Target = "mnt/x" }},
		{"empty target", func(s *Spec) { s.Target = "" }},
		{"target newline", func(s *Spec) { s.Target = "/mnt/x\ny" }},
		{"bad version", func(s *Spec) { s.Version = "9.9" }},
		{"password newline", func(s *Spec) { s.Password = "a\nb" }},
		{"username NUL", func(s *Spec) { s.Username = "a\x00b" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := validSpec()
			c.mut(&s)
			if err := s.Validate(); err == nil {
				t.Fatalf("expected rejection for %s", c.name)
			}
		})
	}
}

func TestValidateTargetForms(t *testing.T) {
	ok := []string{"/mnt/zephyr/x", "C:/mnt/x", `C:\mnt\x`, "//nas/music", `\\nas\music`}
	for _, target := range ok {
		s := validSpec()
		s.Target = target
		if err := s.Validate(); err != nil {
			t.Fatalf("target %q rejected: %v", target, err)
		}
	}
	bad := []string{"mnt/x", "C:mnt/x", "", "/mnt/x\ny", `\\nas\music` + "\x00"}
	for _, target := range bad {
		s := validSpec()
		s.Target = target
		if err := s.Validate(); err == nil {
			t.Fatalf("target %q accepted", target)
		}
	}
	if !IsUNC("//nas/music") || !IsUNC(`\\nas\music`) || IsUNC("/mnt/x") || IsUNC("C:/x") {
		t.Fatal("IsUNC misclassified")
	}
}

func TestUNC(t *testing.T) {
	if got := validSpec().UNC(); got != "//nas.local/music" {
		t.Fatalf("unc = %q", got)
	}
}

func TestOptionsCredentials(t *testing.T) {
	s := validSpec()
	opts := s.Options("/run/zephyr/cifs-x.cred")
	if !strings.Contains(opts, "vers=3.0") {
		t.Fatalf("missing default vers: %q", opts)
	}
	if !strings.Contains(opts, "credentials=/run/zephyr/cifs-x.cred") {
		t.Fatalf("missing credentials: %q", opts)
	}
	if strings.Contains(opts, "guest") {
		t.Fatalf("authenticated mount must not be guest: %q", opts)
	}
	if !strings.Contains(opts, "ro") {
		t.Fatalf("default must be read-only: %q", opts)
	}
	if strings.Contains(opts, s.Password) {
		t.Fatalf("password leaked into options: %q", opts)
	}
}

func TestOptionsGuestWritableVersion(t *testing.T) {
	s := Spec{Host: "nas", Share: "pub", Target: "/mnt/pub", Writable: true, Version: "3.1.1"}
	opts := s.Options("")
	if !strings.Contains(opts, "guest") || !strings.Contains(opts, "rw") || !strings.Contains(opts, "vers=3.1.1") {
		t.Fatalf("guest/rw/vers options wrong: %q", opts)
	}
	if strings.Contains(opts, "credentials=") {
		t.Fatalf("guest mount must not reference credentials: %q", opts)
	}
}

func TestArgs(t *testing.T) {
	s := validSpec()
	args := s.Args("/run/cred")
	want := []string{"-t", "cifs", "//nas.local/music", "/mnt/zephyr/library", "-o"}
	if len(args) != 6 {
		t.Fatalf("argc = %d (%v)", len(args), args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Fatalf("arg[%d] = %q want %q", i, args[i], w)
		}
	}
	if strings.Contains(strings.Join(args, " "), s.Password) {
		t.Fatal("password leaked into argv")
	}
}

func TestCredentialsContent(t *testing.T) {
	s := validSpec()
	got := s.Credentials()
	want := "username=zephyr\npassword=s3cr3t-p@ss\ndomain=WORKGROUP\n"
	if got != want {
		t.Fatalf("credentials = %q want %q", got, want)
	}
	s.Domain = ""
	if strings.Contains(s.Credentials(), "domain=") {
		t.Fatal("empty domain must be omitted")
	}
}

func TestRedactedHidesPassword(t *testing.T) {
	s := validSpec()
	r := s.Redacted()
	if strings.Contains(r, s.Password) {
		t.Fatalf("password leaked: %q", r)
	}
	if !strings.Contains(r, "WORKGROUP\\zephyr") || !strings.Contains(r, "//nas.local/music") {
		t.Fatalf("redacted missing context: %q", r)
	}
	if s.String() != r {
		t.Fatal("String must equal Redacted")
	}
}

// fakeRunner records calls and captures the credentials-file body at exec time.
type fakeRunner struct {
	calls       []fakeCall
	out         string
	err         error
	credPath    string
	credContent string
}

type fakeCall struct {
	name string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, fakeCall{name: name, args: append([]string(nil), args...)})
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "-o" {
			continue
		}
		for _, opt := range strings.Split(args[i+1], ",") {
			if strings.HasPrefix(opt, "credentials=") {
				f.credPath = strings.TrimPrefix(opt, "credentials=")
				b, _ := os.ReadFile(f.credPath)
				f.credContent = string(b)
			}
		}
	}
	return f.out, f.err
}

func TestManagerMount(t *testing.T) {
	dir := t.TempDir()
	fr := &fakeRunner{}
	m := NewManager(fr, dir)
	s := validSpec()
	if err := m.Mount(context.Background(), s); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if len(fr.calls) != 1 || fr.calls[0].name != "mount" {
		t.Fatalf("expected one mount call, got %+v", fr.calls)
	}
	if fr.credContent != s.Credentials() {
		t.Fatalf("cred file body = %q want %q", fr.credContent, s.Credentials())
	}
	// 0600 enforced on Linux (Windows only maps the read-only bit).
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(fr.credPath); err == nil {
			if st.Mode().Perm() != 0o600 {
				t.Fatalf("cred perms = %o want 600", st.Mode().Perm())
			}
		}
	}
	// Credential file removed after mount returns.
	if _, err := os.Stat(fr.credPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cred file not removed: %v", err)
	}
	// No secret in any recorded argv.
	for _, c := range fr.calls {
		if strings.Contains(strings.Join(c.args, " "), s.Password) {
			t.Fatal("password leaked into argv")
		}
	}
	// credDir holds no leftover files.
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Fatalf("credDir not clean: %d entries", len(ents))
	}
}

func TestManagerMountGuestNoCredFile(t *testing.T) {
	dir := t.TempDir()
	fr := &fakeRunner{}
	m := NewManager(fr, dir)
	s := Spec{Host: "nas", Share: "pub", Target: "/mnt/pub"}
	if err := m.Mount(context.Background(), s); err != nil {
		t.Fatalf("guest mount: %v", err)
	}
	if fr.credPath != "" {
		t.Fatalf("guest mount wrote a cred file: %q", fr.credPath)
	}
	if !strings.Contains(strings.Join(fr.calls[0].args, " "), "guest") {
		t.Fatal("guest option missing")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Fatalf("credDir not clean: %d entries", len(ents))
	}
}

func TestManagerMountErrorRedacted(t *testing.T) {
	dir := t.TempDir()
	fr := &fakeRunner{out: "mount error: permission denied", err: errors.New("exit 32")}
	m := NewManager(fr, dir)
	s := validSpec()
	err := m.Mount(context.Background(), s)
	if err == nil {
		t.Fatal("expected mount error")
	}
	if strings.Contains(err.Error(), s.Password) {
		t.Fatalf("password leaked into error: %v", err)
	}
	if !strings.Contains(err.Error(), "//nas.local/music") {
		t.Fatalf("error missing redacted context: %v", err)
	}
	if _, statErr := os.Stat(fr.credPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cred file not removed on failure: %v", statErr)
	}
}

func TestManagerMountValidationBeforeExec(t *testing.T) {
	fr := &fakeRunner{}
	m := NewManager(fr, t.TempDir())
	s := validSpec()
	s.Target = "relative/path"
	if err := m.Mount(context.Background(), s); err == nil {
		t.Fatal("expected validation error")
	}
	if len(fr.calls) != 0 {
		t.Fatal("runner invoked despite invalid spec")
	}
}

func TestManagerUnmount(t *testing.T) {
	fr := &fakeRunner{}
	m := NewManager(fr, t.TempDir())
	if err := m.Unmount(context.Background(), "/mnt/zephyr/library"); err != nil {
		t.Fatalf("unmount: %v", err)
	}
	if len(fr.calls) != 1 || fr.calls[0].name != "umount" || fr.calls[0].args[0] != "/mnt/zephyr/library" {
		t.Fatalf("unexpected umount call: %+v", fr.calls)
	}
	if err := m.Unmount(context.Background(), "relative"); err == nil {
		t.Fatal("expected target validation error")
	}
}

func TestWriteCredentialsInDir(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(&fakeRunner{}, dir)
	p, err := m.writeCredentials(validSpec())
	if err != nil {
		t.Fatalf("writeCredentials: %v", err)
	}
	defer os.Remove(p)
	if filepath.Dir(p) != dir {
		t.Fatalf("cred file %q not in %q", p, dir)
	}
}
