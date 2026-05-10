package v1

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// idempotencyLocks serializes concurrent requests sharing the same
// (user_id, key) so the wrapped handler runs at most once per key. Without
// this, two requests racing on a fresh key would both pass the lookup and
// invoke the handler before either could persist the response.
var idempotencyLocks sync.Map

func acquireIdempotencyLock(userID int, key string) *sync.Mutex {
	lockKey := fmt.Sprintf("%d:%s", userID, key)
	mu, _ := idempotencyLocks.LoadOrStore(lockKey, &sync.Mutex{})
	m := mu.(*sync.Mutex)
	m.Lock()
	return m
}

// Idempotency returns a middleware that implements Idempotency-Key replay
// semantics. Active only on POST requests carrying an Idempotency-Key header
// from an authenticated user; other requests pass through untouched.
//
//   - First request for (user, key): handler runs; 2xx responses are cached
//     for 24h. Non-2xx responses are NOT cached so clients can retry.
//   - Repeat with matching request hash: cached response is replayed without
//     invoking the handler.
//   - Repeat with mismatched request hash: 409 idempotency_conflict.
func Idempotency(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			user := auth.UserFromContext(r.Context())
			if user == nil {
				next.ServeHTTP(w, r)
				return
			}

			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				WriteError(w, r, http.StatusBadRequest, CodeInvalidRequest, "failed to read request body", nil)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			requestHash := model.HashRequest(user.ID, r.Method, r.URL.Path, bodyBytes)

			mu := acquireIdempotencyLock(user.ID, key)
			defer mu.Unlock()

			rec, err := model.LookupIdempotency(db, key, user.ID)
			if err != nil {
				slog.Error("idempotency: lookup failed", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			if rec != nil {
				if rec.RequestHash != requestHash {
					WriteError(w, r, http.StatusConflict, CodeIdempotencyConflict,
						"idempotency key reused with different request body", nil)
					return
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(rec.ResponseStatus)
				_, _ = w.Write(rec.ResponseBody)
				return
			}

			rw := &responseCapture{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			if rw.status >= 200 && rw.status < 300 {
				if err := model.StoreIdempotency(db, key, user.ID, requestHash, rw.status, rw.body.Bytes()); err != nil {
					slog.Error("idempotency: store failed", "error", err)
				}
			}
		})
	}
}

// responseCapture tees the wrapped handler's status and body into an
// in-memory buffer so the middleware can persist it after the handler
// returns. The bytes are still streamed to the real ResponseWriter so the
// client sees the response immediately.
type responseCapture struct {
	http.ResponseWriter
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func (rc *responseCapture) WriteHeader(status int) {
	if rc.wroteHeader {
		return
	}
	rc.status = status
	rc.wroteHeader = true
	rc.ResponseWriter.WriteHeader(status)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if !rc.wroteHeader {
		rc.wroteHeader = true
	}
	rc.body.Write(b)
	return rc.ResponseWriter.Write(b)
}
