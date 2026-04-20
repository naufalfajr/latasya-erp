package tmpl

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

func FuncMap() template.FuncMap {
	return template.FuncMap{
		"formatIDR": formatIDR,
		"formatQty": formatQty,
		"formatDate": formatDate,
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
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
