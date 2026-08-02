package handler

import (
	"errors"
	"net/http"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/model"
)

type roleFormData struct {
	Role            *model.Role
	AllCapabilities []string
	Errors          map[string]string
	IsEdit          bool
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	result, err := h.Access.ListRoles(r.Context(), accessActor(r), access.ListFilter{})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/roles/index.html", "Roles", result.Roles)
}

func (h *Handler) roleForm(w http.ResponseWriter, r *http.Request, title string, role *model.Role, fields map[string]string, edit bool) {
	h.render(w, r, "templates/roles/form.html", title, roleFormData{Role: role, AllCapabilities: model.AllCapabilities, Errors: fields, IsEdit: edit})
}

func (h *Handler) NewRole(w http.ResponseWriter, r *http.Request) {
	h.roleForm(w, r, "New Role", &model.Role{}, map[string]string{}, false)
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	draft := access.RoleDraft{Name: r.FormValue("name"), Description: r.FormValue("description"), Capabilities: r.Form["capabilities"]}
	role, err := h.Access.CreateRole(r.Context(), accessActor(r), draft)
	if err != nil {
		h.handleRoleFormError(w, r, "New Role", &model.Role{Name: draft.Name, Description: draft.Description, Capabilities: draft.Capabilities}, false, err)
		return
	}
	_ = role
	h.setFlash(w, "Role created successfully")
	http.Redirect(w, r, h.BasePath+"/roles", http.StatusSeeOther)
}

func (h *Handler) EditRole(w http.ResponseWriter, r *http.Request) {
	role, err := h.Access.GetRole(r.Context(), accessActor(r), r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if role.Name == model.RoleAdmin {
		http.Error(w, "The admin role cannot be edited", http.StatusForbidden)
		return
	}
	h.roleForm(w, r, "Edit Role", role, map[string]string{}, true)
}

func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	draft := access.RoleDraft{Description: r.FormValue("description"), Capabilities: r.Form["capabilities"]}
	role, err := h.Access.UpdateRole(r.Context(), accessActor(r), name, draft)
	if err != nil {
		var conflict *access.ConflictError
		if errors.As(err, &conflict) && name == model.RoleAdmin {
			http.Error(w, "The admin role cannot be edited", http.StatusForbidden)
			return
		}
		h.handleRoleFormError(w, r, "Edit Role", &model.Role{Name: name, Description: draft.Description, Capabilities: draft.Capabilities}, true, err)
		return
	}
	_ = role
	h.setFlash(w, "Role updated successfully")
	http.Redirect(w, r, h.BasePath+"/roles", http.StatusSeeOther)
}

func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	_, err := h.Access.DeleteRole(r.Context(), accessActor(r), r.PathValue("name"))
	if err != nil {
		var conflict *access.ConflictError
		switch {
		case errors.As(err, &conflict):
			msg := "Cannot delete role: still assigned to one or more users"
			if conflict.Message == "system roles cannot be deleted" {
				msg = "System roles cannot be deleted"
			}
			h.setFlash(w, msg)
			http.Redirect(w, r, h.BasePath+"/roles", http.StatusSeeOther)
		case errors.Is(err, access.ErrNotFound):
			http.NotFound(w, r)
		default:
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.setFlash(w, "Role deleted")
	http.Redirect(w, r, h.BasePath+"/roles", http.StatusSeeOther)
}

func (h *Handler) handleRoleFormError(w http.ResponseWriter, r *http.Request, title string, role *model.Role, edit bool, err error) {
	var validation *access.ValidationError
	var conflict *access.ConflictError
	switch {
	case errors.As(err, &validation):
		h.roleForm(w, r, title, role, validation.Fields, edit)
	case errors.As(err, &conflict):
		h.roleForm(w, r, title, role, map[string]string{"name": "Role name already exists"}, edit)
	case errors.Is(err, access.ErrNotFound):
		http.NotFound(w, r)
	default:
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
