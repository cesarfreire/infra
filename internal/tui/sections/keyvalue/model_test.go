package keyvalue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

var orders = domain.Table{
	Name:         "orders",
	Status:       "ACTIVE",
	PartitionKey: domain.KeyAttribute{Name: "customer", Type: "S"},
	SortKey:      &domain.KeyAttribute{Name: "date", Type: "S"},
}

type fakeService struct {
	scans   []domain.Cursor
	queries []domain.KeyQuery
	err     error
	saved   []string
	deleted []domain.Item
}

func (f *fakeService) ListTables(context.Context) ([]domain.Table, error) {
	return []domain.Table{orders, {Name: "users", PartitionKey: domain.KeyAttribute{Name: "id", Type: "S"}}}, nil
}

func (f *fakeService) Scan(_ context.Context, _ domain.Table, cursor domain.Cursor) (domain.ItemPage, error) {
	f.scans = append(f.scans, cursor)
	if f.err != nil {
		return domain.ItemPage{}, f.err
	}
	if cursor == nil {
		return domain.ItemPage{Items: []domain.Item{
			{"customer": "c1", "date": "2026-01-01", "total": domain.Number("10.5"), "status": "paid"},
			{"customer": "c2", "date": "2026-01-02", "total": domain.Number("99"), "status": "open"},
		}, Next: "page2"}, nil
	}
	return domain.ItemPage{Items: []domain.Item{{"customer": "c3", "date": "2026-01-03", "notes": "late"}}}, nil
}

func (f *fakeService) Query(_ context.Context, _ domain.Table, q domain.KeyQuery, _ domain.Cursor) (domain.ItemPage, error) {
	f.queries = append(f.queries, q)
	return domain.ItemPage{Items: []domain.Item{{"customer": q.PartitionValue, "date": "2026-02-01"}}}, nil
}

func (f *fakeService) OpenConsole(domain.Table) (string, error) { return "", nil }

func (f *fakeService) SaveItem(_ context.Context, _ domain.Table, original domain.Item, edited string) (domain.Item, error) {
	f.saved = append(f.saved, edited)
	item := domain.Item{}
	for k, v := range original {
		item[k] = v
	}
	item["status"] = "edited"
	return item, nil
}

func (f *fakeService) DeleteItem(_ context.Context, _ domain.Table, it domain.Item) error {
	f.deleted = append(f.deleted, it)
	return nil
}

func (f *fakeService) Metrics(context.Context, domain.Table, domain.MetricWindow) ([]domain.Series, error) {
	return []domain.Series{{Label: "Read capacity", Unit: domain.UnitPerSecond, Points: []domain.Point{{Time: time.Now().Add(-time.Minute), Value: 3}}}}, nil
}

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func typeText(s string) []tea.Msg {
	var out []tea.Msg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

// send updates m and runs the resulting commands like the runtime would,
// feeding load results back and returning everything else.
func send(m *Model, msgs ...tea.Msg) []tea.Msg {
	var out []tea.Msg
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		_, cmd := m.Update(msg)
		for _, r := range run(cmd) {
			switch r.(type) {
			case tablesLoadedMsg, itemsLoadedMsg, itemChangedMsg:
				msgs = append(msgs, r)
			default:
				out = append(out, r)
			}
		}
	}
	return out
}

