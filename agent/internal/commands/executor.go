// Package commands executes terminal commands (os-check, os-upgrade, netdiag).
// The same Executor serves both the server command poll loop and the local
// console routes, so local and server-driven actions behave identically. Parsers
// and result shaping are pure functions and unit-tested; live probes use only
// the standard library.
package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hifihub/agent/internal/serverclient"
	"hifihub/agent/internal/systemctrl"
)

// CheckData is the os-check / os-upgrade payload.
type CheckData struct {
	LastCheck      int64     `json:"last_check"`
	RebootRequired bool      `json:"reboot_required"`
	Packages       []Package `json:"packages"`
}

// Executor runs terminal commands. playing/playProtect gate os-upgrade and
// agent-upgrade (FR-3.10): while playing and play-protection is on, upgrades are
// refused.
type Executor struct {
	run         systemctrl.Runner
	serverURL   string
	playing     func() bool
	playProtect func() bool
	rebootPath  string

	// agent-upgrade inputs. download fetches a distribution path from this
	// agent's own server; arch/currentVersion enable the idempotency check that
	// stops a post-restart re-poll from reinstalling forever.
	arch           string
	currentVersion string
	dataDir        string
	download       func(ctx context.Context, urlPath, dest string) (string, error)

	// apt-source / apt-install filesystem targets (overridable in tests).
	aptKeyringDir string
	aptListDir    string
	osReleasePath string
	lookPath      func(string) (string, error)
}

// New builds a command Executor. run executes apt/system commands; serverURL is
// the management server (probed by netdiag); playing/playProtect supply the
// real-time playback gate.
func New(run systemctrl.Runner, serverURL string, playing, playProtect func() bool) *Executor {
	return &Executor{
		run:         run,
		serverURL:   serverURL,
		playing:     playing,
		playProtect: playProtect,
		rebootPath:  "/var/run/reboot-required",
	}
}

// SetRebootPath overrides the reboot-required marker path (used by tests).
func (e *Executor) SetRebootPath(p string) { e.rebootPath = p }

// DownloadFunc fetches a server-root-relative distribution path to dest and
// returns the computed sha256 hex.
type DownloadFunc func(ctx context.Context, urlPath, dest string) (string, error)

// SetUpgrade wires the agent-upgrade inputs: the terminal's own arch and running
// version, the artifact staging dir, and the server download function.
func (e *Executor) SetUpgrade(arch, currentVersion, dataDir string, download DownloadFunc) {
	e.arch = arch
	e.currentVersion = currentVersion
	e.dataDir = dataDir
	e.download = download
}

// upgradeArchs bounds agent-upgrade to the Linux deb architectures; Android
// terminals converge through the APK channel instead.
var upgradeArchs = map[string]bool{"amd64": true, "arm64": true, "armhf": true}

// Execute runs one command and returns its result. It never panics out: a
// failing command yields an err result so the poll loop keeps going.
func (e *Executor) Execute(ctx context.Context, cmd serverclient.Command) (result serverclient.CommandResult) {
	defer func() {
		if r := recover(); r != nil {
			result = serverclient.CommandResult{Status: "err", Log: fmt.Sprintf("command panic: %v", r)}
		}
	}()
	switch cmd.Type {
	case "os-check":
		return e.osCheck(ctx)
	case "os-upgrade":
		return e.osUpgrade(ctx, cmd.Params.Packages)
	case "netdiag":
		return e.netdiag(ctx)
	case "agent-upgrade":
		return e.agentUpgrade(ctx, cmd.Params)
	case "apt-source":
		return e.aptSource(ctx, cmd.Params.Sources)
	case "apt-install":
		return e.aptInstall(ctx, cmd.Params.Packages)
	default:
		return serverclient.CommandResult{Status: "err", Log: fmt.Sprintf("unknown command type %q", cmd.Type)}
	}
}

