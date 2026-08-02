package testutil

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/tmpl"

	latasyaerp "github.com/naufal/latasya-erp"
)

// CreateInvoice creates an invoice through the business module for test setup.
func CreateInvoice(db *sql.DB, inv *model.Invoice, lines []model.InvoiceLine) (int, error) {
	draftLines := make([]invoice.DraftLine, len(lines))
	for i, line := range lines {
		draftLines[i] = invoice.DraftLine{
			Description: line.Description,
			Quantity:    line.Quantity,
			UnitPrice:   line.UnitPrice,
			AccountID:   line.AccountID,
		}
	}
	userID := inv.CreatedBy
	if userID == 0 {
		userID = 1
	}
	created, err := invoice.New(db).Create(context.Background(), invoice.Actor{UserID: userID, CanManage: true}, invoice.Draft{
		ContactID: inv.ContactID, InvoiceDate: inv.InvoiceDate, DueDate: inv.DueDate,
		TaxAmount: inv.TaxAmount, Notes: inv.Notes, Lines: draftLines,
	})
	if err != nil {
		return 0, err
	}
	inv.ID, inv.InvoiceNumber = created.ID, created.InvoiceNumber
	return created.ID, nil
}

// SendInvoice advances a fixture through the real invoice lifecycle.
func SendInvoice(db *sql.DB, id, userID int) error {
	_, err := invoice.New(db).Send(context.Background(), invoice.Actor{UserID: userID, CanManage: true}, id)
	return err
}

// RecordInvoicePayment records a fixture payment through the business module.
func RecordInvoicePayment(db *sql.DB, id, amount int, paymentDate string, accountID, userID int) error {
	_, err := invoice.New(db).RecordPayment(context.Background(), invoice.Actor{UserID: userID, CanManage: true}, id, invoice.Payment{
		Amount: amount, Date: paymentDate, AccountID: accountID,
	})
	return err
}

// GetInvoice loads a fixture through the business module.
func GetInvoice(db *sql.DB, id int) (*model.Invoice, error) {
	return invoice.New(db).Get(context.Background(), id)
}

// Parallel tests all register the same FS; once is enough and keeps the
// global write out of the race detector's way.
var setMigrations = sync.OnceFunc(func() {
	database.SetMigrations(latasyaerp.MigrationFS)
})

// SetupTestDB creates an in-memory SQLite database with migrations and seed data applied.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	setMigrations()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	if err := database.Seed(db); err != nil {
		t.Fatalf("seed test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// SetupTestHandler creates a Handler configured for testing with real templates.
func SetupTestHandler(t *testing.T, db *sql.DB) *handler.Handler {
	t.Helper()
	return &handler.Handler{
		DB:         db,
		TemplateFS: latasyaerp.TemplateFS,
		FuncMap:    tmpl.FuncMap(),
		DevMode:    true,
		Invoices:   invoice.New(db),
		Journals:   journal.New(db),
	}
}

// CreateJournalEntry creates accounting fixtures through the journal module.
// It retains the old test signature while production code uses typed drafts.
func CreateJournalEntry(db *sql.DB, entry *model.JournalEntry, lines []model.JournalLine) (int, error) {
	actor := journal.Actor{UserID: entry.CreatedBy, CanManageJournals: true, CanManageIncome: true, CanManageExpenses: true}
	module := journal.New(db)
	toLines := make([]journal.Line, 0, len(lines))
	for _, line := range lines {
		toLines = append(toLines, journal.Line{AccountID: line.AccountID, Debit: line.Debit, Credit: line.Credit, Memo: line.Memo})
	}
	var created *model.JournalEntry
	var err error
	switch entry.SourceType {
	case model.SourceIncome:
		amount, deposit, revenue := journalShape(lines)
		created, err = module.CreateIncome(context.Background(), actor, journal.IncomeDraft{EntryDate: entry.EntryDate,
			Description: entry.Description, Amount: amount, RevenueAccount: revenue, DepositAccount: deposit})
	case model.SourceExpense:
		amount, expense, payment := journalShape(lines)
		created, err = module.CreateExpense(context.Background(), actor, journal.ExpenseDraft{EntryDate: entry.EntryDate,
			Description: entry.Description, Amount: amount, ExpenseAccount: expense, PaymentAccount: payment, VehicleID: entry.VehicleID})
	default:
		created, err = module.CreateManual(context.Background(), actor, journal.ManualDraft{EntryDate: entry.EntryDate,
			Description: entry.Description, Lines: toLines})
	}
	if err != nil {
		return 0, err
	}
	if !entry.IsPosted {
		if _, err := db.Exec("UPDATE journal_entries SET is_posted=0 WHERE id=?", created.ID); err != nil {
			return 0, err
		}
	}
	entry.ID, entry.Reference = created.ID, created.Reference
	return created.ID, nil
}

func GetJournalEntry(db *sql.DB, id int) (*model.JournalEntry, error) {
	return journal.New(db).Get(context.Background(), id)
}

func journalShape(lines []model.JournalLine) (amount, debitAccount, creditAccount int) {
	for _, line := range lines {
		if line.Debit > 0 {
			amount, debitAccount = line.Debit, line.AccountID
		}
		if line.Credit > 0 {
			creditAccount = line.AccountID
		}
	}
	return
}

// CreateTestUser creates a user in the test database and returns the user ID.
func CreateTestUser(t *testing.T, db *sql.DB, username, password, role string) int {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	result, err := db.Exec(
		"INSERT INTO users (username, password, full_name, role) VALUES (?, ?, ?, ?)",
		username, hash, username, role,
	)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

// CreateTestSession creates a session for the given user and returns the session ID.
func CreateTestSession(t *testing.T, db *sql.DB, userID int) string {
	t.Helper()
	sessionID, err := auth.CreateSession(db, userID)
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}
	return sessionID
}
