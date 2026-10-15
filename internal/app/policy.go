package app

import (
	"fmt"

	"infra/internal/domain"
)

// Policy decides whether use cases may change resources. The UI checks it
// too, to fail early, but use cases are the authority.
type Policy struct {
	ReadOnly bool
}

// CheckWrite returns domain.ErrReadOnly when changes are not allowed.
func (p Policy) CheckWrite(action string) error {
	if p.ReadOnly {
		return fmt.Errorf("%s: %w", action, domain.ErrReadOnly)
	}
	return nil
}
