package api

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hifihub/server/internal/config"
	"hifihub/server/internal/library"
)

const (
	settingsCookie     = "zephyr_settings_session"
	settingsIterations = 600000
	settingsSessionTTL = 12 * time.Hour
)

type settingsPassword struct {
	Version    int    `json:"version"`
	Iterations int    `json:"iterations"`
	Salt       []byte `json:"salt"`
	Hash       []byte `json:"hash"`
}

// One local administrator, not an agent identity or an RBAC role. Tokens are
// never persisted; only their SHA-256 digests live in this process.
type settingsAdmin struct {
	mu          sync.Mutex
	dataDir     string
	configured  bool
	unavailable bool
	password    settingsPassword
	sessions    map[[32]byte]time.Time
	failures    int
	nextAttempt time.Time
	now         func() time.Time

	profileInitialized bool
	profile            *library.SettingsProfile // in-memory fallback only when lib is nil
	defaultLanguage    string                   // independent in-memory fallback only when lib is nil
}

func newSettingsAdmin(cfg config.Config) *settingsAdmin {
	s := &settingsAdmin{dataDir: cfg.DataDir, sessions: make(map[[32]byte]time.Time), now: time.Now}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return s
	}
	path := filepath.Join(cfg.DataDir, "settings-admin.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s
	}
	// Existing but unreadable/corrupt credentials must never re-enable setup.
	s.configured = true
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		s.unavailable = true
		return s
	}
	f, err := os.Open(path)
	if err != nil {
		s.unavailable = true
		return s
	}
	defer f.Close()
	err = decodeSettingsJSON(io.LimitReader(f, 4097), &s.password)
	p := s.password
	if err != nil || info.Size() > 4096 || p.Version != 1 || p.Iterations != settingsIterations || len(p.Salt) != 32 || len(p.Hash) != 32 {
		s.unavailable = true
	}
	return s
}

func decodeSettingsJSON(r io.Reader, out any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("invalid JSON document")
	}
	return nil
}

func settingsAuthority(host, scheme string) (string, bool) {
	u, err := url.Parse(scheme + "://" + host)
	if err != nil || u.Host != host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" {
		return "", false
	}
	name := strings.ToLower(u.Hostname())
	if strings.ContainsAny(name, " /\\%\t\r\n") {
		return "", false
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(name, port), true
}

func settingsLocalRequest(r *http.Request) bool {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(remote)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	authority, ok := settingsAuthority(r.Host, "http")
	if !ok {
		return false
	}
	host, _, _ := net.SplitHostPort(authority)
	if host == "localhost" {
		return true
	}
	ip = net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func settingsSecureContext(r *http.Request) bool {
	// Deliberately ignore Forwarded/X-Forwarded-*; a remote cleartext request
	// must not become privileged by claiming that a proxy used TLS.
	return r.TLS != nil || settingsLocalRequest(r)
}

func settingsRequestAllowed(w http.ResponseWriter, r *http.Request, mutation bool) bool {
	w.Header().Set("Cache-Control", "no-store")
	if !settingsSecureContext(r) {
		writeErrorCode(w, http.StatusForbidden, "settings_secure_context_required", "管理操作需要 HTTPS，或本机 localhost 连接")
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host, ok := settingsAuthority(r.Host, scheme)
	origin := r.Header.Get("Origin")
	if !ok || (mutation && origin == "") {
		writeErrorCode(w, http.StatusForbidden, "settings_same_origin_required", "需要同源请求")
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		writeErrorCode(w, http.StatusForbidden, "settings_cross_site_denied", "不允许跨站请求")
		return false
	}
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != scheme || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			writeErrorCode(w, http.StatusForbidden, "settings_cross_origin_denied", "不允许跨源请求")
			return false
		}
		o, valid := settingsAuthority(u.Host, u.Scheme)
		if !valid || o != host {
			writeErrorCode(w, http.StatusForbidden, "settings_cross_origin_denied", "不允许跨源请求")
			return false
		}
	}
	if mutation {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		artifactUpload := r.Pattern == "POST /api/v1/apps/{id}/versions" && mediaType == "multipart/form-data"
		if err != nil || (mediaType != "application/json" && !artifactUpload) {
			writeErrorCode(w, http.StatusUnsupportedMediaType, "settings_json_required", "需要 application/json")
			return false
		}
	}
	return true
}

func settingsSetCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	c := &http.Cookie{Name: settingsCookie, Value: token, Path: "/api/v1", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, Expires: expires, MaxAge: int(settingsSessionTTL.Seconds())}
	if token == "" {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}

func (s *settingsAdmin) authenticated(w http.ResponseWriter, r *http.Request) bool {
	if s == nil {
		return false
	}
	c, err := r.Cookie(settingsCookie)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, until := range s.sessions {
		if !now.Before(until) {
			delete(s.sessions, k)
		}
	}
	digest := sha256.Sum256([]byte(c.Value))
	until, ok := s.sessions[digest]
	if s.configured && !s.unavailable && settingsSecureContext(r) && ok && now.Before(until) {
		return true
	}
	delete(s.sessions, digest)
	settingsSetCookie(w, r, "", time.Time{})
	return false
}

func (s *settingsAdmin) issueSession(w http.ResponseWriter, r *http.Request) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	until := s.now().Add(settingsSessionTTL)
	// Bound memory even if an administrator repeatedly signs in.
	for k, expiry := range s.sessions {
		if !s.now().Before(expiry) {
			delete(s.sessions, k)
		}
	}
	if len(s.sessions) >= 32 {
		var oldest [32]byte
		var earliest time.Time
		for k, expiry := range s.sessions {
			if earliest.IsZero() || expiry.Before(earliest) {
				oldest, earliest = k, expiry
			}
		}
		delete(s.sessions, oldest)
	}
	s.sessions[sha256.Sum256([]byte(token))] = until
	settingsSetCookie(w, r, token, until)
	return nil
}

func (a *API) settingsSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	auth := a.admin.authenticated(w, r)
	configured, canSetup := false, false
	if a.admin != nil {
		a.admin.mu.Lock()
		configured = a.admin.configured
		canSetup = !configured && !a.admin.unavailable && strings.TrimSpace(a.admin.dataDir) != "" && settingsLocalRequest(r)
		if canSetup {
			_, err := a.settingsProfileLocked()
			canSetup = err == nil
		}
		a.admin.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": configured, "authenticated": auth, "can_setup": canSetup, "secure_context": settingsSecureContext(r)})
}

func settingsReadPassword(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeSettingsJSON(http.MaxBytesReader(w, r.Body, 8192), &req); err != nil || !utf8.ValidString(req.Password) || len(req.Password) > 1024 || utf8.RuneCountInString(req.Password) < 12 {
		writeErrorCode(w, http.StatusBadRequest, "settings_password_invalid", "密码须为至少12个字符，最多1024字节")
		return "", false
	}
	return req.Password, true
}

func settingsReadSetup(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	var req struct {
		Password    string  `json:"password"`
		DisplayName *string `json:"display_name"`
	}
	if err := decodeSettingsProfileRequest(w, r, &req, "password", "display_name"); err != nil || req.DisplayName == nil {
		writeErrorCode(w, http.StatusBadRequest, "settings_identity_required", "需要 password 和 display_name")
		return "", "", false
	}
	if !utf8.ValidString(req.Password) || len(req.Password) > 1024 || utf8.RuneCountInString(req.Password) < 12 {
		writeErrorCode(w, http.StatusBadRequest, "settings_password_invalid", "密码须为至少12个字符，最多1024字节")
		return "", "", false
	}
	name, err := library.NormalizeSettingsDisplayName(*req.DisplayName)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "settings_display_name_invalid", "显示名字须为1至40个Unicode字符，且不含控制字符")
		return "", "", false
	}
	return req.Password, name, true
}

