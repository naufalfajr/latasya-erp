package handler

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type contactPageData struct {
	Contacts      []model.Contact
	RouteCapacity []model.RouteCapacity
	Filter        string
	Search        string
	Sort          string
	Order         string
	SortURLs      map[string]string
}

func (h *Handler) ListContacts(w http.ResponseWriter, r *http.Request) {
	filterType := r.URL.Query().Get("type")
	search := r.URL.Query().Get("search")
	sort := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")

	// Show both active and inactive contacts so the status column is meaningful
	// and inactive contacts remain reachable for reactivation via the edit page.
	contacts, err := model.ListContacts(h.DB, model.ContactFilter{
		Type:   filterType,
		Search: search,
		Sort:   sort,
		Order:  order,
	})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	capacity, _ := model.ListRouteCapacity(h.DB)
	h.render(w, r, "templates/contacts/index.html", "Contacts", contactPageData{
		Contacts:      contacts,
		RouteCapacity: capacity,
		Filter:        filterType,
		Search:        search,
		Sort:          sort,
		Order:         order,
		SortURLs:      contactSortURLs(r, sort, order),
	})
}

func contactSortURLs(r *http.Request, sort, order string) map[string]string {
	urls := make(map[string]string, 3)
	for _, column := range []string{"name", "class", "status"} {
		q := r.URL.Query()
		q.Set("sort", column)
		if sort == column && order != "desc" {
			q.Set("order", "desc")
		} else {
			q.Set("order", "asc")
		}
		urls[column] = "/contacts?" + q.Encode()
	}
	return urls
}

type contactFormData struct {
	Contact *model.Contact
	Routes  []model.Route
	Errors  map[string]string
	IsEdit  bool
}

func (h *Handler) NewContact(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/contacts/form.html", "New Contact", contactFormData{
		Contact: &model.Contact{IsActive: true},
		Routes:  h.contactRoutes(),
	})
}

func (h *Handler) CreateContact(w http.ResponseWriter, r *http.Request) {
	c := &model.Contact{
		Name:        r.FormValue("name"),
		ContactType: r.FormValue("contact_type"),
		Phone:       r.FormValue("phone"),
		Email:       r.FormValue("email"),
		Address:     r.FormValue("address"),
		Notes:       r.FormValue("notes"),
		MapsLink:    strings.TrimSpace(r.FormValue("maps_link")),
		Class:       strings.TrimSpace(r.FormValue("class")),
		Price:       parseIDR(r.FormValue("price")),
		RouteID:     parseOptionalInt(r.FormValue("route_id")),
		IsActive:    r.FormValue("is_active") == "on",
	}

	errors := validateContact(c)
	if len(errors) > 0 {
		h.render(w, r, "templates/contacts/form.html", "New Contact", contactFormData{
			Contact: c,
			Routes:  h.contactRoutes(),
			Errors:  errors,
		})
		return
	}

	if err := model.CreateContact(h.DB, c); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var createdID int64
	h.DB.QueryRow("SELECT last_insert_rowid()").Scan(&createdID)
	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "contact.create",
		TargetType:  "contact",
		TargetID:    createdID,
		TargetLabel: c.Name,
		Metadata: map[string]any{
			"after": map[string]any{
				"name":         c.Name,
				"contact_type": c.ContactType,
				"email":        c.Email,
				"phone":        c.Phone,
				"class":        c.Class,
				"price":        c.Price,
				"is_active":    c.IsActive,
			},
		},
	})

	h.setFlash(w, "Contact created successfully")
	http.Redirect(w, r, "/contacts", http.StatusSeeOther)
}

func (h *Handler) EditContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contact, err := model.GetContact(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	h.render(w, r, "templates/contacts/form.html", "Edit Contact", contactFormData{
		Contact: contact,
		Routes:  h.contactRoutes(),
		IsEdit:  true,
	})
}

func (h *Handler) UpdateContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	existing, err := model.GetContact(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	c := &model.Contact{
		ID:          id,
		Name:        r.FormValue("name"),
		ContactType: r.FormValue("contact_type"),
		Phone:       r.FormValue("phone"),
		Email:       r.FormValue("email"),
		Address:     r.FormValue("address"),
		Notes:       r.FormValue("notes"),
		MapsLink:    strings.TrimSpace(r.FormValue("maps_link")),
		Class:       strings.TrimSpace(r.FormValue("class")),
		Price:       parseIDR(r.FormValue("price")),
		RouteID:     parseOptionalInt(r.FormValue("route_id")),
		IsActive:    r.FormValue("is_active") == "on",
	}

	errors := validateContact(c)
	if len(errors) > 0 {
		h.render(w, r, "templates/contacts/form.html", "Edit Contact", contactFormData{
			Contact: c,
			Routes:  h.contactRoutes(),
			Errors:  errors,
			IsEdit:  true,
		})
		return
	}

	if err := model.UpdateContact(h.DB, c); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	oldFields := map[string]any{
		"name":         existing.Name,
		"contact_type": existing.ContactType,
		"email":        existing.Email,
		"phone":        existing.Phone,
		"address":      existing.Address,
		"notes":        existing.Notes,
		"maps_link":    existing.MapsLink,
		"class":        existing.Class,
		"price":        existing.Price,
		"route_id":     existing.RouteID,
		"is_active":    existing.IsActive,
	}
	newFields := map[string]any{
		"name":         c.Name,
		"contact_type": c.ContactType,
		"email":        c.Email,
		"phone":        c.Phone,
		"address":      c.Address,
		"notes":        c.Notes,
		"maps_link":    c.MapsLink,
		"class":        c.Class,
		"price":        c.Price,
		"route_id":     c.RouteID,
		"is_active":    c.IsActive,
	}
	metadata := audit.Diff(oldFields, newFields,
		[]string{"name", "contact_type", "email", "phone", "address", "notes", "maps_link", "class", "price", "route_id", "is_active"})
	if metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "contact.update",
			TargetType:  "contact",
			TargetID:    int64(id),
			TargetLabel: existing.Name,
			Metadata:    metadata,
		})
	}

	h.setFlash(w, "Contact updated successfully")
	http.Redirect(w, r, "/contacts", http.StatusSeeOther)
}

func (h *Handler) DeleteContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	existing, _ := model.GetContact(h.DB, id)

	if err := model.DeleteContact(h.DB, id); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if existing != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "contact.delete",
			TargetType:  "contact",
			TargetID:    int64(id),
			TargetLabel: existing.Name,
			Metadata: map[string]any{
				"before": map[string]any{
					"name":         existing.Name,
					"contact_type": existing.ContactType,
					"email":        existing.Email,
				},
			},
		})
	}

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "Contact deleted successfully")
	http.Redirect(w, r, "/contacts", http.StatusSeeOther)
}

func (h *Handler) contactRoutes() []model.Route {
	routes, _ := model.ListRoutes(h.DB)
	return routes
}

func validateContact(c *model.Contact) map[string]string {
	errors := make(map[string]string)
	if c.Name == "" {
		errors["name"] = "Name is required"
	}
	if c.ContactType == "" {
		errors["contact_type"] = "Contact type is required"
	}
	if utf8.RuneCountInString(c.Class) > 5 {
		errors["class"] = "Class must be 5 characters or fewer"
	}
	return errors
}
