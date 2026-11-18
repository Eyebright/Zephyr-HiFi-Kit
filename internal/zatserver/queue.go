package zatserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Per-node playback queue. The queue lives server-side (one row per node); the
// terminal's prev/next controls and the console both drive it through the API,
// and natural end-of-track advances it automatically.

var (
	// ErrQueueEmpty is returned when prev/next targets a node with no queue.
	ErrQueueEmpty = errors.New("播放队列为空")
	// ErrQueueEnd is returned when next is pressed on the last queue entry.
	ErrQueueEnd = errors.New("已是队列最后一首")
	// ErrQueueIndex is returned when a jump index is out of range.
	ErrQueueIndex = errors.New("队列位置无效")
)

// QueueTrack is one queue entry with resolved library metadata. Missing marks
// entries whose track disappeared from the library since enqueueing.
type QueueTrack struct {
	TrackID    string `json:"track_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMS int    `json:"duration_ms"`
	Codec      string `json:"codec"`
	Decodable  bool   `json:"decodable"`
	Missing    bool   `json:"missing"`
}

// QueueState is the persisted queue plus resolved track metadata.
type QueueState struct {
	NodeID    string       `json:"node_id"`
	Tracks    []QueueTrack `json:"tracks"`
	Position  int          `json:"position"`
	UpdatedAt int64        `json:"updated_at"`
}

func (s *Server) loadQueue(nodeID string) ([]string, int, int64, error) {
	var idsJSON string
	var pos int
	var updatedAt int64
	err := s.db.QueryRow(`SELECT track_ids,position,updated_at FROM node_queue WHERE node_id=?`, nodeID).
		Scan(&idsJSON, &pos, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, 0, nil
	}
	if err != nil {
		return nil, 0, 0, err
	}
	var ids []string
	if err := json.Unmarshal([]byte(idsJSON), &ids); err != nil {
		return nil, 0, 0, err
	}
	return ids, pos, updatedAt, nil
}

func (s *Server) saveQueue(nodeID string, ids []string, pos int) error {
	if ids == nil {
		ids = []string{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO node_queue(node_id,track_ids,position,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(node_id) DO UPDATE SET track_ids=excluded.track_ids,position=excluded.position,updated_at=excluded.updated_at`,
		nodeID, string(b), pos, time.Now().Unix())
	return err
}

// Queue reports the node's queue with resolved track metadata.
func (s *Server) Queue(nodeID string) (QueueState, error) {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	return s.queueLocked(nodeID)
}

func (s *Server) queueLocked(nodeID string) (QueueState, error) {
	ids, pos, updatedAt, err := s.loadQueue(nodeID)
	if err != nil {
		return QueueState{}, err
	}
	q := QueueState{NodeID: nodeID, Tracks: []QueueTrack{}, Position: pos, UpdatedAt: updatedAt}
	for _, id := range ids {
		qt := QueueTrack{TrackID: id}
		if t, err := s.lib.GetTrack(id); err == nil {
			qt.Title, qt.Artist, qt.Album = t.Title, t.Artist, t.Album
			qt.DurationMS, qt.Codec, qt.Decodable = t.DurationMS, t.Codec, t.Decodable
		} else {
			qt.Missing = true
		}
		q.Tracks = append(q.Tracks, qt)
	}
	return q, nil
}

// SetQueue replaces the whole queue (console reorder / remove / clear). It does
// not start playback; position is clamped into range.
func (s *Server) SetQueue(nodeID string, ids []string, pos int) error {
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
	return s.saveQueue(nodeID, ids, pos)
}

// Enqueue appends a track. When the queue was empty and the terminal is idle,
// playback starts immediately at the appended track.
func (s *Server) Enqueue(ctx context.Context, nodeID, trackID string) (QueueState, string, error) {
	if _, err := s.lib.GetTrack(trackID); err != nil {
		return QueueState{}, "", fmt.Errorf("曲目不存在：%s", trackID)
	}
	s.qmu.Lock()
	ids, pos, _, err := s.loadQueue(nodeID)
	if err != nil {
		s.qmu.Unlock()
		return QueueState{}, "", err
	}
	startNow := len(ids) == 0
	ids = append(ids, trackID)
	if startNow {
		pos = 0
	}
	if err := s.saveQueue(nodeID, ids, pos); err != nil {
		s.qmu.Unlock()
		return QueueState{}, "", err
	}
	q, err := s.queueLocked(nodeID)
	s.qmu.Unlock()
	if err != nil {
		return QueueState{}, "", err
	}
	sessionID := ""
	if startNow && s.Connected(nodeID) {
		if st, err := s.Status(nodeID); err == nil && !st.Playing {
			sessionID, err = s.StartPlay(ctx, nodeID, trackID)
			if err != nil {
				return q, "", err
			}
		}
	}
	return q, sessionID, nil
}

