# Migration Notes: HTML-to-API Strangler-Fig

This document is the single source of truth for the ongoing migration from a pure HTML/HTMX app to a dual-surface architecture: HTML routes for the browser UI and JSON API routes at `/api/v1/*` for bots, scripts, MCP servers, and Telegram integrations.

The migration follows the strangler-fig pattern: the HTML surface stays fully functional while the API surface grows alongside it. No HTML routes are removed until the SPA reaches parity for that domain.

---

## 1. Strangler-Fig Invariants

These five rules are immutable contracts. They must not be violated by any implementation task, refactor, or shortcut.

**Invariant 1: Path namespaces never overlap.**
`/invoices`, `/bills`, `/accounts`, and all other HTML routes serve HTML only. `/api/v1/invoices`, `/api/v1/bills`, `/api/v1/accounts`, and all other API routes serve JSON only. The same path NEVER serves both content types. No content-negotiation tricks, no `Accept` header switching on a shared handler.

**Invariant 2: Handlers share models, never response code.**
HTML handlers and API handlers may call the same `model.*` functions (queries, business logic). They must never share response-writing code. An HTML handler returns `text/html`; an API handler returns `application/json`. Mixing them creates a maintenance trap.

**Invariant 3: Both surfaces get new features.**
New features must be implemented for both the HTML surface and the API surface unless the task is explicitly marked `HTML-only` or `API-only`. Skipping one surface creates silent gaps that are expensive to discover later.

**Invariant 4: Auth paths are separate.**
CSRF tokens are for cookie-authenticated browser sessions only. Bearer tokens skip CSRF validation entirely. The two auth paths must never cross: a cookie session must not accept a Bearer token header as a CSRF bypass, and a Bearer token request must not require a CSRF token.

**Invariant 5: Audit log records the auth method.**
The audit log sets `actor_token_id` to the API token's ID when the request came via Bearer token. For cookie-authenticated requests, `actor_token_id` is null. This distinction is permanent and must not be collapsed.

---

## 2. Per-Domain Sunset Criteria

HTML routes for each domain may be removed only after the SPA reaches full parity for that domain. "Parity" means the SPA can do everything the HTML UI can do, including edge cases like print views and admin-only flows.

The 14 domains and their sunset conditions:

### auth

HTML routes: `/login`, `/logout`, `/password/change`

May be removed when the SPA has:
- A login page that sets the session cookie
- A logout flow that invalidates the session
- A password change form for the current user

### accounts

HTML routes: `/accounts`, `/accounts/new`, `/accounts/{id}/edit`, `/accounts/{id}` (delete)

May be removed when the SPA can:
- List accounts with filtering by type and search
- Create a new account
- Edit an existing account
- Delete an account (with validation that it has no journal lines)

### contacts

HTML routes: `/contacts`, `/contacts/new`, `/contacts/{id}/edit`, `/contacts/{id}` (delete)

May be removed when the SPA can:
- List contacts with search
- Create a new contact
- Edit an existing contact
- Delete a contact

### journals

HTML routes: `/journals`, `/journals/new`, `/journals/{id}`, `/journals/{id}/edit`, `/journals/{id}` (delete)

May be removed when the SPA can:
- List journal entries with date range and search filters
- Create a new journal entry with dynamic line items
- Edit an existing journal entry
- Delete a journal entry
- Validate in real time that debits equal credits before submission

### income

HTML routes: `/income`, `/income/new`, `/income/{id}/edit`, `/income/{id}` (delete)

May be removed when the SPA can:
- List income entries with date range filters
- Create a new income entry (auto-creates journal entry)
- Edit an existing income entry
- Delete an income entry

### expenses

HTML routes: `/expenses`, `/expenses/new`, `/expenses/{id}/edit`, `/expenses/{id}` (delete)

May be removed when the SPA can:
- List expense entries with date range filters
- Create a new expense entry (auto-creates journal entry)
- Edit an existing expense entry
- Delete an expense entry

### invoices

HTML routes: `/invoices`, `/invoices/new`, `/invoices/{id}`, `/invoices/{id}/edit`, `/invoices/{id}/send`, `/invoices/{id}/payment`, `/invoices/{id}/print`, `/invoices/{id}` (delete)

May be removed when the SPA can:
- List invoices with status and date range filters
- Create a new invoice with line items
- Edit a draft invoice
- Send an invoice (creates AR journal entry)
- Record a payment against an invoice
- Delete a draft invoice

**Exception (resolved):** A `/api/v1/invoices/{id}/pdf` endpoint now generates the PDF directly (pure-Go stdlib generator in `internal/pdf`, no headless browser), so this no longer blocks invoice sunset. The HTML `/invoices/{id}/print` route is retained as a browser-print convenience but is no longer the only way to produce a printable invoice.

### bills

HTML routes: `/bills`, `/bills/new`, `/bills/{id}`, `/bills/{id}/edit`, `/bills/{id}/receive`, `/bills/{id}/payment`, `/bills/{id}` (delete)