func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, run(c)...)
			}
			return out
		}
		if msg == nil || strings.Contains(fmt.Sprintf("%T", msg), "Tick") {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func newModel(t *testing.T) (*Model, *fakeService) {
	t.Helper()
	svc := &fakeService{}
	m := New(svc, section.MemoryDeps(theme.Default()))
	m.SetSize(120, 20)
	m.Focus()
	send(m, run(m.Init())...)
	return m, svc
}

func TestBrowseScanAndLoadMore(t *testing.T) {
	m, svc := newModel(t)
	send(m, press("enter"))
	view := m.View()
	if m.level != levelItems || !strings.Contains(view, "orders › scan") || !strings.Contains(view, "more available") {
		t.Fatalf("enter should scan the table:\n%s", view)
	}
	// Key attributes come first, then the most common attributes.
	if !strings.Contains(view, "customer") || !strings.Contains(view, "status") || !strings.Contains(view, "10.5") {
		t.Errorf("items table missing columns or values:\n%s", view)
	}

	send(m, press("n"))
	if len(svc.scans) != 2 || svc.scans[1] != "page2" || len(m.items.Items()) != 3 {
		t.Fatalf("n should load the next page; scans = %v, items = %d", svc.scans, len(m.items.Items()))
	}
	if strings.Contains(m.View(), "more available") {
		t.Error("no more pages after the last one")
	}
	send(m, press("n"))
	if len(svc.scans) != 2 {
		t.Error("n without a cursor must not scan again")
	}

	send(m, press("esc"))
	if m.level != levelTables || m.InputMode() != section.ModeNormal {
		t.Error("esc should go back to the tables")
	}
}

func TestInspectItem(t *testing.T) {
	m, _ := newModel(t)
	send(m, press("enter"))
	send(m, press("enter"))
	view := m.View()
	if m.view != viewItem || !strings.Contains(view, `"customer": "c1"`) || !strings.Contains(view, `"total": 10.5`) {
		t.Fatalf("enter on an item should show it as JSON:\n%s", view)
	}
	if strings.Index(view, `"customer"`) > strings.Index(view, `"status"`) {
		t.Error("key attributes should come first")
	}
	send(m, press("esc"))
	if m.view != viewList || m.level != levelItems {
		t.Error("esc should return to the items")
	}
}

func TestFindByKey(t *testing.T) {
	m, svc := newModel(t)
	send(m, press("f"))
	if m.view != viewQuery {
		t.Fatal("f should open the query form")
	}
	send(m, press("enter"))
	if m.form.err == nil || len(svc.queries) != 0 {
		t.Fatal("an empty partition key must be rejected before querying")
	}
	send(m, typeText("c9")...)
	send(m, press("tab"))
	send(m, typeText("2026-")...)
	send(m, press("enter"))
	if len(svc.queries) != 1 || svc.queries[0] != (domain.KeyQuery{PartitionValue: "c9", SortKeyPrefix: "2026-"}) {
		t.Fatalf("queries = %v", svc.queries)
	}
	if !strings.Contains(m.View(), "customer = c9, date begins with 2026-") {
		t.Errorf("title should describe the query:\n%s", m.View())
	}
	send(m, press("r"))
	if len(svc.queries) != 2 {
		t.Error("r should rerun the query")
	}
}

func TestItemsFilter(t *testing.T) {
	m, _ := newModel(t)
	send(m, press("enter"))
	send(m, press("/"))
	send(m, typeText("status:open")...)
	if got := m.items.Visible(); len(got) != 1 || got[0]["customer"] != "c2" {
		t.Errorf("visible = %v", got)
	}
}

func TestStaleItemsAreIgnored(t *testing.T) {
	m, _ := newModel(t)
	send(m, press("enter"))
	send(m, itemsLoadedMsg{seq: m.seq - 1, page: domain.ItemPage{Items: []domain.Item{{"customer": "ghost"}}}})
	if strings.Contains(m.View(), "ghost") {
		t.Error("results of an older browse must be ignored")
	}
}

func TestScanAccessDeniedRaisesAlert(t *testing.T) {
	m, svc := newModel(t)
	svc.err = fmt.Errorf("scanning orders: %w", domain.ErrAccessDenied)
	out := send(m, press("enter"))
	var alerted bool
	for _, msg := range out {
		if a, ok := msg.(section.AlertMsg); ok && a.Kind == section.AlertAccessDenied {
			alerted = true
		}
	}
	if !alerted || !strings.Contains(m.View(), "access denied") {
		t.Errorf("expected an access denied alert, got %v", out)
	}
}

func TestTableDetails(t *testing.T) {
	m, _ := newModel(t)
	send(m, press("d"))
	if m.view != viewTableDetails || !strings.Contains(m.View(), "customer (S), date (S)") {
		t.Errorf("details:\n%s", m.View())
	}
}

func TestFormatValue(t *testing.T) {
	tests := map[string]any{
		"":          nil,
		"a⏎b":       "a\nb",
		"12":        domain.Number("12"),
		"true":      true,
		"<3 bytes>": []byte("abc"),
		`["x",1]`:   []any{"x", domain.Number("1")},
		`{"k":"v"}`: map[string]any{"k": "v"},
	}
	for want, v := range tests {
		if got := formatValue(v); got != want {
			t.Errorf("formatValue(%#v) = %q, want %q", v, got, want)
		}
	}
	if err := errors.New("x"); formatValue(err) == "" {
		t.Error("unknown types should still render")
	}
}
