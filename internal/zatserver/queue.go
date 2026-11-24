package zatserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"
)

// Per-device playback queue. The queue lives server-side (one row per playback
// target); the terminal's prev/next controls and the console both drive it, and
// natural end-of-track advances it automatically.

var (
	// ErrQueueEmpty is returned when prev/next targets a node with no queue.
	ErrQueueEmpty = errors.New("播放队列为空")
	// ErrQueueEnd is returned when next is pressed on the last queue entry.
	ErrQueueEnd = errors.New("已是队列最后一首")
	// ErrQueueIndex is returned when a jump index is out of range.
	ErrQueueIndex = errors.New("队列位置无效")
	// ErrQueueMode is returned when an unknown playback mode is requested.
	ErrQueueMode = errors.New("队列播放模式无效")
)

// Persisted per-device playback modes.
const (
	QueueModeSequence = "sequence" // stop at the end of the queue
	QueueModeLoopAll  = "loop_all" // wrap to the first track after the last
	QueueModeLoopOne  = "loop_one" // repeat the current track at natural EOF
	QueueModeShuffle  = "shuffle"  // follow a frozen shuffled order, wrapping
)

// ValidQueueMode reports whether mode is a supported playback mode.
func ValidQueueMode(mode string) bool {
	switch mode {
	case QueueModeSequence, QueueModeLoopAll, QueueModeLoopOne, QueueModeShuffle:
		return true
	}
	return false
}

// shuffleShuffle is a seam for tests; production uses the global math/rand/v2
// source, which is seeded from the OS at startup.
var shuffleShuffle = rand.Shuffle

// QueueTrack is one queue entry with resolved library metadata. Missing marks
// entries whose track disappeared from the library since enqueueing.
type QueueTrack struct {
	TrackID    string `json:"track_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int    `json:"duration_ms"`
	Codec      string `json:"codec"`
	CoverHash  string `json:"cover_hash,omitempty"`
	Decodable  bool   `json:"decodable"`
	MQA        bool   `json:"mqa"`
	Missing    bool   `json:"missing"`
}

// QueueState is the persisted queue plus resolved track metadata.
type QueueState struct {
	NodeID       string       `json:"node_id"`
	DeviceKey    string       `json:"device_key"`
	HW           string       `json:"hw"`
	Tracks       []QueueTrack `json:"tracks"`
	Position     int          `json:"position"`
	Mode         string       `json:"mode"`
	ShuffleOrder []string     `json:"shuffle_order,omitempty"`
	UpdatedAt    int64        `json:"updated_at"`
}

// queueRow is the raw persisted queue: track order, cursor and mode metadata.
type queueRow struct {
	IDs          []string
	Position     int
	UpdatedAt    int64
	Mode         string
	ShuffleOrder []string
}

func (s *Server) loadQueue(deviceKey string) (queueRow, error) {
	var idsJSON, mode, orderJSON string
	var pos int
	var updatedAt int64
	err := s.db.QueryRow(`SELECT track_ids,position,updated_at,mode,shuffle_order FROM playback_queue WHERE device_key=?`, deviceKey).
		Scan(&idsJSON, &pos, &updatedAt, &mode, &orderJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return queueRow{IDs: []string{}, Mode: QueueModeSequence, ShuffleOrder: []string{}}, nil
	}
	if err != nil {
		return queueRow{}, err
	}
	var ids []string
	if err := json.Unmarshal([]byte(idsJSON), &ids); err != nil {
		return queueRow{}, err
	}
	var order []string
	if orderJSON != "" {
		if err := json.Unmarshal([]byte(orderJSON), &order); err != nil {
			return queueRow{}, err
		}
	}
	if ids == nil {
		ids = []string{}
	}
	if order == nil {
		order = []string{}
	}
	// Rows written before the mode column existed (or by an older binary) keep
	// the historical behaviour.
	if !ValidQueueMode(mode) {
		mode = QueueModeSequence
	}
	return queueRow{IDs: ids, Position: pos, UpdatedAt: updatedAt, Mode: mode, ShuffleOrder: order}, nil
}

// saveQueue persists track order and cursor, preserving the device's playback
// mode. The shuffle order survives only while it still describes the new track
// set, so queue edits drop stale orders and the next shuffle step regenerates.
func (s *Server) saveQueue(deviceKey string, ids []string, pos int) error {
	cur, err := s.loadQueue(deviceKey)
	if err != nil {
		return err
	}
	order := cur.ShuffleOrder
	if !shuffleOrderValid(order, ids) {
		order = []string{}
	}
	return s.saveQueueFull(deviceKey, ids, pos, cur.Mode, order)
}

// saveQueueFull writes every queue field at once (used by edits and mode changes).
func (s *Server) saveQueueFull(deviceKey string, ids []string, pos int, mode string, order []string) error {
	if ids == nil {
		ids = []string{}
	}
	if !ValidQueueMode(mode) {
		mode = QueueModeSequence
	}
	if order == nil {
		order = []string{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	ob, err := json.Marshal(order)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO playback_queue(device_key,track_ids,position,updated_at,mode,shuffle_order) VALUES(?,?,?,?,?,?)
		ON CONFLICT(device_key) DO UPDATE SET track_ids=excluded.track_ids,position=excluded.position,
		updated_at=excluded.updated_at,mode=excluded.mode,shuffle_order=excluded.shuffle_order`,
		deviceKey, string(b), pos, time.Now().Unix(), mode, string(ob))
	return err
}

