package oauthas_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	v1oauthas "github.com/naufal/latasya-erp/internal/api/v1/oauthas"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/oauthas"
	"github.com/naufal/latasya-erp/internal/testutil"
)

const verifier = "test-verifier-test-verifier-test-verifier-1234"

func challengeFor() string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func newTokenServer(t *testing.T) (*httptest.Server, *oauthas.Module) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	m := oauthas.New(db)
	h := &v1oauthas.Handler{OAuth: m}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, m
}

func exchangeHTTP(t *testing.T, ts *httptest.Server, form url.Values) (int, string) {
	t.Helper()
	resp, err := ts.Client().PostForm(ts.URL+"/oauth/token", form)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	return resp.StatusCode, string(body[:n])
}

func validExchangeForm() url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"irrelevant"},
		"code_verifier": {verifier},
		"client_id":     {"claude-desktop"},
		"redirect_uri":  {"http://127.0.0.1:59123/callback"},
	}
}

func TestToken_Success(t *testing.T) {
	ts, m := newTokenServer(t)
	code, err := m.Issue(t.Context(), oauthas.Grant{
		UserID: 1, Username: "admin", ClientID: "claude-desktop",
		RedirectURI: "http://127.0.0.1:59123/callback", CodeChallenge: challengeFor(),
		Scopes: []string{model.CapReportsView},
	})
	if err != nil {
		t.Fatal(err)
	}
	form := validExchangeForm()
	form.Set("code", code)

	status, body := exchangeHTTP(t, ts, form)
	if status != http.StatusOK {
		t.Fatalf("status: got %d body=%s", status, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if !strings.HasPrefix(tok.AccessToken, "lat_") || tok.TokenType != "Bearer" ||
		tok.Scope != model.CapReportsView || tok.ExpiresIn < 29*24*3600 {
		t.Fatalf("token payload: %+v", tok)
	}
}

func TestToken_ReplayAndWrongVerifier(t *testing.T) {
	ts, m := newTokenServer(t)
	code, err := m.Issue(t.Context(), oauthas.Grant{
		UserID: 1, Username: "admin", ClientID: "claude-desktop",
		RedirectURI: "http://127.0.0.1:59123/callback", CodeChallenge: challengeFor(),
		Scopes: []string{model.CapReportsView},
	})
	if err != nil {
		t.Fatal(err)
	}
	form := validExchangeForm()
	form.Set("code", code)
	if status, _ := exchangeHTTP(t, ts, form); status != http.StatusOK {
		t.Fatal("first exchange failed")
	}

	// Replay: consumed.
	status, body := exchangeHTTP(t, ts, form)
	if status != http.StatusBadRequest || !strings.Contains(body, "invalid_grant") {
		t.Fatalf("replay: status=%d body=%s", status, body)
	}

	// Wrong verifier on a fresh code.
	code2, _ := m.Issue(t.Context(), oauthas.Grant{
		UserID: 1, Username: "admin", ClientID: "claude-desktop",
		RedirectURI: "http://127.0.0.1:59123/callback", CodeChallenge: challengeFor(),
		Scopes: []string{model.CapReportsView},
	})
	form2 := validExchangeForm()
	form2.Set("code", code2)
	form2.Set("code_verifier", "wrong-verifier-wrong-verifier-wrong-verifier")
	status, body = exchangeHTTP(t, ts, form2)
	if status != http.StatusBadRequest || !strings.Contains(body, "invalid_grant") {
		t.Fatalf("wrong verifier: status=%d body=%s", status, body)
	}
}

func TestToken_UnsupportedGrantAndMissingFields(t *testing.T) {
	ts, _ := newTokenServer(t)

	form := validExchangeForm()
	form.Set("grant_type", "client_credentials")
	status, body := exchangeHTTP(t, ts, form)
	if status != http.StatusBadRequest || !strings.Contains(body, "unsupported_grant_type") {
		t.Fatalf("grant: status=%d body=%s", status, body)
	}

	form = url.Values{"grant_type": {"authorization_code"}}
	status, body = exchangeHTTP(t, ts, form)
	if status != http.StatusBadRequest || !strings.Contains(body, "invalid_request") {
		t.Fatalf("missing: status=%d body=%s", status, body)
	}
}

func TestWellKnown_Metadata(t *testing.T) {
	ts, _ := newTokenServer(t)

	resp, err := ts.Client().Get(ts.URL + "/.well-known/oauth-protected-resource")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var pr struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(pr.Resource, "/mcp") || len(pr.AuthorizationServers) != 1 {
		t.Fatalf("protected-resource metadata: %+v", pr)
	}

	resp2, err := ts.Client().Get(ts.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var as struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&as); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(as.AuthorizationEndpoint, "/oauth/authorize") ||
		!strings.HasSuffix(as.TokenEndpoint, "/oauth/token") {
		t.Fatalf("authorization-server metadata: %+v", as)
	}
}
