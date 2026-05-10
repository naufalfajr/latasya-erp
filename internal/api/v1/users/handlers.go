package users

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct {
	DB *sql.DB
}

type userInput struct {
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	IsActive *bool  `json:"is_active"`
	Password string `json:"password"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapUsersManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	users, err := model.ListUsers(h.DB)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list users", nil)
		return
	}
	if users == nil {
		users = []model.User{}
	}

	page := v1.ParsePage(r)
	total := len(users)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}

	v1.WriteList(w, http.StatusOK, users[start:end], page, total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapUsersManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid user id", nil)
		return
	}

	u, err := model.GetUserByID(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "user not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "internal server error", nil)
		return
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": u})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapUsersManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	var inp userInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields := make(map[string]string)
	if strings.TrimSpace(inp.Username) == "" {
		fields["username"] = "required"
	}
	if strings.TrimSpace(inp.FullName) == "" {
		fields["full_name"] = "required"
	}
	if inp.Password == "" {
		fields["password"] = "required"
	} else if len(inp.Password) < 8 {
		fields["password"] = "minimum 8 characters"
	}
	if inp.Role == "" {
		fields["role"] = "required"
	} else if _, err := model.GetRoleByName(h.DB, inp.Role); err != nil {
		fields["role"] = "invalid role"
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	hashed, err := auth.HashPassword(inp.Password)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to hash password", nil)
		return
	}

	isActive := true
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}

	u := &model.User{
		Username:           strings.TrimSpace(inp.Username),
		Password:           hashed,
		FullName:           strings.TrimSpace(inp.FullName),
		Role:               inp.Role,
		IsActive:           isActive,
		MustChangePassword: true,
	}

	if err := model.CreateUser(h.DB, u); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "username already exists", nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create user", nil)
		return
	}

	created, err := model.GetUserByUsername(h.DB, u.Username)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve created user", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "user.create",
		TargetType:  "user",
		TargetID:    int64(created.ID),
		TargetLabel: created.Username,
		Metadata: map[string]any{
			"after": map[string]any{
				"username":  created.Username,
				"full_name": created.FullName,
				"role":      created.Role,
				"is_active": created.IsActive,
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": created})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapUsersManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid user id", nil)
		return
	}

	existing, err := model.GetUserByID(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "user not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "internal server error", nil)
		return
	}

	var inp userInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields := make(map[string]string)
	if strings.TrimSpace(inp.FullName) == "" {
		fields["full_name"] = "required"
	}
	if inp.Role == "" {
		fields["role"] = "required"
	} else if _, err := model.GetRoleByName(h.DB, inp.Role); err != nil {
		fields["role"] = "invalid role"
	}
	if inp.Password != "" && len(inp.Password) < 8 {
		fields["password"] = "minimum 8 characters"
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	isActive := existing.IsActive
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}

	currentUser := auth.UserFromContext(r.Context())
	if currentUser != nil && currentUser.ID == id && !isActive {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "cannot deactivate your own account", nil)
		return
	}

	updated := &model.User{
		ID:       id,
		FullName: strings.TrimSpace(inp.FullName),
		Role:     inp.Role,
		IsActive: isActive,
	}
	if err := model.UpdateUser(h.DB, updated); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update user", nil)
		return
	}

	if inp.Password != "" {
		hashed, err := auth.HashPassword(inp.Password)
		if err != nil {
			v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to hash password", nil)
			return
		}
		if err := model.UpdateUserPassword(h.DB, id, hashed); err != nil {
			v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update password", nil)
			return
		}
		if currentUser == nil || currentUser.ID != id {
			if err := model.SetMustChangePassword(h.DB, id, true); err != nil {
				v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to set password change flag", nil)
				return
			}
		}
	}

	result, err := model.GetUserByID(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve updated user", nil)
		return
	}

	oldFields := map[string]any{"full_name": existing.FullName, "role": existing.Role, "is_active": existing.IsActive}
	newFields := map[string]any{"full_name": updated.FullName, "role": updated.Role, "is_active": updated.IsActive}
	if metadata := audit.Diff(oldFields, newFields, []string{"full_name", "role", "is_active"}); metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "user.update",
			TargetType:  "user",
			TargetID:    int64(id),
			TargetLabel: existing.Username,
			Metadata:    metadata,
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapUsersManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid user id", nil)
		return
	}

	currentUser := auth.UserFromContext(r.Context())
	if currentUser != nil && currentUser.ID == id {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "cannot deactivate your own account", nil)
		return
	}

	existing, err := model.GetUserByID(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "user not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "internal server error", nil)
		return
	}

	wasActive := existing.IsActive
	existing.IsActive = false
	if err := model.UpdateUser(h.DB, existing); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to deactivate user", nil)
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

	w.WriteHeader(http.StatusNoContent)
}
