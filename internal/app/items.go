package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"infra/internal/domain"
)

// ParseItemJSON parses an item edited as JSON. Numbers keep their exact
// text as domain.Number.
func ParseItemJSON(text string) (domain.Item, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.More() {
		return nil, errors.New("invalid JSON: unexpected content after the item")
	}
	item := make(domain.Item, len(raw))
	for k, v := range raw {
		item[k] = fromJSON(v)
	}
	return item, nil
}

func fromJSON(v any) any {
	switch v := v.(type) {
	case json.Number:
		return domain.Number(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = fromJSON(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = fromJSON(e)
		}
		return out
	}
	return v
}

// PrepareItemUpdate checks that the key attributes did not change and gives
// back the types JSON cannot express: sets and binary values of the
// original item stay sets and binaries when edited compatibly.
func PrepareItemUpdate(table domain.Table, original, edited domain.Item) (domain.Item, error) {
	for name, want := range table.Key(original) {
		got, ok := edited[name]
		if !ok || !reflect.DeepEqual(restore(want, got), want) {
			return nil, fmt.Errorf("key attribute %q cannot change; create a new item instead", name)
		}
	}
	out := make(domain.Item, len(edited))
	for k, v := range edited {
		out[k] = restore(original[k], v)
	}
	return out, nil
}

// restore converts edited back to the type of original when compatible.
func restore(original, edited any) any {
	switch o := original.(type) {
	case []byte:
		if s, ok := edited.(string); ok {
			if b, err := base64.StdEncoding.DecodeString(s); err == nil {
				return b
			}
		}
	case domain.StringSet:
		if list, ok := edited.([]any); ok {
			if set, ok := convertAll(list, func(e any) (string, bool) { s, ok := e.(string); return s, ok }); ok {
				return domain.StringSet(set)
			}
		}
	case domain.NumberSet:
		if list, ok := edited.([]any); ok {
			if set, ok := convertAll(list, func(e any) (domain.Number, bool) { n, ok := e.(domain.Number); return n, ok }); ok {
				return domain.NumberSet(set)
			}
		}
	case domain.BinarySet:
		if list, ok := edited.([]any); ok {
			set, ok := convertAll(list, func(e any) ([]byte, bool) {
				s, ok := e.(string)
				if !ok {
					return nil, false
				}
				b, err := base64.StdEncoding.DecodeString(s)
				return b, err == nil
			})
			if ok {
				return domain.BinarySet(set)
			}
		}
	case map[string]any:
		if m, ok := edited.(map[string]any); ok {
			out := make(map[string]any, len(m))
			for k, v := range m {
				out[k] = restore(o[k], v)
			}
			return out
		}
	}
	return edited
}

func convertAll[T any](list []any, conv func(any) (T, bool)) ([]T, bool) {
	out := make([]T, 0, len(list))
	for _, e := range list {
		v, ok := conv(e)
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}
