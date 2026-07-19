package handler

import "testing"

func TestNormalizePhoneID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"081234567890", "6281234567890"},
		{"6281234567890", "6281234567890"},
		{"+62 812-3456-7890", "6281234567890"},
		{"0812 3456 7890", "6281234567890"},
	}
	for _, tt := range tests {
		if got := normalizePhoneID(tt.in); got != tt.want {
			t.Errorf("normalizePhoneID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildWALink_EncodesMessage(t *testing.T) {
	link := buildWALink("081234567890", "Halo & terima kasih")
	want := "https://wa.me/6281234567890?text=Halo+%26+terima+kasih"
	if link != want {
		t.Errorf("buildWALink() = %q, want %q", link, want)
	}
}
