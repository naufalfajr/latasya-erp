package model_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestSchoolClosureCRUDAndList(t *testing.T) {
	db := testutil.SetupTestDB(t)

	closure := &model.SchoolClosure{
		Source:    model.SchoolClosureSourceManual,
		Title:     "Midterm break",
		StartDate: "2026-06-10",
		EndDate:   "2026-06-12",
	}
	id, err := model.CreateSchoolClosure(db, closure)
	if err != nil {
		t.Fatalf("create school closure: %v", err)
	}
	if id == 0 {
		t.Fatal("expected school closure ID")
	}

	created, err := model.GetSchoolClosure(db, id)
	if err != nil {
		t.Fatalf("get school closure: %v", err)
	}
	if created.GoogleEventID != "" {
		t.Errorf("manual google event id: got %q want empty", created.GoogleEventID)
	}

	closures, err := model.ListSchoolClosures(db, "2026-06")
	if err != nil {
		t.Fatalf("list school closures: %v", err)
	}
	if len(closures) != 1 {
		t.Fatalf("closures in June: got %d want 1", len(closures))
	}

	closure.Title = "Updated break"
	closure.StartDate = "2026-07-01"
	closure.EndDate = "2026-07-02"
	if err := model.UpdateSchoolClosure(db, closure); err != nil {
		t.Fatalf("update school closure: %v", err)
	}
	updated, err := model.GetSchoolClosure(db, id)
	if err != nil {
		t.Fatalf("get updated school closure: %v", err)
	}
	if updated.Title != "Updated break" || updated.StartDate != "2026-07-01" {
		t.Fatalf("updated closure = %+v", updated)
	}

	closures, err = model.ListSchoolClosures(db, "2026-06")
	if err != nil {
		t.Fatalf("list school closures after update: %v", err)
	}
	if len(closures) != 0 {
		t.Fatalf("closures in June after update: got %d want 0", len(closures))
	}

	if err := model.DeleteSchoolClosure(db, id); err != nil {
		t.Fatalf("delete school closure: %v", err)
	}
	closures, err = model.ListSchoolClosures(db, "")
	if err != nil {
		t.Fatalf("list all school closures: %v", err)
	}
	if len(closures) != 0 {
		t.Fatalf("closures after delete: got %d want 0", len(closures))
	}
}

func TestEffectiveSchoolDaysCountsMondaySaturday(t *testing.T) {
	db := testutil.SetupTestDB(t)

	days, err := model.EffectiveSchoolDays(db, "2026-06")
	if err != nil {
		t.Fatalf("effective school days: %v", err)
	}
	if days != 26 {
		t.Fatalf("effective school days: got %d want 26", days)
	}
}

func TestEffectiveSchoolDaysIgnoresSundayClosures(t *testing.T) {
	db := testutil.SetupTestDB(t)
	createSchoolClosure(t, db, "Sunday event", "2026-06-07", "2026-06-07")

	days, err := model.EffectiveSchoolDays(db, "2026-06")
	if err != nil {
		t.Fatalf("effective school days: %v", err)
	}
	if days != 26 {
		t.Fatalf("effective school days: got %d want 26", days)
	}
}

func TestEffectiveSchoolDaysDedupesOverlapsAndBoundsMonth(t *testing.T) {
	db := testutil.SetupTestDB(t)
	createSchoolClosure(t, db, "Previous month boundary", "2026-05-30", "2026-06-02")
	createSchoolClosure(t, db, "Overlap A", "2026-06-01", "2026-06-03")
	createSchoolClosure(t, db, "Overlap B", "2026-06-03", "2026-06-05")
	createSchoolClosure(t, db, "Next month boundary", "2026-06-29", "2026-07-03")

	days, err := model.EffectiveSchoolDays(db, "2026-06")
	if err != nil {
		t.Fatalf("effective school days: %v", err)
	}
	if days != 19 {
		t.Fatalf("effective school days: got %d want 19", days)
	}
}

func TestMonthlyPriceMultiplierPercent(t *testing.T) {
	tests := []struct {
		days int
		want int
	}{
		{days: 13, want: 75},
		{days: 14, want: 85},
		{days: 19, want: 85},
		{days: 20, want: 100},
	}

	for _, tt := range tests {
		got := model.MonthlyPriceMultiplierPercent(tt.days)
		if got != tt.want {
			t.Errorf("MonthlyPriceMultiplierPercent(%d): got %d want %d", tt.days, got, tt.want)
		}
	}
}

func TestApplyMonthlyPriceMultiplier(t *testing.T) {
	if got := model.ApplyMonthlyPriceMultiplier(400000, 85); got != 340000 {
		t.Fatalf("85 percent multiplier: got %d want 340000", got)
	}
	if got := model.ApplyMonthlyPriceMultiplier(333333, 75); got != 249999 {
		t.Fatalf("75 percent integer multiplier: got %d want 249999", got)
	}
}