func (e *Executor) hasApt() bool {
	_, err := e.lookup("apt-get")
	return err == nil
}

func (e *Executor) lookup(name string) (string, error) {
	if e.lookPath != nil {
		return e.lookPath(name)
	}
	return exec.LookPath(name)
}

func (e *Executor) rebootRequired() bool {
	_, err := os.Stat(e.rebootPath)
	return err == nil
}

// collectCheck runs apt-get update + apt list --upgradable and builds CheckData,
// appending all output to log. It returns an error on non-apt systems or when
// the update step fails.
func (e *Executor) collectCheck(ctx context.Context, log *strings.Builder) (CheckData, error) {
	if !e.hasApt() {
		return CheckData{}, fmt.Errorf("此系统不支持 apt（非 Debian/Ubuntu），无法检查系统更新")
	}
	uctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	out, err := e.run.Run(uctx, "apt-get", "update", "-qq")
	log.WriteString("$ apt-get update -qq\n")
	log.WriteString(out)
	if err != nil {
		log.WriteString(fmt.Sprintf("\napt-get update 失败: %v\n", err))
		return CheckData{}, err
	}
	lout, lerr := e.run.Run(ctx, "apt", "list", "--upgradable")
	log.WriteString("\n$ apt list --upgradable\n")
	log.WriteString(lout)
	pkgs := ParseUpgradable(lout)
	if pkgs == nil {
		pkgs = []Package{}
	}
	if lerr != nil {
		log.WriteString(fmt.Sprintf("\napt list 警告: %v\n", lerr))
	}
	return CheckData{LastCheck: time.Now().Unix(), RebootRequired: e.rebootRequired(), Packages: pkgs}, nil
}

func (e *Executor) osCheck(ctx context.Context) serverclient.CommandResult {
	var log strings.Builder
	data, err := e.collectCheck(ctx, &log)
	if err != nil {
		return serverclient.CommandResult{Status: "err", Log: log.String()}
	}
	return serverclient.CommandResult{Status: "ok", Log: log.String(), Data: data}
}

func (e *Executor) osUpgrade(ctx context.Context, packages []string) serverclient.CommandResult {
	var log strings.Builder
	if e.playing != nil && e.playing() && e.playProtect != nil && e.playProtect() {
		return serverclient.CommandResult{Status: "err", Log: "FR-3.10：终端正在播放，请先停止播放再升级"}
	}
	if !e.hasApt() {
		return serverclient.CommandResult{Status: "err", Log: "此系统不支持 apt（非 Debian/Ubuntu），无法升级系统"}
	}
	// Empty package list means "all upgradable".
	if len(packages) == 0 {
		out, _ := e.run.Run(ctx, "apt", "list", "--upgradable")
		for _, p := range ParseUpgradable(out) {
			packages = append(packages, p.Name)
		}
	}
	status := "ok"
	if len(packages) == 0 {
		log.WriteString("没有可升级的软件包，系统已是最新。\n")
	} else {
		args := append([]string{"install", "--only-upgrade", "-y"}, packages...)
		uctx, cancel := context.WithTimeout(ctx, 900*time.Second)
		defer cancel()
		log.WriteString("$ apt-get " + strings.Join(args, " ") + "\n")
		out, err := e.run.Run(uctx, "apt-get", args...)
		log.WriteString(out)
		if err != nil {
			log.WriteString(fmt.Sprintf("\napt-get upgrade 失败: %v\n", err))
			status = "err"
		}
	}
	// Re-run the check for fresh data regardless of the upgrade outcome.
	data, cerr := e.collectCheck(ctx, &log)
	if cerr != nil && status == "ok" {
		status = "err"
	}
	return serverclient.CommandResult{Status: status, Log: log.String(), Data: data}
}

// UpgradeData is the agent-upgrade result payload.
type UpgradeData struct {
	Version   string `json:"version"`
	Arch      string `json:"arch"`
	SHA256    string `json:"sha256"`
	Installed bool   `json:"installed"`
}

