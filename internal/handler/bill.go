package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/auth"
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

func (h *Handler) ListBills(w http.ResponseWriter, r *http.Request) {
	f := model.BillFilter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
	}
	bills, err := model.ListBills(h.DB, f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/bills/index.html", "Bills", map[string]any{
		"Bills":  bills,
		"Filter": f.Status,
	})
}

func (h *Handler) NewBill(w http.ResponseWriter, r *http.Request) {
	fd := h.newBillFormData()
	fd.Bill = &model.Bill{}
	fd.Lines = []model.BillLine{{Quantity: 100}, {Quantity: 100}}
	h.render(w, r, "templates/bills/form.html", "New Bill", fd, "templates/bills/line_partial.html")
}

func (h *Handler) CreateBill(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	user := auth.UserFromContext(r.Context())

	contactID, _ := strconv.Atoi(r.FormValue("contact_id"))
	taxAmount := parseIDR(r.FormValue("tax_amount"))

	b := &model.Bill{
		ContactID: contactID,
		BillDate:  r.FormValue("bill_date"),
		DueDate:   r.FormValue("due_date"),
		TaxAmount: taxAmount,
		Notes:     r.FormValue("notes"),
		CreatedBy: user.ID,
	}

	lines := parseBillLines(r)
	errors := validateBill(b, lines)

	if len(errors) > 0 {
		fd := h.newBillFormData()
		fd.Bill = b
		fd.Lines = lines
		fd.Errors = errors
		h.render(w, r, "templates/bills/form.html", "New Bill", fd, "templates/bills/line_partial.html")
		return
	}

	billID, err := model.CreateBill(h.DB, b, lines)
	if err != nil {
		fd := h.newBillFormData()
		fd.Bill = b
		fd.Lines = lines
		fd.Errors = map[string]string{"general": err.Error()}
		h.render(w, r, "templates/bills/form.html", "New Bill", fd, "templates/bills/line_partial.html")
		return
	}

	h.setFlash(w, "Bill created successfully")
	http.Redirect(w, r, fmt.Sprintf("/bills/%d", billID), http.StatusSeeOther)
}

func (h *Handler) ViewBill(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	b, err := model.GetBill(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	active := true
	assetAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "asset", IsActive: &active})
	h.render(w, r, "templates/bills/view.html", "Bill "+b.BillNumber, map[string]any{
		"Bill":          b,
		"AssetAccounts": assetAccounts,
	})
}

func (h *Handler) EditBill(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	b, err := model.GetBill(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if b.Status != "draft" {
		h.setFlash(w, "Can only edit draft bills")
		http.Redirect(w, r, fmt.Sprintf("/bills/%d", id), http.StatusSeeOther)
		return
	}
	fd := h.newBillFormData()
	fd.Bill = b
	fd.Lines = b.Lines
	fd.IsEdit = true
	h.render(w, r, "templates/bills/form.html", "Edit Bill", fd, "templates/bills/line_partial.html")
}

func (h *Handler) UpdateBill(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	contactID, _ := strconv.Atoi(r.FormValue("contact_id"))
	taxAmount := parseIDR(r.FormValue("tax_amount"))

	b := &model.Bill{
		ID: id, ContactID: contactID, BillDate: r.FormValue("bill_date"),
		DueDate: r.FormValue("due_date"), TaxAmount: taxAmount, Notes: r.FormValue("notes"),
	}
	lines := parseBillLines(r)
	errors := validateBill(b, lines)

	if len(errors) > 0 {
		fd := h.newBillFormData()
		fd.Bill = b
		fd.Lines = lines
		fd.Errors = errors
		fd.IsEdit = true
		h.render(w, r, "templates/bills/form.html", "Edit Bill", fd, "templates/bills/line_partial.html")
		return
	}

	if err := model.UpdateBill(h.DB, b, lines); err != nil {
		fd := h.newBillFormData()
		fd.Bill = b
		fd.Lines = lines
		fd.Errors = map[string]string{"general": err.Error()}
		fd.IsEdit = true
		h.render(w, r, "templates/bills/form.html", "Edit Bill", fd, "templates/bills/line_partial.html")
		return
	}

	h.setFlash(w, "Bill updated successfully")
	http.Redirect(w, r, fmt.Sprintf("/bills/%d", id), http.StatusSeeOther)
}

func (h *Handler) ReceiveBill(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user := auth.UserFromContext(r.Context())
	if err := model.ReceiveBill(h.DB, id, user.ID); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		h.setFlash(w, "Bill received — journal entry created")
	}
	http.Redirect(w, r, fmt.Sprintf("/bills/%d", id), http.StatusSeeOther)
}

