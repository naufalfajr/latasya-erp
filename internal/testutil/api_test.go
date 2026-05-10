package testutil_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestAPIMatrix_SelfCheck(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	db := testutil.SetupTestDB(t)

	testutil.APIMatrix(t, ts, db, "GET", "/test", "", testutil.AuthMatrix{
		Anon: http.StatusOK,
	})
}

func TestAPIMatrix_AllScenarios(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	db := testutil.SetupTestDB(t)

	testutil.APIMatrix(t, ts, db, "GET", "/test", "", testutil.AuthMatrix{
		Anon:                http.StatusOK,
		ValidBearer:         http.StatusOK,
		ExpiredBearer:       http.StatusOK,
		RevokedBearer:       http.StatusOK,
		ScopeMissingBearer:  http.StatusOK,
		ValidCookieCSRF:     http.StatusOK,
		ValidCookieNoCSRF:   http.StatusOK,
		BearerMustChangePwd: http.StatusOK,
	})
}

func TestRecordContractCheck(t *testing.T) {
	testutil.ResetContractChecks()

	testutil.RecordContractCheck("GET", "/api/v1/accounts", 200, []byte(`{"data":[],"meta":{}}`))
	checks := testutil.GetContractChecks()
	if len(checks) != 1 {
		t.Fatalf("expected 1 contract check, got %d", len(checks))
	}
	if checks[0].Method != "GET" {
		t.Errorf("expected method GET, got %s", checks[0].Method)
	}
	if checks[0].Path != "/api/v1/accounts" {
		t.Errorf("expected path /api/v1/accounts, got %s", checks[0].Path)
	}
	if checks[0].Status != 200 {
		t.Errorf("expected status 200, got %d", checks[0].Status)
	}
}

func TestGetContractChecks_Snapshot(t *testing.T) {
	testutil.ResetContractChecks()

	testutil.RecordContractCheck("POST", "/api/v1/accounts", 201, []byte(`{"id":1}`))
	testutil.RecordContractCheck("DELETE", "/api/v1/accounts/1", 204, nil)

	checks := testutil.GetContractChecks()
	if len(checks) != 2 {
		t.Fatalf("expected 2 contract checks, got %d", len(checks))
	}
}
