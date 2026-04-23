// Package audit records business-meaningful mutations and security events.
// The Log call is fire-and-forget: an insert failure is logged via slog but
// never returned to the caller, so it cannot break the triggering handler.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/naufal/latasya-erp/internal/auth"
)

// Result values for the audit_log.result column.
const (
	ResultOK   = "ok"
	ResultFail = "fail"
)

// Event describes a single audited action. Action is a dotted verb like
// "invoice.create" or "auth.login". TargetLabel is a human-readable snapshot
// (e.g. invoice number, username) preserved against future renames/deletions.
type Event struct {
	Action      string
	TargetType  string
	TargetID    int64
	TargetLabel string
	Metadata    map[string]any
	Result      string // "" defaults to ResultOK
	Err         error  // if set, Result becomes ResultFail and the message is recorded

	// ActorUsername overrides the context-derived actor. Used for auth.login
	// events where the caller is pre-authentication and no user is in context.
	ActorUsername string
}

// Log records an event. Pulls actor from the context and request_id + IP from
// the audit.RequestContext middleware. Failures are logged, not returned.
func Log(ctx context.Context, db *sql.DB, e Event) {
	result := e.Result
	if result == "" {
		if e.Err != nil {
			result = ResultFail
		} else {
			result = ResultOK
		}
	}

	var errMsg *string
	if e.Err != nil {
		s := e.Err.Error()
		errMsg = &s
	}

	var actorID *int64
	var actorUsername *string
	if u := auth.UserFromContext(ctx); u != nil {
		id := int64(u.ID)
		actorID = &id
		username := u.Username
		actorUsername = &username
	}
	if e.ActorUsername != "" {
		actorUsername = &e.ActorUsername
	}

	var metadataJSON *string
	if len(e.Metadata) > 0 {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			slog.Error("audit: marshal metadata", "action", e.Action, "error", err)
		} else {
			s := string(b)
			metadataJSON = &s
		}
	}

	requestID := nullIfEmpty(RequestIDFromContext(ctx))
	ip := nullIfEmpty(ClientIPFromContext(ctx))
	targetType := nullIfEmpty(e.TargetType)
	targetLabel := nullIfEmpty(e.TargetLabel)
	var targetID *int64
	if e.TargetID != 0 {
		id := e.TargetID
		targetID = &id
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO audit_log (
			request_id, actor_id, actor_username,
			action, target_type, target_id, target_label,
			result, error_message, ip, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		requestID, actorID, actorUsername,
		e.Action, targetType, targetID, targetLabel,
		result, errMsg, ip, metadataJSON,
	)
	if err != nil {
		slog.Error("audit: insert", "action", e.Action, "error", err)
	}
}

// Diff returns {"before": {...}, "after": {...}} containing only the fields
// that actually changed between old and new. Fields not listed in the `fields`
// allow-list are ignored entirely — this is how sensitive columns (password
// hashes, tokens) are kept out of the log: you simply don't include them.
func Diff(old, new map[string]any, fields []string) map[string]any {
	before := map[string]any{}
	after := map[string]any{}
	for _, f := range fields {
		ov, oldOK := old[f]
		nv, newOK := new[f]
		if !oldOK && !newOK {
			continue
		}
		if equal(ov, nv) {
			continue
		}
		before[f] = ov
		after[f] = nv
	}
	if len(before) == 0 && len(after) == 0 {
		return nil
	}
	return map[string]any{"before": before, "after": after}
}

func equal(a, b any) bool {
	// JSON round-trip comparison keeps numeric types consistent (int vs int64)
	// and handles nested maps/slices without a deep-reflect dance.
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
