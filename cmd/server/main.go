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

	// Accounts
	protected.HandleFunc("GET /accounts", h.ListAccounts)
	protected.HandleFunc("GET /accounts/new", h.NewAccount)
	protected.HandleFunc("POST /accounts", h.CreateAccount)
	protected.HandleFunc("GET /accounts/{id}/edit", h.EditAccount)
	protected.HandleFunc("POST /accounts/{id}", h.UpdateAccount)
	protected.HandleFunc("DELETE /accounts/{id}", h.DeleteAccount)

	// Contacts
	protected.HandleFunc("GET /contacts", h.ListContacts)
	protected.HandleFunc("GET /contacts/new", h.NewContact)
	protected.HandleFunc("POST /contacts", h.CreateContact)
	protected.HandleFunc("GET /contacts/{id}/edit", h.EditContact)
	protected.HandleFunc("POST /contacts/{id}", h.UpdateContact)
	protected.HandleFunc("DELETE /contacts/{id}", h.DeleteContact)

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