func TestGoogleOAuthStateConsumeRejectsExpiredMismatchedAndIsSingleUse(t *testing.T) {
	db := testutil.SetupTestDB(t)

	if err := model.CreateGoogleOAuthState(db, "valid", 1, "verifier", time.Now().UTC().Add(time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("create valid oauth state: %v", err)
	}
	if err := model.CreateGoogleOAuthState(db, "expired", 1, "expired-verifier", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("create expired oauth state: %v", err)
	}

	if _, err := model.ConsumeGoogleOAuthState(db, "expired", 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired consume error: got %v want sql.ErrNoRows", err)
	}
	if _, err := model.ConsumeGoogleOAuthState(db, "valid", 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("mismatched user consume error: got %v want sql.ErrNoRows", err)
	}

	state, err := model.ConsumeGoogleOAuthState(db, "valid", 1)
	if err != nil {
		t.Fatalf("consume valid oauth state: %v", err)
	}
	if state.PKCEVerifier != "verifier" || state.UserID != 1 {
		t.Fatalf("consumed state = %+v", state)
	}
	if _, err := model.ConsumeGoogleOAuthState(db, "valid", 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second consume error: got %v want sql.ErrNoRows", err)
	}
}

func TestReplaceGoogleSchoolClosuresDeletesOnlyOverlappingGoogleRows(t *testing.T) {
	db := testutil.SetupTestDB(t)
	createSchoolClosureWithSource(t, db, model.SchoolClosureSourceManual, "Manual overlap", "2026-06-05", "2026-06-06", "")
	createSchoolClosureWithSource(t, db, model.SchoolClosureSourceGoogle, "Google overlap", "2026-06-02", "2026-06-03", "g-overlap")
	createSchoolClosureWithSource(t, db, model.SchoolClosureSourceGoogle, "Google before", "2026-05-01", "2026-05-02", "g-before")
	createSchoolClosureWithSource(t, db, model.SchoolClosureSourceGoogle, "Google after", "2026-07-10", "2026-07-11", "g-after")

	err := model.ReplaceGoogleSchoolClosures(db, []model.SchoolClosure{
		{Title: "Synced break", StartDate: "2026-06-10", EndDate: "2026-06-12", GoogleEventID: "g-new"},
	}, "2026-06-01", "2026-06-30")
	if err != nil {
		t.Fatalf("replace google school closures: %v", err)
	}

	closures, err := model.ListSchoolClosures(db, "")
	if err != nil {
		t.Fatalf("list closures: %v", err)
	}
	got := map[string]model.SchoolClosure{}
	for _, closure := range closures {
		got[closure.Title] = closure
	}
	for _, title := range []string{"Manual overlap", "Google before", "Google after", "Synced break"} {
		if _, ok := got[title]; !ok {
			t.Fatalf("%q missing after replacement; got %+v", title, closures)
		}
	}
	if _, ok := got["Google overlap"]; ok {
		t.Fatalf("overlapping google closure was preserved: %+v", closures)
	}
	if got["Manual overlap"].Source != model.SchoolClosureSourceManual {
		t.Fatalf("manual closure source changed: %+v", got["Manual overlap"])
	}
}

func TestGoogleCalendarConnectionSaveGetAndDelete(t *testing.T) {
	db := testutil.SetupTestDB(t)

	conn, err := model.GetGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get empty connection: %v", err)
	}
	if conn.ID != 1 || conn.IsActive || conn.RefreshToken != "" {
		t.Fatalf("empty connection = %+v", conn)
	}

	if err := model.SaveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{CalendarID: "primary", RefreshToken: "refresh-1", IsActive: true}); err != nil {
		t.Fatalf("save connection: %v", err)
	}
	if err := model.SaveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{CalendarID: "school", RefreshToken: "refresh-2", IsActive: true, LastSyncStatus: "success"}); err != nil {
		t.Fatalf("update connection: %v", err)
	}
	conn, err = model.GetGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get saved connection: %v", err)
	}
	if conn.CalendarID != "school" || conn.RefreshToken != "refresh-2" || !conn.IsActive {
		t.Fatalf("saved connection = %+v", conn)
	}

	createSchoolClosureWithSource(t, db, model.SchoolClosureSourceGoogle, "Google row", "2026-06-01", "2026-06-01", "delete-me")
	createSchoolClosureWithSource(t, db, model.SchoolClosureSourceManual, "Manual row", "2026-06-01", "2026-06-01", "")
	if err := model.DeleteGoogleCalendarConnection(db); err != nil {
		t.Fatalf("delete connection: %v", err)
	}
	closures, err := model.ListSchoolClosures(db, "")
	if err != nil {
		t.Fatalf("list closures after delete: %v", err)
	}
	if len(closures) != 1 || closures[0].Title != "Manual row" {
		t.Fatalf("closures after delete = %+v", closures)
	}
}

func createSchoolClosure(t *testing.T, db *sql.DB, title, startDate, endDate string) {
	t.Helper()
	createSchoolClosureWithSource(t, db, model.SchoolClosureSourceManual, title, startDate, endDate, "")
}

func createSchoolClosureWithSource(t *testing.T, db *sql.DB, source, title, startDate, endDate, googleEventID string) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO school_closures (source, title, start_date, end_date, google_event_id) VALUES (?, ?, ?, ?, ?)",
		source, title, startDate, endDate, nullTestString(googleEventID),
	)
	if err != nil {
		t.Fatalf("seed school closure %q: %v", title, err)
	}
}

func nullTestString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