// shuffleOrderValid reports whether order is a permutation of ids (same track
// multiset), i.e. it was generated for exactly this queue content.
func shuffleOrderValid(order, ids []string) bool {
	if len(order) != len(ids) || len(order) == 0 {
		return false
	}
	counts := make(map[string]int, len(ids))
	for _, id := range ids {
		counts[id]++
	}
	for _, id := range order {
		counts[id]--
		if counts[id] < 0 {
			return false
		}
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

// newShuffleOrder builds a Fisher-Yates permutation of the queue IDs.
func newShuffleOrder(ids []string) []string {
	order := append([]string(nil), ids...)
	shuffleShuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	return order
}

// ensureShuffleOrder returns a valid order for ids, generating a fresh one when
// the stored order is missing or stale (queue edited while shuffle was active).
func ensureShuffleOrder(order, ids []string) []string {
	if shuffleOrderValid(order, ids) {
		return order
	}
	return newShuffleOrder(ids)
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return -1
}

// shuffleStep returns the track one step (delta ±1) from cur within order,
// wrapping at both ends.
func shuffleStep(order []string, cur string, delta int) string {
	idx := indexOf(order, cur)
	if idx < 0 {
		idx = 0
	}
	n := len(order)
	return order[((idx+delta)%n+n)%n]
}

// Queue reports the node's active queue with resolved track metadata.
func (s *Server) Queue(nodeID string) (QueueState, error) {
	return s.queueDevice(s.activeTarget(nodeID))
}

func (s *Server) QueueDevice(nodeID, deviceKey string) (QueueState, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return QueueState{}, err
	}
	return s.queueDevice(target)
}

func (s *Server) queueDevice(target playbackTarget) (QueueState, error) {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	return s.queueLocked(target)
}

func (s *Server) queueLocked(target playbackTarget) (QueueState, error) {
	row, err := s.loadQueue(target.DeviceKey)
	if err != nil {
		return QueueState{}, err
	}
	q := QueueState{NodeID: target.NodeID, DeviceKey: target.DeviceKey, HW: target.HW,
		Tracks: []QueueTrack{}, Position: row.Position, Mode: row.Mode,
		ShuffleOrder: row.ShuffleOrder, UpdatedAt: row.UpdatedAt}
	for _, id := range row.IDs {
		qt := QueueTrack{TrackID: id}
		if t, err := s.lib.GetTrack(id); err == nil {
			qt.Title, qt.Artist, qt.Album = t.Title, t.Artist, t.Album
			qt.DurationMS, qt.Codec, qt.Decodable, qt.MQA = t.DurationMS, t.Codec, t.Decodable, t.MQA
			qt.CoverHash = t.CoverHash
		} else {
			qt.Missing = true
		}
		q.Tracks = append(q.Tracks, qt)
	}
	return q, nil
}

// SetQueue replaces the whole queue (console reorder / remove / clear). It does
// not start playback; position is clamped into range. The playback mode is
// preserved; a shuffle order is dropped because the track set changed.
func (s *Server) SetQueue(nodeID string, ids []string, pos int) error {
	return s.setQueueDevice(s.activeTarget(nodeID), ids, pos)
}

func (s *Server) SetQueueDevice(nodeID, deviceKey string, ids []string, pos int) error {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return err
	}
	return s.setQueueDevice(target, ids, pos)
}

