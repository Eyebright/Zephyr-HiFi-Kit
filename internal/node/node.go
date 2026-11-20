// Package node implements terminal registration, heartbeat and snapshots (FR-4).
package node

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/store"
)

// Service manages nodes and registration tokens.
type Service struct {
	db           *sql.DB
	offlineAfter time.Duration
}

// New creates a node service.
func New(s *store.Store, offlineAfter time.Duration) *Service {
	return &Service{db: s.DB, offlineAfter: offlineAfter}
}

// ErrPlaying is returned when a destructive operation is blocked because the
// terminal is currently playing (FR-3.10). The message is user-facing Chinese.
var ErrPlaying = errors.New("FR-3.10：终端正在播放，请先停止播放再注销")

// DefaultOSWindow is the default unattended OS-update window.
const DefaultOSWindow = "23:00–07:00"

// RegisterRequest is sent by a fresh agent with its one-time token.
type RegisterRequest struct {
	Token     string `json:"token"`
	Alias     string `json:"alias"`
	Arch      string `json:"arch"`
	Distro    string `json:"distro"`
	Kernel    string `json:"kernel"`
	MachineID string `json:"machine_id"`
	MAC       string `json:"mac"`
}

// Heartbeat is the periodic liveness/state report (doc 4.2).
type Heartbeat struct {
	AgentVersion string  `json:"agent_version"`
	BuildFlavor  string  `json:"build_flavor"`
	BomID        string  `json:"bom_id"`
	BomVersion   int     `json:"bom_version"`
	Playing      bool    `json:"playing"`
	ActiveProto  string  `json:"active_proto"`
	DacHolderPID int     `json:"dac_holder_pid"`
	CPU          float64 `json:"cpu"`
	MemMB        float64 `json:"mem_mb"`
	TempC        float64 `json:"temp_c"`
}

// HeartbeatResult is returned to the agent so it can apply server-side policy.
type HeartbeatResult struct {
	OK             bool   `json:"ok"`
	PlayProtect    bool   `json:"play_protect"`
	AutoOSUpdate   bool   `json:"auto_os_update"`
	OSWindow       string `json:"os_window"`
	DacArbitration string `json:"dac_arbitration"`
	DacDevice      string `json:"dac_device"`
	OfflineAfter   int    `json:"offline_after"`
}

// Snapshot is the full periodic state report including installed apps.
type Snapshot struct {
	Alias  string                `json:"alias"`
	Group  string                `json:"group"`
	Tags   []string              `json:"tags"`
	Arch   string                `json:"arch"`
	Distro string                `json:"distro"`
	Kernel string                `json:"kernel"`
	Apps   []domain.InstalledApp `json:"apps"`
}

// Patch is a partial node update; nil fields are left unchanged.
type Patch struct {
	Alias          *string `json:"alias"`
	Group          *string `json:"group"`
	Note           *string `json:"note"`
	BomID          *string `json:"bom_id"`
	BomVersion     *int    `json:"bom_version"`
	DacArbitration *string `json:"dac_arbitration"`
	DacDevice      *string `json:"dac_device"`
	PlayProtect    *bool   `json:"play_protect"`
	AutoOSUpdate   *bool   `json:"auto_os_update"`
	OSWindow       *string `json:"os_window"`
	OfflineAfter   *int    `json:"offline_after"`
}

// IssueToken creates a one-time registration token valid for ttl (FR-4.1.1).
func (s *Service) IssueToken(ttl time.Duration) (string, int64, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", 0, err
	}
	token := hex.EncodeToString(buf)
	now := time.Now().Unix()
	exp := now + int64(ttl.Seconds())
	_, err := s.db.Exec(`INSERT INTO registration_tokens(token,created_at,expires_at,used) VALUES(?,?,?,0)`,
		token, now, exp)
	return token, exp, err
}

