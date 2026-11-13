package systemctrl

import (
	"context"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls []string
	out   string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return f.out, f.err
}

func TestWhitelistRejectsUnknownUnit(t *testing.T) {
	c := New(&fakeRunner{}, true)
	if err := c.Start("evil.service"); err == nil {
		t.Fatal("expected whitelist rejection for unknown unit")
	}
	if err := c.Start("shairport-sync.service"); err != nil {
		t.Fatalf("known unit should be allowed: %v", err)
	}
}

func TestStatusParsing(t *testing.T) {
	f := &fakeRunner{out: "LoadState=loaded\nActiveState=active\nSubState=running\nMainPID=1234\nDescription=AirPlay\nUnitFileState=enabled\n"}
	c := New(f, true)
	us, err := c.Status("shairport-sync.service")
	if err != nil {
		t.Fatal(err)
	}
	if !us.Running() || !us.Enabled || us.MainPID != 1234 || us.SubState != "running" {
		t.Fatalf("status=%+v", us)
	}
}

func TestPowerGating(t *testing.T) {
	f := &fakeRunner{}
	off := New(f, false)
	if err := off.Reboot(); err == nil {
		t.Fatal("reboot must be refused when power disabled")
	}
	if err := off.Shutdown(); err == nil {
		t.Fatal("shutdown must be refused when power disabled")
	}
	on := New(f, true)
	if err := on.Reboot(); err != nil {
		t.Fatalf("reboot allowed: %v", err)
	}
	if err := on.Shutdown(); err != nil {
		t.Fatalf("shutdown allowed: %v", err)
	}
	if !strings.Contains(strings.Join(f.calls, ";"), "systemctl reboot") ||
		!strings.Contains(strings.Join(f.calls, ";"), "systemctl poweroff") {
		t.Fatalf("expected reboot+poweroff calls, got %v", f.calls)
	}
}
