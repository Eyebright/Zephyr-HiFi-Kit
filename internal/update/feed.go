package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultFeed is the official public release feed. Operators on isolated LANs
// point HIFIHUB_UPDATE_FEED at an internal mirror serving the same JSON shape.
const DefaultFeed = "https://api.github.com/repos/Eyebright/zephyr-music-server/releases/latest"

// Asset is one downloadable release artifact. Digest is the feed-published
// sha256 ("sha256:<hex>") when the feed provides it; it pins the exact bytes.
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// Release is the newest published release offered by the feed.
type Release struct {
	Tag    string  `json:"tag"`
	Assets []Asset `json:"assets"`
}

// Version strips the leading v from the tag.
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Asset returns the named asset, or false.
func (r Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Feed supplies the latest release. Implemented by httpFeed; tests inject a
// local httptest server so the whole pipeline runs without public network.
type Feed interface {
	Latest(ctx context.Context) (Release, error)
}

type httpFeed struct {
	url    string
	client *http.Client
}

// NewFeed builds an HTTP release feed client.
func NewFeed(url string) Feed {
	if url == "" {
		url = DefaultFeed
	}
	return &httpFeed{url: url, client: &http.Client{Timeout: 20 * time.Second}}
}

// githubRelease mirrors the subset of the GitHub releases API we consume.
type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"assets"`
}

func (f *httpFeed) Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := f.client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("feed %s: HTTP %d", f.url, resp.StatusCode)
	}
	var gr githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return Release{}, fmt.Errorf("feed decode: %w", err)
	}
	if gr.TagName == "" {
		return Release{}, fmt.Errorf("feed returned no tag_name")
	}
	r := Release{Tag: gr.TagName, Assets: []Asset{}}
	for _, a := range gr.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Digest: a.Digest, Size: a.Size})
	}
	return r, nil
}
