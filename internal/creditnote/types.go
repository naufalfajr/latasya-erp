package creditnote

import "github.com/naufal/latasya-erp/internal/model"

type Filter struct {
	Status, Search string
	Limit, Offset  int
}
type Draft struct {
	ContactID    int
	InvoiceID    *int
	Date, Reason string
	TaxAmount    int
	Notes        string
	Lines        []Line
}
type Line struct {
	Description                    string
	Quantity, UnitPrice, AccountID int
}
type ListResult struct {
	CreditNotes []model.CreditNote
	Total       int
}
type FormOptions struct {
	Contacts        []model.Contact
	RevenueAccounts []model.Account
}
