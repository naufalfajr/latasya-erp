package handler_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListHTMXRequestsReturnDirectFragments(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	for _, tc := range []struct {
		path, target, id string
	}{
		{"/accounts?search=cash", "account-table", `id="account-table"`},
		{"/contacts?search=missing", "contact-table", `id="contact-table"`},
		{"/journals?search=missing", "journal-table", `id="journal-table"`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req, err := requestWithCookies(db, http.MethodGet, ts.URL+tc.path, cookies, "")
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("HX-Request", "true")
			req.Header.Set("HX-Target", tc.target)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if !strings.Contains(string(body), tc.id) {
				t.Fatalf("fragment missing %s", tc.id)
			}
			if strings.Contains(string(body), "<!DOCTYPE html>") || strings.Contains(string(body), "<html") {
				t.Fatal("HTMX response rendered the full page")
			}
		})
	}
}

func TestBoostedListNavigationReturnsFullPage(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	for _, path := range []string{"/accounts", "/contacts", "/journals"} {
		t.Run(path, func(t *testing.T) {
			req, err := requestWithCookies(db, http.MethodGet, ts.URL+path, cookies, "")
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("HX-Request", "true")
			req.Header.Set("HX-Boosted", "true")
			req.Header.Set("HX-Target", "body")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "<!DOCTYPE html>") {
				t.Fatal("boosted navigation returned a table fragment instead of the full page")
			}
			if !strings.Contains(string(body), "Latasya ERP") {
				t.Fatal("boosted navigation omitted the application shell")
			}
		})
	}
}
