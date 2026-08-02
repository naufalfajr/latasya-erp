package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/documentnumber"
	"github.com/naufal/latasya-erp/internal/model"
)

const maxLinesPerEntry = 100

func validateManual(draft ManualDraft) error {
	fields := map[string]string{}
	if strings.TrimSpace(draft.EntryDate) == "" {
		fields["entry_date"] = "required"
	}
	if strings.TrimSpace(draft.Description) == "" {
		fields["description"] = "required"
	}
	if len(draft.Lines) < 2 {
		fields["lines"] = "at least two lines required"
	}
	if len(draft.Lines) > maxLinesPerEntry {
		fields["lines"] = "too many lines (max 100)"
	}
	var debit, credit int
	for i, line := range draft.Lines {
		prefix := "lines[" + strconv.Itoa(i) + "]"
		if line.AccountID <= 0 {
			fields[prefix+".account_id"] = "required"
		}
		if line.Debit < 0 {
			fields[prefix+".debit"] = "must be non-negative"
		}
		if line.Credit < 0 {
			fields[prefix+".credit"] = "must be non-negative"
		}
		if line.Debit == 0 && line.Credit == 0 {
			fields[prefix] = "debit or credit amount required"
		}
		if line.Debit > 0 && line.Credit > 0 {
			fields[prefix] = "cannot have both debit and credit"
		}
		debit += line.Debit
		credit += line.Credit
	}
	if debit == 0 || credit == 0 {
		fields["lines"] = "must have at least one debit and one credit line"
	} else if debit != credit {
		fields["balance"] = fmt.Sprintf("debits (%d) must equal credits (%d)", debit, credit)
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func validateIncome(draft IncomeDraft) error {
	fields := validateSimple(draft.EntryDate, draft.Description, draft.Amount)
	if draft.RevenueAccount <= 0 {
		fields["revenue_account"] = "required"
	}
	if draft.DepositAccount <= 0 {
		fields["deposit_account"] = "required"
	}
	if draft.RevenueAccount > 0 && draft.RevenueAccount == draft.DepositAccount {
		fields["accounts"] = "revenue and deposit accounts must be different"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func validateExpense(draft ExpenseDraft) error {
	fields := validateSimple(draft.EntryDate, draft.Description, draft.Amount)
	if draft.ExpenseAccount <= 0 {
		fields["expense_account"] = "required"
	}
	if draft.PaymentAccount <= 0 {
		fields["payment_account"] = "required"
	}
	if draft.ExpenseAccount > 0 && draft.ExpenseAccount == draft.PaymentAccount {
		fields["accounts"] = "expense and payment accounts must be different"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func validateSimple(date, description string, amount int) map[string]string {
	fields := map[string]string{}
	if strings.TrimSpace(date) == "" {
		fields["entry_date"] = "required"
	}
	if strings.TrimSpace(description) == "" {
		fields["description"] = "required"
	}
	if amount <= 0 {
		fields["amount"] = "must be greater than 0"
	}
	return fields
}

func (m *Module) CreateManual(ctx context.Context, actor Actor, draft ManualDraft) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageJournals); err != nil {
		return nil, err
	}
	if err := validateManual(draft); err != nil {
		return nil, err
	}
	return m.create(ctx, actor, model.SourceManual, draft.EntryDate, draft.Description, 0, draft.Lines)
}

func (m *Module) UpdateManual(ctx context.Context, actor Actor, id int, draft ManualDraft) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageJournals); err != nil {
		return nil, err
	}
	if err := validateManual(draft); err != nil {
		return nil, err
	}
	return m.update(ctx, actor, id, model.SourceManual, draft.EntryDate, draft.Description, 0, draft.Lines)
}

func (m *Module) DeleteManual(ctx context.Context, actor Actor, id int) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageJournals); err != nil {
		return nil, err
	}
	return m.delete(ctx, actor, id, model.SourceManual)
}

func (m *Module) CreateIncome(ctx context.Context, actor Actor, draft IncomeDraft) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageIncome); err != nil {
		return nil, err
	}
	if err := validateIncome(draft); err != nil {
		return nil, err
	}
	if err := m.requireAccountTypes(ctx, map[int]string{draft.RevenueAccount: model.AccountTypeRevenue, draft.DepositAccount: model.AccountTypeAsset}); err != nil {
		return nil, err
	}
	return m.create(ctx, actor, model.SourceIncome, draft.EntryDate, draft.Description, 0, []Line{
		{AccountID: draft.DepositAccount, Debit: draft.Amount},
		{AccountID: draft.RevenueAccount, Credit: draft.Amount},
	})
}

