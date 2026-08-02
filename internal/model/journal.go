package model

import (
	"encoding/json"
	"strconv"
)

// JournalEntry is the shared persistence and presentation data shape. Journal
// business operations live in internal/journal.
type JournalEntry struct {
	ID             int           `json:"id"`
	EntryDate      string        `json:"entry_date"`
	Reference      string        `json:"reference"`
	Description    string        `json:"description"`
	SourceType     string        `json:"source_type"`
	SourceID       *int          `json:"source_id"`
	IsPosted       bool          `json:"is_posted"`
	VehicleID      int           `json:"vehicle_id,omitempty"`
	CreatedBy      int           `json:"created_by"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
	Lines          []JournalLine `json:"lines"`
	CreatedByName  string        `json:"created_by_name,omitempty"`
	TotalDebit     int           `json:"-"`
	TotalCredit    int           `json:"-"`
	AccountSummary string        `json:"account_summary,omitempty"`
	VehicleCode    string        `json:"vehicle_code,omitempty"`
}

func (j JournalEntry) MarshalJSON() ([]byte, error) {
	type alias JournalEntry
	return json.Marshal(struct {
		alias
		TotalDebit  string `json:"total_debit"`
		TotalCredit string `json:"total_credit"`
	}{alias: alias(j), TotalDebit: strconv.Itoa(j.TotalDebit), TotalCredit: strconv.Itoa(j.TotalCredit)})
}

type JournalLine struct {
	ID          int    `json:"id"`
	EntryID     int    `json:"entry_id"`
	AccountID   int    `json:"account_id"`
	Debit       int    `json:"-"`
	Credit      int    `json:"-"`
	Memo        string `json:"memo"`
	AccountCode string `json:"account_code,omitempty"`
	AccountName string `json:"account_name,omitempty"`
}

func (l JournalLine) MarshalJSON() ([]byte, error) {
	type alias JournalLine
	return json.Marshal(struct {
		alias
		Debit  string `json:"debit"`
		Credit string `json:"credit"`
	}{alias: alias(l), Debit: strconv.Itoa(l.Debit), Credit: strconv.Itoa(l.Credit)})
}
