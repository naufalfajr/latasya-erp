package roles

import (
	"errors"
	"net/http"

	"github.com/naufal/latasya-erp/internal/access"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Access *access.Module }

type roleInput struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
}

func roleActor(r *http.Request) access.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return access.Actor{}
	}
	return access.Actor{UserID: u.ID, CanManageRoles: v1.HasEffectiveCapability(r.Context(), model.CapRolesManage)}
}

func roleAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if v1.HasEffectiveCapability(r.Context(), model.CapRolesManage) {
		return true
	}
	v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
	return false
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/roles", h.List)
	mux.HandleFunc("GET /api/v1/roles/capabilities", h.Capabilities)
	mux.HandleFunc("GET /api/v1/roles/{name}", h.Get)
	mux.HandleFunc("POST /api/v1/roles", h.Create)
	mux.HandleFunc("PUT /api/v1/roles/{name}", h.Update)
	mux.HandleFunc("DELETE /api/v1/roles/{name}", h.Delete)
}

func (h *Handler) Capabilities(w http.ResponseWriter, r *http.Request) {
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": model.AllCapabilities})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if !roleAuthorized(w, r) {
		return
	}
	page := v1.ParsePage(r)
	result, err := h.Access.ListRoles(r.Context(), roleActor(r), access.ListFilter{Limit: page.PerPage, Offset: page.Offset()})
	if err != nil {
		roleError(w, r, err)
		return
	}
	v1.WriteList(w, http.StatusOK, result.Roles, page, result.Total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !roleAuthorized(w, r) {
		return
	}
	role, err := h.Access.GetRole(r.Context(), roleActor(r), r.PathValue("name"))
	if err != nil {
		roleError(w, r, err)
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": role})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !roleAuthorized(w, r) {
		return
	}
	var inp roleInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	role, err := h.Access.CreateRole(r.Context(), roleActor(r), access.RoleDraft{Name: inp.Name, Description: inp.Description, Capabilities: inp.Capabilities})
	if err != nil {
		var conflict *access.ConflictError
		if errors.As(err, &conflict) {
			v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", map[string]string{"name": "role name already exists"})
			return
		}
		roleError(w, r, err)
		return
	}
	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": role})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !roleAuthorized(w, r) {
		return
	}
	var inp roleInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	role, err := h.Access.UpdateRole(r.Context(), roleActor(r), r.PathValue("name"), access.RoleDraft{Description: inp.Description, Capabilities: inp.Capabilities})
	if err != nil {
		roleError(w, r, err)
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": role})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !roleAuthorized(w, r) {
		return
	}
	if _, err := h.Access.DeleteRole(r.Context(), roleActor(r), r.PathValue("name")); err != nil {
		roleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func roleError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *access.ValidationError
	var conflict *access.ConflictError
	switch {
	case errors.Is(err, access.ErrNotFound):
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "role not found", nil)
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
