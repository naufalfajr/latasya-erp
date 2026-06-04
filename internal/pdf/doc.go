// Package pdf generates invoice PDFs using only the Go standard library.
// It writes a minimal PDF 1.4 document with the built-in Helvetica fonts
// (WinAnsi encoding), so no external dependency or font file is required and
// the single-binary deploy model is preserved. Text is restricted to the
// printable ASCII range, which fully covers Indonesian invoice content.
package pdf

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const (
	pageWidth  = 595.28
	pageHeight = 841.89
)

type doc struct {
	content bytes.Buffer
}

func newDoc() *doc { return &doc{} }

func num(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// ascii drops any byte outside printable ASCII so the WinAnsi-encoded content
// stream never emits a glyph the base-14 Helvetica font can't render.
func ascii(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b = append(b, ' ')
		case r >= 32 && r < 127:
			b = append(b, byte(r))
		default:
			b = append(b, '?')
		}
	}
	return string(b)
}

// escapeString escapes the three characters that are syntactically significant
// inside a PDF literal string: backslash and the two parentheses. Without this
// a customer name containing ")" would corrupt the content stream.
func escapeString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(s)
}

func (d *doc) text(x, y, size float64, bold bool, s string) {
	font := "F1"
	if bold {
		font = "F2"
	}
	fmt.Fprintf(&d.content, "BT /%s %s Tf %s %s Td (%s) Tj ET\n",
		font, num(size), num(x), num(y), escapeString(ascii(s)))
}

func (d *doc) textRight(xRight, y, size float64, bold bool, s string) {
	d.text(xRight-stringWidth(s, size, bold), y, size, bold, s)
}

func (d *doc) line(x1, y1, x2, y2, width float64) {
	fmt.Fprintf(&d.content, "%s w %s %s m %s %s l S\n",
		num(width), num(x1), num(y1), num(x2), num(y2))
}

func (d *doc) fillRect(x, y, w, h, gray float64) {
	fmt.Fprintf(&d.content, "%s g %s %s %s %s re f 0 g\n",
		num(gray), num(x), num(y), num(w), num(h))
}

// charWidth returns the advance width of r in 1/1000 em from the Adobe Core-14
// Helvetica (regular) AFM metrics. Bold text is measured with these regular
// widths; the difference is at most a few percent and stays sub-point at the
// sizes used here. Unknown runes (already filtered to '?') fall back to 556.
func charWidth(r rune) int {
	if w, ok := helveticaWidths[r]; ok {
		return w
	}
	return 556
}

var helveticaWidths = map[rune]int{
	' ': 278, '!': 278, '"': 355, '#': 556, '$': 556, '%': 889, '&': 667, '\'': 191,
	'(': 333, ')': 333, '*': 389, '+': 584, ',': 278, '-': 333, '.': 278, '/': 278,
	'0': 556, '1': 556, '2': 556, '3': 556, '4': 556, '5': 556, '6': 556, '7': 556, '8': 556, '9': 556,
	':': 278, ';': 278, '<': 584, '=': 584, '>': 584, '?': 556, '@': 1015,
	'A': 667, 'B': 667, 'C': 722, 'D': 722, 'E': 667, 'F': 611, 'G': 778, 'H': 722, 'I': 278,
	'J': 500, 'K': 667, 'L': 556, 'M': 833, 'N': 722, 'O': 778, 'P': 667, 'Q': 778, 'R': 722,
	'S': 667, 'T': 611, 'U': 722, 'V': 667, 'W': 944, 'X': 667, 'Y': 667, 'Z': 611,
	'[': 278, '\\': 278, ']': 278, '^': 469, '_': 556, '`': 333,
	'a': 556, 'b': 556, 'c': 500, 'd': 556, 'e': 556, 'f': 278, 'g': 556, 'h': 556, 'i': 222,
	'j': 222, 'k': 500, 'l': 222, 'm': 833, 'n': 556, 'o': 556, 'p': 556, 'q': 556, 'r': 333,
	's': 500, 't': 278, 'u': 556, 'v': 500, 'w': 722, 'x': 500, 'y': 500, 'z': 500,
	'{': 334, '|': 260, '}': 334, '~': 584,
}

func stringWidth(s string, size float64, bold bool) float64 {
	total := 0
	for _, r := range s {
		total += charWidth(r)
	}
	return float64(total) * size / 1000.0
}

func (d *doc) render() []byte {
	var b bytes.Buffer
	var offsets []int
	obj := func(s string) {
		offsets = append(offsets, b.Len())
		b.WriteString(s)
	}

	b.WriteString("%PDF-1.4\n")
	obj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	obj(fmt.Sprintf("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] "+
		"/Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> /Contents 4 0 R >>\nendobj\n",
		num(pageWidth), num(pageHeight)))

	cs := d.content.Bytes()
	obj(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n", len(cs)))
	b.Write(cs)
	b.WriteString("endstream\nendobj\n")

	obj("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>\nendobj\n")
	obj("6 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>\nendobj\n")

	xref := b.Len()
	count := len(offsets) + 1
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", count)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", count, xref)
	return b.Bytes()
}
