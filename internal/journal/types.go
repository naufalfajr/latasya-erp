package journal

import "github.com/naufal/latasya-erp/internal/model"

type Filter struct {
	DateFrom   string
	DateTo     string
	SourceType string
	Search     string
	Limit      int
	Offset     int
}

type Line struct {
	AccountID int
	Debit     int
	Credit    int
	Memo      string
}

type ManualDraft struct {
	EntryDate   string
	Description string
	Lines       []Line
}

type IncomeDraft struct {
	EntryDate      string
	Description    string
	Amount         int
	RevenueAccount int
	DepositAccount int
}

type ExpenseDraft struct {
	EntryDate      string
	Description    string
	Amount         int
	ExpenseAccount int
	PaymentAccount int
	VehicleID      int
}

type ListResult struct {
	Entries []model.JournalEntry
	Total   int
}

type FormOptions struct {
	Accounts []model.Account
	Vehicles []model.Vehicle
}
