package account

import "errors"

var (
	ErrForbidden = errors.New("accounts management capability missing")
	ErrNotFound  = errors.New("account not found")
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "validation failed" }

type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }
