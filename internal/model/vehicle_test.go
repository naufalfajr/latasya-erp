package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestListRouteCapacity(t *testing.T) {
	db := testutil.SetupTestDB(t)

	routes, err := model.ListRoutes(db)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected seeded routes, got %d", len(routes))
	}

	var westID int
	for _, r := range routes {
		if r.Name == "West" {
			westID = r.ID
		}
	}
	if westID == 0 {
		t.Fatal("west route not seeded")
	}

	if err := model.CreateContact(db, &model.Contact{Name: "Student", ContactType: "customer", RouteID: westID, IsActive: true}); err != nil {
		t.Fatalf("create contact: %v", err)
	}

	capacities, err := model.ListRouteCapacity(db)
	if err != nil {
		t.Fatalf("list route capacity: %v", err)
	}
	for _, c := range capacities {
		if c.RouteName == "West" {
			if c.VehicleCode != "LA001" || c.Capacity != 14 || c.Used != 1 {
				t.Fatalf("unexpected west capacity: %+v", c)
			}
			return
		}
	}
	t.Fatal("west capacity not found")
}
