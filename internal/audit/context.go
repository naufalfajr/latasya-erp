package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
)

type contextKey string

const (
	requestIDKey contextKey = "audit.request_id"
	clientIPKey  contextKey = "audit.client_ip"
	tokenIDKey   contextKey = "audit.token_id"
)

// WithTokenID stores the API token ID on the context so audit.Log can record
// which token authorised a Bearer-authenticated mutation. Set by the v1
// BearerOrCookie middleware in parallel with its own private token-ID key,
// to avoid an audit→v1 import cycle.
func WithTokenID(ctx context.Context, id int) context.Context {
	return context.WithValue(ctx, tokenIDKey, id)
}

// TokenIDFromContext returns the token ID stored by WithTokenID, or nil if
// the request was cookie-authenticated or anonymous.
func TokenIDFromContext(ctx context.Context) *int {
	if v, ok := ctx.Value(tokenIDKey).(int); ok {
		return &v
	}
	return nil
}

// RequestContext attaches a random request_id and the best-effort client IP
// to the context. Audit rows correlate to the request that produced them via
// the request_id, and capture the IP for forensics on security events.
func RequestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, requestIDKey, newRequestID())
		ctx = context.WithValue(ctx, clientIPKey, clientIP(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request_id attached by RequestContext, or
// empty if none was set (e.g. handler invoked outside the middleware chain).
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// ClientIPFromContext returns the client IP attached by RequestContext.
func ClientIPFromContext(ctx context.Context) string {
	v, _ := ctx.Value(clientIPKey).(string)
	return v
}

func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// clientIP pulls the caller's IP, preferring X-Forwarded-For's first entry
// when present (we sit behind Cloudflare). Falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if cfip := r.Header.Get("CF-Connecting-IP"); cfip != "" {
		return cfip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
