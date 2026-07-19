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
// display logic (who it's for, whether it's overdue, how to pay it)
// precomputed so the template stays dumb.
type portalInvoiceView struct {
	model.Invoice
	ChildName string
	Overdue   bool
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

// PortalIndex is the parent-facing invoice page at GET /i/{token}. A token
// resolves to a family (the token's contact plus any siblings sharing its
// phone number) and shows every non-draft invoice across that family.
func (h *Handler) PortalIndex(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	family, err := model.ContactsByPortalToken(h.DB, token)
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

	today := time.Now().Format("2006-01-02")
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
			Overdue:   due > 0 && inv.DueDate < today && (inv.Status == model.StatusSent || inv.Status == model.StatusPartial),
			PDFPath:   fmt.Sprintf("/i/%s/invoice/%d/pdf", token, inv.ID),
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
		Title: "Tagihan " + strings.Join(names, " & "),
		Data: portalData{
			FamilyLabel:     strings.Join(names, " & "),
			Invoices:        views,
			HasCurrentMonth: hasCurrentMonth,
			TotalDue:        totalDue,
			Company:         company,
		},
	})
}

// PortalInvoicePDF serves one invoice's PDF at GET
// /i/{token}/invoice/{id}/pdf. The token must resolve to a family that
// actually owns the invoice, and drafts are never served — both guard
// against a parent enumerating another family's or an unfinalized invoice.
func (h *Handler) PortalInvoicePDF(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	family, err := model.ContactsByPortalToken(h.DB, token)
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
