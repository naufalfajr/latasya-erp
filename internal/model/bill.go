package model

import (
	"database/sql"
	"fmt"
	"time"
)

type Bill struct {
	ID          int
	BillNumber  string
	ContactID   int
	BillDate    string
	DueDate     string
	Status      string
	Subtotal    int
	TaxAmount   int
	Total       int
	AmountPaid  int
	Notes       string
	JournalID   *int
	CreatedBy   int
	CreatedAt   string
	UpdatedAt   string
	ContactName string
	Lines       []BillLine
}

type BillLine struct {
	ID          int
	BillID      int
	Description string
	Quantity    int
	UnitPrice   int
	Amount      int
	AccountID   int
	AccountCode string
	AccountName string
}

type BillFilter struct {
	Status string
	Search string
}

func GenerateBillNumber(db *sql.DB) (string, error) {
	now := time.Now()
	prefix := fmt.Sprintf("BILL-%s", now.Format("200601"))
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM bills WHERE bill_number LIKE ?", prefix+"%").Scan(&count)
	if err != nil {
		return "", fmt.Errorf("count bills: %w", err)
	}
	return fmt.Sprintf("%s-%04d", prefix, count+1), nil
}

func CreateBill(db *sql.DB, b *Bill, lines []BillLine) (int, error) {
	if b.BillNumber == "" {
		num, err := GenerateBillNumber(db)
		if err != nil {
			return 0, err
		}
		b.BillNumber = num
	}

	var subtotal int
	for i := range lines {
		lines[i].Amount = lines[i].Quantity * lines[i].UnitPrice / 100
		subtotal += lines[i].Amount
	}
	b.Subtotal = subtotal
	b.Total = subtotal + b.TaxAmount

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO bills (bill_number, contact_id, bill_date, due_date, status, subtotal, tax_amount, total, amount_paid, notes, created_by)
		 VALUES (?, ?, ?, ?, 'draft', ?, ?, ?, 0, ?, ?)`,
		b.BillNumber, b.ContactID, b.BillDate, b.DueDate,
		b.Subtotal, b.TaxAmount, b.Total, b.Notes, b.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert bill: %w", err)
	}

	billID64, _ := result.LastInsertId()
	billID := int(billID64)

	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO bill_lines (bill_id, description, quantity, unit_price, amount, account_id) VALUES (?, ?, ?, ?, ?, ?)",
			billID, l.Description, l.Quantity, l.UnitPrice, l.Amount, l.AccountID,
		)
		if err != nil {
			return 0, fmt.Errorf("insert bill line: %w", err)
		}
	}

	return billID, tx.Commit()
}

func GetBill(db *sql.DB, id int) (*Bill, error) {
	b := &Bill{}
	err := db.QueryRow(
		`SELECT b.id, b.bill_number, b.contact_id, b.bill_date, b.due_date, b.status,
			b.subtotal, b.tax_amount, b.total, b.amount_paid, COALESCE(b.notes,''),
			b.journal_id, b.created_by, b.created_at, b.updated_at, c.name
		 FROM bills b
		 JOIN contacts c ON c.id = b.contact_id
		 WHERE b.id = ?`, id,
	).Scan(&b.ID, &b.BillNumber, &b.ContactID, &b.BillDate, &b.DueDate, &b.Status,
		&b.Subtotal, &b.TaxAmount, &b.Total, &b.AmountPaid, &b.Notes,
		&b.JournalID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.ContactName)
	if err != nil {
		return nil, fmt.Errorf("get bill: %w", err)
	}

	rows, err := db.Query(
		`SELECT bl.id, bl.bill_id, bl.description, bl.quantity, bl.unit_price, bl.amount, bl.account_id, a.code, a.name
		 FROM bill_lines bl JOIN accounts a ON a.id = bl.account_id WHERE bl.bill_id = ? ORDER BY bl.id`, id)
	if err != nil {
		return nil, fmt.Errorf("get bill lines: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var l BillLine
		rows.Scan(&l.ID, &l.BillID, &l.Description, &l.Quantity, &l.UnitPrice, &l.Amount, &l.AccountID, &l.AccountCode, &l.AccountName)
		b.Lines = append(b.Lines, l)
	}

	return b, nil
}

func ListBills(db *sql.DB, f BillFilter) ([]Bill, error) {
	query := `SELECT b.id, b.bill_number, b.contact_id, b.bill_date, b.due_date, b.status,
			b.subtotal, b.tax_amount, b.total, b.amount_paid, COALESCE(b.notes,''),
			b.journal_id, b.created_by, b.created_at, b.updated_at, c.name
		 FROM bills b JOIN contacts c ON c.id = b.contact_id WHERE 1=1`
	var args []any

	if f.Status != "" {
		query += " AND b.status = ?"
		args = append(args, f.Status)
	}
	if f.Search != "" {
		query += " AND (b.bill_number LIKE ? OR c.name LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s)
	}
	query += " ORDER BY b.bill_date DESC, b.id DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list bills: %w", err)
	}
	defer rows.Close()

	var bills []Bill
	for rows.Next() {
		var b Bill
		rows.Scan(&b.ID, &b.BillNumber, &b.ContactID, &b.BillDate, &b.DueDate, &b.Status,
			&b.Subtotal, &b.TaxAmount, &b.Total, &b.AmountPaid, &b.Notes,
			&b.JournalID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.ContactName)
		bills = append(bills, b)
	}
	return bills, nil
}

func UpdateBill(db *sql.DB, b *Bill, lines []BillLine) error {
	var status string
	db.QueryRow("SELECT status FROM bills WHERE id = ?", b.ID).Scan(&status)
	if status != "draft" {
		return fmt.Errorf("can only edit draft bills (current: %s)", status)
	}

	var subtotal int
	for i := range lines {
		lines[i].Amount = lines[i].Quantity * lines[i].UnitPrice / 100
		subtotal += lines[i].Amount
	}
	b.Subtotal = subtotal
	b.Total = subtotal + b.TaxAmount

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	tx.Exec(`UPDATE bills SET contact_id=?, bill_date=?, due_date=?, subtotal=?, tax_amount=?, total=?, notes=?, updated_at=datetime('now') WHERE id=?`,
		b.ContactID, b.BillDate, b.DueDate, b.Subtotal, b.TaxAmount, b.Total, b.Notes, b.ID)

	tx.Exec("DELETE FROM bill_lines WHERE bill_id = ?", b.ID)
	for _, l := range lines {
		tx.Exec("INSERT INTO bill_lines (bill_id, description, quantity, unit_price, amount, account_id) VALUES (?, ?, ?, ?, ?, ?)",
			b.ID, l.Description, l.Quantity, l.UnitPrice, l.Amount, l.AccountID)
	}

	return tx.Commit()
}

// ReceiveBill marks a bill as "received" and creates the AP journal entry:
// Debit: Expense accounts, Credit: Accounts Payable
func ReceiveBill(db *sql.DB, id int, userID int) error {
	b, err := GetBill(db, id)
	if err != nil {
		return err
	}
	if b.Status != "draft" {
		return fmt.Errorf("can only receive draft bills (current: %s)", b.Status)
	}

	var apAccountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '2-1001'").Scan(&apAccountID)
	if apAccountID == 0 {
		return fmt.Errorf("accounts payable account not found")
	}

	je := &JournalEntry{
		EntryDate:   b.BillDate,
		Description: fmt.Sprintf("Bill %s - %s", b.BillNumber, b.ContactName),
		SourceType:  "bill",
		IsPosted:    true,
		CreatedBy:   userID,
	}

	var lines []JournalLine
	// Debit each expense line
	for _, bl := range b.Lines {
		lines = append(lines, JournalLine{
			AccountID: bl.AccountID, Debit: bl.Amount, Credit: 0, Memo: bl.Description,
		})
	}
	// Debit tax if any
	if b.TaxAmount > 0 {
		var taxAccountID int
		db.QueryRow("SELECT id FROM accounts WHERE code = '2-1200'").Scan(&taxAccountID)
		if taxAccountID > 0 {
			lines = append(lines, JournalLine{
				AccountID: taxAccountID, Debit: b.TaxAmount, Credit: 0, Memo: "Tax",
			})
		}
	}
	// Credit AP for total
	lines = append(lines, JournalLine{
		AccountID: apAccountID, Debit: 0, Credit: b.Total, Memo: b.BillNumber,
	})

	journalID, err := CreateJournalEntry(db, je, lines)
	if err != nil {
		return fmt.Errorf("create journal entry: %w", err)
	}

	_, err = db.Exec("UPDATE bills SET status = 'received', journal_id = ?, updated_at = datetime('now') WHERE id = ?", journalID, id)
	return err
}

// RecordBillPayment records a payment against a bill
func RecordBillPayment(db *sql.DB, billID int, amount int, paymentDate string, paymentAccountID int, userID int) error {
	b, err := GetBill(db, billID)
	if err != nil {
		return err
	}
	if b.Status == "draft" || b.Status == "cancelled" || b.Status == "paid" {
		return fmt.Errorf("cannot record payment for %s bill", b.Status)
	}

	remaining := b.Total - b.AmountPaid
	if amount > remaining {
		return fmt.Errorf("payment amount (%d) exceeds remaining balance (%d)", amount, remaining)
	}

	var apAccountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '2-1001'").Scan(&apAccountID)

	// Debit AP (reduce liability), Credit Cash/Bank (reduce asset)
	je := &JournalEntry{
		EntryDate:   paymentDate,
		Description: fmt.Sprintf("Payment for %s", b.BillNumber),
		SourceType:  "bill",
		IsPosted:    true,
		CreatedBy:   userID,
	}
	jLines := []JournalLine{
		{AccountID: apAccountID, Debit: amount, Credit: 0, Memo: b.BillNumber},
		{AccountID: paymentAccountID, Debit: 0, Credit: amount, Memo: "Payment"},
	}

	journalID, err := CreateJournalEntry(db, je, jLines)
	if err != nil {
		return fmt.Errorf("create payment journal: %w", err)
	}

	db.Exec("INSERT INTO payments (payment_date, amount, payment_type, reference_id, payment_method, account_id, journal_id, created_by) VALUES (?, ?, 'bill', ?, 'bank_transfer', ?, ?, ?)",
		paymentDate, amount, billID, paymentAccountID, journalID, userID)

	newAmountPaid := b.AmountPaid + amount
	newStatus := "partial"
	if newAmountPaid >= b.Total {
		newStatus = "paid"
	}

	_, err = db.Exec("UPDATE bills SET amount_paid = ?, status = ?, updated_at = datetime('now') WHERE id = ?",
		newAmountPaid, newStatus, billID)
	return err
}

func DeleteBill(db *sql.DB, id int) error {
	var status string
	db.QueryRow("SELECT status FROM bills WHERE id = ?", id).Scan(&status)
	if status != "draft" {
		return fmt.Errorf("can only delete draft bills (current: %s)", status)
	}
	_, err := db.Exec("DELETE FROM bills WHERE id = ?", id)
	return err
}

func (b *Bill) AmountDue() int {
	return b.Total - b.AmountPaid
}
