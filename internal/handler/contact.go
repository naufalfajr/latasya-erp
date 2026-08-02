package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/naufal/latasya-erp/internal/auth"
	contactModule "github.com/naufal/latasya-erp/internal/contact"
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
	f := contactModule.Filter{Type: r.URL.Query().Get("type"), Search: r.URL.Query().Get("search"), Sort: r.URL.Query().Get("sort"), Order: r.URL.Query().Get("order")}
	result, err := h.Contacts.List(r.Context(), f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	capacity, _ := model.ListRouteCapacity(h.DB)
	h.render(w, r, "templates/contacts/index.html", "Contacts", contactPageData{Contacts: result.Contacts, RouteCapacity: capacity, Filter: f.Type, Search: f.Search, Sort: f.Sort, Order: f.Order, SortURLs: h.contactSortURLs(r, f.Sort, f.Order)})
}

func (h *Handler) contactSortURLs(r *http.Request, sort, order string) map[string]string {
	urls := make(map[string]string, 4)
	for _, column := range []string{"name", "class", "route", "status"} {
		q := r.URL.Query()
		q.Set("sort", column)
		if sort == column && order != "desc" {
			q.Set("order", "desc")
		} else {
			q.Set("order", "asc")
		}
		urls[column] = h.BasePath + "/contacts?" + q.Encode()
	}
	return urls
}

type contactFormData struct {
	Contact   *model.Contact
	Routes    []model.Route
	Errors    map[string]string
	IsEdit    bool
	PortalURL string
}

func (h *Handler) NewContact(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/contacts/form.html", "New Contact", contactFormData{Contact: &model.Contact{IsActive: true}, Routes: h.contactRoutes()})
}

func contactDraft(r *http.Request) contactModule.Draft {
	return contactModule.Draft{Name: r.FormValue("name"), ContactType: r.FormValue("contact_type"), Phone: r.FormValue("phone"), Email: r.FormValue("email"), Address: r.FormValue("address"), Notes: r.FormValue("notes"), MapsLink: strings.TrimSpace(r.FormValue("maps_link")), Class: strings.TrimSpace(r.FormValue("class")), DistanceKm: parseOptionalFloat(r.FormValue("distance_km")), HasSiblingDiscount: r.FormValue("has_sibling_discount") == "on", IsReturnOnly: r.FormValue("is_return_only") == "on", RouteID: parseOptionalInt(r.FormValue("route_id")), IsActive: r.FormValue("is_active") == "on"}
}

func contactView(id int, d contactModule.Draft) *model.Contact {
	return &model.Contact{ID: id, Name: d.Name, ContactType: d.ContactType, Phone: d.Phone, Email: d.Email, Address: d.Address, Notes: d.Notes, MapsLink: d.MapsLink, Class: d.Class, DistanceKm: d.DistanceKm, HasSiblingDiscount: d.HasSiblingDiscount, IsReturnOnly: d.IsReturnOnly, RouteID: d.RouteID, IsActive: d.IsActive}
}

func contactActor(r *http.Request) contactModule.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return contactModule.Actor{}
	}
	return contactModule.Actor{UserID: u.ID, CanManage: u.HasCapability(model.CapContactsManage), CanManagePortal: u.Role == model.RoleAdmin}
}

func contactFormErrors(err error) map[string]string {
	var validation *contactModule.ValidationError
	if !errors.As(err, &validation) {
		return nil
	}
	fields := map[string]string{}
	for field, message := range validation.Fields {
		switch {
		case field == "name" && message == "required":
			message = "Name is required"
		case field == "contact_type" && message == "required":
			message = "Contact type is required"
		case field == "class":
			message = "Class must be 5 characters or fewer"
		case field == "distance_km":
			message = "Distance must be 0 or greater"
		}
		fields[field] = message
	}
	return fields
}

func (h *Handler) CreateContact(w http.ResponseWriter, r *http.Request) {
	draft := contactDraft(r)
	if _, err := h.Contacts.Create(r.Context(), contactActor(r), draft); err != nil {
		if fields := contactFormErrors(err); fields != nil {
			h.render(w, r, "templates/contacts/form.html", "New Contact", contactFormData{Contact: contactView(0, draft), Routes: h.contactRoutes(), Errors: fields})
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.setFlash(w, "Contact created successfully")
	http.Redirect(w, r, h.BasePath+"/contacts", http.StatusSeeOther)
}

func (h *Handler) EditContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := h.Contacts.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	portalURL := ""
	if c.PortalCode != "" {
		portalURL = h.publicOrigin(r) + "/p/" + c.PortalCode
	}
	h.render(w, r, "templates/contacts/form.html", "Edit Contact", contactFormData{Contact: c, Routes: h.contactRoutes(), IsEdit: true, PortalURL: portalURL})
}

func (h *Handler) UpdateContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	draft := contactDraft(r)
	if _, err := h.Contacts.Update(r.Context(), contactActor(r), id, draft); err != nil {
		if errors.Is(err, contactModule.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if fields := contactFormErrors(err); fields != nil {
			h.render(w, r, "templates/contacts/form.html", "Edit Contact", contactFormData{Contact: contactView(id, draft), Routes: h.contactRoutes(), Errors: fields, IsEdit: true})
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.setFlash(w, "Contact updated successfully")
	http.Redirect(w, r, h.BasePath+"/contacts", http.StatusSeeOther)
}

func (h *Handler) SaveContactPortalCode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	code, err := h.Contacts.SetPortalCode(r.Context(), contactActor(r), id, r.FormValue("portal_code"))
	switch {
	case errors.Is(err, contactModule.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, contactModule.ErrPortalCodeTaken):
		h.setFlash(w, "Link itu sudah dipakai kontak lain. Coba yang lain.")
	case err != nil:
		h.setFlash(w, "Error: "+err.Error())
	default:
		h.setFlash(w, "Link portal berhasil disimpan: "+code)
	}
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/contacts/%d/edit", id), http.StatusSeeOther)
}

func (h *Handler) DeleteContact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.Contacts.Delete(r.Context(), contactActor(r), id); err != nil {
		if errors.Is(err, contactModule.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.setFlash(w, "Contact deleted successfully")
	http.Redirect(w, r, h.BasePath+"/contacts", http.StatusSeeOther)
}

func (h *Handler) contactRoutes() []model.Route {
	routes, _ := model.ListRoutes(h.DB)
	return routes
}

func (h *Handler) RegisterContactRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /contacts", h.ListContacts)
	mux.HandleFunc("GET /contacts/new", h.NewContact)
	mux.HandleFunc("POST /contacts", auth.CapabilityOnly(model.CapContactsManage, h.CreateContact))
	mux.HandleFunc("GET /contacts/{id}/edit", h.EditContact)
	mux.HandleFunc("POST /contacts/{id}", auth.CapabilityOnly(model.CapContactsManage, h.UpdateContact))
	mux.HandleFunc("DELETE /contacts/{id}", auth.CapabilityOnly(model.CapContactsManage, h.DeleteContact))
	mux.HandleFunc("POST /contacts/{id}/portal-code", auth.AdminOnly(h.SaveContactPortalCode))
}