// TokenStatus reports consumption state of a registration token for wizard polling.
func (s *Service) TokenStatus(token string) (used bool, expiresAt int64, nodeID string, err error) {
	var u int
	var nid sql.NullString
	err = s.db.QueryRow(`SELECT used,expires_at,node_id FROM registration_tokens WHERE token=?`, token).
		Scan(&u, &expiresAt, &nid)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, "", catalog.ErrNotFound
	}
	if err != nil {
		return false, 0, "", err
	}
	return u == 1, expiresAt, nid.String, nil
}

// Register consumes a token and creates a node, returning the node and its
// one-time console secret. ip is captured from the request remote address.
func (s *Service) Register(req RegisterRequest, ip string) (domain.Node, string, error) {
	var n domain.Node
	var used int
	var exp int64
	err := s.db.QueryRow(`SELECT used,expires_at FROM registration_tokens WHERE token=?`, req.Token).
		Scan(&used, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return n, "", fmt.Errorf("invalid token")
	}
	if err != nil {
		return n, "", err
	}
	if used == 1 {
		return n, "", fmt.Errorf("token already used")
	}
	if time.Now().Unix() > exp {
		return n, "", fmt.Errorf("token expired")
	}

	secret, err := newSecret()
	if err != nil {
		return n, "", err
	}
	now := time.Now().Unix()
	n = domain.Node{
		NodeID:          "n-" + uuid.NewString(),
		Alias:           req.Alias,
		Arch:            req.Arch,
		Distro:          req.Distro,
		Kernel:          req.Kernel,
		Status:          domain.NodeOnline,
		LastSeen:        now,
		RegisteredAt:    now,
		CertFingerprint: req.MachineID,
		IP:              ip,
		PlayProtect:     true,
		AutoOSUpdate:    false,
		OSWindow:        DefaultOSWindow,
		OfflineAfter:    90,
		Secret:          secret,
	}
	if n.Alias == "" {
		n.Alias = n.NodeID
	}
	if err := s.insertNode(n); err != nil {
		return n, "", err
	}
	if _, err := s.db.Exec(`UPDATE registration_tokens SET used=1,node_id=? WHERE token=?`, n.NodeID, req.Token); err != nil {
		return n, "", err
	}
	return n, secret, nil
}

func newSecret() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

