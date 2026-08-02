package contact_test

import (
	"context"
	"testing"

	"github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestListRouteCapacity(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	module := contact.New(db)
	routes, err := module.ListRoutes(context.Background())
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 3 {
		t.Fatalf("expected seeded routes, got %d", len(routes))
	}

	var westID int
	var southSeeded bool
	for _, r := range routes {
		if r.Name == "West" {
			westID = r.ID
		}
		if r.Name == "South" {
			southSeeded = true
		}
	}
	if westID == 0 {
		t.Fatal("west route not seeded")
	}
	if !southSeeded {
		t.Fatal("south route not seeded")
	}

	if err := testutil.CreateContact(db, &model.Contact{Name: "Student", ContactType: "customer", RouteID: westID, IsActive: true}); err != nil {
		t.Fatalf("create contact: %v", err)
	}

	capacities, err := module.ListRouteCapacity(context.Background())
	if err != nil {
		t.Fatalf("list route capacity: %v", err)
	}
	byRoute := make(map[string]contact.RouteCapacity, len(capacities))
	for _, c := range capacities {
		byRoute[c.RouteName] = c
	}
	if got := byRoute["West"]; got.VehicleCode != "LA001" || got.Capacity != 13 || got.Used != 1 {
		t.Fatalf("unexpected west capacity: %+v", got)
	}
	if got := byRoute["South"]; got.VehicleCode != "LA003" || got.Capacity != 13 || got.Used != 0 {
		t.Fatalf("unexpected south capacity: %+v", got)
	}
}
