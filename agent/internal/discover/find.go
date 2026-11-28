package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// FindServer locates a management server on the terminal's own /24 for first
// pairing, before any node secret exists. Without credentials the reply cannot
// be authenticated, so it only accepts the hifihub health marker; the one-time
// registration code the user types next is what actually binds this terminal.
func FindServer(ctx context.Context, port int) (string, bool) {
	if port <= 0 {
		port = 8080
	}
	return findServer(ctx, port, candidateIPs())
}

func findServer(ctx context.Context, port int, ips []string) (string, bool) {
	client := &http.Client{Timeout: 900 * time.Millisecond}
	type result struct {
		url string
		ok  bool
	}
	found := make(chan result, 1)
	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for _, ip := range ips {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-scanCtx.Done():
				return
			}
			defer func() { <-sem }()
			u := fmt.Sprintf("http://%s:%d/api/v1/health", ip, port)
			req, err := http.NewRequestWithContext(scanCtx, http.MethodGet, u, nil)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var h struct {
				Status  string `json:"status"`
				Service string `json:"service"`
			}
			if json.NewDecoder(resp.Body).Decode(&h) != nil {
				return
			}
			if h.Status != "ok" || h.Service != "hifihub-server" {
				return
			}
			select {
			case found <- result{url: fmt.Sprintf("http://%s:%d", ip, port), ok: true}:
				cancel()
			default:
			}
		}(ip)
	}
	go func() { wg.Wait(); close(found) }()
	for r := range found {
		if r.ok {
			return r.url, true
		}
	}
	return "", false
}
