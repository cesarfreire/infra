package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"infra/internal/app"
	"infra/internal/domain"
)

type fakeReleases struct {
	release domain.Release
	err     error
	calls   int
}

func (f *fakeReleases) LatestRelease(context.Context) (domain.Release, error) {
	f.calls++
	return f.release, f.err
}

type memStore map[string]string

func (m memStore) GetGlobal(k string) string   { return m[k] }
func (m memStore) SetGlobal(k, v string) error { m[k] = v; return nil }

func TestUpdateCheckComparesVersions(t *testing.T) {
	src := &fakeReleases{release: domain.Release{Version: "v1.3.0", URL: "https://example.com/v1.3.0"}}
	tests := []struct {
		current string
		want    bool
	}{
		{"1.2.9", true},
		{"v1.3.0", false},
		{"1.4.0", false},
		{"1.3.0-rc.1", true},
		{"dev", false},
		{"0.0.0-SNAPSHOT-none", false},
	}
	for _, tt := range tests {
		r, ok, err := app.NewUpdateChecker(app.UpdateDeps{Source: src, Current: tt.current}).Check(context.Background())
		if err != nil || ok != tt.want {
			t.Errorf("current %s: ok = %v, err = %v; want %v", tt.current, ok, err, tt.want)
		}
		if ok && r.Version != "v1.3.0" {
			t.Errorf("release = %+v", r)
		}
	}
}

func TestUpdateCheckIsCachedForADay(t *testing.T) {
	src := &fakeReleases{release: domain.Release{Version: "v2.0.0", URL: "u"}}
	store := memStore{}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	check := func() bool {
		_, ok, _ := app.NewUpdateChecker(app.UpdateDeps{Source: src, Store: store, Current: "1.0.0",
			Now: func() time.Time { return now }}).Check(context.Background())
		return ok
	}
	if !check() || src.calls != 1 {
		t.Fatalf("first check: calls = %d", src.calls)
	}
	now = now.Add(23 * time.Hour)
	if !check() || src.calls != 1 {
		t.Errorf("within a day the cached release must be used; calls = %d", src.calls)
	}
	now = now.Add(2 * time.Hour)
	check()
	if src.calls != 2 {
		t.Errorf("after a day the source must be asked again; calls = %d", src.calls)
	}
}

func TestUpdateCheckErrorsAreNotCached(t *testing.T) {
	src := &fakeReleases{err: errors.New("rate limited")}
	store := memStore{}
	if _, _, err := app.NewUpdateChecker(app.UpdateDeps{Source: src, Store: store, Current: "1.0.0"}).Check(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if len(store) != 0 {
		t.Errorf("a failed check must not be cached: %v", store)
	}
}
