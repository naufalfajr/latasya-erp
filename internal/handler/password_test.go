package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPasswordChangePage_RendersForm(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/password/change", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "current_password") {
		t.Error("expected the password-change form fields in the body")
	}
}

func TestPasswordChange_WrongCurrentPassword(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"current_password": {"totally-wrong"},
		"new_password":     {"NewPass12345"},
		"confirm_password": {"NewPass12345"},
	}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/password/change", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Current password is incorrect") {
		t.Error("expected 'Current password is incorrect' error in body")
	}
}

func TestPasswordChange_TooShort(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"current_password": {adminTestPassword},
		"new_password":     {"short"},
		"confirm_password": {"short"},
	}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/password/change", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "at least 8 characters") {
		t.Error("expected the minimum-length error in body")
	}
}

func TestPasswordChange_ConfirmMismatch(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"current_password": {adminTestPassword},
		"new_password":     {"NewPass12345"},
		"confirm_password": {"DoesNotMatch1"},
	}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/password/change", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "do not match") {
		t.Error("expected the confirmation-mismatch error in body")
	}
}

func TestPasswordChange_SameAsCurrentPassword(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"current_password": {adminTestPassword},
		"new_password":     {adminTestPassword},
		"confirm_password": {adminTestPassword},
	}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/password/change", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "must be different from current password") {
		t.Error("expected the same-as-current error in body")
	}
}
