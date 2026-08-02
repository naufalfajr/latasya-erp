package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/model"
)

func where(f Filter) (string, []any) {
	var clause string
	var args []any
	if f.DateFrom != "" {
		clause += " AND je.entry_date >= ?"
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		clause += " AND je.entry_date <= ?"
		args = append(args, f.DateTo)
	}
	if f.SourceType != "" {
		clause += " AND je.source_type = ?"
		args = append(args, f.SourceType)
	}
	if f.Search != "" {
		clause += " AND (je.reference LIKE ? OR je.description LIKE ?)"
		search := "%" + f.Search + "%"
		args = append(args, search, search)
	}
	return clause, args
}

func (m *Module) List(ctx context.Context, f Filter) (*ListResult, error) {
	clause, args := where(f)
	var total int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM journal_entries je JOIN users u ON u.id=je.created_by WHERE 1=1`+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count journal entries: %w", err)
	}

	accountExpr := "''"
	switch f.SourceType {
	case model.SourceIncome:
		accountExpr = `COALESCE((SELECT a.code || ' ' || a.name FROM journal_lines jl2 JOIN accounts a ON a.id=jl2.account_id WHERE jl2.entry_id=je.id AND jl2.credit>0 ORDER BY jl2.id LIMIT 1), '')`
	case model.SourceExpense:
		accountExpr = `COALESCE((SELECT a.code || ' ' || a.name FROM journal_lines jl2 JOIN accounts a ON a.id=jl2.account_id WHERE jl2.entry_id=je.id AND jl2.debit>0 ORDER BY jl2.id LIMIT 1), '')`
	}
	query := `SELECT je.id, je.entry_date, COALESCE(je.reference,''), je.description,
		COALESCE(je.source_type,''), je.source_id, je.is_posted, COALESCE(je.vehicle_id,0), je.created_by,
		je.created_at, je.updated_at, u.full_name,
		COALESCE((SELECT SUM(debit) FROM journal_lines WHERE entry_id=je.id),0),
		COALESCE((SELECT SUM(credit) FROM journal_lines WHERE entry_id=je.id),0), ` + accountExpr + `,
		COALESCE(v.code,'') FROM journal_entries je JOIN users u ON u.id=je.created_by
		LEFT JOIN vehicles v ON v.id=je.vehicle_id WHERE 1=1` + clause + ` ORDER BY je.entry_date DESC, je.id DESC`
	queryArgs := append([]any(nil), args...)
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		queryArgs = append(queryArgs, f.Limit, f.Offset)
	}
	rows, err := m.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("list journal entries: %w", err)
	}
	defer rows.Close()
	entries := make([]model.JournalEntry, 0)
	for rows.Next() {
		var entry model.JournalEntry
		if err := rows.Scan(&entry.ID, &entry.EntryDate, &entry.Reference, &entry.Description,
			&entry.SourceType, &entry.SourceID, &entry.IsPosted, &entry.VehicleID, &entry.CreatedBy,
			&entry.CreatedAt, &entry.UpdatedAt, &entry.CreatedByName, &entry.TotalDebit,
			&entry.TotalCredit, &entry.AccountSummary, &entry.VehicleCode); err != nil {
			return nil, fmt.Errorf("scan journal entry: %w", err)
		}
		entry.CreatedAt = instant(entry.CreatedAt)
		entry.UpdatedAt = instant(entry.UpdatedAt)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate journal entries: %w", err)
	}
	return &ListResult{Entries: entries, Total: total}, nil
}

func (m *Module) Get(ctx context.Context, id int) (*model.JournalEntry, error) {
	return getWith(ctx, m.db, id)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func getWith(ctx context.Context, db queryer, id int) (*model.JournalEntry, error) {
	entry := &model.JournalEntry{}
	err := db.QueryRowContext(ctx, `SELECT je.id, je.entry_date, COALESCE(je.reference,''), je.description,
		COALESCE(je.source_type,''), je.source_id, je.is_posted, COALESCE(je.vehicle_id,0), je.created_by,
		je.created_at, je.updated_at, u.full_name, COALESCE(v.code,'')
		FROM journal_entries je JOIN users u ON u.id=je.created_by LEFT JOIN vehicles v ON v.id=je.vehicle_id
		WHERE je.id=?`, id).Scan(&entry.ID, &entry.EntryDate, &entry.Reference, &entry.Description,
		&entry.SourceType, &entry.SourceID, &entry.IsPosted, &entry.VehicleID, &entry.CreatedBy,
		&entry.CreatedAt, &entry.UpdatedAt, &entry.CreatedByName, &entry.VehicleCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get journal entry: %w", err)
	}
	entry.CreatedAt = instant(entry.CreatedAt)
	entry.UpdatedAt = instant(entry.UpdatedAt)
	rows, err := db.QueryContext(ctx, `SELECT jl.id, jl.entry_id, jl.account_id, jl.debit, jl.credit, COALESCE(jl.memo,''), a.code, a.name
		FROM journal_lines jl JOIN accounts a ON a.id=jl.account_id WHERE jl.entry_id=? ORDER BY jl.id`, id)
	if err != nil {
		return nil, fmt.Errorf("get journal lines: %w", err)
	}
	defer rows.Close()
	entry.Lines = make([]model.JournalLine, 0)
	for rows.Next() {
		var line model.JournalLine
		if err := rows.Scan(&line.ID, &line.EntryID, &line.AccountID, &line.Debit, &line.Credit, &line.Memo, &line.AccountCode, &line.AccountName); err != nil {
			return nil, fmt.Errorf("scan journal line: %w", err)
		}
		entry.TotalDebit += line.Debit
		entry.TotalCredit += line.Credit
		entry.Lines = append(entry.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate journal lines: %w", err)
	}
	return entry, nil
}

func instant(value string) string {
	parsed, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return value
	}
	return parsed.UTC().Format(time.RFC3339)
}

func (m *Module) Options(ctx context.Context, accountType string, withVehicles bool) (*FormOptions, error) {
	active := true
	accounts, err := m.accounts.List(ctx, account.Filter{Type: accountType, IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list journal accounts: %w", err)
	}
	result := &FormOptions{Accounts: accounts.Accounts, Vehicles: make([]model.Vehicle, 0)}
	if !withVehicles {
		return result, nil
	}
	vehicleRows, err := m.db.QueryContext(ctx, `SELECT id, code, capacity, is_active FROM vehicles WHERE is_active=1 ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("list expense vehicles: %w", err)
	}
	defer vehicleRows.Close()
	for vehicleRows.Next() {
		var vehicle model.Vehicle
		if err := vehicleRows.Scan(&vehicle.ID, &vehicle.Code, &vehicle.Capacity, &vehicle.IsActive); err != nil {
			return nil, fmt.Errorf("scan expense vehicle: %w", err)
		}
		result.Vehicles = append(result.Vehicles, vehicle)
	}
	return result, vehicleRows.Err()
}
