package contact

import "github.com/naufal/latasya-erp/internal/model"

type Filter struct {
	Type     string
	IsActive *bool
	Search   string
	Sort     string
	Order    string
	Limit    int
	Offset   int
}

type ListResult struct {
	Contacts []model.Contact
	Total    int
}

type Draft struct {
	Name               string
	ContactType        string
	Phone              string
	Email              string
	Address            string
	Notes              string
	MapsLink           string
	Class              string
	DistanceKm         float64
	HasSiblingDiscount bool
	IsReturnOnly       bool
	RouteID            int
	IsActive           bool
}

type PortalFamily struct {
	Contacts []model.Contact
	Code     string
}

func (f *PortalFamily) ContactIDs() []int {
	ids := make([]int, len(f.Contacts))
	for i, c := range f.Contacts {
		ids[i] = c.ID
	}
	return ids
}

func (f *PortalFamily) Has(contactID int) bool {
	for _, c := range f.Contacts {
		if c.ID == contactID {
			return true
		}
	}
	return false
}
