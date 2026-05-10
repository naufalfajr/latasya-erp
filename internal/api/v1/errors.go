package v1

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/audit"
)

// Error code constants — all snake_case strings
const (
	CodeInvalidRequest        = "invalid_request"
	CodeValidationFailed      = "validation_failed"
	CodeUnauthorized          = "unauthorized"
	CodeForbidden             = "forbidden"
	CodeNotFound              = "not_found"
	CodeConflict              = "conflict"
	CodeRateLimited           = "rate_limited"
	CodeInvalidToken          = "invalid_token"
	CodePasswordChangeRequired = "password_change_required"
	CodeIdempotencyConflict   = "idempotency_conflict"
	CodeInternal              = "internal_error"
)

// ErrorEnvelope is the standard JSON error response shape.
type ErrorEnvelope struct {
	Error     string            `json:"error"`
	Code      string            `json:"code"`
	Fields    map[string]string `json:"fields,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

// WriteError writes a JSON error response with the standard envelope.
// It pulls request_id from the audit context (injected by audit.RequestContext middleware).
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	env := ErrorEnvelope{
		Error:     message,
		Code:      code,
		Fields:    fields,
		RequestID: audit.RequestIDFromContext(r.Context()),
	}
	WriteJSON(w, status, env)
}
