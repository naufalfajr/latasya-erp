package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/auth"
	billModule "github.com/naufal/latasya-erp/internal/bill"
	"github.com/naufal/latasya-erp/internal/model"
)

type billFormData struct {
	Bill            *model.Bill
	Lines           []model.BillLine
	Contacts        []model.Contact
	ExpenseAccounts []model.Account
	AssetAccounts   []model.Account
	Errors          map[string]string
	IsEdit          bool
}

func (h *Handler) billActor(r *http.Request) billModule.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return billModule.Actor{}
	}
	return billModule.Actor{UserID: u.ID, CanManage: u.HasCapability(model.CapBillsManage)}
}
func billDraft(b *model.Bill, lines []model.BillLine) billModule.Draft {
	d := billModule.Draft{ContactID: b.ContactID, BillDate: b.BillDate, DueDate: b.DueDate, TaxAmount: b.TaxAmount, Notes: b.Notes, Lines: make([]billModule.Line, len(lines))}
	for i, l := range lines {
		d.Lines[i] = billModule.Line{Description: l.Description, Quantity: l.Quantity, UnitPrice: l.UnitPrice, AccountID: l.AccountID}
	}
	return d
}

func (h *Handler) ListBills(w http.ResponseWriter, r *http.Request) {
	f := billModule.Filter{Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("search")}
	page := parsePage(r)
	f.Limit, f.Offset = listPageSize, (page-1)*listPageSize
	result, err := h.Bills.List(r.Context(), f)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	pg := newPagination(page, result.Total)
	if pg.Page != page {
		f.Offset = pg.Offset()
		result, err = h.Bills.List(r.Context(), f)
		if err != nil {
			http.Error(w, "Internal Server Error", 500)
			return
		}
	}
	h.render(w, r, "templates/bills/index.html", "Bills", map[string]any{"Bills": result.Bills, "Filter": f.Status, "Search": f.Search, "Pagination": newPageNav(pg, map[string]string{"status": f.Status, "search": f.Search})})
}
func (h *Handler) NewBill(w http.ResponseWriter, r *http.Request) {
	fd, err := h.newBillFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	fd.Bill = &model.Bill{}
	fd.Lines = []model.BillLine{{Quantity: 100}, {Quantity: 100}}
	h.render(w, r, "templates/bills/form.html", "New Bill", fd, "templates/bills/line_partial.html")
}
func (h *Handler) CreateBill(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	b, lines := parseBill(r)
	created, err := h.Bills.Create(r.Context(), h.billActor(r), billDraft(b, lines))
	if err != nil {
		h.renderBillForm(w, r, b, lines, billFormErrors(err), false)
		return
	}
	h.setFlash(w, "Bill created successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/bills/%d", created.ID), 303)
}
func (h *Handler) ViewBill(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	b, err := h.Bills.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	options, err := h.Bills.Options(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	h.render(w, r, "templates/bills/view.html", "Bill "+b.BillNumber, map[string]any{"Bill": b, "AssetAccounts": options.AssetAccounts})
}
func (h *Handler) EditBill(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	b, err := h.Bills.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if b.Status != model.StatusDraft {
		h.setFlash(w, "Can only edit draft bills")
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/bills/%d", id), 303)
		return
	}
	fd, err := h.newBillFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	fd.Bill, fd.Lines, fd.IsEdit = b, b.Lines, true
	h.render(w, r, "templates/bills/form.html", "Edit Bill", fd, "templates/bills/line_partial.html")
}
func (h *Handler) UpdateBill(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = h.Bills.Get(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	b, lines := parseBill(r)
	b.ID = id
	if _, err = h.Bills.Update(r.Context(), h.billActor(r), id, billDraft(b, lines)); err != nil {
		h.renderBillForm(w, r, b, lines, billFormErrors(err), true)
		return
	}
	h.setFlash(w, "Bill updated successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/bills/%d", id), 303)
}
func (h *Handler) ReceiveBill(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = h.Bills.Receive(r.Context(), h.billActor(r), id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		h.setFlash(w, "Bill received — journal entry created")
	}
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/bills/%d", id), 303)
}
func (h *Handler) BillPayment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	amount := parseIDR(r.FormValue("amount"))
	date := r.FormValue("payment_date")
	account, _ := strconv.Atoi(r.FormValue("payment_account"))
	if _, err = h.Bills.RecordPayment(r.Context(), h.billActor(r), id, billModule.Payment{Amount: amount, PaymentDate: date, PaymentAccount: account}); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		h.setFlash(w, "Payment recorded successfully")
	}
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/bills/%d", id), 303)
}
func (h *Handler) DeleteBill(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = h.Bills.Delete(r.Context(), h.billActor(r), id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/bills/%d", id), 303)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(200)
		return
	}
	h.setFlash(w, "Bill deleted")
	http.Redirect(w, r, h.BasePath+"/bills", 303)
}
func (h *Handler) BillLinePartial(w http.ResponseWriter, r *http.Request) {
	options, err := h.Bills.Options(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	t, err := h.getTemplate("templates/bills/line_partial.html")
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	t.ExecuteTemplate(w, "bill-line", map[string]any{"Accounts": options.ExpenseAccounts})
}

func pathID(r *http.Request) (int, error) { return strconv.Atoi(r.PathValue("id")) }
func parseBill(r *http.Request) (*model.Bill, []model.BillLine) {
	contact, _ := strconv.Atoi(r.FormValue("contact_id"))
	b := &model.Bill{ContactID: contact, BillDate: r.FormValue("bill_date"), DueDate: r.FormValue("due_date"), TaxAmount: parseIDR(r.FormValue("tax_amount")), Notes: r.FormValue("notes")}
	return b, parseBillLines(r)
}
func parseBillLines(r *http.Request) []model.BillLine {
	descriptions, quantities, prices, accounts := r.Form["line_description"], r.Form["line_quantity"], r.Form["line_unit_price"], r.Form["line_account_id"]
	lines := []model.BillLine{}
	for i := range descriptions {
		desc := getIndex(descriptions, i)
		qty := parseQuantity(getIndex(quantities, i))
		if qty == 0 {
			qty = 100
		}
		price := parseIDR(getIndex(prices, i))
		account, _ := strconv.Atoi(getIndex(accounts, i))
		if desc == "" && price == 0 && account == 0 {
			continue
		}
		lines = append(lines, model.BillLine{Description: desc, Quantity: qty, UnitPrice: price, AccountID: account, Amount: qty * price / 100})
	}
	return lines
}
func billFormErrors(err error) map[string]string {
	var validation *billModule.ValidationError
	if !errors.As(err, &validation) {
		return map[string]string{"general": err.Error()}
	}
	return transportFormFields(validation.Fields)
}
func (h *Handler) newBillFormData(r *http.Request) (billFormData, error) {
	options, err := h.Bills.Options(r.Context())
	if err != nil {
		return billFormData{}, err
	}
	return billFormData{Contacts: options.Contacts, ExpenseAccounts: options.ExpenseAccounts, AssetAccounts: options.AssetAccounts, Errors: map[string]string{}}, nil
}
func (h *Handler) renderBillForm(w http.ResponseWriter, r *http.Request, b *model.Bill, lines []model.BillLine, errs map[string]string, edit bool) {
	fd, err := h.newBillFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	fd.Bill, fd.Lines, fd.Errors, fd.IsEdit = b, lines, errs, edit
	title := "New Bill"
	if edit {
		title = "Edit Bill"
	}
	h.render(w, r, "templates/bills/form.html", title, fd, "templates/bills/line_partial.html")
}