May be removed when the SPA can:
- List bills with status and date range filters
- Create a new bill with line items
- Edit a draft bill
- Mark a bill as received (creates AP journal entry)
- Record a payment against a bill
- Delete a draft bill

### credit-notes

HTML routes: `/credit-notes`, `/credit-notes/new`, `/credit-notes/{id}`, `/credit-notes/{id}/edit`, `/credit-notes/{id}/issue`, `/credit-notes/{id}/void`, `/credit-notes/{id}` (delete)

May be removed when the SPA can:
- List credit notes with status and date range filters
- Create a new credit note linked to an invoice
- Edit a draft credit note
- Issue a credit note (creates reversal journal entry)
- Void an issued credit note
- Delete a draft credit note

### reports

HTML routes: `/reports/trial-balance`, `/reports/profit-loss`, `/reports/balance-sheet`, `/reports/cash-flow`, `/reports/general-ledger`

May be removed when the SPA can display all five reports with date range filters:
1. Trial Balance
2. Profit & Loss
3. Balance Sheet
4. Cash Flow Statement
5. General Ledger (per-account transaction list with running balance)

### users

HTML routes: `/users`, `/users/new`, `/users/{id}/edit`, `/users/{id}` (delete)

May be removed when the SPA has user management (admin-only):
- List users
- Create a new user with role assignment
- Edit a user's details and role
- Delete a user

### roles

HTML routes: `/roles`, `/roles/new`, `/roles/{id}/edit`, `/roles/{id}` (delete)

May be removed when the SPA has role management (admin-only):
- List roles
- Create a new role with capability checkboxes
- Edit a role's capabilities
- Delete a role (with validation that no users hold it)

### audit

HTML route: `/audit`

May be removed when the SPA has an audit log viewer (admin-only) with:
- Filters by actor, action, resource type, and date range
- Pagination
- Display of `actor_token_id` when the action came via API token

### dashboard

HTML route: `/` (root)

May be removed when the SPA has a dashboard showing:
- Current cash balance
- Monthly revenue and expenses (current month)
- Outstanding accounts receivable total
- Outstanding accounts payable total
- Recent transactions list

---

## 3. Auth Strategy Decision Log

The API uses hybrid authentication: session cookies for browser clients and Bearer tokens for non-browser clients. Here's why.

### Why sessions were kept

Sessions were already implemented and working. They provide instant revocation (delete the row, the session is dead), a built-in audit trail (session table records login time and IP), and zero token management complexity for the browser. Throwing them away would have been pure churn.

### Why API tokens were added

Non-browser clients (bots, MCP servers, Telegram integrations, scripts) can't use session cookies reliably. They need long-lived credentials that don't expire on browser close. API tokens are:
- **Scoped**: each token carries a subset of the user's capabilities
- **Revocable**: delete the row, the token is dead immediately
- **Auditable**: every request via Bearer token records `actor_token_id` in the audit log
- **Named**: tokens have human-readable names so you know which bot is which

### Why JWT was rejected

JWT's main selling point is statelessness: no DB lookup per request. For a single-binary, single-VPS deployment with SQLite, this is irrelevant. The DB lookup is a local file read, not a network round-trip.

JWT's main cost is revocation: you need a denylist in the DB to revoke tokens before expiry, which defeats the stateless purpose. You end up with DB lookups anyway, plus the complexity of key rotation, token expiry windows, and clock skew handling.

The attack surface is also larger: JWT libraries have a history of algorithm confusion bugs (`alg: none`, RS256/HS256 confusion). A random 32-byte token stored in the DB has no such attack surface.

Net verdict: JWT adds complexity with no benefit for this deployment model.

---

## 4. Pagination and Data Format Spec

This section is the single source of truth for wire formats. All API endpoints must conform.

### Currency (IDR)

All monetary amounts are JSON strings representing integer IDR values. No decimals, no subunits.

```
"50000"   = Rp 50.000
"1500000" = Rp 1.500.000
```

IDR has no subunit (no cents). Storing as integer strings avoids floating-point rounding and makes the intent explicit. The internal DB representation is `INTEGER` (same value, no scaling).

### Quantities

Quantities are decimal strings with up to 2 decimal places. The internal DB representation is `INTEGER` scaled by 100.

```
"1.50"  = 1.5 units  (stored as 150)
"10.00" = 10 units   (stored as 1000)
"0.25"  = 0.25 units (stored as 25)
```

### Pagination

All list endpoints return a consistent envelope:

```json
{
  "data": [...],
  "meta": {
    "page": 1,
    "per_page": 50,
    "total": 100,
    "total_pages": 2
  }
}
```

Query parameters: `?page=1&per_page=50`
- Default `per_page`: 50
- Maximum `per_page`: 200
- Pages are 1-indexed

### Timestamps

All instants are ISO-8601 UTC strings:

```
"2026-05-10T03:00:00Z"
```

### Business Dates

