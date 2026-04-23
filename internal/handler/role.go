package handler

import (
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type roleFormData struct {
	Role            *model.Role
	AllCapabilities []string
	Errors          map[string]string
	IsEdit          bool
}

var roleNameRegexp = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := model.ListRoles(h.DB)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/roles/index.html", "Roles", roles)
}

func (h *Handler) NewRole(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/roles/form.html", "New Role", roleFormData{
		Role:            &model.Role{},
		AllCapabilities: model.AllCapabilities,
		Errors:          make(map[string]string),
	})
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	role := &model.Role{
		Name:         strings.TrimSpace(r.FormValue("name")),
		Description:  strings.TrimSpace(r.FormValue("description")),
		IsSystem:     false,
		Capabilities: r.Form["capabilities"],
	}

	errors := validateRole(role, false)
	if len(errors) == 0 {
		if _, err := model.GetRoleByName(h.DB, role.Name); err == nil {
			errors["name"] = "Role name already exists"
		}
	}
	if len(errors) > 0 {
		h.render(w, r, "templates/roles/form.html", "New Role", roleFormData{
			Role: role, AllCapabilities: model.AllCapabilities, Errors: errors,
		})
		return
	}

	if err := model.CreateRole(h.DB, role); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "role.create",
		TargetType:  "role",
		TargetLabel: role.Name,
		Metadata: map[string]any{
			"after": map[string]any{
				"name":         role.Name,
				"description":  role.Description,
				"capabilities": role.Capabilities,
			},
		},
	})

	h.setFlash(w, "Role created successfully")
	http.Redirect(w, r, "/roles", http.StatusSeeOther)
}

func (h *Handler) EditRole(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	role, err := model.GetRoleByName(h.DB, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if role.Name == model.RoleAdmin {
		http.Error(w, "The admin role cannot be edited", http.StatusForbidden)
		return
	}

	h.render(w, r, "templates/roles/form.html", "Edit Role", roleFormData{
		Role: role, AllCapabilities: model.AllCapabilities, Errors: make(map[string]string), IsEdit: true,
	})
}

func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	existing, err := model.GetRoleByName(h.DB, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if existing.Name == model.RoleAdmin {
		http.Error(w, "The admin role cannot be edited", http.StatusForbidden)
		return
	}

	role := &model.Role{
		Name:         existing.Name,
		Description:  strings.TrimSpace(r.FormValue("description")),
		IsSystem:     existing.IsSystem,
		Capabilities: r.Form["capabilities"],
	}

	errors := validateRole(role, true)
	if len(errors) > 0 {
		h.render(w, r, "templates/roles/form.html", "Edit Role", roleFormData{
			Role: role, AllCapabilities: model.AllCapabilities, Errors: errors, IsEdit: true,
		})
		return
	}

	if err := model.UpdateRole(h.DB, role); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Sort capabilities so order-only drift doesn't show up as a change.
	oldCaps := append([]string(nil), existing.Capabilities...)
	newCaps := append([]string(nil), role.Capabilities...)
	sort.Strings(oldCaps)
	sort.Strings(newCaps)
	metadata := audit.Diff(
		map[string]any{"description": existing.Description, "capabilities": oldCaps},
		map[string]any{"description": role.Description, "capabilities": newCaps},
		[]string{"description", "capabilities"},
	)
	if metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "role.update",
			TargetType:  "role",
			TargetLabel: role.Name,
			Metadata:    metadata,
		})
	}

	h.setFlash(w, "Role updated successfully")
	http.Redirect(w, r, "/roles", http.StatusSeeOther)
}

func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	role, err := model.GetRoleByName(h.DB, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if role.IsSystem {
		h.setFlash(w, "System roles cannot be deleted")
		http.Redirect(w, r, "/roles", http.StatusSeeOther)
		return
	}

	count, err := model.CountUsersWithRole(h.DB, name)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if count > 0 {
		h.setFlash(w, "Cannot delete role: still assigned to one or more users")
		http.Redirect(w, r, "/roles", http.StatusSeeOther)
		return
	}

	if err := model.DeleteRole(h.DB, name); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "role.delete",
		TargetType:  "role",
		TargetLabel: role.Name,
		Metadata: map[string]any{
			"before": map[string]any{
				"name":         role.Name,
				"description":  role.Description,
				"capabilities": role.Capabilities,
			},
		},
	})

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "Role deleted")
	http.Redirect(w, r, "/roles", http.StatusSeeOther)
}

func validateRole(r *model.Role, isEdit bool) map[string]string {
	errors := make(map[string]string)
	if !isEdit {
		if r.Name == "" {
			errors["name"] = "Name is required"
		} else if !roleNameRegexp.MatchString(r.Name) {
			errors["name"] = "Use lowercase letters, digits, hyphens or underscores (must start with a letter)"
		} else if r.Name == model.RoleAdmin {
			errors["name"] = "Reserved role name"
		}
	}
	allowed := make(map[string]struct{}, len(model.AllCapabilities))
	for _, c := range model.AllCapabilities {
		allowed[c] = struct{}{}
	}
	for _, c := range r.Capabilities {
		if _, ok := allowed[c]; !ok {
			errors["capabilities"] = "Unknown capability: " + c
			break
		}
	}
	return errors
}
