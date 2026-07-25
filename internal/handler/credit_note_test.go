package handler_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestNewCreditNote_Form(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/credit-notes/new", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestNewCreditNote_PrefilledFromInvoice(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SD Prefill Test', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SD Prefill Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	invForm := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=Bus+fee&line_account_id=%d&line_quantity=1&line_unit_price=5000000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, invForm)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice: expected 303, got %d", resp.StatusCode)
	}
	invLoc := resp.Header.Get("Location")
	invID := strings.TrimPrefix(invLoc, "/invoices/")

	client := &http.Client{}
	reqPrefill, _ := requestWithCookies(db, "GET", ts.URL+"/credit-notes/new?invoice_id="+invID, cookies, "")
	respPrefill, err := client.Do(reqPrefill)
	if err != nil {
		t.Fatal(err)
	}
	defer respPrefill.Body.Close()
	if respPrefill.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", respPrefill.StatusCode)
	}
	if body := readBody(t, respPrefill); !strings.Contains(body, "Bus fee") {
		t.Errorf("expected pre-filled credit note form to include invoice line description")
	}
}

func TestCreditNoteLinePartial_HTMX(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/htmx/credit-note-line", cookies, "")
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

func TestViewCreditNote_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/credit-notes/999999", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestEditCreditNote_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/credit-notes/999999/edit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateCreditNote_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/credit-notes/999999", cookies, "cn_date=2026-04-04&reason=other")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteCreditNote_InvalidID(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/credit-notes/not-a-number", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCreateCreditNote_ValidationError_InvalidReason(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	// Missing contact/date and an invalid reason to hit the "Invalid reason" branch.
	form := "contact_id=&cn_date=&reason=not-a-real-reason"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/credit-notes", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

func TestCreditNoteFullLifecycle(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SD CN Test', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SD CN Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	client := &http.Client{}

	// 1. Create a draft credit note (standalone, no invoice link).
	form := fmt.Sprintf(
		"contact_id=%d&cn_date=2026-04-04&reason=cancellation&line_description=Refund&line_account_id=%d&line_quantity=1&line_unit_price=1000000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/credit-notes", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create credit note: expected 303, got %d", resp.StatusCode)
	}
	cnLoc := resp.Header.Get("Location")
	cnIDStr := strings.TrimPrefix(cnLoc, "/credit-notes/")

	// 2. View the credit note.
	reqView, _ := requestWithCookies(db, "GET", ts.URL+cnLoc, cookies, "")
	respView, err := client.Do(reqView)
	if err != nil {
		t.Fatal(err)
	}
	defer respView.Body.Close()
	if respView.StatusCode != http.StatusOK {
		t.Errorf("view credit note: expected 200, got %d", respView.StatusCode)
	}

	// 3. Edit form renders while draft.
	reqEdit, _ := requestWithCookies(db, "GET", ts.URL+cnLoc+"/edit", cookies, "")
	respEdit, err := client.Do(reqEdit)
	if err != nil {
		t.Fatal(err)
	}
	defer respEdit.Body.Close()
	if respEdit.StatusCode != http.StatusOK {
		t.Errorf("edit credit note (draft): expected 200, got %d", respEdit.StatusCode)
	}

	// 4. Update the draft.
	updateForm := fmt.Sprintf(
		"contact_id=%d&cn_date=2026-04-05&reason=return&line_description=Refund+updated&line_account_id=%d&line_quantity=1&line_unit_price=1500000",
		contactID, revenueID,
	)
	reqUpdate, _ := requestWithCookies(db, "POST", ts.URL+cnLoc, cookies, updateForm)
	respUpdate, err := noRedirect.Do(reqUpdate)
	if err != nil {
		t.Fatal(err)
	}
	defer respUpdate.Body.Close()
	if respUpdate.StatusCode != http.StatusSeeOther {
		t.Errorf("update credit note: expected 303, got %d", respUpdate.StatusCode)
	}
	var reason string
	db.QueryRow("SELECT reason FROM credit_notes WHERE id = ?", cnIDStr).Scan(&reason)
	if reason != "return" {
		t.Errorf("expected updated reason 'return', got %q", reason)
	}

	// 5. Issue the credit note — moves it out of draft.
	reqIssue, _ := requestWithCookies(db, "POST", ts.URL+cnLoc+"/issue", cookies, "")
	respIssue, err := noRedirect.Do(reqIssue)
	if err != nil {
		t.Fatal(err)
	}
	defer respIssue.Body.Close()
	if respIssue.StatusCode != http.StatusSeeOther {
		t.Errorf("issue credit note: expected 303, got %d", respIssue.StatusCode)
	}

	// 6. Edit form on an issued (non-draft) note should redirect, not render.
	reqEditIssued, _ := requestWithCookies(db, "GET", ts.URL+cnLoc+"/edit", cookies, "")
	respEditIssued, err := noRedirect.Do(reqEditIssued)
	if err != nil {
		t.Fatal(err)
	}
	defer respEditIssued.Body.Close()
	if respEditIssued.StatusCode != http.StatusSeeOther {
		t.Errorf("edit credit note (issued): expected 303 redirect, got %d", respEditIssued.StatusCode)
	}

	// 7. Updating an issued note should be rejected by the model-layer guard
	// and re-render the form with a general error (200), not apply changes.
	reqUpdateIssued, _ := requestWithCookies(db, "POST", ts.URL+cnLoc, cookies, updateForm)
	respUpdateIssued, err := client.Do(reqUpdateIssued)
	if err != nil {
		t.Fatal(err)
	}
	defer respUpdateIssued.Body.Close()
	if respUpdateIssued.StatusCode != http.StatusOK {
		t.Errorf("update credit note (issued): expected 200 (form re-render with error), got %d", respUpdateIssued.StatusCode)
	}

	// 8. Deleting an issued note should be rejected (dependent state guard:
	// only draft credit notes can be deleted).
	reqDelete, _ := requestWithCookies(db, "DELETE", ts.URL+cnLoc, cookies, "")
	respDelete, err := noRedirect.Do(reqDelete)
	if err != nil {
		t.Fatal(err)
	}
	defer respDelete.Body.Close()
	if respDelete.StatusCode != http.StatusSeeOther {
		t.Errorf("delete issued credit note: expected 303 redirect (rejected), got %d", respDelete.StatusCode)
	}
	var stillExists int
	db.QueryRow("SELECT COUNT(*) FROM credit_notes WHERE id = ?", cnIDStr).Scan(&stillExists)
	if stillExists != 1 {
		t.Errorf("issued credit note should not have been deleted")
	}

	// 9. Create a second, untouched draft credit note and delete it via HTMX
	// — the happy-path delete branch.
	form2 := fmt.Sprintf(
		"contact_id=%d&cn_date=2026-04-06&reason=discount&line_description=Discount+2&line_account_id=%d&line_quantity=1&line_unit_price=500000",
		contactID, revenueID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/credit-notes", cookies, form2)
	resp2, err := noRedirect.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("create second credit note: expected 303, got %d", resp2.StatusCode)
	}
	cnLoc2 := resp2.Header.Get("Location")
	cnID2Str := strings.TrimPrefix(cnLoc2, "/credit-notes/")

	reqDelete2, _ := requestWithCookies(db, "DELETE", ts.URL+cnLoc2, cookies, "")
	reqDelete2.Header.Set("HX-Request", "true")
	respDelete2, err := client.Do(reqDelete2)
	if err != nil {
		t.Fatal(err)
	}
	defer respDelete2.Body.Close()
	if respDelete2.StatusCode != http.StatusOK {
		t.Errorf("htmx delete draft credit note: expected 200, got %d", respDelete2.StatusCode)
	}
	var remaining int
	db.QueryRow("SELECT COUNT(*) FROM credit_notes WHERE id = ?", cnID2Str).Scan(&remaining)
	if remaining != 0 {
		t.Errorf("draft credit note should have been deleted, found %d rows", remaining)
	}
}

func TestCreditNotes_ViewerDenied(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/credit-notes", cookies, "contact_id=1&cn_date=2026-04-04&reason=other")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer creating a credit note should get 403, got %d", resp.StatusCode)
	}
}
