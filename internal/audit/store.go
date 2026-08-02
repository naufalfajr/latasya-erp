package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrForbidden = errors.New("audit viewing capability missing")

type Module struct{ db *sql.DB }

func New(db *sql.DB) *Module { return &Module{db: db} }

type Actor struct {
	UserID  int
	CanView bool
}

type Page struct {
	Entries []Entry
	Total   int
}

func (m *Module) List(ctx context.Context, actor Actor, f ListFilter) (*Page, error) {
	if actor.UserID <= 0 || !actor.CanView {
		return nil, ErrForbidden
	}
	entries, total, err := list(ctx, m.db, f)
	if err != nil {
		return nil, err
	}
	return &Page{Entries: entries, Total: total}, nil
}

// Entry is one audit_log row, shaped for template rendering.
type Entry struct {
	ID            int64
	OccurredAt    time.Time
	RequestID     string
	ActorID       sql.NullInt64
	ActorUsername string
	Action        string
	TargetType    string
	TargetID      sql.NullInt64
	TargetLabel   string
	Result        string
	ErrorMessage  string
	IP            string
	Metadata      string // raw JSON; template can pretty-print
}

// ListFilter narrows an audit log query. Zero-valued fields are ignored.
type ListFilter struct {
	ActorUsername string    // exact match
	ActionPrefix  string    // matches action LIKE prefix%
	From          time.Time // inclusive lower bound on occurred_at
	To            time.Time // inclusive upper bound on occurred_at
	Limit         int
	Offset        int
}

func list(ctx context.Context, db *sql.DB, f ListFilter) ([]Entry, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}

	var where []string
	var args []any

	if f.ActorUsername != "" {
		where = append(where, "actor_username = ?")
		args = append(args, f.ActorUsername)
	}
	if f.ActionPrefix != "" {
		where = append(where, "action LIKE ?")
		args = append(args, f.ActionPrefix+"%")
	}
	if !f.From.IsZero() {
		where = append(where, "occurred_at >= ?")
		args = append(args, f.From.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	if !f.To.IsZero() {
		where = append(where, "occurred_at <= ?")
		args = append(args, f.To.UTC().Format("2006-01-02T15:04:05.999Z"))
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit_log: %w", err)
	}

	query := `
		SELECT id, occurred_at, COALESCE(request_id, ''), actor_id,
		       COALESCE(actor_username, ''), action,
		       COALESCE(target_type, ''), target_id, COALESCE(target_label, ''),
		       result, COALESCE(error_message, ''), COALESCE(ip, ''),
		       COALESCE(metadata, '')
		FROM audit_log
		` + whereSQL + `
		ORDER BY occurred_at DESC, id DESC
		LIMIT ? OFFSET ?`

	args = append(args, f.Limit, f.Offset)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit_log: %w", err)
	}
	defer rows.Close()

	entries := []Entry{}
	for rows.Next() {
		var e Entry
		var occurredAt string
		if err := rows.Scan(
			&e.ID, &occurredAt, &e.RequestID, &e.ActorID,
			&e.ActorUsername, &e.Action,
			&e.TargetType, &e.TargetID, &e.TargetLabel,
			&e.Result, &e.ErrorMessage, &e.IP,
			&e.Metadata,
		); err != nil {
			return nil, 0, fmt.Errorf("scan audit_log: %w", err)
		}
		// Times are stored as ISO-8601 UTC with milliseconds.
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", occurredAt); err == nil {
			e.OccurredAt = t
		} else if t, err := time.Parse(time.RFC3339, occurredAt); err == nil {
			e.OccurredAt = t
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit_log: %w", err)
	}
	return entries, total, nil
}
