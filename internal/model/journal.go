package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
)

type JournalEntry struct {
	ID          int    `json:"id"`
	EntryDate   string `json:"entry_date"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
	SourceType  string `json:"source_type"`
	SourceID    *int   `json:"source_id"`
	IsPosted    bool   `json:"is_posted"`
	CreatedBy   int    `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	// Joined fields
	Lines         []JournalLine `json:"lines"`
	CreatedByName string        `json:"created_by_name,omitempty"`
	TotalDebit    int           `json:"-"`
	TotalCredit   int           `json:"-"`
	// AccountSummary is the category account ("code name") shown on the
	// income/expense list pages. Empty for callers that don't request it.
	AccountSummary string `json:"account_summary,omitempty"`
}

// MarshalJSON serializes IDR-valued totals as strings to match the API
// contract (currency is always a string of integer IDR).
func (j JournalEntry) MarshalJSON() ([]byte, error) {
	type alias JournalEntry
	return json.Marshal(struct {
		alias
		TotalDebit  string `json:"total_debit"`
		TotalCredit string `json:"total_credit"`
	}{
		alias:       alias(j),
		TotalDebit:  strconv.Itoa(j.TotalDebit),
		TotalCredit: strconv.Itoa(j.TotalCredit),
	})
}

type JournalLine struct {
	ID        int    `json:"id"`
	EntryID   int    `json:"entry_id"`
	AccountID int    `json:"account_id"`
	Debit     int    `json:"-"`
	Credit    int    `json:"-"`
	Memo      string `json:"memo"`
	// Joined fields
	AccountCode string `json:"account_code,omitempty"`
	AccountName string `json:"account_name,omitempty"`
}

// MarshalJSON serializes Debit/Credit as integer-string IDR amounts.
func (l JournalLine) MarshalJSON() ([]byte, error) {
	type alias JournalLine
	return json.Marshal(struct {
		alias
		Debit  string `json:"debit"`
		Credit string `json:"credit"`
	}{
		alias:  alias(l),
		Debit:  strconv.Itoa(l.Debit),
		Credit: strconv.Itoa(l.Credit),
	})
}

type JournalFilter struct {
	DateFrom   string
	DateTo     string
	SourceType string
	Search     string
	Limit      int // 0 = no limit (return all)
	Offset     int
}

// journalWhere builds the shared WHERE fragment (and its args) used by both
// ListJournalEntries and CountJournalEntries so the two never drift.
func journalWhere(f JournalFilter) (string, []any) {
	var where string
	var args []any
	if f.DateFrom != "" {
		where += " AND je.entry_date >= ?"
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		where += " AND je.entry_date <= ?"
		args = append(args, f.DateTo)
	}
	if f.SourceType != "" {
		where += " AND je.source_type = ?"
		args = append(args, f.SourceType)
	}
	if f.Search != "" {
		where += " AND (je.reference LIKE ? OR je.description LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s)
	}
	return where, args
}

