package schoolcalendar_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/schoolcalendar"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestClosuresAndEffectiveDays(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := schoolcalendar.New(db)
	ctx := context.Background()
	actor := schoolcalendar.Actor{UserID: 1, CanManage: true}
	closure, err := module.CreateManual(ctx, actor, schoolcalendar.ClosureDraft{Title: "Break", StartDate: "2026-06-08", EndDate: "2026-06-10"})
	if err != nil {
		t.Fatal(err)
	}
	days, err := module.EffectiveDays(ctx, "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if days != 23 {
		t.Fatalf("days=%d want 23", days)
	}
	if _, err := module.Delete(ctx, actor, closure.ID); err != nil {
		t.Fatal(err)
	}
}

func TestOAuthStateIsUserBoundAndSingleUse(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := schoolcalendar.New(db)
	ctx := context.Background()
	actor := schoolcalendar.Actor{UserID: 1, IsAdmin: true}
	if err := module.CreateOAuthState(ctx, actor, "state", "verifier", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := module.ConsumeOAuthState(ctx, schoolcalendar.Actor{UserID: 2, IsAdmin: true}, "state"); !errors.Is(err, schoolcalendar.ErrNotFound) {
		t.Fatalf("mismatch error=%v", err)
	}
	if _, err := module.ConsumeOAuthState(ctx, actor, "state"); err != nil {
		t.Fatal(err)
	}
	if _, err := module.ConsumeOAuthState(ctx, actor, "state"); !errors.Is(err, schoolcalendar.ErrNotFound) {
		t.Fatalf("reuse error=%v", err)
	}
}

func TestEffectiveDaysDeduplicatesOverlappingClosures(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := schoolcalendar.New(db)
	ctx := context.Background()
	actor := schoolcalendar.Actor{UserID: 1, CanManage: true}
	for _, draft := range []schoolcalendar.ClosureDraft{
		{Title: "First", StartDate: "2026-08-03", EndDate: "2026-08-05"},
		{Title: "Second", StartDate: "2026-08-05", EndDate: "2026-08-07"},
	} {
		if _, err := module.CreateManual(ctx, actor, draft); err != nil {
			t.Fatal(err)
		}
	}
	days, err := module.EffectiveDays(ctx, "2026-08")
	if err != nil || days != 21 {
		t.Fatalf("days=%d err=%v", days, err)
	}
	if _, err := module.List(ctx, "2026-13"); err == nil {
		t.Fatal("invalid month should fail")
	}
}

func TestPricingMultipliers(t *testing.T) {
	for _, tc := range []struct{ days, percent int }{{13, 75}, {14, 85}, {19, 85}, {20, 100}} {
		if got := schoolcalendar.MultiplierPercent(tc.days); got != tc.percent {
			t.Fatalf("days=%d percent=%d want=%d", tc.days, got, tc.percent)
		}
	}
	if got := schoolcalendar.ApplyMultiplier(10_000, 85); got != 8_500 {
		t.Fatalf("amount=%d", got)
	}
}

func TestGoogleConnectionUpdatesDoNotOverwriteUnrelatedFields(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := schoolcalendar.New(db)
	ctx := context.Background()
	actor := schoolcalendar.Actor{UserID: 1, Username: "admin", IsAdmin: true}

	if _, err := module.Connect(ctx, actor, "refresh-one"); err != nil {
		t.Fatal(err)
	}
	if err := module.UpdateSyncStatus(ctx, "failed", "temporary"); err != nil {
		t.Fatal(err)
	}
	connection, err := module.SaveCalendarID(ctx, actor, "school@example.com")
	if err != nil || connection.LastSyncStatus != "failed" || connection.LastSyncError != "temporary" || connection.RefreshToken != "refresh-one" {
		t.Fatalf("connection=%+v err=%v", connection, err)
	}
	connection, err = module.Connect(ctx, actor, "refresh-two")
	if err != nil || connection.CalendarID != "school@example.com" || connection.RefreshToken != "refresh-two" {
		t.Fatalf("connection=%+v err=%v", connection, err)
	}
	if err := module.UpdateSyncStatus(ctx, "success", ""); err != nil {
		t.Fatal(err)
	}
	connection, err = module.Connection(ctx)
	if err != nil || connection.CalendarID != "school@example.com" || connection.RefreshToken != "refresh-two" || connection.LastSyncStatus != "success" {
		t.Fatalf("connection=%+v err=%v", connection, err)
	}
}

func TestReplaceAndDisconnectGoogleClosuresPreserveManualData(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := schoolcalendar.New(db)
	ctx := context.Background()
	manage := schoolcalendar.Actor{UserID: 1, CanManage: true}
	admin := schoolcalendar.Actor{UserID: 1, IsAdmin: true}
	if _, err := module.CreateManual(ctx, manage, schoolcalendar.ClosureDraft{Title: "Manual", StartDate: "2026-08-03", EndDate: "2026-08-03"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO school_closures (source,title,start_date,end_date,google_event_id) VALUES ('google','Old','2026-08-04','2026-08-04','old'),('google','Outside','2026-09-04','2026-09-04','outside')`); err != nil {
		t.Fatal(err)
	}
	closures := []model.SchoolClosure{{Title: "New", StartDate: "2026-08-05", EndDate: "2026-08-05", GoogleEventID: "new"}}
	if err := module.ReplaceGoogleClosures(ctx, closures, "2026-08-01", "2026-08-31"); err != nil {
		t.Fatal(err)
	}
	all, err := module.List(ctx, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("closures=%v err=%v", all, err)
	}
	if _, err := module.Connect(ctx, admin, "refresh"); err != nil {
		t.Fatal(err)
	}
	if err := module.Disconnect(ctx, admin); err != nil {
		t.Fatal(err)
	}
	remaining, err := module.List(ctx, "")
	if err != nil || len(remaining) != 1 || remaining[0].Source != model.SchoolClosureSourceManual {
		t.Fatalf("closures=%v err=%v", remaining, err)
	}
}

func TestOAuthStateExpiry(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := schoolcalendar.New(db)
	ctx := context.Background()
	actor := schoolcalendar.Actor{UserID: 1, IsAdmin: true}
	if err := module.CreateOAuthState(ctx, actor, "expired", "verifier", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := module.ConsumeOAuthState(ctx, actor, "expired"); !errors.Is(err, schoolcalendar.ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
}
