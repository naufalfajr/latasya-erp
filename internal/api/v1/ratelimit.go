package v1

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/naufal/latasya-erp/internal/auth"
)

const (
	rateLimitBucketSize  = 5                // max login attempts per window
	rateLimitWindow      = 15 * time.Minute // rolling window for logins
	rateLimitCleanupIdle = time.Hour        // evict buckets idle longer than this

	// 1000 codes per name; 5/hour makes a sweep take ~8 days.
	portalCodeBucketSize = 5
	portalCodeWindow     = time.Hour

	// Authenticated API traffic per credential. Generous for humans and bots
	// (2/s sustained, burst 120), small enough that a runaway agent loop
	// cannot monopolize the single SQLite writer.
	bearerRateLimitBucket = 120
	bearerRateLimitWindow = time.Minute

	// One MCP login polls every 5s for at most 10 minutes = 120 calls. All
	// calls consume quota: the endpoint is unauthenticated, so the bucket
	// also caps junk-row creation.
	mcpDeviceBucketSize = 120
	mcpDeviceRateWindow = 15 * time.Minute
)

type bucket struct {
	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

func (b *bucket) allow(size float64, window time.Duration) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill)
	refill := elapsed.Seconds() / window.Seconds() * size
	b.tokens = min(size, b.tokens+refill)
	b.lastRefill = now
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// newRateLimiter returns a token-bucket middleware keyed by keyFn. When
// refundOn2xx is set, successful responses give the token back: right for
// brute-force surfaces (login, portal codes) where only failures should
// count; throughput caps pass false so every request consumes quota.
func newRateLimiter(size int, window time.Duration, keyFn func(*http.Request) string, deny http.HandlerFunc, refundOn2xx bool) func(http.Handler) http.Handler {
	var buckets sync.Map
	bucketSize := float64(size)

	go func() {
		ticker := time.NewTicker(rateLimitCleanupIdle)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-rateLimitCleanupIdle)
			buckets.Range(func(k, v any) bool {
				b := v.(*bucket)
				b.mu.Lock()
				idle := b.lastSeen.Before(cutoff)
				b.mu.Unlock()
				if idle {
					buckets.Delete(k)
				}
				return true
			})
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			val, _ := buckets.LoadOrStore(keyFn(r), &bucket{
				tokens:     bucketSize,
				lastRefill: time.Now(),
				lastSeen:   time.Now(),
			})
			b := val.(*bucket)

			if !b.allow(bucketSize, window) {
				w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
				deny(w, r)
				return
			}

			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			if refundOn2xx && rw.status >= 200 && rw.status < 300 {
				b.mu.Lock()
				b.tokens = min(bucketSize, b.tokens+1)
				b.mu.Unlock()
			}
		})
	}
}

// unspoofableClientIP keys rate limits. Not audit's client IP: that trusts
// X-Forwarded-For, which a caller forges to mint a fresh bucket per request.
func unspoofableClientIP(r *http.Request) string {
	if cfip := r.Header.Get("CF-Connecting-IP"); cfip != "" {
		return cfip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// LoginRateLimiter throttles login attempts per client IP + username.
// Limit: 5 per 15 minutes; only failed attempts consume quota.
func LoginRateLimiter() func(http.Handler) http.Handler {
	return newRateLimiter(rateLimitBucketSize, rateLimitWindow,
		func(r *http.Request) string {
			username := r.FormValue("username")
			if username == "" {
				username = "unknown"
			}
			return unspoofableClientIP(r) + ":" + username
		},
		func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, r, http.StatusTooManyRequests, CodeRateLimited, "too many login attempts, please try again later", nil)
		}, true)
}

// PortalCodeLimiter throttles guesses at the short portal code, which is
// short enough to brute-force and has no login behind it.
func PortalCodeLimiter() func(http.Handler) http.Handler {
	return newRateLimiter(portalCodeBucketSize, portalCodeWindow, unspoofableClientIP,
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Terlalu banyak percobaan. Coba lagi beberapa saat lagi.", http.StatusTooManyRequests)
		}, true)
}

// BearerRateLimiter caps authenticated /api/v1 traffic per credential
// (bearer token id, else session user id). Unlike the login limiter, every
// request consumes quota: this is a throughput cap protecting the single
// SQLite connection, not a brute-force guard.
func BearerRateLimiter() func(http.Handler) http.Handler {
	return newRateLimiter(bearerRateLimitBucket, bearerRateLimitWindow,
		func(r *http.Request) string {
			if id := TokenIDFromContext(r.Context()); id != nil {
				return "t:" + strconv.Itoa(*id)
			}
			if user := auth.UserFromContext(r.Context()); user != nil {
				return "u:" + strconv.Itoa(user.ID)
			}
			return unspoofableClientIP(r)
		},
		func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, r, http.StatusTooManyRequests, CodeRateLimited, "rate limit exceeded, slow down", nil)
		}, false)
}

// MCPDeviceLimiter throttles the unauthenticated MCP login-poll endpoint per
// client IP: enough headroom for one full 10-minute handoff, a hard cap on
// anything faster.
func MCPDeviceLimiter() func(http.Handler) http.Handler {
	return newRateLimiter(mcpDeviceBucketSize, mcpDeviceRateWindow, unspoofableClientIP,
		func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, r, http.StatusTooManyRequests, CodeRateLimited, "too many login attempts, please try again later", nil)
		}, false)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(status int) {
	sr.status = status
	sr.ResponseWriter.WriteHeader(status)
}
