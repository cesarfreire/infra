package keyvalue

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/theme"
)

// queryForm asks for the partition key value and, for tables with a string
// sort key, an optional sort key prefix.
type queryForm struct {
	pk, sk  textinput.Model
	hasSK   bool
	focusSK bool
	err     error
}

func newQueryForm() queryForm {
	return queryForm{pk: textinput.New(), sk: textinput.New()}
}

func (f *queryForm) reset(t domain.Table) tea.Cmd {
	f.pk.SetValue("")
	f.sk.SetValue("")
	f.pk.Prompt = t.PartitionKey.Name + " = "
	f.pk.Placeholder = "value (" + t.PartitionKey.Type + ")"
	f.hasSK = t.SortKey != nil && t.SortKey.Type == "S"
	if f.hasSK {
		f.sk.Prompt = t.SortKey.Name + " begins with "
		f.sk.Placeholder = "optional"
	}
	f.err = nil
	f.focusSK = false
	f.sk.Blur()
	return f.pk.Focus()
}

func (f *queryForm) toggle() tea.Cmd {
	if !f.hasSK {
		return nil
	}
	f.focusSK = !f.focusSK
	if f.focusSK {
		f.pk.Blur()
		return f.sk.Focus()
	}
	f.sk.Blur()
	return f.pk.Focus()
}

func (f *queryForm) update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		f.err = nil
	}
	var cmd tea.Cmd
	if f.focusSK {
		f.sk, cmd = f.sk.Update(msg)
	} else {
		f.pk, cmd = f.pk.Update(msg)
	}
	return cmd
}

func (f *queryForm) query() domain.KeyQuery {
	q := domain.KeyQuery{PartitionValue: strings.TrimSpace(f.pk.Value())}
	if f.hasSK {
		q.SortKeyPrefix = f.sk.Value()
	}
	return q
}

func (f queryForm) view(th theme.Theme, table string) string {
	lines := []string{th.Title.Render("Find items") + " " + th.Muted.Render(table), "", f.pk.View()}
	if f.hasSK {
		lines = append(lines, f.sk.View())
	}
	if f.err != nil {
		lines = append(lines, "", th.StatusError.Render(f.err.Error()))
	}
	lines = append(lines, "", th.Muted.Render("Runs a Query on the table's primary key (no full scan)."))
	return strings.Join(lines, "\n")
}
