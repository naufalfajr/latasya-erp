package model

import (
	"database/sql"
	"fmt"
	"time"
)

type Invoice struct {
	ID            int
	InvoiceNumber string
	ContactID     int
	InvoiceDate   string
	DueDate       string
	Status        string
	Subtotal      int
	TaxAmount     int
	Total         int
	AmountPaid    int
	Notes         string
	JournalID     *int
	CreatedBy     int
	CreatedAt     string
	UpdatedAt     string
	// Joined
	ContactName string
	Lines       []InvoiceLine
}

type InvoiceLine struct {
	ID          int
	InvoiceID   int
	Description string
	Quantity    int // stored as qty * 100 (e.g. 1 = 100, 1.5 = 150)
	UnitPrice   int
	Amount      int
	AccountID   int
	// Joined
	AccountCode string
	AccountName string
}

type InvoiceFilter struct {
	Status string
	Search string
}

func GenerateInvoiceNumber(db *sql.DB) (string, error) {
	now := time.Now()
	prefix := fmt.Sprintf("INV-%s", now.Format("200601"))
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM invoices WHERE invoice_number LIKE ?", prefix+"%").Scan(&count)
	if err != nil {
		return "", fmt.Errorf("count invoices: %w", err)
	}
	return fmt.Sprintf("%s-%04d", prefix, count+1), nil
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
		inv.InvoiceNumber, inv.ContactID, inv.InvoiceDate, inv.DueDate, "draft",
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
			i.subtotal, i.tax_amount, i.total, i.amount_paid, COALESCE(i.notes,''),
			i.journal_id, i.created_by, i.created_at, i.updated_at, c.name
		 FROM invoices i
		 JOIN contacts c ON c.id = i.contact_id
		 WHERE i.id = ?`, id,
	).Scan(&inv.ID, &inv.InvoiceNumber, &inv.ContactID, &inv.InvoiceDate, &inv.DueDate, &inv.Status,
		&inv.Subtotal, &inv.TaxAmount, &inv.Total, &inv.AmountPaid, &inv.Notes,
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
	query := `SELECT i.id, i.invoice_number, i.contact_id, i.invoice_date, i.due_date, i.status,
			i.subtotal, i.tax_amount, i.total, i.amount_paid, COALESCE(i.notes,''),
			i.journal_id, i.created_by, i.created_at, i.updated_at, c.name
		 FROM invoices i
		 JOIN contacts c ON c.id = i.contact_id
		 WHERE 1=1`
	var args []any

	if f.Status != "" {
		query += " AND i.status = ?"
		args = append(args, f.Status)
	}
	if f.Search != "" {
		query += " AND (i.invoice_number LIKE ? OR c.name LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s)
	}
	query += " ORDER BY i.invoice_date DESC, i.id DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()

	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		err := rows.Scan(&inv.ID, &inv.InvoiceNumber, &inv.ContactID, &inv.InvoiceDate, &inv.DueDate, &inv.Status,
			&inv.Subtotal, &inv.TaxAmount, &inv.Total, &inv.AmountPaid, &inv.Notes,
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

	tx.Exec("DELETE FROM invoice_lines WHERE invoice_id = ?", inv.ID)
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
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1100'").Scan(&arAccountID)
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
		db.QueryRow("SELECT id FROM accounts WHERE code = '2-1200'").Scan(&taxAccountID)
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

	remaining := inv.Total - inv.AmountPaid
	if amount > remaining {
		return fmt.Errorf("payment amount (%d) exceeds remaining balance (%d)", amount, remaining)
	}

	// Create journal entry: Debit Cash/Bank, Credit AR
	var arAccountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1100'").Scan(&arAccountID)

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
	if newAmountPaid >= inv.Total {
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
	return inv.Total - inv.AmountPaid
}
