package model

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// ErrNoDefaultRevenueAccount is returned by GenerateRecurringInvoices when
// company_profile.default_revenue_account_id is not configured.
var ErrNoDefaultRevenueAccount = errors.New("set a default revenue account in Company Profile before generating recurring invoices")

type Invoice struct {
	ID             int           `json:"id"`
	InvoiceNumber  string        `json:"invoice_number"`
	ContactID      int           `json:"contact_id"`
	InvoiceDate    string        `json:"invoice_date"`
	DueDate        string        `json:"due_date"`
	Status         string        `json:"status"`
	Subtotal       int           `json:"-"`
	TaxAmount      int           `json:"-"`
	Total          int           `json:"-"`
	AmountPaid     int           `json:"-"`
	AmountCredited int           `json:"-"`
	Notes          string        `json:"notes"`
	JournalID      *int          `json:"journal_id"`
	CreatedBy      int           `json:"created_by"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
	ContactName    string        `json:"contact_name,omitempty"`
	Lines          []InvoiceLine `json:"lines,omitempty"`
}

// MarshalJSON serializes IDR-valued fields as strings and exposes
// computed amount_due so API clients always see a consistent contract.
func (inv Invoice) MarshalJSON() ([]byte, error) {
	type alias Invoice
	return json.Marshal(struct {
		alias
		Subtotal       string `json:"subtotal"`
		TaxAmount      string `json:"tax_amount"`
		Total          string `json:"total"`
		AmountPaid     string `json:"amount_paid"`
		AmountCredited string `json:"amount_credited"`
		AmountDue      string `json:"amount_due"`
	}{
		alias:          alias(inv),
		Subtotal:       strconv.Itoa(inv.Subtotal),
		TaxAmount:      strconv.Itoa(inv.TaxAmount),
		Total:          strconv.Itoa(inv.Total),
		AmountPaid:     strconv.Itoa(inv.AmountPaid),
		AmountCredited: strconv.Itoa(inv.AmountCredited),
		AmountDue:      strconv.Itoa(inv.Total - inv.AmountPaid - inv.AmountCredited),
	})
}

type InvoiceLine struct {
	ID          int    `json:"id"`
	InvoiceID   int    `json:"invoice_id"`
	Description string `json:"description"`
	Quantity    int    `json:"-"` // stored as qty * 100 (e.g. 1 = 100, 1.5 = 150)
	UnitPrice   int    `json:"-"`
	Amount      int    `json:"-"`
	AccountID   int    `json:"account_id"`
	AccountCode string `json:"account_code,omitempty"`
	AccountName string `json:"account_name,omitempty"`
}

// MarshalJSON serializes Quantity as a 2-decimal string and currency
// fields as integer-IDR strings.
func (l InvoiceLine) MarshalJSON() ([]byte, error) {
	type alias InvoiceLine
	return json.Marshal(struct {
		alias
		Quantity  string `json:"quantity"`
		UnitPrice string `json:"unit_price"`
		Amount    string `json:"amount"`
	}{
		alias:     alias(l),
		Quantity:  fmt.Sprintf("%d.%02d", l.Quantity/100, l.Quantity%100),
		UnitPrice: strconv.Itoa(l.UnitPrice),
		Amount:    strconv.Itoa(l.Amount),
	})
}

type InvoiceFilter struct {
	Status string
	Search string
	Limit  int // 0 = no limit (return all)
	Offset int
}

// invoiceWhere builds the shared WHERE fragment used by ListInvoices and
// CountInvoices. References c.name, so callers must JOIN contacts c.
func invoiceWhere(f InvoiceFilter) (string, []any) {
	var where string
	var args []any
	if f.Status != "" {
		where += " AND i.status = ?"
		args = append(args, f.Status)
	}
	if f.Search != "" {
		where += " AND (i.invoice_number LIKE ? OR c.name LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s)
	}
	return where, args
}

// CountInvoices returns the total invoices matching the filter, ignoring
// Limit/Offset.
func CountInvoices(db *sql.DB, f InvoiceFilter) (int, error) {
	where, args := invoiceWhere(f)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices i JOIN contacts c ON c.id = i.contact_id WHERE 1=1`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count invoices: %w", err)
	}
	return n, nil
}

