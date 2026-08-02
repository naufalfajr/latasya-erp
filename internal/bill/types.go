package bill

import "github.com/naufal/latasya-erp/internal/model"

type Filter struct {
	Status string
	Search string
	Limit  int
	Offset int
}

type Draft struct {
	ContactID int
	BillDate  string
	DueDate   string
	TaxAmount int
	Notes     string
	Lines     []Line
}

type Line struct {
	Description string
	Quantity    int
	UnitPrice   int
	AccountID   int
}

type Payment struct {
	Amount         int
	PaymentDate    string
	PaymentAccount int
}

type ListResult struct {
	Bills []model.Bill
	Total int
}

type FormOptions struct {
	Contacts        []model.Contact
	ExpenseAccounts []model.Account
	AssetAccounts   []model.Account
}
