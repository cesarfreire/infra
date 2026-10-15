package chart

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"infra/internal/domain"
	"infra/internal/tui/theme"
)

var start = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func TestRender(t *testing.T) {
	s := domain.Series{Label: "CPU", Unit: domain.UnitPercent}
	for i := range 6 {
		s.Points = append(s.Points, domain.Point{Time: start.Add(time.Duration(i) * 30 * time.Minute), Value: float64(i * 10)})
	}
	out := ansi.Strip(Render(theme.Default(), s, Options{Width: 40, Height: 4, Start: start, End: start.Add(3 * time.Hour)}))
	lines := strings.Split(out, "\n")
	if len(lines) != 1+4+2 {
		t.Fatalf("got %d lines:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "now 50% · avg 25% · max 50%") {
		t.Errorf("title = %q", lines[0])
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[1]), "50%") {
		t.Errorf("the axis should top at 50%% (nice ceiling): %q", lines[1])
	}
	if !strings.Contains(lines[4], "█") || !strings.Contains(lines[4], "·") {
		t.Errorf("bottom row should have bars and gaps: %q", lines[4])
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 40 {
			t.Errorf("line wider than 40 (%d): %q", w, l)
		}
	}
}

func TestRenderNoData(t *testing.T) {
	out := ansi.Strip(Render(theme.Default(), domain.Series{Label: "CPU"}, Options{Width: 40, Height: 4}))
	if out != "CPU  no data" {
		t.Errorf("got %q", out)
	}
}

func TestBucketize(t *testing.T) {
	points := []domain.Point{
		{Time: start, Value: 10},
		{Time: start.Add(10 * time.Minute), Value: 30}, // same bucket as the first
		{Time: start.Add(50 * time.Minute), Value: 5},
		{Time: start.Add(-time.Minute), Value: 99}, // out of range
	}
	got := bucketize(points, start, start.Add(time.Hour), 3)
	if got[0] != 20 || !math.IsNaN(got[1]) || got[2] != 5 {
		t.Errorf("bucketize() = %v", got)
	}
}

func TestScaleAndFormat(t *testing.T) {
	if got := scaleMax(domain.UnitPercent, 97); got != 100 {
		t.Errorf("percent scale = %v, want 100", got)
	}
	if got := scaleMax(domain.UnitCount, 1300); got != 1500 {
		t.Errorf("count scale = %v, want 1500", got)
	}
	tests := map[string]string{
		Format(domain.UnitPercent, 3.25):           "3.2%",
		Format(domain.UnitBytesPerSecond, 1536):    "1.5 KiB/s",
		Format(domain.UnitBytes, 5*1024*1024*1024): "5.0 GiB",
		Format(domain.UnitPerSecond, 12500):        "12.5k/s",
		Format(domain.UnitCount, 7):                "7",
	}
	for got, want := range tests {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestFewPointsFillColumns(t *testing.T) {
	s := domain.Series{Label: "CPU", Unit: domain.UnitPercent}
	for i := range 6 { // every 30 minutes over 3h: 6 slots for 30 columns
		s.Points = append(s.Points, domain.Point{Time: start.Add(time.Duration(i) * 30 * time.Minute), Value: 50})
	}
	out := ansi.Strip(Render(theme.Default(), s, Options{Width: 36, Height: 3, Start: start, End: start.Add(3 * time.Hour), Period: 30 * time.Minute}))
	bottom := strings.Split(out, "\n")[3]
	if strings.Contains(bottom, "·") {
		t.Errorf("no gaps expected when every period has a point: %q", bottom)
	}
}

func TestLongTitleIsTruncated(t *testing.T) {
	s := domain.Series{Label: "Network in", Unit: domain.UnitBytesPerSecond,
		Points: []domain.Point{{Time: start, Value: 123456789}}}
	out := Render(theme.Default(), s, Options{Width: 30, Height: 3, Start: start, End: start.Add(time.Hour)})
	if w := ansi.StringWidth(strings.Split(out, "\n")[0]); w > 30 {
		t.Errorf("title width = %d", w)
	}
}
