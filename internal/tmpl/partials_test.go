package tmpl

import (
	"html/template"
	"strings"
	"testing"
)

// The status badge is the one place document status gets colour
// (DESIGN.md "Document status mapping"), so pin the mapping.
func TestStatusBadge_Mapping(t *testing.T) {
	tpl := template.Must(template.New("").Funcs(FuncMap()).ParseFiles("../../templates/partials/ui.html"))
	for status, want := range map[string]struct{ tone, label string }{
		"draft":     {"", "Draft"},
		"sent":      {"asta-badge-accent", "Sent"},
		"received":  {"asta-badge-accent", "Received"},
		"issued":    {"asta-badge-accent", "Issued"},
		"partial":   {"asta-badge-warning", "Partial"},
		"paid":      {"asta-badge-success", "Paid"},
		"overdue":   {"asta-badge-danger", "Overdue"},
		"cancelled": {"asta-badge-outline", "Cancelled"},
		"void":      {"asta-badge-outline", "Void"},
	} {
		var b strings.Builder
		if err := tpl.ExecuteTemplate(&b, "status-badge", status); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		html := b.String()
		classes := html[strings.Index(html, `class="`)+7:]
		classes = classes[:strings.Index(classes, `"`)]
		fields := strings.Fields(classes)
		tones := 0
		for _, c := range fields {
			if strings.HasPrefix(c, "asta-badge-") && c != "asta-badge-sm" && c != "asta-badge-dot" {
				tones++
				if c != want.tone {
					t.Errorf("%s: tone %q, want %q", status, c, want.tone)
				}
			}
		}
		if want.tone == "" && tones != 0 {
			t.Errorf("%s: got a tone in %q, want the neutral badge", status, classes)
		}
		if want.tone != "" && tones != 1 {
			t.Errorf("%s: %d tone classes in %q, want exactly one", status, tones, classes)
		}
		if !strings.Contains(html, ">"+want.label+"</span>") {
			t.Errorf("%s: label missing %q in %s", status, want.label, html)
		}
	}
}
