package v1_test

import (
	"testing"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
)

func TestStrictAmountParsers(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        int
		invalid     bool
		parse       func(string) (int, error)
	}{
		{"idr", "125000", 125000, false, v1.ParseIDR}, {"negative idr", "-1", 0, true, v1.ParseIDR},
		{"quantity", "1.25", 125, false, v1.ParseQuantity}, {"single decimal", "1.5", 150, false, v1.ParseQuantity},
		{"negative fraction", "1.-2", 0, true, v1.ParseQuantity}, {"too precise", "1.234", 0, true, v1.ParseQuantity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.parse(tc.value)
			if tc.invalid && err == nil {
				t.Fatal("expected error")
			}
			if !tc.invalid && (err != nil || got != tc.want) {
				t.Fatalf("got %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}
