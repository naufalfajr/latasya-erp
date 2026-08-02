package handler_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNewBill_Form(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/bills/new", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "form") {
		t.Errorf("expected new bill page to render a form")
	}
}

func TestBillLinePartial_HTMX(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/htmx/bill-line", cookies, "")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestViewBill_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/bills/999999", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestEditBill_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/bills/999999/edit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateBill_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills/999999", cookies, "bill_date=2026-04-04&due_date=2026-04-30")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteBill_InvalidID(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/bills/not-a-number", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCreateBill_ValidationError(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Missing contact, dates, and line items entirely.
	form := "contact_id=&bill_date=&due_date="
	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

func TestCreateBill_LineValidationError(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var contactID int
	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SPBU Line Test', 'supplier', 1)")
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SPBU Line Test'").Scan(&contactID)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Valid header fields but a line item missing price/account is rejected by
	// the bill module and rendered back into the HTML form.
	form := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-04-30&line_description=Diesel&line_quantity=1&line_unit_price=0&line_account_id=0",
		contactID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "must be positive") || !strings.Contains(string(body), "input-error") {
		t.Fatalf("missing row validation feedback: %s", body)
	}
}

func TestBillFullLifecycle(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SPBU Full Test', 'supplier', 1)")
	var contactID, fuelID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SPBU Full Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	client := &http.Client{}

	// 1. Create a draft bill.
	form := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-04-30&line_description=Diesel&line_account_id=%d&line_quantity=1&line_unit_price=2000000",
		contactID, fuelID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create bill: expected 303, got %d", resp.StatusCode)
	}
	billLoc := resp.Header.Get("Location")

	// 2. View the bill.
	reqView, _ := requestWithCookies(db, "GET", ts.URL+billLoc, cookies, "")
	respView, err := client.Do(reqView)
	if err != nil {
		t.Fatal(err)
	}
	defer respView.Body.Close()
	if respView.StatusCode != http.StatusOK {
		t.Errorf("view bill: expected 200, got %d", respView.StatusCode)
	}

	// 3. Edit form should render while still draft.
	reqEdit, _ := requestWithCookies(db, "GET", ts.URL+billLoc+"/edit", cookies, "")
	respEdit, err := client.Do(reqEdit)
	if err != nil {
		t.Fatal(err)
	}
	defer respEdit.Body.Close()
	if respEdit.StatusCode != http.StatusOK {
		t.Errorf("edit bill (draft): expected 200, got %d", respEdit.StatusCode)
	}

	// 4. Update the draft bill with a new due date.
	updateForm := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-05-15&line_description=Diesel+Updated&line_account_id=%d&line_quantity=1&line_unit_price=2500000",
		contactID, fuelID,
	)
	reqUpdate, _ := requestWithCookies(db, "POST", ts.URL+billLoc, cookies, updateForm)
	respUpdate, err := noRedirect.Do(reqUpdate)
	if err != nil {
		t.Fatal(err)
	}
	defer respUpdate.Body.Close()
	if respUpdate.StatusCode != http.StatusSeeOther {
		t.Errorf("update bill: expected 303, got %d", respUpdate.StatusCode)
	}

	var dueDate string
	billIDStr := strings.TrimPrefix(billLoc, "/bills/")
	db.QueryRow("SELECT due_date FROM bills WHERE id = ?", billIDStr).Scan(&dueDate)
	if dueDate != "2026-05-15" {
		t.Errorf("expected updated due_date 2026-05-15, got %q", dueDate)
	}

	// 5. Receive the bill — moves it out of draft status.
	reqReceive, _ := requestWithCookies(db, "POST", ts.URL+billLoc+"/receive", cookies, "")
	respReceive, err := noRedirect.Do(reqReceive)
	if err != nil {
		t.Fatal(err)
	}
	defer respReceive.Body.Close()
	if respReceive.StatusCode != http.StatusSeeOther {
		t.Errorf("receive bill: expected 303, got %d", respReceive.StatusCode)
	}

	// 6. Edit form on a received (non-draft) bill should redirect away, not render.
	reqEditReceived, _ := requestWithCookies(db, "GET", ts.URL+billLoc+"/edit", cookies, "")
	respEditReceived, err := noRedirect.Do(reqEditReceived)
	if err != nil {
		t.Fatal(err)
	}
	defer respEditReceived.Body.Close()
	if respEditReceived.StatusCode != http.StatusSeeOther {
		t.Errorf("edit bill (received): expected 303 redirect, got %d", respEditReceived.StatusCode)
	}

	// 7. Attempting to update a received bill should re-render the form with
	// a general error rather than applying the change (model-layer guard).
	reqUpdateReceived, _ := requestWithCookies(db, "POST", ts.URL+billLoc, cookies, updateForm)
	respUpdateReceived, err := client.Do(reqUpdateReceived)
	if err != nil {
		t.Fatal(err)
	}
	defer respUpdateReceived.Body.Close()
	if respUpdateReceived.StatusCode != http.StatusOK {
		t.Errorf("update bill (received): expected 200 (form re-render with error), got %d", respUpdateReceived.StatusCode)
	}

	// 8. Attempting to delete a received bill should be rejected (dependent
	// state guard: only draft bills can be deleted) and redirect back to the
	// bill rather than deleting it.
	reqDelete, _ := requestWithCookies(db, "DELETE", ts.URL+billLoc, cookies, "")
	respDelete, err := noRedirect.Do(reqDelete)
	if err != nil {
		t.Fatal(err)
	}
	defer respDelete.Body.Close()
	if respDelete.StatusCode != http.StatusSeeOther {
		t.Errorf("delete received bill: expected 303 redirect (rejected), got %d", respDelete.StatusCode)
	}
	var stillExists int
	db.QueryRow("SELECT COUNT(*) FROM bills WHERE id = ?", billIDStr).Scan(&stillExists)
	if stillExists != 1 {
		t.Errorf("received bill should not have been deleted")
	}

	// 9. Pay off the bill in full so we can also exercise DeleteBill's HTMX
	// response path on an entirely separate draft bill below.
	payForm := fmt.Sprintf("payment_date=2026-04-10&amount=2500000&payment_account=%d", cashID)
	reqPay, _ := requestWithCookies(db, "POST", ts.URL+billLoc+"/payment", cookies, payForm)
	respPay, err := noRedirect.Do(reqPay)
	if err != nil {
		t.Fatal(err)
	}
	defer respPay.Body.Close()
	if respPay.StatusCode != http.StatusSeeOther {
		t.Errorf("pay bill: expected 303, got %d", respPay.StatusCode)
	}

	// 10. Create a second, untouched draft bill and delete it via HTMX — the
	// happy-path delete branch.
	form2 := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-06&due_date=2026-04-20&line_description=Diesel+2&line_account_id=%d&line_quantity=1&line_unit_price=1000000",
		contactID, fuelID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, form2)
	resp2, err := noRedirect.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("create second bill: expected 303, got %d", resp2.StatusCode)
	}
	billLoc2 := resp2.Header.Get("Location")

	reqDelete2, _ := requestWithCookies(db, "DELETE", ts.URL+billLoc2, cookies, "")
	reqDelete2.Header.Set("HX-Request", "true")
	respDelete2, err := client.Do(reqDelete2)
	if err != nil {
		t.Fatal(err)
	}
	defer respDelete2.Body.Close()
	if respDelete2.StatusCode != http.StatusOK {
		t.Errorf("htmx delete draft bill: expected 200, got %d", respDelete2.StatusCode)
	}
	billID2Str := strings.TrimPrefix(billLoc2, "/bills/")
	var remaining int
	db.QueryRow("SELECT COUNT(*) FROM bills WHERE id = ?", billID2Str).Scan(&remaining)
	if remaining != 0 {
		t.Errorf("draft bill should have been deleted, found %d rows", remaining)
	}
}

func TestBills_ViewerDenied(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, "contact_id=1&bill_date=2026-04-04&due_date=2026-04-30")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer creating a bill should get 403, got %d", resp.StatusCode)
	}
}
