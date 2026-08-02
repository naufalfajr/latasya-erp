package handler_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestNewAccount_RendersForm(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/accounts/new", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, `action="/accounts"`) {
		t.Error("expected new-account form to post to /accounts")
	}
	if !strings.Contains(body, "New Account") {
		t.Error("expected 'New Account' heading")
	}
}

func TestEditAccount_RendersForm(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var id int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '1-1002'").Scan(&id); err != nil {
		t.Fatalf("lookup seeded account: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/accounts/"+strconv.Itoa(id)+"/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Edit Account") {
		t.Error("expected 'Edit Account' heading")
	}
	if !strings.Contains(body, "1-1002") {
		t.Error("expected the account code to be pre-filled")
	}
}

func TestEditAccount_InvalidID_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/accounts/not-a-number/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-numeric id, got %d", resp.StatusCode)
	}
}

func TestEditAccount_UnknownID_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/accounts/999999/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown id, got %d", resp.StatusCode)
	}
}

func TestUpdateAccount_InvalidID_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := "code=9-0001&name=X&account_type=asset&normal_balance=debit"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts/not-a-number", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-numeric id, got %d", resp.StatusCode)
	}
}

func TestUpdateAccount_UnknownID_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := "code=9-0001&name=X&account_type=asset&normal_balance=debit"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts/999999", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown id, got %d", resp.StatusCode)
	}
}

func TestUpdateAccount_DuplicateCode(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var targetID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '1-1003'").Scan(&targetID); err != nil {
		t.Fatalf("lookup seeded account: %v", err)
	}

	// Try to rename it to another account's already-existing code.
	form := "code=1-1001&name=Renamed&account_type=asset&normal_balance=debit&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts/"+strconv.Itoa(targetID), cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render on duplicate code), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "already exists") {
		t.Error("expected 'already exists' error in body")
	}
}

func TestCreateAccount_DuplicateCode(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// '1-1001' is seeded (Cash on Hand); creating another account with the
	// same code should hit the UNIQUE constraint and re-render with an error.
	form := "code=1-1001&name=Dup&account_type=asset&normal_balance=debit&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render on duplicate code), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "already exists") {
		t.Error("expected 'already exists' error in body")
	}
}

func TestDeleteAccount_InvalidID_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/accounts/not-a-number", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-numeric id, got %d", resp.StatusCode)
	}
}

func TestDeleteAccount_SystemAccount_Forbidden(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// '1-1001' (Cash on Hand) is seeded with is_system = 1.
	var id int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&id); err != nil {
		t.Fatalf("lookup seeded account: %v", err)
	}

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/accounts/"+strconv.Itoa(id), cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 deleting a system account, got %d", resp.StatusCode)
	}

	var stillThere int
	db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id = ?", id).Scan(&stillThere)
	if stillThere != 1 {
		t.Error("system account should not have been deleted")
	}
}

func TestDeleteAccount_HTMX_RemovesRow(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	if err := testutil.CreateAccount(db, &model.Account{
		Code: "9-7777", Name: "Delete Me", AccountType: "asset", NormalBalance: "debit", IsActive: true,
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	var id int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '9-7777'").Scan(&id); err != nil {
		t.Fatalf("lookup account: %v", err)
	}

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/accounts/"+strconv.Itoa(id), cookies, "")
	req.Header.Set("HX-Request", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for HTMX delete, got %d", resp.StatusCode)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id = ?", id).Scan(&count)
	if count != 0 {
		t.Error("account should have been deleted")
	}
}
