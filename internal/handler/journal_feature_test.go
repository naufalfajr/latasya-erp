package handler_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestListJournalsAccountFilterSummaryAndInvalidState(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	var cashID, revenueID, expenseID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cashID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenueID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='5-1001'").Scan(&expenseID); err != nil {
		t.Fatal(err)
	}
	entryID, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-10", Description: "April tuition receipt", SourceType: model.SourceManual, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: cashID, Debit: 500}, {AccountID: revenueID, Credit: 500}})
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-11", Description: "April unrelated receipt", SourceType: model.SourceManual, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: revenueID, Debit: 500}, {AccountID: cashID, Credit: 500}})
	if err != nil {
		t.Fatal(err)
	}
	nonMatchingID, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-10", Description: "April tuition without cash", SourceType: model.SourceManual, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: expenseID, Debit: 200}, {AccountID: revenueID, Credit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE accounts SET is_active=0 WHERE id=?", cashID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts(code,name,account_type,normal_balance,is_active,is_cash)
		VALUES('9-9901','Unused inactive','asset','debit',0,0)`); err != nil {
		t.Fatal(err)
	}
	var unusedID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='9-9901'").Scan(&unusedID); err != nil {
		t.Fatal(err)
	}

	query := url.Values{"account": {strconv.Itoa(cashID)}, "from": {"2026-04-01"}, "to": {"2026-04-30"}, "search": {"tuition"}}
	req, _ := requestWithCookies(db, http.MethodGet, ts.URL+"/journals?"+query.Encode(), cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered list status=%d", resp.StatusCode)
	}
	if !strings.Contains(body, fmt.Sprintf(`id="journal-%d"`, entryID)) ||
		strings.Contains(body, fmt.Sprintf(`id="journal-%d"`, otherID)) ||
		strings.Contains(body, fmt.Sprintf(`id="journal-%d"`, nonMatchingID)) {
		t.Fatal("account, date, and description filters did not select only the matching entry")
	}
	if !strings.Contains(body, "1-1001") || !strings.Contains(body, "+1 more") {
		t.Fatal("list should show the first account and the remaining line count")
	}
	if !strings.Contains(body, `optgroup label="Asset"`) {
		t.Fatal("account selector options should be grouped by account type")
	}
	if !strings.Contains(body, "(Inactive)") || !strings.Contains(body, fmt.Sprintf(`value="%d"`, cashID)) {
		t.Fatal("historical inactive account should remain available in the global selector")
	}
	if strings.Contains(body, fmt.Sprintf(`value="%d"`, unusedID)) {
		t.Fatal("inactive account without journal history should not be in the selector")
	}
	if !strings.Contains(body, `href="/journals" class="btn btn-ghost btn-sm">Reset filters</a>`) {
		t.Fatal("journal list should provide a visible reset control")
	}

	for _, invalid := range []string{"not-an-integer", "999999", strconv.Itoa(unusedID)} {
		invalidURL := ts.URL + "/journals?account=" + url.QueryEscape(invalid)
		invalidReq, _ := requestWithCookies(db, http.MethodGet, invalidURL, cookies, "")
		invalidResp, err := http.DefaultClient.Do(invalidReq)
		if err != nil {
			t.Fatal(err)
		}
		invalidBody := readBody(t, invalidResp)
		invalidResp.Body.Close()
		if invalidResp.StatusCode != http.StatusOK || !strings.Contains(invalidBody, "invalid or unavailable") ||
			!strings.Contains(invalidBody, "No journal entries found") || strings.Contains(invalidBody, fmt.Sprintf(`id="journal-%d"`, entryID)) {
			t.Fatalf("invalid account %q should show warning and zero results; status=%d", invalid, invalidResp.StatusCode)
		}
	}
	duplicateReq, _ := requestWithCookies(db, http.MethodGet,
		fmt.Sprintf("%s/journals?account=%d&account=%d", ts.URL, cashID, revenueID), cookies, "")
	duplicateResp, err := http.DefaultClient.Do(duplicateReq)
	if err != nil {
		t.Fatal(err)
	}
	duplicateBody := readBody(t, duplicateResp)
	duplicateResp.Body.Close()
	if duplicateResp.StatusCode != http.StatusOK || !strings.Contains(duplicateBody, "invalid or unavailable") ||
		!strings.Contains(duplicateBody, "No journal entries found") {
		t.Fatal("a multi-account bookmark should be treated as invalid and return no entries")
	}
}

func TestJournalAccountFilterPaginationAndHTMXPreserveQueryState(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenueID)
	for i := 0; i < 51; i++ {
		if _, err := testutil.CreateJournalEntry(db,
			&model.JournalEntry{EntryDate: "2026-04-10", Description: "Tuition page", SourceType: model.SourceManual, IsPosted: true, CreatedBy: 1},
			[]model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}}); err != nil {
			t.Fatal(err)
		}
	}

	firstReq, _ := requestWithCookies(db, http.MethodGet, fmt.Sprintf("%s/journals?account=%d&search=Tuition", ts.URL, cashID), cookies, "")
	firstResp, err := http.DefaultClient.Do(firstReq)
	if err != nil {
		t.Fatal(err)
	}
	firstBody := readBody(t, firstResp)
	firstResp.Body.Close()
	if !strings.Contains(firstBody, "Page 1 of 2") || !strings.Contains(firstBody, "[name='from'],[name='to'],[name='search'],[name='account']") {
		t.Fatal("journal filter controls and pagination should include the selected account")
	}
	if !strings.Contains(firstBody, "Reset filters") || !strings.Contains(firstBody, `name="account"`) {
		t.Fatal("journal list should render account selection and reset affordance")
	}

	secondReq, _ := requestWithCookies(db, http.MethodGet, fmt.Sprintf("%s/journals?account=%d&search=Tuition&page=2", ts.URL, cashID), cookies, "")
	secondReq.Header.Set("HX-Request", "true")
	secondReq.Header.Set("HX-Target", "journal-table")
	secondResp, err := http.DefaultClient.Do(secondReq)
	if err != nil {
		t.Fatal(err)
	}
	secondBody := readBody(t, secondResp)
	secondResp.Body.Close()
	if !strings.Contains(secondBody, "Page 2 of 2") || strings.Count(secondBody, `<tr id="journal-`) != 1 {
		t.Fatal("selected account and text query should be preserved on page two")
	}
	returnTo := fmt.Sprintf("/journals?account=%d&search=Tuition&page=2", cashID)
	if !strings.Contains(secondBody, "return_to="+url.QueryEscape(returnTo)) {
		t.Fatal("HTMX page-two rows should carry the complete filtered URL into View")
	}
}

