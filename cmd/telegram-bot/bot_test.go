package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	accountmod "github.com/naufal/latasya-erp/internal/account"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	v1accounts "github.com/naufal/latasya-erp/internal/api/v1/accounts"
	v1auth "github.com/naufal/latasya-erp/internal/api/v1/auth"
	v1expenses "github.com/naufal/latasya-erp/internal/api/v1/expenses"
	v1income "github.com/naufal/latasya-erp/internal/api/v1/income"
	v1invoices "github.com/naufal/latasya-erp/internal/api/v1/invoices"
	"github.com/naufal/latasya-erp/internal/idempotency"
	invoicemod "github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

const userID = 4242

var allScopes = []string{model.CapIncomeManage, model.CapExpensesManage, model.CapInvoicesManage}

// harness drives the bot against the real ERP API (in-process, wired like
// cmd/server/main.go) and a fake Telegram server that records every call.
type harness struct {
	t     *testing.T
	b     *bot
	db    *sql.DB
	mu    sync.Mutex
	calls []tgCall
	msgID int
	// polls are the getUpdates batches the fake serves in order; once they
	// run out it answers 401, like Telegram does for a revoked bot token.
	polls [][]update
	// dropNext makes the ERP process the next request fully, then cut the
	// connection before replying: a save that committed but whose response
	// was lost.
	dropNext atomic.Bool
}

type tgCall struct {
	Method      string
	MessageID   int    `json:"message_id"`
	Offset      int64  `json:"offset"`
	Text        string `json:"text"`
	ReplyMarkup *struct {
		InlineKeyboard keyboard `json:"inline_keyboard"`
	} `json:"reply_markup"`
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, db: testutil.SetupTestDB(t)}

	idem := v1.Idempotency(idempotency.New(h.db))
	apiMux := http.NewServeMux()
	v1auth.New(h.db, true).RegisterRoutes(apiMux)
	(&v1accounts.Handler{Accounts: accountmod.New(h.db)}).RegisterRoutes(apiMux)
	(&v1income.Handler{Journals: journal.New(h.db)}).RegisterRoutes(apiMux, idem)
	(&v1expenses.Handler{Journals: journal.New(h.db)}).RegisterRoutes(apiMux, idem)
	(&v1invoices.Handler{Invoices: invoicemod.New(h.db)}).RegisterRoutes(apiMux, idem)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", v1.BearerOrCookie(h.db)(apiMux))
	erp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.dropNext.CompareAndSwap(true, false) {
			mux.ServeHTTP(httptest.NewRecorder(), r)
			panic(http.ErrAbortHandler)
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(erp.Close)

	tg := httptest.NewServer(http.HandlerFunc(h.fakeTelegram))
	t.Cleanup(tg.Close)

	h.b = newBot(tg.URL+"/botTEST", erp.URL, filepath.Join(t.TempDir(), "sessions.json"))
	// Go's transport silently replays a request carrying Idempotency-Key when a
	// reused connection drops; fresh connections keep dropped saves visible.
	h.b.apiHTTP.Transport = &http.Transport{DisableKeepAlives: true}
	return h
}

func (h *harness) fakeTelegram(w http.ResponseWriter, r *http.Request) {
	var call tgCall
	if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
		h.t.Errorf("decode telegram request: %v", err)
	}
	call.Method = strings.TrimPrefix(r.URL.Path, "/botTEST/")
	if n := len([]rune(call.Text)); n > 4096 {
		h.t.Errorf("%s text has %d chars, over Telegram's 4096 limit", call.Method, n)
	}
	if call.ReplyMarkup != nil {
		for _, row := range call.ReplyMarkup.InlineKeyboard {
			for _, btn := range row {
				if n := len(btn.CallbackData); n == 0 || n > 64 {
					h.t.Errorf("button %q callback_data is %d bytes, want 1-64", btn.Text, n)
				}
			}
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, call)
	if call.Method == "getUpdates" {
		if len(h.polls) == 0 {
			fmt.Fprint(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
			return
		}
		batch, _ := json.Marshal(h.polls[0])
		h.polls = h.polls[1:]
		fmt.Fprintf(w, `{"ok":true,"result":%s}`, batch)
		return
	}
	h.msgID++
	fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, h.msgID)
}

