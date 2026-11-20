package tmpl

import (
	"regexp"
	"testing"
)

func TestNavActive(t *testing.T) {
	navActive := FuncMap()["navActive"].(func(string, string) bool)
	for _, c := range []struct {
		path, section string
		want          bool
	}{
		{"/invoices", "/invoices", true},
		{"/invoices/43", "/invoices", true},
		{"/invoices-archive", "/invoices", false},
		{"/settings/api-tokens", "/settings/company", false},
	} {
		if got := navActive(c.path, c.section); got != c.want {
			t.Errorf("navActive(%q, %q) = %v, want %v", c.path, c.section, got, c.want)
		}
	}
}

func TestHumanize(t *testing.T) {
	humanize := FuncMap()["humanize"].(func(string) string)
	for in, want := range map[string]string{"credit_note": "Credit note", "sent": "Sent", "asset": "Asset", "": "", "écart": "Écart"} {
		if got := humanize(in); got != want {
			t.Errorf("humanize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatIDR(t *testing.T) {
	for in, want := range map[int]string{
		0: "Rp 0", 5: "Rp 5", 999: "Rp 999", 1000: "Rp 1.000", 150000: "Rp 150.000",
		1234567: "Rp 1.234.567", -1290000: "-Rp 1.290.000", -500: "-Rp 500",
	} {
		if got := formatIDR(in); got != want {
			t.Errorf("formatIDR(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatQty(t *testing.T) {
	for in, want := range map[int]string{0: "0", 100: "1", 150: "1.5", 125: "1.25", 25: "0.25", 1000: "10", 105: "1.05"} {
		if got := formatQty(in); got != want {
			t.Errorf("formatQty(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatDate(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-01":          "1 Nov 2026",
		"2026-01-31 08:15:00": "31 Jan 2026",
		"":                    "",
		"not a date":          "not a date",
	} {
		if got := formatDate(in); got != want {
			t.Errorf("formatDate(%q) = %q, want %q", in, got, want)
		}
	}
}

// Static URLs carry a content hash so a deploy never pairs new HTML with a
// CSS or JS file still cached at the Cloudflare edge or in the browser.
func TestAsset_HashesEmbeddedFiles(t *testing.T) {
	got := asset("css/app.css")
	if !regexp.MustCompile(`^/static/css/app\.css\?v=[0-9a-f]{8}$`).MatchString(got) {
		t.Errorf("asset(css/app.css) = %q, want a hashed URL", got)
	}
	if got := asset("missing.css"); got != "/static/missing.css" {
		t.Errorf("asset(missing.css) = %q, want the plain URL", got)
	}
}
