package model

type CompanyProfile struct {
	Name                         string `json:"name"`
	Tagline                      string `json:"tagline"`
	Address                      string `json:"address"`
	Phone                        string `json:"phone"`
	Email                        string `json:"email"`
	NPWP                         string `json:"npwp"`
	BankName                     string `json:"bank_name"`
	BankAccountNumber            string `json:"bank_account_number"`
	BankAccountHolder            string `json:"bank_account_holder"`
	InvoiceFooter                string `json:"invoice_footer"`
	DefaultRevenueAccountID      int    `json:"default_revenue_account_id"`
	RecurringDescriptionTemplate string `json:"recurring_description_template"`
	UpdatedAt                    string `json:"updated_at"`
}