func GenerateInvoiceNumber(db *sql.DB) (string, error) {
	return GenerateDocNumber(db, "invoices", "invoice_number", "INV")
}

func CreateInvoice(db *sql.DB, inv *Invoice, lines []InvoiceLine) (int, error) {
	if inv.InvoiceNumber == "" {
		num, err := GenerateInvoiceNumber(db)
		if err != nil {
			return 0, err
		}
		inv.InvoiceNumber = num
	}

	// Calculate totals
	var subtotal int
	for i := range lines {
		lines[i].Amount = lines[i].Quantity * lines[i].UnitPrice / 100
		subtotal += lines[i].Amount
	}
	inv.Subtotal = subtotal
	inv.Total = subtotal + inv.TaxAmount

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO invoices (invoice_number, contact_id, invoice_date, due_date, status, subtotal, tax_amount, total, amount_paid, notes, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		inv.InvoiceNumber, inv.ContactID, inv.InvoiceDate, inv.DueDate, StatusDraft,
		inv.Subtotal, inv.TaxAmount, inv.Total, inv.Notes, inv.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert invoice: %w", err)
	}

	invID64, _ := result.LastInsertId()
	invID := int(invID64)

	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO invoice_lines (invoice_id, description, quantity, unit_price, amount, account_id) VALUES (?, ?, ?, ?, ?, ?)",
			invID, l.Description, l.Quantity, l.UnitPrice, l.Amount, l.AccountID,
		)
		if err != nil {
			return 0, fmt.Errorf("insert invoice line: %w", err)
		}
	}

	return invID, tx.Commit()
}

func GetInvoice(db *sql.DB, id int) (*Invoice, error) {
	inv := &Invoice{}
	err := db.QueryRow(
		`SELECT i.id, i.invoice_number, i.contact_id, i.invoice_date, i.due_date, i.status,
			i.subtotal, i.tax_amount, i.total, i.amount_paid, i.amount_credited, COALESCE(i.notes,''),
			i.journal_id, i.created_by, i.created_at, i.updated_at, c.name
		 FROM invoices i
		 JOIN contacts c ON c.id = i.contact_id
		 WHERE i.id = ?`, id,
	).Scan(&inv.ID, &inv.InvoiceNumber, &inv.ContactID, &inv.InvoiceDate, &inv.DueDate, &inv.Status,
		&inv.Subtotal, &inv.TaxAmount, &inv.Total, &inv.AmountPaid, &inv.AmountCredited, &inv.Notes,
		&inv.JournalID, &inv.CreatedBy, &inv.CreatedAt, &inv.UpdatedAt, &inv.ContactName)
	if err != nil {
		return nil, fmt.Errorf("get invoice: %w", err)
	}

	lines, err := getInvoiceLines(db, id)
	if err != nil {
		return nil, err
	}
	inv.Lines = lines

	return inv, nil
}