// agentUpgrade installs the exact signed-chain agent deb the server pinned,
// verifies its sha256 before dpkg touches it, and lets the deb's postinst restart
// the unit. It is idempotent by version so the post-restart re-poll of a still
// pending command reports success instead of reinstalling in a loop.
func (e *Executor) agentUpgrade(ctx context.Context, p serverclient.CommandParams) serverclient.CommandResult {
	var log strings.Builder
	fail := func(msg string) serverclient.CommandResult {
		log.WriteString(msg + "\n")
		return serverclient.CommandResult{Status: "err", Log: log.String()}
	}
	if e.playing != nil && e.playing() && e.playProtect != nil && e.playProtect() {
		return fail("FR-3.10：终端正在播放，请先停止播放再升级")
	}
	if !upgradeArchs[p.Arch] {
		return fail(fmt.Sprintf("架构 %q 不在终端自动更新范围（安卓经 APK 升级）", p.Arch))
	}
	if e.arch != "" && p.Arch != e.arch {
		return fail(fmt.Sprintf("命令架构 %q 与本机 %q 不符", p.Arch, e.arch))
	}
	if p.Version == "" || p.SHA256 == "" || p.URL == "" {
		return fail("升级命令缺少 version/sha256/url 参数")
	}
	if p.Version == e.currentVersion {
		log.WriteString(fmt.Sprintf("已运行目标版本 %s，无需重复安装。\n", p.Version))
		return serverclient.CommandResult{Status: "ok", Log: log.String(),
			Data: UpgradeData{Version: p.Version, Arch: p.Arch, SHA256: p.SHA256, Installed: false}}
	}
	if e.download == nil {
		return fail("终端未配置下载通道，无法自升级")
	}
	name := filepath.Base(p.URL)
	if !strings.HasSuffix(name, ".deb") || strings.Contains(name, "..") {
		return fail(fmt.Sprintf("非法的分发文件名 %q", name))
	}
	dir := e.dataDir
	if dir == "" {
		dir = "/var/lib/hifihub"
	}
	dest := filepath.Join(dir, "updates", name)
	uctx, cancel := context.WithTimeout(ctx, 600*time.Second)
	defer cancel()
	log.WriteString("$ download " + p.URL + "\n")
	got, err := e.download(uctx, p.URL, dest)
	if err != nil {
		return fail(fmt.Sprintf("下载失败: %v", err))
	}
	if !strings.EqualFold(got, p.SHA256) {
		_ = os.Remove(dest)
		return fail(fmt.Sprintf("SHA256 校验失败: got %s want %s", got, p.SHA256))
	}
	log.WriteString("sha256 校验通过: " + got + "\n")
	// Detach dpkg into a transient scope: the deb postinst restarts this very
	// unit, so a same-cgroup install would be killed mid-transaction.
	if _, lpErr := exec.LookPath("systemd-run"); lpErr == nil {
		out, rerr := e.run.Run(uctx, "systemd-run", "--scope", "--quiet", "dpkg", "-i", dest)
		log.WriteString("$ systemd-run --scope --quiet dpkg -i " + dest + "\n" + out)
		if rerr != nil {
			return fail(fmt.Sprintf("dpkg 安装失败: %v", rerr))
		}
	} else {
		out, rerr := e.run.Run(uctx, "dpkg", "-i", dest)
		log.WriteString("$ dpkg -i " + dest + "\n" + out)
		if rerr != nil {
			return fail(fmt.Sprintf("dpkg 安装失败: %v", rerr))
		}
	}
	log.WriteString("安装完成，deb postinst 将重启终端服务以加载新版本。\n")
	return serverclient.CommandResult{Status: "ok", Log: log.String(),
		Data: UpgradeData{Version: p.Version, Arch: p.Arch, SHA256: got, Installed: true}}
}