// CountJournalEntries returns the total entries matching the filter, ignoring
// Limit/Offset — used for pagination page counts.
func CountJournalEntries(db *sql.DB, f JournalFilter) (int, error) {
	where, args := journalWhere(f)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM journal_entries je WHERE 1=1`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count journal entries: %w", err)
	}
	return n, nil
}

// GenerateReference creates a reference like JE-202604-0001
func GenerateReference(db *sql.DB) (string, error) {
	return GenerateDocNumber(db, "journal_entries", "reference", "JE")
}

func CreateJournalEntry(db *sql.DB, je *JournalEntry, lines []JournalLine) (int, error) {
	// Validate debit = credit
	var totalDebit, totalCredit int
	for _, l := range lines {
		totalDebit += l.Debit
		totalCredit += l.Credit
	}
	if totalDebit != totalCredit {
		return 0, fmt.Errorf("debits (%d) must equal credits (%d)", totalDebit, totalCredit)
	}
	if totalDebit == 0 {
		return 0, fmt.Errorf("journal entry must have at least one debit and credit line")
	}

	// Generate reference before starting transaction (avoids deadlock with single connection)
	if je.Reference == "" {
		ref, err := GenerateReference(db)
		if err != nil {
			return 0, err
		}
		je.Reference = ref
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO journal_entries (entry_date, reference, description, source_type, source_id, is_posted, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		je.EntryDate, je.Reference, je.Description, je.SourceType, je.SourceID, je.IsPosted, je.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert journal entry: %w", err)
	}

	entryID64, _ := result.LastInsertId()
	entryID := int(entryID64)

	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo) VALUES (?, ?, ?, ?, ?)",
			entryID, l.AccountID, l.Debit, l.Credit, l.Memo,
		)
		if err != nil {
			return 0, fmt.Errorf("insert journal line: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return entryID, nil
}

func GetJournalEntry(db *sql.DB, id int) (*JournalEntry, error) {
	je := &JournalEntry{}
	err := db.QueryRow(
		`SELECT je.id, je.entry_date, COALESCE(je.reference,''), je.description,
			COALESCE(je.source_type,''), je.source_id, je.is_posted, je.created_by,
			je.created_at, je.updated_at, u.full_name
		 FROM journal_entries je
		 JOIN users u ON u.id = je.created_by
		 WHERE je.id = ?`, id,
	).Scan(&je.ID, &je.EntryDate, &je.Reference, &je.Description,
		&je.SourceType, &je.SourceID, &je.IsPosted, &je.CreatedBy,
		&je.CreatedAt, &je.UpdatedAt, &je.CreatedByName)
	if err != nil {
		return nil, fmt.Errorf("get journal entry: %w", err)
	}

	lines, err := getJournalLines(db, id)
	if err != nil {
		return nil, err
	}
	je.Lines = lines

	for _, l := range lines {
		je.TotalDebit += l.Debit
		je.TotalCredit += l.Credit
	}

	return je, nil
}

func getJournalLines(db *sql.DB, entryID int) ([]JournalLine, error) {
	rows, err := db.Query(
		`SELECT jl.id, jl.entry_id, jl.account_id, jl.debit, jl.credit, COALESCE(jl.memo,''),
			a.code, a.name
		 FROM journal_lines jl
		 JOIN accounts a ON a.id = jl.account_id
		 WHERE jl.entry_id = ?
		 ORDER BY jl.id`, entryID,
	)
	if err != nil {
		return nil, fmt.Errorf("get journal lines: %w", err)
	}
	defer rows.Close()

	var lines []JournalLine
	for rows.Next() {
		var l JournalLine
		err := rows.Scan(&l.ID, &l.EntryID, &l.AccountID, &l.Debit, &l.Credit, &l.Memo,
			&l.AccountCode, &l.AccountName)
		if err != nil {
			return nil, fmt.Errorf("scan journal line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, nil
}

func ListJournalEntries(db *sql.DB, f JournalFilter) ([]JournalEntry, error) {
	// Category account shown on the income/expense list pages: income entries
	// credit a revenue account, expense entries debit an expense account. Other
	// callers (journals page, etc.) don't display it, so leave it empty.
	accountExpr := "''"
	switch f.SourceType {
	case SourceIncome:
		accountExpr = `COALESCE((SELECT a.code || ' ' || a.name
			FROM journal_lines jl2 JOIN accounts a ON a.id = jl2.account_id
			WHERE jl2.entry_id = je.id AND jl2.credit > 0
			ORDER BY jl2.id LIMIT 1), '')`
	case SourceExpense:
		accountExpr = `COALESCE((SELECT a.code || ' ' || a.name
			FROM journal_lines jl2 JOIN accounts a ON a.id = jl2.account_id
			WHERE jl2.entry_id = je.id AND jl2.debit > 0
			ORDER BY jl2.id LIMIT 1), '')`
	}

	where, args := journalWhere(f)
	query := `SELECT je.id, je.entry_date, COALESCE(je.reference,''), je.description,
			COALESCE(je.source_type,''), je.source_id, je.is_posted, je.created_by,
			je.created_at, je.updated_at, u.full_name,
			COALESCE((SELECT SUM(debit) FROM journal_lines WHERE entry_id = je.id), 0),
			COALESCE((SELECT SUM(credit) FROM journal_lines WHERE entry_id = je.id), 0),
			` + accountExpr + `
		 FROM journal_entries je
		 JOIN users u ON u.id = je.created_by
		 WHERE 1=1` + where + ` ORDER BY je.entry_date DESC, je.id DESC`
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list journal entries: %w", err)
	}
	defer rows.Close()

	var entries []JournalEntry
	for rows.Next() {
		var je JournalEntry
		err := rows.Scan(&je.ID, &je.EntryDate, &je.Reference, &je.Description,
			&je.SourceType, &je.SourceID, &je.IsPosted, &je.CreatedBy,
			&je.CreatedAt, &je.UpdatedAt, &je.CreatedByName,
			&je.TotalDebit, &je.TotalCredit, &je.AccountSummary)
		if err != nil {
			return nil, fmt.Errorf("scan journal entry: %w", err)
		}
		entries = append(entries, je)
	}
	return entries, nil
}

func UpdateJournalEntry(db *sql.DB, je *JournalEntry, lines []JournalLine) error {
	// Validate debit = credit
	var totalDebit, totalCredit int
	for _, l := range lines {
		totalDebit += l.Debit
		totalCredit += l.Credit
	}
	if totalDebit != totalCredit {
		return fmt.Errorf("debits (%d) must equal credits (%d)", totalDebit, totalCredit)
	}
	if totalDebit == 0 {
		return fmt.Errorf("journal entry must have at least one debit and credit line")
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`UPDATE journal_entries SET entry_date=?, description=?, updated_at=datetime('now') WHERE id=?`,
		je.EntryDate, je.Description, je.ID,
	)
	if err != nil {
		return fmt.Errorf("update journal entry: %w", err)
	}

	// Delete old lines and insert new ones
	_, err = tx.Exec("DELETE FROM journal_lines WHERE entry_id = ?", je.ID)
	if err != nil {
		return fmt.Errorf("delete old lines: %w", err)
	}

	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo) VALUES (?, ?, ?, ?, ?)",
			je.ID, l.AccountID, l.Debit, l.Credit, l.Memo,
		)
		if err != nil {
			return fmt.Errorf("insert journal line: %w", err)
		}
	}

	return tx.Commit()
}

func DeleteJournalEntry(db *sql.DB, id int) error {
	// Only allow deleting manual entries
	var sourceType sql.NullString
	err := db.QueryRow("SELECT source_type FROM journal_entries WHERE id = ?", id).Scan(&sourceType)
	if err != nil {
		return fmt.Errorf("get journal entry: %w", err)
	}
	if sourceType.Valid && sourceType.String != "manual" && sourceType.String != "" {
		return fmt.Errorf("cannot delete auto-generated journal entry (source: %s)", sourceType.String)
	}

	_, err = db.Exec("DELETE FROM journal_entries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete journal entry: %w", err)
	}
	return nil
}

// DeleteJournalEntryBySource deletes a journal entry with the given source type.
// This is used by income/expense delete to bypass the auto-generated check.
func DeleteJournalEntryBySource(db *sql.DB, id int, sourceType string) error {
	var actualSource sql.NullString
	err := db.QueryRow("SELECT source_type FROM journal_entries WHERE id = ?", id).Scan(&actualSource)
	if err != nil {
		return fmt.Errorf("get journal entry: %w", err)
	}
	if !actualSource.Valid || actualSource.String != sourceType {
		return fmt.Errorf("journal entry %d is not a %s entry", id, sourceType)
	}
	_, err = db.Exec("DELETE FROM journal_entries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete journal entry: %w", err)
	}
	return nil
}
