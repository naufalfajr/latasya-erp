package contact_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestModuleContactInvariants(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := contact.New(db)
	manager := contact.Actor{UserID: 1, CanManage: true}
	draft := contact.Draft{Name: "Student One", ContactType: "customer", Phone: "0812-3456", IsActive: true}

	if _, err := m.Create(context.Background(), contact.Actor{}, draft); !errors.Is(err, contact.ErrForbidden) {
		t.Fatalf("create without capability: got %v", err)
	}
	bad := draft
	bad.ContactType, bad.DistanceKm = "invalid", -1
	var validation *contact.ValidationError
	if _, err := m.Create(context.Background(), manager, bad); !errors.As(err, &validation) || validation.Fields["contact_type"] == "" || validation.Fields["distance_km"] == "" {
		t.Fatalf("invalid contact: got %v", err)
	}
	created, err := m.Create(context.Background(), manager, draft)
	if err != nil {
		t.Fatal(err)
	}
	updatedDraft := draft
	updatedDraft.Name = "Student Updated"
	updated, err := m.Update(context.Background(), manager, created.ID, updatedDraft)
	if err != nil || updated.Name != updatedDraft.Name {
		t.Fatalf("update: contact=%v err=%v", updated, err)
	}
	if _, err := m.Delete(context.Background(), manager, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(context.Background(), created.ID); !errors.Is(err, contact.ErrNotFound) {
		t.Fatalf("get deleted contact: got %v", err)
	}
}

func TestModulePortalIdentity(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := contact.New(db)
	actor := contact.Actor{UserID: 1, CanManage: true, CanManagePortal: true}
	if _, err := m.SetPortalCode(context.Background(), actor, 999999, ""); !errors.Is(err, contact.ErrNotFound) {
		t.Fatalf("reset missing contact: got %v", err)
	}
	var missingAudits int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='contact.portal_token_reset' AND target_id=999999").Scan(&missingAudits); err != nil || missingAudits != 0 {
		t.Fatalf("missing contact audit count=%d err=%v", missingAudits, err)
	}
	first, err := m.Create(context.Background(), actor, contact.Draft{Name: "Sibling One", ContactType: "customer", Phone: "+62 812-3456", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Create(context.Background(), actor, contact.Draft{Name: "Sibling Two", ContactType: "customer", Phone: "08123456", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetPortalCode(context.Background(), contact.Actor{UserID: 1, CanManage: true}, first.ID, "Family-123"); !errors.Is(err, contact.ErrForbidden) {
		t.Fatalf("contacts manager reset portal code: got %v", err)
	}
	code, err := m.SetPortalCode(context.Background(), actor, first.ID, "Family-123")
	if err != nil {
		t.Fatal(err)
	}
	family, err := m.FamilyByPortalCode(context.Background(), "FAMILY123")
	if err != nil || family == nil || !family.Has(first.ID) || !family.Has(second.ID) || family.Code != code {
		t.Fatalf("family lookup: family=%v err=%v", family, err)
	}
	if _, err := m.SetPortalCode(context.Background(), actor, second.ID, "family123"); !errors.Is(err, contact.ErrPortalCodeTaken) {
		t.Fatalf("normalized collision: got %v", err)
	}
	var metadata string
	if err := db.QueryRow("SELECT COALESCE(metadata,'') FROM audit_log WHERE action='contact.portal_token_reset' ORDER BY id DESC LIMIT 1").Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(metadata), "family") || !strings.Contains(metadata, "portal_code_changed") {
		t.Fatalf("portal audit metadata leaks credential or omits change marker: %s", metadata)
	}
}
