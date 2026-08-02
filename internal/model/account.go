package model

type Account struct {
	ID            int    `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	AccountType   string `json:"account_type"`
	NormalBalance string `json:"normal_balance"`
	ParentID      *int   `json:"parent_id,omitempty"`
	IsSystem      bool   `json:"is_system"`
	IsActive      bool   `json:"is_active"`
	IsCash        bool   `json:"is_cash"`
	Description   string `json:"description"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func AccountTypeLabel(t string) string {
	switch t {
	case "asset":
		return "Asset"
	case "liability":
		return "Liability"
	case "equity":
		return "Equity"
	case "revenue":
		return "Revenue"
	case "expense":
		return "Expense"
	default:
		return t
	}
}
