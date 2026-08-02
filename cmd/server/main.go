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
	"github.com/naufal/latasya-erp/internal/access"
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
	"github.com/naufal/latasya-erp/internal/apitoken"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/bill"
	companyModule "github.com/naufal/latasya-erp/internal/company"
	contactModule "github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/creditnote"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/idempotency"
	"github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/reporting"
	"github.com/naufal/latasya-erp/internal/schoolcalendar"
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
	idempotencyStore := idempotency.New(db)

	go auth.CleanExpiredSessions(db)
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			idempotencyStore.CleanExpired(context.Background())
		}
	}()

	invoiceModule := invoice.New(db)
	journalModule := journal.New(db)
	billModule := bill.New(db)
	creditNoteModule := creditnote.New(db)
	accountModule := account.New(db)
	accessModule := access.New(db, auth.HashPassword)
	apiTokenModule := apitoken.New(db)
	auditModule := audit.New(db)
	schoolCalendarModule := schoolcalendar.New(db)
	reportingModule := reporting.New(db)
	contactsModule := contactModule.New(db)
	companyProfileModule := companyModule.New(db)
	h := &handler.Handler{
		DB:             db,
		TemplateFS:     latasyaerp.TemplateFS,
		FuncMap:        tmpl.FuncMap(),
		DevMode:        devMode,
		BasePath:       "/dashboard",
		Invoices:       invoiceModule,
		Journals:       journalModule,
		Bills:          billModule,
		CreditNotes:    creditNoteModule,
		Accounts:       accountModule,
		Access:         accessModule,
		APITokens:      apiTokenModule,
		Audit:          auditModule,
		SchoolCalendar: schoolCalendarModule,
		Contacts:       contactsModule,
		Company:        companyProfileModule,
		Reporting:      reportingModule,
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
	authAPI.RegisterRoutes(apiMux)

	accts := &v1accounts.Handler{Accounts: accountModule}
	accts.RegisterRoutes(apiMux)

	contacts := &v1contacts.Handler{Contacts: contactsModule}
	contacts.RegisterRoutes(apiMux)

	idem := v1.Idempotency(idempotencyStore)

	incomeAPI := &v1income.Handler{Journals: journalModule}
	incomeAPI.RegisterRoutes(apiMux, idem)

	expensesAPI := &v1expenses.Handler{Journals: journalModule}
	expensesAPI.RegisterRoutes(apiMux, idem)

	journalsAPI := &v1journals.Handler{Journals: journalModule}
	journalsAPI.RegisterRoutes(apiMux, idem)

	invoicesAPI := &v1invoices.Handler{Invoices: invoiceModule}
	invoicesAPI.RegisterRoutes(apiMux, idem)

	apiTokensAPI := &v1apitokens.Handler{Tokens: apiTokenModule}
	apiTokensAPI.RegisterRoutes(apiMux, idem)

	bills := &v1bills.Handler{Bills: billModule}
	bills.RegisterRoutes(apiMux, idem)

	creditNotes := &v1creditnotes.Handler{CreditNotes: creditNoteModule}
	creditNotes.RegisterRoutes(apiMux, idem)

	reportsAPI := &v1reports.Handler{Reporting: reportingModule}
	reportsAPI.RegisterRoutes(apiMux)

	usersAPI := &v1users.Handler{Access: accessModule}
	usersAPI.RegisterRoutes(apiMux)

	rolesAPI := &v1roles.Handler{Access: accessModule}
	rolesAPI.RegisterRoutes(apiMux)

	auditAPI := &v1audit.Handler{Audit: auditModule}
	auditAPI.RegisterRoutes(apiMux)

	dashboardAPI := &v1dashboard.Handler{Reporting: reportingModule}
	dashboardAPI.RegisterRoutes(apiMux)

	schoolCalendarAPI := &v1schoolcalendar.Handler{Calendar: schoolCalendarModule, GoogleCalendarConfig: h.GoogleCalendarConfig}
	schoolCalendarAPI.RegisterRoutes(apiMux)

	mux.Handle("/api/v1/", v1.BearerOrCookie(db)(apiMux))
	authAPI.RegisterLoginRoute(mux, v1.LoginRateLimiter())

	// Public site: company profile at the bare domain, plus the parent
	// invoice portal. No auth required.
	// Short parent link. Rate limited: guessable code, no login behind it.
	// One limiter instance, so both routes share a bucket per IP.
	portalLimiter := v1.PortalCodeLimiter()
	h.RegisterPublicRoutes(mux, portalLimiter)

	// Auth routes (no auth required)
	h.RegisterAuthRoutes(mux, v1.LoginRateLimiter())

	// Protected routes (any authenticated user), mounted under BasePath.
	// StripPrefix removes it before dispatch, so every pattern below and
	// every handler's PathValue/r.URL.Path logic is unprefixed and
	// untouched — only outbound redirects and rendered links need it,
	// via h.BasePath.
	protected := http.NewServeMux()
	h.RegisterProtectedRoutes(protected)

	mux.Handle(h.BasePath+"/", http.StripPrefix(h.BasePath, auth.RequireAuth(db, accessModule, auth.CSRFProtect(h.EnforcePasswordChange(protected)))))

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
