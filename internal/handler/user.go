package handler

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type userFormData struct {
	User   *model.User
	Roles  []model.Role
	Errors map[string]string
	IsEdit bool
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := model.ListUsers(h.DB)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/users/index.html", "Users", users)
}

func (h *Handler) NewUser(w http.ResponseWriter, r *http.Request) {
	roles, err := model.ListRoles(h.DB)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/users/form.html", "New User", userFormData{
		User:   &model.User{IsActive: true, Role: model.RoleViewer},
		Roles:  roles,
		Errors: make(map[string]string),
	})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	u := &model.User{
		Username:           r.FormValue("username"),
		FullName:           r.FormValue("full_name"),
		Role:               r.FormValue("role"),
		IsActive:           r.FormValue("is_active") == "on",
		MustChangePassword: true,
	}
	password := r.FormValue("password")

	errors := validateUser(h.DB, u, password, false)
	if len(errors) > 0 {
		roles, _ := model.ListRoles(h.DB)
		h.render(w, r, "templates/users/form.html", "New User", userFormData{
			User: u, Roles: roles, Errors: errors,
		})
		return
	}

	hashed, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	u.Password = hashed

	if err := model.CreateUser(h.DB, u); err != nil {
		errors := map[string]string{"username": "Username already exists"}
		roles, _ := model.ListRoles(h.DB)
		h.render(w, r, "templates/users/form.html", "New User", userFormData{
			User: u, Roles: roles, Errors: errors,
		})
		return
	}

	// Re-query to pick up the auto-generated ID for the audit row.
	created, _ := model.GetUserByUsername(h.DB, u.Username)
	var createdID int64
	if created != nil {
		createdID = int64(created.ID)
	}
	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "user.create",
		TargetType:  "user",
		TargetID:    createdID,
		TargetLabel: u.Username,
		Metadata: map[string]any{
			"after": map[string]any{
				"username":  u.Username,
				"full_name": u.FullName,
				"role":      u.Role,
				"is_active": u.IsActive,
			},
		},
	})

	h.setFlash(w, "User created successfully")
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (h *Handler) EditUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	u, err := model.GetUserByID(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	roles, err := model.ListRoles(h.DB)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/users/form.html", "Edit User", userFormData{
		User: u, Roles: roles, Errors: make(map[string]string), IsEdit: true,
	})
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Load existing user up-front — needed both for the error re-render path
	// (to preserve the immutable username) and for the audit before/after diff.
	existing, err := model.GetUserByID(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	u := &model.User{
		ID:       id,
		FullName: r.FormValue("full_name"),
		Role:     r.FormValue("role"),
		IsActive: r.FormValue("is_active") == "on",
	}

	// Prevent self-deactivation
	currentUser := auth.UserFromContext(r.Context())
	if currentUser.ID == id && !u.IsActive {
		u.IsActive = true
	}

	errors := make(map[string]string)
	if u.FullName == "" {
		errors["full_name"] = "Full name is required"
	}
	if !isValidRole(h.DB, u.Role) {
		errors["role"] = "Invalid role"
	}

	// Handle optional password change
	newPassword := r.FormValue("password")
	if newPassword != "" && len(newPassword) < 4 {
		errors["password"] = "Password must be at least 4 characters"
	}

	if len(errors) > 0 {
		u.Username = existing.Username
		roles, _ := model.ListRoles(h.DB)
		h.render(w, r, "templates/users/form.html", "Edit User", userFormData{
			User: u, Roles: roles, Errors: errors, IsEdit: true,
		})
		return
	}

	if err := model.UpdateUser(h.DB, u); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if newPassword != "" {
		hashed, err := auth.HashPassword(newPassword)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if err := model.UpdateUserPassword(h.DB, id, hashed); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		// Admin-reset password requires the target user to change it on next login,
		// unless the admin is editing their own account.
		if currentUser.ID != id {
			if err := model.SetMustChangePassword(h.DB, id, true); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}
	}

	oldFields := map[string]any{
		"full_name": existing.FullName,
		"role":      existing.Role,
		"is_active": existing.IsActive,
	}
	newFields := map[string]any{
		"full_name": u.FullName,
		"role":      u.Role,
		"is_active": u.IsActive,
	}
	metadata := audit.Diff(oldFields, newFields, []string{"full_name", "role", "is_active"})
	if newPassword != "" {
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["password_reset"] = true
	}
	if metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "user.update",
			TargetType:  "user",
			TargetID:    int64(id),
			TargetLabel: existing.Username,
			Metadata:    metadata,
		})
	}

	h.setFlash(w, "User updated successfully")
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Don't allow deleting yourself
	currentUser := auth.UserFromContext(r.Context())
	if currentUser.ID == id {
		h.setFlash(w, "Cannot delete your own account")
		http.Redirect(w, r, "/users", http.StatusSeeOther)
		return
	}

	// Deactivate instead of delete (preserve audit trail)
	existing, err := model.GetUserByID(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	wasActive := existing.IsActive
	existing.IsActive = false
	if err := model.UpdateUser(h.DB, existing); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "user.delete",
		TargetType:  "user",
		TargetID:    int64(existing.ID),
		TargetLabel: existing.Username,
		Metadata: map[string]any{
			"before": map[string]any{"is_active": wasActive},
			"after":  map[string]any{"is_active": false},
		},
	})

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "User deactivated")
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func validateUser(db *sql.DB, u *model.User, password string, isEdit bool) map[string]string {
	errors := make(map[string]string)
	if u.Username == "" {
		errors["username"] = "Username is required"
	}
	if u.FullName == "" {
		errors["full_name"] = "Full name is required"
	}
	if !isValidRole(db, u.Role) {
		errors["role"] = "Invalid role"
	}
	if !isEdit && password == "" {
		errors["password"] = "Password is required"
	}
	if password != "" && len(password) < 4 {
		errors["password"] = "Password must be at least 4 characters"
	}
	return errors
}

func isValidRole(db *sql.DB, name string) bool {
	if name == "" {
		return false
	}
	_, err := model.GetRoleByName(db, name)
	return err == nil
}
