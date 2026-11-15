package zat

import (
	"errors"
	"testing"
)

func TestSessionHappyPath(t *testing.T) {
	s := NewSession("s-1", "n-1")
	if s.State != StateIdle {
		t.Fatalf("initial state = %s", s.State)
	}
	steps := []struct {
		msg  Type
		want State
	}{
		{TypeHello, StateAuthenticated},
		{TypeSetup, StateReady},
		{TypePlay, StatePlaying},
		{TypePause, StatePaused},
		{TypePlay, StatePlaying},
		{TypeSeek, StatePlaying},
		{TypeStop, StateStopped},
		{TypeSetup, StateReady},
		{TypePlay, StatePlaying},
	}
	for _, st := range steps {
		if err := s.Apply(st.msg); err != nil {
			t.Fatalf("apply %s: %v", st.msg, err)
		}
		if s.State != st.want {
			t.Fatalf("after %s state = %s want %s", st.msg, s.State, st.want)
		}
	}
}

func TestSessionPlayingFlag(t *testing.T) {
	s := NewSession("s", "n")
	if s.State.Playing() {
		t.Fatal("idle should not be playing")
	}
	_ = s.Apply(TypeHello)
	_ = s.Apply(TypeSetup)
	_ = s.Apply(TypePlay)
	if !s.State.Playing() {
		t.Fatal("playing state should report Playing()")
	}
	_ = s.Apply(TypePause)
	if s.State.Playing() {
		t.Fatal("paused should not report Playing()")
	}
}

func TestSessionIllegalTransition(t *testing.T) {
	s := NewSession("s", "n")
	err := s.Apply(TypePlay) // can't play from idle
	var bt ErrBadTransition
	if !errors.As(err, &bt) {
		t.Fatalf("want ErrBadTransition, got %v", err)
	}
	if bt.State != StateIdle || bt.Type != TypePlay {
		t.Fatalf("bad transition detail: %+v", bt)
	}
	if s.State != StateIdle {
		t.Fatalf("state changed on illegal transition: %s", s.State)
	}
}

func TestSessionTeardownResets(t *testing.T) {
	for _, st := range []State{StateIdle, StateAuthenticated, StateReady, StatePlaying, StatePaused, StateStopped} {
		s := NewSession("s", "n")
		s.State = st
		if err := s.Apply(TypeTeardown); err != nil {
			t.Fatalf("teardown from %s: %v", st, err)
		}
		if s.State != StateIdle {
			t.Fatalf("teardown from %s → %s, want idle", st, s.State)
		}
	}
}

func TestSessionReset(t *testing.T) {
	s := NewSession("s", "n")
	s.State = StatePlaying
	s.Reset()
	if s.State != StateIdle {
		t.Fatalf("reset → %s", s.State)
	}
}

func TestSessionCanPlay(t *testing.T) {
	s := NewSession("s", "n")
	if s.CanPlay() {
		t.Fatal("idle cannot play")
	}
	_ = s.Apply(TypeHello)
	_ = s.Apply(TypeSetup)
	if !s.CanPlay() {
		t.Fatal("ready can play")
	}
}
