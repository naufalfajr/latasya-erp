# Latasya ERP

Simple bookkeeping web app for a transport business (school bus & travel) in Indonesia.

Built with Go stdlib, HTMX, Tailwind CSS + DaisyUI, and SQLite. Deploys as a single static binary behind Cloudflare Tunnel.

## Features

- **Double-entry bookkeeping** — every transaction creates balanced journal entries (debits = credits)
- **Income & Expense recording** — simplified forms that auto-create journal entries
- **Invoices** — create, send (creates AR entry), record payments, print
- **Bills** — create, receive (creates AP entry), record payments
- **Chart of Accounts** — 45 predefined accounts for Indonesian transport business (fuel, tolls, KIR, PKB/STNK, THR, etc.)
- **Contacts** — manage customers and suppliers
- **Financial Reports** — Trial Balance, Profit & Loss, Balance Sheet, Cash Flow, General Ledger
- **User Management** — capability-based roles (admin, bookkeeper, viewer) with a `/roles` page to manage custom roles
- **Dashboard** — cash balance, monthly revenue/expenses, outstanding invoices/bills
- **Responsive** — works on desktop and mobile (DaisyUI drawer layout)
- **HTMX** — SPA-like navigation with `hx-boost`, inline delete, live search, dynamic form rows

## Tech Stack

| Layer | Choice |
|-------|--------|
| Backend | Go stdlib (`net/http`, `html/template`) |
| Frontend | HTMX + Tailwind CSS + DaisyUI (CDN) |
| Database | SQLite via `modernc.org/sqlite` (pure Go, no CGO) |
| Auth | Session-based (bcrypt + HttpOnly cookie) |
| Deploy | systemd + Cloudflare Tunnel |

No Node.js, no npm, no JS framework. Single binary with embedded templates and static files.

## API

Latasya ERP exposes a JSON API at `/api/v1/*` alongside the HTML UI. Bots, scripts, MCP servers, and Telegram integrations authenticate via scoped Bearer tokens managed at `/settings/api-tokens`.

### Quick Start

**1. Login and get a session:**
```bash
curl -s -c /tmp/cookies.txt -X POST \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}' \
  http://localhost:8080/api/v1/auth/login
```

**2. Create an API token (cookie auth required):**
```bash
CSRF=$(curl -s -b /tmp/cookies.txt http://localhost:8080/api/v1/auth/csrf | jq -r .csrf_token)
TOKEN=$(curl -s -b /tmp/cookies.txt \
  -H "X-CSRF-Token: $CSRF" \
  -H "Content-Type: application/json" \
  -d '{"name":"My Bot","scopes":["reports.view","invoices.manage"]}' \
  http://localhost:8080/api/v1/api-tokens | jq -r .data.plaintext)
```

**3. Use the Bearer token:**
```bash
# List accounts
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/accounts | jq .meta.total

# Create an invoice (with idempotency)
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"contact_id":1,"invoice_date":"2026-05-10","due_date":"2026-06-10","tax_amount":"0","notes":"","lines":[{"description":"Service","quantity":"1.00","unit_price":"500000","account_id":4001}]}' \
  http://localhost:8080/api/v1/invoices | jq .data.invoice_number
```

**4. Revoke the token:**
```bash
TOKEN_ID=$(curl -s -b /tmp/cookies.txt http://localhost:8080/api/v1/api-tokens | jq '.data[] | select(.name=="My Bot") | .id')
curl -s -X DELETE \
  -b /tmp/cookies.txt \
  -H "X-CSRF-Token: $CSRF" \
  http://localhost:8080/api/v1/api-tokens/$TOKEN_ID
```

### Authentication

| Method | Use Case | How |
|--------|----------|-----|
| Session cookie | Browser / SPA | Login via `/api/v1/auth/login`, cookie set automatically |
| Bearer token | Bots, MCP, Telegram, scripts | Create at `/settings/api-tokens`, use `Authorization: Bearer lat_...` |

Bearer tokens are scoped (subset of your capabilities) and revocable. They skip CSRF validation.

### Pagination

All list endpoints return:
```json
{"data": [...], "meta": {"page": 1, "per_page": 50, "total": 100, "total_pages": 2}}
```
Query params: `?page=1&per_page=50` (default 50, max 200).

