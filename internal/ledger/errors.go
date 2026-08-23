package ledger

import "fmt"

type ConstraintError struct {
	Field string
	Value string
}

func (e ConstraintError) Error() string { return fmt.Sprintf("constraint %s=%s", e.Field, e.Value) }