// send delivers a typed message from the test user and returns its ID.
func (h *harness) send(text string) int {
	h.mu.Lock()
	h.msgID++
	id := h.msgID
	h.mu.Unlock()
	h.b.handle(update{Message: &message{MessageID: id, From: &tgUser{ID: userID},
		Chat: tgChat{ID: userID, Type: "private"}, Text: text}})
	return id
}

func (h *harness) callsTo(method string) []tgCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []tgCall
	for _, c := range h.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// last is the most recent message the bot sent or edited.
func (h *harness) last() tgCall {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.calls) - 1; i >= 0; i-- {
		if c := h.calls[i]; c.Method == "sendMessage" || c.Method == "editMessageText" {
			return c
		}
	}
	h.t.Fatal("bot has not sent any message")
	return tgCall{}
}

func (h *harness) expect(substr string) {
	h.t.Helper()
	if text := h.last().Text; !strings.Contains(text, substr) {
		h.t.Fatalf("last message does not contain %q:\n%s", substr, text)
	}
}

// button finds a button on the latest message whose label starts with prefix.
func (h *harness) button(prefix string) button {
	h.t.Helper()
	last := h.last()
	if last.ReplyMarkup != nil {
		for _, row := range last.ReplyMarkup.InlineKeyboard {
			for _, b := range row {
				if strings.HasPrefix(b.Text, prefix) {
					return b
				}
			}
		}
	}
	h.t.Fatalf("no button starting %q on last message:\n%s", prefix, last.Text)
	return button{}
}

func (h *harness) hasButton(prefix string) bool {
	last := h.last()
	if last.ReplyMarkup != nil {
		for _, row := range last.ReplyMarkup.InlineKeyboard {
			for _, b := range row {
				if strings.HasPrefix(b.Text, prefix) {
					return true
				}
			}
		}
	}
	return false
}

// tap presses a button on the latest message and returns the toast text.
func (h *harness) tap(prefix string) string {
	h.t.Helper()
	return h.press(h.button(prefix).CallbackData)
}

func (h *harness) press(data string) string {
	h.t.Helper()
	before := len(h.callsTo("answerCallbackQuery"))
	h.b.handle(update{CallbackQuery: &callbackQuery{ID: "cb", From: tgUser{ID: userID}, Data: data,
		Message: &message{MessageID: 1, Chat: tgChat{ID: userID, Type: "private"}}}})
	answers := h.callsTo("answerCallbackQuery")
	if got := len(answers) - before; got != 1 {
		h.t.Fatalf("callback answered %d times, want exactly 1", got)
	}
	return answers[len(answers)-1].Text
}

// connect creates a bookkeeper with a token and pastes it into the bot.
func (h *harness) connect(scopes ...string) (tokenID int, plaintext string, messageID int) {
	h.t.Helper()
	uid := testutil.CreateTestUser(h.t, h.db, fmt.Sprintf("budi%d", time.Now().UnixNano()), "pw", model.RoleBookkeeper)
	tok, plaintext, err := testutil.CreateAPIToken(h.db, uid, "telegram", scopes, nil)
	if err != nil {
		h.t.Fatalf("create token: %v", err)
	}
	messageID = h.send(plaintext)
	return tok.ID, plaintext, messageID
}

func (h *harness) label(query string, args ...any) string {
	h.t.Helper()
	var label string
	if err := h.db.QueryRow(query, args...).Scan(&label); err != nil {
		h.t.Fatalf("label query: %v", err)
	}
	return clipRunes(label, 30)
}

func (h *harness) int(query string, args ...any) int {
	h.t.Helper()
	var n sql.NullInt64
	if err := h.db.QueryRow(query, args...).Scan(&n); err != nil {
		h.t.Fatalf("query %q: %v", query, err)
	}
	return int(n.Int64)
}

func today() string { return time.Now().In(wib).Format("2006-01-02") }

