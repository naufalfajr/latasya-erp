package schoolcalendar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

var (
	ErrForbidden = errors.New("school calendar management capability missing")
	ErrNotFound  = errors.New("school calendar record not found")
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "validation failed" }

type Actor struct {
	UserID    int
	Username  string
	CanManage bool
	IsAdmin   bool
}
type ClosureDraft struct{ Title, StartDate, EndDate string }
type Module struct{ db *sql.DB }

func New(db *sql.DB) *Module { return &Module{db: db} }

func requireManage(actor Actor) error {
	if actor.UserID <= 0 || !actor.CanManage {
		return ErrForbidden
	}
	return nil
}
func requireAdmin(actor Actor) error {
	if actor.UserID <= 0 || !actor.IsAdmin {
		return ErrForbidden
	}
	return nil
}

func (m *Module) CreateManual(ctx context.Context, actor Actor, draft ClosureDraft) (*model.SchoolClosure, error) {
	if err := requireManage(actor); err != nil {
		return nil, err
	}
	draft.Title, draft.StartDate, draft.EndDate = strings.TrimSpace(draft.Title), strings.TrimSpace(draft.StartDate), strings.TrimSpace(draft.EndDate)
	if fields := validateClosure(draft); len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin school closure create: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO school_closures (source,title,start_date,end_date) VALUES (?,?,?,?)`, model.SchoolClosureSourceManual, draft.Title, draft.StartDate, draft.EndDate)
	if err != nil {
		return nil, fmt.Errorf("create school closure: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("school closure id: %w", err)
	}
	created, err := getClosureWith(ctx, tx, int(id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit school closure create: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "school_closure.create", ActorID: int64(actor.UserID), ActorUsername: actor.Username, TargetType: "school_closure", TargetID: id, TargetLabel: created.Title, Metadata: map[string]any{"after": closureSnapshot(created)}})
	return created, nil
}

func (m *Module) Get(ctx context.Context, id int) (*model.SchoolClosure, error) {
	return getClosureWith(ctx, m.db, id)
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getClosureWith(ctx context.Context, queryer rowQueryer, id int) (*model.SchoolClosure, error) {
	var closure model.SchoolClosure
	err := queryer.QueryRowContext(ctx, `SELECT id,source,title,start_date,end_date,COALESCE(google_event_id,''),created_at,updated_at FROM school_closures WHERE id=?`, id).Scan(&closure.ID, &closure.Source, &closure.Title, &closure.StartDate, &closure.EndDate, &closure.GoogleEventID, &closure.CreatedAt, &closure.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get school closure: %w", err)
	}
	return &closure, nil
}

func (m *Module) List(ctx context.Context, month string) ([]model.SchoolClosure, error) {
	query := `SELECT id,source,title,start_date,end_date,COALESCE(google_event_id,''),created_at,updated_at FROM school_closures`
	args := []any{}
	if month != "" {
		start, end, err := monthBounds(month)
		if err != nil {
			return nil, err
		}
		query += ` WHERE start_date<=? AND end_date>=?`
		args = append(args, end.Format(time.DateOnly), start.Format(time.DateOnly))
	}
	query += ` ORDER BY start_date,id`
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list school closures: %w", err)
	}
	defer rows.Close()
	closures := []model.SchoolClosure{}
	for rows.Next() {
		var c model.SchoolClosure
		if err := rows.Scan(&c.ID, &c.Source, &c.Title, &c.StartDate, &c.EndDate, &c.GoogleEventID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan school closure: %w", err)
		}
		closures = append(closures, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate school closures: %w", err)
	}
	return closures, nil
}

func (m *Module) Delete(ctx context.Context, actor Actor, id int) (*model.SchoolClosure, error) {
	if err := requireManage(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin school closure delete: %w", err)
	}
	defer tx.Rollback()
	var c model.SchoolClosure
	err = tx.QueryRowContext(ctx, `SELECT id,source,title,start_date,end_date,COALESCE(google_event_id,''),created_at,updated_at FROM school_closures WHERE id=?`, id).Scan(&c.ID, &c.Source, &c.Title, &c.StartDate, &c.EndDate, &c.GoogleEventID, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get school closure: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM school_closures WHERE id=?`, id); err != nil {
		return nil, fmt.Errorf("delete school closure: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit school closure delete: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "school_closure.delete", ActorID: int64(actor.UserID), ActorUsername: actor.Username, TargetType: "school_closure", TargetID: int64(c.ID), TargetLabel: c.Title, Metadata: map[string]any{"before": closureSnapshot(&c)}})
	return &c, nil
}

func (m *Module) EffectiveDays(ctx context.Context, month string) (int, error) {
	start, end, err := monthBounds(month)
	if err != nil {
		return 0, err
	}
	days := map[string]bool{}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Sunday {
			days[d.Format(time.DateOnly)] = true
		}
	}
	rows, err := m.db.QueryContext(ctx, `SELECT start_date,end_date FROM school_closures WHERE start_date<=? AND end_date>=?`, end.Format(time.DateOnly), start.Format(time.DateOnly))
	if err != nil {
		return 0, fmt.Errorf("list school closure dates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return 0, fmt.Errorf("scan school closure dates: %w", err)
		}
		from, e1 := time.Parse(time.DateOnly, a)
		to, e2 := time.Parse(time.DateOnly, b)
		if e1 != nil || e2 != nil {
			return 0, fmt.Errorf("invalid stored school closure date")
		}
		if from.Before(start) {
			from = start
		}
		if to.After(end) {
			to = end
		}
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			delete(days, d.Format(time.DateOnly))
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate school closure dates: %w", err)
	}
	return len(days), nil
}

