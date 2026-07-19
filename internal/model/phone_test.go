package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
)

func TestNormalizePhoneID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"081234567890", "6281234567890"},
		{"6281234567890", "6281234567890"},
		{"+62 812-3456-7890", "6281234567890"},
		{"0812 3456 7890", "6281234567890"},
	}
	for _, tt := range tests {
		if got := model.NormalizePhoneID(tt.in); got != tt.want {
			t.Errorf("NormalizePhoneID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
