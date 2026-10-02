package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var commands = []map[string]string{
	{"command": "menu", "description": "Main menu"},
	{"command": "income", "description": "Income entries"},
	{"command": "expense", "description": "Expense entries"},
	{"command": "invoices", "description": "Invoices"},
	{"command": "cancel", "description": "Cancel the current form"},
	{"command": "logout", "description": "Disconnect and revoke your token"},
	{"command": "start", "description": "Help and connection status"},
}

// requiredScopes are what the bot's write actions need from a token.
var requiredScopes = []string{"income.manage", "expenses.manage", "invoices.manage"}

// tokenRe finds an ERP API token anywhere in a message, even glued to other
// text ("token_lat_…"), so it is deleted rather than saved as a description.
var tokenRe = regexp.MustCompile(`lat_[0-9A-Za-z]{32}`)

const connectHelp = `To connect, create a personal API token in Latasya ERP:
1. Open ERP → API Tokens (sidebar) → Create new token
2. Tick income.manage, expenses.manage and invoices.manage
3. Leave the expiry empty
4. Paste the token here

I delete your token message right away. Revoke the token in ERP any time to disconnect this bot.`

const expired = "This menu expired. Send /menu."

// wib is Latasya's business timezone. "Today" follows local midnight, not the
// server clock (UTC on the VPS).
// ponytail: fixed UTC+7; make configurable if a branch opens outside WIB.
var wib = time.FixedZone("WIB", 7*60*60)

const pageSize = 10

// turn is one incoming update being handled.
type turn struct {
	chatID int64
	userID int64
	msgID  int // message to edit in place (callbacks); 0 sends a new message
}

// chat is a user's in-memory conversation state. Every button embeds the
// current nonce, which rotates on navigation, when a form opens, and after
// each change, so stale buttons (double taps, old forms, messages from before
// a restart) are rejected instead of acting twice.
type chat struct {
	nonce        string
	flow         string // "i" income, "e" expense, "p" invoice payment; "" = no form open
	step         string // typed input awaited: "amount", "desc", "date"
	editID       int    // entry being edited; 0 for a new entry
	invoice      *invoice
	back         string // invoice list to return to, "status:page"
	d, orig      draft
	vehicleAsked bool
	pickAction   string         // action of the picker currently on screen
	options      map[int]string // labels of that picker's buttons, by ID
	idemKey      string
}

type draft struct {
	Amount  int
	Desc    string
	Date    string
	Acct    ref // revenue or expense account
	Cash    ref // deposit, paid-from, or payment account
	Vehicle ref // expenses only; ID 0 means no vehicle
}

// same reports whether two drafts describe the same stored values.
func (d draft) same(o draft) bool {
	return d.Amount == o.Amount && d.Desc == o.Desc && d.Date == o.Date &&
		d.Acct.ID == o.Acct.ID && d.Cash.ID == o.Cash.ID && d.Vehicle.ID == o.Vehicle.ID
}

type ref struct {
	ID    int
	Label string
}

func (b *bot) chat(userID int64) *chat {
	c := b.chats[userID]
	if c == nil {
		c = &chat{nonce: newNonce()}
		b.chats[userID] = c
	}
	return c
}

// reset closes any open form and invalidates every button already sent.
func (c *chat) reset() { *c = chat{nonce: newNonce()} }

func (c *chat) btn(text, action, arg string) button {
	data := c.nonce + "|" + action + "|" + arg
	if len(data) > 64 {
		panic("telegram callback_data over 64 bytes: " + data)
	}
	return button{Text: text, CallbackData: data}
}

func (c *chat) complete() bool {
	if c.step != "" {
		return false
	}
	if c.flow == "p" {
		return c.d.Amount > 0 && c.d.Cash.ID > 0 && c.d.Date != ""
	}
	k := kinds[c.flow]
	return c.d.Amount > 0 && c.d.Desc != "" && c.d.Acct.ID > 0 && c.d.Cash.ID > 0 &&
		c.d.Date != "" && (!k.vehicle || c.vehicleAsked)
}

