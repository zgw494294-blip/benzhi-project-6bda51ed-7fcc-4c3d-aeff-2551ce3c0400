package ledger

import (
	"errors"
	"fmt"
)

type ConstraintError struct {
	Field string
	Value string
}

func (e ConstraintError) Error() string { return fmt.Sprintf("constraint %s=%s", e.Field, e.Value) }

// IntegrityError describes a corrupted cross-entity relationship that the
// startup migration detected, such as a frozen snapshot referencing a dossier
// that no longer exists. Returning this error from Migrate aborts startup so
// the service never listens on top of broken state.
type IntegrityError struct {
	Kind   string
	Target string
	Detail string
}

func (e IntegrityError) Error() string {
	detail := e.Detail
	if detail == "" {
		detail = "关系校验失败"
	}
	return fmt.Sprintf("ledger integrity failure: %s %s: %s", e.Kind, e.Target, detail)
}

// Is makes IntegrityError comparable so callers can use errors.Is while still
// exposing the structured kind/target fields for diagnostics.
func (e IntegrityError) Is(target error) bool {
	ie, ok := target.(IntegrityError)
	if !ok {
		return false
	}
	return ie.Kind == "" || ie.Kind == e.Kind
}

// ErrIntegrity provides a sentinel for integrity failures raised by migrations.
var ErrIntegrity = errors.New("ledger integrity failure")
