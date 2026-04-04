package handler

import (
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

// getIndex safely gets index i from a string slice, returning "" if out of bounds.
func getIndex(slice []string, i int) string {
	if i < len(slice) {
		return slice[i]
	}
	return ""
}
