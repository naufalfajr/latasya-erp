package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type userFormData struct {
	User   *model.User
	Roles  []model.Role
	Errors map[string]string
	IsEdit bool
}

func accessActor(r *http.Request) access.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return access.Actor{}
	}
	return access.Actor{UserID: u.ID, CanManageUsers: u.HasCapability(model.CapUsersManage), CanManageRoles: u.HasCapability(model.CapRolesManage)}
}

func (h *Handler) userForm(w http.ResponseWriter, r *http.Request, title string, u *model.User, fields map[string]string, edit bool) {
	roles, err := h.Access.ListRoles(r.Context(), accessActor(r), access.ListFilter{})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/users/form.html", title, userFormData{User: u, Roles: roles.Roles, Errors: fields, IsEdit: edit})
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	result, err := h.Access.ListUsers(r.Context(), accessActor(r), access.ListFilter{})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/users/index.html", "Users", result.Users)
}

func (h *Handler) NewUser(w http.ResponseWriter, r *http.Request) {
	h.userForm(w, r, "New User", &model.User{IsActive: true, Role: model.RoleViewer}, map[string]string{}, false)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	draft := access.UserDraft{Username: r.FormValue("username"), FullName: r.FormValue("full_name"), Role: r.FormValue("role"), IsActive: r.FormValue("is_active") == "on", Password: r.FormValue("password")}
	_, err := h.Access.CreateUser(r.Context(), accessActor(r), draft)
	if err != nil {
		u := &model.User{Username: draft.Username, FullName: draft.FullName, Role: draft.Role, IsActive: draft.IsActive, MustChangePassword: true}
		var validation *access.ValidationError
		var conflict *access.ConflictError
		switch {
		case errors.As(err, &validation):
			h.userForm(w, r, "New User", u, userFormErrors(validation.Fields), false)
		case errors.As(err, &conflict):
			h.userForm(w, r, "New User", u, map[string]string{"username": "Username already exists"}, false)
		default:
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}
	h.setFlash(w, "User created successfully")
	http.Redirect(w, r, h.BasePath+"/users", http.StatusSeeOther)
}

func (h *Handler) EditUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	u, err := h.Access.GetUser(r.Context(), accessActor(r), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.userForm(w, r, "Edit User", u, map[string]string{}, true)
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	existing, err := h.Access.GetUser(r.Context(), accessActor(r), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	draft := access.UserDraft{FullName: r.FormValue("full_name"), Role: r.FormValue("role"), IsActive: r.FormValue("is_active") == "on", Password: r.FormValue("password")}
	if actor := accessActor(r); actor.UserID == id && !draft.IsActive {
		draft.IsActive = true
	}
	_, err = h.Access.UpdateUser(r.Context(), accessActor(r), id, draft)
	if err != nil {
		var validation *access.ValidationError
		if errors.As(err, &validation) {
			u := &model.User{ID: id, Username: existing.Username, FullName: draft.FullName, Role: draft.Role, IsActive: draft.IsActive}
			h.userForm(w, r, "Edit User", u, userFormErrors(validation.Fields), true)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.setFlash(w, "User updated successfully")
	http.Redirect(w, r, h.BasePath+"/users", http.StatusSeeOther)
}

func userFormErrors(fields map[string]string) map[string]string {
	result := map[string]string{}
	for field, message := range fields {
		switch field {
		case "username":
			result[field] = "Username is required"
		case "full_name":
			result[field] = "Full name is required"
		case "role":
			result[field] = "Invalid role"
		case "password":
			if message == "required" {
				result[field] = "Password is required"
			} else {
				result[field] = "Password must be at least 4 characters"
			}
		default:
			result[field] = message
		}
	}
	return result
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = h.Access.DeactivateUser(r.Context(), accessActor(r), id)
	if err != nil {
		var conflict *access.ConflictError
		if errors.As(err, &conflict) {
			h.setFlash(w, "Cannot delete your own account")
			http.Redirect(w, r, h.BasePath+"/users", http.StatusSeeOther)
			return
		}
		if errors.Is(err, access.ErrNotFound) {
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
	h.setFlash(w, "User deactivated")
	http.Redirect(w, r, h.BasePath+"/users", http.StatusSeeOther)
}