func (m *Module) UpdateIncome(ctx context.Context, actor Actor, id int, draft IncomeDraft) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageIncome); err != nil {
		return nil, err
	}
	if err := validateIncome(draft); err != nil {
		return nil, err
	}
	if err := m.requireAccountTypes(ctx, map[int]string{draft.RevenueAccount: model.AccountTypeRevenue, draft.DepositAccount: model.AccountTypeAsset}); err != nil {
		return nil, err
	}
	return m.update(ctx, actor, id, model.SourceIncome, draft.EntryDate, draft.Description, 0, []Line{
		{AccountID: draft.DepositAccount, Debit: draft.Amount},
		{AccountID: draft.RevenueAccount, Credit: draft.Amount},
	})
}

func (m *Module) DeleteIncome(ctx context.Context, actor Actor, id int) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageIncome); err != nil {
		return nil, err
	}
	return m.delete(ctx, actor, id, model.SourceIncome)
}

func (m *Module) CreateExpense(ctx context.Context, actor Actor, draft ExpenseDraft) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageExpenses); err != nil {
		return nil, err
	}
	if err := validateExpense(draft); err != nil {
		return nil, err
	}
	if err := m.requireAccountTypes(ctx, map[int]string{draft.ExpenseAccount: model.AccountTypeExpense, draft.PaymentAccount: model.AccountTypeAsset}); err != nil {
		return nil, err
	}
	return m.create(ctx, actor, model.SourceExpense, draft.EntryDate, draft.Description, draft.VehicleID, []Line{
		{AccountID: draft.ExpenseAccount, Debit: draft.Amount},
		{AccountID: draft.PaymentAccount, Credit: draft.Amount},
	})
}

func (m *Module) UpdateExpense(ctx context.Context, actor Actor, id int, draft ExpenseDraft) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageExpenses); err != nil {
		return nil, err
	}
	if err := validateExpense(draft); err != nil {
		return nil, err
	}
	if err := m.requireAccountTypes(ctx, map[int]string{draft.ExpenseAccount: model.AccountTypeExpense, draft.PaymentAccount: model.AccountTypeAsset}); err != nil {
		return nil, err
	}
	return m.update(ctx, actor, id, model.SourceExpense, draft.EntryDate, draft.Description, draft.VehicleID, []Line{
		{AccountID: draft.ExpenseAccount, Debit: draft.Amount},
		{AccountID: draft.PaymentAccount, Credit: draft.Amount},
	})
}

func (m *Module) DeleteExpense(ctx context.Context, actor Actor, id int) (*model.JournalEntry, error) {
	if err := require(actor, actor.CanManageExpenses); err != nil {
		return nil, err
	}
	return m.delete(ctx, actor, id, model.SourceExpense)
}

func (m *Module) create(ctx context.Context, actor Actor, source, date, description string, vehicleID int, lines []Line) (*model.JournalEntry, error) {
	if err := validateManual(ManualDraft{EntryDate: date, Description: description, Lines: lines}); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin journal create: %w", err)
	}
	defer tx.Rollback()
	reference, err := documentnumber.Next(ctx, tx, documentnumber.JournalEntry)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO journal_entries
		(entry_date, reference, description, source_type, is_posted, vehicle_id, created_by)
		VALUES (?, ?, ?, ?, 1, NULLIF(?,0), ?)`, date, reference, description, source, vehicleID, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("insert journal entry: %w", err)
	}
	id64, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("journal entry id: %w", err)
	}
	id := int(id64)
	if err := insertLines(ctx, tx, id, lines); err != nil {
		return nil, err
	}
	created, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit journal create: %w", err)
	}
	m.auditCreate(ctx, actor, source, created)
	return created, nil
}

func (m *Module) update(ctx context.Context, actor Actor, id int, source, date, description string, vehicleID int, lines []Line) (*model.JournalEntry, error) {
	if err := validateManual(ManualDraft{EntryDate: date, Description: description, Lines: lines}); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin journal update: %w", err)
	}
	defer tx.Rollback()
	if err := lockSource(ctx, tx, id, source); err != nil {
		return nil, err
	}
	existing, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE journal_entries SET entry_date=?, description=?, vehicle_id=NULLIF(?,0), updated_at=datetime('now')
		WHERE id=? AND COALESCE(NULLIF(source_type,''),'manual')=?`, date, description, vehicleID, id, source)
	if err != nil {
		return nil, fmt.Errorf("update journal entry: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("updated journal rows: %w", err)
	}
	if changed == 0 {
		return nil, &ConflictError{Message: "journal entry source changed"}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM journal_lines WHERE entry_id=?", id); err != nil {
		return nil, fmt.Errorf("delete old journal lines: %w", err)
	}
	if err := insertLines(ctx, tx, id, lines); err != nil {
		return nil, err
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit journal update: %w", err)
	}
	m.auditUpdate(ctx, actor, source, existing, updated)
	return updated, nil
}

