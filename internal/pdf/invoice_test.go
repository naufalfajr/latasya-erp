package pdf

import (
	"bytes"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
)

func sampleInvoice() *model.Invoice {
	return &model.Invoice{
		InvoiceNumber: "INV-202604-0001",
		ContactName:   "Sekolah Ceria",
		InvoiceDate:   "2026-04-10",
		DueDate:       "2026-05-10",
		Subtotal:      1500000,
		Total:         1500000,
		Notes:         "Pembayaran via transfer sebelum jatuh tempo.",
		Lines: []model.InvoiceLine{
			{Description: "Sewa bus sekolah - April 2026", Quantity: 100, UnitPrice: 1500000, Amount: 1500000},
		},
	}
}

func TestInvoicePDF_ProducesValidPDF(t *testing.T) {
	co := &model.CompanyProfile{
		Name:              "Latasya Transport",
		Tagline:           "School Bus & Travel Service",
		Address:           "Jl. Contoh No. 1, Jakarta",
		BankName:          "BCA",
		BankAccountNumber: "1234567890",
		BankAccountHolder: "Latasya",
		InvoiceFooter:     "Terima kasih atas kepercayaan Anda.",
	}

	data, err := InvoicePDF(sampleInvoice(), co)
	if err != nil {
		t.Fatalf("InvoicePDF: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty pdf")
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.")) {
		t.Errorf("missing PDF header, got %q", data[:min(len(data), 8)])
	}
	if !bytes.Contains(data, []byte("%%EOF")) {
		t.Error("missing EOF trailer marker")
	}
}

func TestInvoicePDF_NilCompany(t *testing.T) {
	data, err := InvoicePDF(sampleInvoice(), nil)
	if err != nil {
		t.Fatalf("InvoicePDF with nil company: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.")) {
		t.Error("missing PDF header")
	}
}

func TestInvoicePDF_NilInvoice(t *testing.T) {
	if _, err := InvoicePDF(nil, nil); err == nil {
		t.Error("expected an error for a nil invoice")
	}
}

func TestInvoicePDF_EscapesParensAndBackslash(t *testing.T) {
	inv := sampleInvoice()
	inv.ContactName = `Budi (Bendahara) \ Co`

	data, err := InvoicePDF(inv, nil)
	if err != nil {
		t.Fatalf("InvoicePDF: %v", err)
	}
	if bytes.Contains(data, []byte("Budi (Bendahara)")) {
		t.Error("unescaped parentheses leaked into the content stream")
	}
	if !bytes.Contains(data, []byte(`Budi \(Bendahara\) \\ Co`)) {
		t.Error("expected escaped parentheses and backslash in the content stream")
	}
}

func TestFormatIDR(t *testing.T) {
	cases := map[int]string{
		0:        "Rp 0",
		1000:     "Rp 1.000",
		150000:   "Rp 150.000",
		1500000:  "Rp 1.500.000",
		-2000:    "-Rp 2.000",
		12345678: "Rp 12.345.678",
	}
	for in, want := range cases {
		if got := formatIDR(in); got != want {
			t.Errorf("formatIDR(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatQty(t *testing.T) {
	cases := map[int]string{100: "1", 150: "1.5", 125: "1.25", 25: "0.25", 1000: "10"}
	for in, want := range cases {
		if got := formatQty(in); got != want {
			t.Errorf("formatQty(%d) = %q, want %q", in, got, want)
		}
	}
}