### Errors

All errors return:
```json
{"error": "message", "code": "snake_case_code", "request_id": "...", "fields": {...}}
```

### Idempotency

Financial mutations (invoices, bills, journals, credit-notes) support `Idempotency-Key` header. Replaying the same key within 24h returns the original response without re-executing.

### OpenAPI Spec

```bash
curl http://localhost:8080/api/v1/openapi.yaml
```

See `MIGRATION_NOTES.md` for the full migration strategy and sunset criteria.

## Quick Start

### Prerequisites

- Go 1.22+ (uses stdlib routing patterns)
- [Tailwind CSS standalone CLI](https://tailwindcss.com/blog/standalone-cli) (auto-downloaded by `make`)

### Run locally

```bash
# Build CSS and start the server
make css
make run

# Or with CSS watcher (two terminals)
make css-watch  # terminal 1
make run        # terminal 2
```

Open http://localhost:8080. Login with `admin` / `admin`.

### Deploy to VPS (with Cloudflare Tunnel)

Prerequisites: domain on Cloudflare DNS, a Linux VPS (amd64), SSH access.

**One-time VPS setup:**

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin latasya
sudo mkdir -p /var/lib/latasya
sudo chown latasya:latasya /var/lib/latasya
sudo chmod 750 /var/lib/latasya
sudo timedatectl set-timezone Asia/Jakarta
```

Install the systemd unit:

```bash
sudo cp deploy/latasya-erp.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable latasya-erp
```

**Build and ship the binary (from your Mac):**

```bash
make build-linux                     # produces ./latasya-erp for linux/amd64
scp latasya-erp user@vps:/tmp/
ssh user@vps 'sudo install -m 755 /tmp/latasya-erp /usr/local/bin/latasya-erp && sudo systemctl restart latasya-erp'
```

**Cloudflare Tunnel (one-time):**

```bash
curl -L https://pkg.cloudflare.com/install.sh | sudo bash
sudo apt install cloudflared
cloudflared tunnel login
cloudflared tunnel create latasya
sudo cp deploy/cloudflared-config.yml.example /etc/cloudflared/config.yml
# edit config.yml: set tunnel id, credentials path, hostname
sudo cloudflared tunnel route dns latasya latasya.naufalf.net
sudo cloudflared service install
sudo systemctl enable --now cloudflared
```

The tunnel is outbound-only — you can keep ports 80/443/8080 closed on the VPS firewall.

## Project Structure

```
latasya-erp/
├── cmd/server/main.go           # Entry point, routes, graceful shutdown
├── internal/
│   ├── auth/                    # Login, sessions, middleware (RequireAuth, AdminOnly)
│   ├── database/                # SQLite setup, migration runner, seed
│   ├── handler/                 # HTTP handlers (one file per feature)
│   ├── model/                   # Data structs + DB queries (no ORM)
│   ├── testutil/                # Test helpers (in-memory DB, auth)
│   └── tmpl/                    # Template functions (formatIDR, formatDate, dict)
├── migrations/                  # SQL migrations (embedded, auto-applied)
├── templates/                   # HTML templates (embedded)
├── static/                      # CSS, JS (embedded)
├── embed.go                     # Go embed directives
├── deploy/                      # systemd unit + cloudflared config example
└── Makefile
```

## Accounting Model

### Double-Entry Bookkeeping

Every financial transaction creates a journal entry with at least 2 lines where total debits equal total credits.

| Transaction | Debit | Credit |
|-------------|-------|--------|
| Record income | Cash/Bank (asset +) | Revenue (+) |
| Record expense | Expense (+) | Cash/Bank (asset -) |
| Send invoice | Accounts Receivable (+) | Revenue (+) |
| Receive invoice payment | Cash/Bank (+) | Accounts Receivable (-) |
| Receive bill | Expense (+) | Accounts Payable (+) |
| Pay bill | Accounts Payable (-) | Cash/Bank (-) |

### Chart of Accounts

Predefined for an Indonesian transport business:

- **Assets (1-xxxx):** Cash, Bank BCA/Mandiri, Accounts Receivable, Vehicles, Equipment
- **Liabilities (2-xxxx):** Accounts Payable, Tax Payable, Vehicle Loans
- **Equity (3-xxxx):** Owner Capital, Drawings, Retained Earnings
- **Revenue (4-xxxx):** School Bus Contract, Extra Trip, Travel Charter, Airport Transfer
- **Expenses (5-xxxx):** Fuel (Solar/Pertamax), Maintenance, Spare Parts, Tires, Driver/Kenek Salary, THR, Insurance, Tolls, Parking, PKB/STNK, KIR, Route Permit, Office Rent, Utilities, Depreciation

### Reports

- **Trial Balance** — verify debits = credits for a period
- **Profit & Loss** — revenue minus expenses = net income
- **Balance Sheet** — Assets = Liabilities + Equity + Retained Earnings
- **Cash Flow** — opening cash + inflows - outflows = closing cash
- **General Ledger** — per-account transaction list with running balance

## Development Notes

### Key Patterns

- **Template rendering**: `handler.render(w, r, "templates/foo/bar.html", "Title", data, ...extraTemplates)` loads base + partials + page. Cached in production, re-parsed every request when `DEV_MODE=true`. Each page is parsed separately to avoid `{{define "content"}}` collisions.
- **Authorization**: Write endpoints are wrapped with `auth.CapabilityOnly(model.CapXxxManage, handler)` — the capability-to-role mapping lives in the `roles` table and is editable via `/roles`. Admin implicitly holds every capability.
- **Journal entries are the core**: Income, expenses, invoices, and bills all create journal entries. No separate transaction tables — reports read from `journal_entries` and `journal_lines`.
- **SQLite single connection**: `SetMaxOpenConns(1)` prevents "database is locked". Generate document numbers *before* starting transactions.
- **Tests**: Each test calls `testutil.SetupTestDB()` for an isolated in-memory DB. `handler_test.go` sets up a full `httptest.Server` with all routes wired, mirroring `cmd/server/main.go`.

### Conventions

- **Currency**: stored as integer IDR. `formatIDR(150000)` → `"Rp 150.000"`. No subunits, no floats.
- **Quantity**: stored as integer scaled ×100 (so `150` means `1.5`). Forms accept/display decimal via `formatQty` / `parseQuantity`.
- **Account codes**: `1-xxxx` assets, `2-xxxx` liabilities, `3-xxxx` equity, `4-xxxx` revenue, `5-xxxx` expenses.
- **Document numbers**: auto-generated as `PREFIX-YYYYMM-NNNN` (e.g. `JE-202604-0001`).
- **Migrations**: numbered SQL files in `migrations/`, tracked in `schema_migrations`, applied on startup with foreign-key enforcement temporarily disabled.

## Configuration

Environment variables (all optional):

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `DB_PATH` | `./latasya.db` | SQLite database file path |
| `DEV_MODE` | `false` | Re-parse templates on each request |

## Testing

```bash
make test
# or
go test ./... -v
```

101 tests covering:
- Auth flow (login, logout, sessions, middleware, admin/viewer roles)
- CRUD for all entities (accounts, contacts, journals, income, expenses, invoices, bills, users)
- Accounting correctness (balanced entries, P&L, balance sheet equation, trial balance)
- Invoice/bill lifecycle (create → send/receive → payment → paid)
- Authorization (viewer denied write access)

## Currency

All monetary values are stored as integers in IDR (Indonesian Rupiah). IDR has no subunit, so `5000000` = Rp 5.000.000. No floating-point math.

## Security

- Passwords hashed with bcrypt
- Session tokens: 32 bytes cryptographically random, HttpOnly + SameSite=Lax + Secure cookies
- Session fixation prevention (old sessions invalidated on login)
- Admin-only enforcement on all write endpoints (POST/DELETE)
- All SQL queries parameterized (no injection)
- HTML auto-escaped by `html/template` (no XSS)
- systemd service runs as non-root `latasya` user with filesystem sandboxing
- Cloudflare Tunnel: no inbound ports exposed on the VPS
- HTTP server timeouts (Read: 15s, Write: 30s, Idle: 60s)

## License

MIT
