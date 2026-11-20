package tmpl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	latasyaerp "github.com/naufal/latasya-erp"
	"github.com/naufal/latasya-erp/internal/contact"
)

// assetHashes maps each embedded static file to a short hash of its bytes.
var assetHashes = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	_ = fs.WalkDir(latasyaerp.StaticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := latasyaerp.StaticFS.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		m[strings.TrimPrefix(p, "static/")] = hex.EncodeToString(sum[:4])
		return nil
	})
	return m
})

// asset returns the URL of a static file with its content hash, so a deploy
// changes the URL and no cache (browser or Cloudflare edge) serves stale CSS
// or JS next to new HTML.
func asset(p string) string {
	if h, ok := assetHashes()[p]; ok {
		return "/static/" + p + "?v=" + h
	}
	return "/static/" + p
}

func FuncMap() template.FuncMap {
	return template.FuncMap{
		"formatIDR":    formatIDR,
		"contactPrice": contact.Price,
		"formatQty":    formatQty,
		"formatDate":   formatDate,
		"isNegative":   func(n *int) bool { return n != nil && *n < 0 },
		"toJSON": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b) //nolint:gosec // JSON encoding escapes script-breaking characters.
		},
		// navActive marks a sidebar section current on its index and every page under it.
		"navActive": func(path, section string) bool {
			return path == section || strings.HasPrefix(path, section+"/")
		},
		// humanize turns a stored enum ("credit_note") into a sentence-case label ("Credit note").
		"humanize": func(s string) string {
			s = strings.ReplaceAll(s, "_", " ")
			r, size := utf8.DecodeRuneInString(s)
			if size == 0 {
				return s
			}
			return string(unicode.ToUpper(r)) + s[size:]
		},
		"splitN": strings.SplitN,
		"asset":  asset,
		"add":    func(a, b int) int { return a + b },
		"sub":    func(a, b int) int { return a - b },
		"mul":    func(a, b int) int { return a * b },
		// Go templates have built-in eq that handles multiple types
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
		},
		"dict": func(pairs ...any) map[string]any {
			m := make(map[string]any, len(pairs)/2)
			for i := 0; i+1 < len(pairs); i += 2 {
				if key, ok := pairs[i].(string); ok {
					m[key] = pairs[i+1]
				}
			}
			return m
		},
		"hasString": func(needle string, haystack []string) bool {
			for _, s := range haystack {
				if s == needle {
					return true
				}
			}
			return false
		},
	}
}

// formatIDR formats an integer as Indonesian Rupiah: 150000 → "Rp 150.000"
func formatIDR(amount int) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}

	s := fmt.Sprintf("%d", amount)
	// Add dots as thousands separators
	var result []string
	for i, j := len(s), 0; i > 0; j++ {
		end := i
		i -= 3
		if i < 0 {
			i = 0
		}
		result = append([]string{s[i:end]}, result...)
	}

	formatted := "Rp " + strings.Join(result, ".")
	if negative {
		formatted = "-" + formatted
	}
	return formatted
}

// formatQty converts the internal ×100 fixed-point quantity back to a human
// decimal string: 100 → "1", 150 → "1.5", 125 → "1.25", 25 → "0.25".
// Trailing zeros are stripped.
func formatQty(n int) string {
	whole := n / 100
	frac := n % 100
	switch {
	case frac == 0:
		return fmt.Sprintf("%d", whole)
	case frac%10 == 0:
		return fmt.Sprintf("%d.%d", whole, frac/10)
	default:
		return fmt.Sprintf("%d.%02d", whole, frac)
	}
}

func formatDate(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		t, err = time.Parse(time.DateTime, s)
		if err != nil {
			return s
		}
	}
	return t.Format("2 Jan 2006")
}
