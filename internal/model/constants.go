package model

// User roles
const (
	RoleAdmin      = "admin"
	RoleBookkeeper = "bookkeeper"
	RoleViewer     = "viewer"
)

// Capabilities. Admin role implicitly holds every capability.
const (
	CapAccountsManage = "accounts.manage"
	CapUsersManage    = "users.manage"
	CapRolesManage    = "roles.manage"
	CapContactsManage = "contacts.manage"
	CapJournalsManage = "journals.manage"
	CapIncomeManage   = "income.manage"
	CapExpensesManage = "expenses.manage"
	CapInvoicesManage = "invoices.manage"
	CapBillsManage    = "bills.manage"
	CapReportsView    = "reports.view"
	CapAuditView      = "audit.view"
)

// AllCapabilities lists every capability the system knows about. Used to
// render the checkbox grid in the role form and to grant everything to admin.
var AllCapabilities = []string{
	CapAccountsManage,
	CapUsersManage,
	CapRolesManage,
	CapContactsManage,
	CapJournalsManage,
	CapIncomeManage,
	CapExpensesManage,
	CapInvoicesManage,
	CapBillsManage,
	CapReportsView,
	CapAuditView,
}

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
	AccountCodeAR  = "1-1100" // Accounts Receivable
	AccountCodeAP  = "2-1001" // Accounts Payable
	AccountCodeTax = "2-1200" // Tax Payable
)
