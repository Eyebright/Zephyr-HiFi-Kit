package mount

import "context"

// Backend mounts and unmounts shares using the host platform's SMB client.
type Backend interface {
	Mount(ctx context.Context, s Spec) error
	Unmount(ctx context.Context, target string) error
	Check(ctx context.Context, target string) error
}

// NewBackend returns the platform backend: kernel mount.cifs on Linux, the
// Windows SMB client (WNet UNC sessions) on Windows.
func NewBackend(run Runner, credDir string) Backend {
	return newPlatformBackend(run, credDir)
}
