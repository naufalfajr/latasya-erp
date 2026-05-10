package v1

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
)

const (
	rateLimitBucketSize  = 5                // max attempts per window
	rateLimitRefillRate  = 5                // tokens refilled per window
	rateLimitWindow      = 15 * time.Minute // rolling window duration
	rateLimitCleanupIdle = time.Hour        // evict buckets idle longer than this
)

type bucket struct {
	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

func (b *bucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill)
	refill := elapsed.Seconds() / rateLimitWindow.Seconds() * float64(rateLimitRefillRate)
	b.tokens = min(float64(rateLimitBucketSize), b.tokens+refill)
	b.lastRefill = now
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// LoginRateLimiter returns a middleware that applies a token-bucket rate limit
// to login attempts, keyed by clientIP+username. Only failed attempts (4xx)
// consume quota; successful logins restore the token. Limit: 5 per 15 minutes.
func LoginRateLimiter() func(http.Handler) http.Handler {
	var buckets sync.Map

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
			ip := audit.ClientIPFromContext(r.Context())
			if ip == "" {
				ip = r.RemoteAddr
			}

			username := r.FormValue("username")
			if username == "" {
				username = "unknown"
			}

			key := ip + ":" + username

			val, _ := buckets.LoadOrStore(key, &bucket{
				tokens:     float64(rateLimitBucketSize),
				lastRefill: time.Now(),
				lastSeen:   time.Now(),
			})
			b := val.(*bucket)

			if !b.allow() {
				w.Header().Set("Retry-After", strconv.Itoa(int(rateLimitWindow.Seconds())))
				WriteError(w, r, http.StatusTooManyRequests, CodeRateLimited, "too many login attempts, please try again later", nil)
				return
			}

			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			if rw.status >= 200 && rw.status < 300 {
				b.mu.Lock()
				b.tokens = min(float64(rateLimitBucketSize), b.tokens+1)
				b.mu.Unlock()
			}
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(status int) {
	sr.status = status
	sr.ResponseWriter.WriteHeader(status)
}
