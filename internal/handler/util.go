package handler

import (
	"math"
	"strconv"
	"strings"
)

// parseIDR parses a string like "150000" or "150.000" into an integer (IDR amount).
func parseIDR(s string) int {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "Rp", "")
	s = strings.TrimSpace(s)
	n, _ := strconv.Atoi(s)
	return n
}

// parseQuantity parses a decimal quantity string ("1", "1.5", "0.25") into the
// internal fixed-point integer representation (×100). "1" → 100, "1.5" → 150,
// "0.25" → 25. Returns 0 for empty/invalid input so the caller can apply a
// default.
func parseQuantity(s string) int {
	s = strings.TrimSpace(s)
	// Accept Indonesian decimal comma as well as dot.
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(math.Round(f * 100))
}

// getIndex safely gets index i from a string slice, returning "" if out of bounds.
func getIndex(slice []string, i int) string {
	if i < len(slice) {
		return slice[i]
	}
	return ""
}
