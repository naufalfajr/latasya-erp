package model

// User roles
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// Account types
const (
	AccountTypeAsset     = "asset"
	AccountTypeLiability = "liability"
	AccountTypeEquity    = "equity"
	AccountTypeRevenue   = "revenue"
	AccountTypeExpense   = "expense"
)

// Journal entry source types
const (
	SourceManual  = "manual"
	SourceIncome  = "income"
	SourceExpense = "expense"
	SourceInvoice = "invoice"
	SourceBill    = "bill"
)

// Document statuses
const (
	StatusDraft     = "draft"
	StatusSent      = "sent"
	StatusReceived  = "received"
	StatusPaid      = "paid"
	StatusPartial   = "partial"
	StatusOverdue   = "overdue"
	StatusCancelled = "cancelled"
)

// Well-known account codes
const (
	AccountCodeAR = "1-1100" // Accounts Receivable
	AccountCodeAP = "2-1001" // Accounts Payable
	AccountCodeTax = "2-1200" // Tax Payable
)
