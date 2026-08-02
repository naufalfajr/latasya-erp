package invoice

import "errors"

var (
	ErrForbidden               = errors.New("invoices.manage capability required")
	ErrNotFound                = errors.New("invoice not found")
	ErrNoDefaultRevenueAccount = errors.New("set a default revenue account in Company Profile before generating recurring invoices")
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

type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string { return e.Message }
