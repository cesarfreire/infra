package prefs

import (
	"os"
	"path/filepath"
	"testing"

	"infra/internal/domain"
)

func TestPersistsAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetLastContext("prod", "sa-east-1")
	sc := s.Scope("prod", "sa-east-1")
	web := domain.Bookmark{Kind: domain.KindInstance, ID: "i-1", Label: "web"}
	if fav, _ := sc.ToggleFavorite(web); !fav {
		t.Fatal("first toggle should add")
	}
	sc.Visit(web)
	sc.Set("jumphost.sessions", "i-9")
	sc.SetGlobal("metrics.window", "12h")

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p, r := s2.LastContext(); p != "prod" || r != "sa-east-1" {
		t.Errorf("LastContext() = %s, %s", p, r)
	}
	sc2 := s2.Scope("prod", "sa-east-1")
	if !sc2.IsFavorite(web) || len(sc2.Recents()) != 1 || sc2.Get("jumphost.sessions") != "i-9" {
		t.Errorf("scoped state not persisted")
	}
	if s2.Scope("dev", "sa-east-1").IsFavorite(web) {
		t.Error("favorites are per profile and region")
	}
	if s2.Scope("dev", "us-east-1").GetGlobal("metrics.window") != "12h" {
		t.Error("global values are shared")
	}
	if info, _ := os.Stat(filepath.Join(dir, "state.json")); info.Mode().Perm() != 0o600 {
		t.Errorf("state.json mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestRecentsAreNewestFirstAndBounded(t *testing.T) {
	sc, _ := Open(t.TempDir())
	scope := sc.Scope("", "us-east-1")
	for i := range maxRecents + 5 {
		scope.Visit(domain.Bookmark{Kind: domain.KindTable, ID: string(rune('a' + i))})
	}
	scope.Visit(domain.Bookmark{Kind: domain.KindTable, ID: "c"}) // again: moves to the front
	got := scope.Recents()
	if len(got) != maxRecents || got[0].ID != "c" || got[1].ID != string(rune('a'+maxRecents+4)) {
		t.Errorf("recents = %v", got)
	}
	scope.Forget(domain.Bookmark{Kind: domain.KindTable, ID: "c"})
	if scope.Recents()[0].ID == "c" {
		t.Error("Forget should remove the entry")
	}
}

func TestReadOnlySettings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"readOnlyProfiles": ["prod", "*-prd", "default"]}`), 0o600)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for profile, want := range map[string]bool{"prod": true, "billing-prd": true, "dev": false, "": true} {
		if got := s.ReadOnly(profile); got != want {
			t.Errorf("ReadOnly(%q) = %v, want %v", profile, got, want)
		}
	}
}

func TestUpdateCheckSetting(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if !s.UpdateCheckEnabled() {
		t.Error("the update check is on by default")
	}
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"updateCheck": false}`), 0o600)
	s, _ = Open(dir)
	if s.UpdateCheckEnabled() {
		t.Error("settings.json should turn it off")
	}
}

func TestCorruptFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{nope`), 0o600)
	if _, err := Open(dir); err == nil {
		t.Error("a corrupt settings file should be reported")
	}
}
