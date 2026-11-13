package api

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// distNameRE whitelists the exact distributable file names; anything else
// (including path traversal attempts) is a 404.
var distNameRE = regexp.MustCompile(`^(hifihub-agent_[0-9.]+_(amd64|arm64|armhf)\.deb|agent-install\.sh|SHA256SUMS)$`)

func (a *API) downloadDist(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !distNameRE.MatchString(name) {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	f, err := os.Open(filepath.Join(a.cfg.DistDir, name))
	if err != nil {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		writeError(w, http.StatusNotFound, "文件不存在")
		return
	}
	switch {
	case strings.HasSuffix(name, ".deb"):
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
	case strings.HasSuffix(name, ".sh"):
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, fi.ModTime(), f)
}
