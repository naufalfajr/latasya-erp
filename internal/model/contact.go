package model

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
	PortalCode         string  `json:"-"`
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
