package audit_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func listAudit(db *sql.DB, filter audit.ListFilter) ([]audit.Entry, int, error) {
	page, err := audit.New(db).List(context.Background(), audit.Actor{UserID: 1, CanView: true}, filter)
	if err != nil {
		return nil, 0, err
	}
	return page.Entries, page.Total, nil
}

func TestListRequiresAuthorization(t *testing.T) {
	db := testutil.SetupTestDB(t)
	if _, err := audit.New(db).List(context.Background(), audit.Actor{}, audit.ListFilter{}); !errors.Is(err, audit.ErrForbidden) {
		t.Fatalf("error=%v", err)
	}
}

func TestList_ReturnsMostRecentFirst(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	audit.Log(ctx, db, audit.Event{Action: "first", ActorUsername: "a"})
	time.Sleep(5 * time.Millisecond)
	audit.Log(ctx, db, audit.Event{Action: "second", ActorUsername: "a"})
	time.Sleep(5 * time.Millisecond)
	audit.Log(ctx, db, audit.Event{Action: "third", ActorUsername: "a"})

	entries, total, err := listAudit(db, audit.ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(entries))
	}
	if entries[0].Action != "third" || entries[2].Action != "first" {
		t.Errorf("order wrong: %s, %s, %s", entries[0].Action, entries[1].Action, entries[2].Action)
	}
}

func TestList_FilterByActor(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	audit.Log(ctx, db, audit.Event{Action: "a", ActorUsername: "alice"})
	audit.Log(ctx, db, audit.Event{Action: "b", ActorUsername: "bob"})
	audit.Log(ctx, db, audit.Event{Action: "c", ActorUsername: "alice"})

	entries, total, err := listAudit(db, audit.ListFilter{ActorUsername: "alice"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(entries) != 2 {
		t.Errorf("expected 2 entries for alice, got total=%d len=%d", total, len(entries))
	}
}

func TestList_FilterByActionPrefix(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	audit.Log(ctx, db, audit.Event{Action: "invoice.create"})
	audit.Log(ctx, db, audit.Event{Action: "invoice.update"})
	audit.Log(ctx, db, audit.Event{Action: "bill.create"})

	entries, total, err := listAudit(db, audit.ListFilter{ActionPrefix: "invoice."})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(entries) != 2 {
		t.Errorf("expected 2 invoice.* entries, got total=%d len=%d", total, len(entries))
	}
}

func TestList_Pagination(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	for i := 0; i < 7; i++ {
		audit.Log(ctx, db, audit.Event{Action: "test", ActorUsername: "a"})
	}

	page1, total, err := listAudit(db, audit.ListFilter{Limit: 3, Offset: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 7 {
		t.Errorf("total = %d, want 7", total)
	}
	if len(page1) != 3 {
		t.Errorf("page1 len = %d, want 3", len(page1))
	}

	page3, _, _ := listAudit(db, audit.ListFilter{Limit: 3, Offset: 6})
	if len(page3) != 1 {
		t.Errorf("page3 len = %d, want 1 (leftover from 7)", len(page3))
	}
}