func newNonce() string {
	buf := make([]byte, 3)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// --- Dispatch ---

func (b *bot) handle(u update) {
	if q := u.CallbackQuery; q != nil {
		toast := ""
		if q.Message != nil && q.Message.Chat.Type == "private" && !q.From.IsBot {
			toast = b.onCallback(turn{chatID: q.Message.Chat.ID, userID: q.From.ID, msgID: q.Message.MessageID}, q.Data)
		}
		b.answer(q.ID, toast)
		return
	}
	// Private chats only: accounting data must not land in a group.
	if m := u.Message; m != nil && m.Chat.Type == "private" && m.From != nil && !m.From.IsBot {
		b.onMessage(turn{chatID: m.Chat.ID, userID: m.From.ID}, m)
	}
}

func (b *bot) onMessage(t turn, m *message) {
	text := strings.TrimSpace(m.Text)
	if token := tokenRe.FindString(text); token != "" {
		b.connect(t, m.MessageID, token)
		return
	}
	s := b.sessions[t.userID]
	if s == nil {
		b.say(t.chatID, connectHelp, nil)
		return
	}
	c := b.chat(t.userID)
	cmd := ""
	if strings.HasPrefix(text, "/") {
		cmd, _, _ = strings.Cut(text, " ")
		cmd, _, _ = strings.Cut(cmd, "@") // "/menu@LatasyaBot" in group-style clients
	}
	switch cmd {
	case "":
		if c.step == "" {
			b.say(t.chatID, "Send /menu to see what I can do.", nil)
			return
		}
		b.onInput(t, s, c, text)
	case "/start", "/help", "/login", "/menu":
		c.reset()
		header := ""
		if cmd != "/menu" {
			header = fmt.Sprintf("Connected as %s (%s). Paste a new token to switch, or /logout to disconnect.", s.FullName, s.Username)
		}
		text, kb := menu(c, header)
		b.say(t.chatID, text, kb)
	case "/income":
		c.reset()
		b.listEntries(t, s, c, kinds["i"], 1)
	case "/expense":
		c.reset()
		b.listEntries(t, s, c, kinds["e"], 1)
	case "/invoices":
		c.reset()
		b.invoiceMenu(t, c)
	case "/cancel":
		c.reset()
		text, kb := menu(c, "Cancelled.")
		b.say(t.chatID, text, kb)
	case "/logout":
		b.logout(t, s)
	default:
		b.say(t.chatID, "Unknown command. Send /menu.", nil)
	}
}

func (b *bot) onCallback(t turn, data string) string {
	s := b.sessions[t.userID]
	if s == nil {
		b.show(t, connectHelp, nil)
		return ""
	}
	c := b.chat(t.userID)
	nonce, rest, _ := strings.Cut(data, "|")
	action, arg, _ := strings.Cut(rest, "|")
	if nonce != c.nonce {
		return expired
	}
	args := strings.Split(arg, ":")
	num := func(i int) int {
		if i >= len(args) {
			return 0
		}
		n, _ := strconv.Atoi(args[i])
		return n
	}
	k := kinds[args[0]]

	switch action {
	case "mn":
		c.reset()
		text, kb := menu(c, "")
		b.show(t, text, kb)
	case "cx":
		c.reset()
		text, kb := menu(c, "Cancelled.")
		b.show(t, text, kb)
	case "ls", "vw", "add", "ed", "dl", "dly":
		if k == nil {
			return expired
		}
		switch action {
		case "ls":
			c.reset()
			b.listEntries(t, s, c, k, max(num(1), 1))
		case "vw":
			c.reset()
			b.viewEntry(t, s, c, k, num(1), num(2), "")
		case "add":
			b.startAdd(t, s, c, k)
		case "ed":
			b.startEdit(t, s, c, k, num(1))
		case "dl":
			c.reset()
			b.confirmDelete(t, s, c, k, num(1), num(2))
		case "dly":
			b.doDelete(t, s, c, k, num(1), num(2))
		}
	case "ac", "ca", "vh":
		return b.pick(t, s, c, action, num(0))
	case "dt":
		if c.flow == "" || c.step != "date" {
			return expired
		}
		c.d.Date = time.Now().In(wib).AddDate(0, 0, -num(0)).Format("2006-01-02")
		b.next(t, s, c)
	case "pf":
		if c.flow != "p" || c.step != "amount" {
			return expired
		}
		c.d.Amount = c.invoice.due()
		b.next(t, s, c)
	case "fx":
		return b.editField(t, s, c, arg)
	case "sv":
		return b.save(t, s, c)
	case "im":
		c.reset()
		b.invoiceMenu(t, c)
	case "il":
		c.reset()
		b.listInvoices(t, s, c, args[0], max(num(1), 1))
	case "iv", "is", "isy", "ip":
		// arg is "id:status:page"; status:page is the list to go back to.
		id, back := num(0), strings.Join(args[min(1, len(args)):], ":")
		switch action {
		case "iv":
			c.reset()
			b.viewInvoice(t, s, c, id, back, "")
		case "is":
			c.reset()
			b.confirmSend(t, s, c, id, back)
		case "isy":
			b.doSend(t, s, c, id, back)
		case "ip":
			b.startPayment(t, s, c, id, back)
		}
	default:
		return expired
	}
	return ""
}

func menu(c *chat, header string) (string, keyboard) {
	text := "What do you want to do?"
	if header != "" {
		text = header + "\n\n" + text
	}
	return text, keyboard{
		{c.btn("Income", "ls", "i:1"), c.btn("Expense", "ls", "e:1")},
		{c.btn("Invoices", "im", "")},
	}
}

func menuKB(c *chat) keyboard { return keyboard{{c.btn("« Menu", "mn", "")}} }

func cancelKB(c *chat) keyboard { return keyboard{{c.btn("Cancel", "cx", "")}} }

// --- Connect / disconnect ---

func (b *bot) connect(t turn, messageID int, token string) {
	// The message holds a live credential: remove it before anything else.
	b.deleteMessage(t.chatID, messageID)

	s := &session{Token: token}
	var me struct {
		Data struct {
			Username    string   `json:"username"`
			FullName    string   `json:"full_name"`
			TokenScopes []string `json:"token_scopes"`
		} `json:"data"`
	}
	if err := b.api(s, http.MethodGet, "/api/v1/auth/me", nil, "", &me); err != nil {
		var ae *apiErr
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
			b.say(t.chatID, "That token is invalid, expired, or revoked.\n\n"+connectHelp, nil)
			return
		}
		b.say(t.chatID, errText(err), nil)
		return
	}
	if old := b.sessions[t.userID]; old != nil && old.Token != token {
		// The old token is being replaced; don't leave it live. Best effort.
		if err := b.api(old, http.MethodDelete, "/api/v1/auth/token", nil, "", nil); err != nil {
			slog.Warn("failed to revoke replaced token", "error", err)
		}
	}
	s.Username, s.FullName = me.Data.Username, me.Data.FullName
	b.sessions[t.userID] = s
	b.saveSessions()

	header := fmt.Sprintf("Connected as %s (%s).", s.FullName, s.Username)
	var missing []string
	for _, scope := range requiredScopes {
		if !slices.Contains(me.Data.TokenScopes, scope) {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		header += "\n\nHeads-up: this token lacks " + strings.Join(missing, ", ") +
			", so those actions will fail. Create a new token with all three scopes to fix it."
	}
	c := b.chat(t.userID)
	c.reset()
	text, kb := menu(c, header)
	b.say(t.chatID, text, kb)
}

func (b *bot) logout(t turn, s *session) {
	text := "Disconnected and your token is revoked. Paste a new token any time to reconnect."
	if err := b.api(s, http.MethodDelete, "/api/v1/auth/token", nil, "", nil); err != nil {
		var ae *apiErr
		if !errors.As(err, &ae) || ae.Status != http.StatusUnauthorized {
			text = "Disconnected, but I couldn't reach ERP to revoke the token. Revoke it in ERP → API Tokens."
		}
	}
	b.forget(t.userID)
	b.say(t.chatID, text, nil)
}

func (b *bot) forget(userID int64) {
	delete(b.sessions, userID)
	delete(b.chats, userID)
	b.saveSessions()
}

// fail reports an API error. A 401 means the token was revoked or expired, so
// the session is dropped and the user is asked to reconnect.
func (b *bot) fail(t turn, c *chat, err error) {
	var ae *apiErr
	if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
		b.forget(t.userID)
		b.show(t, "Your token was revoked or expired.\n\n"+connectHelp, nil)
		return
	}
	slog.Warn("erp request failed", "error", err)
	b.show(t, errText(err), menuKB(c))
}

