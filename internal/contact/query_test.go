package contact_test

import (
	"context"
	"testing"

	contactModule "github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestListRouteAndActiveFilters(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	m := contactModule.New(db)

	routes, err := m.ListRoutes(context.Background())
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	var westID, southID int
	for _, r := range routes {
		switch r.Name {
		case "West":
			westID = r.ID
		case "South":
			southID = r.ID
		}
	}
	if westID == 0 || southID == 0 {
		t.Fatalf("expected West and South routes seeded, got %v", routes)
	}

	seed := []model.Contact{
		{Name: "West Active", ContactType: "customer", RouteID: westID, IsActive: true},
		{Name: "West Inactive", ContactType: "customer", RouteID: westID, IsActive: false},
		{Name: "South Active", ContactType: "customer", RouteID: southID, IsActive: true},
	}
	for i := range seed {
		if err := createContact(m, &seed[i]); err != nil {
			t.Fatalf("create contact %s: %v", seed[i].Name, err)
		}
	}

	got, err := listContacts(m, contactModule.Filter{RouteID: westID})
	if err != nil {
		t.Fatalf("list by route: %v", err)
	}
	if len(got) != 2 || got[0].Name != "West Active" || got[1].Name != "West Inactive" {
		t.Fatalf("route filter: got %+v", got)
	}

	inactive := false
	got, err = listContacts(m, contactModule.Filter{RouteID: westID, IsActive: &inactive})
	if err != nil {
		t.Fatalf("list by route+active: %v", err)
	}
	if len(got) != 1 || got[0].Name != "West Inactive" {
		t.Fatalf("route+active filter: got %+v", got)
	}

	got, err = listContacts(m, contactModule.Filter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("no filter: got %d contacts, want 3", len(got))
	}
}
