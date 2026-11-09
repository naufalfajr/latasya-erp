package oauthas_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/oauthas"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func pkce(t *testing.T) (verifier, challenge string) {
	t.Helper()
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func issue(t *testing.T, m *oauthas.Module, challenge string, scopes []string) string {
	t.Helper()
	code, err := m.Issue(context.Background(), oauthas.Grant{
		UserID: 1, Username: "admin", ClientID: "claude-desktop",
		RedirectURI: "http://127.0.0.1:59123/callback", CodeChallenge: challenge, Scopes: scopes,
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return code
}

func exchange(t *testing.T, m *oauthas.Module, code, verifier string) (token string, scopes []string, err error) {
	t.Helper()
	created, scopes, err := m.Exchange(context.Background(), code, verifier,
		"claude-desktop", "http://127.0.0.1:59123/callback")
	if err == nil {
		token = created.Plaintext
		if !strings.HasPrefix(token, "lat_") {
			t.Fatalf("minted token malformed: %q", token)
		}
	}
	return token, scopes, err
}

func TestExchange_HappyPathMintsWorkingToken(t *testing.T) {
	m := oauthas.New(testutil.SetupTestDB(t))
	verifier, challenge := pkce(t)
	code := issue(t, m, challenge, []string{model.CapReportsView})

	token, scopes, err := exchange(t, m, code, verifier)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if token == "" || len(scopes) != 1 || scopes[0] != model.CapReportsView {
		t.Fatalf("exchange result: token=%q scopes=%v", token, scopes)
	}
}

func TestExchange_WrongVerifierRejected(t *testing.T) {
	m := oauthas.New(testutil.SetupTestDB(t))
	_, challenge := pkce(t)
	code := issue(t, m, challenge, []string{model.CapReportsView})

	other, _ := pkce(t)
	if _, _, err := exchange(t, m, code, other); !errors.Is(err, oauthas.ErrInvalidGrant) {
		t.Fatalf("wrong verifier: error=%v, want ErrInvalidGrant", err)
	}
}

func TestExchange_CodeSingleUse(t *testing.T) {
	m := oauthas.New(testutil.SetupTestDB(t))
	verifier, challenge := pkce(t)
	code := issue(t, m, challenge, []string{model.CapReportsView})

	if _, _, err := exchange(t, m, code, verifier); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if _, _, err := exchange(t, m, code, verifier); !errors.Is(err, oauthas.ErrInvalidGrant) {
		t.Fatalf("replay: error=%v, want ErrInvalidGrant", err)
	}
}

func TestExchange_ClientOrRedirectMismatchRejected(t *testing.T) {
	m := oauthas.New(testutil.SetupTestDB(t))
	verifier, challenge := pkce(t)
	code := issue(t, m, challenge, []string{model.CapReportsView})

	created, scopes, err := m.Exchange(context.Background(), code, verifier,
		"evil-client", "http://127.0.0.1:59123/callback")
	_ = created
	_ = scopes
	if !errors.Is(err, oauthas.ErrInvalidGrant) {
		t.Fatalf("client mismatch: error=%v, want ErrInvalidGrant", err)
	}

	// Fresh code; wrong redirect.
	code = issue(t, m, challenge, []string{model.CapReportsView})
	if _, _, err := m.Exchange(context.Background(), code, verifier,
		"claude-desktop", "https://evil.example/callback"); !errors.Is(err, oauthas.ErrInvalidGrant) {
		t.Fatalf("redirect mismatch: error=%v, want ErrInvalidGrant", err)
	}
}

func TestExchange_ScopeShrunkSinceConsent(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := oauthas.New(db)
	verifier, challenge := pkce(t)

	// viewer holds only reports.view; the consent smuggles invoices.manage.
	userID := testutil.CreateTestUser(t, db, "shrink-oauth", "pw", "viewer")
	code, err := m.Issue(context.Background(), oauthas.Grant{
		UserID: userID, Username: "shrink-oauth", ClientID: "c", RedirectURI: "http://localhost:1/cb",
		CodeChallenge: challenge, Scopes: []string{model.CapReportsView, model.CapInvoicesManage},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := m.Exchange(context.Background(), code, verifier, "c", "http://localhost:1/cb")
	_ = created
	if !errors.Is(err, oauthas.ErrInvalidScope) {
		t.Fatalf("shrunk scope: error=%v, want ErrInvalidScope", err)
	}
}

func TestIssue_ExpiredCodesAreSwept(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := oauthas.New(db)
	_, challenge := pkce(t)

	code, err := m.Issue(context.Background(), oauthas.Grant{
		UserID: 1, Username: "admin", ClientID: "c", RedirectURI: "http://localhost:1/cb",
		CodeChallenge: challenge, Scopes: []string{model.CapReportsView},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Age the row past its 5-minute TTL.
	if _, err := db.Exec(`UPDATE mcp_oauth_codes SET expires_at = datetime('now','-1 minute')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Exchange(context.Background(), code, "any-verifier", "c", "http://localhost:1/cb"); !errors.Is(err, oauthas.ErrInvalidGrant) {
		t.Fatalf("expired code: error=%v, want ErrInvalidGrant", err)
	}
}

func TestExchange_TokenCarriesExpiry(t *testing.T) {
	m := oauthas.New(testutil.SetupTestDB(t))
	verifier, challenge := pkce(t)
	code := issue(t, m, challenge, []string{model.CapReportsView})

	created, _, err := m.Exchange(context.Background(), code, verifier, "claude-desktop", "http://127.0.0.1:59123/callback")
	if err != nil {
		t.Fatal(err)
	}
	if created.Token.ExpiresAt == nil || !created.Token.ExpiresAt.After(time.Now().Add(29*24*time.Hour)) {
		t.Fatalf("oauth token should carry ~30-day expiry, got %v", created.Token.ExpiresAt)
	}
	if !strings.HasPrefix(created.Token.Name, "MCP Remote claude-desktop ") {
		t.Fatalf("token name: %q", created.Token.Name)
	}
}
