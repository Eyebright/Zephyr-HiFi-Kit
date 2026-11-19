package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hifihub/agent/internal/audio"
	"hifihub/agent/internal/model"
	"hifihub/agent/internal/serverclient"
	"hifihub/agent/internal/sysinfo"
)

type fakeBackend struct {
	installed   map[string]bool
	enabled     map[string]bool
	lastVersion string
	lastConfig  map[string]string
	powerAction string
	failInstall bool
	gate        GateState
	osChecks    int
	upgPackages []string
	netdiags    int

	playback       PlaybackState
	playbackErr    error
	playbackPaused bool
	pauseCalls     int
	stopCalls      int
	seekPTS        uint64
	volumeSet      int
	deviceSet      string
	queue          serverclient.QueueState
	queueErr       error
	nextCalls      int
	prevCalls      int
}

func newFake() *fakeBackend {
	return &fakeBackend{
		installed: map[string]bool{},
		enabled:   map[string]bool{},
		gate:      GateState{AllowLocal: true},
	}
}

func (f *fakeBackend) Identity(context.Context) Identity {
	return Identity{Alias: "living-pi", Arch: "arm64", Registered: true, ServerReachable: true, SystemAvailable: true, AllowPower: true, AllowLocal: true, AgentVersion: "0.1.0"}
}
func (f *fakeBackend) Services(context.Context) []ServiceStatus {
	var out []ServiceStatus
	for _, d := range model.PrimaryServices() {
		out = append(out, ServiceStatus{
			AppID: d.AppID, Name: d.Name, Protocol: d.Protocol, Primary: true,
			Installed: f.installed[d.AppID], Enabled: f.enabled[d.AppID], Running: f.enabled[d.AppID],
			ConfigFields: d.ConfigFields, OccupiesDAC: d.OccupiesDAC,
		})
	}
	return out
}
func (f *fakeBackend) Metrics() sysinfo.Metrics {
	return sysinfo.Metrics{CPUPercent: 12, MemPercent: 40, TempC: 45}
}
func (f *fakeBackend) DAC() []audio.Holder {
	return []audio.Holder{{PID: 9, Process: "shairport-sync", Device: "/dev/snd/pcmC1D0p"}}
}
func (f *fakeBackend) Devices() []audio.ALSADevice {
	return []audio.ALSADevice{{Card: 1, Device: 0, Name: "DAC", HW: "hw:1,0"}}
}
func (f *fakeBackend) Catalog(context.Context) ([]CatalogEntry, error) {
	return []CatalogEntry{{AppID: "shairport-sync", Name: "AirPlay", Versions: []string{"4.3.2"}}}, nil
}
func (f *fakeBackend) Install(_ context.Context, app, ver string, cfg map[string]string) error {
	if f.failInstall {
		return errors.New("integrity check failed")
	}
	f.installed[app] = true
	f.enabled[app] = true
	f.lastVersion = ver
	f.lastConfig = cfg
	return nil
}
func (f *fakeBackend) Uninstall(_ context.Context, app string) error {
	delete(f.installed, app)
	delete(f.enabled, app)
	return nil
}
func (f *fakeBackend) Enable(app string) error  { f.enabled[app] = true; return nil }
func (f *fakeBackend) Disable(app string) error { f.enabled[app] = false; return nil }
func (f *fakeBackend) Configure(app string, cfg map[string]string) error {
	f.lastConfig = cfg
	return nil
}
func (f *fakeBackend) Power(action string) error { f.powerAction = action; return nil }
func (f *fakeBackend) GateState() GateState      { return f.gate }
func (f *fakeBackend) Secret() string            { return f.gate.Secret }
func (f *fakeBackend) OSCheck(context.Context) serverclient.CommandResult {
	f.osChecks++
	return serverclient.CommandResult{Status: "ok", Log: "check log", Data: map[string]any{
		"last_check": int64(1700000000), "reboot_required": false, "packages": []any{},
	}}
}
func (f *fakeBackend) OSUpgrade(_ context.Context, packages []string) serverclient.CommandResult {
	f.upgPackages = packages
	return serverclient.CommandResult{Status: "ok", Log: "upgrade log"}
}
func (f *fakeBackend) Netdiag(context.Context) serverclient.CommandResult {
	f.netdiags++
	return serverclient.CommandResult{Status: "ok", Log: "netdiag log", Data: map[string]any{
		"at": int64(1700000000), "results": []any{},
	}}
}

