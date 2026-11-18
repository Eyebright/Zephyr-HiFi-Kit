//go:build !windows

package mount

func newPlatformBackend(run Runner, credDir string) Backend {
	return NewManager(run, credDir)
}