func TestConnect(t *testing.T) {
	t.Run("deletes token message and persists session 0600", func(t *testing.T) {
		h := newHarness(t)
		_, plaintext, msgID := h.connect(allScopes...)

		deleted := false
		for _, c := range h.callsTo("deleteMessage") {
			deleted = deleted || c.MessageID == msgID
		}
		if !deleted {
			t.Error("token message was not deleted")
		}
		h.expect("Connected as")
		if strings.Contains(h.last().Text, "Heads-up") {
			t.Error("unexpected missing-scope warning for a full token")
		}

		info, err := os.Stat(h.b.statePath)
		if err != nil {
			t.Fatalf("stat state file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("state file mode: got %o, want 600", perm)
		}
		reloaded := newBot("", "", h.b.statePath)
		if err := reloaded.loadSessions(); err != nil {
			t.Fatalf("reload sessions: %v", err)
		}
		if s := reloaded.sessions[userID]; s == nil || s.Token != plaintext || s.Username == "" {
			t.Errorf("reloaded session: got %+v", s)
		}
	})

	t.Run("token inside a sentence still connects and is deleted", func(t *testing.T) {
		h := newHarness(t)
		uid := testutil.CreateTestUser(t, h.db, "sari", "pw", model.RoleBookkeeper)
		_, plaintext, err := testutil.CreateAPIToken(h.db, uid, "telegram", allScopes, nil)
		if err != nil {
			t.Fatal(err)
		}
		h.send("/login " + plaintext)
		h.expect("Connected as sari")
		if len(h.callsTo("deleteMessage")) != 1 {
			t.Error("token message was not deleted")
		}
	})

	t.Run("warns about missing scopes", func(t *testing.T) {
		h := newHarness(t)
		h.connect(model.CapIncomeManage)
		h.expect("lacks expenses.manage, invoices.manage")
	})

	t.Run("bad token creates no session", func(t *testing.T) {
		h := newHarness(t)
		h.send("lat_" + strings.Repeat("x", 32))
		h.expect("invalid, expired, or revoked")
		if len(h.b.sessions) != 0 {
			t.Error("session stored for a bad token")
		}
		if _, err := os.Stat(h.b.statePath); !errors.Is(err, os.ErrNotExist) {
			t.Error("state file written for a bad token")
		}
	})

	t.Run("group chats are ignored", func(t *testing.T) {
		h := newHarness(t)
		h.b.handle(update{Message: &message{MessageID: 1, From: &tgUser{ID: userID},
			Chat: tgChat{ID: -100, Type: "group"}, Text: "/menu"}})
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.calls) != 0 {
			t.Errorf("bot replied in a group: %+v", h.calls)
		}
	})
}

func TestIncomeFlow(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)
	amount := func() int {
		return h.int(`SELECT SUM(l.debit) FROM journal_lines l JOIN journal_entries e ON e.id=l.entry_id WHERE e.source_type='income'`)
	}
	count := func() int { return h.int(`SELECT COUNT(*) FROM journal_entries WHERE source_type='income'`) }

	staleExpense := h.button("Expense").CallbackData
	h.tap("Income")
	h.expect("No income recorded yet.")
	if toast := h.press(staleExpense); !strings.Contains(toast, "expired") {
		t.Errorf("button from a replaced screen: toast %q, want expired", toast)
	}
	h.tap("+ Add income")
	h.expect("Income amount")
	h.send("150,000,00")
	h.expect("couldn't read that amount")
	h.send("Rp 150.000")
	h.expect("Description?")
	h.send("SPP antar jemput Budi")
	h.expect("Revenue account?")
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='4-1001'`))
	h.expect("Deposit to?")
	if h.hasButton("1-1100") {
		t.Error("Accounts Receivable offered as a deposit account; only cash accounts expected")
	}
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`))
	h.expect("Date?")
	h.tap("Today")
	h.expect("check and save")
	save := h.button("Save").CallbackData
	h.press(save)
	h.expect("Saved.")
	if count() != 1 || amount() != 150000 {
		t.Fatalf("after save: count=%d amount=%d, want 1 and 150000", count(), amount())
	}
	if date := h.label(`SELECT entry_date FROM journal_entries WHERE source_type='income'`); date != today() {
		t.Errorf("entry_date: got %s, want %s (WIB today)", date, today())
	}

	// A stale Save tap (double tap) must not create a second entry.
	if toast := h.press(save); !strings.Contains(toast, "expired") {
		t.Errorf("stale Save toast: got %q, want expired", toast)
	}
	if count() != 1 {
		t.Errorf("count after stale tap: got %d, want 1", count())
	}

	// Edit one field from the review screen.
	h.tap("Edit")
	h.expect("Edit income")
	h.tap("Amount")
	h.send("175000")
	h.expect("Rp 175.000")
	h.tap("Save")
	h.expect("Saved.")
	if amount() != 175000 {
		t.Errorf("amount after edit: got %d, want 175000", amount())
	}

	// An edit is aborted when the entry changed on the web after it was opened.
	h.tap("Edit")
	if _, err := h.db.Exec(`UPDATE journal_entries SET description='changed on web' WHERE source_type='income'`); err != nil {
		t.Fatal(err)
	}
	h.tap("Amount")
	h.send("200000")
	h.tap("Save")
	h.expect("changed elsewhere")
	h.expect("changed on web")
	if amount() != 175000 {
		t.Errorf("amount after aborted edit: got %d, want 175000", amount())
	}

	// Delete with confirmation.
	h.tap("Delete")
	h.expect("cannot be undone")
	h.tap("Yes, delete")
	h.expect("Deleted.")
	if count() != 0 {
		t.Errorf("count after delete: got %d, want 0", count())
	}
}

