package users

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/access"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Access *access.Module }

type userInput struct {
	Username string `json:"username"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	IsActive *bool  `json:"is_active"`
	Password string `json:"password"`
}

func actor(r *http.Request) access.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return access.Actor{}
	}
	return access.Actor{UserID: u.ID, CanManageUsers: v1.HasEffectiveCapability(r.Context(), model.CapUsersManage)}
}

func authorize(w http.ResponseWriter, r *http.Request) bool {
	if v1.HasEffectiveCapability(r.Context(), model.CapUsersManage) {
		return true
	}
	v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
	return false
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users", h.List)
	mux.HandleFunc("GET /api/v1/users/{id}", h.Get)
	mux.HandleFunc("POST /api/v1/users", h.Create)
	mux.HandleFunc("PUT /api/v1/users/{id}", h.Update)
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.Delete)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	page := v1.ParsePage(r)
	result, err := h.Access.ListUsers(r.Context(), actor(r), access.ListFilter{Limit: page.PerPage, Offset: page.Offset()})
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list users", nil)
		return
	}
	v1.WriteList(w, http.StatusOK, result.Users, page, result.Total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	id, ok := userID(w, r)
	if !ok {
		return
	}
	u, err := h.Access.GetUser(r.Context(), actor(r), id)
	if err != nil {
		writeAccessError(w, r, err, "user")
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": u})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	var inp userInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	if inp.Password != "" && len(inp.Password) < 8 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", map[string]string{"password": "minimum 8 characters"})
		return
	}
	isActive := true
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}
	u, err := h.Access.CreateUser(r.Context(), actor(r), access.UserDraft{Username: inp.Username, FullName: inp.FullName, Role: inp.Role, IsActive: isActive, Password: inp.Password})
	if err != nil {
		writeAccessError(w, r, err, "user")
		return
	}
	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": u})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	id, ok := userID(w, r)
	if !ok {
		return
	}
	existing, err := h.Access.GetUser(r.Context(), actor(r), id)
	if err != nil {
		writeAccessError(w, r, err, "user")
		return
	}
	var inp userInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	if inp.Password != "" && len(inp.Password) < 8 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", map[string]string{"password": "minimum 8 characters"})
		return
	}
	isActive := existing.IsActive
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}
	u, err := h.Access.UpdateUser(r.Context(), actor(r), id, access.UserDraft{FullName: inp.FullName, Role: inp.Role, IsActive: isActive, Password: inp.Password})
	if err != nil {
		writeAccessError(w, r, err, "user")
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": u})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !authorize(w, r) {
		return
	}
	id, ok := userID(w, r)
	if !ok {
		return
	}
	if _, err := h.Access.DeactivateUser(r.Context(), actor(r), id); err != nil {
		writeAccessError(w, r, err, "user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func userID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid user id", nil)
		return 0, false
	}
	return id, true
}

func writeAccessError(w http.ResponseWriter, r *http.Request, err error, noun string) {
	var validation *access.ValidationError
	var conflict *access.ConflictError
	switch {
	case errors.Is(err, access.ErrNotFound):
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, noun+" not found", nil)
	case errors.Is(err, access.ErrForbidden):
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
	case errors.As(err, &validation):
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", validation.Fields)
	case errors.As(err, &conflict):
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, conflict.Message, nil)
	default:
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "internal server error", nil)
	}
}