const insertNodeSQL = `INSERT INTO nodes
	(node_id,alias,node_group,tags,arch,distro,kernel,status,last_seen,bom_id,bom_version,
	 agent_version,build_flavor,playing,active_proto,dac_holder_pid,cert_fingerprint,registered_at,
	 note,ip,authorized,authorized_at,play_protect,auto_os_update,os_window,offline_after,
	 dac_arbitration,dac_device,secret)
	VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

func (s *Service) insertNode(n domain.Node) error {
	if n.Tags == nil {
		n.Tags = []string{}
	}
	if n.OSWindow == "" {
		n.OSWindow = DefaultOSWindow
	}
	if n.OfflineAfter == 0 {
		n.OfflineAfter = 90
	}
	_, err := s.db.Exec(insertNodeSQL,
		n.NodeID, n.Alias, n.Group, store.ToJSON(n.Tags), n.Arch, n.Distro, n.Kernel,
		n.Status, n.LastSeen, n.BomID, n.BomVersion, n.AgentVersion, n.BuildFlavor, boolInt(n.Playing),
		n.ActiveProto, n.DacHolderPID, n.CertFingerprint, n.RegisteredAt,
		n.Note, n.IP, boolInt(n.Authorized), n.AuthorizedAt, boolInt(n.PlayProtect),
		boolInt(n.AutoOSUpdate), n.OSWindow, n.OfflineAfter, n.DacArbitration, n.DacDevice, n.Secret)
	return err
}

// Heartbeat updates liveness and playback state, returning the policy the agent
// should apply (play protection, OS-update window, DAC arbitration, etc.).
func (s *Service) Heartbeat(nodeID string, hb Heartbeat) (HeartbeatResult, error) {
	var res HeartbeatResult
	// Older agents omit build_flavor; keep the last reported value instead of
	// clearing it on every heartbeat.
	r, err := s.db.Exec(`UPDATE nodes SET status=?,last_seen=?,agent_version=?,
		build_flavor=CASE WHEN ? <> '' THEN ? ELSE build_flavor END,
		bom_id=?,bom_version=?,playing=?,active_proto=?,dac_holder_pid=?,cpu=?,mem_mb=?,temp_c=? WHERE node_id=?`,
		domain.NodeOnline, time.Now().Unix(), hb.AgentVersion, hb.BuildFlavor, hb.BuildFlavor, hb.BomID, hb.BomVersion,
		boolInt(hb.Playing), hb.ActiveProto, hb.DacHolderPID, hb.CPU, hb.MemMB, hb.TempC, nodeID)
	if err := affected(r, err); err != nil {
		return res, err
	}
	n, err := s.Get(nodeID)
	if err != nil {
		return res, err
	}
	res = HeartbeatResult{
		OK:             true,
		PlayProtect:    n.PlayProtect,
		AutoOSUpdate:   n.AutoOSUpdate,
		OSWindow:       n.OSWindow,
		DacArbitration: n.DacArbitration,
		DacDevice:      n.DacDevice,
		OfflineAfter:   n.OfflineAfter,
	}
	return res, nil
}

// Snapshot updates system info and replaces the installed-app inventory.
func (s *Service) Snapshot(nodeID string, snap Snapshot) error {
	if _, err := s.Get(nodeID); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE nodes SET alias=?,node_group=?,tags=?,arch=?,distro=?,kernel=?,last_seen=?,status=?
		WHERE node_id=?`,
		snap.Alias, snap.Group, store.ToJSON(snap.Tags), snap.Arch, snap.Distro, snap.Kernel,
		time.Now().Unix(), domain.NodeOnline, nodeID)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM installed_apps WHERE node_id=?`, nodeID); err != nil {
		return err
	}
	for _, a := range snap.Apps {
		_, err := s.db.Exec(`INSERT INTO installed_apps
			(node_id,app_id,version,package,unit,enabled,config_hash,build_flags,source)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			nodeID, a.AppID, a.Version, a.Package, a.Unit, boolInt(a.Enabled), a.ConfigHash,
			store.ToJSON(a.BuildFlags), a.Source)
		if err != nil {
			return err
		}
	}
	return nil
}

