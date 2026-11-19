package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hifihub/agent/internal/serverclient"
)

// AppliedSource reports one repository file written by apt-source.
type AppliedSource struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Suite string `json:"suite"`
	File  string `json:"file"`
}

// AptSourceData is the apt-source result payload.
type AptSourceData struct {
	Sources []AppliedSource `json:"sources"`
}

// InstalledPkg is one package version reported after apt-install.
type InstalledPkg struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// AptInstallData is the apt-install result payload.
type AptInstallData struct {
	Installed []InstalledPkg `json:"installed"`
}

var aptIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var aptPkgRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*$`)

// SetAptPaths overrides the apt filesystem targets (used by tests).
func (e *Executor) SetAptPaths(keyringDir, listDir, osRelease string) {
	e.aptKeyringDir = keyringDir
	e.aptListDir = listDir
	e.osReleasePath = osRelease
}

func (e *Executor) keyringDir() string {
	if e.aptKeyringDir != "" {
		return e.aptKeyringDir
	}
	return "/usr/share/keyrings"
}

func (e *Executor) listDir() string {
	if e.aptListDir != "" {
		return e.aptListDir
	}
	return "/etc/apt/sources.list.d"
}

func (e *Executor) osReleaseFile() string {
	if e.osReleasePath != "" {
		return e.osReleasePath
	}
	return "/etc/os-release"
}

// codename resolves the terminal's Debian codename, preferring
// VERSION_CODENAME and falling back to the PRETTY_NAME parenthetical.
func (e *Executor) codename() string {
	b, err := os.ReadFile(e.osReleaseFile())
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	for _, ln := range lines {
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "VERSION_CODENAME="); ok {
			return strings.ToLower(strings.Trim(strings.TrimSpace(v), `"`))
		}
	}
	for _, ln := range lines {
		v, ok := strings.CutPrefix(strings.TrimSpace(ln), "PRETTY_NAME=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if i := strings.LastIndex(v, "("); i >= 0 {
			return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(v[i+1:], ")")))
		}
	}
	return ""
}

// aptSource writes one signed-by sources.list.d file per spec (downloading the
// repository keyring first) and refreshes the package index.
func (e *Executor) aptSource(ctx context.Context, specs []serverclient.AptSourceSpec) serverclient.CommandResult {
	var log strings.Builder
	fail := func(msg string) serverclient.CommandResult {
		log.WriteString(msg + "\n")
		return serverclient.CommandResult{Status: "err", Log: log.String()}
	}
	if !e.hasApt() {
		return fail("此系统不支持 apt（非 Debian/Ubuntu），无法配置 APT 源")
	}
	if len(specs) == 0 {
		return fail("没有需要应用的 APT 源")
	}
	data := AptSourceData{Sources: []AppliedSource{}}
	for _, sp := range specs {
		if !aptIDRe.MatchString(sp.ID) {
			return fail(fmt.Sprintf("非法的 APT 源 ID %q", sp.ID))
		}
		if !strings.HasPrefix(sp.URL, "http://") && !strings.HasPrefix(sp.URL, "https://") {
			return fail(fmt.Sprintf("源 %s 的仓库 URL 非法", sp.Name))
		}
		for _, f := range []string{sp.URL, sp.KeyURL, sp.Components} {
			if strings.ContainsAny(f, " \t\r\n") {
				return fail(fmt.Sprintf("源 %s 的字段包含空白字符", sp.Name))
			}
		}
		suite := sp.Suite
		if suite == "" {
			suite = e.codename()
		}
		if !aptPkgRe.MatchString(suite) {
			return fail(fmt.Sprintf("无法识别终端的 Debian 代号（源 %s）", sp.Name))
		}
		comp := sp.Components
		if comp == "" {
			comp = "main"
		}
		keyring := filepath.Join(e.keyringDir(), "zephyr-"+sp.ID+".gpg")
		if sp.KeyURL != "" {
			if err := e.fetchKey(ctx, sp.KeyURL, keyring, &log); err != nil {
				return fail(fmt.Sprintf("源 %s 密钥获取失败: %v", sp.Name, err))
			}
		}
		line := "deb "
		if sp.KeyURL != "" {
			line += "[signed-by=" + keyring + "] "
		}
		line += sp.URL + " " + suite + " " + comp + "\n"
		file := filepath.Join(e.listDir(), "zephyr-"+sp.ID+".list")
		if err := os.MkdirAll(e.listDir(), 0o755); err != nil {
			return fail(fmt.Sprintf("创建 sources.list.d 失败: %v", err))
		}
		if err := os.WriteFile(file, []byte(line), 0o644); err != nil {
			return fail(fmt.Sprintf("写入源文件失败: %v", err))
		}
		log.WriteString("$ write " + file + "\n" + line)
		data.Sources = append(data.Sources, AppliedSource{ID: sp.ID, Name: sp.Name, Suite: suite, File: file})
	}
	uctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	log.WriteString("$ apt-get update -qq\n")
	out, err := e.run.Run(uctx, "apt-get", "update", "-qq")
	log.WriteString(out)
	if err != nil {
		log.WriteString(fmt.Sprintf("\napt-get update 失败: %v\n", err))
		return serverclient.CommandResult{Status: "err", Log: log.String(), Data: data}
	}
	return serverclient.CommandResult{Status: "ok", Log: log.String(), Data: data}
}

