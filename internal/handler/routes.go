package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// RegisterAuthRoutes installs the public HTML login endpoints and logout.
func (h *Handler) RegisterAuthRoutes(mux *http.ServeMux, loginMiddleware func(http.Handler) http.Handler) {
	mux.HandleFunc("GET "+h.BasePath+"/login", h.LoginPage)
	mux.Handle("POST "+h.BasePath+"/login", loginMiddleware(http.HandlerFunc(h.Login)))
	mux.HandleFunc("POST "+h.BasePath+"/logout", h.Logout)
}

// RegisterAccessRoutes installs user, role, and self-service password routes.
func (h *Handler) RegisterAccessRoutes(mux *http.ServeMux) {
	users := http.NewServeMux()
	users.HandleFunc("GET /users", h.ListUsers)
	users.HandleFunc("GET /users/new", h.NewUser)
	users.HandleFunc("POST /users", h.CreateUser)
	users.HandleFunc("GET /users/{id}/edit", h.EditUser)
	users.HandleFunc("POST /users/{id}", h.UpdateUser)
	users.HandleFunc("DELETE /users/{id}", h.DeleteUser)
	mux.Handle("/users", auth.RequireCapability(model.CapUsersManage)(users))
	mux.Handle("/users/", auth.RequireCapability(model.CapUsersManage)(users))

	roles := http.NewServeMux()
	roles.HandleFunc("GET /roles", h.ListRoles)
	roles.HandleFunc("GET /roles/new", h.NewRole)
	roles.HandleFunc("POST /roles", h.CreateRole)
	roles.HandleFunc("GET /roles/{name}/edit", h.EditRole)
	roles.HandleFunc("POST /roles/{name}", h.UpdateRole)
	roles.HandleFunc("DELETE /roles/{name}", h.DeleteRole)
	mux.Handle("/roles", auth.RequireCapability(model.CapRolesManage)(roles))
	mux.Handle("/roles/", auth.RequireCapability(model.CapRolesManage)(roles))

	mux.HandleFunc("GET /password/change", h.PasswordChangePage)
	mux.HandleFunc("POST /password/change", h.PasswordChange)
}

// RegisterSettingsRoutes installs administrative settings, integrations, and audit routes.
func (h *Handler) RegisterSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/api-tokens", auth.AdminOnly(h.ListAPITokens))
	mux.HandleFunc("GET /settings/api-tokens/new", auth.AdminOnly(h.NewAPIToken))
	mux.HandleFunc("GET /settings/api-tokens/created", auth.AdminOnly(h.CreatedAPIToken))
	mux.HandleFunc("POST /settings/api-tokens", auth.AdminOnly(h.CreateAPIToken))
	mux.HandleFunc("POST /settings/api-tokens/{id}/revoke", auth.AdminOnly(h.RevokeAPIToken))

	h.RegisterCompanyRoutes(mux)
	mux.HandleFunc("GET /settings/school-calendar", auth.AdminOnly(h.SchoolCalendarPage))
	mux.HandleFunc("POST /settings/school-calendar/closures", auth.AdminOnly(h.CreateSchoolClosure))
	mux.HandleFunc("POST /settings/school-calendar/closures/{id}/delete", auth.AdminOnly(h.DeleteSchoolClosure))
	mux.HandleFunc("POST /settings/school-calendar/google-calendar-id", auth.AdminOnly(h.SaveGoogleCalendarID))
	mux.HandleFunc("POST /integrations/google-calendar/connect", auth.AdminOnly(h.ConnectGoogleCalendar))
	mux.HandleFunc("GET /integrations/google-calendar/callback", auth.AdminOnly(h.GoogleCalendarCallback))
	mux.HandleFunc("POST /integrations/google-calendar/sync", auth.AdminOnly(h.SyncGoogleCalendar))
	mux.HandleFunc("POST /integrations/google-calendar/disconnect", auth.AdminOnly(h.DisconnectGoogleCalendar))
	mux.HandleFunc("GET /audit", auth.CapabilityOnly(model.CapAuditView, h.AuditList))
}

// RegisterProtectedRoutes is the production route manifest for the HTML application.
func (h *Handler) RegisterProtectedRoutes(mux *http.ServeMux) {
	h.RegisterReportingRoutes(mux)
	h.RegisterAccountRoutes(mux)
	h.RegisterContactRoutes(mux)
	h.RegisterAccountingRoutes(mux)
	h.RegisterReceivablesPayablesRoutes(mux)
	h.RegisterInvoiceRoutes(mux)
	h.RegisterAccessRoutes(mux)
	h.RegisterSettingsRoutes(mux)
}
