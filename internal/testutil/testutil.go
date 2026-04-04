package testutil

import (
	"database/sql"
	"embed"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/tmpl"

	latasyaerp "github.com/naufal/latasya-erp"
)

// SetupTestDB creates an in-memory SQLite database with migrations and seed data applied.
func SetupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database.SetMigrations(latasyaerp.MigrationFS)
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
	}
}

// SetupTestTemplates returns a minimal template set for tests that don't need real HTML.
func SetupTestTemplates() *template.Template {
	return template.Must(template.New("").Funcs(tmpl.FuncMap()).Parse(`
		{{define "base"}}{{block "content" .}}{{end}}{{end}}
		{{define "nav"}}{{end}}
		{{define "sidebar"}}{{end}}
		{{define "flash"}}{{end}}
	`))
}

// MustEmbedFS returns the embedded template FS from the root package.
func MustEmbedFS() embed.FS {
	return latasyaerp.TemplateFS
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

// AuthenticatedRequest creates an HTTP request with a valid session cookie for the admin user.
// Returns the request and the admin's user ID.
func AuthenticatedRequest(t *testing.T, db *sql.DB, method, path string, body *http.Request) (*http.Request, int) {
	t.Helper()
	// Get admin user ID
	var userID int
	err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)
	if err != nil {
		t.Fatalf("get admin user: %v", err)
	}
	sessionID := CreateTestSession(t, db, userID)
	req := httptest.NewRequest(method, path, nil)
	if body != nil {
		req = httptest.NewRequest(method, path, body.Body)
		req.Header = body.Header
	}
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	return req, userID
}

// AdminRequest creates an HTTP request authenticated as the seeded admin user.
func AdminRequest(t *testing.T, db *sql.DB, method, path string) *http.Request {
	t.Helper()
	req, _ := AuthenticatedRequest(t, db, method, path, nil)
	return req
}

// ViewerRequest creates an HTTP request authenticated as a viewer user.
func ViewerRequest(t *testing.T, db *sql.DB, method, path string) *http.Request {
	t.Helper()
	// Create viewer if not exists
	var viewerID int
	err := db.QueryRow("SELECT id FROM users WHERE username = 'viewer'").Scan(&viewerID)
	if err != nil {
		viewerID = CreateTestUser(t, db, "viewer", "viewer", "viewer")
	}
	sessionID := CreateTestSession(t, db, viewerID)
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	return req
}
