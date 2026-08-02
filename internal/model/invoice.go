package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type Invoice struct {
	ID             int    `json:"id"`
	InvoiceNumber  string `json:"invoice_number"`
	ContactID      int    `json:"contact_id"`
	InvoiceDate    string `json:"invoice_date"`
	DueDate        string `json:"due_date"`
	Status         string `json:"status"`
	Subtotal       int    `json:"-"`
	TaxAmount      int    `json:"-"`
	Total          int    `json:"-"`
	AmountPaid     int    `json:"-"`
	AmountCredited int    `json:"-"`
	Notes          string `json:"notes"`
	JournalID      *int   `json:"journal_id"`
	CreatedBy      int    `json:"created_by"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	// PaidDate is when the invoice was actually settled: the most recent
	// payment's payment_date (which staff can backdate, unlike updated_at),
	// or updated_at as a fallback for invoices settled purely by credit
	// note (no payments row). Only meaningful once Status == "paid";
	// populated by invoice list queries, left blank elsewhere.
	PaidDate    string        `json:"paid_date,omitempty"`
	ContactName string        `json:"contact_name,omitempty"`
	Lines       []InvoiceLine `json:"lines,omitempty"`
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
		CreatedAt      string `json:"created_at"`
		UpdatedAt      string `json:"updated_at"`
	}{
		alias:          alias(inv),
		Subtotal:       strconv.Itoa(inv.Subtotal),
		TaxAmount:      strconv.Itoa(inv.TaxAmount),
		Total:          strconv.Itoa(inv.Total),
		AmountPaid:     strconv.Itoa(inv.AmountPaid),
		AmountCredited: strconv.Itoa(inv.AmountCredited),
		AmountDue:      strconv.Itoa(inv.Total - inv.AmountPaid - inv.AmountCredited),
		CreatedAt:      invoiceInstant(inv.CreatedAt),
		UpdatedAt:      invoiceInstant(inv.UpdatedAt),
	})
}

func invoiceInstant(value string) string {
	parsed, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return value
	}
	return parsed.UTC().Format(time.RFC3339)
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

func (inv *Invoice) AmountDue() int {
	return inv.Total - inv.AmountPaid - inv.AmountCredited
}

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
