package contact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/naufal/latasya-erp/internal/model"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func where(f Filter) (string, []any) {
	clause := ""
	args := []any{}
	if f.Type != "" {
		clause += " AND (c.contact_type=? OR c.contact_type='both')"
		args = append(args, f.Type)
	}
	if f.IsActive != nil {
		clause += " AND c.is_active=?"
		args = append(args, *f.IsActive)
	}
	if f.Search != "" {
		clause += " AND (c.name LIKE ? OR c.phone LIKE ? OR c.email LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s, s)
	}
	return clause, args
}

func orderBy(f Filter) string {
	column := "c.name"
	switch f.Sort {
	case "class":
		column = "c.class"
	case "route":
		column = "COALESCE(r.name,'')"
	case "status":
		column = "c.is_active"
	}
	direction := "ASC"
	if f.Order == "desc" {
		direction = "DESC"
	}
	return " ORDER BY " + column + " " + direction + ",c.name ASC"
}

func (m *Module) List(ctx context.Context, f Filter) (*ListResult, error) {
	clause, args := where(f)
	var total int
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM contacts c WHERE 1=1"+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count contacts: %w", err)
	}
	query := `SELECT c.id,c.name,c.contact_type,COALESCE(c.phone,''),COALESCE(c.email,''),COALESCE(c.address,''),COALESCE(c.notes,''),c.maps_link,c.class,c.distance_km,c.has_sibling_discount,c.is_return_only,COALESCE(c.route_id,0),c.is_active,c.created_at,c.updated_at,COALESCE(r.name,'') FROM contacts c LEFT JOIN routes r ON r.id=c.route_id WHERE 1=1` + clause + orderBy(f)
	listArgs := append([]any(nil), args...)
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		listArgs = append(listArgs, f.Limit, f.Offset)
	}
	rows, err := m.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()
	contacts := []model.Contact{}
	for rows.Next() {
		var c model.Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.ContactType, &c.Phone, &c.Email, &c.Address, &c.Notes, &c.MapsLink, &c.Class, &c.DistanceKm, &c.HasSiblingDiscount, &c.IsReturnOnly, &c.RouteID, &c.IsActive, &c.CreatedAt, &c.UpdatedAt, &c.RouteName); err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contacts: %w", err)
	}
	return &ListResult{Contacts: contacts, Total: total}, nil
}

func getWith(ctx context.Context, q queryer, id int) (*model.Contact, error) {
	c := &model.Contact{}
	err := q.QueryRowContext(ctx, `SELECT id,name,contact_type,COALESCE(phone,''),COALESCE(email,''),COALESCE(address,''),COALESCE(notes,''),maps_link,class,distance_km,has_sibling_discount,is_return_only,COALESCE(route_id,0),is_active,created_at,updated_at,COALESCE(portal_code,'') FROM contacts WHERE id=?`, id).Scan(&c.ID, &c.Name, &c.ContactType, &c.Phone, &c.Email, &c.Address, &c.Notes, &c.MapsLink, &c.Class, &c.DistanceKm, &c.HasSiblingDiscount, &c.IsReturnOnly, &c.RouteID, &c.IsActive, &c.CreatedAt, &c.UpdatedAt, &c.PortalCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get contact: %w", err)
	}
	return c, nil
}

func (m *Module) Get(ctx context.Context, id int) (*model.Contact, error) {
	return getWith(ctx, m.db, id)
}
