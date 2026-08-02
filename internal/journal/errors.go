package journal

import "errors"

var (
	ErrForbidden = errors.New("required accounting capability missing")
	ErrNotFound  = errors.New("journal entry not found")
)

type ValidationError struct {
	Fields  map[string]string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "validation failed"
}

type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }
