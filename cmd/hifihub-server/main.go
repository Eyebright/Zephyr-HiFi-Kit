// Command hifihub-server is the HiFiHub management backend (FR-1..FR-9).
// This increment delivers the distribution core: catalog, artifact repository,
// BOM gates, node registry and the provision state machine with the play gate.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hifihub/server/internal/alerts"
	"hifihub/server/internal/api"
	"hifihub/server/internal/audit"
	"hifihub/server/internal/bom"
	"hifihub/server/internal/catalog"
	"hifihub/server/internal/commands"
	"hifihub/server/internal/config"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/keys"
	"hifihub/server/internal/library"
	"hifihub/server/internal/license"
	"hifihub/server/internal/metrics"
	"hifihub/server/internal/mount"
	"hifihub/server/internal/node"
	"hifihub/server/internal/repo"
	"hifihub/server/internal/rollouts"
	"hifihub/server/internal/signature"
	"hifihub/server/internal/store"
	"hifihub/server/internal/task"
	"hifihub/server/internal/web"
	"hifihub/server/internal/zatserver"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()
	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		logger.Error("create artifacts dir", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	verifier := signature.New(cfg.SignatureMode, cfg.GPGHome)
	cat := catalog.New(st)
	rp := repo.New(cfg.ArtifactsDir, verifier)
	boms := bom.New(st, cat)
	nodes := node.New(st, cfg.OfflineAfter)
	lic := license.New(st, verifier)
	ks := keys.New(cfg.GPGHome, st)
	cmds := commands.New(st)
	aud := audit.New(st)
	alts := alerts.New(st, nodes, lic, cmds)
	tasks := task.New(st, nodes, boms, cat, lic, cfg.PlayGateEnabled, cfg.IdleWaitTimeout)
	rolls := rollouts.New(st, nodes, tasks, boms)
	met := metrics.New(cfg.DBPath)

	// Media library (SMB mounts + scanned tracks) and the ZAT push-playback
	// server: the Roon-like core of Zephyr Music Player. Mount dirs are a
	// privileged, optional concern (kernel mount.cifs needs root), so create
	// them best-effort: the management core must start unprivileged. Each mount
	// point is created lazily at mount time by the library service.
	if err := os.MkdirAll(cfg.MountDir, 0o755); err != nil {
		logger.Warn("mount dir unavailable; SMB mounts require root", "dir", cfg.MountDir, "err", err)
	}
	if err := os.MkdirAll(cfg.MountCredDir, 0o700); err != nil {
		logger.Warn("mount cred dir unavailable", "dir", cfg.MountCredDir, "err", err)
	}
	mntMgr := mount.NewManager(mount.ExecRunner{}, cfg.MountCredDir)
	lib := library.New(st, mntMgr, cfg.MountDir)
	zat := zatserver.New(cfg.ZATAddr, nodes, lib, st)

	a := api.New(cfg, api.Deps{
		Cat: cat, Repo: rp, Boms: boms, Nodes: nodes, Tasks: tasks,
		License: lic, Keys: ks, Audit: aud, Alerts: alts, Commands: cmds,
		Rollouts: rolls, Metrics: met, Library: lib, ZAT: zat,
	})

	// Root mux: REST API under /api/v1/, agent distribution files under /dl/,
	// self metrics under /metrics/, embedded management console at /.
	root := http.NewServeMux()
	root.Handle("/api/v1/", a.Handler())
	root.Handle("/dl/", a.Handler())
	root.Handle("/metrics/", a.Handler())
	root.Handle("/", web.Handler())

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go backgroundLoop(ctx, nodes, tasks, alts)

	go func() {
		logger.Info("zat server listening", "addr", cfg.ZATAddr)
		if err := zat.ListenAndServe(ctx); err != nil {
			logger.Error("zat server", "err", err)
		}
	}()

	go func() {
		logger.Info("hifihub-server listening",
			"addr", cfg.ListenAddr,
			"db", cfg.DBPath,
			"artifacts", cfg.ArtifactsDir,
			"dist", cfg.DistDir,
			"signature_mode", cfg.SignatureMode,
			"play_gate", cfg.PlayGateEnabled)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
}

// backgroundLoop sweeps stale nodes offline, re-checks jobs waiting for idle
// and refreshes the deduplicated alert rules.
func backgroundLoop(ctx context.Context, nodes *node.Service, tasks *task.Service, alts *alerts.Service) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := nodes.SweepOffline(); err == nil && n > 0 {
				slog.Info("marked nodes offline", "count", n)
			}
			if _, err := alts.Scan(); err != nil {
				slog.Warn("alert scan", "err", err)
			}
			jobs, err := tasks.List("")
			if err != nil {
				continue
			}
			for _, j := range jobs {
				if j.State == domain.JobWaitingIdle {
					if _, err := tasks.Advance(j.ID); err != nil {
						slog.Warn("recheck waiting job", "job", j.ID, "err", err)
					}
				}
			}
		}
	}
}
