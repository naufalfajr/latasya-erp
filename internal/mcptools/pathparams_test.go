package mcptools_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/mcptools"
)

// TestPathParamsExposed guards against path parameters being dropped from
// generated schemas: every {placeholder} must be a required path argument, and vice versa.
func TestPathParamsExposed(t *testing.T) {
	for _, tool := range mcptools.GeneratedTools {
		var placeholders []string
		for _, seg := range strings.Split(tool.PathTmpl, "/") {
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				placeholders = append(placeholders, strings.Trim(seg, "{}"))
			}
		}
		have := map[string]bool{}
		for _, p := range tool.Params {
			if p.In != "path" {
				continue
			}
			have[p.Name] = true
			if !slices.Contains(placeholders, p.Name) {
				t.Errorf("%s: path param %q has no placeholder in %s", tool.Name, p.Name, tool.PathTmpl)
			}
			if !p.Required {
				t.Errorf("%s: path param %q must be required", tool.Name, p.Name)
			}
		}
		for _, name := range placeholders {
			if !have[name] {
				t.Errorf("%s: route %s has no path param for {%s} — the tool cannot name the record",
					tool.Name, tool.PathTmpl, name)
			}
		}
	}
}

// TestPathParamsSubstitutedIntoURL pins the dispatch half: the id argument
// must land in the URL path, and omitting it must fail loudly (a placeholder route 404s).
func TestPathParamsSubstitutedIntoURL(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"getInvoice", map[string]any{"id": float64(173)}, "/api/v1/invoices/173"},
		{"recordInvoicePayment", map[string]any{"id": 173, "body": map[string]any{"amount": "350000"}},
			"/api/v1/invoices/173/payment"},
		{"updateExpense", map[string]any{"id": 580, "body": map[string]any{}}, "/api/v1/expenses/580"},
	}
	for _, tc := range cases {
		tool := mcptools.Find(tc.tool)
		if tool == nil {
			t.Fatalf("tool %q not found", tc.tool)
		}
		path, _, err := mcptools.BuildRequestParts(tool, tc.args)
		if err != nil || path != tc.want {
			t.Errorf("%s: got %q, err %v; want %q", tc.tool, path, err, tc.want)
		}
	}
	if _, _, err := mcptools.BuildRequestParts(
		mcptools.Find("updateExpense"), map[string]any{"body": map[string]any{}}); err == nil {
		t.Error("updateExpense without id: want error, got none")
	}
}
