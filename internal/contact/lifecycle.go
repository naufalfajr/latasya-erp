package contact

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

func validate(d Draft) error {
	fields := map[string]string{}
	if strings.TrimSpace(d.Name) == "" {
		fields["name"] = "required"
	}
	if d.ContactType == "" {
		fields["contact_type"] = "required"
	} else if d.ContactType != "customer" && d.ContactType != "supplier" && d.ContactType != "both" {
		fields["contact_type"] = "must be customer, supplier, or both"
	}
	if utf8.RuneCountInString(d.Class) > 5 {
		fields["class"] = "must be 5 characters or fewer"
	}
	if d.DistanceKm < 0 {
		fields["distance_km"] = "must be 0 or greater"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func nullRoute(id int) any {
	if id == 0 {
		return nil
	}
	return id
}

func (m *Module) Create(ctx context.Context, actor Actor, d Draft) (*model.Contact, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validate(d); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin contact create: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO contacts (name,contact_type,phone,email,address,notes,maps_link,class,distance_km,has_sibling_discount,is_return_only,route_id,is_active) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, d.Name, d.ContactType, d.Phone, d.Email, d.Address, d.Notes, d.MapsLink, d.Class, d.DistanceKm, d.HasSiblingDiscount, d.IsReturnOnly, nullRoute(d.RouteID), d.IsActive)
	if err != nil {
		return nil, fmt.Errorf("create contact: %w", err)
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("contact id: %w", err)
	}
	created, err := getWith(ctx, tx, int(id64))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit contact create: %w", err)
	}
	m.audit(ctx, actor, "contact.create", created, map[string]any{"after": snapshot(created)})
	return created, nil
}

func (m *Module) Update(ctx context.Context, actor Actor, id int, d Draft) (*model.Contact, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validate(d); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin contact update: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE contacts SET id=id WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("lock contact: %w", err)
	}
	old, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE contacts SET name=?,contact_type=?,phone=?,email=?,address=?,notes=?,maps_link=?,class=?,distance_km=?,has_sibling_discount=?,is_return_only=?,route_id=?,is_active=?,updated_at=datetime('now') WHERE id=?`, d.Name, d.ContactType, d.Phone, d.Email, d.Address, d.Notes, d.MapsLink, d.Class, d.DistanceKm, d.HasSiblingDiscount, d.IsReturnOnly, nullRoute(d.RouteID), d.IsActive, id)
	if err != nil {
		return nil, fmt.Errorf("update contact: %w", err)
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit contact update: %w", err)
	}
	if diff := audit.Diff(snapshot(old), snapshot(updated), []string{"name", "contact_type", "email", "phone", "address", "notes", "maps_link", "class", "distance_km", "has_sibling_discount", "is_return_only", "route_id", "is_active"}); diff != nil {
		audit.Log(ctx, m.db, audit.Event{Action: "contact.update", TargetType: "contact", TargetID: int64(updated.ID), TargetLabel: old.Name, Metadata: diff, ActorID: int64(actor.UserID)})
	}
	return updated, nil
}

func (m *Module) Delete(ctx context.Context, actor Actor, id int) (*model.Contact, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin contact delete: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE contacts SET id=id WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("lock contact: %w", err)
	}
	old, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	for _, linked := range []struct{ table, label string }{{"invoices", "invoice"}, {"bills", "bill"}, {"credit_notes", "credit note"}} {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+linked.table+" WHERE contact_id=?", id).Scan(&count); err != nil {
			return nil, fmt.Errorf("count linked %ss: %w", linked.label, err)
		}
		if count > 0 {
			return nil, &ConflictError{Message: fmt.Sprintf("cannot delete contact: has %d linked %s(s)", count, linked.label)}
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM contacts WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("delete contact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit contact delete: %w", err)
	}
	m.audit(ctx, actor, "contact.delete", old, map[string]any{"before": snapshot(old)})
	return old, nil
}

func snapshot(c *model.Contact) map[string]any {
	return map[string]any{"name": c.Name, "contact_type": c.ContactType, "email": c.Email, "phone": c.Phone, "address": c.Address, "notes": c.Notes, "maps_link": c.MapsLink, "class": c.Class, "distance_km": c.DistanceKm, "has_sibling_discount": c.HasSiblingDiscount, "is_return_only": c.IsReturnOnly, "route_id": c.RouteID, "is_active": c.IsActive}
}

func (m *Module) audit(ctx context.Context, actor Actor, action string, c *model.Contact, metadata map[string]any) {
	audit.Log(ctx, m.db, audit.Event{Action: action, TargetType: "contact", TargetID: int64(c.ID), TargetLabel: c.Name, Metadata: metadata, ActorID: int64(actor.UserID)})
}
