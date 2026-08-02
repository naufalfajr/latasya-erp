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
