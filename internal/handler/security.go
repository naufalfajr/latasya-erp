package handler

import "net/http"

// securityHeaders is the app-wide response hardening. The CSP keeps
// 'unsafe-inline' and 'unsafe-eval' for scripts: the HTMX templates embed
// inline blocks in a dozen pages, and htmx's hx-vals="js:…" expressions
// (journals form) evaluate via the Function constructor. The policy still
// blocks the attacks that matter for this app — external script/font/style
// loads, outbound beacons (connect-src), framing, and form exfiltration to
// other origins. Nonce-izing inline blocks and replacing js: expressions is
// the upgrade path when templates are consolidated.
var securityHeaders = http.Header{
	"X-Content-Type-Options": []string{"nosniff"},
	"Referrer-Policy":        []string{"same-origin"},
	"X-Frame-Options":        []string{"DENY"},
	"Content-Security-Policy": []string{
		"default-src 'self'; " +
			"script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
			"font-src 'self' https://fonts.gstatic.com; " +
			"img-src 'self' data:; " +
			"connect-src 'self'; " +
			"frame-ancestors 'none'; " +
			"base-uri 'self'; " +
			// Chromium applies form-action to post-submit redirects: the
			// OAuth consent 303s to the client's loopback callback and
			// Google Calendar connect 303s to accounts.google.com, so both
			// must be allowed or the flows die in the browser (invisible to
			// Go http tests, which don't enforce CSP). ::1 cannot be
			// expressed as a CSP host-source, so validRedirectURI drops it.
			"form-action 'self' https://accounts.google.com http://127.0.0.1:* http://localhost:*"},
}

// SecurityHeaders sets app-wide defensive headers on every response. API
// responses additionally get Cache-Control: no-store so financial JSON is
// never cached by shared caches or the browser. Headers already set by an
// inner handler (e.g. the portal's private, no-store) are never overridden.
// HSTS is sent only outside dev mode: local runs are plain HTTP.
func SecurityHeaders(devMode bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			for name, values := range securityHeaders {
				if h.Get(name) == "" {
					h[name] = values
				}
			}
			if !devMode {
				if h.Get("Strict-Transport-Security") == "" {
					h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
				}
			}
			if h.Get("Cache-Control") == "" &&
				(len(r.URL.Path) >= len("/api/v1/") && r.URL.Path[:len("/api/v1/")] == "/api/v1/") {
				h.Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}