func (f *fakeBackend) Playback() PlaybackState { return f.playback }
func (f *fakeBackend) PlaybackPause() (bool, error) {
	f.pauseCalls++
	f.playbackPaused = !f.playbackPaused
	return f.playbackPaused, f.playbackErr
}
func (f *fakeBackend) PlaybackStop() error { f.stopCalls++; return f.playbackErr }
func (f *fakeBackend) PlaybackSeek(pts uint64) error {
	f.seekPTS = pts
	return f.playbackErr
}
func (f *fakeBackend) PlaybackVolume(v int) error {
	f.volumeSet = v
	return f.playbackErr
}
func (f *fakeBackend) PlaybackDevice(dev string) error {
	f.deviceSet = dev
	return f.playbackErr
}
func (f *fakeBackend) PlaybackQueue(context.Context) (serverclient.QueueState, error) {
	return f.queue, f.queueErr
}
func (f *fakeBackend) PlaybackNext(context.Context) error { f.nextCalls++; return f.queueErr }
func (f *fakeBackend) PlaybackPrev(context.Context) error { f.prevCalls++; return f.queueErr }

func do(t *testing.T, h http.Handler, method, path, body string) (int, string) {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func TestServesIndex(t *testing.T) {
	h := New(newFake()).Handler()
	code, body := do(t, h, "GET", "/", "")
	if code != 200 || !strings.Contains(body, "Zephyr HiFi Kit") || !strings.Contains(body, "和风HiFi音乐播放套件") {
		t.Fatalf("index: %d %s", code, body[:min(80, len(body))])
	}
	if code, _ := do(t, h, "GET", "/app.js", ""); code != 200 {
		t.Fatal("app.js not served")
	}
	if code, _ := do(t, h, "GET", "/style.css", ""); code != 200 {
		t.Fatal("style.css not served")
	}
}

func TestStatusEndpoint(t *testing.T) {
	h := New(newFake()).Handler()
	code, body := do(t, h, "GET", "/local/api/status", "")
	if code != 200 {
		t.Fatalf("status: %d", code)
	}
	var m map[string]any
	json.Unmarshal([]byte(body), &m)
	for _, k := range []string{"identity", "metrics", "services", "dac", "devices"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("status missing %s", k)
		}
	}
}

func TestInstallEnableDisableUninstall(t *testing.T) {
	f := newFake()
	h := New(f).Handler()
	code, body := do(t, h, "POST", "/local/api/services/shairport-sync/install", `{"version":"4.3.2","config":{"airplay_name":"客厅"}}`)
	if code != 200 {
		t.Fatalf("install: %d %s", code, body)
	}
	if f.lastVersion != "4.3.2" || f.lastConfig["airplay_name"] != "客厅" {
		t.Fatalf("install args: %s %v", f.lastVersion, f.lastConfig)
	}
	if !f.installed["shairport-sync"] {
		t.Fatal("not installed")
	}
	if code, _ := do(t, h, "POST", "/local/api/services/shairport-sync/disable", ""); code != 200 || f.enabled["shairport-sync"] {
		t.Fatal("disable failed")
	}
	if code, _ := do(t, h, "POST", "/local/api/services/shairport-sync/enable", ""); code != 200 || !f.enabled["shairport-sync"] {
		t.Fatal("enable failed")
	}
	if code, _ := do(t, h, "POST", "/local/api/services/shairport-sync/config", `{"config":{"dac_device":"hw:2,0"}}`); code != 200 || f.lastConfig["dac_device"] != "hw:2,0" {
		t.Fatal("config failed")
	}
	if code, _ := do(t, h, "POST", "/local/api/services/shairport-sync/uninstall", ""); code != 200 || f.installed["shairport-sync"] {
		t.Fatal("uninstall failed")
	}
}

