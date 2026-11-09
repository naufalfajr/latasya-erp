package handler_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/testutil"
)

const oauthNextURL = "/oauth/authorize?state=s"

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

func TestLogin_NextParam_AllowlistedOAuthURL(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	resp := postLogin(t, ts.URL+"/login", oauthNextURL)
	if loc := resp.Header.Get("Location"); loc != oauthNextURL {
		t.Errorf("Location: got %q, want the authorize URL", loc)
	}
}

func TestLogin_NextParam_RejectsEverythingElse(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	for _, next := range []string{
		"https://evil.example.com",
		"//evil.example.com",
		"/dashboard",            // not the authorize page
		"/mcp/authorize?c=1",    // device flow removed; no longer valid
		"/oauth/authorize/evil", // prefix trick
		"\\evil",
	} {
		resp := postLogin(t, ts.URL+"/login", next)
		if loc := resp.Header.Get("Location"); loc != "/" {
			t.Errorf("next=%q: Location: got %q, want \"/\" fallback", next, loc)
		}
	}
}

func TestLogin_NextParam_OAuthURLUnderBasePath(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t, "/dashboard")

	// /oauth/authorize lives unprefixed on the outer mux: its bare form is
	// the only valid spelling, under every deployment.
	resp := postLogin(t, ts.URL+"/dashboard/login", oauthNextURL)
	if loc := resp.Header.Get("Location"); loc != oauthNextURL {
		t.Errorf("Location: got %q, want %q", loc, oauthNextURL)
	}

	// A BasePath-prefixed spelling must not smuggle through.
	resp = postLogin(t, ts.URL+"/dashboard/login", "/dashboard"+oauthNextURL)
	if loc := resp.Header.Get("Location"); loc != "/dashboard/" {
		t.Errorf("Location: got %q, want \"/dashboard/\" fallback", loc)
	}
}

func TestLoginPage_PreservesNextInForm(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	encoded := url.QueryEscape(oauthNextURL)
	resp, err := http.Get(ts.URL + "/login?next=" + encoded)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	want := `name="next" value="` + oauthNextURL + `"`
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

func TestLogin_FailedAttempt_PreservesNext(t *testing.T) {
	t.Parallel()
	ts, _ := testServer(t)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/login",
		url.Values{"username": {"admin"}, "password": {"wrong"}, "next": {oauthNextURL}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	want := `name="next" value="` + oauthNextURL + `"`
	if !strings.Contains(string(body), want) {
		t.Errorf("failed-login re-render dropped next: hidden field %q missing", want)
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
		url.Values{"username": {"force-rotate"}, "password": {"pw"}, "next": {oauthNextURL}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/password/change" {
		t.Errorf("Location: got %q, want /password/change (rotation outranks next)", loc)
	}
}
