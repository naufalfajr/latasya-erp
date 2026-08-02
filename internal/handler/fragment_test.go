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
		path, id string
	}{
		{"/accounts?search=cash", `id="account-table"`},
		{"/contacts?search=missing", `id="contact-table"`},
		{"/journals?search=missing", `id="journal-table"`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req, err := requestWithCookies(db, http.MethodGet, ts.URL+tc.path, cookies, "")
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("HX-Request", "true")
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
