// Package mount builds and executes kernel CIFS (SMB) mounts for the media
// library. It is pure Go: the command/credential builders are unit-testable
// without root or CGO, and a Runner interface lets tests inject a fake exec.
//
// Secrets never touch the command line (visible in ps/logs): the username,
// password and domain are written to a transient 0600 credentials file that is
// referenced by path and removed as soon as mount returns. Spec.String redacts
// the password so accidental logging is safe.
package mount

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultVersion is the SMB dialect used when Spec.Version is empty (PROTOCOL.md §14).
const DefaultVersion = "3.0"

var allowedVersions = map[string]bool{
	"1.0": true, "2.0": true, "2.1": true, "3.0": true, "3.1.1": true,
}

// Spec describes one SMB share to mount. The zero value is a read-only guest
// mount once Host/Share/Target are filled in; credentials are optional.
type Spec struct {
	Host     string // SMB server hostname or IP
	Share    string // share name (single path segment)
	Target   string // local absolute mount point, e.g. /mnt/zephyr/library
	Username string // empty => guest mount
	Password string
	Domain   string
	Version  string // SMB dialect; empty => DefaultVersion
	Writable bool   // default false: the media library only reads
}

// NeedsCredentials reports whether a credentials file must be written.
func (s Spec) NeedsCredentials() bool { return s.Username != "" }

// UNC returns the //host/share source path.
func (s Spec) UNC() string { return "//" + s.Host + "/" + s.Share }

// Credentials renders the mount.cifs credentials-file body. It is line-based,
// so Validate rejects newlines in these fields.
func (s Spec) Credentials() string {
	var b strings.Builder
	b.WriteString("username=" + s.Username + "\n")
	b.WriteString("password=" + s.Password + "\n")
	if s.Domain != "" {
		b.WriteString("domain=" + s.Domain + "\n")
	}
	return b.String()
}

// Options builds the comma-separated -o mount options. credPath is the transient
// credentials file; when empty and no username is set the share mounts as guest.
func (s Spec) Options(credPath string) string {
	ver := s.Version
	if ver == "" {
		ver = DefaultVersion
	}
	opts := []string{"vers=" + ver}
	if credPath != "" {
		opts = append(opts, "credentials="+credPath)
	} else if s.Username == "" {
		opts = append(opts, "guest")
	}
	if s.Writable {
		opts = append(opts, "rw")
	} else {
		opts = append(opts, "ro")
	}
	opts = append(opts, "iocharset=utf8")
	return strings.Join(opts, ",")
}

// Args returns the full argv for mount(8): mount -t cifs //host/share target -o opts.
func (s Spec) Args(credPath string) []string {
	return []string{"-t", "cifs", s.UNC(), s.Target, "-o", s.Options(credPath)}
}

// Redacted is a log-safe description that never includes the password.
func (s Spec) Redacted() string {
	ver := s.Version
	if ver == "" {
		ver = DefaultVersion
	}
	mode := "ro"
	if s.Writable {
		mode = "rw"
	}
	user := s.Username
	if user == "" {
		user = "guest"
	} else if s.Domain != "" {
		user = s.Domain + "\\" + user
	}
	return fmt.Sprintf("%s → %s [vers=%s %s user=%s]", s.UNC(), s.Target, ver, mode, user)
}

// String implements fmt.Stringer via Redacted so printing a Spec leaks no secret.
func (s Spec) String() string { return s.Redacted() }

// Validate checks user-supplied mount configuration at the system boundary.
func (s Spec) Validate() error {
	if err := validateSegment(s.Host, "主机"); err != nil {
		return err
	}
	if err := validateSegment(s.Share, "共享名"); err != nil {
		return err
	}
	if err := validateTarget(s.Target); err != nil {
		return err
	}
	if s.Version != "" && !allowedVersions[s.Version] {
		return fmt.Errorf("不支持的 SMB 版本：%s", s.Version)
	}
	for _, f := range []struct{ label, val string }{
		{"用户名", s.Username}, {"密码", s.Password}, {"域", s.Domain},
	} {
		if strings.ContainsAny(f.val, "\n\r\x00") {
			return fmt.Errorf("%s不能包含换行或空字符", f.label)
		}
	}
	return nil
}

