// Command hifihub-server is the Zephyr HiFi Kit management backend (FR-1..FR-9).
// This increment delivers the distribution core: catalog, artifact repository,
// BOM gates, node registry and the provision state machine with the play gate.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"hifihub/server/internal/alerts"
	"hifihub/server/internal/api"
	"hifihub/server/internal/aptsrc"
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
	"hifihub/server/internal/serverpush"
	"hifihub/server/internal/signature"
	"hifihub/server/internal/store"
	"hifihub/server/internal/task"
	"hifihub/server/internal/update"
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
	aptSrc := aptsrc.New(st)
	if err := aptSrc.Seed(); err != nil {
		logger.Warn("seed apt sources", "err", err)
	}
	if err := cat.SeedAptApps(); err != nil {
		logger.Warn("seed open-source apps", "err", err)
	}
	if err := cat.SeedCommercialApps(); err != nil {
		logger.Warn("seed commercial apps", "err", err)
	}
	if err := cat.SeedComponentLibrary(); err != nil {
		logger.Warn("seed component library", "err", err)
	}
	if err := cat.SeedAppOrigins(); err != nil {
		logger.Warn("seed app origins", "err", err)
	}
	aud := audit.New(st)
	alts := alerts.New(st, nodes, lic, cmds)
	tasks := task.New(st, nodes, boms, cat, cfg.PlayGateEnabled, cfg.IdleWaitTimeout)
	rolls := rollouts.New(st, nodes, tasks, boms)
	met := metrics.New(cfg.DBPath)

	// Media library (SMB mounts + scanned tracks) and the ZAT push-playback
	// server: the Roon-like core of Zephyr HiFi Kit. Mount dirs are a
	// privileged, optional concern (kernel mount.cifs needs root), so create
	// them best-effort: the management core must start unprivileged. Each mount
	// point is created lazily at mount time by the library service.
	if err := os.MkdirAll(cfg.MountDir, 0o755); err != nil {
		logger.Warn("mount dir unavailable; SMB mounts require root", "dir", cfg.MountDir, "err", err)
	}
	if err := os.MkdirAll(cfg.MountCredDir, 0o700); err != nil {
		logger.Warn("mount cred dir unavailable", "dir", cfg.MountCredDir, "err", err)
	}
	mntMgr := mount.NewBackend(mount.ExecRunner{}, cfg.MountCredDir)
	lib := library.New(st, mntMgr, cfg.MountDir)
	if err := lib.EnsureArtistIndex(context.Background()); err != nil {
		logger.Warn("artist index", "err", err)
	}
	zat := zatserver.New(cfg.ZATAddr, nodes, lib, st)

	// Version convergence: self-upgrade from the official release feed and poll
	// terminals to this server's version. Restart swaps the running unit via
	// systemd; without systemd the upgrade stages and waits for a manual restart.
	var restart func() error
	if _, err := exec.LookPath("systemctl"); err == nil {
		restart = func() error {
			return exec.Command("systemctl", "restart", "hifihub-server.service").Start()
		}
	}
	upd := update.New(st, nodes, cmds, tasks, lic, verifier, update.NewFeed(cfg.UpdateFeedURL), update.Options{
		Current:      metrics.Version,
		FeedURL:      cfg.UpdateFeedURL,
		SelfEnabled:  cfg.UpdateSelfEnabled,
		AutoApply:    cfg.UpdateAutoApply,
		FleetEnabled: cfg.UpdateFleetEnabled,
		Interval:     cfg.UpdateInterval,
		MaxParallel:  cfg.UpdateMaxParallel,
		MaxAttempts:  cfg.UpdateMaxAttempts,
		RingPercent:  cfg.UpdateRingPercent,
		DistDir:      cfg.DistDir,
		StageDir:     cfg.UpdateStageDir,
		Restart:      restart,
	})

	a := api.New(cfg, api.Deps{
		Cat: cat, Repo: rp, Boms: boms, Nodes: nodes, Tasks: tasks,
		License: lic, Keys: ks, Audit: aud, Alerts: alts, Commands: cmds, AptSrc: aptSrc,
		Rollouts: rolls, Metrics: met, Library: lib, ZAT: zat, Update: upd,
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

	// Server-driven address configuration: when a terminal is offline (for
	// example after the server's DHCP address changed), push our reachable
	// address to its local console. The agent proves its identity first.
	go pushLoop(ctx, nodes, cfg)
	// Recover saved music sources after a reboot (Windows loses its SMB session).
	go reconnectSources(ctx, lib)

	go backgroundLoop(ctx, nodes, tasks, alts)
	go updateLoop(ctx, upd, cfg.UpdateInterval)

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

// updateLoop drives version convergence on its own cadence: one immediate pass
// at boot, then every interval. Tick self-checks the feed (auto-applying when
// enabled and idle) and iterates the fleet toward this server's version.
func updateLoop(ctx context.Context, upd *update.Service, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	upd.Tick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			upd.Tick(ctx)
		}
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

// pushLoop periodically offers the current server address to offline terminals.
func pushLoop(ctx context.Context, nodes *node.Service, cfg config.Config) {
	last := map[string]time.Time{}
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		pushOffline(ctx, nodes, cfg, last)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func pushOffline(ctx context.Context, nodes *node.Service, cfg config.Config, last map[string]time.Time) {
	ns, err := nodes.List()
	if err != nil {
		return
	}
	for _, n := range ns {
		if !n.Authorized || n.Secret == "" || n.IP == "" || n.Status == "online" {
			continue
		}
		if time.Since(last[n.NodeID]) < 45*time.Second {
			continue
		}
		last[n.NodeID] = time.Now()
		go func(n domain.Node) {
			pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			res, err := serverpush.Push(pctx, n.NodeID, n.IP, n.Secret, listenPort(cfg.ListenAddr), listenPort(cfg.ZATAddr))
			if err != nil {
				slog.Debug("server endpoint push", "node", n.NodeID, "ip", n.IP, "err", err)
				return
			}
			if res.Changed {
				slog.Info("server endpoint pushed", "node", n.NodeID, "url", res.ServerURL)
			}
		}(n)
	}
}

func listenPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// reconnectSources restores shares whose saved state is mounted. Failures are
// retried a few times because a NAS may still be booting when the server starts.
func reconnectSources(ctx context.Context, lib *library.Service) {
	for attempt := 1; attempt <= 3; attempt++ {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		attempted, restored, failures := lib.Reconnect(rctx)
		cancel()
		if attempted > 0 {
			slog.Info("music sources reconnect", "attempt", attempt, "attempted", attempted, "restored", restored, "failed", len(failures))
		}
		for _, f := range failures {
			slog.Warn("music source reconnect failed", "err", f)
		}
		if len(failures) == 0 || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}
