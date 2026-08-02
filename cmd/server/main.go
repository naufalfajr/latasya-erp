package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	latasyaerp "github.com/naufal/latasya-erp"
	"github.com/naufal/latasya-erp/internal/account"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	v1accounts "github.com/naufal/latasya-erp/internal/api/v1/accounts"
	v1apitokens "github.com/naufal/latasya-erp/internal/api/v1/apitokens"
	v1audit "github.com/naufal/latasya-erp/internal/api/v1/audit"
	v1auth "github.com/naufal/latasya-erp/internal/api/v1/auth"
	v1bills "github.com/naufal/latasya-erp/internal/api/v1/bills"
	v1contacts "github.com/naufal/latasya-erp/internal/api/v1/contacts"
	v1creditnotes "github.com/naufal/latasya-erp/internal/api/v1/credit_notes"
	v1dashboard "github.com/naufal/latasya-erp/internal/api/v1/dashboard"
	v1expenses "github.com/naufal/latasya-erp/internal/api/v1/expenses"
	v1income "github.com/naufal/latasya-erp/internal/api/v1/income"
	v1invoices "github.com/naufal/latasya-erp/internal/api/v1/invoices"
	v1journals "github.com/naufal/latasya-erp/internal/api/v1/journals"
	v1reports "github.com/naufal/latasya-erp/internal/api/v1/reports"
	v1roles "github.com/naufal/latasya-erp/internal/api/v1/roles"
	v1schoolcalendar "github.com/naufal/latasya-erp/internal/api/v1/school_calendar"
	v1users "github.com/naufal/latasya-erp/internal/api/v1/users"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/bill"
	companyModule "github.com/naufal/latasya-erp/internal/company"
	contactModule "github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/creditnote"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/tmpl"
)

// version identifies the build. Overridden at link time via
// `-ldflags "-X main.version=<sha>"`; stays "dev" for local `go run`.
var version = "dev"

