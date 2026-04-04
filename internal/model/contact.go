package model

import (
	"database/sql"
	"fmt"
)

type Contact struct {
	ID          int
	Name        string
	ContactType string
	Phone       string
	Email       string
	Address     string
	Notes       string
	IsActive    bool
	CreatedAt   string
	UpdatedAt   string
}

type ContactFilter struct {
	Type     string // customer, supplier, both
	IsActive *bool
	Search   string
}

func ListContacts(db *sql.DB, f ContactFilter) ([]Contact, error) {
	query := `SELECT id, name, contact_type, COALESCE(phone,''), COALESCE(email,''),
		COALESCE(address,''), COALESCE(notes,''), is_active, created_at, updated_at
		FROM contacts WHERE 1=1`
	var args []any

	if f.Type != "" {
		query += " AND (contact_type = ? OR contact_type = 'both')"
		args = append(args, f.Type)
	}
	if f.IsActive != nil {
		query += " AND is_active = ?"
		if *f.IsActive {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if f.Search != "" {
		query += " AND (name LIKE ? OR phone LIKE ? OR email LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s, s)
	}
	query += " ORDER BY name"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		err := rows.Scan(&c.ID, &c.Name, &c.ContactType, &c.Phone, &c.Email,
			&c.Address, &c.Notes, &c.IsActive, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	return contacts, nil
}

func GetContact(db *sql.DB, id int) (*Contact, error) {
	c := &Contact{}
	err := db.QueryRow(
		`SELECT id, name, contact_type, COALESCE(phone,''), COALESCE(email,''),
		COALESCE(address,''), COALESCE(notes,''), is_active, created_at, updated_at
		FROM contacts WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.ContactType, &c.Phone, &c.Email,
		&c.Address, &c.Notes, &c.IsActive, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get contact: %w", err)
	}
	return c, nil
}

func CreateContact(db *sql.DB, c *Contact) error {
	_, err := db.Exec(
		"INSERT INTO contacts (name, contact_type, phone, email, address, notes, is_active) VALUES (?, ?, ?, ?, ?, ?, ?)",
		c.Name, c.ContactType, c.Phone, c.Email, c.Address, c.Notes, c.IsActive,
	)
	if err != nil {
		return fmt.Errorf("create contact: %w", err)
	}
	return nil
}

func UpdateContact(db *sql.DB, c *Contact) error {
	_, err := db.Exec(
		"UPDATE contacts SET name=?, contact_type=?, phone=?, email=?, address=?, notes=?, is_active=?, updated_at=datetime('now') WHERE id=?",
		c.Name, c.ContactType, c.Phone, c.Email, c.Address, c.Notes, c.IsActive, c.ID,
	)
	if err != nil {
		return fmt.Errorf("update contact: %w", err)
	}
	return nil
}

func DeleteContact(db *sql.DB, id int) error {
	_, err := db.Exec("DELETE FROM contacts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	return nil
}
