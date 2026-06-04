package pdf

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
)

const (
	marginLeft  = 50.0
	marginRight = pageWidth - 50.0
)

// InvoicePDF renders a single-page A4 invoice in Bahasa Indonesia and returns
// the raw PDF bytes. co supplies the seller identity and payment details; a nil
// co is treated as empty so a missing profile never panics.
func InvoicePDF(inv *model.Invoice, co *model.CompanyProfile) ([]byte, error) {
	if inv == nil {
		return nil, fmt.Errorf("invoice is nil")
	}
	if co == nil {
		co = &model.CompanyProfile{}
	}

	d := newDoc()
	yTop := pageHeight - 50.0

	ly := yTop
	if co.Name != "" {
		d.text(marginLeft, ly, 18, true, co.Name)
		ly -= 20
	}
	if co.Tagline != "" {
		d.text(marginLeft, ly, 10, false, co.Tagline)
		ly -= 13
	}
	for _, l := range splitLines(co.Address) {
		d.text(marginLeft, ly, 9, false, l)
		ly -= 11
	}
	if c := joinNonEmpty(" | ", co.Phone, co.Email); c != "" {
		d.text(marginLeft, ly, 9, false, c)
		ly -= 11
	}
	if co.NPWP != "" {
		d.text(marginLeft, ly, 9, false, "NPWP: "+co.NPWP)
		ly -= 11
	}

	ry := yTop
	d.textRight(marginRight, ry, 20, true, "FAKTUR")
	ry -= 22
	d.textRight(marginRight, ry, 10, false, "No: "+inv.InvoiceNumber)
	ry -= 13
	d.textRight(marginRight, ry, 10, false, "Tanggal: "+formatDateID(inv.InvoiceDate))
	ry -= 13
	d.textRight(marginRight, ry, 10, false, "Jatuh Tempo: "+formatDateID(inv.DueDate))
	ry -= 13

	y := minF(ly, ry) - 8
	d.line(marginLeft, y, marginRight, y, 1)
	y -= 22

	d.text(marginLeft, y, 9, false, "Kepada Yth:")
	y -= 14
	d.text(marginLeft, y, 12, true, inv.ContactName)
	y -= 26

	const (
		colDescX  = marginLeft + 4
		colQtyX   = 360.0
		colPriceX = 470.0
	)
	colAmtX := marginRight - 4

	d.fillRect(marginLeft, y-4, marginRight-marginLeft, 16, 0.92)
	d.text(colDescX, y, 9, true, "Deskripsi")
	d.textRight(colQtyX, y, 9, true, "Qty")
	d.textRight(colPriceX, y, 9, true, "Harga Satuan")
	d.textRight(colAmtX, y, 9, true, "Jumlah")
	y -= 18

	for i, l := range inv.Lines {
		if y < 120 {
			d.text(colDescX, y, 9, false, fmt.Sprintf("... %d item lainnya", len(inv.Lines)-i))
			y -= 15
			break
		}
		d.text(colDescX, y, 9, false, truncate(l.Description, 46))
		d.textRight(colQtyX, y, 9, false, formatQty(l.Quantity))
		d.textRight(colPriceX, y, 9, false, formatIDR(l.UnitPrice))
		d.textRight(colAmtX, y, 9, false, formatIDR(l.Amount))
		y -= 15
	}

	y -= 2
	d.line(marginLeft, y, marginRight, y, 0.5)
	y -= 16

	const labelX = 440.0
	valX := marginRight - 4
	totalRow := func(label, val string, bold bool) {
		d.textRight(labelX, y, 10, bold, label)
		d.textRight(valX, y, 10, bold, val)
		y -= 15
	}
	totalRow("Subtotal", formatIDR(inv.Subtotal), false)
	if inv.TaxAmount > 0 {
		totalRow("Pajak", formatIDR(inv.TaxAmount), false)
	}
	totalRow("Total", formatIDR(inv.Total), true)
	if inv.AmountPaid > 0 {
		totalRow("Dibayar", formatIDR(inv.AmountPaid), false)
	}
	if inv.AmountCredited > 0 {
		totalRow("Kredit", formatIDR(inv.AmountCredited), false)
	}
	if inv.AmountPaid > 0 || inv.AmountCredited > 0 {
		totalRow("Sisa Tagihan", formatIDR(inv.AmountDue()), true)
	}
	y -= 10

	if inv.Notes != "" {
		d.text(marginLeft, y, 9, true, "Catatan:")
		y -= 13
		for _, l := range wrap(inv.Notes, 95) {
			d.text(marginLeft, y, 9, false, l)
			y -= 12
		}
		y -= 8
	}

	if co.BankName != "" || co.BankAccountNumber != "" {
		d.text(marginLeft, y, 9, true, "Pembayaran:")
		y -= 13
		pay := joinNonEmpty(" ", co.BankName, co.BankAccountNumber)
		if co.BankAccountHolder != "" {
			pay += " (a.n. " + co.BankAccountHolder + ")"
		}
		d.text(marginLeft, y, 9, false, pay)
	}

	fy := 56.0
	for _, l := range splitLines(co.InvoiceFooter) {
		d.textCenter(pageWidth/2, fy, 9, false, l)
		fy -= 12
	}

	return d.render(), nil
}

func (d *doc) textCenter(xc, y, size float64, bold bool, s string) {
	d.text(xc-stringWidth(s, size, bold)/2, y, size, bold, s)
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

func splitLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, strings.TrimRight(l, " \r"))
	}
	return out
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func wrap(s string, max int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := ""
	for _, w := range words {
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= max:
			cur += " " + w
		default:
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func formatIDR(amount int) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}
	s := strconv.Itoa(amount)
	n := len(s)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := "Rp " + b.String()
	if negative {
		out = "-" + out
	}
	return out
}

func formatQty(n int) string {
	whole := n / 100
	frac := n % 100
	switch {
	case frac == 0:
		return strconv.Itoa(whole)
	case frac%10 == 0:
		return fmt.Sprintf("%d.%d", whole, frac/10)
	default:
		return fmt.Sprintf("%d.%02d", whole, frac)
	}
}

var monthsID = [...]string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember"}

func formatDateID(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d %s %d", t.Day(), monthsID[int(t.Month())], t.Year())
}
