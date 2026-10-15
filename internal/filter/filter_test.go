package filter_test

import (
	"slices"
	"testing"

	"infra/internal/filter"
)

var web = []filter.Field{
	{Name: "name", Value: "web-prod"},
	{Name: "state", Value: "running"},
	{Name: "ip", Value: "10.0.0.1"},
	{Name: "ip", Value: "54.1.2.3"},
	{Name: "tag", Value: "env=prod"},
	{Name: "tag", Value: "city=São Paulo"},
}

func TestMatch(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{"", true},
		{"WEB", true},
		{"web stopped", false},
		{"state:run", true},
		{"state:stopped", false},
		{"name:running", false},
		{"ip:54.1", true},
		{"tag:env=prod web", true},
		{"unknown:x", false},
		{"running:", true}, // trailing colon is treated as free text
		{"sao paulo", true},
		{"tag:city=são", true},
	}
	for _, tt := range tests {
		if got := filter.Parse(tt.query).Match(web); got != tt.want {
			t.Errorf("Parse(%q).Match() = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestApply(t *testing.T) {
	items := []string{"alpha", "beta", "alphabet"}
	fields := func(s string) []filter.Field { return []filter.Field{{Name: "name", Value: s}} }

	got := filter.Apply(filter.Parse("alpha"), items, fields)
	if want := []string{"alpha", "alphabet"}; !slices.Equal(got, want) {
		t.Errorf("Apply() = %v, want %v", got, want)
	}
	if got := filter.Apply(filter.Query{}, items, fields); len(got) != 3 {
		t.Errorf("empty query should keep everything, got %v", got)
	}
}
