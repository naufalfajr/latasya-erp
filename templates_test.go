package latasyaerp

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// Pages still on DaisyUI until they migrate (see templates/README.md).
var daisyPages = map[string]bool{
	"templates/home/index.html":          true, // public page, own styles
	"templates/portal/index.html":        true,
	"templates/invoices/print.html":      true,
	"templates/auth/login.html":          true,
	"templates/mcp/oauth_consent.html":   true,
	"templates/partials/csrf.html":       true,
	"templates/invoices/README.md":       true,
	"templates/README.md":                true,
	"templates/partials/theme.html":      true,
	"templates/partials/pagination.html": false,
}

func erpTemplates(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(TemplateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") || daisyPages[path] {
			return err
		}
		b, err := TemplateFS.ReadFile(path)
		out[path] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 40 {
		t.Fatalf("found only %d ERP templates; the walk is broken", len(out))
	}
	return out
}

var (
	classAttr = regexp.MustCompile(`class="([^"]*)"`)
	// DaisyUI component classes. Tailwind utilities with the same stem
	// (table-cell, hidden) are not in this list.
	daisyClass = regexp.MustCompile(`^(btn|card|card-body|card-title|card-actions|badge|alert|input-bordered|select-bordered|textarea-bordered|form-control|label-text|tab|tabs|tab-active|join|join-item|stat|stats|stat-title|stat-value|divider|drawer|drawer-side|drawer-toggle|menu|menu-title|navbar|link|link-hover|link-primary|checkbox|toggle|loading|rounded-box)(-[a-z0-9]+)*$|^(bg|text|border)-(base|primary|secondary|accent|neutral|info|success|warning|error)(-[a-z0-9]+)?$`)
)

// Migrated screens must not fall back to DaisyUI: mixing the two kits is
// how a page drifts out of the design system unnoticed.
func TestERPTemplates_NoDaisyUIClasses(t *testing.T) {
	for path, src := range erpTemplates(t) {
		if path == "templates/base.html" {
			// The signed-out branch still serves the DaisyUI login page.
			src = src[:strings.Index(src, "{{else}}\n<body class=\"min-h-screen bg-base-200\"")]
		}
		for _, m := range classAttr.FindAllStringSubmatch(src, -1) {
			for _, cls := range strings.Fields(m[1]) {
				cls = cls[strings.LastIndex(cls, ":")+1:]
				if daisyClass.MatchString(cls) {
					t.Errorf("%s: DaisyUI class %q in class=%q", path, cls, m[1])
				}
			}
		}
	}
}

// Tag patterns skip quoted values, so a "=>" inside onclick does not end the tag.
var buttonTag = regexp.MustCompile(`<button\b(?:[^>"]|"[^"]*")*>`)

// Destructive actions are quiet outline buttons, so the confirm prompt is
// what protects them: every one must ask first.
func TestERPTemplates_DestructiveActionsConfirm(t *testing.T) {
	found := 0
	for path, src := range erpTemplates(t) {
		if strings.Contains(src, "asta-btn-danger") {
			t.Errorf("%s: filled asta-btn-danger; use asta-btn-outline erp-danger", path)
		}
		for _, loc := range buttonTag.FindAllStringIndex(src, -1) {
			tag := src[loc[0]:loc[1]]
			if !strings.Contains(tag, "erp-danger") {
				continue
			}
			found++
			if strings.Contains(tag, "confirm") {
				continue
			}
			formStart := strings.LastIndex(src[:loc[0]], "<form")
			if formStart >= 0 && !strings.Contains(src[formStart:loc[0]], "</form>") {
				formTag := src[formStart : formStart+strings.Index(src[formStart:], ">")]
				if strings.Contains(formTag, "confirm") {
					continue
				}
			}
			line := strings.Count(src[:loc[0]], "\n") + 1
			t.Errorf("%s:%d: destructive button without a confirm: %s", path, line, tag)
		}
	}
	if found == 0 {
		t.Fatal("no erp-danger buttons found; the guard is not checking anything")
	}
}

var astaButton = regexp.MustCompile(`(?s)<(a|button)\b((?:[^>"]|"[^"]*")*)>(.*?)</(?:a|button)>`)

// Kit buttons need a label: the asta-btn-label span (which the loading
// spinner overlays) or, for icon-only buttons, an aria-label.
func TestERPTemplates_ButtonsHaveLabels(t *testing.T) {
	for path, src := range erpTemplates(t) {
		for _, m := range astaButton.FindAllStringSubmatch(src, -1) {
			attrs, inner := m[2], m[3]
			if !regexp.MustCompile(`class="[^"]*\basta-btn\b`).MatchString(attrs) {
				continue
			}
			if strings.Contains(inner, "asta-btn-label") || strings.Contains(attrs, "aria-label=") {
				continue
			}
			t.Errorf("%s: <%s %s> has neither an asta-btn-label nor an aria-label", path, m[1], strings.TrimSpace(attrs))
		}
	}
}

// Line-row partials are rendered standalone for HTMX (journal.go parses its
// partial alone), so they must not depend on shared UI templates.
func TestLinePartials_SelfContained(t *testing.T) {
	for path, src := range erpTemplates(t) {
		if !strings.HasSuffix(path, "line_partial.html") {
			continue
		}
		for _, shared := range []string{`template "select-chevron"`, `template "check-icon"`, `template "alert-icon"`, `template "status-badge"`} {
			if strings.Contains(src, shared) {
				t.Errorf("%s calls {{%s}}; inline it instead", path, shared)
			}
		}
	}
}

// htmx snapshots the page for Back before it swaps. An open <dialog> (the
// phone menu) would be restored as a stuck non-modal overlay, so base.html
// must close dialogs before the snapshot.
func TestBase_ClosesDialogsBeforeHistorySnapshot(t *testing.T) {
	b, err := TemplateFS.ReadFile("templates/base.html")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "htmx:beforeHistorySave")
	if i < 0 || !strings.Contains(src[i:i+200], "dialog[open]") || !strings.Contains(src[i:i+200], ".close()") {
		t.Fatal("base.html must close open dialogs on htmx:beforeHistorySave")
	}
}