func TestExpenseVehicleFlow(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)

	h.tap("Expense")
	h.tap("+ Add expense")
	h.send("50000")
	h.send("Solar")
	h.expect("Expense account?")
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='5-1001'`))
	h.expect("Paid from?")
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`))
	h.expect("Vehicle?")
	h.tap("LA001")
	h.tap("Yesterday")
	h.expect("Vehicle: LA001")
	h.tap("Save")
	h.expect("Saved.")

	id := h.int(`SELECT id FROM journal_entries WHERE source_type='expense'`)
	h.tap("Edit")
	h.tap("Description")
	h.send("Solar SPBU Cibubur")
	h.tap("Save")
	h.expect("Saved.")

	entry, err := testutil.GetJournalEntry(h.db, id)
	if err != nil {
		t.Fatal(err)
	}
	vehicleID := h.int(`SELECT id FROM vehicles WHERE code='LA001'`)
	if entry.VehicleID != vehicleID || entry.Description != "Solar SPBU Cibubur" {
		t.Errorf("after edit: vehicle=%d description=%q, want %d and new description", entry.VehicleID, entry.Description, vehicleID)
	}
	if want := time.Now().In(wib).AddDate(0, 0, -1).Format("2006-01-02"); entry.EntryDate != want {
		t.Errorf("entry_date: got %s, want %s", entry.EntryDate, want)
	}
}

func TestInvoiceFlow(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)

	res, err := h.db.Exec(`INSERT INTO contacts (name, contact_type, is_active) VALUES ('Budi Santoso', 'customer', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	contactID, _ := res.LastInsertId()
	invoiceID, err := testutil.CreateInvoice(h.db, &model.Invoice{ContactID: int(contactID), InvoiceDate: "2026-09-01", DueDate: "2026-09-10"},
		[]model.InvoiceLine{{Description: "Antar jemput September", Quantity: 100, UnitPrice: 750000,
			AccountID: h.int(`SELECT id FROM accounts WHERE code='4-1001'`)}})
	if err != nil {
		t.Fatal(err)
	}
	cash := h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`)

	h.tap("Invoices")
	h.tap("Draft")
	h.tap("INV")
	h.expect("Status: draft")
	h.expect("Antar jemput September")
	h.tap("Mark as sent")
	h.expect("as sent?")
	h.tap("Yes, mark as sent")
	h.expect("Marked as sent.")
	h.expect("Status: sent")

	h.tap("Record payment")
	h.expect("Amount due: Rp 750.000")
	h.send("900000")
	h.expect("more than the amount due")
	h.send("250.000")
	h.expect("Deposit to which account?")
	h.tap(cash)
	h.tap("Today")
	h.expect("Record payment for")
	h.tap("Save")
	h.expect("Payment recorded.")
	h.expect("Status: partial")

	h.tap("Record payment")
	h.tap("Full Rp 500.000")
	h.tap(cash)
	h.tap("Today")
	h.tap("Save")
	h.expect("Status: paid")

	inv, err := testutil.GetInvoice(h.db, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status != model.StatusPaid || inv.AmountPaid != 750000 {
		t.Errorf("invoice: status=%s paid=%d, want paid 750000", inv.Status, inv.AmountPaid)
	}
}

