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
	v1users "github.com/naufal/latasya-erp/internal/api/v1/users"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/handler"
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

	h := &handler.Handler{
		DB:         db,
		TemplateFS: latasyaerp.TemplateFS,
		FuncMap:    tmpl.FuncMap(),
		DevMode:    devMode,
	}

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

	accts := &v1accounts.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/accounts", accts.List)
	apiMux.HandleFunc("GET /api/v1/accounts/{id}", accts.Get)
	apiMux.HandleFunc("POST /api/v1/accounts", accts.Create)
	apiMux.HandleFunc("PUT /api/v1/accounts/{id}", accts.Update)
	apiMux.HandleFunc("DELETE /api/v1/accounts/{id}", accts.Delete)

	contacts := &v1contacts.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/contacts", contacts.List)
	apiMux.HandleFunc("GET /api/v1/contacts/{id}", contacts.Get)
	apiMux.HandleFunc("POST /api/v1/contacts", contacts.Create)
	apiMux.HandleFunc("PUT /api/v1/contacts/{id}", contacts.Update)
	apiMux.HandleFunc("DELETE /api/v1/contacts/{id}", contacts.Delete)

	idem := v1.Idempotency(db)

	incomeAPI := &v1income.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/income", incomeAPI.List)
	apiMux.HandleFunc("GET /api/v1/income/{id}", incomeAPI.Get)
	apiMux.Handle("POST /api/v1/income", idem(http.HandlerFunc(incomeAPI.Create)))
	apiMux.Handle("PUT /api/v1/income/{id}", idem(http.HandlerFunc(incomeAPI.Update)))
	apiMux.HandleFunc("DELETE /api/v1/income/{id}", incomeAPI.Delete)

	expensesAPI := &v1expenses.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/expenses", expensesAPI.List)
	apiMux.HandleFunc("GET /api/v1/expenses/{id}", expensesAPI.Get)
	apiMux.Handle("POST /api/v1/expenses", idem(http.HandlerFunc(expensesAPI.Create)))
	apiMux.Handle("PUT /api/v1/expenses/{id}", idem(http.HandlerFunc(expensesAPI.Update)))
	apiMux.HandleFunc("DELETE /api/v1/expenses/{id}", expensesAPI.Delete)

	journalsAPI := &v1journals.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/journals", journalsAPI.List)
	apiMux.HandleFunc("GET /api/v1/journals/{id}", journalsAPI.Get)
	apiMux.Handle("POST /api/v1/journals", idem(http.HandlerFunc(journalsAPI.Create)))
	apiMux.Handle("PUT /api/v1/journals/{id}", idem(http.HandlerFunc(journalsAPI.Update)))
	apiMux.HandleFunc("DELETE /api/v1/journals/{id}", journalsAPI.Delete)

	invoicesAPI := &v1invoices.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/invoices", invoicesAPI.List)
	apiMux.HandleFunc("GET /api/v1/invoices/{id}", invoicesAPI.Get)
	apiMux.Handle("POST /api/v1/invoices", idem(http.HandlerFunc(invoicesAPI.Create)))
	apiMux.Handle("PUT /api/v1/invoices/{id}", idem(http.HandlerFunc(invoicesAPI.Update)))
	apiMux.HandleFunc("DELETE /api/v1/invoices/{id}", invoicesAPI.Delete)
	apiMux.Handle("POST /api/v1/invoices/{id}/send", idem(http.HandlerFunc(invoicesAPI.Send)))
	apiMux.Handle("POST /api/v1/invoices/{id}/payment", idem(http.HandlerFunc(invoicesAPI.Payment)))
	apiMux.Handle("POST /api/v1/invoices/generate-recurring", idem(http.HandlerFunc(invoicesAPI.GenerateRecurring)))
	apiMux.HandleFunc("POST /api/v1/invoices/bulk-delete", invoicesAPI.BulkDelete)
	apiMux.HandleFunc("POST /api/v1/invoices/bulk-send", invoicesAPI.BulkSend)

	apiTokensAPI := &v1apitokens.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/api-tokens", apiTokensAPI.List)
	apiMux.Handle("POST /api/v1/api-tokens", idem(http.HandlerFunc(apiTokensAPI.Create)))
	apiMux.HandleFunc("DELETE /api/v1/api-tokens/{id}", apiTokensAPI.Revoke)

	bills := &v1bills.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/bills", bills.List)
	apiMux.HandleFunc("GET /api/v1/bills/{id}", bills.Get)
	apiMux.Handle("POST /api/v1/bills", idem(http.HandlerFunc(bills.Create)))
	apiMux.Handle("PUT /api/v1/bills/{id}", idem(http.HandlerFunc(bills.Update)))
	apiMux.HandleFunc("DELETE /api/v1/bills/{id}", bills.Delete)
	apiMux.Handle("POST /api/v1/bills/{id}/receive", idem(http.HandlerFunc(bills.Receive)))
	apiMux.Handle("POST /api/v1/bills/{id}/payment", idem(http.HandlerFunc(bills.Payment)))

	creditNotes := &v1creditnotes.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/credit-notes", creditNotes.List)
	apiMux.HandleFunc("GET /api/v1/credit-notes/{id}", creditNotes.Get)
	apiMux.Handle("POST /api/v1/credit-notes", idem(http.HandlerFunc(creditNotes.Create)))
	apiMux.Handle("PUT /api/v1/credit-notes/{id}", idem(http.HandlerFunc(creditNotes.Update)))
	apiMux.HandleFunc("DELETE /api/v1/credit-notes/{id}", creditNotes.Delete)
	apiMux.Handle("POST /api/v1/credit-notes/{id}/issue", idem(http.HandlerFunc(creditNotes.Issue)))
	apiMux.Handle("POST /api/v1/credit-notes/{id}/void", idem(http.HandlerFunc(creditNotes.Void)))

	reportsAPI := &v1reports.Handler{DB: db}
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

	mux.Handle("/api/v1/", v1.BearerOrCookie(db)(apiMux))
	mux.Handle("POST /api/v1/auth/login", v1.LoginRateLimiter()(http.HandlerFunc(authAPI.Login)))

	// Auth routes (no auth required)
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.Handle("POST /login", v1.LoginRateLimiter()(http.HandlerFunc(h.Login)))
	mux.HandleFunc("POST /logout", h.Logout)

	// Protected routes (any authenticated user)
	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", h.Dashboard)

	// Accounts (read: any user, write: requires accounts.manage)
	protected.HandleFunc("GET /accounts", h.ListAccounts)
	protected.HandleFunc("GET /accounts/new", h.NewAccount)
	protected.HandleFunc("POST /accounts", auth.CapabilityOnly(model.CapAccountsManage, h.CreateAccount))
	protected.HandleFunc("GET /accounts/{id}/edit", h.EditAccount)
	protected.HandleFunc("POST /accounts/{id}", auth.CapabilityOnly(model.CapAccountsManage, h.UpdateAccount))
	protected.HandleFunc("DELETE /accounts/{id}", auth.CapabilityOnly(model.CapAccountsManage, h.DeleteAccount))

	// Contacts
	protected.HandleFunc("GET /contacts", h.ListContacts)
	protected.HandleFunc("GET /contacts/new", h.NewContact)
	protected.HandleFunc("POST /contacts", auth.CapabilityOnly(model.CapContactsManage, h.CreateContact))
	protected.HandleFunc("GET /contacts/{id}/edit", h.EditContact)
	protected.HandleFunc("POST /contacts/{id}", auth.CapabilityOnly(model.CapContactsManage, h.UpdateContact))
	protected.HandleFunc("DELETE /contacts/{id}", auth.CapabilityOnly(model.CapContactsManage, h.DeleteContact))

	// Journal Entries
	protected.HandleFunc("GET /journals", h.ListJournals)
	protected.HandleFunc("GET /journals/new", h.NewJournal)
	protected.HandleFunc("POST /journals", auth.CapabilityOnly(model.CapJournalsManage, h.CreateJournal))
	protected.HandleFunc("GET /journals/{id}", h.ViewJournal)
	protected.HandleFunc("GET /journals/{id}/edit", h.EditJournal)
	protected.HandleFunc("POST /journals/{id}", auth.CapabilityOnly(model.CapJournalsManage, h.UpdateJournal))
	protected.HandleFunc("DELETE /journals/{id}", auth.CapabilityOnly(model.CapJournalsManage, h.DeleteJournal))

	// Income
	protected.HandleFunc("GET /income", h.ListIncome)
	protected.HandleFunc("GET /income/new", h.NewIncome)
	protected.HandleFunc("POST /income", auth.CapabilityOnly(model.CapIncomeManage, h.CreateIncome))
	protected.HandleFunc("GET /income/{id}/edit", h.EditIncome)
	protected.HandleFunc("POST /income/{id}", auth.CapabilityOnly(model.CapIncomeManage, h.UpdateIncome))
	protected.HandleFunc("DELETE /income/{id}", auth.CapabilityOnly(model.CapIncomeManage, h.DeleteIncome))

	// Expenses
	protected.HandleFunc("GET /expenses", h.ListExpenses)
	protected.HandleFunc("GET /expenses/new", h.NewExpense)
	protected.HandleFunc("POST /expenses", auth.CapabilityOnly(model.CapExpensesManage, h.CreateExpense))
	protected.HandleFunc("GET /expenses/{id}/edit", h.EditExpense)
	protected.HandleFunc("POST /expenses/{id}", auth.CapabilityOnly(model.CapExpensesManage, h.UpdateExpense))
	protected.HandleFunc("DELETE /expenses/{id}", auth.CapabilityOnly(model.CapExpensesManage, h.DeleteExpense))

	// Invoices
	protected.HandleFunc("GET /invoices", h.ListInvoices)
	protected.HandleFunc("GET /invoices/new", h.NewInvoice)
	protected.HandleFunc("POST /invoices", auth.CapabilityOnly(model.CapInvoicesManage, h.CreateInvoice))
	protected.HandleFunc("POST /invoices/generate-recurring", auth.CapabilityOnly(model.CapInvoicesManage, h.GenerateRecurringInvoices))
	protected.HandleFunc("POST /invoices/bulk-delete", auth.CapabilityOnly(model.CapInvoicesManage, h.BulkDeleteInvoices))
	protected.HandleFunc("POST /invoices/bulk-send", auth.CapabilityOnly(model.CapInvoicesManage, h.BulkSendInvoices))
	protected.HandleFunc("GET /invoices/{id}", h.ViewInvoice)
	protected.HandleFunc("GET /invoices/{id}/edit", h.EditInvoice)
	protected.HandleFunc("POST /invoices/{id}", auth.CapabilityOnly(model.CapInvoicesManage, h.UpdateInvoice))
	protected.HandleFunc("DELETE /invoices/{id}", auth.CapabilityOnly(model.CapInvoicesManage, h.DeleteInvoice))
	protected.HandleFunc("POST /invoices/{id}/send", auth.CapabilityOnly(model.CapInvoicesManage, h.SendInvoice))
	protected.HandleFunc("POST /invoices/{id}/payment", auth.CapabilityOnly(model.CapInvoicesManage, h.InvoicePayment))
	protected.HandleFunc("GET /invoices/{id}/print", h.PrintInvoice)

	// Credit Notes (piggyback on invoices.manage capability)
	protected.HandleFunc("GET /credit-notes", h.ListCreditNotes)
	protected.HandleFunc("GET /credit-notes/new", h.NewCreditNote)
	protected.HandleFunc("POST /credit-notes", auth.CapabilityOnly(model.CapInvoicesManage, h.CreateCreditNote))
	protected.HandleFunc("GET /credit-notes/{id}", h.ViewCreditNote)
	protected.HandleFunc("GET /credit-notes/{id}/edit", h.EditCreditNote)
	protected.HandleFunc("POST /credit-notes/{id}", auth.CapabilityOnly(model.CapInvoicesManage, h.UpdateCreditNote))
	protected.HandleFunc("DELETE /credit-notes/{id}", auth.CapabilityOnly(model.CapInvoicesManage, h.DeleteCreditNote))
	protected.HandleFunc("POST /credit-notes/{id}/issue", auth.CapabilityOnly(model.CapInvoicesManage, h.IssueCreditNote))
	protected.HandleFunc("POST /credit-notes/{id}/void", auth.CapabilityOnly(model.CapInvoicesManage, h.VoidCreditNote))

	// Bills
	protected.HandleFunc("GET /bills", h.ListBills)
	protected.HandleFunc("GET /bills/new", h.NewBill)
	protected.HandleFunc("POST /bills", auth.CapabilityOnly(model.CapBillsManage, h.CreateBill))
	protected.HandleFunc("GET /bills/{id}", h.ViewBill)
	protected.HandleFunc("GET /bills/{id}/edit", h.EditBill)
	protected.HandleFunc("POST /bills/{id}", auth.CapabilityOnly(model.CapBillsManage, h.UpdateBill))
	protected.HandleFunc("DELETE /bills/{id}", auth.CapabilityOnly(model.CapBillsManage, h.DeleteBill))
	protected.HandleFunc("POST /bills/{id}/receive", auth.CapabilityOnly(model.CapBillsManage, h.ReceiveBill))
	protected.HandleFunc("POST /bills/{id}/payment", auth.CapabilityOnly(model.CapBillsManage, h.BillPayment))

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
	protected.HandleFunc("GET /htmx/journal-line", h.JournalLinePartial)
	protected.HandleFunc("GET /htmx/invoice-line", h.InvoiceLinePartial)
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

	// Audit log (admin-only via audit.view capability)
	protected.HandleFunc("GET /audit", auth.CapabilityOnly(model.CapAuditView, h.AuditList))

	mux.Handle("/", auth.RequireAuth(db, auth.CSRFProtect(handler.EnforcePasswordChange(protected))))

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
