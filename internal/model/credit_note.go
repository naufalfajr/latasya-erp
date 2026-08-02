package model

type CreditNote struct {
	ID            int              `json:"id"`
	CNNumber      string           `json:"cn_number"`
	ContactID     int              `json:"contact_id"`
	InvoiceID     *int             `json:"invoice_id,omitempty"`
	CNDate        string           `json:"cn_date"`
	Reason        string           `json:"reason"`
	Status        string           `json:"status"`
	Subtotal      int              `json:"subtotal"`
	TaxAmount     int              `json:"tax_amount"`
	Total         int              `json:"total"`
	Notes         string           `json:"notes"`
	JournalID     *int             `json:"journal_id,omitempty"`
	CreatedBy     int              `json:"created_by"`
	CreatedAt     string           `json:"created_at"`
	UpdatedAt     string           `json:"updated_at"`
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
	AccountCode  string `json:"account_code,omitempty"`
	AccountName  string `json:"account_name,omitempty"`
}