// fetchKey downloads a repository keyring. Armored keys are dearmored with gpg
// so apt's signed-by always points at a binary keyring.
func (e *Executor) fetchKey(ctx context.Context, url, dest string, log *strings.Builder) error {
	uctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(uctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	log.WriteString("$ download key " + url + "\n")
	if !strings.HasPrefix(string(body), "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
		return os.WriteFile(dest, body, 0o644)
	}
	if _, lpErr := e.lookup("gpg"); lpErr != nil {
		return fmt.Errorf("密钥为 armored 格式但系统缺少 gpg")
	}
	tmp := dest + ".asc"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)
	out, rerr := e.run.Run(uctx, "gpg", "--dearmor", "-o", dest, tmp)
	log.WriteString("$ gpg --dearmor -o " + dest + "\n" + out)
	if rerr != nil {
		return rerr
	}
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("gpg --dearmor 未生成 keyring")
	}
	return nil
}

// aptInstall installs distro packages, repairing broken dependencies once the
// same way the deb installer does, then reports the installed versions.
func (e *Executor) aptInstall(ctx context.Context, packages []string) serverclient.CommandResult {
	var log strings.Builder
	fail := func(msg string) serverclient.CommandResult {
		log.WriteString(msg + "\n")
		return serverclient.CommandResult{Status: "err", Log: log.String()}
	}
	if e.playing != nil && e.playing() && e.playProtect != nil && e.playProtect() {
		return fail("FR-3.10：终端正在播放，请先停止播放再安装音频服务")
	}
	if !e.hasApt() {
		return fail("此系统不支持 apt（非 Debian/Ubuntu），无法安装软件包")
	}
	if len(packages) == 0 {
		return fail("没有需要安装的软件包")
	}
	for _, p := range packages {
		if !aptPkgRe.MatchString(p) {
			return fail(fmt.Sprintf("非法的软件包名 %q", p))
		}
	}
	uctx, cancel := context.WithTimeout(ctx, 900*time.Second)
	defer cancel()
	args := append([]string{"install", "-y"}, packages...)
	log.WriteString("$ apt-get " + strings.Join(args, " ") + "\n")
	out, err := e.run.Run(uctx, "apt-get", args...)
	log.WriteString(out)
	if err != nil {
		log.WriteString("\n$ apt-get -f install -y（依赖修复）\n")
		fctx, fcancel := context.WithTimeout(ctx, 300*time.Second)
		fout, ferr := e.run.Run(fctx, "apt-get", "-f", "install", "-y")
		fcancel()
		log.WriteString(fout)
		if ferr == nil {
			log.WriteString("\n$ apt-get " + strings.Join(args, " ") + "（重试）\n")
			out2, err2 := e.run.Run(uctx, "apt-get", args...)
			log.WriteString(out2)
			err = err2
		}
		if err != nil {
			log.WriteString(fmt.Sprintf("\napt-get install 失败: %v\n", err))
			return serverclient.CommandResult{Status: "err", Log: log.String()}
		}
	}
	data := AptInstallData{Installed: []InstalledPkg{}}
	qargs := append([]string{"-W", "-f=${Package}\t${Version}\n"}, packages...)
	qout, qerr := e.run.Run(ctx, "dpkg-query", qargs...)
	log.WriteString("$ dpkg-query -W " + strings.Join(packages, " ") + "\n" + qout)
	if qerr != nil {
		log.WriteString(fmt.Sprintf("dpkg-query 警告: %v\n", qerr))
	}
	for _, ln := range strings.Split(qout, "\n") {
		name, ver, ok := strings.Cut(ln, "\t")
		if ok && name != "" {
			data.Installed = append(data.Installed, InstalledPkg{Name: name, Version: ver})
		}
	}
	return serverclient.CommandResult{Status: "ok", Log: log.String(), Data: data}
}
