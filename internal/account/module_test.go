package account_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestModuleAccountInvariants(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := account.New(db)
	manager := account.Actor{UserID: 1, CanManage: true}
	draft := account.Draft{Code: "TEST-ACCOUNT", Name: "Test Account", AccountType: model.AccountTypeAsset, NormalBalance: "debit", IsActive: true}

	if _, err := m.Create(context.Background(), account.Actor{}, draft); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("create without capability: got %v", err)
	}
	bad := draft
	bad.Code, bad.IsCash, bad.AccountType, bad.NormalBalance = "BAD-CASH", true, model.AccountTypeLiability, "credit"
	var validation *account.ValidationError
	if _, err := m.Create(context.Background(), manager, bad); !errors.As(err, &validation) || validation.Fields["is_cash"] == "" {
		t.Fatalf("invalid cash account: got %v", err)
	}
	created, err := m.Create(context.Background(), manager, draft)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("created account has no ID")
	}
	var conflict *account.ConflictError
	if _, err := m.Create(context.Background(), manager, draft); !errors.As(err, &conflict) {
		t.Fatalf("duplicate code: got %v", err)
	}

	updatedDraft := draft
	updatedDraft.Name = "Updated Account"
	updated, err := m.Update(context.Background(), manager, created.ID, updatedDraft)
	if err != nil || updated.Name != updatedDraft.Name {
		t.Fatalf("update: account=%v err=%v", updated, err)
	}
	if _, err := m.Delete(context.Background(), manager, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(context.Background(), created.ID); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("get deleted account: got %v", err)
	}

	var systemID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE is_system=1 LIMIT 1").Scan(&systemID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Delete(context.Background(), manager, systemID); !errors.As(err, &conflict) {
		t.Fatalf("delete system account: got %v", err)
	}
	system, err := m.Get(context.Background(), systemID)
	if err != nil {
		t.Fatal(err)
	}
	systemDraft := account.Draft{Code: system.Code, Name: system.Name, AccountType: system.AccountType, NormalBalance: system.NormalBalance, Description: system.Description, IsActive: system.IsActive, IsCash: system.IsCash}
	changed := systemDraft
	changed.Code += "-CHANGED"
	if _, err := m.Update(context.Background(), manager, systemID, changed); !errors.As(err, &validation) || validation.Fields["code"] == "" {
		t.Fatalf("change system account structure: got %v", err)
	}
	systemDraft.Name += " Updated"
	if updated, err := m.Update(context.Background(), manager, systemID, systemDraft); err != nil || updated.Name != systemDraft.Name {
		t.Fatalf("update system account label: account=%v err=%v", updated, err)
	}
}

func TestModuleDeleteRejectsLinkedAccount(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := account.New(db)
	actor := account.Actor{UserID: 1, CanManage: true}
	a, err := m.Create(context.Background(), actor, account.Draft{Code: "LINKED-A", Name: "Linked", AccountType: model.AccountTypeAsset, NormalBalance: "debit", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	var otherID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE id<>? LIMIT 1", a.ID).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	entry := &model.JournalEntry{EntryDate: "2026-01-01", Description: "linked", CreatedBy: 1}
	if _, err := testutil.CreateJournalEntry(db, entry, []model.JournalLine{{AccountID: a.ID, Debit: 100}, {AccountID: otherID, Credit: 100}}); err != nil {
		t.Fatal(err)
	}
	var conflict *account.ConflictError
	if _, err := m.Delete(context.Background(), actor, a.ID); !errors.As(err, &conflict) {
		t.Fatalf("delete linked account: got %v", err)
	}
}

func TestModuleDeleteRejectsEveryAccountReference(t *testing.T) {
	tests := []struct {
		name        string
		accountType string
		setup       func(*testing.T, *sql.DB, int)
	}{
		{"child account", model.AccountTypeAsset, func(t *testing.T, db *sql.DB, id int) {
			_, err := db.Exec("INSERT INTO accounts (code,name,account_type,normal_balance,parent_id) VALUES ('CHILD-A','Child','asset','debit',?)", id)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"invoice line", model.AccountTypeRevenue, func(t *testing.T, db *sql.DB, id int) {
			contactID := insertContact(t, db)
			res, err := db.Exec("INSERT INTO invoices (invoice_number,contact_id,invoice_date,due_date,created_by) VALUES ('INV-REF',?,'2026-01-01','2026-01-31',1)", contactID)
			if err != nil {
				t.Fatal(err)
			}
			invoiceID, _ := res.LastInsertId()
			if _, err = db.Exec("INSERT INTO invoice_lines (invoice_id,description,unit_price,amount,account_id) VALUES (?,'Line',100,100,?)", invoiceID, id); err != nil {
				t.Fatal(err)
			}
		}},
		{"bill line", model.AccountTypeExpense, func(t *testing.T, db *sql.DB, id int) {
			contactID := insertContact(t, db)
			res, err := db.Exec("INSERT INTO bills (bill_number,contact_id,bill_date,due_date,created_by) VALUES ('BILL-REF',?,'2026-01-01','2026-01-31',1)", contactID)
			if err != nil {
				t.Fatal(err)
			}
			billID, _ := res.LastInsertId()
			if _, err = db.Exec("INSERT INTO bill_lines (bill_id,description,unit_price,amount,account_id) VALUES (?,'Line',100,100,?)", billID, id); err != nil {
				t.Fatal(err)
			}
		}},
		{"payment", model.AccountTypeAsset, func(t *testing.T, db *sql.DB, id int) {
			if _, err := db.Exec("INSERT INTO payments (payment_date,amount,payment_type,reference_id,account_id,created_by) VALUES ('2026-01-01',100,'invoice',1,?,1)", id); err != nil {
				t.Fatal(err)
			}
		}},
		{"credit note line", model.AccountTypeRevenue, func(t *testing.T, db *sql.DB, id int) {
			contactID := insertContact(t, db)
			res, err := db.Exec("INSERT INTO credit_notes (cn_number,contact_id,cn_date,reason,created_by) VALUES ('CN-REF',?,'2026-01-01','other',1)", contactID)
			if err != nil {
				t.Fatal(err)
			}
			creditID, _ := res.LastInsertId()
			if _, err = db.Exec("INSERT INTO credit_note_lines (credit_note_id,description,unit_price,amount,account_id) VALUES (?,'Line',100,100,?)", creditID, id); err != nil {
				t.Fatal(err)
			}
		}},
		{"company default", model.AccountTypeRevenue, func(t *testing.T, db *sql.DB, id int) {
			if _, err := db.Exec("UPDATE company_profile SET default_revenue_account_id=? WHERE id=1", id); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.SetupTestDB(t)
			m := account.New(db)
			normal := "debit"
			if tt.accountType == model.AccountTypeRevenue {
				normal = "credit"
			}
			a, err := m.Create(context.Background(), account.Actor{UserID: 1, CanManage: true}, account.Draft{Code: "REF-A", Name: "Referenced", AccountType: tt.accountType, NormalBalance: normal, IsActive: true})
			if err != nil {
				t.Fatal(err)
			}
			tt.setup(t, db, a.ID)
			var conflict *account.ConflictError
			if _, err := m.Delete(context.Background(), account.Actor{UserID: 1, CanManage: true}, a.ID); !errors.As(err, &conflict) {
				t.Fatalf("delete referenced account: got %v", err)
			}
		})
	}
}

func insertContact(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO contacts (name,contact_type) VALUES ('Reference Contact','both')")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}