func getInvoiceLines(db *sql.DB, invoiceID int) ([]InvoiceLine, error) {
	rows, err := db.Query(
		`SELECT il.id, il.invoice_id, il.description, il.quantity, il.unit_price, il.amount, il.account_id,
			a.code, a.name
		 FROM invoice_lines il
		 JOIN accounts a ON a.id = il.account_id
		 WHERE il.invoice_id = ?
		 ORDER BY il.id`, invoiceID,
	)
	if err != nil {
		return nil, fmt.Errorf("get invoice lines: %w", err)
	}
	defer rows.Close()

	var lines []InvoiceLine
	for rows.Next() {
		var l InvoiceLine
		err := rows.Scan(&l.ID, &l.InvoiceID, &l.Description, &l.Quantity, &l.UnitPrice, &l.Amount, &l.AccountID,
			&l.AccountCode, &l.AccountName)
		if err != nil {
			return nil, fmt.Errorf("scan invoice line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, nil
}

func ListInvoices(db *sql.DB, f InvoiceFilter) ([]Invoice, error) {
	where, args := invoiceWhere(f)
	query := `SELECT i.id, i.invoice_number, i.contact_id, i.invoice_date, i.due_date, i.status,
			i.subtotal, i.tax_amount, i.total, i.amount_paid, i.amount_credited, COALESCE(i.notes,''),
			i.journal_id, i.created_by, i.created_at, i.updated_at, c.name
		 FROM invoices i
		 JOIN contacts c ON c.id = i.contact_id
		 WHERE 1=1` + where + ` ORDER BY i.invoice_date DESC, i.id DESC`
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()

	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		err := rows.Scan(&inv.ID, &inv.InvoiceNumber, &inv.ContactID, &inv.InvoiceDate, &inv.DueDate, &inv.Status,
			&inv.Subtotal, &inv.TaxAmount, &inv.Total, &inv.AmountPaid, &inv.AmountCredited, &inv.Notes,
			&inv.JournalID, &inv.CreatedBy, &inv.CreatedAt, &inv.UpdatedAt, &inv.ContactName)
		if err != nil {
			return nil, fmt.Errorf("scan invoice: %w", err)
		}
		invoices = append(invoices, inv)
	}
	return invoices, nil
}

func UpdateInvoice(db *sql.DB, inv *Invoice, lines []InvoiceLine) error {
	// Only allow editing drafts
	var status string
	db.QueryRow("SELECT status FROM invoices WHERE id = ?", inv.ID).Scan(&status)
	if status != "draft" {
		return fmt.Errorf("can only edit draft invoices (current: %s)", status)
	}

	var subtotal int
	for i := range lines {
		lines[i].Amount = lines[i].Quantity * lines[i].UnitPrice / 100
		subtotal += lines[i].Amount
	}
	inv.Subtotal = subtotal
	inv.Total = subtotal + inv.TaxAmount

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`UPDATE invoices SET contact_id=?, invoice_date=?, due_date=?, subtotal=?, tax_amount=?, total=?, notes=?, updated_at=datetime('now') WHERE id=?`,
		inv.ContactID, inv.InvoiceDate, inv.DueDate, inv.Subtotal, inv.TaxAmount, inv.Total, inv.Notes, inv.ID,
	)
	if err != nil {
		return fmt.Errorf("update invoice: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM invoice_lines WHERE invoice_id = ?", inv.ID); err != nil {
		return fmt.Errorf("delete invoice lines: %w", err)
	}
	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO invoice_lines (invoice_id, description, quantity, unit_price, amount, account_id) VALUES (?, ?, ?, ?, ?, ?)",
			inv.ID, l.Description, l.Quantity, l.UnitPrice, l.Amount, l.AccountID,
		)
		if err != nil {
			return fmt.Errorf("insert invoice line: %w", err)
		}
	}

	return tx.Commit()
}

// SendInvoice marks an invoice as "sent" and creates the AR journal entry:
// Debit: Accounts Receivable, Credit: Revenue accounts
func SendInvoice(db *sql.DB, id int, userID int) error {
	inv, err := GetInvoice(db, id)
	if err != nil {
		return err
	}
	if inv.Status != "draft" {
		return fmt.Errorf("can only send draft invoices (current: %s)", inv.Status)
	}

	// Create journal entry: Debit AR, Credit Revenue
	var arAccountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = ?", AccountCodeAR).Scan(&arAccountID)
	if arAccountID == 0 {
		return fmt.Errorf("accounts receivable account not found")
	}

	je := &JournalEntry{
		EntryDate:   inv.InvoiceDate,
		Description: fmt.Sprintf("Invoice %s - %s", inv.InvoiceNumber, inv.ContactName),
		SourceType:  "invoice",
		IsPosted:    true,
		CreatedBy:   userID,
	}

	var lines []JournalLine
	// Debit AR for total
	lines = append(lines, JournalLine{
		AccountID: arAccountID,
		Debit:     inv.Total,
		Credit:    0,
		Memo:      inv.InvoiceNumber,
	})
	// Credit each revenue line
	for _, il := range inv.Lines {
		lines = append(lines, JournalLine{
			AccountID: il.AccountID,
			Debit:     0,
			Credit:    il.Amount,
			Memo:      il.Description,
		})
	}
	// Credit tax if any
	if inv.TaxAmount > 0 {
		var taxAccountID int
		db.QueryRow("SELECT id FROM accounts WHERE code = ?", AccountCodeTax).Scan(&taxAccountID)
		if taxAccountID > 0 {
			lines = append(lines, JournalLine{
				AccountID: taxAccountID,
				Debit:     0,
				Credit:    inv.TaxAmount,
				Memo:      "Tax",
			})
		}
	}

	journalID, err := CreateJournalEntry(db, je, lines)
	if err != nil {
		return fmt.Errorf("create journal entry: %w", err)
	}

	_, err = db.Exec("UPDATE invoices SET status = 'sent', journal_id = ?, updated_at = datetime('now') WHERE id = ?", journalID, id)
	return err
}

