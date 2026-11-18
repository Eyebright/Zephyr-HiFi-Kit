// Package license implements the single-row product license: signed activation,
// expiry and seat accounting (FR-6). Activation payloads are detached-GPG-signed
// and verified through the existing signature verifier (enforce mode in prod).
package license

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"hifihub/server/internal/domain"
	"hifihub/server/internal/signature"
	"hifihub/server/internal/store"
)

// ErrInactive is returned when an operation requires an activated, unexpired
// license but none is present.
var ErrInactive = errors.New("许可证未激活或已过期，请先激活许可证")

// BuiltinSeats is the grant bundled with every server: up to three terminals
// can be authorized without any license. Terminals themselves never need a
// certificate; a license only extends the server's seat limit beyond this.
const BuiltinSeats = 3

// Service manages the license row and activation.
type Service struct {
	db       *sql.DB
	verifier signature.Verifier
}

// New creates a license service using the configured signature verifier.
func New(s *store.Store, v signature.Verifier) *Service {
	return &Service{db: s.DB, verifier: v}
}

// payload is the signed license document (base64-decoded from payload_b64).
type payload struct {
	Key       string `json:"key"`
	Account   string `json:"account"`
	Seats     int    `json:"seats"`
	ExpiresAt int64  `json:"expires_at"`
}

// Get returns the current license state: none, activated or expired.
func (s *Service) Get() (domain.License, error) {
	var l domain.License
	var key, account string
	var seats int
	var exp, act int64
	err := s.db.QueryRow(`SELECT key,account,seats,expires_at,activated_at FROM license WHERE id=1`).
		Scan(&key, &account, &seats, &exp, &act)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.License{Status: domain.LicenseNone, BuiltinSeats: BuiltinSeats, EffectiveSeats: BuiltinSeats}, nil
	}
	if err != nil {
		return l, err
	}
	l = domain.License{Key: key, Account: account, Seats: seats, ExpiresAt: exp, ActivatedAt: act}
	if time.Now().Unix() >= exp {
		l.Status = domain.LicenseExpired
	} else {
		l.Status = domain.LicenseActivated
	}
	l.BuiltinSeats = BuiltinSeats
	l.EffectiveSeats = BuiltinSeats
	if l.Status == domain.LicenseActivated && l.Seats > l.EffectiveSeats {
		l.EffectiveSeats = l.Seats
	}
	return l, nil
}

// SeatLimit returns the effective seat limit and whether an activated license
// provides it. The built-in grant is a floor: a license only raises the limit.
func (s *Service) SeatLimit() (int, bool, error) {
	l, err := s.Get()
	if err != nil {
		return 0, false, err
	}
	return l.EffectiveSeats, l.Status == domain.LicenseActivated, nil
}

// RequireActive returns the license only when activated and unexpired.
func (s *Service) RequireActive() (domain.License, error) {
	l, err := s.Get()
	if err != nil {
		return l, err
	}
	if l.Status != domain.LicenseActivated {
		return l, ErrInactive
	}
	return l, nil
}

// Activate verifies the detached signature over the payload, validates the
// license terms and stores the single row. payloadB64/sigB64 are base64 of the
// JSON document and the armored detached GPG signature respectively.
func (s *Service) Activate(payloadB64, sigB64 string) (domain.License, error) {
	var l domain.License
	raw, err := decodeB64(payloadB64)
	if err != nil {
		return l, fmt.Errorf("payload_b64 解码失败：%w", err)
	}
	sig, err := decodeB64(sigB64)
	if err != nil {
		return l, fmt.Errorf("sig_b64 解码失败：%w", err)
	}
	if err := s.verify(raw, sig); err != nil {
		return l, fmt.Errorf("许可证签名校验失败：%w", err)
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return l, fmt.Errorf("许可证内容解析失败：%w", err)
	}
	if p.Key == "" {
		return l, fmt.Errorf("许可证 key 不能为空")
	}
	if p.Seats < 1 {
		return l, fmt.Errorf("许可证席位必须不少于 1")
	}
	now := time.Now().Unix()
	if p.ExpiresAt <= now {
		return l, fmt.Errorf("许可证已过期")
	}
	_, err = s.db.Exec(`INSERT INTO license(id,key,account,seats,expires_at,activated_at)
		VALUES(1,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET key=excluded.key,account=excluded.account,
		seats=excluded.seats,expires_at=excluded.expires_at,activated_at=excluded.activated_at`,
		p.Key, p.Account, p.Seats, p.ExpiresAt, now)
	if err != nil {
		return l, err
	}
	return s.Get()
}

// Receipt is the plaintext activation receipt exported for the offline issuer's
// ledger. The server holds only public keys, so the receipt is unsigned: it
// records state for bookkeeping and never participates in gating decisions.
type Receipt struct {
	Key         string `json:"key"`
	Account     string `json:"account"`
	Seats       int    `json:"seats"`
	Used        int    `json:"used"`
	ExpiresAt   int64  `json:"expires_at"`
	ActivatedAt int64  `json:"activated_at"`
	ServerID    string `json:"server_id"`
	ExportedAt  int64  `json:"exported_at"`
}

// Receipt returns the activation receipt of the current license. It requires
// an activated, unexpired license (ErrInactive otherwise).
func (s *Service) Receipt() (Receipt, error) {
	l, err := s.RequireActive()
	if err != nil {
		return Receipt{}, err
	}
	var used int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE authorized=1`).Scan(&used); err != nil {
		return Receipt{}, err
	}
	id, err := s.serverID()
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{
		Key: l.Key, Account: l.Account, Seats: l.Seats, Used: used,
		ExpiresAt: l.ExpiresAt, ActivatedAt: l.ActivatedAt,
		ServerID: id, ExportedAt: time.Now().Unix(),
	}, nil
}

// serverID returns the stable per-instance fingerprint, generating and
// persisting it on first use.
func (s *Service) serverID() (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT value FROM server_state WHERE key='server.id'`).Scan(&id)
	if err == nil && id != "" {
		return id, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = uuid.NewString()
	if _, err := s.db.Exec(`INSERT INTO server_state(key,value) VALUES('server.id',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, id); err != nil {
		return "", err
	}
	return id, nil
}

// verify checks the detached signature using the configured verifier. In dev
// mode cryptographic verification is skipped (local bring-up only); enforce mode
// verifies against the server trust-root keyring (NFR-S2).
func (s *Service) verify(raw, sig []byte) error {
	if s.verifier == nil || s.verifier.Mode() != "enforce" {
		return nil
	}
	dir, err := os.MkdirTemp("", "hifihub-license-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	payloadPath := filepath.Join(dir, "payload.json")
	sigPath := filepath.Join(dir, "payload.json.asc")
	if err := os.WriteFile(payloadPath, raw, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		return err
	}
	return s.verifier.Verify(payloadPath, sigPath)
}

func decodeB64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
