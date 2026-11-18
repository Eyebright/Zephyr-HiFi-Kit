package executor

import (
	"context"
	"fmt"
	"strings"

	"hifihub/agent/internal/systemctrl"
)

// DpkgPackages installs .deb artifacts and OS dependencies via dpkg/apt. It is
// the production Packages implementation; tests inject a fake.
type DpkgPackages struct{ Run systemctrl.Runner }

// Install installs OS dependencies from the server's apt mirror, then the
// downloaded artifact, repairing dependencies if dpkg reports missing ones.
func (p DpkgPackages) Install(ctx context.Context, debPath string, deps []string) error {
	if err := p.InstallDeps(ctx, deps); err != nil {
		return err
	}
	if out, err := p.Run.Run(ctx, "dpkg", "-i", debPath); err != nil {
		// Attempt to repair broken dependencies, then retry once.
		_, _ = p.Run.Run(ctx, "apt-get", "-f", "install", "-y")
		if out2, err2 := p.Run.Run(ctx, "dpkg", "-i", debPath); err2 != nil {
			return fmt.Errorf("dpkg -i: %w (%s)", err, strings.TrimSpace(out+out2))
		}
	}
	return nil
}

// InstallDeps installs OS dependencies from the server's apt mirror.
func (p DpkgPackages) InstallDeps(ctx context.Context, deps []string) error {
	if len(deps) == 0 {
		return nil
	}
	args := append([]string{"install", "-y", "--no-install-recommends"}, deps...)
	if out, err := p.Run.Run(ctx, "apt-get", args...); err != nil {
		return fmt.Errorf("apt-get install deps: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// Remove removes an installed package.
func (p DpkgPackages) Remove(ctx context.Context, pkg string) error {
	if out, err := p.Run.Run(ctx, "apt-get", "remove", "-y", pkg); err != nil {
		return fmt.Errorf("apt-get remove %s: %w (%s)", pkg, err, strings.TrimSpace(out))
	}
	return nil
}
