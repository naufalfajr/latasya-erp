package v1

import (
	"errors"
	"net/http"

	"github.com/naufal/latasya-erp/internal/journal"
)

type JournalErrorLabels struct {
	Capability string
	NotFound   string
	Operation  string
	BalanceKey string
}

// WriteJournalError translates journal-module errors for JSON transports.
func WriteJournalError(w http.ResponseWriter, r *http.Request, err error, labels JournalErrorLabels) bool {
	if err == nil {
		return true
	}
	var validation *journal.ValidationError
	var conflict *journal.ConflictError
	switch {
	case errors.Is(err, journal.ErrForbidden):
		WriteError(w, r, http.StatusForbidden, CodeForbidden, labels.Capability+" capability required", nil)
	case errors.Is(err, journal.ErrNotFound):
		WriteError(w, r, http.StatusNotFound, CodeNotFound, labels.NotFound+" not found", nil)
	case errors.As(err, &validation):
		fields := validation.Fields
		if labels.BalanceKey != "" {
			if balance, ok := fields["balance"]; ok {
				fields = make(map[string]string, len(validation.Fields))
				for key, value := range validation.Fields {
					fields[key] = value
				}
				delete(fields, "balance")
				fields[labels.BalanceKey] = balance
			}
		}
		WriteError(w, r, http.StatusUnprocessableEntity, CodeValidationFailed, validation.Error(), fields)
	case errors.As(err, &conflict):
		WriteError(w, r, http.StatusConflict, CodeConflict, conflict.Error(), nil)
	default:
		WriteError(w, r, http.StatusInternalServerError, CodeInternal, labels.Operation+" failed", nil)
	}
	return false
}
