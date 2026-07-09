package model

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	SchoolClosureSourceManual = "manual"
	SchoolClosureSourceGoogle = "google"
)

const schoolDateLayout = "2006-01-02"

type SchoolClosure struct {
	ID            int    `json:"id"`
	Source        string `json:"source"`
	Title         string `json:"title"`
	StartDate     string `json:"start_date"`
	EndDate       string `json:"end_date"`
	GoogleEventID string `json:"google_event_id,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type GoogleCalendarConnection struct {
	ID             int    `json:"id"`
	CalendarID     string `json:"calendar_id"`
	RefreshToken   string `json:"-"`
	IsActive       bool   `json:"is_active"`
	LastSyncAt     string `json:"last_sync_at"`
	LastSyncStatus string `json:"last_sync_status"`
	LastSyncError  string `json:"last_sync_error"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type GoogleOAuthState struct {
	State        string `json:"state"`
	UserID       int    `json:"user_id"`
	PKCEVerifier string `json:"pkce_verifier"`
	ExpiresAt    string `json:"expires_at"`
	CreatedAt    string `json:"created_at"`
}

func CreateSchoolClosure(db *sql.DB, c *SchoolClosure) (int, error) {
	result, err := db.Exec(
		`INSERT INTO school_closures (source, title, start_date, end_date, google_event_id)
		 VALUES (?, ?, ?, ?, ?)`,
		c.Source, c.Title, c.StartDate, c.EndDate, nullString(c.GoogleEventID),
	)
	if err != nil {
		return 0, fmt.Errorf("create school closure: %w", err)
	}
	id, _ := result.LastInsertId()
	c.ID = int(id)
	return c.ID, nil
}

func GetSchoolClosure(db *sql.DB, id int) (*SchoolClosure, error) {
	c := &SchoolClosure{}
	err := db.QueryRow(
		`SELECT id, source, title, start_date, end_date, COALESCE(google_event_id, ''), created_at, updated_at
		 FROM school_closures WHERE id = ?`, id,
	).Scan(&c.ID, &c.Source, &c.Title, &c.StartDate, &c.EndDate, &c.GoogleEventID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get school closure: %w", err)
	}
	return c, nil
}

func ListSchoolClosures(db *sql.DB, month string) ([]SchoolClosure, error) {
	query := `SELECT id, source, title, start_date, end_date, COALESCE(google_event_id, ''), created_at, updated_at
		FROM school_closures`
	var args []any
	if month != "" {
		start, end, err := schoolMonthBounds(month)
		if err != nil {
			return nil, err
		}
		query += " WHERE start_date <= ? AND end_date >= ?"
		args = append(args, end.Format(schoolDateLayout), start.Format(schoolDateLayout))
	}
	query += " ORDER BY start_date, id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list school closures: %w", err)
	}
	defer rows.Close()

	var closures []SchoolClosure
	for rows.Next() {
		var c SchoolClosure
		if err := rows.Scan(&c.ID, &c.Source, &c.Title, &c.StartDate, &c.EndDate, &c.GoogleEventID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan school closure: %w", err)
		}
		closures = append(closures, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list school closures rows: %w", err)
	}
	return closures, nil
}

