package contact_test

import (
	"context"
	"testing"

	contactModule "github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

var contactManager = contactModule.Actor{UserID: 1, CanManage: true}

func listContacts(m *contactModule.Module, filter contactModule.Filter) ([]model.Contact, error) {
	result, err := m.List(context.Background(), filter)
	return result.Contacts, err
}

func contactDraft(c *model.Contact) contactModule.Draft {
	return contactModule.Draft{Name: c.Name, ContactType: c.ContactType, Phone: c.Phone, Email: c.Email, Address: c.Address, Notes: c.Notes, MapsLink: c.MapsLink, Class: c.Class, DistanceKm: c.DistanceKm, HasSiblingDiscount: c.HasSiblingDiscount, IsReturnOnly: c.IsReturnOnly, RouteID: c.RouteID, IsActive: c.IsActive}
}

func createContact(m *contactModule.Module, c *model.Contact) error {
	created, err := m.Create(context.Background(), contactManager, contactDraft(c))
	if err == nil {
		*c = *created
	}
	return err
}

func updateContact(m *contactModule.Module, c *model.Contact) error {
	updated, err := m.Update(context.Background(), contactManager, c.ID, contactDraft(c))
	if err == nil {
		*c = *updated
	}
	return err
}

func TestCreateContact(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	c := &model.Contact{
		Name:               "SD Negeri 1",
		ContactType:        "customer",
		Phone:              "08123456789",
		Email:              "sd1@example.com",
		Address:            "Jl. Pendidikan No. 1",
		DistanceKm:         6.5,
		HasSiblingDiscount: true,
		IsReturnOnly:       true,
		IsActive:           true,
	}

	if err := createContact(m, c); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	contacts, _ := listContacts(m, contactModule.Filter{Search: "SD Negeri"})
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	if contacts[0].Phone != "08123456789" {
		t.Errorf("expected phone '08123456789', got %q", contacts[0].Phone)
	}
	if contacts[0].DistanceKm != 6.5 || !contacts[0].HasSiblingDiscount || !contacts[0].IsReturnOnly {
		t.Fatalf("pricing fields not persisted: %+v", contacts[0])
	}
}

func TestContactPrice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		distanceKm         float64
		hasSiblingDiscount bool
		isReturnOnly       bool
		want               int
	}{
		{0, false, false, 350000},
		{3, false, false, 350000},
		{3.9, false, false, 350000},
		{4, false, false, 400000},
		{6, false, false, 400000},
		{6.4, false, false, 400000},
		{6.9, false, false, 400000},
		{7, false, false, 450000},
		{9, false, false, 450000},
		{9.9, false, false, 450000},
		{10, false, false, 500000},
		{11.4, false, false, 500000},
		{12, false, false, 500000},
		{12.9, false, false, 500000},
		{13, false, false, 550000},
		{8, true, false, 400000},
		{8, false, true, 400000},
		{8, true, true, 350000},
	}

	for _, tt := range tests {
		got := contactModule.Price(model.Contact{DistanceKm: tt.distanceKm, HasSiblingDiscount: tt.hasSiblingDiscount, IsReturnOnly: tt.isReturnOnly})
		if got != tt.want {
			t.Fatalf("ContactPrice(%g, %v, %v) = %d, want %d", tt.distanceKm, tt.hasSiblingDiscount, tt.isReturnOnly, got, tt.want)
		}
	}
}

