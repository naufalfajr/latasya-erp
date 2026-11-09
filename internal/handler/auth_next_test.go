package handler_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/testutil"
)

func loginForm(next string) url.Values {
	form := url.Values{"username": {"admin"}, "password": {adminTestPassword}}
	if next != "" {
		form.Set("next", next)
	}
	return form
}

func postLogin(t *testing.T, tsURL, next string) *http.Response {
	t.Helper()
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(tsURL, loginForm(next))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	return resp
}

func TestLogin_NextParam_AllowlistedConsentURL(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	// No base path: the bare consent path is accepted, query preserved.
	resp := postLogin(t, ts.URL+"/login", "/mcp/authorize?code=AB2C-D3F4")
	if loc := resp.Header.Get("Location"); loc != "/mcp/authorize?code=AB2C-D3F4" {
		t.Errorf("Location: got %q, want the consent URL", loc)
	}
}

func TestLogin_NextParam_RejectsEverythingElse(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	for _, next := range []string{
		"https://evil.example.com",
		"//evil.example.com",
		"/dashboard",          // not the consent page
		"/mcp/authorize/evil", // prefix trick
		"\\evil",
	} {
		resp := postLogin(t, ts.URL+"/login", next)
		if loc := resp.Header.Get("Location"); loc != "/" {
			t.Errorf("next=%q: Location: got %q, want \"/\" fallback", next, loc)
		}
	}
}

func TestLogin_FailedAttempt_PreservesNext(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/login",
		url.Values{"username": {"admin"}, "password": {"wrong"}, "next": {"/mcp/authorize?code=AB2C-D3F4"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	want := `name="next" value="/mcp/authorize?code=AB2C-D3F4"`
	if !strings.Contains(string(body), want) {
		t.Errorf("failed-login re-render dropped next: hidden field %q missing", want)
	}
}

func TestLogin_NextParam_BasePathConsentURL(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t, "/dashboard")

	resp := postLogin(t, ts.URL+"/dashboard/login", "/dashboard/mcp/authorize?code=X")
	if loc := resp.Header.Get("Location"); loc != "/dashboard/mcp/authorize?code=X" {
		t.Errorf("Location: got %q, want the base-prefixed consent URL", loc)
	}

	// Without the base prefix it must fall back inside the app.
	resp = postLogin(t, ts.URL+"/dashboard/login", "/mcp/authorize?code=X")
	if loc := resp.Header.Get("Location"); loc != "/dashboard/" {
		t.Errorf("Location: got %q, want \"/dashboard/\" fallback", loc)
	}
}

func TestLoginPage_PreservesNextInForm(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	resp, err := http.Get(ts.URL + "/login?next=/mcp/authorize%3Fcode%3DAB2C-D3F4")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	want := `name="next" value="/mcp/authorize?code=AB2C-D3F4"`
	if !strings.Contains(string(body), want) {
		t.Errorf("login form missing sanitized next hidden field %q", want)
	}

	// A hostile next is sanitized before it reaches the form.
	resp2, err := http.Get(ts.URL + "/login?next=https://evil.example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if strings.Contains(string(body2), "evil.example.com") {
		t.Error("login form reflected a non-allowlisted next value")
	}
}

func TestLogin_MustChangePassword_IgnoresNext(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)

	userID := testutil.CreateTestUser(t, db, "force-rotate", "pw", "viewer")
	if err := testutil.SetMustChangePassword(db, userID, true); err != nil {
		t.Fatalf("set must_change_password: %v", err)
	}

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/login",
		url.Values{"username": {"force-rotate"}, "password": {"pw"}, "next": {"/mcp/authorize?code=X"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/password/change" {
		t.Errorf("Location: got %q, want /password/change (rotation outranks next)", loc)
	}
}

func TestLogin_NextParam_OAuthAuthorizeSurvivesBasePath(t *testing.T) {
	// Regression: /oauth/authorize lives unprefixed on the outer mux, so its
	// bounce-back must stay bare even under BasePath — a prefixed-only
	// allowlist dropped the authorize URL and lost the client's flow.
	t.Parallel()
	ts, _ := testServer(t, "/dashboard")

	resp := postLogin(t, ts.URL+"/dashboard/login", "/oauth/authorize?client_id=c&state=s")
	if loc := resp.Header.Get("Location"); loc != "/oauth/authorize?client_id=c&state=s" {
		t.Errorf("Location: got %q, want the bare authorize URL", loc)
	}

	// The device-flow consent page keeps its prefixed form under BasePath.
	resp = postLogin(t, ts.URL+"/dashboard/login", "/dashboard/mcp/authorize?code=X")
	if loc := resp.Header.Get("Location"); loc != "/dashboard/mcp/authorize?code=X" {
		t.Errorf("Location: got %q, want the prefixed device consent URL", loc)
	}

	// And a bare device-consent path does not smuggle through under BasePath.
	resp = postLogin(t, ts.URL+"/dashboard/login", "/mcp/authorize?code=X")
	if loc := resp.Header.Get("Location"); loc != "/dashboard/" {
		t.Errorf("Location: got %q, want \"/dashboard/\" fallback", loc)
	}
}
