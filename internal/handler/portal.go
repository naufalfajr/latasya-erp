package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/pdf"
)

// portalInvoiceView is one invoice row on a parent's portal page, with the
// display logic (who it's for, how to pay it) precomputed so the template
// stays dumb.
type portalInvoiceView struct {
	model.Invoice
	ChildName string
	Remark    string
	ConfirmWA string
	PDFPath   string
}

type portalData struct {
	Invalid         bool
	FamilyLabel     string
	Invoices        []portalInvoiceView
	HasCurrentMonth bool
	TotalDue        int
	ShortURL        string
	Company         *model.CompanyProfile
}

// portalRemark is the transfer note a parent is asked to write, so the
// owner can eyeball-match a bank transfer to an invoice: "{child} {month
// name} {year}", e.g. "Andi Juli 2026".
func portalRemark(childName, invoiceDate string) string {
	var year, month int
	fmt.Sscanf(invoiceDate[:7], "%d-%d", &year, &month)
	return fmt.Sprintf("%s %s %d", childName, model.MonthNameID(month), year)
}

// PortalIndex is the parent-facing invoice page at GET /p/{code}, showing
// every non-draft invoice for that child and any siblings.
func (h *Handler) PortalIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")

	code := r.PathValue("code")
	family, err := model.ContactsByPortalCode(h.DB, code)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	t, err := h.getTemplate("templates/portal/index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if family == nil {
		// 404, not 200: PortalCodeLimiter only counts non-2xx as a guess.
		w.WriteHeader(http.StatusNotFound)
		t.ExecuteTemplate(w, "index.html", PageData{
			Title: "Link Tidak Valid",
			Data:  portalData{Invalid: true},
		})
		return
	}

	invoices, err := model.ListPortalInvoices(h.DB, family.ContactIDs())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	company, err := model.GetCompanyProfile(h.DB)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	nameByContact := make(map[int]string, len(family.Contacts))
	names := make([]string, len(family.Contacts))
	for i, c := range family.Contacts {
		nameByContact[c.ID] = c.Name
		names[i] = c.Name
	}

	currentMonth := time.Now().Format("2006-01")

	views := make([]portalInvoiceView, 0, len(invoices))
	totalDue := 0
	hasCurrentMonth := false
	for _, inv := range invoices {
		due := inv.AmountDue()
		if due > 0 {
			totalDue += due
		}
		if strings.HasPrefix(inv.InvoiceDate, currentMonth) {
			hasCurrentMonth = true
		}

		childName := nameByContact[inv.ContactID]
		v := portalInvoiceView{
			Invoice:   inv,
			ChildName: childName,
			// family.Code, not what was typed: links stay canonical.
			PDFPath: fmt.Sprintf("/p/%s/invoice/%d/pdf", family.Code, inv.ID),
		}
		if due > 0 {
			v.Remark = portalRemark(childName, inv.InvoiceDate)
			if company.Phone != "" {
				v.ConfirmWA = buildWALink(company.Phone, fmt.Sprintf(
					"Halo, saya sudah transfer untuk %s. Mohon dicek, terima kasih 🙏", v.Remark))
			}
		}
		views = append(views, v)
	}

	t.ExecuteTemplate(w, "index.html", PageData{
		Title: "Invoice " + strings.Join(names, " & "),
		Data: portalData{
			FamilyLabel:     strings.Join(names, " & "),
			Invoices:        views,
			HasCurrentMonth: hasCurrentMonth,
			TotalDue:        totalDue,
			// Canonical spelling, so the parent saves the tidy one.
			ShortURL: h.publicOrigin(r) + "/p/" + family.Code,
			Company:  company,
		},
	})
}

// PortalInvoicePDF serves one invoice's PDF. The code must own the invoice
// and drafts are never served, so neither can be enumerated.
func (h *Handler) PortalInvoicePDF(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")

	code := r.PathValue("code")
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	family, err := model.ContactsByPortalCode(h.DB, code)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if family == nil {
		http.NotFound(w, r)
		return
	}

	inv, err := model.GetInvoice(h.DB, id)
	if err != nil || inv.Status == model.StatusDraft || !family.Has(inv.ContactID) {
		http.NotFound(w, r)
		return
	}

	company, err := model.GetCompanyProfile(h.DB)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data, err := pdf.InvoicePDF(inv, company)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", inv.InvoiceNumber+".pdf"))
	w.Write(data)
}