func (s *Server) setQueueDevice(target playbackTarget, ids []string, pos int) error {
	for _, id := range ids {
		if _, err := s.lib.GetTrack(id); err != nil {
			return fmt.Errorf("曲目不存在：%s", id)
		}
	}
	if pos < 0 {
		pos = 0
	}
	if pos >= len(ids) {
		pos = len(ids) - 1
	}
	if pos < 0 {
		pos = 0
	}
	s.qmu.Lock()
	defer s.qmu.Unlock()
	return s.saveQueue(target.DeviceKey, ids, pos)
}

// SetQueueMode persists the device's playback mode and returns the new state.
// Enabling shuffle freezes a Fisher-Yates permutation of the current queue (a
// previously frozen order for the same tracks is reused); other modes clear it.
func (s *Server) SetQueueMode(nodeID, mode string) (QueueState, error) {
	return s.setQueueModeDevice(s.activeTarget(nodeID), mode)
}

func (s *Server) SetQueueModeDevice(nodeID, deviceKey, mode string) (QueueState, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return QueueState{}, err
	}
	return s.setQueueModeDevice(target, mode)
}

func (s *Server) setQueueModeDevice(target playbackTarget, mode string) (QueueState, error) {
	if !ValidQueueMode(mode) {
		return QueueState{}, ErrQueueMode
	}
	s.qmu.Lock()
	defer s.qmu.Unlock()
	row, err := s.loadQueue(target.DeviceKey)
	if err != nil {
		return QueueState{}, err
	}
	order := []string{}
	if mode == QueueModeShuffle {
		order = ensureShuffleOrder(row.ShuffleOrder, row.IDs)
		if row.Mode != QueueModeShuffle {
			// A fresh enable always picks a new permutation.
			order = newShuffleOrder(row.IDs)
		}
	}
	if err := s.saveQueueFull(target.DeviceKey, row.IDs, row.Position, mode, order); err != nil {
		return QueueState{}, err
	}
	return s.queueLocked(target)
}

// startsIdle reports whether queue edits on this device should start playback:
// the terminal is connected and this device is neither playing nor paused.
func (s *Server) startsIdle(nodeID, deviceKey string) bool {
	if !s.Connected(nodeID) {
		return false
	}
	st, err := s.StatusDevice(nodeID, deviceKey)
	return err == nil && st.Connected && !st.Playing && st.State != "paused"
}

// Enqueue appends a track and starts it when the device is idle, even if the
// queue already holds entries, so library actions always produce sound.
func (s *Server) Enqueue(ctx context.Context, nodeID, trackID string) (QueueState, string, error) {
	return s.enqueueDevice(ctx, s.activeTarget(nodeID), trackID)
}

func (s *Server) EnqueueDevice(ctx context.Context, nodeID, trackID, deviceKey string) (QueueState, string, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return QueueState{}, "", err
	}
	return s.enqueueDevice(ctx, target, trackID)
}

func (s *Server) enqueueDevice(ctx context.Context, target playbackTarget, trackID string) (QueueState, string, error) {
	if _, err := s.lib.GetTrack(trackID); err != nil {
		return QueueState{}, "", fmt.Errorf("曲目不存在：%s", trackID)
	}
	autoPlay := s.startsIdle(target.NodeID, target.DeviceKey)
	s.qmu.Lock()
	row, err := s.loadQueue(target.DeviceKey)
	if err != nil {
		s.qmu.Unlock()
		return QueueState{}, "", err
	}
	ids, pos := row.IDs, row.Position
	ids = append(ids, trackID)
	if autoPlay {
		pos = len(ids) - 1
	}
	if err := s.saveQueue(target.DeviceKey, ids, pos); err != nil {
		s.qmu.Unlock()
		return QueueState{}, "", err
	}
	q, err := s.queueLocked(target)
	s.qmu.Unlock()
	if err != nil {
		return QueueState{}, "", err
	}
	sessionID := ""
	if autoPlay {
		sessionID, err = s.StartPlayDevice(ctx, target.NodeID, trackID, target.DeviceKey)
		if err != nil {
			return q, "", err
		}
	}
	return q, sessionID, nil
}

// EnqueueMany appends or inserts tracks. When the device is idle the first
// added track becomes current and plays immediately, regardless of queue
// content; while playing, queue edits never interrupt the stream.
func (s *Server) EnqueueMany(ctx context.Context, nodeID string, trackIDs []string, next bool) (QueueState, string, error) {
	return s.enqueueManyDevice(ctx, s.activeTarget(nodeID), trackIDs, next)
}

func (s *Server) EnqueueManyDevice(ctx context.Context, nodeID string, trackIDs []string, next bool, deviceKey string) (QueueState, string, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return QueueState{}, "", err
	}
	return s.enqueueManyDevice(ctx, target, trackIDs, next)
}

