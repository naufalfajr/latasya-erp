package invoice

import "errors"

var (
	ErrForbidden = errors.New("invoices.manage capability required")
	ErrNotFound  = errors.New("invoice not found")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "validation failed" }

type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string { return e.Message }
