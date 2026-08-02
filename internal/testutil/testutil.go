package testutil

import (
	"database/sql"
	"sync"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/tmpl"

	latasyaerp "github.com/naufal/latasya-erp"
)

// Parallel tests all register the same FS; once is enough and keeps the
// global write out of the race detector's way.
var setMigrations = sync.OnceFunc(func() {
	database.SetMigrations(latasyaerp.MigrationFS)
})

// SetupTestDB creates an in-memory SQLite database with migrations and seed data applied.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	setMigrations()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	if err := database.Seed(db); err != nil {
		t.Fatalf("seed test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// SetupTestHandler creates a Handler configured for testing with real templates.
func SetupTestHandler(t *testing.T, db *sql.DB) *handler.Handler {
	t.Helper()
	return &handler.Handler{
		DB:         db,
		TemplateFS: latasyaerp.TemplateFS,
		FuncMap:    tmpl.FuncMap(),
		DevMode:    true,
		Invoices:   invoice.New(db),
	}
}

// CreateTestUser creates a user in the test database and returns the user ID.
func CreateTestUser(t *testing.T, db *sql.DB, username, password, role string) int {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	result, err := db.Exec(
		"INSERT INTO users (username, password, full_name, role) VALUES (?, ?, ?, ?)",
		username, hash, username, role,
	)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

// CreateTestSession creates a session for the given user and returns the session ID.
func CreateTestSession(t *testing.T, db *sql.DB, userID int) string {
	t.Helper()
	sessionID, err := auth.CreateSession(db, userID)
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}
	return sessionID
}