func TestIncomeAndExpenseListsHaveViewOnlyActions(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	adminCookies := loginAsAdmin(t, ts)
	var cashID, revenueID, expenseID int
	db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code='5-1001'").Scan(&expenseID)
	if _, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-10", Description: "Income row", SourceType: model.SourceIncome, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}}); err != nil {
		t.Fatal(err)
	}
	if _, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-10", Description: "Expense row", SourceType: model.SourceExpense, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: expenseID, Debit: 100}, {AccountID: cashID, Credit: 100}}); err != nil {
		t.Fatal(err)
	}
	viewerCookies := loginAsViewer(t, ts, db)
	for _, page := range []string{"/income", "/expenses"} {
		for _, cookies := range [][]*http.Cookie{adminCookies, viewerCookies} {
			req, _ := requestWithCookies(db, http.MethodGet, ts.URL+page, cookies, "")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body := readBody(t, resp)
			resp.Body.Close()
			if !strings.Contains(body, ">View</a>") || strings.Contains(body, ">Edit</a>") || strings.Contains(body, ">Delete</button>") || strings.Contains(body, "hx-delete=") {
				t.Fatalf("%s list must expose only View actions", page)
			}
		}
	}
}

func TestJournalDetailActionsFollowSourceCapabilityAndSafeReturnContext(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	adminCookies := loginAsAdmin(t, ts)
	var cashID, revenueID, expenseID int
	db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code='5-1001'").Scan(&expenseID)
	makeEntry := func(source string, lines []model.JournalLine) int {
		t.Helper()
		id, err := testutil.CreateJournalEntry(db, &model.JournalEntry{EntryDate: "2026-04-10", Description: source + " action", SourceType: source, IsPosted: true, CreatedBy: 1}, lines)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	manualID := makeEntry(model.SourceManual, []model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}})
	incomeID := makeEntry(model.SourceIncome, []model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}})
	expenseEntryID := makeEntry(model.SourceExpense, []model.JournalLine{{AccountID: expenseID, Debit: 100}, {AccountID: cashID, Credit: 100}})
	emptySourceID := makeEntry(model.SourceManual, []model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}})
	if _, err := db.Exec("UPDATE journal_entries SET source_type='' WHERE id=?", emptySourceID); err != nil {
		t.Fatal(err)
	}
	otherID := makeEntry("generated", []model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}})
	if _, err := db.Exec("UPDATE journal_entries SET source_type='generated' WHERE id=?", otherID); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		id                                     int
		source, listPath, editPath, deletePath string
	}{
		{manualID, model.SourceManual, "/journals", "/journals/" + strconv.Itoa(manualID) + "/edit", "/journals/" + strconv.Itoa(manualID)},
		{emptySourceID, "", "/journals", "/journals/" + strconv.Itoa(emptySourceID) + "/edit", "/journals/" + strconv.Itoa(emptySourceID)},
		{incomeID, model.SourceIncome, "/income", "/income/" + strconv.Itoa(incomeID) + "/edit", "/income/" + strconv.Itoa(incomeID)},
		{expenseEntryID, model.SourceExpense, "/expenses", "/expenses/" + strconv.Itoa(expenseEntryID) + "/edit", "/expenses/" + strconv.Itoa(expenseEntryID)},
		{otherID, "generated", "/journals", "", ""},
	}
	for _, test := range tests {
		returnTo := test.listPath + "?from=2026-04-01&search=bus+fuel&page=3"
		query := url.Values{"return_to": {returnTo}}
		req, _ := requestWithCookies(db, http.MethodGet, fmt.Sprintf("%s/journals/%d?%s", ts.URL, test.id, query.Encode()), adminCookies, "")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, resp)
		resp.Body.Close()
		if !strings.Contains(body, `href="`+test.listPath+`?from=2026-04-01&amp;search=bus&#43;fuel&amp;page=3"`) {
			t.Errorf("%s detail should return to its exact originating list URL", test.source)
		}
		if test.editPath == "" {
			if strings.Contains(body, "hx-delete=") || strings.Contains(body, ">Edit</a>") {
				t.Errorf("generated source %q must not expose edit/delete actions", test.source)
			}
			continue
		}
		if !strings.Contains(body, `href="`+test.editPath+`?return_to=`) || !strings.Contains(body, `hx-delete="`+test.deletePath+`?return_to=`) {
			t.Errorf("%s detail actions should use the source-specific routes", test.source)
		}
		fallbackReq, _ := requestWithCookies(db, http.MethodGet, fmt.Sprintf("%s/journals/%d", ts.URL, test.id), adminCookies, "")
		fallbackResp, err := http.DefaultClient.Do(fallbackReq)
		if err != nil {
			t.Fatal(err)
		}
		fallbackBody := readBody(t, fallbackResp)
		fallbackResp.Body.Close()
		if !strings.Contains(fallbackBody, `href="`+test.listPath+`"`) {
			t.Errorf("direct detail link for source %q should fall back to %s", test.source, test.listPath)
		}
	}

	viewerCookies := loginAsViewer(t, ts, db)
	viewerReq, _ := requestWithCookies(db, http.MethodGet, fmt.Sprintf("%s/journals/%d", ts.URL, incomeID), viewerCookies, "")
	viewerResp, err := http.DefaultClient.Do(viewerReq)
	if err != nil {
		t.Fatal(err)
	}
	viewerBody := readBody(t, viewerResp)
	viewerResp.Body.Close()
	if strings.Contains(viewerBody, "hx-delete=") || strings.Contains(viewerBody, ">Edit</a>") {
		t.Fatal("read-only user should not see journal mutation actions")
	}
	for _, capabilityCase := range []struct {
		username   string
		capability string
		allowedID  int
	}{
		{"income-only", model.CapIncomeManage, incomeID},
		{"expense-only", model.CapExpensesManage, expenseEntryID},
		{"journal-only", model.CapJournalsManage, manualID},
	} {
		limitedCookies := loginAsCapabilityUser(t, ts, db, capabilityCase.username, capabilityCase.capability)
		for _, test := range []struct {
			id      int
			canEdit bool
		}{{incomeID, incomeID == capabilityCase.allowedID}, {expenseEntryID, expenseEntryID == capabilityCase.allowedID}, {manualID, manualID == capabilityCase.allowedID}, {emptySourceID, manualID == capabilityCase.allowedID}} {
			limitedReq, _ := requestWithCookies(db, http.MethodGet, fmt.Sprintf("%s/journals/%d", ts.URL, test.id), limitedCookies, "")
			limitedResp, err := http.DefaultClient.Do(limitedReq)
			if err != nil {
				t.Fatal(err)
			}
			limitedBody := readBody(t, limitedResp)
			limitedResp.Body.Close()
			if strings.Contains(limitedBody, ">Edit</a>") != test.canEdit || strings.Contains(limitedBody, "hx-delete=") != test.canEdit {
				t.Errorf("%s capability rendered wrong actions for journal %d", capabilityCase.capability, test.id)
			}
		}
	}

	for _, unsafeValue := range []string{
		"https://outside.example/path", "//outside.example/path", "/journals/123", "/other",
		"/journals/../income", "/journals%2F..%2Fincome", "/\\outside.example/path",
	} {
		unsafe := url.Values{"return_to": {unsafeValue}}
		unsafeReq, _ := requestWithCookies(db, http.MethodGet, fmt.Sprintf("%s/journals/%d?%s", ts.URL, incomeID, unsafe.Encode()), adminCookies, "")
		unsafeResp, err := http.DefaultClient.Do(unsafeReq)
		if err != nil {
			t.Fatal(err)
		}
		unsafeBody := readBody(t, unsafeResp)
		unsafeResp.Body.Close()
		if !strings.Contains(unsafeBody, `href="/income"`) || strings.Contains(unsafeBody, `href="https://outside.example`) {
			t.Errorf("unsafe return destination %q should fall back to the source list", unsafeValue)
		}
	}
}

