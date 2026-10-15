// Package filter implements the query language used to filter resource lists.
//
// A query is a whitespace-separated list of terms, all of which must match
// (AND). A term is either free text, matched against every field, or
// field:value, matched only against fields with that name. Matching is a
// case- and accent-insensitive substring search, e.g. `web state:running az:1a`.
package filter

import "strings"

// Field is a named, filterable value of a resource. Several fields may share
// a name (e.g. private and public "ip").
type Field struct {
	Name  string
	Value string
}

type term struct {
	field string // empty for free text
	value string
}

// Query is a parsed filter expression. The zero value matches everything.
type Query struct {
	raw   string
	terms []term
}

// Fold lowercases s and strips common Latin diacritics, so "sao" matches
// "São" and "acao" matches "Ação".
func Fold(s string) string {
	return accents.Replace(strings.ToLower(s))
}

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// Parse parses a query expression.
func Parse(s string) Query {
	q := Query{raw: strings.TrimSpace(s)}
	for _, tok := range strings.Fields(Fold(s)) {
		if name, value, ok := strings.Cut(tok, ":"); ok && name != "" && value != "" {
			q.terms = append(q.terms, term{field: name, value: value})
			continue
		}
		q.terms = append(q.terms, term{value: strings.TrimSuffix(tok, ":")})
	}
	return q
}

// String returns the query as typed, trimmed.
func (q Query) String() string { return q.raw }

// Empty reports whether the query matches everything.
func (q Query) Empty() bool { return len(q.terms) == 0 }

// Match reports whether fields satisfy every term of the query.
func (q Query) Match(fields []Field) bool {
	for _, t := range q.terms {
		if !t.match(fields) {
			return false
		}
	}
	return true
}

func (t term) match(fields []Field) bool {
	for _, f := range fields {
		if t.field != "" && !strings.EqualFold(f.Name, t.field) {
			continue
		}
		if strings.Contains(Fold(f.Value), t.value) {
			return true
		}
	}
	return false
}

// Apply returns the items whose fields match q, preserving order.
func Apply[T any](q Query, items []T, fields func(T) []Field) []T {
	if q.Empty() {
		return items
	}
	out := make([]T, 0, len(items))
	for _, it := range items {
		if q.Match(fields(it)) {
			out = append(out, it)
		}
	}
	return out
}
