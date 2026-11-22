// Package remote drives one-off SSH sessions used by the add-terminal wizard.
// Credentials live only in the caller's memory; nothing here persists or logs
// them. Every command that embeds user input is POSIX-quoted, and output shown
// to users is redacted for the registration token.
package remote

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Target describes how to reach a terminal over SSH.
type Target struct {
	Host         string
	Port         int
	User         string
	Password     string
	PrivateKey   string
	Passphrase   string
	SudoPassword string
}

// Info is what a probe learned about the remote host.
type Info struct {
	Fingerprint string `json:"fingerprint"`
	HostKey     string `json:"host_key"`
	Hostname    string `json:"hostname"`
	Arch        string `json:"arch"`
	OSRelease   string `json:"os_release"`
	Sudo        string `json:"sudo"` // passwordless | password
	HasCurl     bool   `json:"has_curl"`
	HasAgent    bool   `json:"has_agent"`
	User        string `json:"user"`
}

var ErrSudoRequired = errors.New("远程用户需要 sudo 密码，请填写密码或改用有免密 sudo 的账号")

// Probe connects once, captures the host key for user confirmation and
// gathers the facts needed to choose an agent package.
func Probe(ctx context.Context, t Target) (Info, error) {
	var key ssh.PublicKey
	cl, err := dial(ctx, t, func(_ string, _ net.Addr, k ssh.PublicKey) error {
		key = k
		return nil
	})
	if err != nil {
		return Info{}, err
	}
	defer cl.Close()
	if key == nil {
		return Info{}, errors.New("未获取到主机密钥")
	}
	info := Info{
		Fingerprint: ssh.FingerprintSHA256(key),
		HostKey:     base64.StdEncoding.EncodeToString(key.Marshal()),
		User:        t.User,
	}
	for _, c := range []struct {
		cmd string
		dst *string
	}{
		{"hostname", &info.Hostname},
		{"uname -m", &info.Arch},
		{`. /etc/os-release 2>/dev/null && printf '%s' "$PRETTY_NAME"`, &info.OSRelease},
		{"if sudo -n true 2>/dev/null; then echo passwordless; else echo password; fi", &info.Sudo},
	} {
		out, err := run(ctx, cl, c.cmd, nil, 15*time.Second)
		if err != nil {
			return Info{}, fmt.Errorf("远程命令失败（%s）：%w", c.cmd, err)
		}
		*c.dst = strings.TrimSpace(out)
	}
	if out, err := run(ctx, cl, "command -v curl >/dev/null && echo yes || echo no", nil, 15*time.Second); err == nil {
		info.HasCurl = strings.TrimSpace(out) == "yes"
	}
	if out, err := run(ctx, cl, "command -v hifihub-agent >/dev/null && echo yes || echo no", nil, 15*time.Second); err == nil {
		info.HasAgent = strings.TrimSpace(out) == "yes"
	}
	return info, nil
}

// Install pins the host key confirmed by the user, downloads the install
// script from serverURL and runs it with the one-time registration token.
// progress receives coarse stage names (connect, download, install, verify).
func Install(ctx context.Context, t Target, fingerprint, serverURL, token string, progress func(stage, detail string)) error {
	verify := func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if ssh.FingerprintSHA256(k) != fingerprint {
			return fmt.Errorf("主机指纹不匹配：期望 %s，实际 %s", fingerprint, ssh.FingerprintSHA256(k))
		}
		return nil
	}
	cl, err := dial(ctx, t, verify)
	if err != nil {
		return err
	}
	defer cl.Close()
	step := func(stage, detail string) {
		if progress != nil {
			progress(stage, redact(detail, token))
		}
	}
	step("connect", "SSH 已连接，主机指纹校验通过")

	out, err := run(ctx, cl, "id -u", nil, 15*time.Second)
	if err != nil {
		return fmt.Errorf("查询远程用户失败：%w", err)
	}
	root := strings.TrimSpace(out) == "0"

	sudoPrefix := "sudo -n"
	sudoStdin := []byte(nil)
	if !root {
		if _, serr := run(ctx, cl, "sudo -n true", nil, 15*time.Second); serr != nil {
			pw := t.SudoPassword
			if pw == "" && t.Password != "" {
				pw = t.Password
			}
			if pw == "" {
				return ErrSudoRequired
			}
			sudoPrefix = "sudo -S -p ''"
			sudoStdin = []byte(pw + "\n")
		}
	}

	scriptURL := strings.TrimRight(serverURL, "/") + "/dl/agent-install.sh"
	step("download", "正在下载安装脚本")
	if out, err := run(ctx, cl, "curl -fsSL --max-time 120 "+quote(scriptURL)+" -o /tmp/zephyr-agent-install.sh", nil, 140*time.Second); err != nil {
		return fmt.Errorf("下载安装脚本失败：%w%s", err, tail(out, token))
	}

	runCmd := "bash /tmp/zephyr-agent-install.sh --server " + quote(serverURL) + " --token " + quote(token)
	if !root {
		runCmd = sudoPrefix + " " + runCmd
	}
	step("install", "正在安装终端 Agent")
	out, err = run(ctx, cl, runCmd, sudoStdin, 8*time.Minute)
	if err != nil {
		return fmt.Errorf("安装脚本执行失败：%w%s", err, tail(out, token))
	}

	step("verify", "安装完成，等待终端上线")
	ver, _ := run(ctx, cl, "command -v hifihub-agent", nil, 15*time.Second)
	if strings.TrimSpace(ver) == "" {
		return errors.New("安装脚本执行完毕，但未找到 hifihub-agent，请检查安装日志")
	}
	return nil
}

func dial(ctx context.Context, t Target, hostKey ssh.HostKeyCallback) (*ssh.Client, error) {
	var auth []ssh.AuthMethod
	if strings.TrimSpace(t.PrivateKey) != "" {
		var (
			signer ssh.Signer
			err    error
		)
		if t.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(t.PrivateKey), []byte(t.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(t.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("私钥解析失败：%w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if t.Password != "" {
		auth = append(auth, ssh.Password(t.Password))
	}
	if len(auth) == 0 {
		return nil, errors.New("需要 SSH 密码或私钥")
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	cfg := &ssh.ClientConfig{User: t.User, Auth: auth, HostKeyCallback: hostKey, Timeout: 15 * time.Second}
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败：%w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH 握手失败：%w", err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func run(ctx context.Context, cl *ssh.Client, cmd string, stdin []byte, timeout time.Duration) (string, error) {
	sess, err := cl.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var buf bytes.Buffer
	sess.Stdout = &buf
	sess.Stderr = &buf
	if len(stdin) > 0 {
		sess.Stdin = bytes.NewReader(stdin)
	}
	if err := sess.Start(cmd); err != nil {
		return buf.String(), err
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case err := <-done:
		return buf.String(), err
	case <-tctx.Done():
		sess.Close()
		<-done
		return buf.String(), fmt.Errorf("远程命令超时（%s）", timeout)
	}
}

// quote wraps s in single quotes for POSIX shells.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func tail(out, token string) string {
	out = strings.TrimSpace(redact(out, token))
	if out == "" {
		return ""
	}
	if len(out) > 1500 {
		out = "…" + out[len(out)-1500:]
	}
	return "：" + out
}

func redact(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "***")
	}
	return s
}