func (s *Server) enqueueManyDevice(ctx context.Context, target playbackTarget, trackIDs []string, next bool) (QueueState, string, error) {
	if len(trackIDs) == 0 || len(trackIDs) > 10000 {
		return QueueState{}, "", fmt.Errorf("批量曲目数量需在 1..10000")
	}
	for _, id := range trackIDs {
		t, err := s.lib.GetTrack(id)
		if err != nil || !t.Decodable {
			return QueueState{}, "", fmt.Errorf("曲目不存在或无法解码：%s", id)
		}
	}
	autoPlay := s.startsIdle(target.NodeID, target.DeviceKey)
	s.qmu.Lock()
	row, err := s.loadQueue(target.DeviceKey)
	if err != nil {
		s.qmu.Unlock()
		return QueueState{}, "", err
	}
	ids, pos := row.IDs, row.Position
	if len(ids)+len(trackIDs) > 10000 {
		s.qmu.Unlock()
		return QueueState{}, "", fmt.Errorf("队列最多容纳 10000 首曲目")
	}
	empty := len(ids) == 0
	at := len(ids)
	if next && !empty {
		at = min(max(pos+1, 0), len(ids))
	}
	merged := make([]string, 0, len(ids)+len(trackIDs))
	merged = append(merged, ids[:at]...)
	merged = append(merged, trackIDs...)
	merged = append(merged, ids[at:]...)
	if empty {
		pos = 0
	}
	if autoPlay {
		pos = at
	}
	if err = s.saveQueue(target.DeviceKey, merged, pos); err != nil {
		s.qmu.Unlock()
		return QueueState{}, "", err
	}
	q, err := s.queueLocked(target)
	s.qmu.Unlock()
	if err != nil {
		return QueueState{}, "", err
	}
	sessionID := ""
	if autoPlay {
		sessionID, err = s.StartPlayDevice(ctx, target.NodeID, trackIDs[0], target.DeviceKey)
	}
	return q, sessionID, err
}

// ReplaceQueue swaps the queue and, unlike SetQueue, starts the selected entry
// when the device is idle. Console reorders keep using SetQueue.
func (s *Server) ReplaceQueue(ctx context.Context, nodeID string, ids []string, pos int) (string, error) {
	return s.replaceQueueDevice(ctx, s.activeTarget(nodeID), ids, pos)
}

func (s *Server) ReplaceQueueDevice(ctx context.Context, nodeID string, ids []string, pos int, deviceKey string) (string, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return "", err
	}
	return s.replaceQueueDevice(ctx, target, ids, pos)
}

func (s *Server) replaceQueueDevice(ctx context.Context, target playbackTarget, ids []string, pos int) (string, error) {
	autoPlay := s.startsIdle(target.NodeID, target.DeviceKey)
	if err := s.setQueueDevice(target, ids, pos); err != nil {
		return "", err
	}
	if !autoPlay || len(ids) == 0 {
		return "", nil
	}
	if pos < 0 || pos >= len(ids) {
		pos = 0
	}
	return s.StartPlayDevice(ctx, target.NodeID, ids[pos], target.DeviceKey)
}

// QueueJump plays the entry at index and persists the new position.
func (s *Server) QueueJump(ctx context.Context, nodeID string, index int) (string, error) {
	return s.queueJumpDevice(ctx, s.activeTarget(nodeID), index)
}

func (s *Server) QueueJumpDevice(ctx context.Context, nodeID string, index int, deviceKey string) (string, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return "", err
	}
	return s.queueJumpDevice(ctx, target, index)
}

func (s *Server) queueJumpDevice(ctx context.Context, target playbackTarget, index int) (string, error) {
	s.qmu.Lock()
	row, err := s.loadQueue(target.DeviceKey)
	if err != nil {
		s.qmu.Unlock()
		return "", err
	}
	if len(row.IDs) == 0 {
		s.qmu.Unlock()
		return "", ErrQueueEmpty
	}
	if index < 0 || index >= len(row.IDs) {
		s.qmu.Unlock()
		return "", ErrQueueIndex
	}
	if err := s.saveQueue(target.DeviceKey, row.IDs, index); err != nil {
		s.qmu.Unlock()
		return "", err
	}
	s.qmu.Unlock()
	return s.StartPlayDevice(ctx, target.NodeID, row.IDs[index], target.DeviceKey)
}

