package oauthas_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
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
		RegistrationEndpoint  string `json:"registration_endpoint"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&as); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(as.AuthorizationEndpoint, "/oauth/authorize") ||
		!strings.HasSuffix(as.TokenEndpoint, "/oauth/token") ||
		!strings.HasSuffix(as.RegistrationEndpoint, "/oauth/register") {
		t.Fatalf("authorization-server metadata: %+v", as)
	}
}

func TestRegister_DynamicClientRegistration(t *testing.T) {
	ts, _ := newTokenServer(t)

	post := func(body string) (int, string) {
		resp, err := ts.Client().Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(raw)
	}

	status, body := post(`{"client_name":"Claude Code","redirect_uris":["http://localhost:51121/callback"],"token_endpoint_auth_method":"none"}`)
	if status != http.StatusCreated {
		t.Fatalf("register: status=%d body=%s", status, body)
	}
	var reg struct {
		ClientID           string   `json:"client_id"`
		ClientName         string   `json:"client_name"`
		RedirectUris       []string `json:"redirect_uris"`
		TokenEndpointAuth  string   `json:"token_endpoint_auth_method"`
		GrantTypes         []string `json:"grant_types"`
	}
	if err := json.Unmarshal([]byte(body), &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.ClientID) < 16 || reg.ClientName != "Claude Code" ||
		len(reg.RedirectUris) != 1 || reg.RedirectUris[0] != "http://localhost:51121/callback" ||
		reg.TokenEndpointAuth != "none" || len(reg.GrantTypes) != 1 {
		t.Fatalf("registration response: %+v", reg)
	}

	for _, tc := range []struct{ body, want string }{
		{`{"redirect_uris":[]}`, "invalid_redirect_uri"},
		{`{"redirect_uris":[""]}`, "invalid_redirect_uri"},
		{`{"redirect_uris":["not a uri"]}`, "invalid_redirect_uri"},
		{`{"redirect_uris":["/callback"]}`, "invalid_redirect_uri"},
		{`{"redirect_uris":["http://localhost/cb#frag"]}`, "invalid_redirect_uri"},
		{`{"redirect_uris":["http://localhost/cb"],"token_endpoint_auth_method":"client_secret_basic"}`, "invalid_client_metadata"},
		{`not json`, "invalid_client_metadata"},
	} {
		if status, body := post(tc.body); status != http.StatusBadRequest || !strings.Contains(body, tc.want) {
			t.Fatalf("%q: status=%d body=%s", tc.body, status, body)
		}
	}

	// RFC 7591 §3.1: JSON content type required when declared.
	resp, err := ts.Client().Post(ts.URL+"/oauth/register", "text/plain", strings.NewReader(`{"redirect_uris":["http://localhost/cb"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "invalid_client_metadata") {
		t.Fatalf("content type: status=%d body=%s", resp.StatusCode, raw)
	}

	// Absent client_name stays absent (omitempty).
	status2, body2 := post(`{"redirect_uris":["http://localhost/cb"]}`)
	if status2 != http.StatusCreated || strings.Contains(body2, "client_name") {
		t.Fatalf("absent client_name: status=%d body=%s", status2, body2)
	}
}