// InstalledApps returns the reported inventory for a node.
func (s *Service) InstalledApps(nodeID string) ([]domain.InstalledApp, error) {
	rows, err := s.db.Query(`SELECT node_id,app_id,COALESCE(version,''),COALESCE(package,''),
		COALESCE(unit,''),enabled,COALESCE(config_hash,''),COALESCE(build_flags,'[]'),COALESCE(source,'')
		FROM installed_apps WHERE node_id=? ORDER BY app_id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.InstalledApp{}
	for rows.Next() {
		var a domain.InstalledApp
		var enabled int
		var flags string
		if err := rows.Scan(&a.NodeID, &a.AppID, &a.Version, &a.Package, &a.Unit, &enabled,
			&a.ConfigHash, &flags, &a.Source); err != nil {
			return nil, err
		}
		a.Enabled = enabled == 1
		store.FromJSON(flags, &a.BuildFlags)
		out = append(out, a)
	}
	return out, rows.Err()
}

const nodeCols = `node_id,COALESCE(alias,''),COALESCE(node_group,''),COALESCE(tags,'[]'),
	COALESCE(arch,''),COALESCE(distro,''),COALESCE(kernel,''),status,COALESCE(last_seen,0),
	COALESCE(bom_id,''),COALESCE(bom_version,0),COALESCE(agent_version,''),COALESCE(build_flavor,''),playing,
	COALESCE(active_proto,''),COALESCE(dac_holder_pid,0),COALESCE(cert_fingerprint,''),registered_at,
	COALESCE(note,''),COALESCE(ip,''),authorized,COALESCE(authorized_at,0),play_protect,
	auto_os_update,COALESCE(os_window,'23:00–07:00'),COALESCE(offline_after,90),
	COALESCE(dac_arbitration,''),COALESCE(dac_device,''),COALESCE(secret,''),
	COALESCE(cpu,0),COALESCE(mem_mb,0),COALESCE(temp_c,0)`

func scanNode(row interface{ Scan(...any) error }) (domain.Node, error) {
	var n domain.Node
	var tags string
	var playing, authorized, playProtect, autoOS int
	err := row.Scan(&n.NodeID, &n.Alias, &n.Group, &tags, &n.Arch, &n.Distro, &n.Kernel,
		&n.Status, &n.LastSeen, &n.BomID, &n.BomVersion, &n.AgentVersion, &n.BuildFlavor, &playing,
		&n.ActiveProto, &n.DacHolderPID, &n.CertFingerprint, &n.RegisteredAt,
		&n.Note, &n.IP, &authorized, &n.AuthorizedAt, &playProtect, &autoOS,
		&n.OSWindow, &n.OfflineAfter, &n.DacArbitration, &n.DacDevice, &n.Secret,
		&n.CPU, &n.MemMB, &n.TempC)
	if err != nil {
		return n, err
	}
	n.Playing = playing == 1
	n.Authorized = authorized == 1
	n.PlayProtect = playProtect == 1
	n.AutoOSUpdate = autoOS == 1
	store.FromJSON(tags, &n.Tags)
	if n.Tags == nil {
		n.Tags = []string{}
	}
	return n, nil
}

// Get returns one node.
func (s *Service) Get(nodeID string) (domain.Node, error) {
	row := s.db.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE node_id=?`, nodeID)
	n, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return n, catalog.ErrNotFound
	}
	return n, err
}