func (s *Server) EnqueueMany(ctx context.Context, nodeID string, trackIDs []string, next bool) (QueueState, string, error) {
	if len(trackIDs) == 0 || len(trackIDs) > 10000 {
		return QueueState{}, "", fmt.Errorf("批量曲目数量需在 1..10000")
	}
	for _, id := range trackIDs {
		t, err := s.lib.GetTrack(id)
		if err != nil || !t.Decodable {
			return QueueState{}, "", fmt.Errorf("曲目不存在或无法解码：%s", id)
		}
	}
	s.qmu.Lock()
	ids, pos, _, err := s.loadQueue(nodeID)
	if err != nil {
		s.qmu.Unlock()
		return QueueState{}, "", err
	}
	if len(ids)+len(trackIDs) > 10000 {
		s.qmu.Unlock()
		return QueueState{}, "", fmt.Errorf("队列最多容纳 10000 首曲目")
	}
	startNow := len(ids) == 0
	at := len(ids)
	if next && !startNow {
		at = min(max(pos+1, 0), len(ids))
	}
	merged := make([]string, 0, len(ids)+len(trackIDs))
	merged = append(merged, ids[:at]...)
	merged = append(merged, trackIDs...)
	merged = append(merged, ids[at:]...)
	if startNow {
		pos = 0
	}
	if err = s.saveQueue(nodeID, merged, pos); err != nil {
		s.qmu.Unlock()
		return QueueState{}, "", err
	}
	q, err := s.queueLocked(nodeID)
	s.qmu.Unlock()
	if err != nil {
		return QueueState{}, "", err
	}
	sessionID := ""
	if startNow && s.Connected(nodeID) {
		if st, statusErr := s.Status(nodeID); statusErr == nil && !st.Playing && st.State != "paused" {
			sessionID, err = s.StartPlay(ctx, nodeID, trackIDs[0])
		}
	}
	return q, sessionID, err
}

// QueueJump plays the entry at index and persists the new position.
func (s *Server) QueueJump(ctx context.Context, nodeID string, index int) (string, error) {
	s.qmu.Lock()
	ids, _, _, err := s.loadQueue(nodeID)
	if err != nil {
		s.qmu.Unlock()
		return "", err
	}
	if len(ids) == 0 {
		s.qmu.Unlock()
		return "", ErrQueueEmpty
	}
	if index < 0 || index >= len(ids) {
		s.qmu.Unlock()
		return "", ErrQueueIndex
	}
	if err := s.saveQueue(nodeID, ids, index); err != nil {
		s.qmu.Unlock()
		return "", err
	}
	s.qmu.Unlock()
	return s.StartPlay(ctx, nodeID, ids[index])
}

// QueueNext advances to the next entry and plays it. At the last entry it
// returns ErrQueueEnd without wrapping.
func (s *Server) QueueNext(ctx context.Context, nodeID string) (string, error) {
	return s.queueStep(ctx, nodeID, 1)
}

// QueuePrev goes back one entry and plays it; at the first entry it restarts
// the current track.
func (s *Server) QueuePrev(ctx context.Context, nodeID string) (string, error) {
	return s.queueStep(ctx, nodeID, -1)
}

func (s *Server) queueStep(ctx context.Context, nodeID string, delta int) (string, error) {
	s.qmu.Lock()
	ids, pos, _, err := s.loadQueue(nodeID)
	if err != nil {
		s.qmu.Unlock()
		return "", err
	}
	if len(ids) == 0 {
		s.qmu.Unlock()
		return "", ErrQueueEmpty
	}
	next := pos + delta
	if next >= len(ids) {
		s.qmu.Unlock()
		return "", ErrQueueEnd
	}
	if next < 0 {
		next = 0
	}
	if err := s.saveQueue(nodeID, ids, next); err != nil {
		s.qmu.Unlock()
		return "", err
	}
	s.qmu.Unlock()
	return s.StartPlay(ctx, nodeID, ids[next])
}

// autoAdvance runs after a stream ends naturally (EOF): if the finished track
// is still the queued current entry, play the next one. User stops and queue
// edits made in the meantime both suppress the advance.
func (s *Server) autoAdvance(nodeID, finishedTrackID string) {
	s.qmu.Lock()
	ids, pos, _, err := s.loadQueue(nodeID)
	if err != nil || len(ids) == 0 || pos >= len(ids) || ids[pos] != finishedTrackID || pos+1 >= len(ids) {
		s.qmu.Unlock()
		return
	}
	pos++
	if err := s.saveQueue(nodeID, ids, pos); err != nil {
		s.qmu.Unlock()
		return
	}
	trackID := ids[pos]
	s.qmu.Unlock()
	if _, err := s.StartPlay(context.Background(), nodeID, trackID); err != nil {
		slog.Warn("zat queue auto-advance", "node", nodeID, "track", trackID, "err", err)
	}
}
