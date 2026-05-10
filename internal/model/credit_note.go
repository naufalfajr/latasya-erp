package model

import (
	"database/sql"
	"fmt"
)

// CreditNote represents a document that offsets a previously sent invoice.
// When issued, it posts a journal entry that mirrors the original invoice's
// posting (debit revenue, credit accounts receivable), preserving both rows
// for the audit trail.
type CreditNote struct {
	ID        int    `json:"id"`
	CNNumber  string `json:"cn_number"`
	ContactID int    `json:"contact_id"`
	InvoiceID *int   `json:"invoice_id,omitempty"`
	CNDate    string `json:"cn_date"`
	Reason    string `json:"reason"`
	Status    string `json:"status"`
	Subtotal  int    `json:"subtotal"`
	TaxAmount int    `json:"tax_amount"`
	Total     int    `json:"total"`
	Notes     string `json:"notes"`
	JournalID *int   `json:"journal_id,omitempty"`
	CreatedBy int    `json:"created_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// Joined
	ContactName   string           `json:"contact_name,omitempty"`
	InvoiceNumber string           `json:"invoice_number,omitempty"`
	Lines         []CreditNoteLine `json:"lines,omitempty"`
}

type CreditNoteLine struct {
	ID           int    `json:"id"`
	CreditNoteID int    `json:"credit_note_id"`
	Description  string `json:"description"`
	Quantity     int    `json:"quantity"`
	UnitPrice    int    `json:"unit_price"`
	Amount       int    `json:"amount"`
	AccountID    int    `json:"account_id"`
	// Joined
	AccountCode string `json:"account_code,omitempty"`
	AccountName string `json:"account_name,omitempty"`
}

type CreditNoteFilter struct {
	Status string
	Search string
}

func GenerateCreditNoteNumber(db *sql.DB) (string, error) {
	return GenerateDocNumber(db, "credit_notes", "cn_number", "CN")
}

func CreateCreditNote(db *sql.DB, cn *CreditNote, lines []CreditNoteLine) (int, error) {
	if cn.CNNumber == "" {
		num, err := GenerateCreditNoteNumber(db)
		if err != nil {
			return 0, err
		}
		cn.CNNumber = num
	}

	var subtotal int
	for i := range lines {
		lines[i].Amount = lines[i].Quantity * lines[i].UnitPrice / 100
		subtotal += lines[i].Amount
	}
	cn.Subtotal = subtotal
	cn.Total = subtotal + cn.TaxAmount

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO credit_notes (cn_number, contact_id, invoice_id, cn_date, reason, status, subtotal, tax_amount, total, notes, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cn.CNNumber, cn.ContactID, cn.InvoiceID, cn.CNDate, cn.Reason, StatusDraft,
		cn.Subtotal, cn.TaxAmount, cn.Total, cn.Notes, cn.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert credit note: %w", err)
	}

	cnID64, _ := result.LastInsertId()
	cnID := int(cnID64)

	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO credit_note_lines (credit_note_id, description, quantity, unit_price, amount, account_id) VALUES (?, ?, ?, ?, ?, ?)",
			cnID, l.Description, l.Quantity, l.UnitPrice, l.Amount, l.AccountID,
		)
		if err != nil {
			return 0, fmt.Errorf("insert credit note line: %w", err)
		}
	}

	return cnID, tx.Commit()
}

func GetCreditNote(db *sql.DB, id int) (*CreditNote, error) {
	cn := &CreditNote{}
	var invoiceNumber sql.NullString
	err := db.QueryRow(
		`SELECT cn.id, cn.cn_number, cn.contact_id, cn.invoice_id, cn.cn_date, cn.reason, cn.status,
			cn.subtotal, cn.tax_amount, cn.total, COALESCE(cn.notes,''),
			cn.journal_id, cn.created_by, cn.created_at, cn.updated_at,
			c.name, i.invoice_number
		 FROM credit_notes cn
		 JOIN contacts c ON c.id = cn.contact_id
		 LEFT JOIN invoices i ON i.id = cn.invoice_id
		 WHERE cn.id = ?`, id,
	).Scan(&cn.ID, &cn.CNNumber, &cn.ContactID, &cn.InvoiceID, &cn.CNDate, &cn.Reason, &cn.Status,
		&cn.Subtotal, &cn.TaxAmount, &cn.Total, &cn.Notes,
		&cn.JournalID, &cn.CreatedBy, &cn.CreatedAt, &cn.UpdatedAt,
		&cn.ContactName, &invoiceNumber)
	if err != nil {
		return nil, fmt.Errorf("get credit note: %w", err)
	}
	if invoiceNumber.Valid {
		cn.InvoiceNumber = invoiceNumber.String
	}

	lines, err := getCreditNoteLines(db, id)
	if err != nil {
		return nil, err
	}
	cn.Lines = lines

	return cn, nil
}

func getCreditNoteLines(db *sql.DB, cnID int) ([]CreditNoteLine, error) {
	rows, err := db.Query(
		`SELECT cnl.id, cnl.credit_note_id, cnl.description, cnl.quantity, cnl.unit_price, cnl.amount, cnl.account_id,
			a.code, a.name
		 FROM credit_note_lines cnl
		 JOIN accounts a ON a.id = cnl.account_id
		 WHERE cnl.credit_note_id = ?
		 ORDER BY cnl.id`, cnID,
	)
	if err != nil {
		return nil, fmt.Errorf("get credit note lines: %w", err)
	}
	defer rows.Close()

	var lines []CreditNoteLine
	for rows.Next() {
		var l CreditNoteLine
		err := rows.Scan(&l.ID, &l.CreditNoteID, &l.Description, &l.Quantity, &l.UnitPrice, &l.Amount, &l.AccountID,
			&l.AccountCode, &l.AccountName)
		if err != nil {
			return nil, fmt.Errorf("scan credit note line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, nil
}

func ListCreditNotes(db *sql.DB, f CreditNoteFilter) ([]CreditNote, error) {
	query := `SELECT cn.id, cn.cn_number, cn.contact_id, cn.invoice_id, cn.cn_date, cn.reason, cn.status,
			cn.subtotal, cn.tax_amount, cn.total, COALESCE(cn.notes,''),
			cn.journal_id, cn.created_by, cn.created_at, cn.updated_at,
			c.name, COALESCE(i.invoice_number,'')
		 FROM credit_notes cn
		 JOIN contacts c ON c.id = cn.contact_id
		 LEFT JOIN invoices i ON i.id = cn.invoice_id
		 WHERE 1=1`
	var args []any

	if f.Status != "" {
		query += " AND cn.status = ?"
		args = append(args, f.Status)
	}
	if f.Search != "" {
		query += " AND (cn.cn_number LIKE ? OR c.name LIKE ? OR i.invoice_number LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s, s)
	}
	query += " ORDER BY cn.cn_date DESC, cn.id DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list credit notes: %w", err)
	}
	defer rows.Close()

	var notes []CreditNote
	for rows.Next() {
		var cn CreditNote
		err := rows.Scan(&cn.ID, &cn.CNNumber, &cn.ContactID, &cn.InvoiceID, &cn.CNDate, &cn.Reason, &cn.Status,
			&cn.Subtotal, &cn.TaxAmount, &cn.Total, &cn.Notes,
			&cn.JournalID, &cn.CreatedBy, &cn.CreatedAt, &cn.UpdatedAt,
			&cn.ContactName, &cn.InvoiceNumber)
		if err != nil {
			return nil, fmt.Errorf("scan credit note: %w", err)
		}
		notes = append(notes, cn)
	}
	return notes, nil
}

// ListCreditNotesForInvoice returns all credit notes linked to a given
// invoice, used by the invoice view to render a "Credit Notes" section.
func ListCreditNotesForInvoice(db *sql.DB, invoiceID int) ([]CreditNote, error) {
	rows, err := db.Query(
		`SELECT id, cn_number, cn_date, reason, status, total, journal_id
		 FROM credit_notes
		 WHERE invoice_id = ?
		 ORDER BY cn_date DESC, id DESC`, invoiceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list credit notes for invoice: %w", err)
	}
	defer rows.Close()

	var notes []CreditNote
	for rows.Next() {
		var cn CreditNote
		if err := rows.Scan(&cn.ID, &cn.CNNumber, &cn.CNDate, &cn.Reason, &cn.Status, &cn.Total, &cn.JournalID); err != nil {
			return nil, fmt.Errorf("scan credit note: %w", err)
		}
		notes = append(notes, cn)
	}
	return notes, nil
}

func UpdateCreditNote(db *sql.DB, cn *CreditNote, lines []CreditNoteLine) error {
	var status string
	db.QueryRow("SELECT status FROM credit_notes WHERE id = ?", cn.ID).Scan(&status)
	if status != StatusDraft {
		return fmt.Errorf("can only edit draft credit notes (current: %s)", status)
	}

	var subtotal int
	for i := range lines {
		lines[i].Amount = lines[i].Quantity * lines[i].UnitPrice / 100
		subtotal += lines[i].Amount
	}
	cn.Subtotal = subtotal
	cn.Total = subtotal + cn.TaxAmount

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`UPDATE credit_notes SET contact_id=?, invoice_id=?, cn_date=?, reason=?, subtotal=?, tax_amount=?, total=?, notes=?, updated_at=datetime('now') WHERE id=?`,
		cn.ContactID, cn.InvoiceID, cn.CNDate, cn.Reason, cn.Subtotal, cn.TaxAmount, cn.Total, cn.Notes, cn.ID,
	)
	if err != nil {
		return fmt.Errorf("update credit note: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM credit_note_lines WHERE credit_note_id = ?", cn.ID); err != nil {
		return fmt.Errorf("delete credit note lines: %w", err)
	}
	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO credit_note_lines (credit_note_id, description, quantity, unit_price, amount, account_id) VALUES (?, ?, ?, ?, ?, ?)",
			cn.ID, l.Description, l.Quantity, l.UnitPrice, l.Amount, l.AccountID,
		)
		if err != nil {
			return fmt.Errorf("insert credit note line: %w", err)
		}
	}

	return tx.Commit()
}

// IssueCreditNote posts the reversing journal entry. Mirror of SendInvoice:
// debit revenue (per line) + tax, credit accounts receivable (total).
// If linked to an invoice, increments that invoice's amount_credited and may
// flip its status to paid (when fully settled) or cancelled (when fully
// credited and never paid).
func IssueCreditNote(db *sql.DB, id int, userID int) error {
	cn, err := GetCreditNote(db, id)
	if err != nil {
		return err
	}
	if cn.Status != StatusDraft {
		return fmt.Errorf("can only issue draft credit notes (current: %s)", cn.Status)
	}

	var arAccountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = ?", AccountCodeAR).Scan(&arAccountID)
	if arAccountID == 0 {
		return fmt.Errorf("accounts receivable account not found")
	}

	if cn.InvoiceID != nil {
		var invContactID int
		db.QueryRow("SELECT contact_id FROM invoices WHERE id = ?", *cn.InvoiceID).Scan(&invContactID)
		if invContactID != 0 && invContactID != cn.ContactID {
			return fmt.Errorf("credit note contact does not match invoice contact")
		}
	}

	if cn.InvoiceID != nil && cn.TaxAmount > 0 {
		var invTaxAmount int
		db.QueryRow("SELECT tax_amount FROM invoices WHERE id = ?", *cn.InvoiceID).Scan(&invTaxAmount)
		if cn.TaxAmount > invTaxAmount {
			return fmt.Errorf("credit note tax (%d) exceeds original invoice tax (%d)", cn.TaxAmount, invTaxAmount)
		}
	}

	desc := fmt.Sprintf("Credit Note %s - %s", cn.CNNumber, cn.ContactName)
	if cn.InvoiceNumber != "" {
		desc = fmt.Sprintf("Credit Note %s for invoice %s", cn.CNNumber, cn.InvoiceNumber)
	}

	je := &JournalEntry{
		EntryDate:   cn.CNDate,
		Description: desc,
		SourceType:  SourceCreditNote,
		IsPosted:    true,
		CreatedBy:   userID,
	}

	var lines []JournalLine
	// Debit each revenue line — undoing the revenue originally recognized.
	for _, cnl := range cn.Lines {
		lines = append(lines, JournalLine{
			AccountID: cnl.AccountID,
			Debit:     cnl.Amount,
			Credit:    0,
			Memo:      cnl.Description,
		})
	}
	// Debit tax if any.
	if cn.TaxAmount > 0 {
		var taxAccountID int
		db.QueryRow("SELECT id FROM accounts WHERE code = ?", AccountCodeTax).Scan(&taxAccountID)
		if taxAccountID > 0 {
			lines = append(lines, JournalLine{
				AccountID: taxAccountID,
				Debit:     cn.TaxAmount,
				Credit:    0,
				Memo:      "Tax reversal",
			})
		}
	}
	// Credit accounts receivable — the customer no longer owes this amount.
	lines = append(lines, JournalLine{
		AccountID: arAccountID,
		Debit:     0,
		Credit:    cn.Total,
		Memo:      cn.CNNumber,
	})

	// CreateJournalEntry manages its own transaction. Post it first; if it
	// succeeds but the follow-up updates fail, the journal stays posted but
	// the credit note remains in draft and the user can retry.
	var journalID int
	if cn.JournalID != nil {
		journalID = *cn.JournalID
	} else {
		journalID, err = CreateJournalEntry(db, je, lines)
		if err != nil {
			return fmt.Errorf("create journal entry: %w", err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"UPDATE credit_notes SET status = ?, journal_id = ?, updated_at = datetime('now') WHERE id = ?",
		StatusIssued, journalID, id,
	); err != nil {
		return fmt.Errorf("update credit note: %w", err)
	}

	if cn.InvoiceID != nil {
		if err := applyCreditToInvoice(tx, *cn.InvoiceID, cn.Total); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// applyCreditToInvoice bumps amount_credited and adjusts status. If the
// credit fully settles the invoice and nothing was paid, the invoice flips
// to "cancelled"; otherwise to "paid" when paid+credited covers the total.
func applyCreditToInvoice(tx *sql.Tx, invoiceID int, amount int) error {
	var total, amountPaid, amountCredited int
	var status string
	err := tx.QueryRow(
		"SELECT total, amount_paid, amount_credited, status FROM invoices WHERE id = ?",
		invoiceID,
	).Scan(&total, &amountPaid, &amountCredited, &status)
	if err != nil {
		return fmt.Errorf("read invoice for credit: %w", err)
	}

	if status == StatusDraft || status == StatusCancelled {
		return fmt.Errorf("cannot apply credit to a %s invoice", status)
	}

	newCredited := amountCredited + amount
	if newCredited > total-amountPaid {
		return fmt.Errorf("credit (%d) exceeds remaining balance (%d) on invoice", amount, total-amountPaid)
	}

	newStatus := status
	if amountPaid+newCredited >= total {
		if amountPaid == 0 {
			newStatus = StatusCancelled
		} else {
			newStatus = StatusPaid
		}
	}

	_, err = tx.Exec(
		"UPDATE invoices SET amount_credited = ?, status = ?, updated_at = datetime('now') WHERE id = ?",
		newCredited, newStatus, invoiceID,
	)
	if err != nil {
		return fmt.Errorf("apply credit to invoice: %w", err)
	}
	return nil
}

// VoidCreditNote reverses an issued credit note. Posts the inverse journal
// (debit accounts receivable, credit revenue + tax) and rolls back the
// effect on the linked invoice.
func VoidCreditNote(db *sql.DB, id int, userID int) error {
	cn, err := GetCreditNote(db, id)
	if err != nil {
		return err
	}
	if cn.Status != StatusIssued {
		return fmt.Errorf("can only void issued credit notes (current: %s)", cn.Status)
	}

	var arAccountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = ?", AccountCodeAR).Scan(&arAccountID)
	if arAccountID == 0 {
		return fmt.Errorf("accounts receivable account not found")
	}

	je := &JournalEntry{
		EntryDate:   cn.CNDate,
		Description: fmt.Sprintf("Void Credit Note %s", cn.CNNumber),
		SourceType:  SourceCreditNote,
		IsPosted:    true,
		CreatedBy:   userID,
	}
	var lines []JournalLine
	// Debit accounts receivable — the credit is reversed, customer owes again.
	lines = append(lines, JournalLine{
		AccountID: arAccountID, Debit: cn.Total, Credit: 0, Memo: "Void " + cn.CNNumber,
	})
	for _, cnl := range cn.Lines {
		lines = append(lines, JournalLine{
			AccountID: cnl.AccountID, Debit: 0, Credit: cnl.Amount, Memo: cnl.Description,
		})
	}
	if cn.TaxAmount > 0 {
		var taxAccountID int
		db.QueryRow("SELECT id FROM accounts WHERE code = ?", AccountCodeTax).Scan(&taxAccountID)
		if taxAccountID > 0 {
			lines = append(lines, JournalLine{
				AccountID: taxAccountID, Debit: 0, Credit: cn.TaxAmount, Memo: "Tax",
			})
		}
	}

	if _, err := CreateJournalEntry(db, je, lines); err != nil {
		return fmt.Errorf("create void journal: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"UPDATE credit_notes SET status = ?, updated_at = datetime('now') WHERE id = ?",
		StatusVoid, id,
	); err != nil {
		return fmt.Errorf("update credit note: %w", err)
	}

	if cn.InvoiceID != nil {
		if err := unapplyCreditFromInvoice(tx, *cn.InvoiceID, cn.Total); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func unapplyCreditFromInvoice(tx *sql.Tx, invoiceID int, amount int) error {
	var total, amountPaid, amountCredited int
	err := tx.QueryRow(
		"SELECT total, amount_paid, amount_credited FROM invoices WHERE id = ?",
		invoiceID,
	).Scan(&total, &amountPaid, &amountCredited)
	if err != nil {
		return fmt.Errorf("read invoice for void: %w", err)
	}

	newCredited := amountCredited - amount
	if newCredited < 0 {
		newCredited = 0
	}

	// Recompute status from scratch.
	newStatus := StatusSent
	if amountPaid+newCredited >= total {
		if amountPaid == 0 {
			newStatus = StatusCancelled
		} else {
			newStatus = StatusPaid
		}
	} else if amountPaid > 0 {
		newStatus = StatusPartial
	}

	_, err = tx.Exec(
		"UPDATE invoices SET amount_credited = ?, status = ?, updated_at = datetime('now') WHERE id = ?",
		newCredited, newStatus, invoiceID,
	)
	if err != nil {
		return fmt.Errorf("unapply credit from invoice: %w", err)
	}
	return nil
}

func DeleteCreditNote(db *sql.DB, id int) error {
	var status string
	db.QueryRow("SELECT status FROM credit_notes WHERE id = ?", id).Scan(&status)
	if status != StatusDraft {
		return fmt.Errorf("can only delete draft credit notes (current: %s)", status)
	}
	_, err := db.Exec("DELETE FROM credit_notes WHERE id = ?", id)
	return err
}
