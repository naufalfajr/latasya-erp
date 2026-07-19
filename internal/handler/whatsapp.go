package handler

import (
	"net/http"
	"net/url"

	"github.com/naufal/latasya-erp/internal/model"
)

// buildWALink builds a wa.me deep link that opens a chat with the given
// (loosely formatted) Indonesian phone number and a pre-filled message.
func buildWALink(phone, message string) string {
	return "https://wa.me/" + model.NormalizePhoneID(phone) + "?text=" + url.QueryEscape(message)
}

// publicOrigin returns the scheme+host this request arrived on, for
// building absolute links (e.g. a portal link dropped into a WhatsApp
// message) out of otherwise request-relative paths.
func (h *Handler) publicOrigin(r *http.Request) string {
	scheme := "https"
	if h.DevMode {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}
