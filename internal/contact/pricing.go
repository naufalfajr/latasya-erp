package contact

import "github.com/naufal/latasya-erp/internal/model"

// Price returns the monthly transport price for a contact's route attributes.
func Price(c model.Contact) int {
	price := 550000
	switch {
	case c.DistanceKm < 4:
		price = 350000
	case c.DistanceKm < 7:
		price = 400000
	case c.DistanceKm < 10:
		price = 450000
	case c.DistanceKm < 13:
		price = 500000
	}
	if c.HasSiblingDiscount {
		price -= 50000
	}
	if c.IsReturnOnly {
		price -= 50000
	}
	return price
}
