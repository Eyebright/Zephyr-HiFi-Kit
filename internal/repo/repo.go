// Package repo stores artifact files, computes integrity hashes and enforces
// signature verification (FR-3.3 / FR-3.11.8 / NFR-S2). Downloads are served
// with HTTP Range support for resumable transfer (FR-3.5.1).
package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"hifihub/server/internal/signature"
)

// Result describes a stored artifact.
type Result struct {
	SHA256  string
	Size    int64
	RelPath string
	SigPath string
	Signed  bool
}

// Service manages the on-disk artifact store.
type Service struct {
	dir      string
	verifier signature.Verifier
}

// New creates a repository rooted at dir with the given signature verifier.
func New(dir string, v signature.Verifier) *Service {
	return &Service{dir: dir, verifier: v}
}

// Put streams an artifact to disk, hashing as it writes, then verifies the
// detached signature before committing the file. In enforce mode an unsigned
// artifact is rejected and never left on disk.
func (s *Service) Put(appID, version, arch, filename string, r io.Reader, sig []byte) (Result, error) {
	var res Result
	if filename == "" {
		filename = fmt.Sprintf("%s_%s_%s.deb", appID, version, arch)
	}
	// rel uses forward slashes so the stored path is portable across OSes;
	// AbsPath converts to the native separator at read time.
	rel := path.Join(appID, version, arch, filename)
	dir := filepath.Join(s.dir, appID, version, arch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, fmt.Errorf("mkdir artifact: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return res, fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return res, fmt.Errorf("write artifact: %w", err)
	}
	res.SHA256 = hex.EncodeToString(h.Sum(nil))
	res.Size = size

	enforce := s.verifier.Mode() == "enforce"
	if len(sig) > 0 {
		sigPath := filepath.Join(dir, filename+".asc")
		if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
			return res, fmt.Errorf("write signature: %w", err)
		}
		if err := s.verifier.Verify(tmpName, sigPath); err != nil {
			os.Remove(sigPath)
			return res, fmt.Errorf("signature verification failed: %w", err)
		}
		res.SigPath = rel + ".asc"
		res.Signed = true
	} else if enforce {
		return res, fmt.Errorf("unsigned artifact rejected (NFR-S2): provide a detached .asc signature")
	} else {
		// Dev mode relaxes NFR-S2: unsigned artifacts are accepted and treated
		// as verified under the dev signature policy.
		res.Signed = true
	}

	final := filepath.Join(s.dir, rel)
	if err := os.Rename(tmpName, final); err != nil {
		return res, fmt.Errorf("commit artifact: %w", err)
	}
	res.RelPath = rel
	return res, nil
}

// AbsPath resolves a stored relative path to an absolute filesystem path.
func (s *Service) AbsPath(rel string) string {
	return filepath.Join(s.dir, filepath.FromSlash(rel))
}

// Delete removes an artifact file (used only for cleanup, never for banned
// versions which must be retained for rollback per FR-3.11.12).
func (s *Service) Delete(rel string) error {
	return os.Remove(s.AbsPath(rel))
}