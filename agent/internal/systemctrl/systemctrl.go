// Package systemctrl wraps systemd service management and power actions behind
// a restricted whitelist (NFR-S5). The agent runs as root but may only start,
// stop, enable or disable units it knows about, and may only reboot/shutdown
// when explicitly allowed. A Runner interface allows tests to inject a fake.
package systemctrl

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"hifihub/agent/internal/model"
)

// Runner executes a system command and returns combined output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner runs commands via os/exec with a timeout.
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

// UnitStatus is the observed state of one systemd unit.
type UnitStatus struct {
	Unit        string `json:"unit"`
	LoadState   string `json:"load_state"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	UnitFile    string `json:"unit_file_state"`
	Enabled     bool   `json:"enabled"`
	MainPID     int    `json:"main_pid"`
	Description string `json:"description"`
}

// Running reports whether the unit is active.
func (u UnitStatus) Running() bool { return u.ActiveState == "active" }

// Controller manages a whitelist of units plus optional power actions.
type Controller struct {
	run        Runner
	allowed    map[string]bool
	allowPower bool
}

// New builds a Controller whose unit whitelist is derived from the service
// registry. Power actions are gated by allowPower.
func New(run Runner, allowPower bool) *Controller {
	allowed := map[string]bool{}
	for _, def := range model.Registry {
		for _, u := range def.Units {
			allowed[u] = true
		}
	}
	return &Controller{run: run, allowed: allowed, allowPower: allowPower}
}

// AllowUnit adds a unit to the whitelist (e.g. a catalog-declared custom unit).
func (c *Controller) AllowUnit(unit string) { c.allowed[unit] = true }

func (c *Controller) checkUnit(unit string) error {
	if !c.allowed[unit] {
		return fmt.Errorf("unit %q is not in the agent whitelist (NFR-S5)", unit)
	}
	return nil
}

// Available reports whether systemctl can be invoked on this host.
func (c *Controller) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.run.Run(ctx, "systemctl", "--version")
	return err == nil
}

// Status reads the full state of a unit via `systemctl show`.
func (c *Controller) Status(unit string) (UnitStatus, error) {
	if err := c.checkUnit(unit); err != nil {
		return UnitStatus{}, err
	}
	out, err := c.run.Run(context.Background(), "systemctl", "show", unit,
		"--property=LoadState,ActiveState,SubState,MainPID,Description,UnitFileState")
	if err != nil {
		return UnitStatus{}, fmt.Errorf("systemctl show %s: %w (%s)", unit, err, strings.TrimSpace(out))
	}
	us := UnitStatus{Unit: unit}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "LoadState":
			us.LoadState = v
		case "ActiveState":
			us.ActiveState = v
		case "SubState":
			us.SubState = v
		case "UnitFileState":
			us.UnitFile = v
			us.Enabled = v == "enabled" || v == "enabled-runtime" || v == "static"
		case "Description":
			us.Description = v
		case "MainPID":
			fmt.Sscanf(v, "%d", &us.MainPID)
		}
	}
	return us, nil
}

func (c *Controller) verb(unit, verb string) error {
	if err := c.checkUnit(unit); err != nil {
		return err
	}
	out, err := c.run.Run(context.Background(), "systemctl", verb, unit)
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w (%s)", verb, unit, err, strings.TrimSpace(out))
	}
	return nil
}

// Start starts a unit.
func (c *Controller) Start(unit string) error { return c.verb(unit, "start") }

// Stop stops a unit.
func (c *Controller) Stop(unit string) error { return c.verb(unit, "stop") }

// Enable enables a unit at boot.
func (c *Controller) Enable(unit string) error { return c.verb(unit, "enable") }

// Disable disables a unit at boot.
func (c *Controller) Disable(unit string) error { return c.verb(unit, "disable") }

// DaemonReload reloads systemd manager configuration after unit files change.
func (c *Controller) DaemonReload() error {
	out, err := c.run.Run(context.Background(), "systemctl", "daemon-reload")
	if err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// Reboot reboots the terminal (gated by allowPower).
func (c *Controller) Reboot() error {
	if !c.allowPower {
		return fmt.Errorf("power actions are disabled on this terminal")
	}
	out, err := c.run.Run(context.Background(), "systemctl", "reboot")
	if err != nil {
		return fmt.Errorf("reboot: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// Shutdown powers off the terminal (gated by allowPower).
func (c *Controller) Shutdown() error {
	if !c.allowPower {
		return fmt.Errorf("power actions are disabled on this terminal")
	}
	out, err := c.run.Run(context.Background(), "systemctl", "poweroff")
	if err != nil {
		return fmt.Errorf("poweroff: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}
