package contact

import "errors"

var (
	ErrForbidden       = errors.New("contacts management capability missing")
	ErrNotFound        = errors.New("contact not found")
	ErrPortalCodeTaken = errors.New("portal code already used by another contact")
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "validation failed" }

type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }
