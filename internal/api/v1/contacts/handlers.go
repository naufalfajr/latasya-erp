// Package contacts implements the /api/v1/contacts CRUD endpoints.
package contacts

import (
	"errors"
	"net/http"
	"strconv"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	contactModule "github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Contacts *contactModule.Module }

type contactInput struct {
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
	IsActive           *bool   `json:"is_active"`
}

func actor(r *http.Request) contactModule.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return contactModule.Actor{}
	}
	return contactModule.Actor{UserID: u.ID, CanManage: v1.HasEffectiveCapability(r.Context(), model.CapContactsManage)}
}

func writeModuleError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	if errors.Is(err, contactModule.ErrForbidden) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "contacts.manage capability required", nil)
		return
	}
	if errors.Is(err, contactModule.ErrNotFound) {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "contact not found", nil)
		return
	}
	var validation *contactModule.ValidationError
	if errors.As(err, &validation) {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", validation.Fields)
		return
	}
	var conflict *contactModule.ConflictError
	if errors.As(err, &conflict) {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, conflict.Message, nil)
		return
	}
	v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, fallback, nil)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	result, err := h.Contacts.List(r.Context(), contactModule.Filter{Type: r.URL.Query().Get("type"), Search: r.URL.Query().Get("search"), Limit: page.PerPage, Offset: page.Offset()})
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list contacts", nil)
		return
	}
	v1.WriteList(w, http.StatusOK, result.Contacts, page, result.Total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "contact not found", nil)
		return
	}
	c, err := h.Contacts.Get(r.Context(), id)
	if err != nil {
		writeModuleError(w, r, err, "failed to get contact")
		return
	}
	v1.WriteJSON(w, http.StatusOK, c)
}

func draft(inp contactInput, isActive bool) contactModule.Draft {
	return contactModule.Draft{Name: inp.Name, ContactType: inp.ContactType, Phone: inp.Phone, Email: inp.Email, Address: inp.Address, Notes: inp.Notes, MapsLink: inp.MapsLink, Class: inp.Class, DistanceKm: inp.DistanceKm, HasSiblingDiscount: inp.HasSiblingDiscount, IsReturnOnly: inp.IsReturnOnly, IsActive: isActive}
}

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
	isActive := true
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}
	created, err := h.Contacts.Create(r.Context(), actor(r), draft(inp, isActive))
	if err != nil {
		writeModuleError(w, r, err, "failed to create contact")
		return
	}
	v1.WriteJSON(w, http.StatusCreated, created)
}

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
	existing, err := h.Contacts.Get(r.Context(), id)
	if err != nil {
		writeModuleError(w, r, err, "failed to get contact")
		return
	}
	var inp contactInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	isActive := existing.IsActive
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}
	updated, err := h.Contacts.Update(r.Context(), actor(r), id, draft(inp, isActive))
	if err != nil {
		writeModuleError(w, r, err, "failed to update contact")
		return
	}
	v1.WriteJSON(w, http.StatusOK, updated)
}

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
	if _, err := h.Contacts.Delete(r.Context(), actor(r), id); err != nil {
		writeModuleError(w, r, err, "failed to delete contact")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/contacts", h.List)
	mux.HandleFunc("GET /api/v1/contacts/{id}", h.Get)
	mux.HandleFunc("POST /api/v1/contacts", h.Create)
	mux.HandleFunc("PUT /api/v1/contacts/{id}", h.Update)
	mux.HandleFunc("DELETE /api/v1/contacts/{id}", h.Delete)
}
