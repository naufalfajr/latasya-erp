// Package mcpauth is the JSON transport for the MCP device-login poll
// endpoint. It is deliberately unauthenticated: the caller proves itself by
// holding the 256-bit device code, and the endpoint returns nothing but
// status until the approve-and-consume handshake completes.
package mcpauth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/mcpauth"
)

type Handler struct {
	MCP *mcpauth.Module
}

type devicePollInput struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	Client     string `json:"client"`
}

// DevicePoll handles POST /api/v1/mcp/device. The first poll registers the
// pending handoff; later polls report its status. An approved handoff mints
// the token in this response, exactly once; plaintext is never stored.
func (h *Handler) DevicePoll(w http.ResponseWriter, r *http.Request) {
	var inp devicePollInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		return // DecodeJSON wrote the error envelope
	}
	inp.DeviceCode = strings.TrimSpace(inp.DeviceCode)
	inp.UserCode = strings.TrimSpace(inp.UserCode)
	inp.Client = strings.TrimSpace(inp.Client)

	// The client generates 32 random bytes base64url-encoded (43 chars) and
	// an 8-char user code. Length checks keep junk out of the table; the
	// device code is stored only as a SHA-256 hash.
	if len(inp.DeviceCode) < 32 || len(inp.DeviceCode) > 128 ||
		len(inp.UserCode) < 4 || len(inp.UserCode) > 16 ||
		len(inp.Client) == 0 || len(inp.Client) > 128 {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid device login payload", nil)
		return
	}

	sum := sha256.Sum256([]byte(inp.DeviceCode))
	deviceHash := hex.EncodeToString(sum[:])

	handoff, err := h.MCP.Touch(r.Context(), deviceHash, inp.UserCode, inp.Client)
	if err != nil {
		writeHandoffError(w, r, err)
		return
	}

	if handoff.Status == "approved" {
		created, latest, err := h.MCP.Consume(r.Context(), deviceHash)
		if err == nil {
			v1.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
				"status":     "approved",
				"token":      created.Plaintext,
				"token_id":   created.Token.ID,
				"scopes":     created.Token.Scopes,
				"username":   latest.Username,
				"expires_at": created.Token.ExpiresAt,
			}})
			return
		}
		if errors.Is(err, mcpauth.ErrPending) || errors.Is(err, mcpauth.ErrDenied) {
			handoff = latest // the decision changed between touch and consume
		} else {
			writeHandoffError(w, r, err)
			return
		}
	}

	if handoff.Status == "denied" {
		writeHandoffError(w, r, mcpauth.ErrDenied)
		return
	}
	v1.WriteJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"status": "pending"}})
}

func writeHandoffError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, mcpauth.ErrDenied):
		v1.WriteError(w, r, http.StatusForbidden, "access_denied", "handoff denied", nil)
	case errors.Is(err, mcpauth.ErrExpired), errors.Is(err, mcpauth.ErrGone), errors.Is(err, mcpauth.ErrInvalidCode):
		v1.WriteError(w, r, http.StatusGone, "expired_token", "handoff expired or unknown, restart login", nil)
	case errors.Is(err, mcpauth.ErrScopeChange):
		v1.WriteError(w, r, http.StatusForbidden, "scope_changed", err.Error(), nil)
	default:
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "device login failed", nil)
	}
}
