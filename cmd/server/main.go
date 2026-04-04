package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	latasyaerp "github.com/naufal/latasya-erp"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/handler"
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

	// Accounts (read: any user, write: admin only)
	protected.HandleFunc("GET /accounts", h.ListAccounts)
	protected.HandleFunc("GET /accounts/new", h.NewAccount)
	protected.HandleFunc("POST /accounts", auth.AdminOnly(h.CreateAccount))
	protected.HandleFunc("GET /accounts/{id}/edit", h.EditAccount)
	protected.HandleFunc("POST /accounts/{id}", auth.AdminOnly(h.UpdateAccount))
	protected.HandleFunc("DELETE /accounts/{id}", auth.AdminOnly(h.DeleteAccount))

	// Contacts
	protected.HandleFunc("GET /contacts", h.ListContacts)
	protected.HandleFunc("GET /contacts/new", h.NewContact)
	protected.HandleFunc("POST /contacts", auth.AdminOnly(h.CreateContact))
	protected.HandleFunc("GET /contacts/{id}/edit", h.EditContact)
	protected.HandleFunc("POST /contacts/{id}", auth.AdminOnly(h.UpdateContact))
	protected.HandleFunc("DELETE /contacts/{id}", auth.AdminOnly(h.DeleteContact))

	// Journal Entries
	protected.HandleFunc("GET /journals", h.ListJournals)
	protected.HandleFunc("GET /journals/new", h.NewJournal)
	protected.HandleFunc("POST /journals", auth.AdminOnly(h.CreateJournal))
	protected.HandleFunc("GET /journals/{id}", h.ViewJournal)
	protected.HandleFunc("GET /journals/{id}/edit", h.EditJournal)
	protected.HandleFunc("POST /journals/{id}", auth.AdminOnly(h.UpdateJournal))
	protected.HandleFunc("DELETE /journals/{id}", auth.AdminOnly(h.DeleteJournal))

	// Income
	protected.HandleFunc("GET /income", h.ListIncome)
	protected.HandleFunc("GET /income/new", h.NewIncome)
	protected.HandleFunc("POST /income", auth.AdminOnly(h.CreateIncome))
	protected.HandleFunc("GET /income/{id}/edit", h.EditIncome)
	protected.HandleFunc("POST /income/{id}", auth.AdminOnly(h.UpdateIncome))
	protected.HandleFunc("DELETE /income/{id}", auth.AdminOnly(h.DeleteIncome))

	// Expenses
	protected.HandleFunc("GET /expenses", h.ListExpenses)
	protected.HandleFunc("GET /expenses/new", h.NewExpense)
	protected.HandleFunc("POST /expenses", auth.AdminOnly(h.CreateExpense))
	protected.HandleFunc("GET /expenses/{id}/edit", h.EditExpense)
	protected.HandleFunc("POST /expenses/{id}", auth.AdminOnly(h.UpdateExpense))
	protected.HandleFunc("DELETE /expenses/{id}", auth.AdminOnly(h.DeleteExpense))

	// Invoices
	protected.HandleFunc("GET /invoices", h.ListInvoices)
	protected.HandleFunc("GET /invoices/new", h.NewInvoice)
	protected.HandleFunc("POST /invoices", auth.AdminOnly(h.CreateInvoice))
	protected.HandleFunc("GET /invoices/{id}", h.ViewInvoice)
	protected.HandleFunc("GET /invoices/{id}/edit", h.EditInvoice)
	protected.HandleFunc("POST /invoices/{id}", auth.AdminOnly(h.UpdateInvoice))
	protected.HandleFunc("DELETE /invoices/{id}", auth.AdminOnly(h.DeleteInvoice))
	protected.HandleFunc("POST /invoices/{id}/send", auth.AdminOnly(h.SendInvoice))
	protected.HandleFunc("POST /invoices/{id}/payment", auth.AdminOnly(h.InvoicePayment))
	protected.HandleFunc("GET /invoices/{id}/print", h.PrintInvoice)

	// Bills
	protected.HandleFunc("GET /bills", h.ListBills)
	protected.HandleFunc("GET /bills/new", h.NewBill)
	protected.HandleFunc("POST /bills", auth.AdminOnly(h.CreateBill))
	protected.HandleFunc("GET /bills/{id}", h.ViewBill)
	protected.HandleFunc("GET /bills/{id}/edit", h.EditBill)
	protected.HandleFunc("POST /bills/{id}", auth.AdminOnly(h.UpdateBill))
	protected.HandleFunc("DELETE /bills/{id}", auth.AdminOnly(h.DeleteBill))
	protected.HandleFunc("POST /bills/{id}/receive", auth.AdminOnly(h.ReceiveBill))
	protected.HandleFunc("POST /bills/{id}/payment", auth.AdminOnly(h.BillPayment))

	// Reports
	protected.HandleFunc("GET /reports/trial-balance", h.TrialBalance)
	protected.HandleFunc("GET /reports/profit-loss", h.ProfitLoss)
	protected.HandleFunc("GET /reports/balance-sheet", h.BalanceSheet)
	protected.HandleFunc("GET /reports/cash-flow", h.CashFlowReport)
	protected.HandleFunc("GET /reports/general-ledger", h.GeneralLedger)

	// User Management (admin only — enforced in handler via RequireAdmin)
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /users", h.ListUsers)
	adminMux.HandleFunc("GET /users/new", h.NewUser)
	adminMux.HandleFunc("POST /users", h.CreateUser)
	adminMux.HandleFunc("GET /users/{id}/edit", h.EditUser)
	adminMux.HandleFunc("POST /users/{id}", h.UpdateUser)
	adminMux.HandleFunc("DELETE /users/{id}", h.DeleteUser)
	protected.Handle("/users", auth.RequireAdmin(adminMux))
	protected.Handle("/users/", auth.RequireAdmin(adminMux))

	// HTMX partials
	protected.HandleFunc("GET /htmx/journal-line", h.JournalLinePartial)
	protected.HandleFunc("GET /htmx/invoice-line", h.InvoiceLinePartial)
	protected.HandleFunc("GET /htmx/bill-line", h.BillLinePartial)

	mux.Handle("/", auth.RequireAuth(db, protected))

	slog.Info("starting server", "port", port, "dev", devMode)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
