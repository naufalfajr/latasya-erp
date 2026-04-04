# Latasya ERP

Simple bookkeeping web app for a transport business (school bus & travel) in Indonesia.

Built with Go stdlib, HTMX, Tailwind CSS + DaisyUI, and SQLite. Deploys as a single Docker container behind Cloudflare Tunnel.

## Features

- **Double-entry bookkeeping** — every transaction creates balanced journal entries (debits = credits)
- **Income & Expense recording** — simplified forms that auto-create journal entries
- **Invoices** — create, send (creates AR entry), record payments, print
- **Bills** — create, receive (creates AP entry), record payments
- **Chart of Accounts** — 45 predefined accounts for Indonesian transport business (fuel, tolls, KIR, PKB/STNK, THR, etc.)
- **Contacts** — manage customers and suppliers
- **Financial Reports** — Trial Balance, Profit & Loss, Balance Sheet, Cash Flow, General Ledger
- **User Management** — admin (full CRUD) and viewer (reports only) roles
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
| Deploy | Docker + Cloudflare Tunnel |

No Node.js, no npm, no JS framework. Single binary with embedded templates and static files.

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

### Run with Docker

```bash
# Local development
docker compose -f docker-compose.dev.yml up -d
```

Open http://localhost:8080.

### Deploy to VPS (with Cloudflare Tunnel)

```bash
# On your VPS
git clone <repo> && cd latasya-erp
echo "TUNNEL_TOKEN=your-cloudflare-tunnel-token" > .env
docker compose up -d
```

Requires a domain on Cloudflare DNS with a tunnel configured to point to `http://latasya-erp:8080`.

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
├── Dockerfile                   # Multi-stage build (~15MB image)
├── docker-compose.yml           # Production (app + Cloudflare Tunnel)
├── docker-compose.dev.yml       # Development (app only, port 8080)
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

## Configuration

Environment variables (all optional):

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `DB_PATH` | `./latasya.db` | SQLite database file path |
| `DEV_MODE` | `false` | Re-parse templates on each request |
| `TUNNEL_TOKEN` | — | Cloudflare Tunnel token (production only) |

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
- Docker runs as non-root user
- HTTP server timeouts (Read: 15s, Write: 30s, Idle: 60s)

## License

MIT