func main() {
	port := envOr("PORT", "8080")
	dbPath := envOr("DB_PATH", "./latasya.db")
	devMode := os.Getenv("DEV_MODE") == "true"

	// Open database
	database.SetMigrations(latasyaerp.MigrationFS)
	db, err := database.Open(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := database.Seed(db); err != nil {
		slog.Error("failed to seed database", "error", err)
		os.Exit(1)
	}

	go auth.CleanExpiredSessions(db)
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			model.CleanExpiredIdempotencyKeys(db)
		}
	}()

	invoiceModule := invoice.New(db)
	journalModule := journal.New(db)
	billModule := bill.New(db)
	creditNoteModule := creditnote.New(db)
	accountModule := account.New(db)
	contactsModule := contactModule.New(db)
	companyProfileModule := companyModule.New(db)
	h := &handler.Handler{
		DB:          db,
		TemplateFS:  latasyaerp.TemplateFS,
		FuncMap:     tmpl.FuncMap(),
		DevMode:     devMode,
		BasePath:    "/dashboard",
		Invoices:    invoiceModule,
		Journals:    journalModule,
		Bills:       billModule,
		CreditNotes: creditNoteModule,
		Accounts:    accountModule,
		Contacts:    contactsModule,
		Company:     companyProfileModule,
		GoogleCalendarConfig: googlecalendar.Config{
			ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		},
	}
	auth.SetLoginPath(h.BasePath + "/login")

	mux := http.NewServeMux()

	// Static files
	staticSub, _ := fs.Sub(latasyaerp.StaticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	// Health probe (no auth). Returns the build SHA and the count of applied
	// migrations so deploy verification can confirm the right binary is live
	// and its schema ran.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		var migrations int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil {
			http.Error(w, "db unreachable", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "ok version=%s migrations=%d\n", version, migrations)
	})

	// API v1 mux: BearerOrCookie auth, no CSRF on Bearer path.
	// Wave 2 tasks register domain endpoints on apiMux.
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/v1/openapi.yaml", v1.ServeOpenAPI)

	// Auth API (T11). Login is unauthenticated and lives on the outer mux so
	// it bypasses BearerOrCookie; the rest are wired through apiMux so they
	// inherit the standard auth + audit pipeline.
	authAPI := v1auth.New(db, devMode)
	apiMux.HandleFunc("POST /api/v1/auth/logout", authAPI.Logout)
	apiMux.HandleFunc("GET /api/v1/auth/me", authAPI.Me)
	apiMux.HandleFunc("GET /api/v1/auth/csrf", authAPI.CSRF)
	apiMux.HandleFunc("POST /api/v1/auth/password/change", authAPI.PasswordChange)

	accts := &v1accounts.Handler{Accounts: accountModule}
	accts.RegisterRoutes(apiMux)

	contacts := &v1contacts.Handler{Contacts: contactsModule}
	contacts.RegisterRoutes(apiMux)

	idem := v1.Idempotency(db)

	incomeAPI := &v1income.Handler{Journals: journalModule}
	incomeAPI.RegisterRoutes(apiMux, idem)

	expensesAPI := &v1expenses.Handler{Journals: journalModule}
	expensesAPI.RegisterRoutes(apiMux, idem)

	journalsAPI := &v1journals.Handler{Journals: journalModule}
	journalsAPI.RegisterRoutes(apiMux, idem)

	invoicesAPI := &v1invoices.Handler{Invoices: invoiceModule}
	invoicesAPI.RegisterRoutes(apiMux, idem)

	apiTokensAPI := &v1apitokens.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/api-tokens", apiTokensAPI.List)
	apiMux.Handle("POST /api/v1/api-tokens", idem(http.HandlerFunc(apiTokensAPI.Create)))
	apiMux.HandleFunc("DELETE /api/v1/api-tokens/{id}", apiTokensAPI.Revoke)

	bills := &v1bills.Handler{Bills: billModule}
	bills.RegisterRoutes(apiMux, idem)

	creditNotes := &v1creditnotes.Handler{CreditNotes: creditNoteModule}
	creditNotes.RegisterRoutes(apiMux, idem)

	reportsAPI := &v1reports.Handler{DB: db, Accounts: accountModule}
	apiMux.HandleFunc("GET /api/v1/reports/trial-balance", reportsAPI.TrialBalance)
	apiMux.HandleFunc("GET /api/v1/reports/profit-loss", reportsAPI.ProfitLoss)
	apiMux.HandleFunc("GET /api/v1/reports/balance-sheet", reportsAPI.BalanceSheet)
	apiMux.HandleFunc("GET /api/v1/reports/cash-flow", reportsAPI.CashFlow)
	apiMux.HandleFunc("GET /api/v1/reports/general-ledger", reportsAPI.GeneralLedger)

	usersAPI := &v1users.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/users", usersAPI.List)
	apiMux.HandleFunc("GET /api/v1/users/{id}", usersAPI.Get)
	apiMux.HandleFunc("POST /api/v1/users", usersAPI.Create)
	apiMux.HandleFunc("PUT /api/v1/users/{id}", usersAPI.Update)
	apiMux.HandleFunc("DELETE /api/v1/users/{id}", usersAPI.Delete)

	rolesAPI := &v1roles.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/roles", rolesAPI.List)
	apiMux.HandleFunc("GET /api/v1/roles/capabilities", rolesAPI.Capabilities)
	apiMux.HandleFunc("GET /api/v1/roles/{name}", rolesAPI.Get)
	apiMux.HandleFunc("POST /api/v1/roles", rolesAPI.Create)
	apiMux.HandleFunc("PUT /api/v1/roles/{name}", rolesAPI.Update)
	apiMux.HandleFunc("DELETE /api/v1/roles/{name}", rolesAPI.Delete)

	auditAPI := &v1audit.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/audit", auditAPI.List)

	dashboardAPI := &v1dashboard.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/dashboard", dashboardAPI.Get)

	schoolCalendarAPI := &v1schoolcalendar.Handler{DB: db, GoogleCalendarConfig: h.GoogleCalendarConfig}
	apiMux.HandleFunc("GET /api/v1/school-calendar/closures", schoolCalendarAPI.ListClosures)
	apiMux.HandleFunc("POST /api/v1/school-calendar/closures", schoolCalendarAPI.CreateClosure)
	apiMux.HandleFunc("DELETE /api/v1/school-calendar/closures/{id}", schoolCalendarAPI.DeleteClosure)
	apiMux.HandleFunc("GET /api/v1/school-calendar/effective-days", schoolCalendarAPI.EffectiveDays)
	apiMux.HandleFunc("POST /api/v1/integrations/google-calendar/sync", schoolCalendarAPI.SyncGoogleCalendar)

	mux.Handle("/api/v1/", v1.BearerOrCookie(db)(apiMux))
	mux.Handle("POST /api/v1/auth/login", v1.LoginRateLimiter()(http.HandlerFunc(authAPI.Login)))

	// Public site: company profile at the bare domain, plus the parent
	// invoice portal. No auth required.
	mux.HandleFunc("GET /{$}", h.PublicHome)
	// Short parent link. Rate limited: guessable code, no login behind it.
	// One limiter instance, so both routes share a bucket per IP.
	portalLimiter := v1.PortalCodeLimiter()
	mux.Handle("GET /p/{code}", portalLimiter(http.HandlerFunc(h.PortalIndex)))
	mux.Handle("GET /p/{code}/invoice/{id}/pdf", portalLimiter(http.HandlerFunc(h.PortalInvoicePDF)))

	// Auth routes (no auth required)
	mux.HandleFunc("GET /dashboard/login", h.LoginPage)
	mux.Handle("POST /dashboard/login", v1.LoginRateLimiter()(http.HandlerFunc(h.Login)))
	mux.HandleFunc("POST /dashboard/logout", h.Logout)

	// Protected routes (any authenticated user), mounted under BasePath.
	// StripPrefix removes it before dispatch, so every pattern below and
	// every handler's PathValue/r.URL.Path logic is unprefixed and
	// untouched — only outbound redirects and rendered links need it,
	// via h.BasePath.
	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", h.Dashboard)

	h.RegisterAccountRoutes(protected)

	h.RegisterContactRoutes(protected)

	h.RegisterAccountingRoutes(protected)
	h.RegisterReceivablesPayablesRoutes(protected)

	// Invoices
	h.RegisterInvoiceRoutes(protected)

	// Reports
	protected.HandleFunc("GET /reports/trial-balance", h.TrialBalance)
	protected.HandleFunc("GET /reports/profit-loss", h.ProfitLoss)
	protected.HandleFunc("GET /reports/balance-sheet", h.BalanceSheet)
	protected.HandleFunc("GET /reports/cash-flow", h.CashFlowReport)
	protected.HandleFunc("GET /reports/general-ledger", h.GeneralLedger)

	// User Management (requires users.manage capability — admin by default)
	userMux := http.NewServeMux()
	userMux.HandleFunc("GET /users", h.ListUsers)
	userMux.HandleFunc("GET /users/new", h.NewUser)
	userMux.HandleFunc("POST /users", h.CreateUser)
	userMux.HandleFunc("GET /users/{id}/edit", h.EditUser)
	userMux.HandleFunc("POST /users/{id}", h.UpdateUser)
	userMux.HandleFunc("DELETE /users/{id}", h.DeleteUser)
	protected.Handle("/users", auth.RequireCapability(model.CapUsersManage)(userMux))
	protected.Handle("/users/", auth.RequireCapability(model.CapUsersManage)(userMux))

	// Role Management (requires roles.manage capability — admin by default)
	roleMux := http.NewServeMux()
	roleMux.HandleFunc("GET /roles", h.ListRoles)
	roleMux.HandleFunc("GET /roles/new", h.NewRole)
	roleMux.HandleFunc("POST /roles", h.CreateRole)
	roleMux.HandleFunc("GET /roles/{name}/edit", h.EditRole)
	roleMux.HandleFunc("POST /roles/{name}", h.UpdateRole)
	roleMux.HandleFunc("DELETE /roles/{name}", h.DeleteRole)
	protected.Handle("/roles", auth.RequireCapability(model.CapRolesManage)(roleMux))
	protected.Handle("/roles/", auth.RequireCapability(model.CapRolesManage)(roleMux))

	// HTMX partials
	protected.HandleFunc("GET /htmx/bill-line", h.BillLinePartial)
	protected.HandleFunc("GET /htmx/credit-note-line", h.CreditNoteLinePartial)

	// Password change (self-service + forced on first login)
	protected.HandleFunc("GET /password/change", h.PasswordChangePage)
	protected.HandleFunc("POST /password/change", h.PasswordChange)

	// API Tokens management UI
	protected.HandleFunc("GET /settings/api-tokens", auth.AdminOnly(h.ListAPITokens))
	protected.HandleFunc("GET /settings/api-tokens/new", auth.AdminOnly(h.NewAPIToken))
	protected.HandleFunc("GET /settings/api-tokens/created", auth.AdminOnly(h.CreatedAPIToken))
	protected.HandleFunc("POST /settings/api-tokens", auth.AdminOnly(h.CreateAPIToken))
	protected.HandleFunc("POST /settings/api-tokens/{id}/revoke", auth.AdminOnly(h.RevokeAPIToken))

	// Company Profile (admin-only settings shown on invoices)
	h.RegisterCompanyRoutes(protected)

	// School Calendar (admin-only closures and Google Calendar integration)
	protected.HandleFunc("GET /settings/school-calendar", auth.AdminOnly(h.SchoolCalendarPage))
	protected.HandleFunc("POST /settings/school-calendar/closures", auth.AdminOnly(h.CreateSchoolClosure))
	protected.HandleFunc("POST /settings/school-calendar/closures/{id}/delete", auth.AdminOnly(h.DeleteSchoolClosure))
	protected.HandleFunc("POST /settings/school-calendar/google-calendar-id", auth.AdminOnly(h.SaveGoogleCalendarID))
	protected.HandleFunc("POST /integrations/google-calendar/connect", auth.AdminOnly(h.ConnectGoogleCalendar))
	protected.HandleFunc("GET /integrations/google-calendar/callback", auth.AdminOnly(h.GoogleCalendarCallback))
	protected.HandleFunc("POST /integrations/google-calendar/sync", auth.AdminOnly(h.SyncGoogleCalendar))
	protected.HandleFunc("POST /integrations/google-calendar/disconnect", auth.AdminOnly(h.DisconnectGoogleCalendar))

	// Audit log (admin-only via audit.view capability)
	protected.HandleFunc("GET /audit", auth.CapabilityOnly(model.CapAuditView, h.AuditList))

	mux.Handle(h.BasePath+"/", http.StripPrefix(h.BasePath, auth.RequireAuth(db, auth.CSRFProtect(h.EnforcePasswordChange(protected)))))

	// audit.RequestContext wraps everything so pre-auth events (login attempts)
	// still get a request_id and client IP attached.
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      audit.RequestContext(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("starting server", "port", port, "dev", devMode)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("server stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