func TestJournalEditAndDeletePreserveReturnContextWithBasePath(t *testing.T) {
	t.Parallel()
	basePath := "/dashboard"
	ts, db := testServer(t, basePath)
	cookies := loginAsAdmin(t, ts, basePath)
	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenueID)
	id, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-10", Description: "Context test", SourceType: model.SourceManual, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}})
	if err != nil {
		t.Fatal(err)
	}
	listURL := fmt.Sprintf("%s%s/journals?account=%d&from=2026-04-01&page=1", ts.URL, basePath, cashID)
	listReq, _ := requestWithCookies(db, http.MethodGet, listURL, cookies, "")
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatal(err)
	}
	listBody := readBody(t, listResp)
	listResp.Body.Close()
	returnTo := "/journals?account=" + strconv.Itoa(cashID) + "&from=2026-04-01&page=1"
	if !strings.Contains(listBody, fmt.Sprintf(`href="%s/journals/%d?return_to=%s"`, basePath, id, url.QueryEscape(returnTo))) {
		t.Fatal("BasePath journal list should create a single-prefixed View URL with full list context")
	}
	// A prefixed return path from an existing bookmark is normalized to the same
	// internal route before BasePath is applied to links and redirects.
	editURL := fmt.Sprintf("%s%s/journals/%d/edit?return_to=%s", ts.URL, basePath, id, url.QueryEscape(basePath+returnTo))
	editReq, _ := requestWithCookies(db, http.MethodGet, editURL, cookies, "")
	editResp, err := http.DefaultClient.Do(editReq)
	if err != nil {
		t.Fatal(err)
	}
	editBody := readBody(t, editResp)
	editResp.Body.Close()
	if !strings.Contains(editBody, `name="return_to"`) || !strings.Contains(editBody, url.QueryEscape(returnTo)) {
		t.Fatal("journal edit form should carry the exact return context")
	}
	form := url.Values{
		"entry_date": {"2026-04-11"}, "description": {"Context updated"}, "return_to": {returnTo},
		"line_account_id": {strconv.Itoa(cashID), strconv.Itoa(revenueID)},
		"line_debit":      {"200", "0"}, "line_credit": {"0", "200"}, "line_memo": {"", ""},
	}
	updateReq, _ := requestWithCookies(db, http.MethodPost, fmt.Sprintf("%s%s/journals/%d", ts.URL, basePath, id), cookies, form.Encode())
	updateResp, err := noRedirectClient().Do(updateReq)
	if err != nil {
		t.Fatal(err)
	}
	updateResp.Body.Close()
	updatedLocation, err := url.Parse(updateResp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if updateResp.StatusCode != http.StatusSeeOther || updatedLocation.Path != basePath+fmt.Sprintf("/journals/%d", id) || updatedLocation.Query().Get("return_to") != returnTo {
		t.Fatalf("update should return to detail and preserve origin; status=%d location=%q", updateResp.StatusCode, updateResp.Header.Get("Location"))
	}

	deleteReq, _ := requestWithCookies(db, http.MethodDelete, fmt.Sprintf("%s%s/journals/%d?return_to=%s", ts.URL, basePath, id, url.QueryEscape(returnTo)), cookies, "")
	deleteResp, err := noRedirectClient().Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusSeeOther || deleteResp.Header.Get("Location") != basePath+returnTo {
		t.Fatalf("successful delete should return to originating list with BasePath; status=%d location=%q", deleteResp.StatusCode, deleteResp.Header.Get("Location"))
	}
}

