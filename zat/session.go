package zat

import "fmt"

// State is a ZAT session state (PROTOCOL.md §7).
type State string

const (
	StateIdle          State = "idle"
	StateAuthenticated State = "authenticated"
	StateReady         State = "ready"
	StatePlaying       State = "playing"
	StatePaused        State = "paused"
	StateStopped       State = "stopped"
)

// Playing reports whether audio is actively rendering (drives the play gate).
func (s State) Playing() bool { return s == StatePlaying }

// transitions maps (state, message) → next state for the happy path.
var transitions = map[State]map[Type]State{
	StateIdle: {
		TypeHello: StateAuthenticated,
	},
	StateAuthenticated: {
		TypeSetup: StateReady,
	},
	StateReady: {
		TypePlay:  StatePlaying,
		TypeSetup: StateReady,
		TypeStop:  StateStopped,
	},
	StatePlaying: {
		TypePause: StatePaused,
		TypeSeek:  StatePlaying,
		TypeStop:  StateStopped,
		TypeSetup: StateReady,
	},
	StatePaused: {
		TypePlay:  StatePlaying,
		TypeSeek:  StatePaused,
		TypeStop:  StateStopped,
		TypeSetup: StateReady,
	},
	StateStopped: {
		TypeSetup: StateReady,
	},
}

// Session tracks one ZAT session's state. It is transport-agnostic and pure so
// both ends can unit-test the same state machine.
type Session struct {
	ID     string
	NodeID string
	State  State
	Setup  *Setup
	PTS    uint64
	Volume int
}

// NewSession starts an idle session.
func NewSession(id, nodeID string) *Session {
	return &Session{ID: id, NodeID: nodeID, State: StateIdle, Volume: 100}
}

// ErrBadTransition is returned for an illegal (state, message) pair.
type ErrBadTransition struct {
	State State
	Type  Type
}

func (e ErrBadTransition) Error() string {
	return fmt.Sprintf("zat: illegal transition %s + %s", e.State, e.Type)
}

// Apply advances the session by one received message type. Teardown always
// resets to idle; fatal errors are handled by the caller via Reset.
func (s *Session) Apply(t Type) error {
	if t == TypeTeardown {
		s.State = StateIdle
		return nil
	}
	if next, ok := transitions[s.State][t]; ok {
		s.State = next
		return nil
	}
	return ErrBadTransition{State: s.State, Type: t}
}

// Reset forces the session back to idle (fatal error / disconnect).
func (s *Session) Reset() { s.State = StateIdle }

// CanPlay reports whether a PLAY message is legal in the current state.
func (s *Session) CanPlay() bool {
	_, ok := transitions[s.State][TypePlay]
	return ok
}