func TestInstallErrorPropagates(t *testing.T) {
	f := newFake()
	f.failInstall = true
	h := New(f).Handler()
	code, body := do(t, h, "POST", "/local/api/services/shairport-sync/install", `{}`)
	if code != 400 || !strings.Contains(body, "integrity") {
		t.Fatalf("expected 400 integrity error, got %d %s", code, body)
	}
}

func TestPowerValidation(t *testing.T) {
	f := newFake()
	h := New(f).Handler()
	if code, _ := do(t, h, "POST", "/local/api/power", `{"action":"halt"}`); code != 400 {
		t.Fatal("invalid power action must be rejected")
	}
	if code, _ := do(t, h, "POST", "/local/api/power", `{"action":"reboot"}`); code != 200 || f.powerAction != "reboot" {
		t.Fatal("reboot failed")
	}
	if code, _ := do(t, h, "POST", "/local/api/power", `{"action":"shutdown"}`); code != 200 || f.powerAction != "shutdown" {
		t.Fatal("shutdown failed")
	}
}

func doWithSecret(t *testing.T, h http.Handler, method, path, body, secret string) (int, string) {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if secret != "" {
		r.Header.Set("X-Node-Secret", secret)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func TestGateSecret(t *testing.T) {
	f := newFake()
	f.gate.Secret = "s3cret"
	h := New(f).Handler()
	if code, _ := do(t, h, "GET", "/local/api/status", ""); code != 200 {
		t.Fatal("GET status must stay open without a secret")
	}
	if code, body := do(t, h, "POST", "/local/api/services/shairport-sync/install", `{}`); code != 403 || !strings.Contains(body, "X-Node-Secret") {
		t.Fatalf("missing secret: %d %s", code, body)
	}
	if code, _ := doWithSecret(t, h, "POST", "/local/api/services/shairport-sync/install", `{}`, "wrong"); code != 403 {
		t.Fatal("wrong secret must be rejected")
	}
	if code, body := doWithSecret(t, h, "POST", "/local/api/services/shairport-sync/install", `{"version":"4.3.2"}`, "s3cret"); code != 200 || !f.installed["shairport-sync"] {
		t.Fatalf("correct secret: %d %s", code, body)
	}
	// The standalone local console authenticates with the X-Local-UI marker.
	req := httptest.NewRequest("POST", "/local/api/services/shairport-sync/install", strings.NewReader(`{"version":"4.3.2"}`))
	req.Header.Set("X-Local-UI", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("local UI marker: %d %s", w.Code, w.Body.String())
	}
}

func TestGateAllowLocalFalse(t *testing.T) {
	f := newFake()
	f.gate.AllowLocal = false
	h := New(f).Handler()
	code, body := do(t, h, "POST", "/local/api/services/shairport-sync/install", `{}`)
	if code != 403 || !strings.Contains(body, "ALLOW_LOCAL=false") {
		t.Fatalf("allow_local gate: %d %s", code, body)
	}
	req := httptest.NewRequest("POST", "/local/api/services/shairport-sync/install", strings.NewReader(`{}`))
	req.Header.Set("X-Local-UI", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "ALLOW_LOCAL=false") {
		t.Fatalf("marker must not bypass ALLOW_LOCAL=false: %d %s", w.Code, w.Body.String())
	}
	if code, _ := do(t, h, "GET", "/local/api/status", ""); code != 200 {
		t.Fatal("GET must stay open when ALLOW_LOCAL=false")
	}
	if f.installed["shairport-sync"] {
		t.Fatal("install must not run when ALLOW_LOCAL=false")
	}
}

func TestGatePlayProtect(t *testing.T) {
	f := newFake()
	f.gate.Playing = true
	f.gate.PlayProtect = true
	h := New(f).Handler()
	for _, path := range []string{"/local/api/services/shairport-sync/install", "/local/api/power", "/local/api/os/upgrade", "/local/api/netdiag"} {
		code, body := do(t, h, "POST", path, `{"action":"reboot"}`)
		if code != 403 || !strings.Contains(body, "FR-3.10") {
			t.Fatalf("play-protect gate %s: %d %s", path, code, body)
		}
	}
	if f.powerAction != "" || f.osChecks != 0 || f.netdiags != 0 {
		t.Fatal("no mutation may run while play-protected")
	}
	f.gate.PlayProtect = false
	if code, _ := do(t, h, "POST", "/local/api/power", `{"action":"reboot"}`); code != 200 || f.powerAction != "reboot" {
		t.Fatal("playing without play_protect must be allowed")
	}
}

func TestSecretEndpoint(t *testing.T) {
	f := newFake()
	f.gate.Secret = "s3cret"
	h := New(f).Handler()
	// httptest.NewRequest uses a non-loopback RemoteAddr (192.0.2.1).
	if code, _ := do(t, h, "GET", "/local/api/secret", ""); code != 403 {
		t.Fatal("secret endpoint must reject non-loopback callers")
	}
	for _, addr := range []string{"127.0.0.1:54321", "[::1]:54321"} {
		r := httptest.NewRequest("GET", "/local/api/secret", nil)
		r.RemoteAddr = addr
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("loopback secret (%s): %d", addr, w.Code)
		}
		var m map[string]string
		json.Unmarshal([]byte(w.Body.String()), &m)
		if m["secret"] != "s3cret" {
			t.Fatalf("secret payload: %v", m)
		}
	}
}

func TestOSAndNetdiagRoutes(t *testing.T) {
	f := newFake()
	h := New(f).Handler()
	code, body := do(t, h, "POST", "/local/api/os/check", "")
	if code != 200 || !strings.Contains(body, "check log") || f.osChecks != 1 {
		t.Fatalf("os check: %d %s", code, body)
	}
	code, body = do(t, h, "POST", "/local/api/os/upgrade", `{"packages":["alsa-utils","curl"]}`)
	if code != 200 || !strings.Contains(body, "upgrade log") {
		t.Fatalf("os upgrade: %d %s", code, body)
	}
	if len(f.upgPackages) != 2 || f.upgPackages[0] != "alsa-utils" || f.upgPackages[1] != "curl" {
		t.Fatalf("upgrade packages: %v", f.upgPackages)
	}
	code, body = do(t, h, "POST", "/local/api/netdiag", "")
	if code != 200 || !strings.Contains(body, "netdiag log") || f.netdiags != 1 {
		t.Fatalf("netdiag: %d %s", code, body)
	}
}

func TestPlaybackRouteOpen(t *testing.T) {
	f := newFake()
	f.playback = PlaybackState{Connected: true, State: "playing", Playing: true, Title: "夜曲", PTS: 1000, Volume: 80, BufferMS: 250}
	h := New(f).Handler()
	code, body := do(t, h, "GET", "/local/api/playback", "")
	if code != 200 || !strings.Contains(body, "夜曲") || !strings.Contains(body, "playing") {
		t.Fatalf("playback GET: %d %s", code, body)
	}
	// Read stays open even when local mutation is disabled.
	f.gate.AllowLocal = false
	if code, _ := do(t, h, "GET", "/local/api/playback", ""); code != 200 {
		t.Fatal("playback GET must stay open when ALLOW_LOCAL=false")
	}
}

func TestPlaybackQueueRoutes(t *testing.T) {
	f := newFake()
	f.queue = serverclient.QueueState{NodeID: "n-1", Position: 1,
		Tracks: []serverclient.QueueTrack{{TrackID: "t1"}, {TrackID: "t2"}}}
	f.gate.Playing = true
	f.gate.PlayProtect = true
	h := New(f).Handler()
	if code, body := do(t, h, "GET", "/local/api/playback/queue", ""); code != 200 || !strings.Contains(body, `"position":1`) {
		t.Fatalf("queue get: %d %s", code, body)
	}
	// prev/next are playback controls: allowed while play-protection blocks others.
	if code, _ := do(t, h, "POST", "/local/api/playback/next", ""); code != 200 || f.nextCalls != 1 {
		t.Fatalf("next during playback: %d", code)
	}
	if code, _ := do(t, h, "POST", "/local/api/playback/prev", ""); code != 200 || f.prevCalls != 1 {
		t.Fatalf("prev during playback: %d", code)
	}
	f.gate.AllowLocal = false
	if code, _ := do(t, h, "POST", "/local/api/playback/next", ""); code != 403 {
		t.Fatal("next must honor ALLOW_LOCAL")
	}
	f.gate.AllowLocal = true
	f.queueErr = errors.New("server offline")
	if code, _ := do(t, h, "GET", "/local/api/playback/queue", ""); code != 502 {
		t.Fatalf("queue read error: %d", code)
	}
	if code, body := do(t, h, "POST", "/local/api/playback/next", ""); code != 400 || !strings.Contains(body, "server offline") {
		t.Fatalf("next error: %d %s", code, body)
	}
}

func TestPlaybackControlsBypassPlayProtect(t *testing.T) {
	f := newFake()
	f.gate.Playing = true
	f.gate.PlayProtect = true
	h := New(f).Handler()

	// Playback controls are playback itself (PROTOCOL.md §11): they must work
	// while playing, unlike install/power which the play gate blocks.
	if code, body := do(t, h, "POST", "/local/api/playback/pause", ""); code != 200 || !strings.Contains(body, "paused") || f.pauseCalls != 1 {
		t.Fatalf("pause during playback: %d %s", code, body)
	}
	if code, _ := do(t, h, "POST", "/local/api/playback/stop", ""); code != 200 || f.stopCalls != 1 {
		t.Fatalf("stop during playback: %d", code)
	}
	if code, _ := do(t, h, "POST", "/local/api/playback/volume", `{"volume":50}`); code != 200 || f.volumeSet != 50 {
		t.Fatalf("volume during playback: %d", code)
	}
	if code, _ := do(t, h, "POST", "/local/api/playback/seek", `{"pts":12345}`); code != 200 || f.seekPTS != 12345 {
		t.Fatalf("seek during playback: %d", code)
	}
	if code, _ := do(t, h, "POST", "/local/api/playback/device", `{"device":"hw:1,0"}`); code != 200 || f.deviceSet != "hw:1,0" {
		t.Fatalf("device during playback: %d", code)
	}

	// Non-playback mutations remain blocked by FR-3.10.
	if code, body := do(t, h, "POST", "/local/api/services/shairport-sync/install", `{}`); code != 403 || !strings.Contains(body, "FR-3.10") {
		t.Fatalf("install must be play-protected: %d %s", code, body)
	}
	if code, _ := do(t, h, "POST", "/local/api/power", `{"action":"reboot"}`); code != 403 {
		t.Fatal("power must be play-protected")
	}
	if f.installed["shairport-sync"] || f.powerAction != "" {
		t.Fatal("no non-playback mutation may run while play-protected")
	}
}

func TestPlaybackControlsGated(t *testing.T) {
	f := newFake()
	h := New(f).Handler()

	// ALLOW_LOCAL=false blocks playback controls too.
	f.gate.AllowLocal = false
	if code, body := do(t, h, "POST", "/local/api/playback/stop", ""); code != 403 || !strings.Contains(body, "ALLOW_LOCAL=false") {
		t.Fatalf("allow_local gate on playback: %d %s", code, body)
	}
	if f.stopCalls != 0 {
		t.Fatal("stop must not run when ALLOW_LOCAL=false")
	}

	// Node secret required when set.
	f.gate.AllowLocal = true
	f.gate.Secret = "s3cret"
	if code, _ := do(t, h, "POST", "/local/api/playback/stop", ""); code != 403 {
		t.Fatal("missing secret must block playback control")
	}
	if code, _ := doWithSecret(t, h, "POST", "/local/api/playback/stop", "", "wrong"); code != 403 {
		t.Fatal("wrong secret must block playback control")
	}
	if code, _ := doWithSecret(t, h, "POST", "/local/api/playback/stop", "", "s3cret"); code != 200 || f.stopCalls != 1 {
		t.Fatalf("correct secret: %d", code)
	}
}

func TestPlaybackErrorPropagates(t *testing.T) {
	f := newFake()
	f.playbackErr = errors.New("未连接到服务端 ZAT")
	h := New(f).Handler()
	code, body := do(t, h, "POST", "/local/api/playback/stop", "")
	if code != 400 || !strings.Contains(body, "ZAT") {
		t.Fatalf("expected 400 playback error, got %d %s", code, body)
	}
}