func TestIncomeAndExpenseEditAndDeletePreserveReturnContextWithBasePath(t *testing.T) {
	t.Parallel()
	basePath := "/dashboard"
	ts, db := testServer(t, basePath)
	cookies := loginAsAdmin(t, ts, basePath)
	var cashID, revenueID, expenseID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cashID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenueID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='5-1001'").Scan(&expenseID); err != nil {
		t.Fatal(err)
	}
	incomeID, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-10", Description: "Income context", SourceType: model.SourceIncome, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}})
	if err != nil {
		t.Fatal(err)
	}
	expenseIDEntry, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-10", Description: "Expense context", SourceType: model.SourceExpense, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: expenseID, Debit: 100}, {AccountID: cashID, Credit: 100}})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		id, listPath, editPath, updatePath string
		validForm                          url.Values
	}{
		{
			id: strconv.Itoa(incomeID), listPath: "/income", editPath: "/income/" + strconv.Itoa(incomeID) + "/edit", updatePath: "/income/" + strconv.Itoa(incomeID),
			validForm: url.Values{"entry_date": {"2026-04-11"}, "description": {"Income context updated"}, "amount": {"200"},
				"revenue_account": {strconv.Itoa(revenueID)}, "deposit_account": {strconv.Itoa(cashID)}},
		},
		{
			id: strconv.Itoa(expenseIDEntry), listPath: "/expenses", editPath: "/expenses/" + strconv.Itoa(expenseIDEntry) + "/edit", updatePath: "/expenses/" + strconv.Itoa(expenseIDEntry),
			validForm: url.Values{"entry_date": {"2026-04-11"}, "description": {"Expense context updated"}, "amount": {"200"},
				"expense_account": {strconv.Itoa(expenseID)}, "payment_account": {strconv.Itoa(cashID)}},
		},
	}
	for _, test := range tests {
		returnTo := test.listPath + "?from=2026-04-01&search=school+trip&page=2"
		prefixedReturnTo := basePath + returnTo
		editQuery := url.Values{"return_to": {prefixedReturnTo}}
		editReq, _ := requestWithCookies(db, http.MethodGet,
			fmt.Sprintf("%s%s%s?%s", ts.URL, basePath, test.editPath, editQuery.Encode()), cookies, "")
		editResp, err := http.DefaultClient.Do(editReq)
		if err != nil {
			t.Fatal(err)
		}
		editBody := readBody(t, editResp)
		editResp.Body.Close()
		if editResp.StatusCode != http.StatusOK || !strings.Contains(editBody, `name="return_to"`) || !strings.Contains(editBody, url.QueryEscape(returnTo)) {
			t.Fatalf("%s edit form should retain the normalized originating list; status=%d", test.listPath, editResp.StatusCode)
		}

		badForm := url.Values{"entry_date": {""}, "description": {""}, "amount": {"0"}, "return_to": {returnTo}}
		badReq, _ := requestWithCookies(db, http.MethodPost, fmt.Sprintf("%s%s%s", ts.URL, basePath, test.updatePath), cookies, badForm.Encode())
		badResp, err := http.DefaultClient.Do(badReq)
		if err != nil {
			t.Fatal(err)
		}
		badBody := readBody(t, badResp)
		badResp.Body.Close()
		if badResp.StatusCode != http.StatusOK || !strings.Contains(badBody, `name="return_to"`) || !strings.Contains(badBody, url.QueryEscape(returnTo)) {
			t.Fatalf("%s validation response should retain the originating list; status=%d", test.listPath, badResp.StatusCode)
		}

		test.validForm.Set("return_to", returnTo)
		updateReq, _ := requestWithCookies(db, http.MethodPost, fmt.Sprintf("%s%s%s", ts.URL, basePath, test.updatePath), cookies, test.validForm.Encode())
		updateResp, err := noRedirectClient().Do(updateReq)
		if err != nil {
			t.Fatal(err)
		}
		updateResp.Body.Close()
		location, err := url.Parse(updateResp.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if updateResp.StatusCode != http.StatusSeeOther || location.Path != basePath+"/journals/"+test.id || location.Query().Get("return_to") != returnTo {
			t.Fatalf("%s update should return to shared detail with origin; status=%d location=%q", test.listPath, updateResp.StatusCode, updateResp.Header.Get("Location"))
		}

		deleteURL := fmt.Sprintf("%s%s%s?return_to=%s", ts.URL, basePath, test.updatePath, url.QueryEscape(returnTo))
		deleteReq, _ := requestWithCookies(db, http.MethodDelete, deleteURL, cookies, "")
		deleteResp, err := noRedirectClient().Do(deleteReq)
		if err != nil {
			t.Fatal(err)
		}
		deleteResp.Body.Close()
		if deleteResp.StatusCode != http.StatusSeeOther || deleteResp.Header.Get("Location") != basePath+returnTo {
			t.Fatalf("%s delete should return to the filtered list; status=%d location=%q", test.listPath, deleteResp.StatusCode, deleteResp.Header.Get("Location"))
		}
	}
}