func (a *API) setupSettingsAdmin(w http.ResponseWriter, r *http.Request) {
	if !settingsRequestAllowed(w, r, true) {
		return
	}
	if !settingsLocalRequest(r) {
		writeErrorCode(w, http.StatusForbidden, "settings_local_setup_required", "仅本机 localhost 连接可初始化管理员")
		return
	}
	if a.admin == nil || strings.TrimSpace(a.admin.dataDir) == "" {
		writeErrorCode(w, http.StatusServiceUnavailable, "settings_data_dir_required", "请先配置数据目录")
		return
	}
	password, name, ok := settingsReadSetup(w, r)
	if !ok {
		return
	}
	s := a.admin
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.configured || s.unavailable {
		writeErrorCode(w, http.StatusConflict, "settings_setup_unavailable", "管理员已初始化或不可用")
		return
	}
	// Commit the new-instance marker BEFORE creating any credentials. If this
	// fails, no account is created; a restart must not classify it as legacy.
	if _, err := a.settingsProfileLocked(); err != nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "settings_initial_state_failed", "无法保存初始引导状态，尚未创建管理员")
		return
	}
	p := settingsPassword{Version: 1, Iterations: settingsIterations, Salt: make([]byte, 32)}
	if _, err := rand.Read(p.Salt); err != nil {
		writeErrorCode(w, 500, "settings_setup_failed", "无法初始化管理员")
		return
	}
	var err error
	p.Hash, err = pbkdf2.Key(sha256.New, password, p.Salt, p.Iterations, 32)
	if err != nil {
		writeErrorCode(w, 500, "settings_setup_failed", "无法初始化管理员")
		return
	}
	data, _ := json.Marshal(p)
	if err = os.MkdirAll(s.dataDir, 0700); err != nil {
		writeErrorCode(w, 500, "settings_credentials_save_failed", "无法保存管理员配置")
		return
	}
	path := filepath.Join(s.dataDir, "settings-admin.json")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// Another creator (or a corrupt file) won the exclusive create. Do
			// not advertise setup again, or overwrite credentials we do not own.
			s.configured, s.unavailable = true, true
		}
		writeErrorCode(w, http.StatusConflict, "settings_credentials_save_failed", "无法保存管理员配置")
		return
	}
	// From this point a partial file also disables setup, including on restart.
	s.configured, s.unavailable = true, true
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		writeErrorCode(w, 500, "settings_credentials_write_failed", "管理员配置写入失败，无法再次初始化")
		return
	}
	s.password, s.unavailable = p, false
	if err = a.completeSettingsSetupLocked(name); err != nil {
		writeErrorCode(w, 500, "settings_setup_profile_incomplete", "管理员已创建，请登录后补全显示名字并继续引导")
		return
	}
	if err = s.issueSession(w, r); err != nil {
		writeErrorCode(w, 500, "settings_setup_login_required", "管理员已创建，请重新登录")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"configured": true, "authenticated": true})
}

func (a *API) loginSettingsAdmin(w http.ResponseWriter, r *http.Request) {
	if !settingsRequestAllowed(w, r, true) {
		return
	}
	if a.admin == nil {
		writeErrorCode(w, http.StatusUnauthorized, "settings_setup_required", "请先初始化管理员")
		return
	}
	password, ok := settingsReadPassword(w, r)
	if !ok {
		return
	}
	s := a.admin
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configured || s.unavailable {
		writeErrorCode(w, http.StatusUnauthorized, "settings_admin_unavailable", "管理员未初始化或不可用")
		return
	}
	if s.now().Before(s.nextAttempt) {
		w.Header().Set("Retry-After", "60")
		writeErrorCode(w, http.StatusTooManyRequests, "settings_login_rate_limited", "登录尝试过多，请稍后再试")
		return
	}
	derived, err := pbkdf2.Key(sha256.New, password, s.password.Salt, s.password.Iterations, 32)
	if err != nil || subtle.ConstantTimeCompare(derived, s.password.Hash) != 1 {
		s.failures++
		delay := time.Second << min(s.failures-1, 6)
		s.nextAttempt = s.now().Add(delay)
		writeErrorCode(w, http.StatusUnauthorized, "settings_password_incorrect", "密码错误")
		return
	}
	s.failures, s.nextAttempt = 0, time.Time{}
	if err := s.issueSession(w, r); err != nil {
		writeErrorCode(w, 500, "settings_session_failed", "无法创建会话")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (a *API) logoutSettingsAdmin(w http.ResponseWriter, r *http.Request) {
	if !settingsRequestAllowed(w, r, true) {
		return
	}
	if a.admin != nil {
		if c, err := r.Cookie(settingsCookie); err == nil {
			a.admin.mu.Lock()
			delete(a.admin.sessions, sha256.Sum256([]byte(c.Value)))
			a.admin.mu.Unlock()
		}
	}
	settingsSetCookie(w, r, "", time.Time{})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (a *API) requireSettingsAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if !settingsRequestAllowed(w, r, mutation) {
			return
		}
		if !a.admin.authenticated(w, r) {
			writeErrorCode(w, http.StatusUnauthorized, "settings_login_required", "需要管理员登录")
			return
		}
		next(w, r)
	}
}
