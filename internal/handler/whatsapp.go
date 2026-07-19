package handler

import (
	"net/http"
	"net/url"
	"strings"
)

// normalizePhoneID converts a loosely formatted Indonesian phone number
// (leading 0, spaces, dashes, +62) into the digits-only 62xxxxxxxxxx form
// wa.me requires.
func normalizePhoneID(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	switch {
	case strings.HasPrefix(d, "62"):
		return d
	case strings.HasPrefix(d, "0"):
		return "62" + d[1:]
	default:
		return d
	}
}

// buildWALink builds a wa.me deep link that opens a chat with the given
// (loosely formatted) Indonesian phone number and a pre-filled message.
func buildWALink(phone, message string) string {
	return "https://wa.me/" + normalizePhoneID(phone) + "?text=" + url.QueryEscape(message)
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
