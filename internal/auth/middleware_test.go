package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestRequireAuth_NoSession(t *testing.T) {
	db := testutil.SetupTestDB(t)

	handler := auth.RequireAuth(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected redirect to /login, got %q", loc)
	}
}

func TestRequireAuth_InvalidSession(t *testing.T) {
	db := testutil.SetupTestDB(t)

	handler := auth.RequireAuth(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "invalid-session"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", rr.Code)
	}
}

func TestRequireAuth_ValidSession(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var reached bool
	handler := auth.RequireAuth(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		user := auth.UserFromContext(r.Context())
		if user == nil {
			t.Error("expected user in context")
		}
		if user.Username != "admin" {
			t.Errorf("expected admin user, got %q", user.Username)
		}
		w.WriteHeader(http.StatusOK)
	}))

	var userID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)
	sessionID, _ := auth.CreateSession(db, userID)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !reached {
		t.Error("handler was not reached")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAdmin_AdminAllowed(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var reached bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.RequireAuth(db, auth.RequireAdmin(inner))

	var userID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)
	sessionID, _ := auth.CreateSession(db, userID)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !reached {
		t.Error("admin handler was not reached")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAdmin_ViewerDenied(t *testing.T) {
	db := testutil.SetupTestDB(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("viewer should not reach admin handler")
	})

	handler := auth.RequireAuth(db, auth.RequireAdmin(inner))

	viewerID := testutil.CreateTestUser(t, db, "testviewer", "pass", "viewer")
	sessionID := testutil.CreateTestSession(t, db, viewerID)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", rr.Code)
	}
}

func TestRequireCapability_AdminAllowedForAnyCap(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var reached bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.RequireAuth(db, auth.RequireCapability("accounts.manage")(inner))

	var userID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)
	sessionID, _ := auth.CreateSession(db, userID)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !reached {
		t.Error("admin should pass any capability check")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireCapability_BookkeeperAllowedForGrantedCap(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var reached bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.RequireAuth(db, auth.RequireCapability("invoices.manage")(inner))

	bkID := testutil.CreateTestUser(t, db, "bk", "pw", "bookkeeper")
	sessionID := testutil.CreateTestSession(t, db, bkID)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !reached {
		t.Error("bookkeeper should pass invoices.manage check")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireCapability_BookkeeperDeniedForUngrantedCap(t *testing.T) {
	db := testutil.SetupTestDB(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("bookkeeper should not pass users.manage")
	})

	handler := auth.RequireAuth(db, auth.RequireCapability("users.manage")(inner))

	bkID := testutil.CreateTestUser(t, db, "bk", "pw", "bookkeeper")
	sessionID := testutil.CreateTestSession(t, db, bkID)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRequireCapability_ViewerDeniedForWriteCaps(t *testing.T) {
	db := testutil.SetupTestDB(t)

	for _, cap := range []string{"invoices.manage", "bills.manage", "accounts.manage", "users.manage"} {
		t.Run(cap, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("viewer should not pass %s", cap)
			})
			handler := auth.RequireAuth(db, auth.RequireCapability(cap)(inner))

			viewerID := testutil.CreateTestUser(t, db, "viewer_"+cap, "pw", "viewer")
			sessionID := testutil.CreateTestSession(t, db, viewerID)

			req := httptest.NewRequest("GET", "/", nil)
			req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("expected 403, got %d", rr.Code)
			}
		})
	}
}

func TestCapabilityOnly_DeniesWithoutCapability(t *testing.T) {
	db := testutil.SetupTestDB(t)

	fn := auth.CapabilityOnly("invoices.manage", func(w http.ResponseWriter, r *http.Request) {
		t.Error("viewer should not reach handler")
	})
	handler := auth.RequireAuth(db, http.HandlerFunc(fn))

	viewerID := testutil.CreateTestUser(t, db, "vonly", "pw", "viewer")
	sessionID := testutil.CreateTestSession(t, db, viewerID)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRequireAuth_InactiveUser(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Create an inactive user
	hash, _ := auth.HashPassword("pass")
	result, _ := db.Exec(
		"INSERT INTO users (username, password, full_name, role, is_active) VALUES (?, ?, ?, ?, ?)",
		"inactive", hash, "Inactive User", "admin", 0,
	)
	userID, _ := result.LastInsertId()
	sessionID, _ := auth.CreateSession(db, int(userID))

	handler := auth.RequireAuth(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inactive user should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", rr.Code)
	}
}
