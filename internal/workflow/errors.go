package workflow

type FieldError struct{ Field, Message string }

func (e FieldError) Error() string { return e.Field + ": " + e.Message }
