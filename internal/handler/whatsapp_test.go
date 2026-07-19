package handler

import "testing"

func TestBuildWALink_EncodesMessage(t *testing.T) {
	link := buildWALink("081234567890", "Halo & terima kasih")
	want := "https://wa.me/6281234567890?text=Halo+%26+terima+kasih"
	if link != want {
		t.Errorf("buildWALink() = %q, want %q", link, want)
	}
}
