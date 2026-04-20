package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	latasyaerp "github.com/naufal/latasya-erp"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/tmpl"
)

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

	// Auth routes (no auth required)
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)
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
	protected.HandleFunc("GET /invoices/{id}", h.ViewInvoice)
	protected.HandleFunc("GET /invoices/{id}/edit", h.EditInvoice)
	protected.HandleFunc("POST /invoices/{id}", auth.CapabilityOnly(model.CapInvoicesManage, h.UpdateInvoice))
	protected.HandleFunc("DELETE /invoices/{id}", auth.CapabilityOnly(model.CapInvoicesManage, h.DeleteInvoice))
	protected.HandleFunc("POST /invoices/{id}/send", auth.CapabilityOnly(model.CapInvoicesManage, h.SendInvoice))
	protected.HandleFunc("POST /invoices/{id}/payment", auth.CapabilityOnly(model.CapInvoicesManage, h.InvoicePayment))
	protected.HandleFunc("GET /invoices/{id}/print", h.PrintInvoice)

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

	// Password change (self-service + forced on first login)
	protected.HandleFunc("GET /password/change", h.PasswordChangePage)
	protected.HandleFunc("POST /password/change", h.PasswordChange)

	mux.Handle("/", auth.RequireAuth(db, auth.CSRFProtect(handler.EnforcePasswordChange(protected))))

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
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
