package invoice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type DeletedInvoice struct {
	ID            int    `json:"id"`
	InvoiceNumber string `json:"invoice_number"`
}

type BulkDeleteResult struct {
	Deleted []DeletedInvoice `json:"deleted_invoices"`
	Skipped []int            `json:"skipped"`
}

func (m *Module) BulkDelete(ctx context.Context, actor Actor, ids []int) (*BulkDeleteResult, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, &ValidationError{Fields: map[string]string{"ids": "at least one id required"}}
	}
	result := &BulkDeleteResult{Deleted: []DeletedInvoice{}, Skipped: []int{}}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bulk delete: %w", err)
	}
	defer tx.Rollback()
	for _, id := range ids {
		var status, number string
		err := tx.QueryRowContext(ctx, "SELECT status, invoice_number FROM invoices WHERE id=?", id).Scan(&status, &number)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && status != model.StatusDraft) {
			result.Skipped = append(result.Skipped, id)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lookup invoice %d: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM invoices WHERE id=?", id); err != nil {
			return nil, fmt.Errorf("delete invoice %d: %w", id, err)
		}
		result.Deleted = append(result.Deleted, DeletedInvoice{ID: id, InvoiceNumber: number})
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bulk delete: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "invoice.bulk_delete", TargetType: "invoice", ActorID: int64(actor.UserID),
		Metadata: map[string]any{"deleted": result.Deleted, "skipped": result.Skipped}})
	return result, nil
}

type SentInvoice struct {
	ID            int    `json:"id"`
	InvoiceNumber string `json:"invoice_number"`
	JournalID     *int   `json:"journal_id,omitempty"`
}
type FailedInvoice struct {
	ID            int    `json:"id"`
	InvoiceNumber string `json:"invoice_number"`
	Error         string `json:"error"`
}
type BulkSendResult struct {
	Sent    []SentInvoice   `json:"sent"`
	Skipped []int           `json:"skipped"`
	Failed  []FailedInvoice `json:"failed"`
}

func (m *Module) BulkSend(ctx context.Context, actor Actor, ids []int) (*BulkSendResult, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, &ValidationError{Fields: map[string]string{"ids": "at least one id required"}}
	}
	m.bulkSendMu.Lock()
	defer m.bulkSendMu.Unlock()
	result := &BulkSendResult{Sent: []SentInvoice{}, Skipped: []int{}, Failed: []FailedInvoice{}}
	for _, id := range ids {
		var status, number string
		err := m.db.QueryRowContext(ctx, "SELECT status, invoice_number FROM invoices WHERE id=?", id).Scan(&status, &number)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && status != model.StatusDraft) {
			result.Skipped = append(result.Skipped, id)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lookup invoice %d: %w", id, err)
		}
		updated, err := m.send(ctx, actor, id, false)
		if err != nil {
			result.Failed = append(result.Failed, FailedInvoice{ID: id, InvoiceNumber: number, Error: err.Error()})
			continue
		}
		result.Sent = append(result.Sent, SentInvoice{ID: id, InvoiceNumber: updated.InvoiceNumber, JournalID: updated.JournalID})
	}
	audit.Log(ctx, m.db, audit.Event{Action: "invoice.bulk_send", TargetType: "invoice", ActorID: int64(actor.UserID),
		Metadata: map[string]any{"sent": result.Sent, "skipped": result.Skipped, "failed": result.Failed}})
	return result, nil
}

const (
	GeneratedCreated = "created"
	GeneratedSkipped = "skipped_already_invoiced"
	GeneratedFailed  = "failed"
)

type GeneratedInvoice struct {
	ContactID     int    `json:"contact_id"`
	ContactName   string `json:"contact_name"`
	InvoiceID     int    `json:"invoice_id,omitempty"`
	InvoiceNumber string `json:"invoice_number,omitempty"`
	Result        string `json:"result"`
	Error         string `json:"error,omitempty"`
}
type RecurringResult struct {
	Created           int                `json:"created"`
	Skipped           int                `json:"skipped"`
	Failed            int                `json:"failed"`
	EffectiveDays     int                `json:"effective_days"`
	MultiplierPercent int                `json:"multiplier_percent"`
	Items             []GeneratedInvoice `json:"items"`
}

func (r *RecurringResult) CreatedNumbers() []string {
	numbers := []string{}
	for _, item := range r.Items {
		if item.Result == GeneratedCreated {
			numbers = append(numbers, item.InvoiceNumber)
		}
	}
	return numbers
}

func (m *Module) GenerateRecurring(ctx context.Context, actor Actor, invoiceDate, dueDate string) (*RecurringResult, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if len(invoiceDate) < 7 {
		return nil, &ValidationError{Message: fmt.Sprintf("invalid invoice date: %q", invoiceDate)}
	}
	profile, err := model.GetCompanyProfileContext(ctx, m.db)
	if err != nil {
		return nil, fmt.Errorf("load company profile: %w", err)
	}
	if profile.DefaultRevenueAccountID == 0 {
		return nil, ErrNoDefaultRevenueAccount
	}
	var year, month int
	if n, _ := fmt.Sscanf(invoiceDate[:7], "%d-%d", &year, &month); n != 2 {
		return nil, &ValidationError{Message: fmt.Sprintf("invalid invoice date %q", invoiceDate)}
	}
	effectiveDays, err := model.EffectiveSchoolDaysContext(ctx, m.db, invoiceDate[:7])
	if err != nil {
		return nil, fmt.Errorf("calculate effective school days: %w", err)
	}
	multiplier := model.MonthlyPriceMultiplierPercent(effectiveDays)
	template := profile.RecurringDescriptionTemplate
	if template == "" {
		template = "Antar jemput {month} {year}"
	}
	description := strings.NewReplacer("{month}", model.MonthNameID(month), "{year}", strconv.Itoa(year)).Replace(template)

	active := true
	customers, err := model.ListContactsContext(ctx, m.db, model.ContactFilter{Type: "customer", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list active customers: %w", err)
	}
	result := &RecurringResult{EffectiveDays: effectiveDays, MultiplierPercent: multiplier, Items: []GeneratedInvoice{}}
	for _, contact := range customers {
		item := GeneratedInvoice{ContactID: contact.ID, ContactName: contact.Name}
		created, err := m.create(ctx, actor, Draft{ContactID: contact.ID, InvoiceDate: invoiceDate, DueDate: dueDate,
			Lines: []DraftLine{{Description: description, Quantity: 100, UnitPrice: model.ApplyMonthlyPriceMultiplier(contact.Price(), multiplier), AccountID: profile.DefaultRevenueAccountID}}}, false, invoiceDate[:7])
		if errors.Is(err, errRecurringAlreadyExists) {
			item.Result = GeneratedSkipped
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}
		if err != nil {
			item.Result, item.Error = GeneratedFailed, err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		item.InvoiceID, item.InvoiceNumber, item.Result = created.ID, created.InvoiceNumber, GeneratedCreated
		result.Created++
		result.Items = append(result.Items, item)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "invoice.generate_recurring", TargetType: "invoice", ActorID: int64(actor.UserID), Metadata: map[string]any{
		"invoice_date": invoiceDate, "due_date": dueDate, "effective_days": result.EffectiveDays, "multiplier_percent": result.MultiplierPercent,
		"created": result.Created, "skipped": result.Skipped, "failed": result.Failed, "created_invoices": result.CreatedNumbers()}})
	return result, nil
}
