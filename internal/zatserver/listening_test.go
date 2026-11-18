package zatserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hifihub/server/internal/store"
	"hifihub/zat"
)

func listeningStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "listening.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	_, err = st.DB.Exec(`INSERT INTO play_history(id,node_id,track_id,started_at,measured) VALUES('h','n','t',0,1)`)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func listeningTotal(t *testing.T, st *store.Store) int64 {
	t.Helper()
	var ms int64
	if err := st.DB.QueryRow(`SELECT COALESCE(SUM(listen_ms),0) FROM listen_daily`).Scan(&ms); err != nil {
		t.Fatal(err)
	}
	return ms
}

func TestListeningClockGatesAndDiscontinuities(t *testing.T) {
	st := listeningStore(t)
	start := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	var clock listeningClock
	clock.start("h", 1000, start)
	report := func(second int, pts uint64, state string, want int64) {
		t.Helper()
		if err := clock.observe(st.DB, zat.Status{PTS: pts, State: state}, start.Add(time.Duration(second)*time.Second)); err != nil {
			t.Fatal(err)
		}
		if got := listeningTotal(t, st); got != want {
			t.Fatalf("at %ds: listening=%d want %d", second, got, want)
		}
	}
	report(1, 1000, "playing", 1000)
	report(2, 1000, "playing", 1000)
	report(3, 2000, "playing", 2000)
	clock.gate(false)
	report(4, 3000, "playing", 2000)
	report(5, 3000, "paused", 2000)
	clock.gate(true)
	report(6, 3000, "playing", 2000)
	report(7, 4000, "playing", 3000)
	clock.gate(false)
	report(8, 90000, "playing", 3000)
	clock.gate(true)
	report(9, 90000, "playing", 3000)
	report(10, 91000, "playing", 4000)
	report(11, 1000, "playing", 4000)
	report(12, 2000, "playing", 5000)
	report(13, 80000, "playing", 5000)
	report(20, 87000, "playing", 5000)
	report(21, 88000, "playing", 6000)
	report(22, 89000, "stopped", 7000)
	report(23, 90000, "stopped", 7000)
	clock.gate(false)
	report(24, 91000, "playing", 7000)
}

func TestListeningChunkedProgress(t *testing.T) {
	st := listeningStore(t)
	start := time.Now()
	var clock listeningClock
	clock.start("h", 1000, start)
	for i, pts := range []uint64{371, 1113, 1484, 1855, 2597, 2968} {
		if err := clock.observe(st.DB, zat.Status{PTS: pts, State: "playing"}, start.Add(time.Duration(i+1)*500*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if got := listeningTotal(t, st); got != 2968 {
		t.Fatalf("chunked reports lost rendered time: %d", got)
	}
}

func TestListeningMidnightAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "listening.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO play_history(id,node_id,track_id,started_at,measured) VALUES('h','n','t',0,1)`); err != nil {
		t.Fatal(err)
	}
	zone := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 11, 17, 23, 59, 59, 500000000, zone)
	var clock listeningClock
	clock.start("h", 44100, start)
	if err := clock.observe(st.DB, zat.Status{PTS: 44100, State: "playing"}, start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.DB.Query(`SELECT day,listen_ms FROM listen_daily ORDER BY day`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var n int
	for rows.Next() {
		var day, ms int64
		if err := rows.Scan(&day, &ms); err != nil {
			t.Fatal(err)
		}
		wantDay := time.Date(2026, 11, 17+n, 0, 0, 0, 0, zone).Unix()
		if ms != 500 || day != wantDay {
			t.Fatalf("bucket %d: day=%d ms=%d", n, day, ms)
		}
		n++
	}
	if rows.Err() != nil || n != 2 {
		t.Fatalf("buckets=%d err=%v", n, rows.Err())
	}
}

func TestListeningIgnoresStaleSessionAndDisconnectedStatus(t *testing.T) {
	st := listeningStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ps := &playState{ctx: ctx, sessionID: "current"}
	start := time.Now()
	ps.listening.start("h", 1000, start)
	e := &endpoint{srv: &Server{db: st.DB}, play: ps, state: zat.StatePlaying}
	e.acceptStatus(zat.Status{SessionID: "stale", State: "stopped", PTS: 1000}, start.Add(time.Second))
	if e.state != zat.StatePlaying || e.pts != 0 || listeningTotal(t, st) != 0 {
		t.Fatal("stale session changed playback or duration")
	}
	e.acceptStatus(zat.Status{SessionID: "current", State: "playing", PTS: 1000}, start.Add(time.Second))
	if listeningTotal(t, st) != 1000 {
		t.Fatal("active session was not counted")
	}
	cancel()
	e.acceptStatus(zat.Status{SessionID: "current", State: "playing", PTS: 2000}, start.Add(2*time.Second))
	if listeningTotal(t, st) != 1000 {
		t.Fatal("disconnected session counted")
	}
}
