package creditnote

import "errors"

var (
	ErrForbidden = errors.New("invoices.manage capability required")
	ErrNotFound  = errors.New("credit note not found")
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