Date-only fields (invoice date, due date, journal date) use `YYYY-MM-DD`:

```
"2026-05-10"
```

---

## 5. Known Gaps (Deferred)

These items were considered during the API migration plan and explicitly deferred. They are not bugs; they are conscious scope decisions.

**SPA implementation.** The API exists but the SPA that consumes it is a separate plan. The HTML UI remains the primary browser interface until the SPA plan is executed.

**Optimistic locking (ETag / If-Match).** Concurrent edits are not protected by ETags. This is acceptable because `SetMaxOpenConns(1)` serializes all DB writes, and the expected concurrency for a solo-dev single-business deployment is low. A follow-up plan can add ETags when multi-user concurrent editing becomes a real problem.

**CORS for cross-origin SPA hosting.** The API is same-origin only for v1. If the SPA is ever hosted on a different origin (e.g., a CDN), a follow-up plan must add CORS headers. See Section 6 for the full list of changes required.

**OpenAPI code generation.** The OpenAPI spec at `/api/v1/openapi.yaml` is hand-written. Code generation from the spec (client SDKs, server stubs) is deferred. Contract tests verify the spec matches the implementation.

**Rate-limit persistence across deploys.** The in-memory rate limiter resets on every deploy. For a solo-dev cadence with infrequent deploys, this is acceptable. A Redis-backed limiter is a follow-up if abuse becomes a concern.

**`/api/v1/invoices/{id}/pdf` endpoint.** Implemented. PDFs are produced by a pure-Go standard-library writer in `internal/pdf` (no headless browser, no third-party dependency), preserving the single-binary deploy model. Both surfaces are wired: HTML `GET /invoices/{id}/pdf` (inline) and API `GET /api/v1/invoices/{id}/pdf` (attachment). Seller identity and bank details come from the editable Company Profile at `/settings/company`.

**Bulk operations.** No bulk create, bulk update, or bulk delete endpoints exist in v1. Each resource is created/updated/deleted individually. Bulk operations are out of scope.

---

## 6. CORS Policy

**v1 is same-origin only.** No CORS middleware is included. The API is accessed from the same origin as the HTML UI (same host, same port).

If a future plan hosts the SPA on a different origin, the following changes are required before enabling cross-origin access:

1. Add `Access-Control-Allow-Origin` header (specific origin, not `*`, because credentials are involved)
2. Add `Access-Control-Allow-Credentials: true`
3. Change session cookie to `SameSite=None; Secure` (requires HTTPS)
4. Add preflight `OPTIONS` handlers for all mutating endpoints
5. Add `Access-Control-Allow-Headers` to include `Authorization`, `X-CSRF-Token`, `Idempotency-Key`, `Content-Type`
6. Add `Access-Control-Allow-Methods` for each endpoint group

Do not add CORS headers piecemeal. The above changes must be done together in a single plan to avoid half-open CORS states that are hard to debug.

---

## 7. Idempotency Key Behavior

Financial mutation endpoints support the `Idempotency-Key` request header. Covered endpoints:

- `POST /api/v1/invoices`
- `POST /api/v1/bills`
- `POST /api/v1/journals`
- `POST /api/v1/credit-notes`

**Behavior:**
- If a request arrives with an `Idempotency-Key` that was seen within the last 24 hours, the server returns the original response without re-executing the mutation.
- The key is scoped to the authenticated user (two users can use the same key independently).
- Keys are stored in the `idempotency_keys` table with the response body and status code.
- After 24 hours, the key expires and a new request with the same key creates a new resource.

**Client responsibility:** Generate a fresh UUID per request attempt. Use `uuidgen` on macOS/Linux or `crypto.randomUUID()` in browsers.

---

## 8. API Token Scopes Reference

Tokens are created with a subset of the owner's capabilities. Available scopes:

| Scope | Grants |
|-------|--------|
| `accounts.view` | Read accounts |
| `accounts.manage` | Create, edit, delete accounts |
| `contacts.view` | Read contacts |
| `contacts.manage` | Create, edit, delete contacts |
| `journals.view` | Read journal entries |
| `journals.manage` | Create, edit, delete journal entries |
| `income.view` | Read income entries |
| `income.manage` | Create, edit, delete income entries |
| `expenses.view` | Read expense entries |
| `expenses.manage` | Create, edit, delete expense entries |
| `invoices.view` | Read invoices |
| `invoices.manage` | Create, edit, send, payment, delete invoices |
| `bills.view` | Read bills |
| `bills.manage` | Create, edit, receive, payment, delete bills |
| `credit_notes.view` | Read credit notes |
| `credit_notes.manage` | Create, edit, issue, void, delete credit notes |
| `reports.view` | Read all financial reports |
| `users.manage` | User management (admin-only capability) |
| `roles.manage` | Role management (admin-only capability) |
| `audit.view` | Read audit log (admin-only capability) |

A token cannot grant a scope the creating user doesn't hold. Admins can grant any scope.
