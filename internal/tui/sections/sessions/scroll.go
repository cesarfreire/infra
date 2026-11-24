package sessions

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"infra/internal/session"
	"infra/internal/tui/section"
)

const wheelLines = 3

// rows is the height of the terminal area below the title line.
func (m *Model) rows() int { return max(1, m.height-termTop) }

// viewTop returns the first line to show, clamped to the available lines.
// shortcut: top is an absolute line index, so the view drifts by one line per
// line dropped once the scrollback is full (10k lines); keep it that way
// unless that becomes noticeable.
func (m *Model) viewTop(s *session.Session) int {
	bottom := max(0, s.LineCount()-m.rows())
	m.top = max(0, m.top)
	if m.follow || m.top >= bottom {
		m.top, m.follow = bottom, true
	}
	return m.top
}

// scroll moves the view by delta lines; reaching the bottom follows the
// output again.
func (m *Model) scroll(s *session.Session, delta int) {
	m.top = m.viewTop(s) + delta
	m.follow = false
	m.viewTop(s)
}

// scrollStep is a page for pgup/pgdown and a line for the arrows.
func (m *Model) scrollStep(msg tea.KeyPressMsg) int {
	if strings.HasSuffix(msg.String(), "pgup") || strings.HasSuffix(msg.String(), "pgdown") {
		return max(1, m.rows()-1)
	}
	return 1
}

// pos is a cell in session.Lines coordinates.
type pos struct{ line, col int }

func (p pos) before(o pos) bool { return p.line < o.line || (p.line == o.line && p.col < o.col) }

// selection is a mouse selection from anchor to end, both inclusive.
type selection struct {
	active, dragging bool
	anchor, end      pos
}

// bounds returns the selection in reading order.
func (s selection) bounds() (pos, pos) {
	if s.end.before(s.anchor) {
		return s.end, s.anchor
	}
	return s.anchor, s.end
}

// highlight reverses the selected cells of line, which is line number n.
func (s selection) highlight(line uv.Line, n int) {
	if !s.active {
		return
	}
	from, to := s.bounds()
	if n < from.line || n > to.line {
		return
	}
	for x := range line {
		if (n > from.line || x >= from.col) && (n < to.line || x <= to.col) {
			line[x].Style.Attrs |= uv.AttrReverse
		}
	}
}

// at converts a mouse position in the panel to a line position, scrolling
// when a drag goes past the top or bottom of the terminal area.
func (m *Model) at(s *session.Session, mouse tea.Mouse) pos {
	row := mouse.Y - termTop
	switch {
	case row < 0:
		m.scroll(s, row)
		row = 0
	case row >= m.rows():
		m.scroll(s, row-m.rows()+1)
		row = m.rows() - 1
	}
	return pos{line: m.viewTop(s) + row, col: max(0, mouse.X)}
}

func (m *Model) startSelection(s *session.Session, mouse tea.Mouse) {
	if mouse.Button != tea.MouseLeft || mouse.Y < termTop {
		m.sel = selection{}
		return
	}
	p := m.at(s, mouse)
	m.sel = selection{active: true, dragging: true, anchor: p, end: p}
}

func (m *Model) extendSelection(s *session.Session, mouse tea.Mouse) {
	if m.sel.dragging {
		m.sel.end = m.at(s, mouse)
	}
}

// copySelection ends a drag and copies the text with OSC 52. A click
// without a drag clears the selection.
func (m *Model) copySelection(s *session.Session) tea.Cmd {
	if !m.sel.dragging {
		return nil
	}
	m.sel.dragging = false
	if m.sel.anchor == m.sel.end {
		m.sel = selection{}
		return nil
	}
	text := m.selectedText(s)
	return tea.Batch(tea.SetClipboard(text), section.Status(fmt.Sprintf("Copied %d characters", len([]rune(text)))))
}

func (m *Model) selectedText(s *session.Session) string {
	from, to := m.sel.bounds()
	lines := s.Lines(from.line, to.line-from.line+1)
	out := make([]string, len(lines))
	for i, line := range lines {
		n := from.line + i
		start, end := 0, len(line)
		if n == from.line {
			start = min(from.col, len(line))
		}
		if n == to.line {
			end = min(to.col+1, len(line))
		}
		out[i] = strings.TrimRight(line[start:max(start, end)].String(), " ")
	}
	return strings.Join(out, "\n")
}