func MultiplierPercent(days int) int {
	if days < 14 {
		return 75
	}
	if days < 20 {
		return 85
	}
	return 100
}
func ApplyMultiplier(base, percent int) int { return base * percent / 100 }

func (m *Module) Connection(ctx context.Context) (*model.GoogleCalendarConnection, error) {
	c := &model.GoogleCalendarConnection{ID: 1}
	var active int
	err := m.db.QueryRowContext(ctx, `SELECT id,calendar_id,refresh_token,is_active,COALESCE(last_sync_at,''),last_sync_status,last_sync_error,created_at,updated_at FROM google_calendar_connections WHERE id=1`).Scan(&c.ID, &c.CalendarID, &c.RefreshToken, &active, &c.LastSyncAt, &c.LastSyncStatus, &c.LastSyncError, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get google calendar connection: %w", err)
	}
	c.IsActive = active != 0
	return c, nil
}

func (m *Module) SaveCalendarID(ctx context.Context, actor Actor, calendarID string) (*model.GoogleCalendarConnection, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	calendarID = strings.TrimSpace(calendarID)
	if _, err := m.db.ExecContext(ctx, `INSERT INTO google_calendar_connections (id,calendar_id) VALUES (1,?) ON CONFLICT(id) DO UPDATE SET calendar_id=excluded.calendar_id,updated_at=datetime('now')`, calendarID); err != nil {
		return nil, fmt.Errorf("save google calendar id: %w", err)
	}
	c, err := m.Connection(ctx)
	if err != nil {
		return nil, err
	}
	audit.Log(ctx, m.db, audit.Event{Action: "google_calendar.calendar_id.update", ActorID: int64(actor.UserID), ActorUsername: actor.Username, TargetType: "google_calendar_connection", TargetID: 1, Metadata: map[string]any{"calendar_id_set": c.CalendarID != ""}})
	return c, nil
}

