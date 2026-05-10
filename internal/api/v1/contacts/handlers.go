// Package contacts implements the /api/v1/contacts CRUD endpoints.
package contacts

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/audit"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/model"
)

// Handler holds shared dependencies for the contacts API.
type Handler struct {
	DB *sql.DB
}

// contactInput is the JSON request body for Create and Update.
type contactInput struct {
	Name        string `json:"name"`
	ContactType string `json:"contact_type"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Address     string `json:"address"`
	Notes       string `json:"notes"`
	IsActive    *bool  `json:"is_active"`
}

func validateContactInput(inp *contactInput) map[string]string {
	fields := make(map[string]string)
	if inp.Name == "" {
		fields["name"] = "required"
	}
	if inp.ContactType == "" {
		fields["contact_type"] = "required"
	} else if inp.ContactType != "customer" && inp.ContactType != "supplier" && inp.ContactType != "both" {
		fields["contact_type"] = "must be customer, supplier, or both"
	}
	return fields
}

// List handles GET /api/v1/contacts
// Supports ?type=customer|supplier|both and ?search=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	contactType := r.URL.Query().Get("type")
	search := r.URL.Query().Get("search")

	contacts, err := model.ListContacts(h.DB, model.ContactFilter{
		Type:   contactType,
		Search: search,
	})
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list contacts", nil)
		return
	}

	total := len(contacts)
	start := page.Offset()
	end := start + page.PerPage
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	v1.WriteList(w, http.StatusOK, contacts[start:end], page, total)
}

// Get handles GET /api/v1/contacts/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "contact not found", nil)
		return
	}

	contact, err := model.GetContact(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "contact not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "contact not found", nil)
		return
	}

	v1.WriteJSON(w, http.StatusOK, contact)
}

// Create handles POST /api/v1/contacts
// Requires contacts.manage capability. Returns 201 Created.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapContactsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "contacts.manage capability required", nil)
		return
	}

	var inp contactInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields := validateContactInput(&inp)
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	isActive := true
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}

	c := &model.Contact{
		Name:        inp.Name,
		ContactType: inp.ContactType,
		Phone:       inp.Phone,
		Email:       inp.Email,
		Address:     inp.Address,
		Notes:       inp.Notes,
		IsActive:    isActive,
	}

	if err := model.CreateContact(h.DB, c); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create contact", nil)
		return
	}

	var newID int64
	h.DB.QueryRow("SELECT last_insert_rowid()").Scan(&newID)
	c.ID = int(newID)

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "contact.create",
		TargetType:  "contact",
		TargetID:    newID,
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

	created, err := model.GetContact(h.DB, c.ID)
	if err != nil {
		v1.WriteJSON(w, http.StatusCreated, c)
		return
	}
	v1.WriteJSON(w, http.StatusCreated, created)
}

// Update handles PUT /api/v1/contacts/{id}
// Requires contacts.manage capability. Returns 200 OK.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapContactsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "contacts.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "contact not found", nil)
		return
	}

	existing, err := model.GetContact(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "contact not found", nil)
		return
	}

	var inp contactInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields := validateContactInput(&inp)
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	isActive := existing.IsActive
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}

	c := &model.Contact{
		ID:          id,
		Name:        inp.Name,
		ContactType: inp.ContactType,
		Phone:       inp.Phone,
		Email:       inp.Email,
		Address:     inp.Address,
		Notes:       inp.Notes,
		IsActive:    isActive,
	}

	if err := model.UpdateContact(h.DB, c); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update contact", nil)
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
	meta := audit.Diff(oldFields, newFields,
		[]string{"name", "contact_type", "email", "phone", "address", "notes", "is_active"})
	if meta != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "contact.update",
			TargetType:  "contact",
			TargetID:    int64(id),
			TargetLabel: existing.Name,
			Metadata:    meta,
		})
	}

	updated, err := model.GetContact(h.DB, id)
	if err != nil {
		v1.WriteJSON(w, http.StatusOK, c)
		return
	}
	v1.WriteJSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /api/v1/contacts/{id}
// Requires contacts.manage capability. Returns 204 No Content.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapContactsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "contacts.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "contact not found", nil)
		return
	}

	existing, _ := model.GetContact(h.DB, id)

	if err := model.DeleteContact(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
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

	w.WriteHeader(http.StatusNoContent)
}