func TestListContacts_FilterByType(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	// Create customer and supplier
	createContact(m, &model.Contact{Name: "Customer A", ContactType: "customer", IsActive: true})
	createContact(m, &model.Contact{Name: "Supplier B", ContactType: "supplier", IsActive: true})
	createContact(m, &model.Contact{Name: "Both C", ContactType: "both", IsActive: true})

	// Filter customers — should include "customer" and "both"
	customers, err := listContacts(m, contactModule.Filter{Type: "customer"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(customers) != 2 {
		t.Errorf("expected 2 contacts (customer + both), got %d", len(customers))
	}

	// Filter suppliers — should include "supplier" and "both"
	suppliers, _ := listContacts(m, contactModule.Filter{Type: "supplier"})
	if len(suppliers) != 2 {
		t.Errorf("expected 2 contacts (supplier + both), got %d", len(suppliers))
	}
}

func TestListContacts_Search(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	createContact(m, &model.Contact{Name: "SPBU Pertamina", ContactType: "supplier", Phone: "021555", IsActive: true})
	createContact(m, &model.Contact{Name: "SMP Negeri 2", ContactType: "customer", IsActive: true})

	// Search by name
	contacts, _ := listContacts(m, contactModule.Filter{Search: "Pertamina"})
	if len(contacts) != 1 {
		t.Errorf("expected 1 contact, got %d", len(contacts))
	}

	// Search by phone
	contacts, _ = listContacts(m, contactModule.Filter{Search: "021555"})
	if len(contacts) != 1 {
		t.Errorf("expected 1 contact by phone search, got %d", len(contacts))
	}
}

func TestGetContact(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	createContact(m, &model.Contact{Name: "Test Contact", ContactType: "customer", IsActive: true})

	contacts, _ := listContacts(m, contactModule.Filter{Search: "Test Contact"})
	if len(contacts) == 0 {
		t.Fatal("contact not found")
	}

	c, err := m.Get(context.Background(), contacts[0].ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c.Name != "Test Contact" {
		t.Errorf("expected name 'Test Contact', got %q", c.Name)
	}
}

func TestUpdateContact(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	createContact(m, &model.Contact{Name: "Original", ContactType: "customer", IsActive: true})

	contacts, _ := listContacts(m, contactModule.Filter{Search: "Original"})
	contacts[0].Name = "Updated"
	contacts[0].Phone = "0999"
	contacts[0].DistanceKm = 10.5
	contacts[0].HasSiblingDiscount = true

	if err := updateContact(m, &contacts[0]); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	updated, _ := m.Get(context.Background(), contacts[0].ID)
	if updated.Name != "Updated" {
		t.Errorf("expected name 'Updated', got %q", updated.Name)
	}
	if updated.Phone != "0999" {
		t.Errorf("expected phone '0999', got %q", updated.Phone)
	}
	if updated.DistanceKm != 10.5 || !updated.HasSiblingDiscount {
		t.Fatalf("expected updated pricing fields, got %+v", updated)
	}
}

func TestDeleteContact(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	createContact(m, &model.Contact{Name: "To Delete", ContactType: "supplier", IsActive: true})

	contacts, _ := listContacts(m, contactModule.Filter{Search: "To Delete"})
	if len(contacts) == 0 {
		t.Fatal("contact not found")
	}

	if _, err := m.Delete(context.Background(), contactManager, contacts[0].ID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	_, err := m.Get(context.Background(), contacts[0].ID)
	if err == nil {
		t.Error("expected error for deleted contact")
	}
}

func TestListContacts_FilterActive(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	createContact(m, &model.Contact{Name: "Active", ContactType: "customer", IsActive: true})
	createContact(m, &model.Contact{Name: "Inactive", ContactType: "customer", IsActive: false})

	active := true
	contacts, _ := listContacts(m, contactModule.Filter{IsActive: &active})
	for _, c := range contacts {
		if !c.IsActive {
			t.Errorf("expected only active contacts, got inactive: %s", c.Name)
		}
	}

	inactive := false
	contacts, _ = listContacts(m, contactModule.Filter{IsActive: &inactive})
	for _, c := range contacts {
		if c.IsActive {
			t.Errorf("expected only inactive contacts, got active: %s", c.Name)
		}
	}
}

func TestListContacts_Sort(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	var eastID, westID int
	db.QueryRow("SELECT id FROM routes WHERE name = 'East'").Scan(&eastID)
	db.QueryRow("SELECT id FROM routes WHERE name = 'West'").Scan(&westID)
	createContact(m, &model.Contact{Name: "B", ContactType: "customer", Class: "2", RouteID: eastID, IsActive: true})
	createContact(m, &model.Contact{Name: "A", ContactType: "customer", Class: "1", RouteID: westID, IsActive: false})

	contacts, err := listContacts(m, contactModule.Filter{Sort: "name", Order: "desc"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if contacts[0].Name != "B" {
		t.Fatalf("expected B first by name desc, got %s", contacts[0].Name)
	}

	contacts, err = listContacts(m, contactModule.Filter{Sort: "class", Order: "asc"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if contacts[0].Class != "1" {
		t.Fatalf("expected class 1 first, got %s", contacts[0].Class)
	}

	contacts, err = listContacts(m, contactModule.Filter{Sort: "route", Order: "asc"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if contacts[0].RouteName != "East" {
		t.Fatalf("expected East first by route asc, got %s", contacts[0].RouteName)
	}

	contacts, err = listContacts(m, contactModule.Filter{Sort: "status", Order: "desc"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !contacts[0].IsActive {
		t.Fatal("expected active contact first by status desc")
	}
}

func TestContactRoute(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	var routeID int
	if err := db.QueryRow("SELECT id FROM routes WHERE name = 'East'").Scan(&routeID); err != nil {
		t.Fatalf("get route: %v", err)
	}

	if err := createContact(m, &model.Contact{Name: "Routed", ContactType: "customer", RouteID: routeID, IsActive: true}); err != nil {
		t.Fatalf("create contact: %v", err)
	}
	contacts, err := listContacts(m, contactModule.Filter{Search: "Routed"})
	if err != nil {
		t.Fatalf("list contacts: %v", err)
	}
	if contacts[0].RouteID != routeID || contacts[0].RouteName != "East" {
		t.Fatalf("expected east route, got id=%d name=%q", contacts[0].RouteID, contacts[0].RouteName)
	}
}
