# Bun + Effect Rewrite

Status: implementation complete and cutover-ready. Production deployment and
removal of the Go rollback reference remain separate operational decisions.

## Objective

Replace the Go runtime and implementation with Bun and Effect while preserving
the current ERP's observable behavior, stored data, and operating model.

The current Go application remains the reference implementation until every
parity gate passes. Passing a narrow set of new tests is not sufficient evidence
of parity.

## Authoritative baseline

The baseline is the current `main` worktree at `f44f58a`.

- All Go packages pass their tests.
- The suite contains 740 named tests across 74 test files.
- HTTP routes are registered in `cmd/server/main.go`.
- The JSON contract is defined by `api/openapi.yaml` and its contract tests.
- Persistence is defined by 22 migrations in `migrations/`.
- `MIGRATION_NOTES.md` defines the HTML/API separation, authentication,
  idempotency, pagination, and wire-format invariants.
- Templates and static assets are embedded into the deployed Go binary.
- Production is a single executable managed by systemd behind Cloudflare
  Tunnel, using one SQLite database file.

The baseline must be refreshed before each parity milestone because features
may continue to land in the Go implementation during the rewrite.

## Parity inventory

### Public and operational

- Public company profile at `/`
- Parent invoice portal using short contact codes
- Portal invoice PDF download
- Static assets
- `/healthz`, including build version and applied migration count
- Request IDs, structured logs, timeouts, graceful shutdown, and signals
- Single-executable Linux deployment
- Existing environment variables and defaults
- SQLite backup scripts and timers

### Authentication and authorization

- Browser login, logout, password change, and forced first-login password change
- `session_id` cookie behavior
- Six-hour sliding idle expiry, 48-hour absolute expiry, and refresh threshold
- Existing bcrypt password hashes
- Per-session CSRF tokens and CSRF enforcement for cookie mutations
- Bearer API tokens, hashed token storage, scopes, revocation, and last-used time
- Bearer precedence when bearer and cookie credentials are both present
- Runtime intersection of token scopes with the owner's current capabilities
- Admin, bookkeeper, viewer, and custom roles
- Admin's implicit possession of every capability
- Capability-protected HTML and API operations
- Login and public-portal rate limiting

### Accounting and business features

- Chart of accounts
- Contacts, including address, class, pricing, route, distance, portal token,
  and short portal code behavior
- Balanced manual journals and journal lines
- Income and expense workflows that create journal entries
- Invoices and invoice lines
- Draft editing and deletion
- Sending invoices and creating accounts-receivable journal entries
- Invoice payments
- Recurring invoice generation
- Bulk invoice sending and deletion
- Printable invoice HTML, generated PDFs, and WhatsApp links
- Bills and bill lines
- Receiving bills and creating accounts-payable journal entries
- Bill payments
- Credit notes, issue/void transitions, and reversal journal entries
- Trial balance
- Profit and loss
- Balance sheet
- Cash flow
- General ledger with running balances
- Financial dashboard and month-to-month chart data

### Administration and integrations

- User management
- Role and capability management
- Audit log, filters, pagination, request metadata, and `actor_token_id`
- API-token management UI and JSON endpoints
- Company profile and invoice identity/bank details
- Routes, vehicles, and active route assignment persistence used by contacts
- School closures and effective-school-day calculations
- Google Calendar OAuth connection, callback, sync, and disconnect

### Browser behavior

- Existing `/dashboard` route namespace and redirect targets
- Current pages, forms, validation messages, flash messages, and status codes
- HTMX boosted navigation, partial line-item routes, live search, pagination,
  dynamic form rows, and inline deletion
- Current Tailwind/DaisyUI styling and responsive behavior

### JSON behavior

- Every `/api/v1` path and HTTP method in `api/openapi.yaml`
- Exact success and error envelopes
- Status codes and relevant headers
- Currency as integer-IDR strings
- Quantities as decimal strings backed by integers scaled by 100
- UTC timestamp and business-date formats
- One-indexed pagination, defaults, and limits
- Financial-mutation idempotency, including stored responses and 24-hour expiry
- Cookie authentication with CSRF and bearer authentication without CSRF
- Same-origin-only behavior
- OpenAPI document serving and implementation/spec contract checks

### Persistence

- Existing SQLite file opens without conversion.
- Existing IDs and foreign-key relationships remain unchanged.
- Existing migration filenames in `schema_migrations` remain authoritative.
- WAL, busy timeout, synchronous mode, migration ordering, foreign-key
  validation, and single-writer assumptions remain compatible.
- Document numbering and accounting transaction boundaries remain atomic.

## Target architecture

The rewrite uses domain modules with deep interfaces. HTTP and persistence are
adapters at seams; they do not own business rules.

