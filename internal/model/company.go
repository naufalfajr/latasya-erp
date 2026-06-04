package model

import (
	"database/sql"
	"fmt"
)

type CompanyProfile struct {
	Name              string `json:"name"`
	Tagline           string `json:"tagline"`
	Address           string `json:"address"`
	Phone             string `json:"phone"`
	Email             string `json:"email"`
	NPWP              string `json:"npwp"`
	BankName          string `json:"bank_name"`
	BankAccountNumber string `json:"bank_account_number"`
	BankAccountHolder string `json:"bank_account_holder"`
	InvoiceFooter     string `json:"invoice_footer"`
	UpdatedAt         string `json:"updated_at"`
}

func GetCompanyProfile(db *sql.DB) (*CompanyProfile, error) {
	c := &CompanyProfile{}
	err := db.QueryRow(
		`SELECT name, tagline, address, phone, email, npwp,
			bank_name, bank_account_number, bank_account_holder, invoice_footer, updated_at
		 FROM company_profile WHERE id = 1`,
	).Scan(&c.Name, &c.Tagline, &c.Address, &c.Phone, &c.Email, &c.NPWP,
		&c.BankName, &c.BankAccountNumber, &c.BankAccountHolder, &c.InvoiceFooter, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get company profile: %w", err)
	}
	return c, nil
}

func UpdateCompanyProfile(db *sql.DB, c *CompanyProfile) error {
	_, err := db.Exec(
		`INSERT INTO company_profile
			(id, name, tagline, address, phone, email, npwp,
			 bank_name, bank_account_number, bank_account_holder, invoice_footer, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(id) DO UPDATE SET
			name = excluded.name, tagline = excluded.tagline, address = excluded.address,
			phone = excluded.phone, email = excluded.email, npwp = excluded.npwp,
			bank_name = excluded.bank_name, bank_account_number = excluded.bank_account_number,
			bank_account_holder = excluded.bank_account_holder, invoice_footer = excluded.invoice_footer,
			updated_at = datetime('now')`,
		c.Name, c.Tagline, c.Address, c.Phone, c.Email, c.NPWP,
		c.BankName, c.BankAccountNumber, c.BankAccountHolder, c.InvoiceFooter,
	)
	if err != nil {
		return fmt.Errorf("update company profile: %w", err)
	}
	return nil
}