func errText(err error) string {
	var ae *apiErr
	if !errors.As(err, &ae) {
		return "ERP is unreachable right now. Try again in a moment."
	}
	if len(ae.Fields) > 0 {
		keys := make([]string, 0, len(ae.Fields))
		for key := range ae.Fields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, strings.ReplaceAll(key, "_", " ")+" — "+ae.Fields[key])
		}
		return "Please fix: " + strings.Join(parts, "; ")
	}
	if ae.Status >= 500 || ae.Message == "" {
		return fmt.Sprintf("ERP error (%d). Try again later.", ae.Status)
	}
	return "ERP says: " + ae.Message
}

// --- Income and expenses ---

// kind describes income or expense; both are the same two-account shape, so
// they share one code path.
type kind struct {
	key, name, path                string
	acctField, acctType, acctLabel string
	cashField, cashLabel           string
	vehicle                        bool
}

var kinds = map[string]*kind{
	"i": {key: "i", name: "Income", path: "/api/v1/income",
		acctField: "revenue_account", acctType: "revenue", acctLabel: "Revenue account",
		cashField: "deposit_account", cashLabel: "Deposit to"},
	"e": {key: "e", name: "Expense", path: "/api/v1/expenses",
		acctField: "expense_account", acctType: "expense", acctLabel: "Expense account",
		cashField: "payment_account", cashLabel: "Paid from", vehicle: true},
}

// entry decodes both income and expense responses.
type entry struct {
	ID          int      `json:"id"`
	Reference   string   `json:"reference"`
	EntryDate   string   `json:"entry_date"`
	Description string   `json:"description"`
	Amount      string   `json:"amount"`
	Revenue     *nameRef `json:"revenue_account"`
	Deposit     *nameRef `json:"deposit_account"`
	Expense     *nameRef `json:"expense_account"`
	Payment     *nameRef `json:"payment_account"`
	Vehicle     *nameRef `json:"vehicle"`
}

type nameRef struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

func (r *nameRef) ref() ref {
	if r == nil {
		return ref{}
	}
	return ref{ID: r.ID, Label: strings.TrimSpace(r.Code + " " + r.Name)}
}

func (e entry) draft() draft {
	amount, _ := strconv.Atoi(e.Amount)
	d := draft{Amount: amount, Desc: e.Description, Date: e.EntryDate, Vehicle: e.Vehicle.ref()}
	if e.Revenue != nil {
		d.Acct, d.Cash = e.Revenue.ref(), e.Deposit.ref()
	} else {
		d.Acct, d.Cash = e.Expense.ref(), e.Payment.ref()
	}
	return d
}

func describe(k *kind, d draft) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Date: %s\nAmount: %s\n%s: %s\n%s: %s\n",
		fmtDate(d.Date), idr(d.Amount), k.acctLabel, d.Acct.Label, k.cashLabel, d.Cash.Label)
	if k.vehicle {
		vehicle := d.Vehicle.Label
		if d.Vehicle.ID == 0 {
			vehicle = "No vehicle"
		}
		fmt.Fprintf(&sb, "Vehicle: %s\n", vehicle)
	}
	fmt.Fprintf(&sb, "Description: %s", d.Desc)
	return sb.String()
}

