package handler_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/testutil"
)

// assertAstaPage checks a page outside the ERP shell renders on the Asta
// theme: hashed app.css (through theme-head, or linked directly on print),
// body.latasya, and no DaisyUI.
func assertAstaPage(t *testing.T, name, body string, wantToggle bool) {
	t.Helper()
	for _, want := range []string{`<body class="latasya"`, `/static/css/app.css?v=`} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: missing %s", name, want)
		}
	}
	if got := strings.Contains(body, `onclick="toggleTheme()"`); got != wantToggle {
		t.Errorf("%s: theme toggle present = %v, want %v", name, got, wantToggle)
	}
	if m := daisyClassIn(body); m != "" {
		t.Errorf("%s: DaisyUI class %q", name, m)
	}
	if strings.Contains(body, "data-theme=") {
		t.Errorf("%s: still opts into a DaisyUI theme", name)
	}
}

func TestStandalonePages_RenderOnAstaTheme(t *testing.T) {
	t.Parallel()

	ts, db := testServer(t)
	_, login := sendRequest(t, db, nil, "GET", ts.URL+"/login", "")
	assertAstaPage(t, "login", login, true)

	cookies := loginAsAdmin(t, ts)
	resp, consent := sendRequest(t, db, cookies, "GET", ts.URL+oauthAuthorizeURL("http://127.0.0.1:59123/callback", oauthChallenge(t)), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consent: status %d", resp.StatusCode)
	}
	assertAstaPage(t, "consent", consent, true)

	contact := mustContact(t, db, "Print Family", "081200000002")
	inv := mustInvoice(t, db, contact)
	_, printPage := sendRequest(t, db, cookies, "GET", fmt.Sprintf("%s/invoices/%d/print", ts.URL, inv), "")
	// Print is paper: light only, so no toggle and no theme script.
	assertAstaPage(t, "print", printPage, false)
	if strings.Contains(printPage, "toggleTheme") {
		t.Error("print: loads the theme script; print must stay on the light stage")
	}

	pts, pdb := publicTestServer(t)
	child := mustContact(t, pdb, "Portal Family", "081200000003")
	sent := mustInvoice(t, pdb, child)
	if err := testutil.SendInvoice(pdb, sent, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}
	code, err := testutil.GetOrCreatePortalCode(pdb, child)
	if err != nil {
		t.Fatalf("portal code: %v", err)
	}
	_, portal := sendRequest(t, pdb, nil, "GET", pts.URL+"/p/"+code, "")
	assertAstaPage(t, "portal", portal, true)
	if !strings.Contains(portal, `asta-badge-accent"><span class="asta-badge-dot"></span>Belum lunas</span>`) {
		t.Error("portal: a sent invoice should show the navy 'Belum lunas' badge")
	}
	if !strings.Contains(portal, `aria-label="Ganti mode gelap"`) {
		t.Error("portal: theme toggle should be labelled in Indonesian")
	}

	_, invalid := sendRequest(t, pdb, nil, "GET", pts.URL+"/p/no-such-code", "")
	assertAstaPage(t, "portal (invalid link)", invalid, true)
}
