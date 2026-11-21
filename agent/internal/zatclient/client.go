// Package zatclient is the terminal Agent's ZAT sink: it dials out to the
// server's ZAT source (PROTOCOL.md §2), authenticates with the per-node secret,
// receives SETUP/PLAY/AUDIO and renders PCM through the player package (ALSA on
// Linux/CGO, a stub elsewhere). It reports STATUS every ~500ms, exposes a live
// Snapshot for the local console and heartbeat, and drives the FR-3.10 play gate
// via Playing(). Local pause/volume/seek act on the render pipeline; local stop
// drains ALSA and sends TEARDOWN so the source stops streaming.
package zatclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"hifihub/agent/internal/player"
	"hifihub/zat"
)

// Snapshot is the live playback state for the local console and heartbeat.
type Snapshot struct {
	Connected  bool   `json:"connected"`
	State      string `json:"state"`
	Playing    bool   `json:"playing"`
	SessionID  string `json:"session_id,omitempty"`
	TrackID    string `json:"track_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
	Codec      string `json:"codec,omitempty"`
	Rate       int    `json:"rate,omitempty"`
	Channels   int    `json:"channels,omitempty"`
	PTS        uint64 `json:"pts"`
	BufferMS   int    `json:"buffer_ms"`
	Volume     int    `json:"volume"`
	ALSADevice string `json:"alsa_device,omitempty"`
	PrefDevice string `json:"pref_device,omitempty"`
	Detail     string `json:"detail,omitempty"`
	MQA        bool   `json:"mqa,omitempty"`
	DoP        bool   `json:"dop,omitempty"`
	DSDRate    int    `json:"dsd_rate,omitempty"`
	BitPerfect bool   `json:"bit_perfect,omitempty"`
}

// Client maintains one outbound ZAT connection with automatic reconnect.
type Client struct {
	addr      string
	version   string
	creds     func() (nodeID, secret string)
	devices   func() []zat.ALSADevice
	newPlayer func() player.Player

	mu         sync.Mutex
	snap       Snapshot
	prefDevice string
	cur        *session
}

// New builds a sink client. addr is the server ZAT TCP address (host:port).
// creds returns the current node id and secret (read fresh on each reconnect so
// registration is picked up without restarting). devices lists ALSA playback
// devices for HELLO caps.
func New(addr, version string, creds func() (string, string), devices func() []zat.ALSADevice) *Client {
	return &Client{
		addr: addr, version: version, creds: creds, devices: devices,
		newPlayer: player.New,
		snap:      Snapshot{State: string(zat.StateIdle), Volume: 100},
	}
}

// WithPlayerFactory overrides the player constructor (tests inject a fake).
func (c *Client) WithPlayerFactory(f func() player.Player) *Client {
	c.newPlayer = f
	return c
}

// SetDevice sets a local preferred ALSA device that overrides the SETUP suggestion.
func (c *Client) SetDevice(dev string) {
	c.mu.Lock()
	c.prefDevice = dev
	c.mu.Unlock()
	c.setSnap(func(sn *Snapshot) { sn.PrefDevice = dev })
}

// PreferredDevice returns the local device override ("" = use server suggestion).
func (c *Client) PreferredDevice() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prefDevice
}

// Snapshot returns the current playback state.
func (c *Client) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

// Playing reports whether ZAT audio is actively rendering (drives the play gate).
func (c *Client) Playing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap.Playing
}

func (c *Client) setSnap(fn func(*Snapshot)) {
	c.mu.Lock()
	fn(&c.snap)
	c.snap.Playing = c.snap.State == string(zat.StatePlaying)
	c.mu.Unlock()
}

// Run is the reconnect loop: it waits for credentials, dials, authenticates and
// serves one connection until it drops, then backs off and retries. It returns
// when ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		nodeID, secret := c.creds()
		if nodeID == "" || secret == "" {
			if !waitCtx(ctx, 2*time.Second) {
				return
			}
			continue
		}
		err := c.connectAndServe(ctx, nodeID, secret)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("zat client: %v", err)
		}
		c.setSnap(func(s *Snapshot) {
			s.Connected = false
			s.State = string(zat.StateIdle)
			s.Detail = ""
		})
		if !waitCtx(ctx, backoff) {
			return
		}
		if backoff < 15*time.Second {
			backoff *= 2
		}
	}
}

func waitCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func secretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// connectAndServe dials, authenticates and runs one connection to completion.
func (c *Client) connectAndServe(ctx context.Context, nodeID, secret string) error {
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.addr, err)
	}
	defer conn.Close()
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}

	s := newSession(c, conn, nodeID)
	c.mu.Lock()
	c.cur = s
	c.mu.Unlock()
	sctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		c.mu.Lock()
		if c.cur == s {
			c.cur = nil
		}
		c.mu.Unlock()
		s.close()
	}()

	// HELLO -> HELLO_ACK (10s deadline, matching the source).
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	devs := c.devices()
	hws := make([]string, 0, len(devs))
	info := make([]zat.ALSADevice, 0, len(devs))
	for _, d := range devs {
		hws = append(hws, d.HW)
		info = append(info, d)
	}
	hello := zat.Hello{
		NodeID: nodeID, SecretSHA256: secretHash(secret), AgentVersion: c.version,
		Caps: zat.Caps{
			Codecs: []string{"pcm"}, Formats: []zat.Format{zat.S16LE, zat.S24LE, zat.S32LE},
			Rates: []int{44100, 48000, 88200, 96000, 176400, 192000}, MaxChannels: 2,
			ALSADevices: hws, ALSADeviceInfo: info, MQA: player.SupportsBitPerfect(), DoP: player.SupportsBitPerfect(),
		},
	}
	if err := s.write(zat.TypeHello, hello); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	fr, err := zat.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("hello ack: %w", err)
	}
	if fr.Type == zat.TypeError {
		var ze zat.Error
		_ = json.Unmarshal(fr.Payload, &ze)
		return fmt.Errorf("auth rejected: %s (%s)", ze.Code, ze.Detail)
	}
	if fr.Type != zat.TypeHelloAck {
		return fmt.Errorf("expected HELLO_ACK, got %s", fr.Type)
	}
	var ack zat.HelloAck
	if err := json.Unmarshal(fr.Payload, &ack); err != nil {
		return fmt.Errorf("hello ack decode: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	c.setSnap(func(sn *Snapshot) {
		sn.Connected = true
		sn.State = string(zat.StateAuthenticated)
		sn.Volume = 100
		sn.Detail = ""
	})

	s.start()
	return s.readLoop(sctx, ack.PrebufferMS)
}

// --- local playback controls (act on the current session) ---

func (c *Client) session() *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

// TogglePause pauses or resumes local rendering. Returns the new paused state.
func (c *Client) TogglePause() (bool, error) {
	s := c.session()
	if s == nil {
		return false, errors.New("未连接到服务端 ZAT")
	}
	return s.togglePause(), nil
}

// Stop drains ALSA and tears the session down so the source stops streaming.
func (c *Client) Stop() error {
	s := c.session()
	if s == nil {
		return errors.New("未连接到服务端 ZAT")
	}
	s.localStop()
	return nil
}

// Seek skips forward to a sample position within the streamed track.
func (c *Client) Seek(pts uint64) error {
	s := c.session()
	if s == nil {
		return errors.New("未连接到服务端 ZAT")
	}
	s.seek(pts)
	return nil
}

// SetVolume sets soft volume 0..100.
func (c *Client) SetVolume(v int) error {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	s := c.session()
	if s == nil {
		c.setSnap(func(sn *Snapshot) { sn.Volume = v })
		return nil
	}
	s.setVolume(v)
	return nil
}