func (b *bot) listEntries(t turn, s *session, c *chat, k *kind, page int) {
	var res struct {
		Data []entry `json:"data"`
		Meta struct {
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"meta"`
	}
	if err := b.api(s, http.MethodGet, fmt.Sprintf("%s?page=%d&per_page=%d", k.path, page, pageSize), nil, "", &res); err != nil {
		b.fail(t, c, err)
		return
	}
	if len(res.Data) == 0 && page > res.Meta.TotalPages && res.Meta.TotalPages > 0 {
		// The page emptied (e.g. its last entry was deleted): show the last one.
		b.listEntries(t, s, c, k, res.Meta.TotalPages)
		return
	}
	text := k.name + " — recent entries"
	if res.Meta.Total == 0 {
		text = "No " + strings.ToLower(k.name) + " recorded yet."
	} else if res.Meta.TotalPages > 1 {
		text += fmt.Sprintf(" (page %d of %d)", page, res.Meta.TotalPages)
	}
	kb := keyboard{}
	for _, e := range res.Data {
		amount, _ := strconv.Atoi(e.Amount)
		label := fmt.Sprintf("%s · %s · %s", shortDate(e.EntryDate), idr(amount), oneLine(e.Description))
		kb = append(kb, []button{c.btn(clipRunes(label, 60), "vw", fmt.Sprintf("%s:%d:%d", k.key, e.ID, page))})
	}
	kb = append(kb, pager(c, "ls", k.key+":", page, res.Meta.TotalPages)...)
	kb = append(kb, []button{c.btn("+ Add "+strings.ToLower(k.name), "add", k.key), c.btn("« Menu", "mn", "")})
	b.show(t, text, kb)
}

func pager(c *chat, action, prefix string, page, pages int) keyboard {
	var row []button
	if page > 1 {
		row = append(row, c.btn("‹ Prev", action, fmt.Sprintf("%s%d", prefix, page-1)))
	}
	if page < pages {
		row = append(row, c.btn("Next ›", action, fmt.Sprintf("%s%d", prefix, page+1)))
	}
	if row == nil {
		return nil
	}
	return keyboard{row}
}

func (b *bot) getEntry(s *session, k *kind, id int) (entry, error) {
	var res struct {
		Data entry `json:"data"`
	}
	err := b.api(s, http.MethodGet, fmt.Sprintf("%s/%d", k.path, id), nil, "", &res)
	return res.Data, err
}

func (b *bot) viewEntry(t turn, s *session, c *chat, k *kind, id, page int, header string) {
	e, err := b.getEntry(s, k, id)
	if err != nil {
		b.fail(t, c, err)
		return
	}
	b.showEntry(t, c, k, e, page, header)
}

func (b *bot) showEntry(t turn, c *chat, k *kind, e entry, page int, header string) {
	text := fmt.Sprintf("%s %s\n\n%s", k.name, e.Reference, describe(k, e.draft()))
	if header != "" {
		text = header + "\n\n" + text
	}
	arg := fmt.Sprintf("%s:%d:%d", k.key, e.ID, page)
	b.show(t, text, keyboard{
		{c.btn("Edit", "ed", arg), c.btn("Delete", "dl", arg)},
		{c.btn("« Back", "ls", fmt.Sprintf("%s:%d", k.key, max(page, 1)))},
	})
}

func (b *bot) startAdd(t turn, s *session, c *chat, k *kind) {
	c.reset()
	c.flow, c.idemKey = k.key, rand.Text()
	b.next(t, s, c)
}

func (b *bot) startEdit(t turn, s *session, c *chat, k *kind, id int) {
	e, err := b.getEntry(s, k, id)
	if err != nil {
		b.fail(t, c, err)
		return
	}
	c.reset()
	c.flow, c.editID, c.vehicleAsked = k.key, id, true
	c.d = e.draft()
	c.orig = c.d
	b.review(t, c, "")
}

// next asks for the first missing field, or shows the review once the form is
// complete. Changing a field from the review clears it and calls next, so one
// function covers both "fill in order" and "change one thing".
func (b *bot) next(t turn, s *session, c *chat) {
	c.step, c.pickAction, c.options = "", "", nil
	if c.flow == "p" {
		switch {
		case c.d.Amount == 0:
			c.step = "amount"
			due := c.invoice.due()
			b.show(t, fmt.Sprintf("Payment for %s\nAmount due: %s\n\nTap Full or type the amount received.", c.invoice.Number, idr(due)),
				keyboard{{c.btn("Full "+idr(due), "pf", "")}, {c.btn("Cancel", "cx", "")}})
		case c.d.Cash.ID == 0:
			b.askCash(t, s, c, "Deposit to which account?")
		case c.d.Date == "":
			b.askDate(t, c, "Payment date?")
		default:
			b.review(t, c, "")
		}
		return
	}
	k := kinds[c.flow]
	switch {
	case c.d.Amount == 0:
		c.step = "amount"
		b.show(t, k.name+" amount (Rp)?\nExample: 150000 or 150.000", cancelKB(c))
	case c.d.Desc == "":
		c.step = "desc"
		b.show(t, "Description?", cancelKB(c))
	case c.d.Acct.ID == 0:
		accounts, err := b.accounts(s, k.acctType)
		if err != nil {
			b.fail(t, c, err)
			return
		}
		b.picker(t, c, k.acctLabel+"?", "ac", accounts)
	case c.d.Cash.ID == 0:
		b.askCash(t, s, c, k.cashLabel+"?")
	case k.vehicle && !c.vehicleAsked:
		var res struct {
			Data []nameRef `json:"data"`
		}
		if err := b.api(s, http.MethodGet, "/api/v1/expenses/vehicles", nil, "", &res); err != nil {
			b.fail(t, c, err)
			return
		}
		if len(res.Data) == 0 {
			c.vehicleAsked = true
			b.next(t, s, c)
			return
		}
		options := make([]ref, 0, len(res.Data)+1)
		for _, v := range res.Data {
			options = append(options, v.ref())
		}
		b.picker(t, c, "Vehicle?", "vh", append(options, ref{ID: 0, Label: "No vehicle"}))
	case c.d.Date == "":
		b.askDate(t, c, "Date?")
	default:
		b.review(t, c, "")
	}
}

func (b *bot) onInput(t turn, s *session, c *chat, text string) {
	switch c.step {
	case "amount":
		n, ok := parseAmount(text)
		if !ok {
			b.say(t.chatID, "I couldn't read that amount. Type digits only, like 150000 or 150.000.", cancelKB(c))
			return
		}
		if c.flow == "p" && n > c.invoice.due() {
			b.say(t.chatID, fmt.Sprintf("That's more than the amount due (%s). Type a smaller amount.", idr(c.invoice.due())), cancelKB(c))
			return
		}
		c.d.Amount = n
	case "desc":
		if text == "" {
			b.say(t.chatID, "Please type a description.", cancelKB(c))
			return
		}
		c.d.Desc = text
	case "date":
		d, ok := parseDate(text)
		if !ok {
			b.say(t.chatID, "I couldn't read that date. Type it like 2026-09-30 or 30/9/2026.", cancelKB(c))
			return
		}
		c.d.Date = d
	}
	b.next(t, s, c)
}

type account struct {
	ID       int    `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
	IsCash   bool   `json:"is_cash"`
}

func (b *bot) accounts(s *session, accountType string) ([]ref, error) {
	all, err := b.accountList(s, accountType)
	if err != nil {
		return nil, err
	}
	refs := make([]ref, 0, len(all))
	for _, a := range all {
		refs = append(refs, ref{ID: a.ID, Label: a.Code + " " + a.Name})
	}
	return refs, nil
}

func (b *bot) accountList(s *session, accountType string) ([]account, error) {
	var res struct {
		Data []account `json:"data"`
	}
	if err := b.api(s, http.MethodGet, "/api/v1/accounts?per_page=200&type="+accountType, nil, "", &res); err != nil {
		return nil, err
	}
	active := res.Data[:0]
	for _, a := range res.Data {
		if a.IsActive {
			active = append(active, a)
		}
	}
	return active, nil
}

// askCash offers only cash and bank accounts: depositing into "Accounts
// Receivable" or "Vehicles" is never what staff mean. Falls back to every
// active asset if no account is flagged as cash yet.
func (b *bot) askCash(t turn, s *session, c *chat, prompt string) {
	assets, err := b.accountList(s, "asset")
	if err != nil {
		b.fail(t, c, err)
		return
	}
	var options, all []ref
	for _, a := range assets {
		r := ref{ID: a.ID, Label: a.Code + " " + a.Name}
		all = append(all, r)
		if a.IsCash {
			options = append(options, r)
		}
	}
	if len(options) == 0 {
		options = all
	}
	b.picker(t, c, prompt, "ca", options)
}

func (b *bot) picker(t turn, c *chat, prompt, action string, options []ref) {
	c.pickAction, c.options = action, map[int]string{}
	kb := keyboard{}
	var row []button
	for _, o := range options {
		c.options[o.ID] = o.Label
		row = append(row, c.btn(clipRunes(o.Label, 30), action, strconv.Itoa(o.ID)))
		if len(row) == 2 {
			kb = append(kb, row)
			row = nil
		}
	}
	if row != nil {
		kb = append(kb, row)
	}
	if len(options) == 0 {
		prompt += "\n\nNo active accounts to choose from. Add one in ERP first."
	}
	b.show(t, prompt, append(kb, []button{c.btn("Cancel", "cx", "")}))
}

func (b *bot) pick(t turn, s *session, c *chat, action string, id int) string {
	label, ok := c.options[id]
	if c.flow == "" || action != c.pickAction || !ok {
		return expired
	}
	switch action {
	case "ac":
		c.d.Acct = ref{ID: id, Label: label}
	case "ca":
		c.d.Cash = ref{ID: id, Label: label}
	case "vh":
		c.d.Vehicle, c.vehicleAsked = ref{ID: id, Label: label}, true
	}
	b.next(t, s, c)
	return ""
}

func (b *bot) askDate(t turn, c *chat, prompt string) {
	c.step = "date"
	b.show(t, prompt+"\nTap a button or type a date like 2026-09-30 or 30/9/2026.", keyboard{
		{c.btn("Today", "dt", "0"), c.btn("Yesterday", "dt", "1")},
		{c.btn("Cancel", "cx", "")},
	})
}

func (b *bot) review(t turn, c *chat, header string) {
	c.step, c.pickAction, c.options = "", "", nil
	var text string
	var fields keyboard
	if c.flow == "p" {
		text = fmt.Sprintf("Record payment for %s (%s)\n\nAmount: %s\nDeposit to: %s\nDate: %s",
			c.invoice.Number, c.invoice.Contact, idr(c.d.Amount), c.d.Cash.Label, fmtDate(c.d.Date))
		fields = keyboard{{c.btn("Amount", "fx", "amount"), c.btn("Deposit to", "fx", "cash"), c.btn("Date", "fx", "date")}}
	} else {
		k := kinds[c.flow]
		title := "New " + strings.ToLower(k.name)
		if c.editID != 0 {
			title = "Edit " + strings.ToLower(k.name)
		}
		text = title + " — check and save\n\n" + describe(k, c.d)
		last := []button{c.btn("Date", "fx", "date")}
		if k.vehicle {
			last = append(last, c.btn("Vehicle", "fx", "vehicle"))
		}
		fields = keyboard{
			{c.btn("Amount", "fx", "amount"), c.btn("Description", "fx", "desc")},
			{c.btn(k.acctLabel, "fx", "acct"), c.btn(k.cashLabel, "fx", "cash")},
			last,
		}
	}
	if header != "" {
		text = header + "\n\n" + text
	}
	b.show(t, text, append(keyboard{{c.btn("Save", "sv", ""), c.btn("Cancel", "cx", "")}}, fields...))
}

func (b *bot) editField(t turn, s *session, c *chat, field string) string {
	if c.flow == "" {
		return expired
	}
	switch field {
	case "amount":
		c.d.Amount = 0
	case "desc":
		c.d.Desc = ""
	case "acct":
		c.d.Acct = ref{}
	case "cash":
		c.d.Cash = ref{}
	case "vehicle":
		c.d.Vehicle, c.vehicleAsked = ref{}, false
	case "date":
		c.d.Date = ""
	default:
		return expired
	}
	b.next(t, s, c)
	return ""
}

func (b *bot) save(t turn, s *session, c *chat) string {
	if c.flow == "" || !c.complete() {
		return expired
	}
	if c.flow == "p" {
		b.savePayment(t, s, c)
		return ""
	}
	k := kinds[c.flow]
	body := map[string]any{
		"entry_date": c.d.Date, "description": c.d.Desc, "amount": strconv.Itoa(c.d.Amount),
		k.acctField: c.d.Acct.ID, k.cashField: c.d.Cash.ID,
	}
	if k.vehicle {
		body["vehicle_id"] = c.d.Vehicle.ID
	}
	var res struct {
		Data entry `json:"data"`
	}
	var err error
	if c.editID == 0 {
		err = b.api(s, http.MethodPost, k.path, body, c.idemKey, &res)
	} else {
		// No ETag in the API: re-read and compare so an edit left open for an
		// hour can't silently overwrite a change made on the web meanwhile.
		var current entry
		current, err = b.getEntry(s, k, c.editID)
		if err == nil && current.draft().same(c.d) {
			// A retry after a lost reply: the first PUT already landed.
			c.reset()
			b.showEntry(t, c, k, current, 1, "Saved.")
			return ""
		}
		if err == nil && !current.draft().same(c.orig) {
			c.reset()
			b.showEntry(t, c, k, current, 1, "Not saved: this entry was changed elsewhere after you opened it. Here is the latest version — tap Edit to try again.")
			return ""
		}
		if err == nil {
			err = b.api(s, http.MethodPut, fmt.Sprintf("%s/%d", k.path, c.editID), body, "", &res)
		}
	}
	if err != nil {
		b.saveFailed(t, c, err)
		return ""
	}
	c.reset()
	b.showEntry(t, c, k, res.Data, 1, "Saved.")
	return ""
}

// saveFailed keeps the form open when retrying is safe — validation errors, or
// network errors where the idempotency key makes a retried POST replay rather
// than duplicate — and closes it otherwise.
func (b *bot) saveFailed(t turn, c *chat, err error) {
	var ae *apiErr
	if !errors.As(err, &ae) {
		b.review(t, c, errText(err)+" Tap Save to retry.")
		return
	}
	switch {
	case ae.Status == http.StatusUnauthorized:
		b.fail(t, c, err)
	case ae.Code == "idempotency_conflict":
		c.reset()
		b.show(t, "This may already be saved. Check the recent list before trying again.", menuKB(c))
	case ae.Status == http.StatusUnprocessableEntity:
		b.review(t, c, errText(err))
	default:
		c.reset()
		b.fail(t, c, err)
	}
}

func (b *bot) confirmDelete(t turn, s *session, c *chat, k *kind, id, page int) {
	e, err := b.getEntry(s, k, id)
	if err != nil {
		b.fail(t, c, err)
		return
	}
	arg := fmt.Sprintf("%s:%d:%d", k.key, id, page)
	b.show(t, fmt.Sprintf("Delete this %s? This cannot be undone.\n\n%s %s\n%s",
		strings.ToLower(k.name), k.name, e.Reference, describe(k, e.draft())),
		keyboard{{c.btn("Yes, delete", "dly", arg), c.btn("No", "vw", arg)}})
}

func (b *bot) doDelete(t turn, s *session, c *chat, k *kind, id, page int) {
	err := b.api(s, http.MethodDelete, fmt.Sprintf("%s/%d", k.path, id), nil, "", nil)
	var ae *apiErr
	gone := errors.As(err, &ae) && ae.Status == http.StatusNotFound
	if err != nil && !gone {
		b.fail(t, c, err)
		return
	}
	c.reset()
	header := "Deleted."
	if gone {
		header = "Already deleted."
	}
	b.show(t, header, keyboard{{c.btn("« Back to list", "ls", fmt.Sprintf("%s:%d", k.key, max(page, 1))), c.btn("« Menu", "mn", "")}})
}

// --- Invoices ---

type invoice struct {
	ID             int    `json:"id"`
	Number         string `json:"invoice_number"`
	Contact        string `json:"contact_name"`
	InvoiceDate    string `json:"invoice_date"`
	DueDate        string `json:"due_date"`
	Status         string `json:"status"`
	Subtotal       string `json:"subtotal"`
	TaxAmount      string `json:"tax_amount"`
	Total          string `json:"total"`
	AmountPaid     string `json:"amount_paid"`
	AmountCredited string `json:"amount_credited"`
	AmountDue      string `json:"amount_due"`
	Lines          []struct {
		Description string `json:"description"`
		Quantity    string `json:"quantity"`
		UnitPrice   string `json:"unit_price"`
		Amount      string `json:"amount"`
	} `json:"lines"`
}

func (inv *invoice) due() int {
	n, _ := strconv.Atoi(inv.AmountDue)
	return n
}

func payable(status string) bool {
	return status == "sent" || status == "partial" || status == "overdue"
}

func (b *bot) invoiceMenu(t turn, c *chat) {
	b.show(t, "Invoices — pick a status.", keyboard{
		{c.btn("Draft", "il", "draft:1"), c.btn("Sent", "il", "sent:1")},
		{c.btn("Partial", "il", "partial:1"), c.btn("Paid", "il", "paid:1")},
		{c.btn("All", "il", ":1"), c.btn("« Menu", "mn", "")},
	})
}

func (b *bot) listInvoices(t turn, s *session, c *chat, status string, page int) {
	var res struct {
		Data []invoice `json:"data"`
		Meta struct {
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"meta"`
	}
	path := fmt.Sprintf("/api/v1/invoices?status=%s&page=%d&per_page=%d", url.QueryEscape(status), page, pageSize)
	if err := b.api(s, http.MethodGet, path, nil, "", &res); err != nil {
		b.fail(t, c, err)
		return
	}
	if len(res.Data) == 0 && page > res.Meta.TotalPages && res.Meta.TotalPages > 0 {
		// The page emptied (e.g. its last invoice was paid): show the last one.
		b.listInvoices(t, s, c, status, res.Meta.TotalPages)
		return
	}
	label := "All"
	if status != "" {
		label = strings.ToUpper(status[:1]) + status[1:]
	}
	text := label + " invoices"
	if res.Meta.Total == 0 {
		text = "No " + strings.ToLower(label) + " invoices."
	} else if res.Meta.TotalPages > 1 {
		text += fmt.Sprintf(" (page %d of %d)", page, res.Meta.TotalPages)
	}
	kb := keyboard{}
	for _, inv := range res.Data {
		line := fmt.Sprintf("%s · %s · %s", inv.Number, oneLine(inv.Contact), money(inv.Total))
		kb = append(kb, []button{c.btn(clipRunes(line, 60), "iv", fmt.Sprintf("%d:%s:%d", inv.ID, status, page))})
	}
	kb = append(kb, pager(c, "il", status+":", page, res.Meta.TotalPages)...)
	kb = append(kb, []button{c.btn("« Statuses", "im", ""), c.btn("« Menu", "mn", "")})
	b.show(t, text, kb)
}

func (b *bot) getInvoice(s *session, id int) (*invoice, error) {
	var res struct {
		Data invoice `json:"data"`
	}
	if err := b.api(s, http.MethodGet, fmt.Sprintf("/api/v1/invoices/%d", id), nil, "", &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

func (b *bot) viewInvoice(t turn, s *session, c *chat, id int, back, header string) {
	inv, err := b.getInvoice(s, id)
	if err != nil {
		b.fail(t, c, err)
		return
	}
	b.showInvoice(t, c, inv, back, header)
}

// showInvoice renders an invoice already in hand. After a send or payment it
// renders the POST response: a follow-up GET could fail and hide a change that
// already happened, inviting the user to pay twice.
func (b *bot) showInvoice(t turn, c *chat, inv *invoice, back, header string) {
	var sb strings.Builder
	if header != "" {
		sb.WriteString(header + "\n\n")
	}
	fmt.Fprintf(&sb, "Invoice %s\nCustomer: %s\nStatus: %s\nDate: %s · Due: %s\n",
		inv.Number, inv.Contact, inv.Status, fmtDate(inv.InvoiceDate), fmtDate(inv.DueDate))
	if len(inv.Lines) > 0 {
		sb.WriteString("\n")
	}
	for i, l := range inv.Lines {
		if i == 20 {
			fmt.Fprintf(&sb, "…and %d more lines\n", len(inv.Lines)-20)
			break
		}
		fmt.Fprintf(&sb, "• %s — %s × %s = %s\n", oneLine(l.Description), strings.TrimSuffix(l.Quantity, ".00"), money(l.UnitPrice), money(l.Amount))
	}
	optional := func(label, amount string) {
		if amount != "" && amount != "0" {
			fmt.Fprintf(&sb, "%s: %s\n", label, money(amount))
		}
	}
	fmt.Fprintf(&sb, "\nSubtotal: %s\n", money(inv.Subtotal))
	optional("Tax", inv.TaxAmount)
	fmt.Fprintf(&sb, "Total: %s\n", money(inv.Total))
	optional("Paid", inv.AmountPaid)
	optional("Credited", inv.AmountCredited)
	fmt.Fprintf(&sb, "Amount due: %s", money(inv.AmountDue))

	arg := fmt.Sprintf("%d:%s", inv.ID, back)
	kb := keyboard{}
	if inv.Status == "draft" {
		kb = append(kb, []button{c.btn("Mark as sent", "is", arg)})
	}
	if payable(inv.Status) {
		kb = append(kb, []button{c.btn("Record payment", "ip", arg)})
	}
	backStatus, backPage, _ := strings.Cut(back, ":")
	if backPage == "" {
		backPage = "1"
	}
	kb = append(kb, []button{c.btn("« Back", "il", backStatus+":"+backPage), c.btn("« Menu", "mn", "")})
	b.show(t, sb.String(), kb)
}

func (b *bot) confirmSend(t turn, s *session, c *chat, id int, back string) {
	inv, err := b.getInvoice(s, id)
	if err != nil {
		b.fail(t, c, err)
		return
	}
	arg := fmt.Sprintf("%d:%s", id, back)
	b.show(t, fmt.Sprintf("Mark %s (%s, %s) as sent?\n\nThis posts the receivable journal entry, and the invoice can no longer be edited.",
		inv.Number, inv.Contact, money(inv.Total)),
		keyboard{{c.btn("Yes, mark as sent", "isy", arg), c.btn("No", "iv", arg)}})
}

func (b *bot) doSend(t turn, s *session, c *chat, id int, back string) {
	key := fmt.Sprintf("send-%s-%d", c.nonce, id)
	var res struct {
		Data invoice `json:"data"`
	}
	if err := b.api(s, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), nil, key, &res); err != nil {
		b.fail(t, c, err)
		return
	}
	c.reset()
	b.showInvoice(t, c, &res.Data, back, "Marked as sent.")
}

func (b *bot) startPayment(t turn, s *session, c *chat, id int, back string) {
	inv, err := b.getInvoice(s, id)
	if err != nil {
		b.fail(t, c, err)
		return
	}
	c.reset()
	if !payable(inv.Status) || inv.due() <= 0 {
		b.viewInvoice(t, s, c, id, back, "This invoice can't take a payment right now.")
		return
	}
	c.flow, c.invoice, c.back, c.idemKey = "p", inv, back, rand.Text()
	b.next(t, s, c)
}

func (b *bot) savePayment(t turn, s *session, c *chat) {
	body := map[string]any{"amount": strconv.Itoa(c.d.Amount), "payment_date": c.d.Date, "payment_account": c.d.Cash.ID}
	var res struct {
		Data invoice `json:"data"`
	}
	if err := b.api(s, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/payment", c.invoice.ID), body, c.idemKey, &res); err != nil {
		// Refresh the amount due, which a payment on the web may have changed;
		// otherwise "Full" keeps offering the stale amount.
		if inv, getErr := b.getInvoice(s, c.invoice.ID); getErr == nil {
			c.invoice = inv
		}
		b.saveFailed(t, c, err)
		return
	}
	back := c.back
	c.reset()
	b.showInvoice(t, c, &res.Data, back, "Payment recorded.")
}

// --- Parsing and formatting ---

var amountRe = regexp.MustCompile(`^(\d+|\d{1,3}(\.\d{3})+|\d{1,3}(,\d{3})+)$`)

// parseAmount accepts whole rupiah with optional "Rp" and one consistent
// thousands separator. Decimals are rejected on purpose: "150000,00" read
// loosely would be a hundred times too much.
func parseAmount(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "rp") {
		s = strings.TrimLeft(s[2:], ". ")
	}
	s = strings.ReplaceAll(s, " ", "")
	if !amountRe.MatchString(s) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.NewReplacer(".", "", ",", "").Replace(s))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// parseDate accepts ISO dates and Indonesian day-first dates, strictly
// (31/02 is rejected, not rolled over).
func parseDate(s string) (string, bool) {
	for _, layout := range []string{"2006-01-02", "2/1/2006"} {
		if d, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return d.Format("2006-01-02"), true
		}
	}
	return "", false
}

// idr formats rupiah like the web UI: 1500000 -> "Rp 1.500.000".
func idr(n int) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return sign + "Rp " + s
}

func money(s string) string {
	n, _ := strconv.Atoi(s)
	return idr(n)
}

func fmtDate(s string) string {
	if d, err := time.Parse("2006-01-02", s); err == nil {
		return d.Format("2 Jan 2006")
	}
	return s
}

func shortDate(s string) string {
	if d, err := time.Parse("2006-01-02", s); err == nil {
		return d.Format("2 Jan")
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip keeps messages under Telegram's 4096-character limit.
func clip(s string) string { return clipRunes(s, 4000) }

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
