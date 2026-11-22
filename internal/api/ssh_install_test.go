package api

import (
	"errors"
	"strings"
	"testing"
)

func TestSSHTargetValidation(t *testing.T) {
	ok, err := sshTarget("192.168.0.123", "pi", "secret", "", "", 22)
	if err != nil || ok.Host != "192.168.0.123" || ok.Port != 22 {
		t.Fatalf("valid target rejected: %v", err)
	}
	bad := []struct {
		host, user, pass string
		port             int
	}{
		{"", "pi", "x", 22},
		{"host name", "pi", "x", 22},
		{"192.168.0.1; rm -rf /", "pi", "x", 22},
		{"192.168.0.1", "p i", "x", 22},
		{"192.168.0.1", "pi", "", 0},
		{"192.168.0.1", "pi", "x", 70000},
	}
	for _, c := range bad {
		if _, err := sshTarget(c.host, c.user, c.pass, "", "", c.port); err == nil {
			t.Fatalf("target accepted: %+v", c)
		}
	}
	if _, err := sshTarget("192.168.0.1", "pi", "", "-----BEGIN OPENSSH PRIVATE KEY-----\n", "", 22); err != nil {
		t.Fatalf("key auth rejected: %v", err)
	}
}

func TestValidateServerURL(t *testing.T) {
	if got, err := validateServerURL("http://192.168.0.138:8080/"); err != nil || got != "http://192.168.0.138:8080" {
		t.Fatalf("valid url: %q %v", got, err)
	}
	for _, raw := range []string{"", "ftp://x", "http://", "http://user@host", "http://host/path", "http://host?q=1", "javascript:alert(1)"} {
		if _, err := validateServerURL(raw); err == nil {
			t.Fatalf("url accepted: %q", raw)
		}
	}
}

func TestSSHInstallJobStates(t *testing.T) {
	j := newSSHInstallJob()
	j.advance("connect", "ok")
	j.advance("download", "ok")
	j.finish(errors.New("下载安装脚本失败"))
	snap := j.snapshot()
	if snap["state"] != "failed" {
		t.Fatalf("state = %v", snap["state"])
	}
	steps := snap["steps"].([]sshJobStep)
	if steps[0].State != "done" || steps[1].State != "failed" || steps[2].State != "pending" {
		t.Fatalf("steps = %+v", steps)
	}
	if !strings.Contains(snap["error"].(string), "下载") {
		t.Fatalf("error = %v", snap["error"])
	}

	j2 := newSSHInstallJob()
	for _, s := range sshStages {
		j2.advance(s, "")
	}
	j2.finish(nil)
	if j2.snapshot()["state"] != "done" {
		t.Fatal("expected done state")
	}
	for _, s := range j2.snapshot()["steps"].([]sshJobStep) {
		if s.State != "done" {
			t.Fatalf("step %s = %s", s.Name, s.State)
		}
	}
}
