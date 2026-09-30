// Command telegram-bot lets Latasya staff record income and expenses and move
// invoices through their lifecycle from Telegram. It is a plain client of the
// ERP JSON API: every Telegram user connects with their own API token, so all
// changes are authorized and audited as that ERP user.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// version identifies the build. Overridden at link time via
// `-ldflags "-X main.version=<sha>"`; stays "dev" for local `go run`.
var version = "dev"

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		slog.Error("TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}
	b := newBot("https://api.telegram.org/bot"+token,
		envOr("LATASYA_API_URL", "http://127.0.0.1:8080"),
		envOr("BOT_STATE_PATH", "latasya-telegram-sessions.json"))
	if err := b.loadSessions(); err != nil {
		slog.Error("failed to load sessions", "path", b.statePath, "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("starting telegram bot", "version", version, "api", b.apiURL)
	if err := b.run(ctx); err != nil {
		slog.Error("telegram bot stopped", "error", err)
		os.Exit(1)
	}
	slog.Info("telegram bot stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// bot holds all state. Updates are handled one at a time on a single
// goroutine, so nothing here needs a mutex.
// ponytail: sequential handling; fine for a handful of staff, add per-chat
// workers if the ERP ever gets slow enough to queue users behind each other.
type bot struct {
	tgURL     string // https://api.telegram.org/bot<token>; contains the secret
	apiURL    string
	statePath string
	tgHTTP    *http.Client
	apiHTTP   *http.Client
	sessions  map[int64]*session // Telegram user ID -> ERP credential
	chats     map[int64]*chat    // Telegram user ID -> in-memory wizard state
}

func newBot(tgURL, apiURL, statePath string) *bot {
	return &bot{
		tgURL:     tgURL,
		apiURL:    apiURL,
		statePath: statePath,
		tgHTTP:    &http.Client{Timeout: 70 * time.Second}, // outlives the 50s long poll
		apiHTTP:   &http.Client{Timeout: 15 * time.Second},
		sessions:  map[int64]*session{},
		chats:     map[int64]*chat{},
	}
}

// run long-polls Telegram until ctx is cancelled. It only returns an error
// when Telegram rejects the bot token, since retrying cannot fix that.
func (b *bot) run(ctx context.Context) error {
	if err := b.tg("setMyCommands", map[string]any{"commands": commands}, nil); err != nil {
		slog.Warn("setMyCommands failed", "error", err)
	}
	var offset int64
	backoff := time.Second
	for ctx.Err() == nil {
		var updates []update
		err := b.tgCtx(ctx, "getUpdates", map[string]any{
			"offset": offset, "timeout": 50, "allowed_updates": []string{"message", "callback_query"},
		}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var te *tgError
			if errors.As(err, &te) && te.Code == http.StatusUnauthorized {
				return err
			}
			slog.Warn("getUpdates failed", "error", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.handleSafely(u)
		}
	}
	return nil
}

// handleSafely contains a panic to the one update. Without it, Telegram would
// redeliver the same poisoned update after every restart: a crash loop.
func (b *bot) handleSafely(u update) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic while handling update", "update_id", u.UpdateID, "panic", r)
		}
	}()
	b.handle(u)
}

// --- Telegram Bot API ---

type update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *message       `json:"message"`
	CallbackQuery *callbackQuery `json:"callback_query"`
}

type message struct {
	MessageID int     `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text"`
}

type tgUser struct {
	ID    int64 `json:"id"`
	IsBot bool  `json:"is_bot"`
}

type tgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type callbackQuery struct {
	ID      string   `json:"id"`
	From    tgUser   `json:"from"`
	Message *message `json:"message"`
	Data    string   `json:"data"`
}

type button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type keyboard [][]button

type tgError struct {
	Code        int
	Description string
}

func (e *tgError) Error() string { return fmt.Sprintf("telegram error %d: %s", e.Code, e.Description) }

func (b *bot) tg(method string, params, out any) error {
	return b.tgCtx(context.Background(), method, params, out)
}

func (b *bot) tgCtx(ctx context.Context, method string, params, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.tgURL+"/"+method, bytes.NewReader(body))
	if err != nil {
		return redact(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.tgHTTP.Do(req)
	if err != nil {
		return redact(err)
	}
	defer resp.Body.Close()
	var env struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
		ErrorCode   int             `json:"error_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("telegram %s: decode response: %w", method, err)
	}
	if !env.OK {
		return &tgError{Code: env.ErrorCode, Description: env.Description}
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// redact drops the request URL from transport errors: it embeds the bot
// token, which must never reach the logs.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("telegram request: %w", ue.Err)
	}
	return err
}

// say sends a new message.
func (b *bot) say(chatID int64, text string, kb keyboard) {
	params := map[string]any{"chat_id": chatID, "text": clip(text)}
	if len(kb) > 0 {
		params["reply_markup"] = map[string]any{"inline_keyboard": kb}
	}
	if err := b.tg("sendMessage", params, nil); err != nil {
		slog.Warn("sendMessage failed", "error", err)
	}
}

// show edits the tapped message in place, or sends a new one when the turn
// came from typed text.
func (b *bot) show(t turn, text string, kb keyboard) {
	if t.msgID != 0 {
		if kb == nil {
			kb = keyboard{}
		}
		err := b.tg("editMessageText", map[string]any{
			"chat_id": t.chatID, "message_id": t.msgID, "text": clip(text),
			"reply_markup": map[string]any{"inline_keyboard": kb},
		}, nil)
		var te *tgError
		if err == nil || (errors.As(err, &te) && te.Code == http.StatusBadRequest && strings.Contains(te.Description, "not modified")) {
			return
		}
		slog.Warn("editMessageText failed, sending new message", "error", err)
	}
	b.say(t.chatID, text, kb)
}

func (b *bot) answer(callbackID, toast string) {
	params := map[string]any{"callback_query_id": callbackID}
	if toast != "" {
		params["text"] = toast
	}
	if err := b.tg("answerCallbackQuery", params, nil); err != nil {
		slog.Warn("answerCallbackQuery failed", "error", err)
	}
}

func (b *bot) deleteMessage(chatID int64, messageID int) {
	if err := b.tg("deleteMessage", map[string]any{"chat_id": chatID, "message_id": messageID}, nil); err != nil {
		slog.Warn("deleteMessage failed", "error", err)
	}
}

// --- ERP JSON API ---

type apiErr struct {
	Status  int
	Code    string            `json:"code"`
	Message string            `json:"error"`
	Fields  map[string]string `json:"fields"`
}

func (e *apiErr) Error() string { return fmt.Sprintf("erp api %d %s: %s", e.Status, e.Code, e.Message) }

func (b *bot) api(s *session, method, path string, body any, idempotencyKey string, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, b.apiURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := b.apiHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		e := &apiErr{Status: resp.StatusCode}
		_ = json.NewDecoder(resp.Body).Decode(e)
		return e
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- Sessions ---

// session is one Telegram user's ERP credential. It is persisted so restarts
// and deploys don't force everyone to paste their token again.
type session struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	FullName string `json:"full_name"`
}

func (b *bot) loadSessions() error {
	data, err := os.ReadFile(b.statePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &b.sessions)
}

// saveSessions replaces the state file atomically. os.CreateTemp creates the
// file 0600, so tokens are never readable by other users, even mid-write.
// ponytail: plaintext tokens at rest; the systemd unit isolates this file
// under a dynamic user. Encrypting would need a key stored on the same box.
func (b *bot) saveSessions() {
	if err := b.writeSessions(); err != nil {
		slog.Error("failed to save sessions", "path", b.statePath, "error", err)
	}
}

func (b *bot) writeSessions() error {
	data, err := json.Marshal(b.sessions)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.statePath), ".sessions-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), b.statePath)
}
