// Package chart draws time-series bar charts with block characters: each
// cell is split in eighths, so a chart of height h has 8h levels.
package chart

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"infra/internal/domain"
	"infra/internal/tui/theme"
)

const minPlotWidth = 8

var eighths = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// Options sizes and frames a chart.
type Options struct {
	Width  int // total width, axis included
	Height int // rows of bars
	Start  time.Time
	End    time.Time
	// Period is the spacing of the points. With fewer periods than columns,
	// each point spans several columns instead of leaving empty ones.
	Period time.Duration
}

// Render draws series between opts.Start and opts.End: a title with the
// latest, average and maximum values, the bars with a Y axis, and the time
// axis. Columns without data show a dot on the baseline.
func Render(th theme.Theme, s domain.Series, opts Options) string {
	title := th.Label.Render(s.Label)
	values := values(s.Points)
	if len(values) == 0 {
		return title + "  " + th.Muted.Render("no data")
	}
	last, avg, peak := values[len(values)-1], mean(values), slicesMax(values)
	title += "  " + th.Muted.Render(fmt.Sprintf("now %s · avg %s · max %s",
		Format(s.Unit, last), Format(s.Unit, avg), Format(s.Unit, peak)))
	title = ansi.Truncate(title, opts.Width, "…")

	top := scaleMax(s.Unit, peak)
	labels := []string{Format(s.Unit, top), Format(s.Unit, top/2), Format(s.Unit, 0)}
	labelW := 0
	for _, l := range labels {
		labelW = max(labelW, lipgloss.Width(l))
	}
	plotW := max(minPlotWidth, opts.Width-labelW-2)
	height := max(2, opts.Height)
	buckets := spread(bucketize(s.Points, opts.Start, opts.End, slots(opts, plotW)), plotW)

	lines := []string{title}
	for row := range height {
		label := strings.Repeat(" ", labelW)
		axis := " │"
		switch row {
		case 0:
			label, axis = labels[0], " ┤"
		case height / 2:
			label, axis = labels[1], " ┤"
		}
		var b strings.Builder
		b.WriteString(th.Muted.Render(fmt.Sprintf("%*s%s", labelW, label, axis)))
		for _, v := range buckets {
			b.WriteString(cell(th, s.Unit, v, top, height, row))
		}
		lines = append(lines, b.String())
	}
	lines = append(lines,
		th.Muted.Render(strings.Repeat(" ", labelW)+" └"+strings.Repeat("─", plotW)),
		th.Muted.Render(strings.Repeat(" ", labelW+2)+timeAxis(opts.Start, opts.End, plotW)),
	)
	return strings.Join(lines, "\n")
}

// cell is the character of a bar at row (0 = top).
func cell(th theme.Theme, unit domain.Unit, v, top float64, height, row int) string {
	if math.IsNaN(v) {
		if row == height-1 {
			return th.Muted.Render("·")
		}
		return " "
	}
	level := int(math.Round(v / top * float64(height*8)))
	if v > 0 && level == 0 {
		level = 1 // never hide a non-zero value
	}
	fill := level - (height-1-row)*8
	if fill <= 0 {
		return " "
	}
	style := th.Selected
	if unit == domain.UnitPercent {
		switch {
		case v >= 90:
			style = th.StatusError
		case v >= 75:
			style = th.Warning
		}
	}
	return style.Render(eighths[min(fill, 8)])
}

// slots is how many time slots fit the plot: one per period, at most one
// per column.
func slots(opts Options, plotW int) int {
	if opts.Period <= 0 {
		return plotW
	}
	return min(plotW, max(1, int(opts.End.Sub(opts.Start)/opts.Period)))
}

// spread stretches slots over width columns.
func spread(slots []float64, width int) []float64 {
	if len(slots) == width {
		return slots
	}
	out := make([]float64, width)
	for c := range out {
		out[c] = slots[c*len(slots)/width]
	}
	return out
}

// bucketize averages points into n equal time slots; empty slots are NaN.
func bucketize(points []domain.Point, start, end time.Time, n int) []float64 {
	sums, counts := make([]float64, n), make([]int, n)
	span := end.Sub(start)
	for _, p := range points {
		if p.Time.Before(start) || !p.Time.Before(end) || span <= 0 {
			continue
		}
		i := int(float64(p.Time.Sub(start)) / float64(span) * float64(n))
		i = min(max(i, 0), n-1)
		sums[i] += p.Value
		counts[i]++
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
		if counts[i] > 0 {
			out[i] = sums[i] / float64(counts[i])
		}
	}
	return out
}

// scaleMax picks a round top for the Y axis; percentages stop at 100.
func scaleMax(unit domain.Unit, peak float64) float64 {
	top := niceCeil(peak)
	if unit == domain.UnitPercent {
		top = min(top, 100)
	}
	return max(top, smallest(unit))
}

func smallest(unit domain.Unit) float64 {
	if unit == domain.UnitPercent {
		return 1
	}
	return 1e-9
}

// niceCeil rounds up to a round number (1, 1.5, 2, 2.5, 3, 4, 5, 6, 8 times
// a power of ten), so the bars use most of the height.
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 0
	}
	exp := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10} {
		if v <= m*exp {
			return m * exp
		}
	}
	return 10 * exp
}

func timeAxis(start, end time.Time, width int) string {
	layout := "15:04"
	if end.Sub(start) > 24*time.Hour {
		layout = "01/02 15:04"
	}
	left, right := start.Local().Format(layout), end.Local().Format(layout)
	gap := width - len(left) - len(right)
	if gap < 1 {
		return right
	}
	return left + strings.Repeat(" ", gap) + right
}

// Format renders a value in its unit, compactly.
func Format(unit domain.Unit, v float64) string {
	switch unit {
	case domain.UnitPercent:
		if v >= 10 || v == 0 {
			return fmt.Sprintf("%.0f%%", v)
		}
		return fmt.Sprintf("%.1f%%", v)
	case domain.UnitBytes:
		return bytes(v)
	case domain.UnitBytesPerSecond:
		return bytes(v) + "/s"
	case domain.UnitPerSecond:
		return compact(v) + "/s"
	case domain.UnitSeconds:
		return duration(v)
	}
	return compact(v)
}

func duration(seconds float64) string {
	switch {
	case seconds == 0:
		return "0s"
	case seconds < 1:
		return fmt.Sprintf("%.0fms", seconds*1000)
	case seconds < 60:
		return fmt.Sprintf("%.1fs", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%.0fm", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%.1fh", seconds/3600)
	}
	return fmt.Sprintf("%.1fd", seconds/86400)
}

func bytes(v float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 || v >= 10 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func compact(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.1fG", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	case v == math.Trunc(v):
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func values(points []domain.Point) []float64 {
	out := make([]float64, len(points))
	for i, p := range points {
		out[i] = p.Value
	}
	return out
}

func mean(vs []float64) float64 {
	sum := 0.0
	for _, v := range vs {
		sum += v
	}
	return sum / float64(len(vs))
}

func slicesMax(vs []float64) float64 {
	m := vs[0]
	for _, v := range vs[1:] {
		m = max(m, v)
	}
	return m
}
