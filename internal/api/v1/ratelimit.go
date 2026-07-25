package v1

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	rateLimitBucketSize  = 5                // max login attempts per window
	rateLimitWindow      = 15 * time.Minute // rolling window for logins
	rateLimitCleanupIdle = time.Hour        // evict buckets idle longer than this

	// 1000 codes per name; 5/hour makes a sweep take ~8 days.
	portalCodeBucketSize = 5
	portalCodeWindow     = time.Hour
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

// newRateLimiter returns a token-bucket middleware keyed by keyFn. Only
// non-2xx consumes quota, so legitimate callers are never throttled.
func newRateLimiter(size int, window time.Duration, keyFn func(*http.Request) string, deny http.HandlerFunc) func(http.Handler) http.Handler {
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

			if rw.status >= 200 && rw.status < 300 {
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
		})
}

// PortalCodeLimiter throttles guesses at the short portal code, which is
// short enough to brute-force and has no login behind it.
func PortalCodeLimiter() func(http.Handler) http.Handler {
	return newRateLimiter(portalCodeBucketSize, portalCodeWindow, unspoofableClientIP,
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Terlalu banyak percobaan. Coba lagi beberapa saat lagi.", http.StatusTooManyRequests)
		})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(status int) {
	sr.status = status
	sr.ResponseWriter.WriteHeader(status)
}
