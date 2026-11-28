package androidcore

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"time"
)

//go:embed assets/*
var consoleAssets embed.FS

// Handler returns the Android terminal's loopback playback console.
func Handler(c *Core) http.Handler {
	sub, err := fs.Sub(consoleAssets, "assets")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != "/" && r.URL.Path != "/index.html" && r.URL.Path != "/console.css" && r.URL.Path != "/console.js" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/index.html" {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		writeJSON(w, map[string]any{
			"identity": c.Identity(ctx),
			"playback": c.Playback(),
			"metrics":  c.Metrics(),
		})
	})
	mux.HandleFunc("/api/queue", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		q, err := c.PlaybackQueue(ctx)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, q)
	})
	mux.HandleFunc("/api/lyrics", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		l, err := c.PlaybackLyrics(ctx)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, l)
	})
	mux.HandleFunc("/api/pause", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		paused, err := c.Pause()
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"paused": paused})
	})
	mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if err := c.Stop(); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/seek", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		var body struct {
			PTS uint64 `json:"pts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := c.Seek(body.PTS); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/volume", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		var body struct {
			Volume int `json:"volume"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if body.Volume < 0 || body.Volume > 100 {
			writeError(w, http.StatusBadRequest, errVolumeRange)
			return
		}
		if err := c.Volume(body.Volume); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/prev", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := c.PlaybackPrev(ctx); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/next", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := c.PlaybackNext(ctx); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/jump", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		var body struct {
			Index int `json:"index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if body.Index < 0 {
			writeError(w, http.StatusBadRequest, errQueueIndex)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := c.PlaybackJump(ctx, body.Index); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	return mux
}

var (
	errVolumeRange = errors.New("volume must be in 0..100")
	errQueueIndex  = errors.New("queue index must not be negative")
)

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	return false
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// ServeConsole runs the console HTTP server on addr until ctx is cancelled.
func ServeConsole(ctx context.Context, c *Core, addr string) error {
	srv := &http.Server{Addr: addr, Handler: Handler(c)}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
