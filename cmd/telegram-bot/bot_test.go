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
	// tgReply overrides the fake's response body for a Telegram method, e.g.
	// to make editMessageText fail.
	tgReply map[string]string
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
	if body, ok := h.tgReply[call.Method]; ok {
		fmt.Fprint(w, body)
		return
	}
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
		h.send("token_" + plaintext + "_")
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

	t.Run("lost edit response then retry reports saved", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		h.startIncome("100000")
		h.tap("Save")
		h.tap("Edit")
		h.tap("Amount")
		h.send("120000")
		next, lost := h.b.apiHTTP.Transport, false
		h.b.apiHTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp, err := next.RoundTrip(r)
			if err == nil && r.Method == http.MethodPut && !lost {
				lost = true // the PUT committed; only its reply is lost
				resp.Body.Close()
				return nil, errors.New("connection reset by peer")
			}
			return resp, err
		})
		h.tap("Save")
		h.expect("Tap Save to retry")
		h.tap("Save")
		h.expect("Saved.")
		h.expect("Rp 120.000")
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

	// Deleting the only entry on the last page returns to the new last page.
	if _, err := journal.New(h.db).CreateIncome(t.Context(), journal.Actor{UserID: 1, CanManageIncome: true},
		journal.IncomeDraft{EntryDate: "2026-09-12", Description: "Entry 12", Amount: 12000,
			RevenueAccount: revenue, DepositAccount: cash}); err != nil {
		t.Fatal(err)
	}
	h.send("/income")
	h.tap("Next ›")
	h.tap("1 Sep")
	h.tap("Delete")
	h.tap("Yes, delete")
	h.tap("« Back to list")
	h.expect("Income — recent entries")
	if !h.hasButton("2 Sep") {
		t.Errorf("back to list after delete shows no entries:\n%s", h.last().Text)
	}
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

func TestRunExitsOnMalformedBotToken(t *testing.T) {
	h := newHarness(t)
	h.tgReply = map[string]string{"getUpdates": `{"ok":false,"error_code":404,"description":"Not Found"}`}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var te *tgError
	if err := h.b.run(ctx); !errors.As(err, &te) || te.Code != http.StatusNotFound {
		t.Fatalf("run: got %v, want exit on Telegram 404", err)
	}
}

func TestHandleSafelyContainsPanics(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)
	h.b.chats = nil // any chat write now panics
	h.b.handleSafely(update{UpdateID: 1, Message: &message{MessageID: 99, From: &tgUser{ID: userID},
		Chat: tgChat{ID: userID, Type: "private"}, Text: "/menu"}})
}

func TestCommandsAndStaleCallbacks(t *testing.T) {
	h := newHarness(t)

	// Not connected: typed commands and old buttons both point at setup.
	h.send("/menu")
	h.expect("To connect")
	h.press("abcdef|mn|")
	h.expect("To connect")

	h.connect(allScopes...)
	h.send("/start")
	h.expect("Connected as")
	h.send("/expense")
	h.expect("No expense recorded yet.")
	h.send("/invoices")
	h.expect("pick a status")
	h.send("/menu@LatasyaBot")
	h.expect("What do you want to do?")
	h.send("/nope")
	h.expect("Unknown command")

	// Buttons that don't fit the current state never act, even with a valid nonce.
	nonce := h.b.chats[userID].nonce
	for _, data := range []string{"garbage", nonce + "|zz|", nonce + "|ls|x:1", nonce + "|dt|0",
		nonce + "|pf|", nonce + "|fx|amount", nonce + "|sv|", nonce + "|ac|1"} {
		if toast := h.press(data); toast != expired {
			t.Errorf("press(%q): toast %q, want %q", data, toast, expired)
		}
	}

	h.send("/income")
	h.tap("+ Add income")
	nonce = h.b.chats[userID].nonce
	for _, data := range []string{nonce + "|fx|bogus", nonce + "|sv|", nonce + "|ac|1"} {
		if toast := h.press(data); toast != expired {
			t.Errorf("mid-form press(%q): toast %q, want %q", data, toast, expired)
		}
	}
	h.tap("Cancel")
	h.expect("Cancelled.")
}