func TestTokenLifecycle(t *testing.T) {
	t.Run("revoked token forgets session", func(t *testing.T) {
		h := newHarness(t)
		tokenID, _, _ := h.connect(allScopes...)
		if _, err := h.db.Exec(`UPDATE api_tokens SET revoked_at=datetime('now') WHERE id=?`, tokenID); err != nil {
			t.Fatal(err)
		}
		h.tap("Income")
		h.expect("revoked or expired")
		if h.b.sessions[userID] != nil {
			t.Error("session kept after token was revoked")
		}
	})

	t.Run("logout revokes token", func(t *testing.T) {
		h := newHarness(t)
		tokenID, plaintext, _ := h.connect(allScopes...)
		h.send("/logout")
		h.expect("token is revoked")
		if h.int(`SELECT COUNT(*) FROM api_tokens WHERE id=? AND revoked_at IS NOT NULL`, tokenID) != 1 {
			t.Error("token not revoked in ERP")
		}
		data, _ := os.ReadFile(h.b.statePath)
		if strings.Contains(string(data), plaintext) {
			t.Error("token still in state file after logout")
		}
	})
}

func TestParse(t *testing.T) {
	amounts := map[string]int{
		"150000": 150000, "Rp 150.000": 150000, "rp150.000": 150000, "Rp. 1.500.000": 1500000,
		"150,000": 150000, "1.500.000": 1500000, "150 000": 150000,
		"150000,00": 0, "1.500,50": 0, "1.500,000": 0, "0": 0, "-5": 0, "": 0, "abc": 0, "1.5jt": 0,
	}
	for in, want := range amounts {
		got, ok := parseAmount(in)
		if ok != (want > 0) || got != want {
			t.Errorf("parseAmount(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}

	dates := map[string]string{
		"2026-09-30": "2026-09-30", "30/9/2026": "2026-09-30", "30/09/2026": "2026-09-30", " 1/2/2026 ": "2026-02-01",
		"31/02/2026": "", "2026-02-31": "", "30-9-2026": "", "yesterday": "",
	}
	for in, want := range dates {
		got, ok := parseDate(in)
		if ok != (want != "") || got != want {
			t.Errorf("parseDate(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}

	for n, want := range map[int]string{0: "Rp 0", 999: "Rp 999", 150000: "Rp 150.000", 1500000: "Rp 1.500.000", -1500: "-Rp 1.500"} {
		if got := idr(n); got != want {
			t.Errorf("idr(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestRedactHidesBotToken(t *testing.T) {
	err := redact(&url.Error{Op: "Post", URL: "https://api.telegram.org/bot123:SECRET/getUpdates", Err: errors.New("i/o timeout")})
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("bot token leaked in error: %v", err)
	}
}

// startIncome walks the add-income wizard up to the review screen.
func (h *harness) startIncome(amount string) {
	h.t.Helper()
	h.send("/income")
	h.tap("+ Add income")
	h.send(amount)
	h.send("Uang antar jemput")
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='4-1001'`))
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`))
	h.tap("Today")
	h.expect("check and save")
}

func TestSaveFailures(t *testing.T) {
	count := func(h *harness) int { return h.int(`SELECT COUNT(*) FROM journal_entries WHERE source_type='income'`) }

	t.Run("validation error keeps the form open", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		h.startIncome("100000")
		// The chosen revenue account is deactivated on the web meanwhile.
		h.db.Exec(`UPDATE accounts SET is_active=0 WHERE code='4-1001'`)
		h.tap("Save")
		h.expect("Please fix: accounts — accounts must be active")
		h.expect("check and save")
		if count(h) != 0 {
			t.Fatal("entry saved despite validation error")
		}
		h.db.Exec(`UPDATE accounts SET is_active=1 WHERE code='4-1001'`)
		h.tap("Save")
		h.expect("Saved.")
		if count(h) != 1 {
			t.Errorf("count after fixing: got %d, want 1", count(h))
		}
	})

	t.Run("lost response then retry saves once", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		h.startIncome("100000")
		h.dropNext.Store(true)
		h.tap("Save")
		h.expect("ERP is unreachable")
		h.expect("Tap Save to retry")
		if count(h) != 1 {
			t.Fatalf("setup: the dropped save should have committed, count=%d", count(h))
		}
		h.tap("Save")
		h.expect("Saved.")
		if count(h) != 1 {
			t.Errorf("retry duplicated the entry: count=%d, want 1", count(h))
		}
	})

	t.Run("changed form after lost response is not saved twice", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		h.startIncome("100000")
		h.dropNext.Store(true)
		h.tap("Save")
		h.expect("Tap Save to retry")
		h.tap("Amount")
		h.send("120000")
		h.tap("Save")
		h.expect("may already be saved")
		if count(h) != 1 {
			t.Errorf("count: got %d, want 1", count(h))
		}
	})

	t.Run("forbidden closes the form with the ERP message", func(t *testing.T) {
		h := newHarness(t)
		h.connect(model.CapExpensesManage) // no income.manage
		h.startIncome("100000")
		h.tap("Save")
		h.expect("ERP says: income.manage capability required")
		if toast := h.tap("« Menu"); toast != "" {
			t.Errorf("menu after error: toast %q", toast)
		}
		if count(h) != 0 {
			t.Error("entry saved without income.manage")
		}
	})
}

func TestNavigation(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)
	revenue := h.int(`SELECT id FROM accounts WHERE code='4-1001'`)
	cash := h.int(`SELECT id FROM accounts WHERE code='1-1001'`)
	for i := 1; i <= pageSize+1; i++ {
		if _, err := journal.New(h.db).CreateIncome(t.Context(), journal.Actor{UserID: 1, CanManageIncome: true},
			journal.IncomeDraft{EntryDate: fmt.Sprintf("2026-09-%02d", i), Description: fmt.Sprintf("Entry %d", i),
				Amount: 1000 * i, RevenueAccount: revenue, DepositAccount: cash}); err != nil {
			t.Fatal(err)
		}
	}

	h.send("/income")
	h.expect("page 1 of 2")
	h.tap("Next ›")
	h.expect("page 2 of 2")
	h.tap("1 Sep") // oldest entry lives on page 2
	h.expect("Entry 1")
	h.tap("« Back")
	h.expect("page 2 of 2")

	// /cancel closes the form: typed text is no longer taken as an amount.
	h.tap("+ Add income")
	h.send("/cancel")
	h.expect("Cancelled.")
	h.send("5000")
	h.expect("Send /menu")

	// Deleted elsewhere between the confirm prompt and "Yes, delete".
	h.send("/income")
	h.tap("11 Sep")
	h.tap("Delete")
	id := h.int(`SELECT id FROM journal_entries WHERE description='Entry 11'`)
	if _, err := journal.New(h.db).DeleteIncome(t.Context(), journal.Actor{UserID: 1, CanManageIncome: true}, id); err != nil {
		t.Fatal(err)
	}
	h.tap("Yes, delete")
	h.expect("Already deleted.")
}

func TestRunLoop(t *testing.T) {
	h := newHarness(t)
	h.polls = [][]update{{{UpdateID: 7, Message: &message{MessageID: 1, From: &tgUser{ID: userID},
		Chat: tgChat{ID: userID, Type: "private"}, Text: "/start"}}}}

	// A deadline turns "keeps retrying a revoked bot token" into a failure
	// instead of a hung test.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := h.b.run(ctx)
	var te *tgError
	if !errors.As(err, &te) || te.Code != http.StatusUnauthorized {
		t.Fatalf("run: got %v, want exit on Telegram 401", err)
	}
	h.expect("To connect") // the queued /start was handled
	polls := h.callsTo("getUpdates")
	if len(polls) != 2 || polls[1].Offset != 8 {
		t.Errorf("getUpdates calls %+v, want 2 with the second confirming offset 8", polls)
	}
	if len(h.callsTo("setMyCommands")) != 1 {
		t.Error("commands not registered at startup")
	}
}

func TestHandleSafelyContainsPanics(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)
	h.b.chats = nil // any chat write now panics
	h.b.handleSafely(update{UpdateID: 1, Message: &message{MessageID: 99, From: &tgUser{ID: userID},
		Chat: tgChat{ID: userID, Type: "private"}, Text: "/menu"}})
}
