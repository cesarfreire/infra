package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"infra/internal/domain"
)

// UpdateCheckInterval is how long a check result is reused before asking
// the release source again (GitHub allows 60 anonymous requests per hour).
const UpdateCheckInterval = 24 * time.Hour

// ReleaseSource returns the latest published release.
type ReleaseSource interface {
	LatestRelease(ctx context.Context) (domain.Release, error)
}

// GlobalStore persists small values between runs; satisfied by *prefs.Store.
type GlobalStore interface {
	GetGlobal(key string) string
	SetGlobal(key, value string) error
}

// UpdateDeps are what the update check needs.
type UpdateDeps struct {
	Source  ReleaseSource
	Store   GlobalStore // optional: caches the last check
	Current string      // running version, e.g. "1.2.0" or "v1.2.0"
	Now     func() time.Time
}

// UpdateChecker tells whether a newer release exists.
type UpdateChecker struct {
	d UpdateDeps
}

const (
	keyCheckedAt = "update.checkedAt"
	keyVersion   = "update.version"
	keyURL       = "update.url"
)

// NewUpdateChecker returns a checker; Now defaults to time.Now.
func NewUpdateChecker(d UpdateDeps) *UpdateChecker {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &UpdateChecker{d: d}
}

// Check returns the latest release and whether it is newer than the running
// version. Development and snapshot builds never report updates.
func (u *UpdateChecker) Check(ctx context.Context) (domain.Release, bool, error) {
	current := canonical(u.d.Current)
	if !semver.IsValid(current) || strings.Contains(current, "SNAPSHOT") {
		return domain.Release{}, false, nil
	}
	latest, err := u.latest(ctx)
	if err != nil {
		return domain.Release{}, false, err
	}
	v := canonical(latest.Version)
	if !semver.IsValid(v) || semver.Compare(v, current) <= 0 {
		return latest, false, nil
	}
	return latest, true, nil
}

// latest returns the cached release when it is fresh, else asks the source.
func (u *UpdateChecker) latest(ctx context.Context) (domain.Release, error) {
	now := u.d.Now()
	if s := u.d.Store; s != nil {
		checked, err := time.Parse(time.RFC3339, s.GetGlobal(keyCheckedAt))
		if err == nil && now.Sub(checked) < UpdateCheckInterval && s.GetGlobal(keyVersion) != "" {
			return domain.Release{Version: s.GetGlobal(keyVersion), URL: s.GetGlobal(keyURL)}, nil
		}
	}
	r, err := u.d.Source.LatestRelease(ctx)
	if err != nil {
		return domain.Release{}, fmt.Errorf("checking for updates: %w", err)
	}
	if s := u.d.Store; s != nil {
		// Best effort: a failed write only means checking again next time.
		_ = s.SetGlobal(keyVersion, r.Version)
		_ = s.SetGlobal(keyURL, r.URL)
		_ = s.SetGlobal(keyCheckedAt, now.UTC().Format(time.RFC3339))
	}
	return r, nil
}

// canonical adds the "v" semver expects ("1.2.0" → "v1.2.0").
func canonical(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}
