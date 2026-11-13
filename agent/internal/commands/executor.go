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

// Executor runs terminal commands. playing/playProtect gate os-upgrade
// (FR-3.10): while playing and play-protection is on, upgrades are refused.
type Executor struct {
	run         systemctrl.Runner
	serverURL   string
	playing     func() bool
	playProtect func() bool
	rebootPath  string
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
	default:
		return serverclient.CommandResult{Status: "err", Log: fmt.Sprintf("unknown command type %q", cmd.Type)}
	}
}

func (e *Executor) hasApt() bool {
	_, err := exec.LookPath("apt-get")
	return err == nil
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
