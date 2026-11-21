// Package discover finds the management server again after its DHCP address
// changes. It scans the terminal's own /24, and accepts a server only when the
// reply carries an HMAC signed with this terminal's node secret, so a rogue LAN
// host cannot redirect the agent.
package discover

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"hifihub/agent/internal/serveraddr"
)

// Settings wires discovery to the running agent.
type Settings struct {
	NodeID     func() string
	Secret     func() string
	CurrentURL func() string
	DataDir    string
	HTTPPort   int
	Interval   time.Duration
	Restart    func() error
	Logf       func(string, ...any)
}

type reply struct {
	ServerID string `json:"server_id"`
	ZATPort  int    `json:"zat_port"`
	HMAC     string `json:"hmac"`
}

// Run probes the configured server and scans for it while unreachable.
func Run(ctx context.Context, s Settings) {
	if s.Interval <= 0 {
		s.Interval = 20 * time.Second
	}
	if s.HTTPPort <= 0 {
		s.HTTPPort = 8080
	}
	if s.Logf == nil {
		s.Logf = func(string, ...any) {}
	}
	for {
		discoverOnce(ctx, s)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.Interval):
		}
	}
}

func discoverOnce(ctx context.Context, s Settings) {
	nodeID := s.NodeID()
	secret := s.Secret()
	if nodeID == "" || secret == "" {
		return
	}
	current := s.CurrentURL()
	if current != "" && healthy(ctx, current) {
		return
	}
	foundURL, zatAddr, ok := scan(ctx, nodeID, secret, s.HTTPPort, s.Logf)
	if !ok {
		return
	}
	if err := serveraddr.Save(s.DataDir, serveraddr.State{ServerURL: foundURL, ZATAddr: zatAddr}); err != nil {
		s.Logf("discover: persist %s: %v", foundURL, err)
	}
	if foundURL == current {
		return
	}
	s.Logf("discover: server moved to %s", foundURL)
	if s.Restart != nil {
		if err := s.Restart(); err != nil {
			s.Logf("discover: restart after switch: %v", err)
		}
	}
}

func healthy(ctx context.Context, base string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/api/v1/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func scan(ctx context.Context, nodeID, secret string, port int, logf func(string, ...any)) (string, string, bool) {
	nonceRaw := make([]byte, 16)
	if _, err := rand.Read(nonceRaw); err != nil {
		return "", "", false
	}
	nonce := hex.EncodeToString(nonceRaw)
	client := &http.Client{Timeout: 700 * time.Millisecond}
	type result struct {
		url  string
		zat  string
		done bool
	}
	found := make(chan result, 1)
	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	ips := candidateIPs()
	logf("discover: probing %d hosts on :%d", len(ips), port)
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
			u := fmt.Sprintf("http://%s:%d/api/v1/discover?node_id=%s&nonce=%s",
				ip, port, url.QueryEscape(nodeID), url.QueryEscape(nonce))
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
			var r reply
			if json.NewDecoder(resp.Body).Decode(&r) != nil {
				return
			}
			if !verify(nodeID, nonce, ip, r, secret) {
				return
			}
			base := fmt.Sprintf("http://%s:%d", ip, port)
			zat := net.JoinHostPort(ip, strconv.Itoa(r.ZATPort))
			select {
			case found <- result{url: base, zat: zat, done: true}:
				cancel()
			default:
			}
		}(ip)
	}
	go func() { wg.Wait(); close(found) }()
	for r := range found {
		if r.done {
			return r.url, r.zat, true
		}
	}
	return "", "", false
}

// verify checks the server's HMAC over this terminal's id, nonce and the
// discovered ZAT port.
func verify(nodeID, nonce, host string, r reply, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%s|%s|%s|%d", nodeID, nonce, r.ServerID, host, r.ZATPort)
	want, err := hex.DecodeString(r.HMAC)
	if err != nil {
		return false
	}
	return hmac.Equal(mac.Sum(nil), want)
}

func candidateIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
				continue
			}
			mask := ipnet.Mask
			if len(mask) != 4 {
				continue
			}
			network := ip.Mask(mask)
			// Scan the /24 containing this interface address; larger LANs still
			// cover the usual flat home network without a 65k-host probe.
			prefix := network[:3]
			for host := 1; host < 254; host++ {
				cand := net.IPv4(prefix[0], prefix[1], prefix[2], byte(host))
				s := cand.String()
				if s == ip.String() || seen[s] {
					continue
				}
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}
