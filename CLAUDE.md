# CLAUDE.md

## Project Overview

Latasya ERP is a bookkeeping web app for an Indonesian transport business (school bus & travel). It uses double-entry bookkeeping with Go stdlib, HTMX, Tailwind/DaisyUI, and SQLite.

## Build & Run

```bash
make css        # Build Tailwind CSS (downloads CLI if needed)
make run        # Run in dev mode (DEV_MODE=true)
make test       # Run all tests
make build      # Build production binary
```

## Architecture

- `cmd/server/main.go` — entry point, all routes registered here
- `internal/model/` — data structs + SQL queries, no ORM. All money as `int` (IDR, no subunits).
- `internal/handler/` — HTTP handlers, one file per feature. Shared utils in `util.go`.
- `internal/auth/` — bcrypt passwords, session management, `RequireAuth`/`AdminOnly` middleware
- `internal/database/` — SQLite open, PRAGMA setup, migration runner (embed.FS), seed
- `templates/` — Go `html/template` with DaisyUI. Each page parsed separately (base + partials + page) to avoid `{{define "content"}}` collisions.
- `embed.go` — root-level embed directives for templates, static, migrations
- `internal/model/constants.go` — string constants for roles, statuses, source types, account codes

## Key Patterns

- **Template rendering:** `handler.render(w, r, "templates/foo/bar.html", "Title", data, ...extraTemplates)` — loads base+partials+page, caches in production, re-parses in dev mode.
- **Admin-only routes:** Write endpoints use `auth.AdminOnly(h.HandlerFunc)` wrapper. User management uses `auth.RequireAdmin(adminMux)` on a sub-mux.
- **Journal entries are the core:** Income, expenses, invoices, and bills all create journal entries. No separate transaction tables.
- **SQLite single connection:** `SetMaxOpenConns(1)` — prevents deadlocks. Generate document numbers BEFORE starting transactions.
- **Tests:** In-memory SQLite via `testutil.SetupTestDB()`. Each test gets its own DB. `handler_test.go` sets up a full `httptest.Server` with all routes.

## Conventions

- Currency: integer IDR. `formatIDR(150000)` → `"Rp 150.000"`
- Account codes: `1-xxxx` assets, `2-xxxx` liabilities, `3-xxxx` equity, `4-xxxx` revenue, `5-xxxx` expenses
- Document numbers: auto-generated as `PREFIX-YYYYMM-NNNN` (e.g., `JE-202604-0001`)
- Migrations: numbered SQL files in `migrations/`, tracked in `schema_migrations` table
