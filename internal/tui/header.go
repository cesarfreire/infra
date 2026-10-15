package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type headerTarget int

const (
	targetNone headerTarget = iota
	targetProfile
	targetRegion
	targetUpdate
)

type headerSegment struct {
	text   string
	target headerTarget
}

// headerSegments lays out the header; rendering and mouse hit-testing both
// use it so they never disagree.
func (m Model) headerSegments() []headerSegment {
	th := m.theme
	segs := []headerSegment{{text: th.Header.Render(m.opts.Title)}}
	if m.workspace == nil {
		return m.withUpdate(segs)
	}
	region := m.region
	if region == "" {
		region = "none"
	}
	segs = append(segs,
		headerSegment{text: " "},
		headerSegment{text: th.HeaderChip.Render("profile: " + profileLabel(m.profile) + " ▾"), target: targetProfile},
		headerSegment{text: " "},
		headerSegment{text: th.HeaderChip.Render("region: " + region + " ▾"), target: targetRegion},
		headerSegment{text: " "},
	)
	if m.readOnly {
		segs = append(segs, headerSegment{text: th.ReadOnlyChip.Render("read-only")}, headerSegment{text: " "})
	}
	var status string
	switch {
	case m.switching:
		status = th.Muted.Render("switching…")
	case m.verifying:
		status = th.Muted.Render("checking credentials…")
	case m.identityErr != nil:
		status = th.StatusError.Render("⚠ credentials problem")
	case m.identity.Account != "":
		status = th.Muted.Render("account " + m.identity.Account)
	}
	return m.withUpdate(append(segs, headerSegment{text: status}))
}

// withUpdate right-aligns the new-version notice, when there is one.
func (m Model) withUpdate(segs []headerSegment) []headerSegment {
	if m.update == nil {
		return segs
	}
	notice := m.theme.Selected.Render("↑ " + m.update.Version + " available")
	used := 0
	for _, s := range segs {
		used += lipgloss.Width(s.text)
	}
	gap := m.width - used - lipgloss.Width(notice) - 1
	if gap < 1 {
		return segs // no room; the header stays as it was
	}
	return append(segs, headerSegment{text: strings.Repeat(" ", gap)}, headerSegment{text: notice, target: targetUpdate})
}

func (m Model) renderHeader() string {
	var out string
	for _, s := range m.headerSegments() {
		out += s.text
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(out)
}

// headerTargetAt returns what is under column x of the header.
func (m Model) headerTargetAt(x int) headerTarget {
	pos := 0
	for _, s := range m.headerSegments() {
		w := lipgloss.Width(s.text)
		if x >= pos && x < pos+w {
			return s.target
		}
		pos += w
	}
	return targetNone
}
