// Package details renders read-only key/value views of a resource.
package details

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"infra/internal/tui/theme"
)

// Field is a labeled value; empty values render as "-".
type Field struct {
	Label string
	Value string
}

// Section is a titled group of fields rendered after the main ones.
type Section struct {
	Title  string
	Fields []Field
}

// Render renders a heading, fields and extra sections.
func Render(th theme.Theme, title, subtitle string, fields []Field, sections ...Section) string {
	var b strings.Builder
	b.WriteString(th.Title.Render(title) + " " + th.Muted.Render(subtitle) + "\n\n")
	writeFields(&b, th, fields)
	for _, s := range sections {
		if len(s.Fields) == 0 {
			continue
		}
		b.WriteString("\n" + th.Title.Render(s.Title) + "\n")
		writeFields(&b, th, s.Fields)
	}
	return b.String()
}

// Tags converts a tag map into fields sorted by key.
func Tags(tags map[string]string) []Field {
	fields := make([]Field, 0, len(tags))
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		fields = append(fields, Field{Label: k, Value: tags[k]})
	}
	return fields
}

func writeFields(b *strings.Builder, th theme.Theme, fields []Field) {
	width := 0
	for _, f := range fields {
		width = max(width, len(f.Label))
	}
	for _, f := range fields {
		value := f.Value
		if value == "" {
			value = "-"
		}
		b.WriteString(th.Label.Render(fmt.Sprintf("%-*s", width+2, f.Label)) + value + "\n")
	}
}