// RecordInvoicePayment records a payment against an invoice
func RecordInvoicePayment(db *sql.DB, invoiceID int, amount int, paymentDate string, paymentAccountID int, userID int) error {
	inv, err := GetInvoice(db, invoiceID)
	if err != nil {
		return err
	}
	if inv.Status == "draft" || inv.Status == "cancelled" || inv.Status == "paid" {
		return fmt.Errorf("cannot record payment for %s invoice", inv.Status)
	}

	remaining := inv.Total - inv.AmountPaid - inv.AmountCredited
	if amount > remaining {
		return fmt.Errorf("payment amount (%d) exceeds remaining balance (%d)", amount, remaining)
	}

	// Create journal entry: Debit Cash/Bank, Credit AR
	var arAccountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = ?", AccountCodeAR).Scan(&arAccountID)

	je := &JournalEntry{
		EntryDate:   paymentDate,
		Description: fmt.Sprintf("Payment for %s", inv.InvoiceNumber),
		SourceType:  "invoice",
		IsPosted:    true,
		CreatedBy:   userID,
	}

	lines := []JournalLine{
		{AccountID: paymentAccountID, Debit: amount, Credit: 0, Memo: "Payment received"},
		{AccountID: arAccountID, Debit: 0, Credit: amount, Memo: inv.InvoiceNumber},
	}

	journalID, err := CreateJournalEntry(db, je, lines)
	if err != nil {
		return fmt.Errorf("create payment journal: %w", err)
	}

	// Record payment
	_, err = db.Exec(
		"INSERT INTO payments (payment_date, amount, payment_type, reference_id, payment_method, account_id, journal_id, created_by) VALUES (?, ?, 'invoice', ?, 'bank_transfer', ?, ?, ?)",
		paymentDate, amount, invoiceID, paymentAccountID, journalID, userID,
	)
	if err != nil {
		return fmt.Errorf("insert payment: %w", err)
	}

	// Update invoice
	newAmountPaid := inv.AmountPaid + amount
	newStatus := "partial"
	if newAmountPaid+inv.AmountCredited >= inv.Total {
		newStatus = "paid"
	}

	_, err = db.Exec("UPDATE invoices SET amount_paid = ?, status = ?, updated_at = datetime('now') WHERE id = ?",
		newAmountPaid, newStatus, invoiceID)
	return err
}

func DeleteInvoice(db *sql.DB, id int) error {
	var status string
	db.QueryRow("SELECT status FROM invoices WHERE id = ?", id).Scan(&status)
	if status != "draft" {
		return fmt.Errorf("can only delete draft invoices (current: %s)", status)
	}
	_, err := db.Exec("DELETE FROM invoices WHERE id = ?", id)
	return err
}

func (inv *Invoice) AmountDue() int {
	return inv.Total - inv.AmountPaid - inv.AmountCredited
}

// Outcomes reported per customer by GenerateRecurringInvoices.
const (
	GeneratedInvoiceCreated          = "created"
	GeneratedInvoiceSkippedThisMonth = "skipped_already_invoiced"
	GeneratedInvoiceFailed           = "failed"
)

// GeneratedInvoice records the outcome for a single customer in a
// recurring-invoice batch run.
type GeneratedInvoice struct {
	ContactID     int    `json:"contact_id"`
	ContactName   string `json:"contact_name"`
	InvoiceID     int    `json:"invoice_id,omitempty"`
	InvoiceNumber string `json:"invoice_number,omitempty"`
	Result        string `json:"result"`
	Error         string `json:"error,omitempty"`
}

// GenerateRecurringInvoicesResult summarizes a recurring-invoice batch run.
type GenerateRecurringInvoicesResult struct {
	Created int                `json:"created"`
	Skipped int                `json:"skipped"`
	Failed  int                `json:"failed"`
	Items   []GeneratedInvoice `json:"items"`
}

