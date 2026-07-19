package model

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
)

// GetOrCreatePortalToken returns the contact's parent-portal access token,
// generating and persisting one on first use.
func GetOrCreatePortalToken(db *sql.DB, contactID int) (string, error) {
	var token sql.NullString
	err := db.QueryRow("SELECT portal_token FROM contacts WHERE id = ?", contactID).Scan(&token)
	if err != nil {
		return "", fmt.Errorf("get portal token: %w", err)
	}
	if token.Valid && token.String != "" {
		return token.String, nil
	}
	return RegeneratePortalToken(db, contactID)
}

// RegeneratePortalToken assigns a fresh unguessable token to the contact,
// invalidating any link previously issued for it.
func RegeneratePortalToken(db *sql.DB, contactID int) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate portal token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	if _, err := db.Exec("UPDATE contacts SET portal_token = ? WHERE id = ?", token, contactID); err != nil {
		return "", fmt.Errorf("save portal token: %w", err)
	}
	return token, nil
}

// PortalFamily is the set of contacts reachable from one parent portal
// token: the token's own contact plus any siblings sharing its phone number.
type PortalFamily struct {
	Contacts []Contact
}

// ContactIDs returns the family's contact IDs.
func (f *PortalFamily) ContactIDs() []int {
	ids := make([]int, len(f.Contacts))
	for i, c := range f.Contacts {
		ids[i] = c.ID
	}
	return ids
}

// Has reports whether contactID belongs to this family.
func (f *PortalFamily) Has(contactID int) bool {
	for _, c := range f.Contacts {
		if c.ID == contactID {
			return true
		}
	}
	return false
}

// ContactsByPortalToken resolves a token to its family of contacts: the
// token's own contact plus any other contact whose phone number normalizes
// to the same digits (so "0812..." and "+62 812-..." group together). A
// blank phone number never groups with other contacts (each is its own
// family), so a shared blank phone can't leak one family's invoices to
// another. Returns (nil, nil) if the token doesn't match any contact.
func ContactsByPortalToken(db *sql.DB, token string) (*PortalFamily, error) {
	if token == "" {
		return nil, nil
	}

	var origin Contact
	err := db.QueryRow(
		"SELECT id, name, COALESCE(phone,'') FROM contacts WHERE portal_token = ?", token,
	).Scan(&origin.ID, &origin.Name, &origin.Phone)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup portal token: %w", err)
	}

	if origin.Phone == "" {
		return &PortalFamily{Contacts: []Contact{origin}}, nil
	}
	originDigits := NormalizePhoneID(origin.Phone)

	// ponytail: normalization can't be expressed in the WHERE clause, so this
	// scans every contact with a phone on file and filters in Go. Fine at
	// this business's contact-book scale; move to a stored normalized-phone
	// column if that ever changes.
	rows, err := db.Query("SELECT id, name, phone FROM contacts WHERE phone IS NOT NULL AND phone <> '' ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list family contacts: %w", err)
	}
	defer rows.Close()

	var family []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone); err != nil {
			return nil, fmt.Errorf("scan family contact: %w", err)
		}
		if NormalizePhoneID(c.Phone) == originDigits {
			family = append(family, Contact{ID: c.ID, Name: c.Name})
		}
	}
	return &PortalFamily{Contacts: family}, nil
}

// ListPortalInvoices returns non-draft invoices for the given contacts,
// newest first. Drafts are excluded: they aren't finalized yet and
// shouldn't be shown to a parent as something owed.
func ListPortalInvoices(db *sql.DB, contactIDs []int) ([]Invoice, error) {
	if len(contactIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `SELECT id, invoice_number, contact_id, invoice_date, due_date, status,
			subtotal, tax_amount, total, amount_paid, amount_credited, COALESCE(notes,''),
			COALESCE((SELECT MAX(payment_date) FROM payments WHERE payment_type = 'invoice' AND reference_id = invoices.id), updated_at)
		FROM invoices
		WHERE contact_id IN (` + strings.Join(placeholders, ",") + `) AND status != 'draft'
		ORDER BY invoice_date DESC, id DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list portal invoices: %w", err)
	}
	defer rows.Close()

	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		err := rows.Scan(&inv.ID, &inv.InvoiceNumber, &inv.ContactID, &inv.InvoiceDate, &inv.DueDate, &inv.Status,
			&inv.Subtotal, &inv.TaxAmount, &inv.Total, &inv.AmountPaid, &inv.AmountCredited, &inv.Notes, &inv.PaidDate)
		if err != nil {
			return nil, fmt.Errorf("scan portal invoice: %w", err)
		}
		invoices = append(invoices, inv)
	}
	return invoices, nil
}
