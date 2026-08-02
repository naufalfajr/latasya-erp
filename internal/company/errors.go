package company

import "errors"

var (
	ErrForbidden = errors.New("company profile management permission missing")
	ErrNotFound  = errors.New("company profile not found")
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "validation failed" }
