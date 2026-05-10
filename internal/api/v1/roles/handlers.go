package roles

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct {
	DB *sql.DB
}

var roleNameRegexp = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type roleInput struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
}

func (h *Handler) Capabilities(w http.ResponseWriter, r *http.Request) {
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": model.AllCapabilities})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapRolesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	roles, err := model.ListRoles(h.DB)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list roles", nil)
		return
	}
	if roles == nil {
		roles = []model.Role{}
	}

	page := v1.ParsePage(r)
	total := len(roles)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}

	v1.WriteList(w, http.StatusOK, roles[start:end], page, total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapRolesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	name := r.PathValue("name")
	role, err := model.GetRoleByName(h.DB, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "role not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "role not found", nil)
		return
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": role})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapRolesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	var inp roleInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields := validateRoleInput(&inp, false)
	if len(fields) == 0 {
		if _, err := model.GetRoleByName(h.DB, inp.Name); err == nil {
			fields = map[string]string{"name": "role name already exists"}
		}
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	role := &model.Role{
		Name:         inp.Name,
		Description:  strings.TrimSpace(inp.Description),
		IsSystem:     false,
		Capabilities: inp.Capabilities,
	}
	if role.Capabilities == nil {
		role.Capabilities = []string{}
	}

	if err := model.CreateRole(h.DB, role); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create role", nil)
		return
	}

	created, err := model.GetRoleByName(h.DB, role.Name)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve created role", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "role.create",
		TargetType:  "role",
		TargetLabel: created.Name,
		Metadata: map[string]any{
			"after": map[string]any{
				"name":         created.Name,
				"description":  created.Description,
				"capabilities": created.Capabilities,
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": created})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapRolesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	name := r.PathValue("name")
	existing, err := model.GetRoleByName(h.DB, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "role not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "role not found", nil)
		return
	}

	if existing.Name == model.RoleAdmin {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "the admin role cannot be edited", nil)
		return
	}

	var inp roleInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields := validateRoleInput(&inp, true)
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	role := &model.Role{
		Name:         existing.Name,
		Description:  strings.TrimSpace(inp.Description),
		IsSystem:     existing.IsSystem,
		Capabilities: inp.Capabilities,
	}
	if role.Capabilities == nil {
		role.Capabilities = []string{}
	}

	if err := model.UpdateRole(h.DB, role); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update role", nil)
		return
	}

	updated, err := model.GetRoleByName(h.DB, name)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve updated role", nil)
		return
	}

	oldFields := map[string]any{"description": existing.Description, "capabilities": existing.Capabilities}
	newFields := map[string]any{"description": updated.Description, "capabilities": updated.Capabilities}
	if metadata := audit.Diff(oldFields, newFields, []string{"description", "capabilities"}); metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "role.update",
			TargetType:  "role",
			TargetLabel: role.Name,
			Metadata:    metadata,
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapRolesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	name := r.PathValue("name")
	role, err := model.GetRoleByName(h.DB, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "role not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "role not found", nil)
		return
	}

	if role.Name == model.RoleAdmin {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "the admin role cannot be deleted", nil)
		return
	}

	count, err := model.CountUsersWithRole(h.DB, name)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to check role usage", nil)
		return
	}
	if count > 0 {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "cannot delete role: still assigned to users", nil)
		return
	}

	if err := model.DeleteRole(h.DB, name); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to delete role", nil)
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

	w.WriteHeader(http.StatusNoContent)
}

func validateRoleInput(inp *roleInput, isEdit bool) map[string]string {
	fields := make(map[string]string)
	if !isEdit {
		if strings.TrimSpace(inp.Name) == "" {
			fields["name"] = "required"
		} else if !roleNameRegexp.MatchString(inp.Name) {
			fields["name"] = "use lowercase letters, digits, hyphens or underscores (must start with a letter)"
		} else if inp.Name == model.RoleAdmin {
			fields["name"] = "reserved role name"
		}
	}
	allowed := make(map[string]struct{}, len(model.AllCapabilities))
	for _, c := range model.AllCapabilities {
		allowed[c] = struct{}{}
	}
	for _, c := range inp.Capabilities {
		if _, ok := allowed[c]; !ok {
			fields["capabilities"] = "unknown capability: " + c
			break
		}
	}
	return fields
}
