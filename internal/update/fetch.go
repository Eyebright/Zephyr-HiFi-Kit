package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// fetchAsset downloads one release asset to dest, enforcing same-origin with
// the configured feed (a compromised or hostile feed cannot redirect the
// updater at arbitrary internal hosts), then verifies the feed-published
// sha256 digest when present. Returns the computed sha256 hex.
func fetchAsset(ctx context.Context, feedURL string, a Asset, dest string) (string, error) {
	if err := validateAssetURL(feedURL, a.URL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validateAssetURL(feedURL, req.URL.String())
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asset %s: HTTP %d", a.Name, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if a.Digest != "" {
		want := strings.TrimPrefix(a.Digest, "sha256:")
		if !strings.EqualFold(sum, want) {
			return "", fmt.Errorf("asset %s digest mismatch: got %s want %s", a.Name, sum, want)
		}
	}
	return sum, nil
}

// validateAssetURL pins downloads to the feed's own origin and blocks cloud
// metadata endpoints. Loopback/link-local are only reachable when the operator
// deliberately configured a feed there (internal mirror / local bring-up).
func validateAssetURL(feedURL, assetURL string) error {
	fu, err := url.Parse(feedURL)
	if err != nil {
		return fmt.Errorf("bad feed url: %w", err)
	}
	au, err := url.Parse(assetURL)
	if err != nil {
		return fmt.Errorf("bad asset url: %w", err)
	}
	if au.Scheme != "https" && au.Scheme != "http" {
		return fmt.Errorf("asset scheme %q not allowed", au.Scheme)
	}
	if au.Scheme != fu.Scheme {
		return fmt.Errorf("asset scheme %q differs from feed scheme %q", au.Scheme, fu.Scheme)
	}
	if !sameAssetOrigin(fu.Hostname(), au.Hostname()) {
		return fmt.Errorf("asset host %q is not the feed host %q", au.Hostname(), fu.Hostname())
	}
	if au.Hostname() == "169.254.169.254" || au.Hostname() == "metadata.google.internal" {
		return fmt.Errorf("asset host %q is a metadata endpoint", au.Hostname())
	}
	if au.Scheme == "https" {
		return nil
	}
	// Plain http is only acceptable against an operator-chosen internal feed;
	// never let it reach loopback/link-local unless the feed itself is there.
	ip := net.ParseIP(au.Hostname())
	if ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		fip := net.ParseIP(fu.Hostname())
		if fip == nil || !(fip.IsLoopback() || fip.IsLinkLocalUnicast()) {
			return fmt.Errorf("plain-http asset %q not allowed from remote feed", au.Hostname())
		}
	}
	return nil
}

// sameAssetOrigin keeps downloads on the feed's own host, with one bounded
// exception: the official GitHub feed (api.github.com / github.com) serves
// release bytes from github.com and the *.githubusercontent.com CDN, so those
// specific hosts are trusted only when the feed itself is GitHub. A custom
// internal mirror stays strictly same-origin.
func sameAssetOrigin(feedHost, assetHost string) bool {
	fh, ah := strings.ToLower(feedHost), strings.ToLower(assetHost)
	if fh == ah {
		return true
	}
	if fh == "api.github.com" || fh == "github.com" {
		if ah == "github.com" || ah == "api.github.com" {
			return true
		}
		if strings.HasSuffix(ah, ".githubusercontent.com") {
			return true
		}
	}
	return false
}

// extractTarGz unpacks a gzipped tar into dir, rejecting absolute paths and
// any entry escaping dir (zip-slip), and returns the extracted root names.
func extractTarGz(src, dir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// Tar names are always slash-separated; check them with path (not
		// filepath) so an absolute "/abs" entry is rejected on every host OS.
		clean := path.Clean(hdr.Name)
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("unsafe tar entry %q", hdr.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(clean))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) && target != filepath.Clean(dir) {
			return fmt.Errorf("tar entry escapes dir: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			return fmt.Errorf("symlink tar entry rejected: %q", hdr.Name)
		}
	}
}

// verifySums checks every file listed in a SHA256SUMS file (sha256sum output
// format, relative paths) against the extracted tree rooted at dir.
func verifySums(dir, sumsPath string) error {
	raw, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return fmt.Errorf("malformed SHA256SUMS line %q", line)
		}
		want, rel := parts[0], strings.TrimPrefix(parts[1], "*")
		rel = path.Clean(rel)
		if path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("unsafe SHA256SUMS path %q", rel)
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("SHA256SUMS path escapes dir: %q", rel)
		}
		got, err := fileSHA256(target)
		if err != nil {
			return err
		}
		if !strings.EqualFold(got, want) {
			return fmt.Errorf("SHA256SUMS mismatch for %s", rel)
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("SHA256SUMS is empty")
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
