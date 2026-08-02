package v1

import (
	"errors"
	"strconv"
	"strings"
)

// ParseIDR parses a non-negative integer-IDR string. Empty input is zero.
func ParseIDR(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	amount, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if amount < 0 {
		return 0, errors.New("must be non-negative")
	}
	return amount, nil
}

// ParseQuantity parses a non-negative decimal with at most two fractional digits into fixed-point ×100.
func ParseQuantity(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parts := strings.SplitN(value, ".", 2)
	whole, err := strconv.Atoi(parts[0])
	if err != nil || whole < 0 {
		return 0, errors.New("invalid quantity")
	}
	fraction := 0
	if len(parts) == 2 {
		digits := parts[1]
		if len(digits) == 0 || len(digits) > 2 {
			return 0, errors.New("invalid quantity")
		}
		if len(digits) == 1 {
			digits += "0"
		}
		fraction, err = strconv.Atoi(digits)
		if err != nil || fraction < 0 {
			return 0, errors.New("invalid quantity")
		}
	}
	return whole*100 + fraction, nil
}
