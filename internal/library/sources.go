package library

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"hifihub/server/internal/media"
	"hifihub/server/internal/mount"
)

// localDirectory validates, but never creates, a server-side directory.
func localDirectory(p string) (string, error) {
	if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n") {
		return "", fmt.Errorf("本地目录必须是服务端绝对路径")
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("路径不是目录")
	}
	return real, nil
}

func withinRoot(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func sameSource(a, b Mount) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == "local" {
		if filepath.Clean(a.Target) == filepath.Clean(b.Target) {
			return true
		}
		left, leftErr := os.Stat(a.Target)
		right, rightErr := os.Stat(b.Target)
		return leftErr == nil && rightErr == nil && os.SameFile(left, right)
	}
	return strings.EqualFold(strings.TrimSpace(a.Host), strings.TrimSpace(b.Host)) && a.Share == b.Share
}

func (s *Service) configuredMount(id string, req AddMountRequest) (Mount, error) {
	kind := req.Kind
	if kind == "" {
		kind = "smb"
	}
	encoding, ok := media.NormalizeTextEncoding(req.TextEncoding)
	if !ok {
		return Mount{}, fmt.Errorf("不支持的标签/文件名编码：%s", req.TextEncoding)
	}
	m := Mount{ID: id, Kind: kind, Host: req.Host, Share: req.Share, Username: req.Username,
		Password: req.Password, Domain: req.Domain, Version: req.Version, TextEncoding: encoding,
		Writable: req.Writable}
	switch kind {
	case "local":
		p, err := localDirectory(req.Path)
		if err != nil {
			return Mount{}, err
		}
		label := strings.TrimSpace(req.Share)
		if label == "" {
			label = filepath.Base(p)
			if label == string(filepath.Separator) || strings.ContainsAny(label, "/\\:") {
				label = "本地音乐"
			}
		}
		if strings.ContainsAny(label, "/\\\x00\r\n\t") {
			return Mount{}, fmt.Errorf("本地源名称包含非法字符")
		}
		m.Host, m.Share, m.Target = "localhost", label, p
		m.Username, m.Password, m.Domain, m.Version = "", "", "", ""
	case "smb":
		m.Host = strings.ToLower(strings.TrimSpace(m.Host))
		m.Target = path.Join(s.mountRoot, id)
		if runtime.GOOS == "windows" {
			m.Target = "//" + m.Host + "/" + m.Share
		}
		if m.Version == "" {
			m.Version = mount.DefaultVersion
		}
		if err := m.spec().Validate(); err != nil {
			return Mount{}, err
		}
	default:
		return Mount{}, fmt.Errorf("不支持的源类型：%s", kind)
	}
	return m, nil
}

// UpdateMount replaces editable configuration. Empty passwords retain the old
// credential. Kind and physical location are immutable: use a new source for a
// different location, rather than silently reassigning existing track identities.
// For local sources Share is an optional display name; an omitted Path is kept.
func (s *Service) UpdateMount(id string, req AddMountRequest) (Mount, error) {
	s.metadataMu.Lock()
	defer s.metadataMu.Unlock()
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	old, err := s.GetMount(id)
	if err != nil {
		return Mount{}, err
	}
	if req.Kind == "" {
		req.Kind = old.Kind
	}
	if req.Password == "" {
		req.Password = old.Password
	}
	if req.Kind == "local" && req.Path == "" {
		req.Path = old.Target
	}
	if req.TextEncoding == "" {
		req.TextEncoding = old.TextEncoding
	}
	m, err := s.configuredMount(id, req)
	if err != nil {
		return Mount{}, err
	}
	if !sameSource(old, m) {
		return Mount{}, fmt.Errorf("修改源类型或实际位置请添加新音乐源")
	}
	if m.Kind == "smb" && old.Status == StatusMounted &&
		(m.Username != old.Username || m.Password != old.Password || m.Domain != old.Domain || m.Version != old.Version || m.Writable != old.Writable) {
		return Mount{}, fmt.Errorf("请先卸载再修改 SMB 凭据或选项")
	}
	_, err = s.db.Exec(`UPDATE mounts SET host=?,share=?,username=?,password=?,domain=?,version=?,text_encoding=?,writable=? WHERE id=?`,
		m.Host, m.Share, m.Username, m.Password, m.Domain, m.Version, m.TextEncoding, boolInt(m.Writable), id)
	if err != nil {
		return Mount{}, err
	}
	return s.GetMount(id)
}

// TestMount tests directory readability without mounting/unmounting or changing
// state. Local sources can be tested while unmounted; SMB must already be mounted
// so testing cannot disrupt a shared system session or production mount.
func (s *Service) TestMount(ctx context.Context, id string) (Mount, error) {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Mount{}, err
	}
	m, err := s.GetMount(id)
	if err != nil {
		return m, err
	}
	if m.Kind != "local" && m.Status != StatusMounted {
		return m, ErrNotMounted
	}
	p, err := localDirectory(m.Target)
	if err != nil {
		return m, err
	}
	f, err := os.Open(p)
	if err != nil {
		return m, err
	}
	defer f.Close()
	_, err = f.ReadDir(1)
	if err != nil && err != io.EOF {
		return m, err
	}
	return m, ctx.Err()
}
