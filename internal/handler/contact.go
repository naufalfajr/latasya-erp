package handler

import (
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type contactPageData struct {
	Contacts []model.Contact
	Filter   string
	Search   string
}

func (h *Handler) ListContacts(w http.ResponseWriter, r *http.Request) {
	filterType := r.URL.Query().Get("type")
	search := r.URL.Query().Get("search")

	active := true
	contacts, err := model.ListContacts(h.DB, model.ContactFilter{
		Type:     filterType,
		IsActive: &active,
		Search:   search,
	})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/contacts/index.html", "Contacts", contactPageData{
		Contacts: contacts,
		Filter:   filterType,
		Search:   search,
	})
}

type contactFormData struct {
	Contact *model.Contact
	Errors  map[string]string
	IsEdit  bool
}

func (h *Handler) NewContact(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/contacts/form.html", "New Contact", contactFormData{
		Contact: &model.Contact{IsActive: true},
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
		IsActive:    r.FormValue("is_active") == "on",
	}

	errors := validateContact(c)
	if len(errors) > 0 {
		h.render(w, r, "templates/contacts/form.html", "New Contact", contactFormData{
			Contact: c,
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
		IsActive:    r.FormValue("is_active") == "on",
	}

	errors := validateContact(c)
	if len(errors) > 0 {
		h.render(w, r, "templates/contacts/form.html", "Edit Contact", contactFormData{
			Contact: c,
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
		"is_active":    existing.IsActive,
	}
	newFields := map[string]any{
		"name":         c.Name,
		"contact_type": c.ContactType,
		"email":        c.Email,
		"phone":        c.Phone,
		"address":      c.Address,
		"notes":        c.Notes,
		"is_active":    c.IsActive,
	}
	metadata := audit.Diff(oldFields, newFields,
		[]string{"name", "contact_type", "email", "phone", "address", "notes", "is_active"})
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

func validateContact(c *model.Contact) map[string]string {
	errors := make(map[string]string)
	if c.Name == "" {
		errors["name"] = "Name is required"
	}
	if c.ContactType == "" {
		errors["contact_type"] = "Contact type is required"
	}
	return errors
}