func TestReviewChangesEveryField(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)
	fuel := h.label(`SELECT code||' '||name FROM accounts WHERE code='5-1001'`)
	cash := h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`)

	h.send("/expense")
	h.tap("+ Add expense")
	h.send("75000")
	h.send("   ")
	h.expect("Please type a description")
	h.send("Servis rem")
	h.tap(fuel)
	h.tap(cash)
	h.tap("LA001")
	h.send("31/02/2026")
	h.expect("couldn't read that date")
	h.send("15/9/2026")
	h.expect("Date: 15 Sep 2026")
	h.tap("Save")
	h.expect("Saved.")
	id := h.int(`SELECT id FROM journal_entries WHERE source_type='expense'`)

	// Edit every field except amount/description (covered elsewhere),
	// including clearing the vehicle tag.
	h.tap("Edit")
	h.tap("Vehicle")
	h.tap("No vehicle")
	h.expect("Vehicle: No vehicle")
	h.tap("Date")
	h.send("2026-09-16")
	h.tap("Expense account")
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='5-2001'`))
	h.tap("Paid from")
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1002'`))
	h.expect("Edit expense")
	h.tap("Save")
	h.expect("Saved.")

	entry, err := testutil.GetJournalEntry(h.db, id)
	if err != nil {
		t.Fatal(err)
	}
	if entry.VehicleID != 0 || entry.EntryDate != "2026-09-16" {
		t.Errorf("after edit: vehicle=%d date=%s, want untagged on 2026-09-16", entry.VehicleID, entry.EntryDate)
	}
	accounts := map[string]bool{}
	for _, l := range entry.Lines {
		accounts[l.AccountCode] = true
	}
	if !accounts["5-2001"] || !accounts["1-1002"] {
		t.Errorf("after edit: accounts %v, want 5-2001 and 1-1002", accounts)
	}
}