func TestDeleteFailureReturnsToDetailAndHTMXDeleteRedirectsToOrigin(t *testing.T) {
	t.Parallel()
	basePath := "/dashboard"
	ts, db := testServer(t, basePath)
	cookies := loginAsAdmin(t, ts, basePath)
	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenueID)
	incomeID, err := testutil.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-10", Description: "Keep on wrong source delete", SourceType: model.SourceIncome, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{{AccountID: cashID, Debit: 100}, {AccountID: revenueID, Credit: 100}})
	if err != nil {
		t.Fatal(err)
	}
	returnTo := "/income?search=tuition&page=2"
	wrongReq, _ := requestWithCookies(db, http.MethodDelete, fmt.Sprintf("%s%s/expenses/%d?return_to=%s", ts.URL, basePath, incomeID, url.QueryEscape(returnTo)), cookies, "")
	wrongResp, err := noRedirectClient().Do(wrongReq)
	if err != nil {
		t.Fatal(err)
	}
	wrongResp.Body.Close()
	loc, _ := url.Parse(wrongResp.Header.Get("Location"))
	if wrongResp.StatusCode != http.StatusSeeOther || loc.Path != basePath+fmt.Sprintf("/journals/%d", incomeID) || loc.Query().Get("return_to") != returnTo {
		t.Fatalf("failed delete should return to shared detail; status=%d location=%q", wrongResp.StatusCode, wrongResp.Header.Get("Location"))
	}
	if flashValue(wrongResp) == "" {
		t.Fatal("failed delete should set an error flash")
	}

	hxWrongReq, _ := requestWithCookies(db, http.MethodDelete,
		fmt.Sprintf("%s%s/expenses/%d?return_to=%s", ts.URL, basePath, incomeID, url.QueryEscape(returnTo)), cookies, "")
	hxWrongReq.Header.Set("HX-Request", "true")
	hxWrongResp, err := noRedirectClient().Do(hxWrongReq)
	if err != nil {
		t.Fatal(err)
	}
	hxWrongResp.Body.Close()
	if hxWrongResp.StatusCode != http.StatusOK || hxWrongResp.Header.Get("HX-Redirect") != basePath+"/journals/"+strconv.Itoa(incomeID)+"?return_to="+url.QueryEscape(returnTo) {
		t.Fatalf("failed HTMX delete should stay on shared detail; status=%d HX-Redirect=%q", hxWrongResp.StatusCode, hxWrongResp.Header.Get("HX-Redirect"))
	}

	deleteReq, _ := requestWithCookies(db, http.MethodDelete, fmt.Sprintf("%s%s/income/%d?return_to=%s", ts.URL, basePath, incomeID, url.QueryEscape(returnTo)), cookies, "")
	deleteReq.Header.Set("HX-Request", "true")
	deleteResp, err := noRedirectClient().Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK || deleteResp.Header.Get("HX-Redirect") != basePath+returnTo {
		t.Fatalf("HTMX delete should redirect to origin; status=%d HX-Redirect=%q", deleteResp.StatusCode, deleteResp.Header.Get("HX-Redirect"))
	}
}

func loginAsCapabilityUser(t *testing.T, ts *httptest.Server, db *sql.DB, username, capability string) []*http.Cookie {
	t.Helper()
	hash, err := auth.HashPassword("capability-user-password")
	if err != nil {
		t.Fatal(err)
	}
	roleName := username + "-role"
	if _, err := db.Exec("INSERT INTO roles(name,description,is_system,capabilities) VALUES(?,?,0,?)", roleName, "test capability role", `["`+capability+`"]`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO users(username,password,full_name,role) VALUES(?,?,?,?)", username, hash, "Capability User", roleName); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(ts.URL+"/login", url.Values{"username": {username}, "password": {"capability-user-password"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("capability user login status=%d", resp.StatusCode)
	}
	return resp.Cookies()
}
