package v1

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// WriteJSON writes status code and JSON-encodes body to w.
// Sets Content-Type: application/json; charset=utf-8.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("api: encode response", "error", err)
	}
}

// DecodeJSON decodes the JSON request body into dst.
// Caps body at 1MB, rejects unknown fields, returns descriptive errors.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}