func TestPickerFallbacks(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)
	if _, err := h.db.Exec(`UPDATE vehicles SET is_active=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`UPDATE accounts SET is_cash=0`); err != nil {
		t.Fatal(err)
	}

	h.send("/expense")
	h.tap("+ Add expense")
	h.send("10000")
	h.send("Parkir")
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='5-1001'`))
	h.expect("Paid from?")
	// No account is flagged as cash, so every active asset is offered.
	if !h.hasButton("1-1100") {
		t.Error("cash fallback should offer all active assets")
	}
	h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`))
	// No active vehicles: the vehicle step is skipped.
	h.expect("Date?")
	h.tap("Today")
	if strings.Contains(h.last().Text, "Vehicle:") && !strings.Contains(h.last().Text, "No vehicle") {
		t.Errorf("review shows a vehicle: %s", h.last().Text)
	}

	if _, err := h.db.Exec(`UPDATE accounts SET is_active=0 WHERE account_type='expense'`); err != nil {
		t.Fatal(err)
	}
	h.tap("Expense account")
	h.expect("No active accounts")
}

func TestReconnect(t *testing.T) {
	h := newHarness(t)
	oldID, _, _ := h.connect(allScopes...)
	newID, plaintext, _ := h.connect(allScopes...)
	revoked := func(id int) bool {
		return h.int(`SELECT COUNT(*) FROM api_tokens WHERE id=? AND revoked_at IS NOT NULL`, id) == 1
	}
	if !revoked(oldID) {
		t.Error("replaced token was left live")
	}
	h.send(plaintext) // pasting the current token again must not revoke it
	h.expect("Connected as")
	if revoked(newID) {
		t.Error("re-pasting the active token revoked it")
	}
}

func TestERPUnreachable(t *testing.T) {
	h := newHarness(t)
	_, plaintext, _ := h.connect(allScopes...)
	h.b.apiURL = "http://127.0.0.1:1"

	h.send(plaintext)
	h.expect("ERP is unreachable")
	h.send("/logout")
	h.expect("couldn't reach ERP")
	if h.b.sessions[userID] != nil {
		t.Error("session kept after logout")
	}
}

func TestLogoutWithRevokedToken(t *testing.T) {
	h := newHarness(t)
	tokenID, _, _ := h.connect(allScopes...)
	h.db.Exec(`UPDATE api_tokens SET revoked_at=datetime('now') WHERE id=?`, tokenID)
	h.send("/logout")
	h.expect("token is revoked")
}

// TestTokenRevokedMidFlow revokes the token at each step that calls the ERP;
// every one must drop the session and ask to reconnect.
func TestTokenRevokedMidFlow(t *testing.T) {
	fuel := `SELECT code||' '||name FROM accounts WHERE code='5-1001'`
	cash := `SELECT code||' '||name FROM accounts WHERE code='1-1001'`
	steps := map[string]func(h *harness) func(){
		"account list": func(h *harness) func() {
			h.send("/expense")
			h.tap("+ Add expense")
			h.send("10000")
			return func() { h.send("Solar") }
		},
		"cash list": func(h *harness) func() {
			h.send("/expense")
			h.tap("+ Add expense")
			h.send("10000")
			h.send("Solar")
			return func() { h.tap(h.label(fuel)) }
		},
		"vehicle list": func(h *harness) func() {
			h.send("/expense")
			h.tap("+ Add expense")
			h.send("10000")
			h.send("Solar")
			h.tap(h.label(fuel))
			return func() { h.tap(h.label(cash)) }
		},
		"save": func(h *harness) func() {
			h.startIncome("10000")
			return func() { h.tap("Save") }
		},
		"view entry": func(h *harness) func() {
			h.startIncome("10000")
			h.tap("Save")
			h.tap("« Back")
			return func() { h.tap(time.Now().In(wib).Format("2 Jan")) }
		},
		"invoice list": func(h *harness) func() {
			h.send("/invoices")
			return func() { h.tap("Draft") }
		},
	}
	for name, setup := range steps {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			tokenID, _, _ := h.connect(allScopes...)
			act := setup(h)
			h.db.Exec(`UPDATE api_tokens SET revoked_at=datetime('now') WHERE id=?`, tokenID)
			act()
			h.expect("revoked or expired")
			if h.b.sessions[userID] != nil {
				t.Error("session kept after token was revoked")
			}
		})
	}
}

func TestEntryDeletedElsewhere(t *testing.T) {
	for _, button := range []string{"Edit", "Delete", "« Back"} {
		t.Run(button, func(t *testing.T) {
			h := newHarness(t)
			h.connect(allScopes...)
			h.startIncome("10000")
			h.tap("Save")
			h.expect("Saved.")
			if button == "« Back" {
				h.tap("« Back") // list is on screen; the entry goes before it's opened
			}
			id := h.int(`SELECT id FROM journal_entries WHERE source_type='income'`)
			if _, err := journal.New(h.db).DeleteIncome(t.Context(), journal.Actor{UserID: 1, CanManageIncome: true}, id); err != nil {
				t.Fatal(err)
			}
			if button == "« Back" {
				h.tap(time.Now().In(wib).Format("2 Jan"))
			} else {
				h.tap(button)
			}
			h.expect("not found")
			if !h.hasButton("« Menu") {
				t.Error("error screen has no way back")
			}
		})
	}
}

// newInvoice creates a draft invoice for a fresh customer.
func (h *harness) newInvoice(lines int) int {
	h.t.Helper()
	res, err := h.db.Exec(`INSERT INTO contacts (name, contact_type, is_active) VALUES ('Sinta', 'customer', 1)`)
	if err != nil {
		h.t.Fatal(err)
	}
	contactID, _ := res.LastInsertId()
	var ls []model.InvoiceLine
	for i := range lines {
		ls = append(ls, model.InvoiceLine{Description: fmt.Sprintf("Trip %d", i+1), Quantity: 100, UnitPrice: 10000,
			AccountID: h.int(`SELECT id FROM accounts WHERE code='4-1001'`)})
	}
	id, err := testutil.CreateInvoice(h.db, &model.Invoice{ContactID: int(contactID), InvoiceDate: "2026-09-01", DueDate: "2026-09-10"}, ls)
	if err != nil {
		h.t.Fatal(err)
	}
	return id
}

func TestInvoiceEdgeCases(t *testing.T) {
	t.Run("empty and paginated lists", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		h.send("/invoices")
		h.tap("Paid")
		h.expect("No paid invoices.")
		for range pageSize + 1 {
			h.newInvoice(1)
		}
		h.tap("« Statuses")
		h.tap("All")
		h.expect("All invoices (page 1 of 2)")
		h.tap("Next ›")
		h.expect("page 2 of 2")
	})

	t.Run("long invoice is truncated", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		id := h.newInvoice(25)
		h.send("/invoices")
		h.press(h.b.chats[userID].nonce + "|iv|" + fmt.Sprint(id)) // no list to go back to
		h.expect("…and 5 more lines")
		h.tap("« Back")
		h.expect("All invoices")
	})

	t.Run("sent elsewhere before confirming", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		id := h.newInvoice(1)
		h.send("/invoices")
		h.tap("Draft")
		h.tap("INV")
		h.tap("Mark as sent")
		if _, err := invoicemod.New(h.db).Send(t.Context(), invoicemod.Actor{UserID: 1, CanManage: true}, id); err != nil {
			t.Fatal(err)
		}
		h.tap("Yes, mark as sent")
		h.expect("ERP says")
	})

	t.Run("paid elsewhere before paying", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		id := h.newInvoice(1)
		h.send("/invoices")
		h.tap("Draft")
		h.tap("INV")
		h.tap("Mark as sent")
		h.tap("Yes, mark as sent")
		h.tap("Record payment")
		h.tap("Full")
		h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`))
		h.tap("Today")
		h.db.Exec(`UPDATE invoices SET status='paid', amount_paid=total WHERE id=?`, id)
		h.tap("Save")
		h.expect("ERP says")
		if n := h.int(`SELECT COUNT(*) FROM payments WHERE payment_type='invoice' AND reference_id=?`, id); n != 0 {
			t.Errorf("payments recorded: %d, want 0", n)
		}
		// The stale "Record payment" button re-checks the invoice first.
		h.send("/invoices")
		h.tap("Paid")
		h.tap("INV")
		h.press(h.b.chats[userID].nonce + "|ip|" + fmt.Sprint(id) + ":paid:1")
		h.expect("can't take a payment")
	})

	t.Run("recorded payment is shown even if the ERP then fails", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		id := h.newInvoice(1)
		h.send("/invoices")
		h.tap("Draft")
		h.tap("INV")
		h.tap("Mark as sent")
		next := h.b.apiHTTP.Transport
		h.b.apiHTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/invoices/") {
				return nil, errors.New("ERP restarting")
			}
			return next.RoundTrip(r)
		})
		h.tap("Yes, mark as sent")
		h.expect("Marked as sent.")
		h.b.apiHTTP.Transport = next
		h.tap("Record payment")
		h.send("4.000")
		h.tap(h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`))
		h.tap("Today")
		h.b.apiHTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/invoices/") {
				return nil, errors.New("ERP restarting")
			}
			return next.RoundTrip(r)
		})
		h.tap("Save")
		h.expect("Payment recorded.")
		h.expect("Amount due: Rp 6.000")
		if inv, _ := testutil.GetInvoice(h.db, id); inv.AmountPaid != 4000 {
			t.Errorf("amount_paid %d, want 4000", inv.AmountPaid)
		}
	})

	t.Run("rejected payment refreshes the amount due", func(t *testing.T) {
		h := newHarness(t)
		h.connect(allScopes...)
		id := h.newInvoice(1)
		if _, err := invoicemod.New(h.db).Send(t.Context(), invoicemod.Actor{UserID: 1, CanManage: true}, id); err != nil {
			t.Fatal(err)
		}
		cash := h.label(`SELECT code||' '||name FROM accounts WHERE code='1-1001'`)
		h.send("/invoices")
		h.tap("Sent")
		h.tap("INV")
		h.tap("Record payment")
		h.tap("Full Rp 10.000")
		h.tap(cash)
		h.tap("Today")
		// 6.000 is recorded on the web while the form is open.
		if _, err := invoicemod.New(h.db).RecordPayment(t.Context(), invoicemod.Actor{UserID: 1, CanManage: true}, id,
			invoicemod.Payment{Amount: 6000, Date: "2026-09-02", AccountID: h.int(`SELECT id FROM accounts WHERE code='1-1001'`)}); err != nil {
			t.Fatal(err)
		}
		h.tap("Save")
		h.expect("exceeds remaining balance")
		h.tap("Amount")
		h.expect("Amount due: Rp 4.000")
		h.tap("Full Rp 4.000")
		h.tap("Save")
		h.expect("Payment recorded.")
		h.expect("Status: paid")
	})

	t.Run("deleted elsewhere", func(t *testing.T) {
		for _, button := range []string{"Mark as sent", "INV"} {
			h := newHarness(t)
			h.connect(allScopes...)
			id := h.newInvoice(1)
			h.send("/invoices")
			h.tap("Draft")
			if button == "Mark as sent" {
				h.tap("INV")
			}
			h.db.Exec(`DELETE FROM invoice_lines WHERE invoice_id=?`, id)
			h.db.Exec(`DELETE FROM invoices WHERE id=?`, id)
			h.tap(button)
			h.expect("not found")
		}
	})
}

func TestTelegramFailures(t *testing.T) {
	h := newHarness(t)
	h.connect(allScopes...)
	before := len(h.callsTo("sendMessage"))

	// "Not modified" is success: no duplicate message.
	h.tgReply = map[string]string{"editMessageText": `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`}
	h.tap("Income")
	if got := len(h.callsTo("sendMessage")); got != before {
		t.Errorf("not-modified edit sent %d new messages", got-before)
	}

	// Any other edit failure falls back to a new message.
	h.tgReply = map[string]string{"editMessageText": `{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`}
	h.tap("« Menu")
	if got := len(h.callsTo("sendMessage")); got != before+1 {
		t.Errorf("failed edit: %d new messages, want 1", got-before)
	}

	// Failures of fire-and-forget calls are logged, never fatal.
	h.tgReply = map[string]string{
		"sendMessage":         `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`,
		"answerCallbackQuery": `{"ok":false,"error_code":400,"description":"query is too old"}`,
		"deleteMessage":       `{"ok":false,"error_code":400,"description":"message can't be deleted"}`,
		"getUpdates":          `not json`,
	}
	h.send("/menu")
	h.tap("Income")
	_, plaintext, _ := h.connect(allScopes...)
	if s := h.b.sessions[userID]; s == nil || s.Token != plaintext {
		t.Error("connect must still succeed when the token message can't be deleted")
	}

	// A broken getUpdates response is retried with backoff until shutdown.
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	if err := h.b.run(ctx); err != nil {
		t.Errorf("run: got %v, want clean exit on shutdown", err)
	}
	if n := len(h.callsTo("getUpdates")); n != 2 {
		t.Errorf("getUpdates calls: got %d, want 2 (first try + one retry after 1s)", n)
	}
}

func TestTransportErrorsHideBotToken(t *testing.T) {
	b := newBot("http://127.0.0.1:1/bot123:SECRET", "", "")
	err := b.tg("getMe", nil, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("tg error %v: want a failure without the bot token", err)
	}
}

func TestSessionFile(t *testing.T) {
	dir := t.TempDir()
	b := newBot("", "", filepath.Join(dir, "missing.json"))
	if err := b.loadSessions(); err != nil || len(b.sessions) != 0 {
		t.Errorf("missing state file: err=%v sessions=%v, want a clean start", err, b.sessions)
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	os.WriteFile(corrupt, []byte("{"), 0o600)
	if err := newBot("", "", corrupt).loadSessions(); err == nil {
		t.Error("corrupt state file loaded without error")
	}

	b = newBot("", "", filepath.Join(dir, "no-such-dir", "sessions.json"))
	b.sessions[1] = &session{Token: "lat_x"}
	if err := b.writeSessions(); err == nil {
		t.Error("write into a missing directory succeeded")
	}
	b.saveSessions() // logs, doesn't panic
}

func TestHelpers(t *testing.T) {
	t.Setenv("BOT_TEST_ENV", "set")
	if envOr("BOT_TEST_ENV", "x") != "set" || envOr("BOT_TEST_UNSET", "x") != "x" {
		t.Error("envOr")
	}
	for err, want := range map[error]string{
		&apiErr{Status: 502}:                    "ERP error (502)",
		&apiErr{Status: 409}:                    "ERP error (409)",
		&apiErr{Status: 409, Message: "locked"}: "ERP says: locked",
	} {
		if got := errText(err); !strings.Contains(got, want) {
			t.Errorf("errText(%v) = %q, want %q", err, got, want)
		}
	}
	if (&tgError{Code: 400, Description: "bad"}).Error() != "telegram error 400: bad" {
		t.Error("tgError.Error")
	}
	if fmtDate("soon") != "soon" || shortDate("soon") != "soon" {
		t.Error("unparseable dates should pass through")
	}
	if got := clipRunes("ééééé", 3); got != "éé…" {
		t.Errorf("clipRunes = %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("btn should panic on callback_data over 64 bytes")
		}
	}()
	(&chat{nonce: "abcdef"}).btn("x", "ls", strings.Repeat("9", 60))
}

// roundTripFunc injects faults between the bot and the ERP.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
