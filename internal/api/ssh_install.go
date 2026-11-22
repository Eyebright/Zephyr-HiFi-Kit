package api

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/remote"
)

var (
	sshHostRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)
	sshUserRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)
	sshTokenRE  = regexp.MustCompile(`^[0-9a-f]{48}$`)
	sshFprintRE = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{40,64}$`)
)

// sshInstallJob tracks one background SSH install for wizard progress polling.
// It never stores credentials or the registration token.
type sshInstallJob struct {
	mu      sync.Mutex
	State   string        `json:"state"` // running | done | failed
	Steps   []sshJobStep  `json:"steps"`
	Error   string        `json:"error,omitempty"`
	Created time.Time     `json:"-"`
	cur     int           `json:"-"`
}

type sshJobStep struct {
	Name   string `json:"name"`
	State  string `json:"state"` // pending | running | done | failed
	Detail string `json:"detail,omitempty"`
	At     int64  `json:"at,omitempty"`
}

var sshStages = []string{"connect", "download", "install", "verify"}

func newSSHInstallJob() *sshInstallJob {
	j := &sshInstallJob{State: "running", Created: time.Now(), cur: -1}
	for _, s := range sshStages {
		j.Steps = append(j.Steps, sshJobStep{Name: s, State: "pending"})
	}
	return j
}

func (j *sshInstallJob) advance(stage, detail string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cur >= 0 && j.cur < len(j.Steps) && j.Steps[j.cur].State == "running" {
		j.Steps[j.cur].State = "done"
	}
	for i := range j.Steps {
		if j.Steps[i].Name == stage {
			j.Steps[i].State = "running"
			j.Steps[i].Detail = detail
			j.Steps[i].At = time.Now().Unix()
			j.cur = i
			return
		}
	}
}

func (j *sshInstallJob) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cur >= 0 && j.cur < len(j.Steps) && j.Steps[j.cur].State == "running" {
		if err != nil {
			j.Steps[j.cur].State = "failed"
		} else {
			j.Steps[j.cur].State = "done"
		}
	}
	if err != nil {
		j.State = "failed"
		j.Error = err.Error()
		return
	}
	j.State = "done"
}

func (j *sshInstallJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	steps := make([]sshJobStep, len(j.Steps))
	copy(steps, j.Steps)
	return map[string]any{"state": j.State, "steps": steps, "error": j.Error}
}

func sshTarget(host, user, password, key, passphrase string, port int) (remote.Target, error) {
	if !sshHostRE.MatchString(host) {
		return remote.Target{}, invalidInput("ssh_host_invalid", "SSH 主机地址不合法", map[string]any{"field": "host"})
	}
	if port < 1 || port > 65535 {
		return remote.Target{}, invalidInput("ssh_port_invalid", "SSH 端口必须为 1..65535", map[string]any{"field": "port"})
	}
	if !sshUserRE.MatchString(user) {
		return remote.Target{}, invalidInput("ssh_user_invalid", "SSH 用户名不合法", map[string]any{"field": "user"})
	}
	if password == "" && strings.TrimSpace(key) == "" {
		return remote.Target{}, invalidInput("ssh_auth_required", "请填写 SSH 密码或私钥", map[string]any{"field": "password"})
	}
	if len(password) > 512 || len(passphrase) > 512 || len(key) > 32*1024 {
		return remote.Target{}, invalidInput("ssh_credential_too_long", "SSH 凭据过长", map[string]any{"field": "password"})
	}
	return remote.Target{Host: host, Port: port, User: user, Password: password, PrivateKey: key, Passphrase: passphrase}, nil
}

func validateServerURL(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > 200 {
		return "", invalidInput("server_url_invalid", "服务端地址不合法", map[string]any{"field": "server_url"})
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", invalidInput("server_url_invalid", "服务端地址不合法", map[string]any{"field": "server_url"})
	}
	return strings.TrimRight(raw, "/"), nil
}

// sshProbe validates reachability and returns the host key fingerprint plus
// detected OS facts. The fingerprint must be confirmed before install, which
// then refuses a mismatched host key before sending credentials.
func (a *API) sshProbe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		User       string `json:"user"`
		Password   string `json:"password"`
		PrivateKey string `json:"private_key"`
		Passphrase string `json:"passphrase"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	t, err := sshTarget(body.Host, body.User, body.Password, body.PrivateKey, body.Passphrase, body.Port)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	info, err := remote.Probe(ctx, t)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "ssh_probe_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// sshInstall starts a background install. The response carries a job id whose
// progress the wizard polls; credentials are kept only inside the goroutine.
func (a *API) sshInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		User         string `json:"user"`
		Password     string `json:"password"`
		PrivateKey   string `json:"private_key"`
		Passphrase   string `json:"passphrase"`
		SudoPassword string `json:"sudo_password"`
		Fingerprint  string `json:"fingerprint"`
		ServerURL    string `json:"server_url"`
		Token        string `json:"token"`
	}
	if err := decode(r, &body); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return
	}
	t, err := sshTarget(body.Host, body.User, body.Password, body.PrivateKey, body.Passphrase, body.Port)
	if err != nil {
		fail(w, err)
		return
	}
	if !sshFprintRE.MatchString(body.Fingerprint) {
		writeErrorCode(w, http.StatusBadRequest, "ssh_fingerprint_required", "缺少已确认的主机指纹", map[string]any{"field": "fingerprint"})
		return
	}
	if !sshTokenRE.MatchString(body.Token) {
		writeErrorCode(w, http.StatusBadRequest, "ssh_token_invalid", "注册令牌不合法", map[string]any{"field": "token"})
		return
	}
	serverURL, err := validateServerURL(body.ServerURL)
	if err != nil {
		fail(w, err)
		return
	}
	t.SudoPassword = body.SudoPassword

	select {
	case a.sshSem <- struct{}{}:
	default:
		writeErrorCode(w, http.StatusTooManyRequests, "ssh_busy", "已有两个 SSH 安装任务在进行，请稍后再试")
		return
	}
	id := uuid.NewString()
	job := newSSHInstallJob()
	a.sshMu.Lock()
	a.pruneSSHJobsLocked()
	a.sshJobs[id] = job
	a.sshMu.Unlock()

	go func() {
		defer func() { <-a.sshSem }()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		err := remote.Install(ctx, t, body.Fingerprint, serverURL, body.Token, job.advance)
		job.finish(err)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": id})
}

func (a *API) sshInstallStatus(w http.ResponseWriter, r *http.Request) {
	a.sshMu.Lock()
	job := a.sshJobs[r.PathValue("id")]
	a.sshMu.Unlock()
	if job == nil {
		writeErrorCode(w, http.StatusNotFound, "ssh_job_not_found", "安装任务不存在或已过期")
		return
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

func (a *API) pruneSSHJobsLocked() {
	cutoff := time.Now().Add(-30 * time.Minute)
	for id, j := range a.sshJobs {
		if j.Created.Before(cutoff) {
			delete(a.sshJobs, id)
		}
	}
}
