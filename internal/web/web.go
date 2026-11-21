// Package web serves the embedded management console SPA (FR-9.2).
// The console is compiled into the binary so the server ships as a single file.
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed assets
var assets embed.FS

type assetFile struct {
	data []byte
	etag string
}

// Handler returns an http.Handler serving the embedded single-page console.
// Real asset files are served directly; unknown paths fall back to the SPA shell.
// Each response carries an ETag so an upgrade revalidates cached JavaScript
// instead of silently running the previous console.
func Handler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(err)
	}
	files := map[string]assetFile{}
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := strings.TrimPrefix(p, "./")
		raw, err := fs.ReadFile(sub, name)
		if err != nil {
			return nil
		}
		files[name] = assetFile{data: raw, etag: etag(raw)}
		return nil
	})
	indexETag := etag(index)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		file, ok := files[name]
		if !ok {
			if r.Header.Get("If-None-Match") == indexETag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("ETag", indexETag)
			_, _ = w.Write(index)
			return
		}
		if r.Header.Get("If-None-Match") == file.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", file.etag)
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(file.data))
	})
}

func etag(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}
