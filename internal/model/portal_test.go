package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestGetOrCreatePortalToken_StableAcrossCalls(t *testing.T) {
	db := testutil.SetupTestDB(t)
	c := &model.Contact{Name: "Andi", ContactType: "customer", Phone: "081111111111", IsActive: true}
	if err := model.CreateContact(db, c); err != nil {
		t.Fatalf("create contact: %v", err)
	}
	contacts, _ := model.ListContacts(db, model.ContactFilter{Search: "Andi"})
	id := contacts[0].ID

	tok1, err := model.GetOrCreatePortalToken(db, id)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if tok1 == "" {
		t.Fatal("expected non-empty token")
	}
	tok2, err := model.GetOrCreatePortalToken(db, id)
	if err != nil {
		t.Fatalf("get token again: %v", err)
	}
	if tok1 != tok2 {
		t.Errorf("token changed across calls: %q != %q", tok1, tok2)
	}
}

func TestRegeneratePortalToken_InvalidatesOldToken(t *testing.T) {
	db := testutil.SetupTestDB(t)
	c := &model.Contact{Name: "Budi", ContactType: "customer", Phone: "082222222222", IsActive: true}
	model.CreateContact(db, c)
	contacts, _ := model.ListContacts(db, model.ContactFilter{Search: "Budi"})
	id := contacts[0].ID

	oldTok, _ := model.GetOrCreatePortalToken(db, id)
	newTok, err := model.RegeneratePortalToken(db, id)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if newTok == oldTok {
		t.Fatal("expected a different token after regenerate")
	}

	fam, err := model.ContactsByPortalToken(db, oldTok)
	if err != nil {
		t.Fatalf("lookup old token: %v", err)
	}
	if fam != nil {
		t.Error("old token should no longer resolve to a family")
	}

	fam, err = model.ContactsByPortalToken(db, newTok)
	if err != nil {
		t.Fatalf("lookup new token: %v", err)
	}
	if fam == nil || len(fam.Contacts) != 1 {
		t.Fatalf("expected new token to resolve to 1 contact, got %+v", fam)
	}
}

func TestContactsByPortalToken_GroupsSiblingsBySharedPhone(t *testing.T) {
	db := testutil.SetupTestDB(t)
	shared := "083333333333"
	c1 := &model.Contact{Name: "Sibling One", ContactType: "customer", Phone: shared, IsActive: true}
	c2 := &model.Contact{Name: "Sibling Two", ContactType: "customer", Phone: shared, IsActive: true}
	model.CreateContact(db, c1)
	model.CreateContact(db, c2)
	contacts, _ := model.ListContacts(db, model.ContactFilter{Search: "Sibling"})
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(contacts))
	}

	token, err := model.GetOrCreatePortalToken(db, contacts[0].ID)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}

	fam, err := model.ContactsByPortalToken(db, token)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if fam == nil || len(fam.Contacts) != 2 {
		t.Fatalf("expected family of 2, got %+v", fam)
	}
}

func TestContactsByPortalToken_BlankPhoneDoesNotGroup(t *testing.T) {
	db := testutil.SetupTestDB(t)
	c1 := &model.Contact{Name: "No Phone One", ContactType: "customer", Phone: "", IsActive: true}
	c2 := &model.Contact{Name: "No Phone Two", ContactType: "customer", Phone: "", IsActive: true}
	model.CreateContact(db, c1)
	model.CreateContact(db, c2)
	contacts, _ := model.ListContacts(db, model.ContactFilter{Search: "No Phone"})
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(contacts))
	}

	token, err := model.GetOrCreatePortalToken(db, contacts[0].ID)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}

	fam, err := model.ContactsByPortalToken(db, token)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if fam == nil || len(fam.Contacts) != 1 {
		t.Fatalf("blank-phone contact should not group with others, got %+v", fam)
	}
}

func TestContactsByPortalToken_UnknownTokenReturnsNil(t *testing.T) {
	db := testutil.SetupTestDB(t)
	fam, err := model.ContactsByPortalToken(db, "does-not-exist")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fam != nil {
		t.Errorf("expected nil family for unknown token, got %+v", fam)
	}
}

func TestListPortalInvoices_ExcludesDrafts(t *testing.T) {
	db := testutil.SetupTestDB(t)
	c := &model.Contact{Name: "Citra", ContactType: "customer", Phone: "084444444444", IsActive: true}
	model.CreateContact(db, c)
	contacts, _ := model.ListContacts(db, model.ContactFilter{Search: "Citra"})
	contactID := contacts[0].ID

	var revenueAccountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueAccountID)

	draft := &model.Invoice{ContactID: contactID, InvoiceDate: "2026-07-01", DueDate: "2026-07-11", CreatedBy: 1}
	if _, err := model.CreateInvoice(db, draft, []model.InvoiceLine{
		{Description: "Antar jemput", Quantity: 100, UnitPrice: 400000, AccountID: revenueAccountID},
	}); err != nil {
		t.Fatalf("create draft invoice: %v", err)
	}

	sent := &model.Invoice{ContactID: contactID, InvoiceDate: "2026-06-01", DueDate: "2026-06-11", CreatedBy: 1}
	sentID, err := model.CreateInvoice(db, sent, []model.InvoiceLine{
		{Description: "Antar jemput", Quantity: 100, UnitPrice: 400000, AccountID: revenueAccountID},
	})
	if err != nil {
		t.Fatalf("create sent invoice: %v", err)
	}
	if err := model.SendInvoice(db, sentID, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	invoices, err := model.ListPortalInvoices(db, []int{contactID})
	if err != nil {
		t.Fatalf("list portal invoices: %v", err)
	}
	if len(invoices) != 1 {
		t.Fatalf("expected 1 non-draft invoice, got %d", len(invoices))
	}
	if invoices[0].ID != sentID {
		t.Errorf("expected sent invoice %d, got %d", sentID, invoices[0].ID)
	}
}
