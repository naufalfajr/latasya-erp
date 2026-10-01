# Telegram Bot

A Telegram front end for daily bookkeeping. Staff can:

- record, edit, delete, and view **income** and **expenses** (with an optional vehicle tag)
- view **invoices**, mark drafts as **sent**, and record full or partial **payments**

The bot is a plain client of the JSON API (`/api/v1`). It imports no ERP internals and holds no business rules. Each person connects with their **own API token**, so every change is authorized and audited as that ERP user (`audit_log.actor_token_id` shows it came through the bot).

## How staff connect

1. Log in to the ERP web UI. New users change their password first.
2. Open **API Tokens** in the sidebar → **Create new token**.
3. Tick `income.manage`, `expenses.manage`, and `invoices.manage`. Leave the expiry empty.
4. Open a private chat with the bot and paste the token.

The bot deletes the token message immediately and replies "Connected as …". If the token is missing a scope, the bot warns at connect time and those actions fail with a clear message.

Passwords never go into Telegram. Bot chats are not end-to-end encrypted, and a token can be revoked on its own. Scopes limit what a token can change; it can still read all ERP data, like a logged-in user.

**Disconnecting:**

- `/logout` in the bot revokes the token in the ERP.
- Revoking the token under ERP → API Tokens, or deactivating the user, cuts the bot off on the next tap.

## Commands

| Command | Does |
|---|---|
| `/menu` | Main menu: Income, Expense, Invoices |
| `/income`, `/expense` | Recent entries, 10 per page, with **+ Add** |
| `/invoices` | Invoices by status |
| `/cancel` | Close the current form |
| `/logout` | Revoke the token and disconnect |
| `/start` | Connection status |

Adding or editing always ends on a **review screen** with one button per field and **Save**.

- **Amounts:** whole rupiah, such as `150000`, `150.000`, or `Rp 150.000`. Decimals are rejected on purpose, because a misread `150000,00` would be 100× too much.
- **Dates:** **Today** and **Yesterday** use WIB. You can also type `2026-09-30` or `30/9/2026`.
- **Accounts:** deposit / paid-from / payment choices show only cash and bank accounts (`is_cash`).

## Safety behavior

- **Private chats only.** Messages in groups are ignored. Also disable groups in BotFather (see below).
- **Stale buttons expire.** Every screen's buttons carry a nonce that rotates on navigation and after each save. A double tap or an old message says "This menu expired" instead of acting twice.
- **No duplicate records.** Creates and payments send an `Idempotency-Key`. A retried Save after a network error replays instead of duplicating.
- **Edit collision guard.** On Save, the bot re-reads the entry. If someone changed it on the web after the form was opened, the edit is aborted and the latest version is shown.
- **Protected session file.** Tokens are stored in `BOT_STATE_PATH` with mode 0600. In production the unit runs as a systemd dynamic user, so the file is readable only by root and the bot cannot read the ERP database.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | — (required) | From BotFather |
| `LATASYA_API_URL` | `http://127.0.0.1:8080` | ERP base URL |
| `BOT_STATE_PATH` | `latasya-telegram-sessions.json` | Connected users' tokens (0600) |

## Setup

**1. Create the bot (once)** in Telegram with [@BotFather](https://t.me/BotFather):

- `/newbot` gives you the token.
- `/setjoingroups` → **Disable**, so the bot can't be added to groups.

Use a **separate bot for development**. Telegram allows only one poller per bot token, and a second one gets `409 Conflict`.

**2. Local development:**

```bash
make run                                  # terminal 1: ERP on :8080
TELEGRAM_BOT_TOKEN=<dev-bot-token> make run-bot   # terminal 2
```

**3. Production (once per VPS).** The deploy workflow already builds and installs the binary and `deploy/latasya-telegram.service` on every push to `main`. It only (re)starts the bot once you enable it:

```bash
sudo install -m 600 -o root -g root /dev/null /etc/latasya/latasya-telegram.env
sudoedit /etc/latasya/latasya-telegram.env   # add: TELEGRAM_BOT_TOKEN=<prod-bot-token>
sudo systemctl enable --now latasya-telegram
journalctl -u latasya-telegram -f
```

After that, each deploy restarts the bot after the ERP passes its health check. If the bot fails to start, the deploy job fails, but the ERP is never rolled back because of it.

## Tests

```bash
go test ./cmd/telegram-bot/
```

`bot_test.go` runs the bot against the real ERP API (in-process SQLite) and a fake Telegram server. It covers:

- connect and disconnect
- the income, expense, and invoice flows end to end
- stale taps
- edit collisions
- token revocation

It also checks Telegram's limits: `callback_data` ≤ 64 bytes, text ≤ 4096 characters, and one answer per callback.
