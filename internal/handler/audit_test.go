package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/audit"
)

func TestAuditList_Admin(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Seed one row so the page has content.
	audit.Log(context.Background(), db, audit.Event{
		Action:        "auth.login",
		ActorUsername: "admin",
		TargetType:    "user",
		TargetID:      1,
		TargetLabel:   "admin",
	})

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/audit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if !strings.Contains(body, "auth.login") {
		t.Errorf("response should include the seeded action, got body length %d", len(body))
	}
}

func TestAuditList_ViewerDenied(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/audit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer should get 403 on /audit, got %d", resp.StatusCode)
	}
}

func TestAuditList_BookkeeperDenied(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsBookkeeper(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/audit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bookkeeper should get 403 on /audit, got %d", resp.StatusCode)
	}
}

func TestAuditList_FilterByActor(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	audit.Log(context.Background(), db, audit.Event{Action: "invoice.create", ActorUsername: "alice"})
	audit.Log(context.Background(), db, audit.Event{Action: "invoice.create", ActorUsername: "bob"})

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/audit?actor=alice", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	// Row bodies include the actor username; body should contain alice but
	// not bob (excluding header text, which just has "Actor" label).
	if !strings.Contains(body, "alice") {
		t.Errorf("filter=alice should include alice")
	}
	if strings.Count(body, "bob") > 0 {
		t.Errorf("filter=alice should not include bob anywhere in body")
	}
}

func TestAuditList_Pagination(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	for i := 0; i < 55; i++ {
		audit.Log(context.Background(), db, audit.Event{Action: "test.event", ActorUsername: "admin"})
	}

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/audit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body := readBody(t, resp)
	// 55 events → 2 pages at size 50 → pager should render.
	if !strings.Contains(body, "Page 1 of 2") {
		t.Errorf("expected 'Page 1 of 2' in pager, got body length %d", len(body))
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return b.String()
}