func (m *Module) delete(ctx context.Context, actor Actor, id int, source string) (*model.JournalEntry, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin journal delete: %w", err)
	}
	defer tx.Rollback()
	if err := lockSource(ctx, tx, id, source); err != nil {
		return nil, err
	}
	existing, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM journal_entries WHERE id=? AND COALESCE(NULLIF(source_type,''),'manual')=?`, id, source)
	if err != nil {
		return nil, fmt.Errorf("delete journal entry: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("deleted journal rows: %w", err)
	}
	if changed == 0 {
		return nil, &ConflictError{Message: "journal entry source changed"}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit journal delete: %w", err)
	}
	m.auditDelete(ctx, actor, source, existing)
	return existing, nil
}

func lockSource(ctx context.Context, tx *sql.Tx, id int, source string) error {
	result, err := tx.ExecContext(ctx, `UPDATE journal_entries SET id=id WHERE id=? AND COALESCE(NULLIF(source_type,''),'manual')=?`, id, source)
	if err != nil {
		return fmt.Errorf("lock journal entry: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("locked journal rows: %w", err)
	}
	if changed > 0 {
		return nil
	}
	var actual string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(source_type,''),'manual') FROM journal_entries WHERE id=?`, id).Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check journal source: %w", err)
	}
	return &ConflictError{Message: "cannot mutate journal entry from source " + actual}
}

func normalizedSource(source string) string {
	if source == "" {
		return model.SourceManual
	}
	return source
}

func insertLines(ctx context.Context, tx *sql.Tx, id int, lines []Line) error {
	for _, line := range lines {
		if _, err := tx.ExecContext(ctx, `INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo) VALUES (?, ?, ?, ?, ?)`,
			id, line.AccountID, line.Debit, line.Credit, line.Memo); err != nil {
			return fmt.Errorf("insert journal line: %w", err)
		}
	}
	return nil
}

func (m *Module) requireAccountTypes(ctx context.Context, expected map[int]string) error {
	fields := map[string]string{}
	for id, accountType := range expected {
		var actual string
		err := m.db.QueryRowContext(ctx, "SELECT account_type FROM accounts WHERE id=? AND is_active=1", id).Scan(&actual)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && actual != accountType) {
			fields["accounts"] = "accounts must be active and use the required account types"
			continue
		}
		if err != nil {
			return fmt.Errorf("validate account: %w", err)
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func total(lines []model.JournalLine) int {
	var amount int
	for _, line := range lines {
		amount += line.Debit
	}
	return amount
}

func shape(entry *model.JournalEntry) map[string]any {
	data := map[string]any{"entry_date": entry.EntryDate, "description": entry.Description}
	if entry.SourceType == model.SourceIncome || entry.SourceType == model.SourceExpense {
		data["amount"] = total(entry.Lines)
	} else {
		data["total"] = total(entry.Lines)
	}
	if entry.VehicleID != 0 {
		data["vehicle_id"] = entry.VehicleID
	}
	if entry.SourceType == model.SourceIncome && len(entry.Lines) >= 2 {
		data["deposit_account"] = entry.Lines[0].AccountID
		data["revenue_account"] = entry.Lines[1].AccountID
	}
	if entry.SourceType == model.SourceExpense && len(entry.Lines) >= 2 {
		data["expense_account"] = entry.Lines[0].AccountID
		data["payment_account"] = entry.Lines[1].AccountID
	}
	return data
}

func target(source string) string {
	if source == model.SourceManual {
		return "journal"
	}
	return source
}

func (m *Module) auditCreate(ctx context.Context, actor Actor, source string, entry *model.JournalEntry) {
	audit.Log(ctx, m.db, audit.Event{Action: target(source) + ".create", TargetType: target(source), TargetID: int64(entry.ID),
		TargetLabel: entry.Description, ActorID: int64(actor.UserID), Metadata: map[string]any{"after": shape(entry)}})
}

func (m *Module) auditUpdate(ctx context.Context, actor Actor, source string, before, after *model.JournalEntry) {
	metadata := audit.Diff(shape(before), shape(after), []string{"entry_date", "description", "total", "amount", "deposit_account", "revenue_account", "expense_account", "payment_account", "vehicle_id"})
	if metadata != nil {
		audit.Log(ctx, m.db, audit.Event{Action: target(source) + ".update", TargetType: target(source), TargetID: int64(after.ID),
			TargetLabel: after.Description, ActorID: int64(actor.UserID), Metadata: metadata})
	}
}

func (m *Module) auditDelete(ctx context.Context, actor Actor, source string, entry *model.JournalEntry) {
	audit.Log(ctx, m.db, audit.Event{Action: target(source) + ".delete", TargetType: target(source), TargetID: int64(entry.ID),
		TargetLabel: entry.Description, ActorID: int64(actor.UserID), Metadata: map[string]any{"before": shape(entry)}})
}