// List returns all nodes.
func (s *Service) List() ([]domain.Node, error) {
	rows, err := s.db.Query(`SELECT ` + nodeCols + ` FROM nodes ORDER BY alias`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Patch applies a partial update with validation (FR-4 management). BOM
// existence/publish state is validated by the API layer before calling Patch.
func (s *Service) Patch(nodeID string, p Patch) (domain.Node, error) {
	var n domain.Node
	if _, err := s.Get(nodeID); err != nil {
		return n, err
	}
	sets := []string{}
	args := []any{}
	add := func(col string, val any) {
		sets = append(sets, col+"=?")
		args = append(args, val)
	}
	if p.Alias != nil {
		alias := strings.TrimSpace(*p.Alias)
		if alias == "" {
			return n, fmt.Errorf("别名不能为空")
		}
		if len([]rune(alias)) > 40 {
			return n, fmt.Errorf("别名长度不能超过 40 个字符")
		}
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE alias=? AND node_id<>?`, alias, nodeID).Scan(&count); err != nil {
			return n, err
		}
		if count > 0 {
			return n, fmt.Errorf("别名已存在：%s", alias)
		}
		add("alias", alias)
	}
	if p.Group != nil {
		if len([]rune(*p.Group)) > 24 {
			return n, fmt.Errorf("分组长度不能超过 24 个字符")
		}
		add("node_group", *p.Group)
	}
	if p.Note != nil {
		if len([]rune(*p.Note)) > 200 {
			return n, fmt.Errorf("备注长度不能超过 200 个字符")
		}
		add("note", *p.Note)
	}
	if p.BomID != nil {
		add("bom_id", *p.BomID)
	}
	if p.BomVersion != nil {
		add("bom_version", *p.BomVersion)
	}
	if p.DacArbitration != nil {
		add("dac_arbitration", *p.DacArbitration)
	}
	if p.DacDevice != nil {
		add("dac_device", *p.DacDevice)
	}
	if p.PlayProtect != nil {
		add("play_protect", boolInt(*p.PlayProtect))
	}
	if p.AutoOSUpdate != nil {
		add("auto_os_update", boolInt(*p.AutoOSUpdate))
	}
	if p.OSWindow != nil {
		add("os_window", *p.OSWindow)
	}
	if p.OfflineAfter != nil {
		v := *p.OfflineAfter
		if v < 30 {
			v = 30
		}
		if v > 3600 {
			v = 3600
		}
		add("offline_after", v)
	}
	if len(sets) > 0 {
		args = append(args, nodeID)
		if _, err := s.db.Exec(`UPDATE nodes SET `+strings.Join(sets, ",")+` WHERE node_id=?`, args...); err != nil {
			return n, err
		}
	}
	return s.Get(nodeID)
}

// Delete removes a node and its derived state. It refuses while playing
// (FR-3.10). Rollout ring membership is stripped by the caller (rollouts svc).
func (s *Service) Delete(nodeID string) (domain.Node, error) {
	n, err := s.Get(nodeID)
	if err != nil {
		return n, err
	}
	if n.Playing {
		return n, ErrPlaying
	}
	for _, q := range []string{
		`DELETE FROM nodes WHERE node_id=?`,
		`DELETE FROM osupdates WHERE node_id=?`,
		`DELETE FROM netdiag WHERE node_id=?`,
		`DELETE FROM commands WHERE node_id=?`,
		`DELETE FROM installed_apps WHERE node_id=?`,
		`DELETE FROM drifts WHERE node_id=?`,
		`DELETE FROM sessions WHERE node_id=?`,
	} {
		if _, err := s.db.Exec(q, nodeID); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Restore reinserts a previously deleted node (undo support).
func (s *Service) Restore(n domain.Node) (domain.Node, error) {
	if n.NodeID == "" {
		return n, fmt.Errorf("node_id is required")
	}
	if _, err := s.Get(n.NodeID); err == nil {
		return n, fmt.Errorf("终端已存在：%s", n.NodeID)
	} else if !errors.Is(err, catalog.ErrNotFound) {
		return n, err
	}
	if n.Secret == "" {
		sec, err := newSecret()
		if err != nil {
			return n, err
		}
		n.Secret = sec
	}
	if n.Status == "" {
		n.Status = domain.NodeOffline
	}
	if n.RegisteredAt == 0 {
		n.RegisteredAt = time.Now().Unix()
	}
	if err := s.insertNode(n); err != nil {
		return n, err
	}
	return s.Get(n.NodeID)
}

// SetAuthorized marks a node authorized (license seat consumed) or revoked.
func (s *Service) SetAuthorized(nodeID string, authorized bool) error {
	var at int64
	if authorized {
		at = time.Now().Unix()
	}
	res, err := s.db.Exec(`UPDATE nodes SET authorized=?,authorized_at=? WHERE node_id=?`,
		boolInt(authorized), at, nodeID)
	return affected(res, err)
}

// SetPlaying records live playback state for a node: the playing flag, active
// protocol, DAC-holder PID and a refreshed heartbeat. The ZAT server calls this
// when a terminal starts/stops a stream so the play gate (FR-3.10) and console
// reflect reality without waiting for the agent's next heartbeat.
func (s *Service) SetPlaying(nodeID string, playing bool, proto string, dacPID int) error {
	res, err := s.db.Exec(`UPDATE nodes SET playing=?,active_proto=?,dac_holder_pid=?,last_seen=?,status=?
		WHERE node_id=?`,
		boolInt(playing), proto, dacPID, time.Now().Unix(), domain.NodeOnline, nodeID)
	return affected(res, err)
}

// AuthorizedCount returns the number of currently authorized nodes (seats used).
func (s *Service) AuthorizedCount() (int, error) {
	var c int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE authorized=1`).Scan(&c)
	return c, err
}

// SweepOffline marks nodes offline when their last heartbeat is older than the
// per-node offline_after threshold (FR-4.3).
func (s *Service) SweepOffline() (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(`UPDATE nodes SET status=? WHERE status=? AND last_seen < (? - offline_after)`,
		domain.NodeOffline, domain.NodeOnline, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return catalog.ErrNotFound
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