```text
src/
  app/
    config.ts
    layers.ts
    server.ts
  domain/
    accounting/
    auth/
    contacts/
    invoicing/
    billing/
    credit-notes/
    reporting/
    school-calendar/
  adapters/
    sqlite/
    google-calendar/
    pdf/
    web/
      api/
      html/
  infrastructure/
    audit/
    idempotency/
    rate-limit/
    migrations/
  test/
    parity/
```

Each domain module exposes commands and queries expressed as Effects with
typed success, error, and requirement channels. Callers do not perform SQL,
start transactions, translate SQLite errors, or assemble journal side effects.

The accounting module is the central consistency module. Income, expenses,
invoices, bills, payments, and credit notes call its interface rather than
writing journal rows independently.

The browser and JSON adapters call the same domain modules but keep separate
response rendering, preserving the existing strangler-fig invariant. Effect
Schema owns decoding and encoding at the HTTP seam. SQLite transactions remain
inside domain operations so callers cannot partially execute financial changes.

Expected runtime packages:

- `effect`
- `@effect/platform`
- `@effect/platform-bun`
- `@effect/sql`
- `@effect/sql-sqlite-bun`

Package versions will be pinned exactly in `bun.lock`. Additional packages
require a concrete capability not already provided by Bun or Effect.

## Rewrite sequence

### 0. Freeze and automate the reference contract

- Generate a route manifest from the Go composition root and OpenAPI file.
- Capture representative seeded database fixtures.
- Add differential test runners that can exercise Go and Bun independently.
- Define normalization rules for timestamps, random tokens, request IDs, and
  PDF metadata.

### 1. Runtime and persistence foundation

- Add Bun project configuration and strict TypeScript checks.
- Build Effect configuration and application layers.
- Open the existing SQLite database and run the existing SQL migrations.
- Implement static assets, health checks, request context, logging, startup,
  and graceful shutdown.
- Produce the standalone Linux executable and verify the systemd sandbox.

### 2. Security foundation

- Port password verification, sessions, cookies, CSRF, roles, capabilities,
  bearer tokens, rate limits, idempotency, and audit recording.
- Verify existing credentials and active sessions against copied databases.

### 3. Accounting foundation

- Port accounts, contacts, journals, document numbering, and transactions.
- Establish balance and atomicity property tests before dependent workflows.

### 4. Financial workflows

- Port income, expenses, invoices, payments, recurring/bulk operations, bills,
  credit notes, PDFs, and WhatsApp links.

### 5. Read models

- Port all five reports, dashboard totals, trends, and recent transactions.
- Compare results over identical database snapshots, including empty and
  multi-period datasets.

### 6. Administration, integrations, and public surfaces

- Port users, roles, API-token UI, audit UI, company profile, school calendar,
  Google Calendar, public profile, and parent portal.
- Port every HTML template and HTMX interaction unless the frontend decision
  explicitly selects a different client implementation.

### 7. Cutover

- Run the full Go suite, Bun suite, OpenAPI contract suite, and differential
  parity suite.
- Test a copy of the production database, including backup and restore.
- Build and smoke-test the standalone Linux executable under the systemd unit.
- Take a recoverable database backup and document rollback.
- Switch production only after explicit approval.
- Remove Go runtime code only after production parity is confirmed.

## Parity gates

Every migrated feature must pass all applicable gates:

1. Domain tests cover success, expected failures, authorization, and accounting
   invariants through the module interface.
2. The Go and Bun servers receive the same request fixture against equivalent
   database snapshots.
3. Status, headers, redirects, cookies, normalized bodies, and resulting
   database state match.
4. JSON responses validate against the existing OpenAPI contract.
5. HTML responses preserve semantic content, forms, links, HTMX attributes,
   and visual snapshots at desktop and mobile sizes.
6. Generated PDFs preserve invoice data and rendered layout.
7. Failure injection proves financial writes and integration state changes are
   atomic.
8. A copied real database passes migrations, `foreign_key_check`, financial
   balance checks, and representative read/write smoke tests.

## Confirmed decisions

1. Keep the server-rendered HTMX browser interface and existing templates.
2. Keep Go as a tested rollback reference through production confirmation.
3. Preserve in-place compatibility with the existing SQLite database,
   credentials, sessions, and migration ledger.
4. Preserve the standalone systemd and Cloudflare deployment model.
5. Treat every behavior in the parity inventory as in scope.
6. Preserve observable behavior; handle unrelated behavior changes separately.
7. Work on the dedicated rewrite branch without committing or pushing until
   separately approved.

## Verification snapshot

- Strict TypeScript compilation passes.
- The Bun suite covers domain services, all JSON APIs, all HTML workflows,
  public pages, PDFs, authentication, authorization, and route registration.
- The complete Go suite remains green as the rollback reference.
- The existing 21-entry SQLite migration ledger applies unchanged.
- A copy of the local existing database passes `integrity_check`,
  `foreign_key_check`, balanced-journal validation, and representative Effect
  reads without applying a new migration.
- CI builds and smoke-tests the standalone Linux executable before merge.
- Deploy and restore-drill workflows build the Bun executable.