func validateSegment(v, label string) error {
	if v == "" {
		return fmt.Errorf("%s不能为空", label)
	}
	if strings.ContainsAny(v, "/\\,\n\r\t \x00") {
		return fmt.Errorf("%s包含非法字符", label)
	}
	return nil
}

// IsUNC reports whether target is a UNC share path (//host/share or \\host\share)
// rather than a local directory. UNC targets are read directly over SMB and need
// no kernel mount point directory.
func IsUNC(target string) bool {
	return strings.HasPrefix(target, "//") || strings.HasPrefix(target, `\\`)
}

func validateTarget(t string) error {
	if t == "" {
		return errors.New("挂载点不能为空")
	}
	drive := len(t) >= 3 && t[1] == ':' && (t[2] == '/' || t[2] == '\\') &&
		((t[0] >= 'a' && t[0] <= 'z') || (t[0] >= 'A' && t[0] <= 'Z'))
	if !strings.HasPrefix(t, "/") && !strings.HasPrefix(t, `\\`) && !drive {
		return errors.New("挂载点必须是绝对路径")
	}
	if strings.ContainsAny(t, "\n\r\x00") {
		return errors.New("挂载点包含非法字符")
	}
	return nil
}

// Runner executes a system command and returns combined output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner runs commands via os/exec with a timeout (Linux runtime only).
type ExecRunner struct{ Timeout time.Duration }

// Run executes name with args, enforcing a timeout.
func (e ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if e.Timeout <= 0 {
		e.Timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, name, args...).CombinedOutput()
	return string(out), err
}

// Manager mounts and unmounts CIFS shares, writing transient credential files
// under credDir (which must be root-owned, e.g. a private runtime state dir).
type Manager struct {
	run     Runner
	credDir string
}

// NewManager builds a Manager. credDir is where 0600 credential files are written.
func NewManager(run Runner, credDir string) *Manager {
	return &Manager{run: run, credDir: credDir}
}

// Mount validates the spec, writes a transient credentials file when needed,
// runs mount(8), and removes the credentials file regardless of the outcome.
func (m *Manager) Mount(ctx context.Context, s Spec) error {
	if err := s.Validate(); err != nil {
		return err
	}
	credPath := ""
	if s.NeedsCredentials() {
		p, err := m.writeCredentials(s)
		if err != nil {
			return fmt.Errorf("写入凭据文件失败：%w", err)
		}
		credPath = p
		defer os.Remove(credPath)
	}
	out, err := m.run.Run(ctx, "mount", s.Args(credPath)...)
	if err != nil {
		return fmt.Errorf("挂载 %s 失败：%w (%s)", s.Redacted(), err, strings.TrimSpace(out))
	}
	return nil
}

// Unmount runs umount(8) on an absolute target path.
func (m *Manager) Unmount(ctx context.Context, target string) error {
	if err := validateTarget(target); err != nil {
		return err
	}
	out, err := m.run.Run(ctx, "umount", target)
	if err != nil {
		return fmt.Errorf("卸载 %s 失败：%w (%s)", target, err, strings.TrimSpace(out))
	}
	return nil
}

func (m *Manager) Check(ctx context.Context, target string) error {
	if err := validateTarget(target); err != nil {
		return err
	}
	// An externally unmounted share exposes an empty local directory, not an empty NAS.
	if _, err := m.run.Run(ctx, "findmnt", "--noheadings", "--types", "cifs", "--mountpoint", target); err != nil {
		return fmt.Errorf("共享挂载不可用，保留已有索引：%w", err)
	}
	return nil
}

func (m *Manager) writeCredentials(s Spec) (string, error) {
	f, err := os.CreateTemp(m.credDir, "cifs-*.cred")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := f.WriteString(s.Credentials()); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}
