// Command hifihub-agent is the terminal agent: it serves the local web console
// on the terminal's IP, registers with the management server, reports state, and
// executes server-driven application jobs. It runs as a systemd service.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"hifihub/agent/internal/config"
	"hifihub/agent/internal/core"
	"hifihub/agent/internal/discover"
	"hifihub/agent/internal/ui"
)

func main() {
	cfg := config.Load()
	log.SetPrefix("[hifihub-agent] ")
	log.Printf("starting v%s arch=%s server=%s listen=%s", cfg.AgentVersion, cfg.Arch, cfg.ServerURL, cfg.ListenAddr)

	c, err := core.New(cfg)
	if err != nil {
		log.Fatalf("init core: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := c.Register(ctx); err != nil {
		log.Printf("registration pending: %v", err)
	} else if c.State().Snapshot().Registered {
		log.Printf("registered node=%s", c.State().Snapshot().NodeID)
		_ = c.Snapshot(ctx)
	}

	srv := &http.Server{Addr: cfg.ListenAddr, Handler: ui.New(c).Handler()}
	go func() {
		log.Printf("local console at http://%s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("console server: %v", err)
		}
	}()

	go loop(ctx, cfg.Heartbeat, func() {
		if err := c.Register(ctx); err != nil {
			return
		}
		if err := c.Heartbeat(ctx); err != nil {
			log.Printf("heartbeat: %v", err)
		}
	})
	go loop(ctx, cfg.Snapshot, func() {
		if err := c.Snapshot(ctx); err != nil {
			log.Printf("snapshot: %v", err)
		}
	})
	go loop(ctx, cfg.JobPoll, func() {
		if err := c.RunJobs(ctx); err != nil {
			log.Printf("jobs: %v", err)
		}
	})
	go loop(ctx, cfg.CommandPoll, func() {
		if err := c.RunCommands(ctx); err != nil {
			log.Printf("commands: %v", err)
		}
	})
	// ZAT sink: maintain the outbound audio connection (reconnects automatically).
	go c.RunZAT(ctx)

	// The server's LAN address is DHCP and may change. When it is unreachable,
	// scan the local subnet and switch to the address whose reply is signed with
	// this terminal's node secret.
	if cfg.Discovery {
		port := 8080
		if u, err := url.Parse(cfg.ServerURL); err == nil {
			if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
				port = p
			}
		}
		go discover.Run(ctx, discover.Settings{
			NodeID:     func() string { return c.State().Snapshot().NodeID },
			Secret:     c.Secret,
			CurrentURL: func() string { return cfg.ServerURL },
			DataDir:    cfg.DataDir,
			HTTPPort:   port,
			Interval:   20 * time.Second,
			Restart: func() error {
				return exec.Command("systemctl", "restart", "hifihub-agent.service").Start()
			},
			Logf: log.Printf,
		})
	}

	<-ctx.Done()
	log.Printf("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
}

// loop runs fn immediately and then on every tick until ctx is cancelled.
func loop(ctx context.Context, every time.Duration, fn func()) {
	if every <= 0 {
		every = 15 * time.Second
	}
	fn()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}
