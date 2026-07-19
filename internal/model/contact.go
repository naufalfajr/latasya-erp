package model

import (
	"database/sql"
	"fmt"
)

type Contact struct {
	ID                 int     `json:"id"`
	Name               string  `json:"name"`
	ContactType        string  `json:"contact_type"`
	Phone              string  `json:"phone"`
	Email              string  `json:"email"`
	Address            string  `json:"address"`
	Notes              string  `json:"notes"`
	MapsLink           string  `json:"maps_link"`
	Class              string  `json:"class"`
	DistanceKm         float64 `json:"distance_km"`
	HasSiblingDiscount bool    `json:"has_sibling_discount"`
	IsReturnOnly       bool    `json:"is_return_only"`
	RouteID            int     `json:"route_id"`
	IsActive           bool    `json:"is_active"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	RouteName          string  `json:"route_name,omitempty"`
	PortalToken        string  `json:"-"`
}

func ContactPrice(distanceKm float64, hasSiblingDiscount, isReturnOnly bool) int {
	price := 550000
	switch {
	case distanceKm < 4:
		price = 350000
	case distanceKm < 7:
		price = 400000
	case distanceKm < 10:
		price = 450000
	case distanceKm < 13:
		price = 500000
	}
	if hasSiblingDiscount {
		price -= 50000
	}
	if isReturnOnly {
		price -= 50000
	}
	return price
}

func (c Contact) Price() int {
	return ContactPrice(c.DistanceKm, c.HasSiblingDiscount, c.IsReturnOnly)
}

type ContactFilter struct {
	Type     string // customer, supplier, both
	IsActive *bool
	Search   string
	Sort     string // name, class, route, status
	Order    string // asc, desc
}

func ListContacts(db *sql.DB, f ContactFilter) ([]Contact, error) {
	query := `SELECT c.id, c.name, c.contact_type, COALESCE(c.phone,''), COALESCE(c.email,''),
		COALESCE(c.address,''), COALESCE(c.notes,''), c.maps_link, c.class, c.distance_km, c.has_sibling_discount, c.is_return_only, COALESCE(c.route_id, 0), c.is_active, c.created_at, c.updated_at,
		COALESCE(r.name, '')
		FROM contacts c LEFT JOIN routes r ON r.id = c.route_id WHERE 1=1`
	var args []any

	if f.Type != "" {
		query += " AND (c.contact_type = ? OR c.contact_type = 'both')"
		args = append(args, f.Type)
	}
	if f.IsActive != nil {
		query += " AND c.is_active = ?"
		if *f.IsActive {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if f.Search != "" {
		query += " AND (c.name LIKE ? OR c.phone LIKE ? OR c.email LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s, s)
	}
	column := "c.name"
	switch f.Sort {
	case "class":
		column = "c.class"
	case "route":
		column = "COALESCE(r.name, '')"
	case "status":
		column = "c.is_active"
	}
	direction := "ASC"
	if f.Order == "desc" {
		direction = "DESC"
	}
	query += " ORDER BY " + column + " " + direction + ", c.name ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		err := rows.Scan(&c.ID, &c.Name, &c.ContactType, &c.Phone, &c.Email,
			&c.Address, &c.Notes, &c.MapsLink, &c.Class, &c.DistanceKm, &c.HasSiblingDiscount, &c.IsReturnOnly, &c.RouteID, &c.IsActive, &c.CreatedAt, &c.UpdatedAt, &c.RouteName)
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
		COALESCE(address,''), COALESCE(notes,''), maps_link, class, distance_km, has_sibling_discount, is_return_only, COALESCE(route_id, 0), is_active, created_at, updated_at, COALESCE(portal_token,'')
		FROM contacts WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.ContactType, &c.Phone, &c.Email,
		&c.Address, &c.Notes, &c.MapsLink, &c.Class, &c.DistanceKm, &c.HasSiblingDiscount, &c.IsReturnOnly, &c.RouteID, &c.IsActive, &c.CreatedAt, &c.UpdatedAt, &c.PortalToken)
	if err != nil {
		return nil, fmt.Errorf("get contact: %w", err)
	}
	return c, nil
}

func CreateContact(db *sql.DB, c *Contact) error {
	_, err := db.Exec(
		"INSERT INTO contacts (name, contact_type, phone, email, address, notes, maps_link, class, distance_km, has_sibling_discount, is_return_only, route_id, is_active) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		c.Name, c.ContactType, c.Phone, c.Email, c.Address, c.Notes, c.MapsLink, c.Class, c.DistanceKm, c.HasSiblingDiscount, c.IsReturnOnly, nullInt(c.RouteID), c.IsActive,
	)
	if err != nil {
		return fmt.Errorf("create contact: %w", err)
	}
	return nil
}

func UpdateContact(db *sql.DB, c *Contact) error {
	_, err := db.Exec(
		"UPDATE contacts SET name=?, contact_type=?, phone=?, email=?, address=?, notes=?, maps_link=?, class=?, distance_km=?, has_sibling_discount=?, is_return_only=?, route_id=?, is_active=?, updated_at=datetime('now') WHERE id=?",
		c.Name, c.ContactType, c.Phone, c.Email, c.Address, c.Notes, c.MapsLink, c.Class, c.DistanceKm, c.HasSiblingDiscount, c.IsReturnOnly, nullInt(c.RouteID), c.IsActive, c.ID,
	)
	if err != nil {
		return fmt.Errorf("update contact: %w", err)
	}
	return nil
}

func DeleteContact(db *sql.DB, id int) error {
	// Check for linked invoices or bills
	var count int
	db.QueryRow("SELECT COUNT(*) FROM invoices WHERE contact_id = ?", id).Scan(&count)
	if count > 0 {
		return fmt.Errorf("cannot delete contact: has %d linked invoice(s)", count)
	}
	db.QueryRow("SELECT COUNT(*) FROM bills WHERE contact_id = ?", id).Scan(&count)
	if count > 0 {
		return fmt.Errorf("cannot delete contact: has %d linked bill(s)", count)
	}

	_, err := db.Exec("DELETE FROM contacts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	return nil
}