func (m *Module) CreateOAuthState(ctx context.Context, actor Actor, state, verifier string, expiresAt time.Time) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	_, err := m.db.ExecContext(ctx, `INSERT INTO google_oauth_states (state,user_id,pkce_verifier,expires_at) VALUES (?,?,?,?)`, state, actor.UserID, verifier, expiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create google oauth state: %w", err)
	}
	return nil
}
func (m *Module) ConsumeOAuthState(ctx context.Context, actor Actor, state string) (*model.GoogleOAuthState, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin oauth state consume: %w", err)
	}
	defer tx.Rollback()
	s := &model.GoogleOAuthState{}
	err = tx.QueryRowContext(ctx, `SELECT state,user_id,pkce_verifier,expires_at,created_at FROM google_oauth_states WHERE state=? AND user_id=? AND datetime(expires_at)>datetime('now')`, state, actor.UserID).Scan(&s.State, &s.UserID, &s.PKCEVerifier, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM google_oauth_states WHERE state=?`, state); err != nil {
		return nil, fmt.Errorf("delete oauth state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit oauth state consume: %w", err)
	}
	return s, nil
}

func (m *Module) Connect(ctx context.Context, actor Actor, refreshToken string) (*model.GoogleCalendarConnection, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	if refreshToken == "" {
		return nil, &ValidationError{Fields: map[string]string{"refresh_token": "required"}}
	}
	if _, err := m.db.ExecContext(ctx, `INSERT INTO google_calendar_connections (id,refresh_token,is_active) VALUES (1,?,1) ON CONFLICT(id) DO UPDATE SET refresh_token=excluded.refresh_token,is_active=1,last_sync_status='',last_sync_error='',updated_at=datetime('now')`, refreshToken); err != nil {
		return nil, fmt.Errorf("connect google calendar: %w", err)
	}
	c, err := m.Connection(ctx)
	if err != nil {
		return nil, err
	}
	audit.Log(ctx, m.db, audit.Event{Action: "google_calendar.connect", ActorID: int64(actor.UserID), ActorUsername: actor.Username, TargetType: "google_calendar_connection", TargetID: 1, Metadata: map[string]any{"calendar_id_set": c.CalendarID != ""}})
	return c, nil
}
func (m *Module) Disconnect(ctx context.Context, actor Actor) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin google disconnect: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM google_calendar_connections WHERE id=1`); err != nil {
		return fmt.Errorf("delete google connection: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM school_closures WHERE source=?`, model.SchoolClosureSourceGoogle); err != nil {
		return fmt.Errorf("delete google closures: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit google disconnect: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "google_calendar.disconnect", ActorID: int64(actor.UserID), ActorUsername: actor.Username, TargetType: "google_calendar_connection", TargetID: 1})
	return nil
}

// ReplaceGoogleClosures and UpdateSyncStatus are trusted calls for the Google adapter.
func (m *Module) ReplaceGoogleClosures(ctx context.Context, closures []model.SchoolClosure, windowStart, windowEnd string) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin google closure replace: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM school_closures WHERE source=? AND start_date<=? AND end_date>=?`, model.SchoolClosureSourceGoogle, windowEnd, windowStart); err != nil {
		return fmt.Errorf("delete google closures: %w", err)
	}
	for _, c := range closures {
		if _, err := tx.ExecContext(ctx, `INSERT INTO school_closures (source,title,start_date,end_date,google_event_id) VALUES (?,?,?,?,?)`, model.SchoolClosureSourceGoogle, c.Title, c.StartDate, c.EndDate, nullIfEmpty(c.GoogleEventID)); err != nil {
			return fmt.Errorf("insert google closure: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit google closure replace: %w", err)
	}
	return nil
}
func (m *Module) UpdateSyncStatus(ctx context.Context, status, syncError string) error {
	var at any
	if status == "success" {
		at = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := m.db.ExecContext(ctx, `UPDATE google_calendar_connections SET last_sync_at=COALESCE(?,last_sync_at),last_sync_status=?,last_sync_error=?,updated_at=datetime('now') WHERE id=1`, at, status, syncError)
	if err != nil {
		return fmt.Errorf("update google sync status: %w", err)
	}
	return nil
}

func (m *Module) RecordSync(ctx context.Context, actor Actor, fetched, stored int, windowStart, windowEnd string) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	audit.Log(ctx, m.db, audit.Event{Action: "google_calendar.sync", ActorID: int64(actor.UserID), ActorUsername: actor.Username, TargetType: "google_calendar_connection", TargetID: 1, Metadata: map[string]any{"fetched": fetched, "stored": stored, "window_start": windowStart, "window_end": windowEnd}})
	return nil
}

func validateClosure(d ClosureDraft) map[string]string {
	fields := map[string]string{}
	if d.Title == "" {
		fields["title"] = "required"
	}
	start, e1 := time.Parse(time.DateOnly, d.StartDate)
	if d.StartDate == "" || e1 != nil {
		fields["start_date"] = "must be YYYY-MM-DD"
	}
	end, e2 := time.Parse(time.DateOnly, d.EndDate)
	if d.EndDate == "" || e2 != nil {
		fields["end_date"] = "must be YYYY-MM-DD"
	}
	if e1 == nil && e2 == nil && start.After(end) {
		fields["end_date"] = "must be on or after start_date"
	}
	return fields
}
func monthBounds(month string) (time.Time, time.Time, error) {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid school month %q", month)
	}
	return start, start.AddDate(0, 1, -1), nil
}
func closureSnapshot(c *model.SchoolClosure) map[string]any {
	return map[string]any{"source": c.Source, "title": c.Title, "start_date": c.StartDate, "end_date": c.EndDate}
}
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
