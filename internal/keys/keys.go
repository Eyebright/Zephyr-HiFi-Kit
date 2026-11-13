// Package keys manages the server GPG keyring: listing public keys, importing
// public-only material and marking/exporting the default trust root (NFR-S2).
package keys

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"hifihub/server/internal/catalog"
	"hifihub/server/internal/domain"
	"hifihub/server/internal/store"
)

const trustRootKey = "trust_root_fpr"

// Service wraps gpg operations against the server GNUPGHOME and persists the
// chosen trust root in server_state.
type Service struct {
	home string
	db   *sql.DB
}

// New creates a keys service. home is the GNUPGHOME (empty = user default).
func New(home string, s *store.Store) *Service {
	return &Service{home: home, db: s.DB}
}

// run executes gpg with the server keyring, returning stdout.
func (s *Service) run(args ...string) ([]byte, error) {
	cmd := exec.Command("gpg", args...)
	if s.home != "" {
		cmd.Env = append(os.Environ(), "GNUPGHOME="+s.home)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("gpg %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// List returns the public keys in the server keyring.
func (s *Service) List() ([]domain.Key, error) {
	out, err := s.run("--batch", "--list-keys", "--with-colons", "--fixed-list-mode", "--with-fingerprint")
	if err != nil {
		return nil, err
	}
	root := s.TrustRootFingerprint()
	keys := []domain.Key{}
	var cur *domain.Key
	fprPending := false
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, ":")
		switch f[0] {
		case "pub":
			keys = append(keys, domain.Key{})
			cur = &keys[len(keys)-1]
			if len(f) > 5 {
				cur.ID = f[4]
				cur.Algo = algoName(f[3])
				cur.CreatedAt, _ = strconv.ParseInt(f[5], 10, 64)
			}
			fprPending = true
		case "fpr":
			if cur != nil && fprPending && len(f) > 9 {
				cur.Fingerprint = f[9]
				cur.IsTrustRoot = root != "" && strings.EqualFold(cur.Fingerprint, root)
				fprPending = false
			}
		case "uid":
			if cur != nil && cur.Name == "" && len(f) > 9 {
				cur.Name, cur.Email = parseUID(f[9])
			}
		}
	}
	return keys, nil
}

// ImportPublic imports an armored PUBLIC key, rejecting secret-key material.
func (s *Service) ImportPublic(armored string) ([]domain.Key, error) {
	if strings.Contains(armored, "BEGIN PGP PRIVATE KEY BLOCK") ||
		strings.Contains(armored, "BEGIN PGP SECRET KEY BLOCK") {
		return nil, fmt.Errorf("不允许导入私钥：仅接受公钥材料")
	}
	if !strings.Contains(armored, "BEGIN PGP PUBLIC KEY BLOCK") {
		return nil, fmt.Errorf("无效的公钥材料：缺少 PUBLIC KEY BLOCK")
	}
	before, _ := s.List()
	seen := map[string]bool{}
	for _, k := range before {
		seen[k.Fingerprint] = true
	}
	f, err := os.CreateTemp("", "hifihub-key-*.asc")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(armored); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	if _, err := s.run("--batch", "--import", f.Name()); err != nil {
		return nil, err
	}
	after, err := s.List()
	if err != nil {
		return nil, err
	}
	imported := []domain.Key{}
	for _, k := range after {
		if !seen[k.Fingerprint] {
			imported = append(imported, k)
		}
	}
	return imported, nil
}

// SetTrustRoot marks a key (by keyid or fingerprint) as the default trust root.
func (s *Service) SetTrustRoot(id string) (domain.Key, error) {
	var k domain.Key
	keys, err := s.List()
	if err != nil {
		return k, err
	}
	for _, cand := range keys {
		if strings.EqualFold(cand.ID, id) || strings.EqualFold(cand.Fingerprint, id) {
			k = cand
			break
		}
	}
	if k.Fingerprint == "" {
		return k, catalog.ErrNotFound
	}
	if _, err := s.db.Exec(`INSERT INTO server_state(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, trustRootKey, k.Fingerprint); err != nil {
		return k, err
	}
	k.IsTrustRoot = true
	return k, nil
}

// TrustRootFingerprint returns the persisted trust root fingerprint ("" if unset).
func (s *Service) TrustRootFingerprint() string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM server_state WHERE key=?`, trustRootKey).Scan(&v)
	return v
}

// ExportTrustRootArmor exports the armored public key of the marked trust root.
func (s *Service) ExportTrustRootArmor() ([]byte, error) {
	fpr := s.TrustRootFingerprint()
	if fpr == "" {
		return nil, catalog.ErrNotFound
	}
	out, err := s.run("--batch", "--armor", "--export", fpr)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, catalog.ErrNotFound
	}
	return out, nil
}

func parseUID(uid string) (name, email string) {
	if i := strings.Index(uid, "<"); i >= 0 {
		if j := strings.Index(uid[i:], ">"); j >= 0 {
			email = uid[i+1 : i+j]
		}
		name = strings.TrimSpace(uid[:i])
	} else {
		name = strings.TrimSpace(uid)
	}
	if k := strings.Index(name, "("); k >= 0 {
		name = strings.TrimSpace(name[:k])
	}
	return name, email
}

func algoName(a string) string {
	switch a {
	case "1", "2", "3":
		return "RSA"
	case "16", "20":
		return "ElGamal"
	case "17":
		return "DSA"
	case "18":
		return "ECDH"
	case "19":
		return "ECDSA"
	case "22":
		return "EdDSA"
	default:
		return "algo" + a
	}
}