func UpdateSchoolClosure(db *sql.DB, c *SchoolClosure) error {
	_, err := db.Exec(
		`UPDATE school_closures
		 SET source = ?, title = ?, start_date = ?, end_date = ?, google_event_id = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		c.Source, c.Title, c.StartDate, c.EndDate, nullString(c.GoogleEventID), c.ID,
	)
	if err != nil {
		return fmt.Errorf("update school closure: %w", err)
	}
	return nil
}

func DeleteSchoolClosure(db *sql.DB, id int) error {
	_, err := db.Exec("DELETE FROM school_closures WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete school closure: %w", err)
	}
	return nil
}

func GetGoogleCalendarConnection(db *sql.DB) (*GoogleCalendarConnection, error) {
	c := &GoogleCalendarConnection{ID: 1}
	var isActive int
	err := db.QueryRow(
		`SELECT id, calendar_id, refresh_token, is_active, COALESCE(last_sync_at, ''),
			last_sync_status, last_sync_error, created_at, updated_at
		 FROM google_calendar_connections WHERE id = 1`,
	).Scan(&c.ID, &c.CalendarID, &c.RefreshToken, &isActive, &c.LastSyncAt, &c.LastSyncStatus, &c.LastSyncError, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get google calendar connection: %w", err)
	}
	c.IsActive = isActive != 0
	return c, nil
}

func SaveGoogleCalendarConnection(db *sql.DB, c *GoogleCalendarConnection) error {
	_, err := db.Exec(
		`INSERT INTO google_calendar_connections
			(id, calendar_id, refresh_token, is_active, last_sync_at, last_sync_status, last_sync_error, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(id) DO UPDATE SET
			calendar_id = excluded.calendar_id,
			refresh_token = excluded.refresh_token,
			is_active = excluded.is_active,
			last_sync_at = excluded.last_sync_at,
			last_sync_status = excluded.last_sync_status,
			last_sync_error = excluded.last_sync_error,
			updated_at = datetime('now')`,
		c.CalendarID, c.RefreshToken, boolInt(c.IsActive), nullString(c.LastSyncAt), c.LastSyncStatus, c.LastSyncError,
	)
	if err != nil {
		return fmt.Errorf("save google calendar connection: %w", err)
	}
	return nil
}

func DeleteGoogleCalendarConnection(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM google_calendar_connections WHERE id = 1`); err != nil {
		return fmt.Errorf("delete google calendar connection: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM school_closures WHERE source = ?`, SchoolClosureSourceGoogle); err != nil {
		return fmt.Errorf("delete google school closures: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete google calendar connection: %w", err)
	}
	return nil
}

func CreateGoogleOAuthState(db *sql.DB, state string, userID int, pkceVerifier, expiresAt string) error {
	_, err := db.Exec(
		`INSERT INTO google_oauth_states (state, user_id, pkce_verifier, expires_at)
		 VALUES (?, ?, ?, ?)`,
		state, userID, pkceVerifier, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("create google oauth state: %w", err)
	}
	return nil
}

func ConsumeGoogleOAuthState(db *sql.DB, state string, userID int) (*GoogleOAuthState, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	s := &GoogleOAuthState{}
	err = tx.QueryRow(
		`SELECT state, user_id, pkce_verifier, expires_at, created_at
		 FROM google_oauth_states
		 WHERE state = ? AND user_id = ? AND datetime(expires_at) > datetime('now')`,
		state, userID,
	).Scan(&s.State, &s.UserID, &s.PKCEVerifier, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("consume google oauth state: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM google_oauth_states WHERE state = ?`, state); err != nil {
		return nil, fmt.Errorf("delete google oauth state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit google oauth state: %w", err)
	}
	return s, nil
}

func ReplaceGoogleSchoolClosures(db *sql.DB, closures []SchoolClosure, windowStart, windowEnd string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM school_closures
		 WHERE source = ? AND start_date <= ? AND end_date >= ?`,
		SchoolClosureSourceGoogle, windowEnd, windowStart,
	); err != nil {
		return fmt.Errorf("delete google school closures: %w", err)
	}
	for _, c := range closures {
		if _, err := tx.Exec(
			`INSERT INTO school_closures (source, title, start_date, end_date, google_event_id)
			 VALUES (?, ?, ?, ?, ?)`,
			SchoolClosureSourceGoogle, c.Title, c.StartDate, c.EndDate, nullString(c.GoogleEventID),
		); err != nil {
			return fmt.Errorf("insert google school closure: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace google school closures: %w", err)
	}
	return nil
}

func UpdateGoogleCalendarSyncStatus(db *sql.DB, status, syncError string) error {
	lastSyncAt := any(nil)
	if status == "success" {
		lastSyncAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := db.Exec(
		`UPDATE google_calendar_connections
		 SET last_sync_at = COALESCE(?, last_sync_at), last_sync_status = ?, last_sync_error = ?, updated_at = datetime('now')
		 WHERE id = 1`,
		lastSyncAt, status, syncError,
	)
	if err != nil {
		return fmt.Errorf("update google calendar sync status: %w", err)
	}
	return nil
}

func EffectiveSchoolDays(db *sql.DB, month string) (int, error) {
	start, end, err := schoolMonthBounds(month)
	if err != nil {
		return 0, err
	}

	schoolDays := map[string]bool{}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		if day.Weekday() != time.Sunday {
			schoolDays[day.Format(schoolDateLayout)] = true
		}
	}

	rows, err := db.Query(
		`SELECT start_date, end_date FROM school_closures
		 WHERE start_date <= ? AND end_date >= ?`,
		end.Format(schoolDateLayout), start.Format(schoolDateLayout),
	)
	if err != nil {
		return 0, fmt.Errorf("list school closure dates: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var startDate, endDate string
		if err := rows.Scan(&startDate, &endDate); err != nil {
			return 0, fmt.Errorf("scan school closure date: %w", err)
		}
		closureStart, err := time.Parse(schoolDateLayout, startDate)
		if err != nil {
			return 0, fmt.Errorf("parse school closure start date %q: %w", startDate, err)
		}
		closureEnd, err := time.Parse(schoolDateLayout, endDate)
		if err != nil {
			return 0, fmt.Errorf("parse school closure end date %q: %w", endDate, err)
		}
		if closureStart.Before(start) {
			closureStart = start
		}
		if closureEnd.After(end) {
			closureEnd = end
		}
		for day := closureStart; !day.After(closureEnd); day = day.AddDate(0, 0, 1) {
			delete(schoolDays, day.Format(schoolDateLayout))
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("list school closure date rows: %w", err)
	}
	return len(schoolDays), nil
}

func MonthlyPriceMultiplierPercent(days int) int {
	if days < 14 {
		return 75
	}
	if days < 20 {
		return 85
	}
	return 100
}

func ApplyMonthlyPriceMultiplier(base, percent int) int {
	return base * percent / 100
}

func schoolMonthBounds(month string) (time.Time, time.Time, error) {
	if len(month) != len("2006-01") {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid school month: %q", month)
	}
	start, err := time.Parse(schoolDateLayout, month+"-01")
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid school month %q: %w", month, err)
	}
	end := start.AddDate(0, 1, -1)
	return start, end, nil
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
