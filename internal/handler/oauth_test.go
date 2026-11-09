package handler_test

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// oauthAuthorizeURL builds a valid authorize request.
func oauthAuthorizeURL(redirect, challenge string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {"claude-desktop"},
		"redirect_uri":          {redirect},
		"state":                 {"st4te"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {model.CapReportsView + " " + model.CapAccountsManage},
	}
	return "/oauth/authorize?" + q.Encode()
}

func oauthChallenge(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte("e2e-verifier-e2e-verifier-e2e-verifier-e2e"))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestOAuthAuthorize_UnauthenticatedBouncesToLogin(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + oauthAuthorizeURL("http://127.0.0.1:59123/callback", oauthChallenge(t)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status: got %d, want 303", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Path != "/login" {
		t.Fatalf("Location: got %q, want /login", resp.Header.Get("Location"))
	}
	if next := loc.Query().Get("next"); !strings.HasPrefix(next, "/oauth/authorize?") {
		t.Fatalf("next param must preserve the authorize URL, got %q", next)
	}
}

func TestOAuthAuthorizePage_RendersClientAndScopes(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET",
		ts.URL+oauthAuthorizeURL("http://127.0.0.1:59123/callback", oauthChallenge(t)), cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"claude-desktop", model.CapReportsView, "Authorize"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("consent page missing %q", want)
		}
	}
}

func TestOAuthAuthorize_ApproveRedirectsWithCode(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	redirect := "http://127.0.0.1:59123/callback"

	// GET the page to obtain the session CSRF token.
	req, _ := requestWithCookies(db, "GET", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), cookies, "")
	page, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	csrf := csrfFromHTML(string(body))

	form := url.Values{
		"csrf_token": {csrf},
		"action":     {"approve"},
		"scopes":     {model.CapReportsView},
	}
	post, _ := http.NewRequest("POST", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		post.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status: got %d, want 303", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Host != "127.0.0.1:59123" || loc.Query().Get("code") == "" || loc.Query().Get("state") != "st4te" {
		t.Fatalf("redirect: %q", resp.Header.Get("Location"))
	}

	// A code row exists, hashed.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM mcp_oauth_codes").Scan(&count); err != nil || count != 1 {
		t.Fatalf("codes stored: %d err=%v", count, err)
	}
}

func TestOAuthAuthorize_DenyRedirectsWithError(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	redirect := "http://127.0.0.1:59123/callback"

	req, _ := requestWithCookies(db, "GET", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), cookies, "")
	page, _ := http.DefaultClient.Do(req)
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()

	form := url.Values{"csrf_token": {csrfFromHTML(string(body))}, "action": {"deny"}}
	post, _ := http.NewRequest("POST", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		post.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("error") != "access_denied" {
		t.Fatalf("deny redirect: %q", resp.Header.Get("Location"))
	}
}

func TestOAuthAuthorize_BadRedirectURIRendersErrorNotRedirect(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET",
		ts.URL+oauthAuthorizeURL("https://evil.example/callback", oauthChallenge(t)), cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { // error rendered on the page, not a redirect
		t.Fatalf("status: got %d, want 200 with error", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "redirect_uri") {
		t.Error("expected redirect_uri error on the page")
	}
}

func TestOAuthAuthorize_RequiresCSRF(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)
	cookies := loginAsAdmin(t, ts)
	redirect := "http://127.0.0.1:59123/callback"

	form := url.Values{"action": {"approve"}, "scopes": {model.CapReportsView}}
	post, _ := http.NewRequest("POST", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		post.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("post without csrf: got %d, want 403", resp.StatusCode)
	}
}

func csrfFromHTML(body string) string {
	const marker = `name="csrf_token" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	return rest[:strings.Index(rest, `"`)]
}

var _ = auth.HashPassword // keep import when helpers shift

func TestOAuthAuthorize_WorksUnderBasePath(t *testing.T) {
	// Regression: the consent form must POST to the unprefixed outer-mux
	// route even when the app is mounted under /dashboard — a BasePath'd
	// action 404'd through the protected mux in production.
	t.Parallel()
	ts, db := testServer(t, "/dashboard")
	cookies := loginAsAdmin(t, ts, "/dashboard")
	redirect := "http://127.0.0.1:59123/callback"

	authorize := ts.URL + "/dashboard/login" // prove login works too
	_ = authorize

	// The outer-mux route is reachable bare.
	req, _ := requestWithCookies(db, "GET", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), cookies, "")
	page, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("consent page under BasePath: got %d", page.StatusCode)
	}
	if !strings.Contains(string(body), `action="/oauth/authorize?`) {
		t.Fatalf("form action must be unprefixed, body excerpt: %s", csrfFromHTML(string(body)))
	}

	form := url.Values{
		"csrf_token": {csrfFromHTML(string(body))},
		"action":     {"approve"},
		"scopes":     {model.CapReportsView},
	}
	post, _ := http.NewRequest("POST", ts.URL+"/oauth/authorize?"+oauthAuthorizeURL(redirect, oauthChallenge(t))[strings.Index(oauthAuthorizeURL(redirect, oauthChallenge(t)), "?")+1:], strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		post.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("consent POST under BasePath: got %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "code=") {
		t.Fatalf("redirect missing code: %q", loc)
	}
}

func TestOAuthAuthorize_MergesIntoExistingRedirectQuery(t *testing.T) {
	// Regression: a redirect URI that already carries a query must receive
	// code/state merged, not "?x=1?code=…".
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	redirect := "http://127.0.0.1:59123/callback?x=1"

	req, _ := requestWithCookies(db, "GET", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), cookies, "")
	page, _ := http.DefaultClient.Do(req)
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()

	form := url.Values{
		"csrf_token": {csrfFromHTML(string(body))},
		"action":     {"approve"},
		"scopes":     {model.CapReportsView},
	}
	post, _ := http.NewRequest("POST", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		post.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("x") != "1" || loc.Query().Get("code") == "" || loc.Query().Get("state") != "st4te" {
		t.Fatalf("merged redirect: %q", resp.Header.Get("Location"))
	}
}

func TestOAuthAuthorize_BookkeeperApprovesAndGetsCode(t *testing.T) {
	// Regression: the outer-mux session lookup once returned the user with
	// nil capabilities (no role loading), so every non-admin saw an empty
	// scope grid and could never reach Issue.
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsBookkeeper(t, ts, db)
	redirect := "http://127.0.0.1:59123/callback"

	req, _ := requestWithCookies(db, "GET", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), cookies, "")
	page, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d", page.StatusCode)
	}
	// Bookkeeper holds these; the grid must render them.
	if !strings.Contains(string(body), model.CapContactsManage) ||
		!strings.Contains(string(body), model.CapReportsView) {
		t.Fatal("bookkeeper consent page must render owned capabilities")
	}

	form := url.Values{
		"csrf_token": {csrfFromHTML(string(body))},
		"action":     {"approve"},
		"scopes":     {model.CapReportsView},
	}
	post, _ := http.NewRequest("POST", ts.URL+oauthAuthorizeURL(redirect, oauthChallenge(t)), strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		post.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusSeeOther || loc.Query().Get("code") == "" {
		t.Fatalf("bookkeeper approval: status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
}
