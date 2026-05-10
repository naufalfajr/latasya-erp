package v1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

type probeResult struct {
	Username      string   `json:"username"`
	IsBearer      bool     `json:"is_bearer"`
	TokenID       *int     `json:"token_id"`
	EffectiveCaps []string `json:"effective_caps"`
}

func probeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := auth.UserFromContext(r.Context())
		res := probeResult{
			IsBearer:      v1.IsBearerAuth(r.Context()),
			TokenID:       v1.TokenIDFromContext(r.Context()),
			EffectiveCaps: v1.EffectiveCapabilitiesFromContext(r.Context()),
		}
		if u != nil {
			res.Username = u.Username
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(res)
	})
}

func decodeProbe(t *testing.T, body []byte) probeResult {
	t.Helper()
	var p probeResult
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("decode probe: %v (body=%s)", err, body)
	}
	return p
}

func decodeError(t *testing.T, body []byte) v1.ErrorEnvelope {
	t.Helper()
	var e v1.ErrorEnvelope
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("decode error envelope: %v (body=%s)", err, body)
	}
	return e
}

func TestBearerOrCookie_Anon(t *testing.T) {
	db := testutil.SetupTestDB(t)
	h := v1.BearerOrCookie(db)(probeHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rr.Code)
	}
	env := decodeError(t, rr.Body.Bytes())
	if env.Code != v1.CodeUnauthorized {
		t.Errorf("code: got %q, want %q", env.Code, v1.CodeUnauthorized)
	}
}

func TestBearerOrCookie_ValidBearer(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "alice", "pw", model.RoleBookkeeper)

	tok, plaintext, err := model.CreateAPIToken(db, userID, "t", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	h := v1.BearerOrCookie(db)(probeHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	p := decodeProbe(t, rr.Body.Bytes())
	if !p.IsBearer {
		t.Errorf("IsBearer: got false, want true")
	}
	if p.Username != "alice" {
		t.Errorf("Username: got %q, want %q", p.Username, "alice")
	}
	if p.TokenID == nil || *p.TokenID != tok.ID {
		t.Errorf("TokenID: got %v, want %d", p.TokenID, tok.ID)
	}
	if !reflect.DeepEqual(p.EffectiveCaps, []string{model.CapReportsView}) {
		t.Errorf("EffectiveCaps: got %v, want [%s]", p.EffectiveCaps, model.CapReportsView)
	}
}

func TestBearerOrCookie_ExpiredBearer(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "exp", "pw", model.RoleBookkeeper)

	past := time.Now().Add(-1 * time.Hour)
	_, plaintext, err := model.CreateAPIToken(db, userID, "expired", []string{model.CapReportsView}, &past)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	h := v1.BearerOrCookie(db)(probeHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rr.Code)
	}
	env := decodeError(t, rr.Body.Bytes())
	if env.Code != v1.CodeInvalidToken {
		t.Errorf("code: got %q, want %q", env.Code, v1.CodeInvalidToken)
	}
}

func TestBearerOrCookie_RevokedBearer(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "rev", "pw", model.RoleBookkeeper)

	tok, plaintext, err := model.CreateAPIToken(db, userID, "revoke", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if err := model.RevokeAPIToken(db, userID, tok.ID); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}

	h := v1.BearerOrCookie(db)(probeHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rr.Code)
	}
	env := decodeError(t, rr.Body.Bytes())
	if env.Code != v1.CodeInvalidToken {
		t.Errorf("code: got %q, want %q", env.Code, v1.CodeInvalidToken)
	}
}

func TestBearerOrCookie_WrongHash(t *testing.T) {
	db := testutil.SetupTestDB(t)

	h := v1.BearerOrCookie(db)(probeHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set("Authorization", "Bearer lat_thisIsNotARealTokenAtAllJustGarbageX")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rr.Code)
	}
	env := decodeError(t, rr.Body.Bytes())
	if env.Code != v1.CodeInvalidToken {
		t.Errorf("code: got %q, want %q", env.Code, v1.CodeInvalidToken)
	}
}

func TestBearerOrCookie_BothPresent(t *testing.T) {
	db := testutil.SetupTestDB(t)

	bearerUserID := testutil.CreateTestUser(t, db, "bearer-user", "pw", model.RoleBookkeeper)
	cookieUserID := testutil.CreateTestUser(t, db, "cookie-user", "pw", model.RoleBookkeeper)

	_, plaintext, err := model.CreateAPIToken(db, bearerUserID, "t", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	sessionID := testutil.CreateTestSession(t, db, cookieUserID)

	h := v1.BearerOrCookie(db)(probeHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	p := decodeProbe(t, rr.Body.Bytes())
	if !p.IsBearer {
		t.Errorf("IsBearer: got false, want true (bearer must win)")
	}
	if p.Username != "bearer-user" {
		t.Errorf("Username: got %q, want %q (bearer must win)", p.Username, "bearer-user")
	}
}

func TestBearerOrCookie_ScopeIntersection(t *testing.T) {
	db := testutil.SetupTestDB(t)
	// Bookkeeper role does NOT have accounts.manage, but DOES have reports.view.
	userID := testutil.CreateTestUser(t, db, "scoped", "pw", model.RoleBookkeeper)

	// Token requests both reports.view (granted by role) and accounts.manage (NOT granted).
	tokenScopes := []string{model.CapReportsView, model.CapAccountsManage}
	_, plaintext, err := model.CreateAPIToken(db, userID, "t", tokenScopes, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	h := v1.BearerOrCookie(db)(probeHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	p := decodeProbe(t, rr.Body.Bytes())

	got := append([]string{}, p.EffectiveCaps...)
	sort.Strings(got)
	want := []string{model.CapReportsView}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EffectiveCaps: got %v, want %v (only role-granted scope survives intersection)", got, want)
	}
}

func TestBearerOrCookie_MustChangePassword(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "mcp-user", "pw", model.RoleBookkeeper)
	if err := model.SetMustChangePassword(db, userID, true); err != nil {
		t.Fatalf("SetMustChangePassword: %v", err)
	}

	_, plaintext, err := model.CreateAPIToken(db, userID, "t", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	h := v1.BearerOrCookie(db)(probeHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (bearer must NOT be blocked by must_change_password), body=%s",
			rr.Code, rr.Body.String())
	}
	p := decodeProbe(t, rr.Body.Bytes())
	if !p.IsBearer {
		t.Errorf("IsBearer: got false, want true")
	}
}
