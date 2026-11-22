package zatserver

import (
	"errors"
	"testing"
	"time"
)

func TestSleepTimerSetCancelPersist(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "ep-sleep", true)

	stopAt, err := h.srv.SetSleepTimer(nodeID, 15)
	if err != nil {
		t.Fatal(err)
	}
	if stopAt <= time.Now().Unix() {
		t.Fatalf("deadline in the past: %d", stopAt)
	}
	if got := h.srv.SleepStopAt(nodeID); got != stopAt {
		t.Fatalf("SleepStopAt = %d, want %d", got, stopAt)
	}
	st, err := h.srv.Status(nodeID)
	if err != nil || st.SleepStopAt != stopAt {
		t.Fatalf("status sleep_stop_at: %d %v", st.SleepStopAt, err)
	}
	// Persisted for restart survival.
	var stored int64
	if err := h.st.DB.QueryRow(`SELECT stop_at FROM node_sleep_timer WHERE node_id=?`, nodeID).Scan(&stored); err != nil || stored != stopAt {
		t.Fatalf("persisted stop_at: %d %v", stored, err)
	}
	reloaded := New("127.0.0.1:0", h.nodes, h.lib, h.st)
	if got := reloaded.SleepStopAt(nodeID); got != stopAt {
		t.Fatalf("reloaded deadline = %d, want %d", got, stopAt)
	}

	if err := h.srv.CancelSleepTimer(nodeID); err != nil {
		t.Fatal(err)
	}
	if got := h.srv.SleepStopAt(nodeID); got != 0 {
		t.Fatalf("deadline after cancel = %d", got)
	}
	var n int
	if err := h.st.DB.QueryRow(`SELECT COUNT(*) FROM node_sleep_timer WHERE node_id=?`, nodeID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("row after cancel: %d %v", n, err)
	}
}

func TestSleepTimerValidationAndFire(t *testing.T) {
	h := setup(t)
	nodeID, _ := h.register(t, "ep-sleep2", true)

	for _, m := range []int{0, -1, 1441} {
		if _, err := h.srv.SetSleepTimer(nodeID, m); !errors.Is(err, ErrSleepMinutes) {
			t.Fatalf("minutes %d accepted: %v", m, err)
		}
	}
	if _, err := h.srv.SetSleepTimer("trk-missing-node", 15); err == nil {
		t.Fatal("unknown node accepted")
	}

	stopAt, err := h.srv.SetSleepTimer(nodeID, 30)
	if err != nil {
		t.Fatal(err)
	}
	// The terminal is not connected in this test: firing still clears the timer.
	h.srv.fireSleepTimers(stopAt + 1)
	if got := h.srv.SleepStopAt(nodeID); got != 0 {
		t.Fatalf("deadline after fire = %d", got)
	}
	var n int
	if err := h.st.DB.QueryRow(`SELECT COUNT(*) FROM node_sleep_timer WHERE node_id=?`, nodeID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("row after fire: %d %v", n, err)
	}
	// A not-yet-due timer survives a check.
	if _, err := h.srv.SetSleepTimer(nodeID, 30); err != nil {
		t.Fatal(err)
	}
	h.srv.fireSleepTimers(time.Now().Unix())
	if h.srv.SleepStopAt(nodeID) == 0 {
		t.Fatal("early check cleared a pending timer")
	}
}
