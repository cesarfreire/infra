package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, status int, body string) *Releases {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/infra/releases/latest" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != "infra/1.0.0" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	r := NewReleases("acme/infra/", "1.0.0")
	r.baseURL = srv.URL
	return r
}

func TestLatestRelease(t *testing.T) {
	r := serve(t, http.StatusOK, `{"tag_name":"v1.2.0","html_url":"https://github.com/acme/infra/releases/tag/v1.2.0","published_at":"2026-10-01T10:00:00Z"}`)
	got, err := r.LatestRelease(context.Background())
	if err != nil || got.Version != "v1.2.0" || got.URL == "" || got.PublishedAt.IsZero() {
		t.Errorf("LatestRelease() = %+v, %v", got, err)
	}
}

func TestNoReleases(t *testing.T) {
	_, err := serve(t, http.StatusNotFound, `{}`).LatestRelease(context.Background())
	if !errors.Is(err, ErrNoReleases) {
		t.Errorf("err = %v", err)
	}
}

func TestRateLimited(t *testing.T) {
	if _, err := serve(t, http.StatusForbidden, `{}`).LatestRelease(context.Background()); err == nil {
		t.Error("expected an error")
	}
}
