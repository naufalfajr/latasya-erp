package model

type Bill struct {
	ID          int        `json:"id"`
	BillNumber  string     `json:"bill_number"`
	ContactID   int        `json:"contact_id"`
	BillDate    string     `json:"bill_date"`
	DueDate     string     `json:"due_date"`
	Status      string     `json:"status"`
	Subtotal    int        `json:"subtotal"`
	TaxAmount   int        `json:"tax_amount"`
	Total       int        `json:"total"`
	AmountPaid  int        `json:"amount_paid"`
	Notes       string     `json:"notes"`
	JournalID   *int       `json:"journal_id,omitempty"`
	CreatedBy   int        `json:"created_by"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
	ContactName string     `json:"contact_name,omitempty"`
	Lines       []BillLine `json:"lines,omitempty"`
}

type BillLine struct {
	ID          int    `json:"id"`
	BillID      int    `json:"bill_id"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	UnitPrice   int    `json:"unit_price"`
	Amount      int    `json:"amount"`
	AccountID   int    `json:"account_id"`
	AccountCode string `json:"account_code,omitempty"`
	AccountName string `json:"account_name,omitempty"`
}

func (b *Bill) AmountDue() int { return b.Total - b.AmountPaid }
