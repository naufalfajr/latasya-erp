package model

import "strings"

// NormalizePhoneID converts a loosely formatted Indonesian phone number
// (leading 0, spaces, dashes, +62) into the digits-only 62xxxxxxxxxx form,
// so numbers entered inconsistently (081... vs +62 812...) still compare
// equal.
func NormalizePhoneID(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	switch {
	case strings.HasPrefix(d, "62"):
		return d
	case strings.HasPrefix(d, "0"):
		return "62" + d[1:]
	default:
		return d
	}
}