// CreatedNumbers returns the invoice numbers created in the batch, for audit logging.
func (r *GenerateRecurringInvoicesResult) CreatedNumbers() []string {
	nums := []string{}
	for _, it := range r.Items {
		if it.Result == GeneratedInvoiceCreated {
			nums = append(nums, it.InvoiceNumber)
		}
	}
	return nums
}

// recurringInvoiceMu serializes GenerateRecurringInvoices so two concurrent runs
// cannot both pass the per-customer "already invoiced this month" check, or both
// reserve the same document number, before either commits. SetMaxOpenConns(1)
// only serializes individual statements, not this check-then-insert sequence.
// Sufficient for the single-process deployment; horizontal scaling would need a
// DB-level lock.
var recurringInvoiceMu sync.Mutex

// MonthNameID returns the Indonesian month name for month m (1–12).
func MonthNameID(m int) string {
	names := [...]string{
		"Januari", "Februari", "Maret", "April", "Mei", "Juni",
		"Juli", "Agustus", "September", "Oktober", "November", "Desember",
	}
	if m < 1 || m > 12 {
		return ""
	}
	return names[m-1]
}

// GenerateRecurringInvoices creates a draft invoice for every active customer.
// Invoices are built from scratch using the contact pricing formula, the global
// default revenue account, and the global recurring description template — no
// prior invoice is cloned.
//
// A customer is skipped when they already have an invoice dated in
// invoiceDate's month (prevents double-billing on repeat runs). Per-customer
// errors are recorded as "failed" items and do not abort the batch. Generated
// invoices are drafts.
func GenerateRecurringInvoices(db *sql.DB, invoiceDate, dueDate string, userID int) (*GenerateRecurringInvoicesResult, error) {
	if len(invoiceDate) < 7 {
		return nil, fmt.Errorf("invalid invoice date: %q", invoiceDate)
	}
	monthPrefix := invoiceDate[:7] // YYYY-MM

	profile, err := GetCompanyProfile(db)
	if err != nil {
		return nil, fmt.Errorf("load company profile: %w", err)
	}
	if profile.DefaultRevenueAccountID == 0 {
		return nil, ErrNoDefaultRevenueAccount
	}

	// Derive month name and year from invoiceDate (format YYYY-MM-DD).
	var invYear, invMonth int
	if n, _ := fmt.Sscanf(invoiceDate[:7], "%d-%d", &invYear, &invMonth); n != 2 {
		return nil, fmt.Errorf("invalid invoice date %q", invoiceDate)
	}
	descTemplate := profile.RecurringDescriptionTemplate
	if descTemplate == "" {
		descTemplate = "Antar jemput {month} {year}"
	}
	description := strings.NewReplacer(
		"{month}", MonthNameID(invMonth),
		"{year}", strconv.Itoa(invYear),
	).Replace(descTemplate)

	recurringInvoiceMu.Lock()
	defer recurringInvoiceMu.Unlock()

	active := true
	customers, err := ListContacts(db, ContactFilter{Type: "customer", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list active customers: %w", err)
	}

	result := &GenerateRecurringInvoicesResult{Items: []GeneratedInvoice{}}

	for _, c := range customers {
		item := GeneratedInvoice{ContactID: c.ID, ContactName: c.Name}

		var thisMonth int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM invoices WHERE contact_id = ? AND substr(invoice_date, 1, 7) = ?",
			c.ID, monthPrefix,
		).Scan(&thisMonth); err != nil {
			item.Result, item.Error = GeneratedInvoiceFailed, err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if thisMonth > 0 {
			item.Result = GeneratedInvoiceSkippedThisMonth
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}

		lines := []InvoiceLine{
			{
				Description: description,
				Quantity:    100, // 1.00 × 100
				UnitPrice:   c.Price(),
				AccountID:   profile.DefaultRevenueAccountID,
			},
		}

		inv := &Invoice{
			ContactID:   c.ID,
			InvoiceDate: invoiceDate,
			DueDate:     dueDate,
			TaxAmount:   0,
			CreatedBy:   userID,
		}

		invID, err := CreateInvoice(db, inv, lines)
		if err != nil {
			item.Result, item.Error = GeneratedInvoiceFailed, err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.InvoiceID = invID
		item.InvoiceNumber = inv.InvoiceNumber
		item.Result = GeneratedInvoiceCreated
		result.Created++
		result.Items = append(result.Items, item)
	}

	return result, nil
}

// DeletedInvoice identifies an invoice removed by BulkDeleteDraftInvoices, so
// the caller can record exactly which invoices were deleted in the audit trail.
type DeletedInvoice struct {
	ID            int    `json:"id"`
	InvoiceNumber string `json:"invoice_number"`
}

// BulkDeleteDraftInvoices deletes the draft invoices among the given IDs in a
// single transaction. IDs that are non-existent or not in draft status are
// skipped (not deleted) and returned. Returns the deleted invoices (id +
// number) and the skipped IDs.
func BulkDeleteDraftInvoices(db *sql.DB, ids []int) ([]DeletedInvoice, []int, error) {
	deleted := []DeletedInvoice{}
	skipped := []int{}
	if len(ids) == 0 {
		return deleted, skipped, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, id := range ids {
		var status, number string
		err := tx.QueryRow("SELECT status, invoice_number FROM invoices WHERE id = ?", id).Scan(&status, &number)
		if errors.Is(err, sql.ErrNoRows) {
			skipped = append(skipped, id)
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("lookup invoice %d: %w", id, err)
		}
		if status != StatusDraft {
			skipped = append(skipped, id)
			continue
		}
		if _, err := tx.Exec("DELETE FROM invoices WHERE id = ?", id); err != nil {
			return nil, nil, fmt.Errorf("delete invoice %d: %w", id, err)
		}
		deleted = append(deleted, DeletedInvoice{ID: id, InvoiceNumber: number})
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return deleted, skipped, nil
}

// SentInvoice identifies an invoice marked sent by BulkSendInvoices, with the
// journal entry that was posted (for reconciliation against the ledger).
type SentInvoice struct {
	ID            int    `json:"id"`
	InvoiceNumber string `json:"invoice_number"`
	JournalID     *int   `json:"journal_id,omitempty"`
}

// FailedInvoice identifies an invoice whose bulk send failed, with the reason.
type FailedInvoice struct {
	ID            int    `json:"id"`
	InvoiceNumber string `json:"invoice_number"`
	Error         string `json:"error"`
}

// BulkSendResult summarizes a bulk-send batch: invoices sent, IDs skipped
// (unknown or not draft), and invoices whose send failed (with the reason).
type BulkSendResult struct {
	Sent    []SentInvoice   `json:"sent"`
	Skipped []int           `json:"skipped"`
	Failed  []FailedInvoice `json:"failed"`
}

// bulkSendMu serializes BulkSendInvoices so two concurrent runs (e.g. a
// double-clicked button) cannot both pass the draft check for the same invoice
// and post duplicate journal entries. Single-process scope.
var bulkSendMu sync.Mutex

// BulkSendInvoices sends each draft invoice among the given IDs (marking it
// "sent" and posting its AR journal entry via SendInvoice). IDs that are unknown
// or not in draft status are skipped; a send error is recorded under Failed and
// does not abort the batch.
func BulkSendInvoices(db *sql.DB, ids []int, userID int) (*BulkSendResult, error) {
	bulkSendMu.Lock()
	defer bulkSendMu.Unlock()

	res := &BulkSendResult{Sent: []SentInvoice{}, Skipped: []int{}, Failed: []FailedInvoice{}}
	for _, id := range ids {
		var status, number string
		err := db.QueryRow("SELECT status, invoice_number FROM invoices WHERE id = ?", id).Scan(&status, &number)
		if errors.Is(err, sql.ErrNoRows) {
			res.Skipped = append(res.Skipped, id)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lookup invoice %d: %w", id, err)
		}
		if status != StatusDraft {
			res.Skipped = append(res.Skipped, id)
			continue
		}
		if err := SendInvoice(db, id, userID); err != nil {
			res.Failed = append(res.Failed, FailedInvoice{ID: id, InvoiceNumber: number, Error: err.Error()})
			continue
		}
		var journalID *int
		db.QueryRow("SELECT journal_id FROM invoices WHERE id = ?", id).Scan(&journalID)
		res.Sent = append(res.Sent, SentInvoice{ID: id, InvoiceNumber: number, JournalID: journalID})
	}
	return res, nil
}