// QueueNext advances to the next entry and plays it. sequence/loop_one stop at
// the last entry; loop_all and shuffle wrap around.
func (s *Server) QueueNext(ctx context.Context, nodeID string) (string, error) {
	return s.queueStep(ctx, s.activeTarget(nodeID), 1)
}

func (s *Server) QueueNextDevice(ctx context.Context, nodeID, deviceKey string) (string, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return "", err
	}
	return s.queueStep(ctx, target, 1)
}

// QueuePrev goes back one entry and plays it; at the first entry sequence and
// loop_one restart the current track while loop_all and shuffle wrap to the last.
func (s *Server) QueuePrev(ctx context.Context, nodeID string) (string, error) {
	return s.queueStep(ctx, s.activeTarget(nodeID), -1)
}

func (s *Server) QueuePrevDevice(ctx context.Context, nodeID, deviceKey string) (string, error) {
	target, err := s.resolveTarget(nodeID, deviceKey)
	if err != nil {
		return "", err
	}
	return s.queueStep(ctx, target, -1)
}

func (s *Server) queueStep(ctx context.Context, target playbackTarget, delta int) (string, error) {
	s.qmu.Lock()
	row, err := s.loadQueue(target.DeviceKey)
	if err != nil {
		s.qmu.Unlock()
		return "", err
	}
	ids, pos := row.IDs, row.Position
	if len(ids) == 0 {
		s.qmu.Unlock()
		return "", ErrQueueEmpty
	}
	next := pos
	switch row.Mode {
	case QueueModeShuffle:
		order := ensureShuffleOrder(row.ShuffleOrder, ids)
		cur := pos
		if cur < 0 || cur >= len(ids) {
			cur = 0
		}
		next = indexOf(ids, shuffleStep(order, ids[cur], delta))
		if next < 0 {
			next = 0
		}
		if err := s.saveQueueFull(target.DeviceKey, ids, next, row.Mode, order); err != nil {
			s.qmu.Unlock()
			return "", err
		}
	case QueueModeLoopAll:
		next = pos + delta
		if next >= len(ids) {
			next = 0
		}
		if next < 0 {
			next = len(ids) - 1
		}
		if err := s.saveQueue(target.DeviceKey, ids, next); err != nil {
			s.qmu.Unlock()
			return "", err
		}
	default: // sequence, loop_one: manual next does not stick or wrap
		next = pos + delta
		if next >= len(ids) {
			s.qmu.Unlock()
			return "", ErrQueueEnd
		}
		if next < 0 {
			next = 0
		}
		if err := s.saveQueue(target.DeviceKey, ids, next); err != nil {
			s.qmu.Unlock()
			return "", err
		}
	}
	s.qmu.Unlock()
	return s.StartPlayDevice(ctx, target.NodeID, ids[next], target.DeviceKey)
}

// autoAdvance runs after a stream ends naturally (EOF): if the finished track
// is still the queued current entry, apply that device's playback mode.
func (s *Server) autoAdvance(deviceKey, finishedTrackID string) {
	nodeID, err := s.targetNodeID(deviceKey)
	if err != nil {
		return
	}
	if _, err := parseDeviceKey(nodeID, deviceKey); err != nil {
		return
	}
	s.qmu.Lock()
	row, err := s.loadQueue(deviceKey)
	ids, pos := row.IDs, row.Position
	if err != nil || len(ids) == 0 || pos < 0 || pos >= len(ids) || ids[pos] != finishedTrackID {
		s.qmu.Unlock()
		return
	}
	next := pos
	order := row.ShuffleOrder
	switch row.Mode {
	case QueueModeLoopOne:
		// Replay the same entry; the queue position does not move.
	case QueueModeLoopAll:
		next = (pos + 1) % len(ids)
	case QueueModeShuffle:
		order = ensureShuffleOrder(row.ShuffleOrder, ids)
		next = indexOf(ids, shuffleStep(order, finishedTrackID, 1))
		if next < 0 {
			next = 0
		}
	default: // sequence: stop at the end
		if pos+1 >= len(ids) {
			s.qmu.Unlock()
			return
		}
		next = pos + 1
	}
	if next != pos || row.Mode == QueueModeShuffle {
		if err := s.saveQueueFull(deviceKey, ids, next, row.Mode, order); err != nil {
			s.qmu.Unlock()
			return
		}
	}
	trackID := ids[next]
	s.qmu.Unlock()
	if _, err := s.StartPlayDevice(context.Background(), nodeID, trackID, deviceKey); err != nil {
		slog.Warn("zat queue auto-advance", "node", nodeID, "device_key", deviceKey, "track", trackID, "err", err)
	}
}
