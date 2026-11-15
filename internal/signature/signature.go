// Package signature verifies artifact signatures (NFR-S2 / FR-3.11.8).
// Production uses GPG detached signatures; a dev mode exists only for bring-up.
package signature

import (
	"fmt"
	"os"
	"os/exec"
)

// Verifier checks a detached signature against an artifact.
type Verifier interface {
	// Verify returns nil when the signature is valid for the artifact.
	Verify(artifactPath, sigPath string) error
	// Mode reports the active verification mode.
	Mode() string
}

// New builds a Verifier for the given mode ("enforce" or "dev").
func New(mode, gpgHome string) Verifier {
	if mode == "dev" {
		return devVerifier{}
	}
	return gpgVerifier{home: gpgHome}
}

type gpgVerifier struct{ home string }

func (g gpgVerifier) Mode() string { return "enforce" }

func (g gpgVerifier) Verify(artifactPath, sigPath string) error {
	if _, err := os.Stat(sigPath); err != nil {
		return fmt.Errorf("signature file missing: %w", err)
	}
	args := []string{"--batch", "--verify", sigPath, artifactPath}
	cmd := exec.Command("gpg", args...)
	if g.home != "" {
		cmd.Env = append(os.Environ(), "GNUPGHOME="+g.home)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gpg verify failed: %v: %s", err, string(out))
	}
	return nil
}

type devVerifier struct{}

func (devVerifier) Mode() string { return "dev" }

func (devVerifier) Verify(_, _ string) error { return nil }
