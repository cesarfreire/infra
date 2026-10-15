// Package github reads the releases of this application from GitHub.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"infra/internal/domain"
)

const (
	defaultBaseURL = "https://api.github.com"
	requestTimeout = 5 * time.Second
)

// ErrNoReleases is returned when the repository has no published release.
var ErrNoReleases = errors.New("no releases published")

// Releases implements app.ReleaseSource with the GitHub REST API, without
// authentication.
type Releases struct {
	client    *http.Client
	baseURL   string
	repo      string // owner/name
	userAgent string
}

// NewReleases reads the releases of repo ("owner/name"). version goes into
// the User-Agent, which GitHub requires.
func NewReleases(repo, version string) *Releases {
	return &Releases{
		client:    &http.Client{Timeout: requestTimeout},
		baseURL:   defaultBaseURL,
		repo:      strings.Trim(repo, "/"),
		userAgent: "infra/" + version,
	}
}

// LatestRelease returns the newest published release (drafts and
// prereleases are excluded by GitHub).
func (r *Releases) LatestRelease(ctx context.Context) (domain.Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", r.baseURL, r.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", r.userAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return domain.Release{}, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return domain.Release{}, ErrNoReleases
	case resp.StatusCode != http.StatusOK:
		return domain.Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}

	var body struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return domain.Release{}, fmt.Errorf("decoding the release: %w", err)
	}
	return domain.Release{Version: body.TagName, URL: body.HTMLURL, PublishedAt: body.PublishedAt}, nil
}