func (h *Handler) BillPayment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user := auth.UserFromContext(r.Context())
	amount := parseIDR(r.FormValue("amount"))
	paymentDate := r.FormValue("payment_date")
	paymentAccountID, _ := strconv.Atoi(r.FormValue("payment_account"))

	if amount <= 0 || paymentDate == "" || paymentAccountID == 0 {
		h.setFlash(w, "Error: all payment fields are required")
		http.Redirect(w, r, fmt.Sprintf("/bills/%d", id), http.StatusSeeOther)
		return
	}

	if err := model.RecordBillPayment(h.DB, id, amount, paymentDate, paymentAccountID, user.ID); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		h.setFlash(w, "Payment recorded successfully")
	}
	http.Redirect(w, r, fmt.Sprintf("/bills/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteBill(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := model.DeleteBill(h.DB, id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, fmt.Sprintf("/bills/%d", id), http.StatusSeeOther)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.setFlash(w, "Bill deleted")
	http.Redirect(w, r, "/bills", http.StatusSeeOther)
}

func (h *Handler) BillLinePartial(w http.ResponseWriter, r *http.Request) {
	active := true
	accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "expense", IsActive: &active})
	t, err := h.getTemplate("templates/bills/line_partial.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	t.ExecuteTemplate(w, "bill-line", map[string]any{"Accounts": accounts})
}

func parseBillLines(r *http.Request) []model.BillLine {
	descriptions := r.Form["line_description"]
	quantities := r.Form["line_quantity"]
	unitPrices := r.Form["line_unit_price"]
	accountIDs := r.Form["line_account_id"]

	var lines []model.BillLine
	for i := range descriptions {
		desc := getIndex(descriptions, i)
		qty := parseQuantity(getIndex(quantities, i))
		if qty == 0 {
			qty = 100
		}
		price := parseIDR(getIndex(unitPrices, i))
		accountID, _ := strconv.Atoi(getIndex(accountIDs, i))

		if desc == "" && price == 0 && accountID == 0 {
			continue
		}
		lines = append(lines, model.BillLine{
			Description: desc, Quantity: qty, UnitPrice: price,
			AccountID: accountID, Amount: qty * price / 100,
		})
	}
	return lines
}

func validateBill(b *model.Bill, lines []model.BillLine) map[string]string {
	errors := make(map[string]string)
	if b.ContactID == 0 {
		errors["contact_id"] = "Supplier is required"
	}
	if b.BillDate == "" {
		errors["bill_date"] = "Bill date is required"
	}
	if b.DueDate == "" {
		errors["due_date"] = "Due date is required"
	}
	if len(lines) == 0 {
		errors["lines"] = "At least one line item is required"
	}
	for i, l := range lines {
		if l.Description == "" {
			errors[fmt.Sprintf("line_%d_desc", i)] = "Description required"
		}
		if l.UnitPrice <= 0 {
			errors[fmt.Sprintf("line_%d_price", i)] = "Price required"
		}
		if l.AccountID == 0 {
			errors[fmt.Sprintf("line_%d_account", i)] = "Account required"
		}
	}
	return errors
}

func (h *Handler) newBillFormData() billFormData {
	active := true
	contacts, _ := model.ListContacts(h.DB, model.ContactFilter{Type: "supplier", IsActive: &active})
	expenseAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "expense", IsActive: &active})
	assetAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "asset", IsActive: &active})
	return billFormData{
		Contacts: contacts, ExpenseAccounts: expenseAccounts,
		AssetAccounts: assetAccounts, Errors: make(map[string]string),
	}
}
