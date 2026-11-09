# MCP Plan

Single source of truth for adding an MCP (Model Context Protocol) server for
Latasya ERP. Three architectures were designed and compared (§2); the chosen
one is a **local stdio binary calling the existing JSON API**, so the VPS
gains almost nothing and all authorization keeps flowing through the existing
token machinery.

---

## 1. Goals

1. **All API operations become MCP tools** — one tool per OpenAPI operation,
   named by its `operationId` verbatim.
2. **Scoped by the user's own capabilities** — MCP uses an ordinary
   `api_tokens` row; scopes ⊆ owner capabilities at mint time, re-intersected
   with the *current* role on every request (`v1.BearerOrCookie` →
   `intersectScopes`). A role change shrinks the MCP token instantly.
3. **Browser login** — `latasya-mcp login` opens the browser to a consent page
   behind the existing session login; no passwords in the AI client, token
   plaintext never transits the browser.
4. **Light on server resources** — no new process, port, goroutine, or cleaner
   goroutine. Server diff: 1 JSON endpoint, 1 consent page, 1 small table.
5. **Hardened API + MCP + browser login** — Section 6; two of the items
   (PDF gate, reports gate) are prerequisites that must land first.
6. **Invoice PDF guardrail** — `/api/v1/invoices/{id}/pdf` gets an
   `invoices.manage` capability gate before any MCP token exists.

## Non-goals

- Remote/streamable-HTTP MCP transport and a first-party OAuth 2.1
  authorization server (Phase 2, only if a remote AI client actually appears —
  Section 8).
- Bulk/`resources`/`prompts` MCP capabilities. Tools only.
- Any SPA work (see Section 9 — decided: stay SSR).

---

## 2. Architecture

```
AI client (Claude Desktop / Claude Code / Codex / …)
   │  MCP 2025-06-18 over stdio (JSON-RPC 2.0)
   ▼
cmd/mcp-server  (local binary; imports NO internal/ packages, like cmd/telegram-bot)
   │  HTTPS + Authorization: Bearer lat_…  + Idempotency-Key
   ▼
existing /api/v1/*   →  BearerOrCookie → capability gates → modules → audit (actor_token_id)

Browser login (once per ~90 days):
latasya-mcp login ──► POST /api/v1/mcp/device (poll, rate-limited)
user's browser ────► GET/POST /dashboard/mcp/authorize?code=AB2C-D3F4
                      (existing session login → consent: scope grid + expiry)
token minted at poll time via apitoken.Module.Create, returned once to the binary
```

Why this shape (compared against two alternatives that were fully designed):

| | A. Local stdio (chosen) | B. Embedded `/mcp` + 3 meta-tools | C. Embedded `/mcp` + 80 tools + OAuth 2.1 AS |
|---|---|---|---|
| New server surface | 1 endpoint + 1 page + 1 table | JSON-RPC endpoint + OAuth facade + 1 table | JSON-RPC endpoint + hand-rolled AS + 3 tables |
| Server resources | ~zero (ordinary bearer requests) | stateless, cheap | stateless, cheap |
| Model reliability | concrete tools, host-side selection | **worst** — string-keyed dispatch, 3 round trips on new tasks, schema errors only after a round trip | concrete tools, best |
| `tools/list` context cost | ~12–25K tokens (78 tools) | ~300 tokens | ~12–25K tokens |
| Auth code to get right | device handoff (~150 LOC) | OAuth facade | full AS: codes, PKCE, redirect matching, DCR |
| Remote/web AI clients | no (local hosts only) | yes | yes |

B's catalog concept and C's hardening inventory are folded in where they
strengthen A (spec-driven codegen; bearer rate limiting; security headers).
A loses only remote-client support — deferrable, and everything Phase 1 builds
(PDF/reports gates, capability annotations, consent page, token naming) is
reused verbatim by a Phase 2 HTTP transport.

---

## 3. Tool surface

### Inventory

84 `operationId`s in `api/openapi.yaml`. Excluded (documented in the generated
table + enforced by a CI test):

| Excluded | Reason |
|---|---|
| `login`, `logout`, `getCSRFToken` | cookie-session semantics; useless over bearer; passwords must never enter LLM context |
| `getOpenAPISpec`, `listCapabilities` | public meta, no ERP data |
| `createAPIToken`, `revokeAPIToken` | server already rejects bearer callers (`internal/api/v1/apitokens/handlers.go`) — a token must never spawn a token |

Kept: `getMe` (live `token_scopes` introspection), `revokeCurrentToken`
(AI can kill its own credential), `changePassword`. → **~77 tools**, under the
128-name protocol limit, no pagination of `tools/list` needed.

### Generation, not hand-writing

`cmd/mcp-server/gen/main.go` runs via `go:generate` (`make mcp-tools`) against
`api/openapi.yaml` with kin-openapi (already a direct dependency), emitting
`tools_gen.go`: a pure-data table `{name, method, pathTemplate, params, body
schema, capability, annotations, isPDF}`. Build-time codegen chosen over
runtime spec-fetch: offline `tools/list`, no YAML parser shipped, deterministic.

A companion CI test asserts **every spec operation is mapped or explicitly
excluded** — a new endpoint fails CI until consciously classified.

### Schemas and annotations

- Path params → required properties; query params (`page`, `per_page`,
  `status`, `from`, `to`, `account`, `granularity`, …) → optional typed
  properties; request body → a `body` object property. `Idempotency-Key`
  headers are stripped from schemas (Section "Idempotency").
- `description` = spec `summary` + wire-format rules (integer IDR strings,
  `"1.50"` quantities, `YYYY-MM-DD` dates) ride along in property
  descriptions.
- Annotations: `GET` → readOnly/idempotent; `PUT` → idempotent, not
  destructive; `DELETE` → destructive+idempotent; `POST` → neither;
  `destructiveHint` additionally true for `delete*`/`void*`/`bulkDelete*`
  POSTs. `openWorldHint: false` everywhere.
- `getInvoicePDF` (detected by its `application/pdf` response) returns a
  resource content block `{uri: "latasya://invoices/{id}.pdf", mimeType:
  "application/pdf", blob: <base64>}`; blobs capped at 2 MiB.

### Idempotency — deterministic keys

The binary never exposes the header as a parameter. For every mutation it
computes `key = base64url(SHA-256(token_id ‖ operationId ‖ canonical_json(args)))`
and sends it as `Idempotency-Key`. An identical retry after a transport hiccup
**replays** the original 2xx from the 24 h idempotency store instead of
double-posting an invoice. A deliberate second identical create inside 24 h
also replays the first record — for financial data that duplicate-suppression
is a feature, and the replayed record is visible in the tool result. An
optional `_idempotency_key` input property (underscore-prefixed, stripped
before the HTTP call) overrides the derivation.

### Spec extensions (single source of truth for the mapping)

Add per-operation extensions to `api/openapi.yaml`:

```yaml
x-latasya-capability: invoices.manage   # "" / omitted = any authenticated user
x-latasya-auth: cookie-only             # for the two api-tokens ops
```

Consumed by the generator (pre-flight UX + docs) and two new assertions in
`internal/api/v1/openapi_contract_test.go`: unique `operationId`s; capability
values ∈ `model.AllCapabilities`; every mutating non-public operation declares
a capability. The extensions mirror handler gates — the contract test keeps
them honest, and the capability table below is locked by per-domain tests.

### Capability table (verified against handler gates)

| Operations | Capability |
|---|---|
| list/get × all domains, `listExpenseVehicles`, `getDashboard`, `listAPITokens` (own), `getMe`, `revokeCurrentToken`, `changePassword` | any authenticated |
| accounts / contacts / income / expenses / journals create+update+delete | `accounts.manage` / `contacts.manage` / `income.manage` / `expenses.manage` / `journals.manage` |
| invoices CUD, send, payment, generate-recurring, bulk-delete, bulk-send | `invoices.manage` |
| credit-notes (all, incl. issue/void) | `invoices.manage` |
| bills CUD, receive, payment | `bills.manage` |
| `getInvoicePDF` | `invoices.manage` **(new gate — Section 6.1)** |
| reports ×5 | `reports.view` **(new gate — Section 6.2; capability exists but is currently decorative)** |
| users (all) | `users.manage` |
| roles (all) | `roles.manage` |
| `listAuditLog` | `audit.view` |
| school-calendar ×4, `syncGoogleCalendar` | `invoices.manage` (sync: admin-only) |
| `createAPIToken`, `revokeAPIToken` | cookie-only → excluded from MCP |

`tools/list` is filtered client-side by the token's scopes (UX only); the
server remains the sole enforcer on every call.

### Composite tools — decision: stay 1:1

Tools stay 1:1 with operations; the LLM orchestrates multi-step flows. The
rule for when a sequence deserves more than that:

**If partial failure leaves meaningful state → it becomes an API endpoint,
not an MCP composite.** Multi-call sequences with real business semantics
belong on the server, where they get one transaction, one validation story,
one audit row — and then arrive as a 1:1 tool via codegen. This repo already
works this way: `sendInvoice` (creates the AR journal entry), `receiveBill`,
`generateRecurringInvoices`, `bulkSendInvoices` are all absorbed workflows.
Any future composite (e.g. "issue and send") follows the same path.

**Pure read-gathering and lookup-then-act flows stay LLM-orchestrated.**
Resolving `PT Maju Jaya → contact_id` or `Bank BCA → account_id` via
list-then-create is exactly what models are good at (they can ask the user
when a match is ambiguous — a composite would have to guess). MCP hosts fan
out independent reads in parallel, so "dashboard + 3 reports" is one round
trip anyway. Composites would also break the every-tool↔operationId contract
test, reintroduce hand-written tool code (the drift surface codegen removed),
and complicate idempotency semantics across multiple requests.

Escape hatch: if real usage shows a repeated 4+ call pattern with format
transformation the models get wrong, revisit — as an API endpoint first.

---

## 4. Browser login — device-code handoff

Device flow (RFC 8628 shape), not loopback OAuth: the token never enters a
browser URL, history, or redirect; no `127.0.0.1:<port>` squatting attack;
works identically over SSH/headless.

1. `latasya-mcp login --base-url https://erp.example.com` generates a 32-byte
   `device_code` (kept in memory) + 8-char `user_code`, prints and opens
   `https://erp.example.com/dashboard/mcp/authorize?code=AB2C-D3F4`.
2. Binary polls `POST /api/v1/mcp/device` every 5 s (first poll creates the
   pending row; responses: `202 pending` / `200 approved` / `403 denied` /
   `410 expired`). Endpoint rate-limited 120/15 min per `unspoofableClientIP`
   (all calls consume — covers a 10-min poll cycle and caps junk rows).
3. Browser: existing session (or the ordinary `/dashboard/login` behind
   `LoginRateLimiter`) → consent page: client string, "approve only codes
   you just generated" warning, scope checkbox grid = `availableScopes(user)`
   (`internal/handler/apitokens.go:35`), expiry select (default **90 days**;
   empty = never). POST is CSRF-checked. No token exists yet — only the
   decision is stored. Small prerequisite: the HTML login currently has a
   fixed redirect target (`internal/handler/auth.go`); add an allowlisted
   `next` param (same-app `/dashboard/mcp/authorize` URLs only, modeled on
   `safeJournalReturnTo`, `internal/handler/journal_navigation.go`) so an
   unauthenticated user bounces back to the consent page after logging in.
4. Next poll with status=approved: the server **re-derives the user's
   capabilities from the DB at this moment**, then mints the token via
   `apitoken.Module.Create` (name `MCP <client> <YYYY-MM-DD>`) and deletes the
   row in one transaction. If the role shrank in the window, consume fails
   with "capabilities changed; restart login".
5. Binary receives `{token, token_id, scopes, username, expires_at}` once,
   saves config, prints "Connected as … (N capabilities)".

Layers that keep scopes honest (all existing): grid offers only owned caps →
`allowedScopes` rejects supersets → `intersectScopes` re-checks per request.
The token is an ordinary `api_tokens` row: visible and revocable in
Settings → API Tokens; every MCP call audited with `actor_token_id`.

**New module** `internal/mcpauth` (mirrors the schoolcalendar oauth-state
shape): `Touch` (upsert-pending + lazy expiry sweep — no cleaner goroutine),
`Approve`, `Deny`, `Consume` (mints via apitoken). Table:

```sql
CREATE TABLE mcp_handoffs (
  device_code_hash  TEXT PRIMARY KEY,      -- sha256; plaintext never stored
  user_code         TEXT NOT NULL UNIQUE,  -- 8-char Crockford base32
  client            TEXT NOT NULL,
  status            TEXT NOT NULL,         -- pending | approved | denied
  approved_user_id  INTEGER REFERENCES users(id) ON DELETE CASCADE,
  approved_username TEXT NOT NULL DEFAULT '',
  scopes            TEXT NOT NULL DEFAULT '[]',
  token_expires_at  TEXT,
  created_at        TEXT NOT NULL DEFAULT (datetime('now')),
  expires_at        TEXT NOT NULL          -- +10 minutes
);
CREATE INDEX idx_mcp_handoffs_expires ON mcp_handoffs(expires_at);
```

Audit events: `mcp.handoff.approve`, `mcp.handoff.deny` (token creation
already audits as `api_token.create`).

### Lifetimes

| Thing | Lifetime | Enforced / cleaned by |
|---|---|---|
| stdio MCP session (client ↔ binary) | life of the AI client's connection; stateless — new `initialize` per connect, no server-side state anywhere | protocol; nothing stored |
| API call | 15 s client timeout; server `WriteTimeout` 30 s | `cmd/server/main.go` + client |
| Login handoff row (`mcp_handoffs`) | 10 min, single-use, deleted at consume | lazy sweep in `Touch`/`Consume`; no goroutine |
| Browser session (consent/login) | existing rules: 6 h idle / 48 h absolute | `internal/auth`, unchanged |
| MCP token (`api_tokens` row) | consent-selected expiry, **default 90 days** (empty = never) | server-side per request (`apitoken.Authenticate`); `status` shows days left |
| Idempotency replay window | 24 h per `(user, key)` | `internal/idempotency` hourly cleaner |
| Rate-limit buckets | in-memory, idle-evicted after 1 h, reset on deploy (pre-existing, accepted) | `internal/api/v1/ratelimit.go` |

Token renewal = a new login (`latasya-mcp login` mints a fresh token via a
new consent); no refresh tokens. Revocation kills access on the very next
request: settings UI (self or `users.manage`), `latasya-mcp logout`
(`DELETE /api/v1/auth/token`), the `revokeCurrentToken` tool, or user
deactivation. A role change doesn't end the token — it shrinks the effective
scopes instantly via `intersectScopes`. Expired/revoked tokens surface as 401
→ "run `latasya-mcp login`"; the process stays alive and `tools/list` keeps
working.

---

## 5. Client layout (`cmd/mcp-server/`)

```
main.go        subcommands: login | status | logout | serve (default)
config.go      Config{BaseURL, Token, TokenID, Username, Scopes, ExpiresAt}
               os.UserConfigDir()/latasya/mcp.json, 0600, atomic write
               env: LATASYA_API_URL, LATASYA_MCP_CONFIG, LATASYA_MCP_TOKEN
client.go      api() — ported from cmd/telegram-bot/main.go:289 (bearer,
               JSON, Idempotency-Key, error decode, token redaction)
login.go       device handoff: codegen, poll, openBrowser
server.go      JSON-RPC 2.0 stdio loop: initialize / ping / tools/list /
               tools/call only; listChanged:false; protocolVersion 2025-06-18
dispatch.go    execute(toolDef, args): path substitution, query encoding,
               deterministic idempotency keys, envelope passthrough, PDF→resource
gen/main.go    go:generate: api/openapi.yaml → tools_gen.go (kin-openapi)
tools_gen.go   GENERATED — pure data, DO NOT EDIT
server_test.go in-process harness via internal/testutil (bot_test.go pattern)
```

Deep seams: `server.go` knows MCP, not ERP; `dispatch.go` knows ERP
conventions, not JSON-RPC; `client.go` knows transport. The binary imports no
`internal/` packages — ERP refactors cannot break it, matching the
telegram-bot precedent.

Token at rest: 0600 config file, plaintext (same documented tradeoff as
`BOT_STATE_PATH`); `LATASYA_MCP_TOKEN` env for headless/CI; token never
logged. `status` prints days-to-expiry; 401s surface as "run
`latasya-mcp login`". `logout` = `DELETE /api/v1/auth/token` + config wipe.

Makefile: `run-mcp`, `mcp-tools` (regen). CI: cross-compile matrix publishes
client artifacts (darwin/arm64, darwin/amd64, linux/amd64, linux/arm64);
nothing is deployed to the VPS.

---

## 6. Security hardening

### 6.1 Invoice PDF gate (prerequisite, ships first)

`internal/api/v1/invoices/handlers.go` `PDF()` (~:499): add

```go
if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
    v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden,
        "invoices.manage capability required", nil)
    return
}
```

+ regression test (viewer token → 403) + `x-latasya-capability` in the spec.
The rendered PDF embeds bank account + NPWP that JSON never returns; this
closes it for every down-scoped token (Telegram bot included), not just MCP.
Mirror gate on the HTML print route is unnecessary (browser path already sits
behind login + role UI), but the HTML `/invoices/{id}/pdf` download route
takes the same gate if it exists at implementation time — verify.

### 6.2 Reports gate (decorative capability made real)

`CapReportsView` appears in **zero** production files — the 5 API report
handlers and the 5 HTML report routes are open to any authenticated user.
Add `HasEffectiveCapability(r.Context(), model.CapReportsView)` to the API
handlers and wrap `RegisterReportingRoutes` patterns with
`auth.RequireCapability(model.CapReportsView)` (same style as `/users`).
Note: this narrows existing behavior — any custom role relying on
"authenticated ⇒ reports" needs `reports.view` added. System roles seeded
with viewer/bookkeeper intent should be checked during implementation.

### 6.3 API surface

- **Bearer rate limiting (new)**: `v1.BearerRateLimiter()` — token bucket
  keyed by authenticated token ID (e.g. 120/min, failures consume), wrapping
  `/api/v1/` bearer traffic + the device endpoint. Today only login and
  portal codes are limited; a runaway agent loop must not monopolize the
  single SQLite writer. 429 + `Retry-After` + `rate_limited` error code
  (code constant already exists).
- **Security headers middleware (new)**, applied at the outer mux:
  `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`,
  `X-Frame-Options: DENY`, `Cache-Control: no-store` on `/api/v1/` responses,
  HSTS when not `DEV_MODE`, and a CSP for the HTML surface
  (`default-src 'self'`; audit inline styles/scripts from DaisyUI/HTMX and
  allow exactly what the templates need — prefer nonces over
  `'unsafe-inline'` where feasible).
- **Contract-test assertions**: unique operationIds; valid capability
  extensions; every mutating non-public operation declares a capability (no
  silent un-gated mutations).

### 6.4 Browser login (existing, verified — keep and extend)

Already in place: bcrypt (DefaultCost), login rate limit 5/15 min per IP +
username (failures only), session cookies HttpOnly + Secure(prod) +
SameSite=Lax with 48 h absolute expiry and DB invalidation, constant-time CSRF
compare, `must_change_password` enforcement, `audit.RequestContext`
request-id/IP on everything. Extensions: consent page inherits all of it via
the protected mux; handoff rows are hashed, 10-min TTL, single-use,
lazy-swept; poll endpoint rate-limited; mint-at-consume means an
approved-but-never-polled login mints nothing.

### 6.5 MCP-specific

- Phishing bound: consent shows client string + warning; a tricked approval
  is bounded by the victim's own capabilities, fully audited, one-click
  revocable.
- Scope shrink mid-session: server `intersectScopes` 403s instantly; the
  binary's cached scope list is UX only; `getMe` reflects truth.
- The client enforces HTTPS outside `DEV_MODE`.
- No token amplification: excluded ops can't be reached through MCP by
  construction (the binary only emits method+path pairs compiled from the
  embedded spec).

---

## 7. Build order & verification

| Phase | Deliverable | Verification |
|---|---|---|
| 0 | PDF gate + spec annotation; reports gates (API + HTML) | new handler tests: viewer token 403 on PDF, non-`reports.view` role 403 on reports; `go test ./internal/api/... ./internal/handler/...` |
| 1 | Spec extensions + contract-test assertions; `BearerRateLimiter`; security-headers middleware | contract test green; limiter test (429 after burst, 2xx refund); header test on HTML + API responses |
| 2 | Migration 027; `internal/mcpauth`; `POST /api/v1/mcp/device` + limiter; consent page/template | module unit tests (Touch/Approve/Deny/Consume, expiry, single-use, scope-shrink-at-consume); handler test for the poll state machine; consent page test (CSRF, scope grid, audit rows) |
| 3 | `cmd/mcp-server`: config/login/client/dispatch/server + codegen + coverage test; Makefile + CI artifacts; README | `go test ./cmd/mcp-server/` against in-process ERP (login → tools/list scoped by role → create+retry-replay → revoke); coverage test fails on unclassified operation |
| 4 | Docs: `cmd/mcp-server/README.md` (bot-README style), MODULES.md row (MCP client + mcpauth), MIGRATION_NOTES mention, root README auth table | review |

End-to-end manual check: `make run` + `latasya-mcp login` against a dev
instance with two roles (admin, viewer) and confirm different `tools/list`
and 403 behavior; revoke the token in the UI and watch calls die.

---

## 8. Phase 2 (deferred): remote MCP transport

If a **remote/web** AI client (Claude web connectors, ChatGPT connectors)
becomes a real requirement: mount stateless streamable-HTTP `POST /mcp` on
the existing binary + a first-party OAuth 2.1 (code + PKCE, RFC 9728
discovery) authorization server minting the same `lat_` tokens. Everything in
this plan — capability table, spec extensions, consent UX, token naming,
bearer rate limiting, PDF/reports gates — is reused; the stdio client keeps
working unchanged. Do not build it speculatively.

## 9. Decision record: SSR vs API-calling frontend

**Stay SSR (HTML/HTMX). Do not move the browser UI onto `/api/v1` now.**

- MIGRATION_NOTES §5 already defers the SPA; nothing in the MCP work changes
  that calculus. MCP consumes the JSON API directly — it needs no SPA.
- The API is machine-shaped: bearer tokens, no CSRF, integer-IDR strings,
  pagination envelopes. An SPA would reintroduce cookie auth + CSRF, add a JS
  build chain, CORS work (API is same-origin only today), and an XSS surface —
  real cost, zero new capability.
- Per-domain HTML sunset criteria in MIGRATION_NOTES §2 remain the trigger for
  any future SPA work, evaluated per domain — not a global rewrite, and never
  coupled to MCP.
- The decision is reversible: API parity is already maintained by the
  dual-surface invariant, so an SPA remains buildable whenever a product
  reason appears.

---

## 10. Implementation decision log

### Phase 0 — capability gates (PDF + reports)

1. **HTML `/invoices/{id}/print` gated along with `/invoices/{id}/pdf`.** The
   plan gated the pdf download "if it exists"; print renders the identical
   bank-details document and `/whatsapp` (the third share path) was already
   `manage()`-gated, so leaving print open would be the same leak in HTML
   form. All three now use the existing `manage()` wrapper in
   `internal/handler/invoice_routes.go`.
2. **HTML report routes use a `RequireCapability` sub-mux** (mirrors the
   `/users` pattern in `routes.go`) rather than wrapping each handler; API
   report handlers gate inline with `HasEffectiveCapability` (mirrors the
   invoices idiom) so the check sees bearer scope intersection, not just role.
3. **PDF gate placed before the id parse** — 403 wins over 404, so an
   unprivileged caller gets no invoice-existence oracle. Matches every other
   gated handler in the package (gate first).
4. **Dashboard stays any-authenticated** per plan; only `/reports/*` gated.
5. **`TestCapabilityEnforcement` (reports) rewritten** from a log-only probe
   that documented "reports are open to all authenticated users" to asserting
   403 for an empty-scope token and 200 for a `reports.view`-scoped token.
6. **Spec annotations added now, not in Phase 1** for the six gated operations
   (`x-latasya-capability` + documented 403 responses) so spec and behavior
   never diverge between phases; Phase 1 adds the contract-test assertions and
   the remaining sweep.
7. No schema migration needed. Seeded roles (manager/bookkeeper/viewer) all
   already carry `reports.view`; only custom roles without it are narrowed.
8. **Review round 1 found and fixed 3 issues**: (a) my PDF handler edit had
   swallowed the invalid-id `WriteError`/`return` — 404 only happened because
   `Document(ctx, 0)` happens to return ErrNotFound; restored. (b) invoice
   view page showed Print/PDF buttons outside the `invoices.manage` block —
   moved inside (same conditional as the WhatsApp button). (c) sidebar showed
   the Reports section and the dashboard rendered "View exact values" links
   to `/reports/*` unconditionally — both now conditioned on `reports.view`,
   matching the sidebar's existing capability-conditional pattern. Review
   round 2 (independent reviewer + self): no must-fix findings; confirmed
   403-before-404 ordering, operation-level spec annotations, no other
   bank-detail surface (company profile is admin-only; portal PDF is
   intentionally code-gated public), and no resource/audit changes.

### Phase 1 — API hardening (extensions, rate limit, headers, login next)

1. **`newRateLimiter` gained a `refundOn2xx` parameter.** Login and portal
   limiters keep refunding successful responses (brute-force surfaces where
   only failures should count). `BearerRateLimiter` passes false: it is a
   throughput cap protecting the single SQLite writer, so every request
   consumes quota — a loop of *successful* requests is exactly what it must
   stop.
2. **`BearerRateLimiter`: 120/min per credential** (bearer → token id,
   cookie → user id, fallback `unspoofableClientIP`), wired inside
   `BearerOrCookie` in `cmd/server/main.go` so the auth context is populated
   when the key function runs. 2/s sustained with 120-burst is far above
   human/bot menu-cadence and far below what could starve the writer.
3. **`SecurityHeaders` lives in `internal/handler`** (app-wide, not
   API-only). CSP keeps `'unsafe-inline'` for scripts and styles — a dozen
   HTMX templates embed inline blocks (upgrade path: nonces when templates
   consolidate) — but blocks external loads, outbound `connect-src`,
   framing, and cross-origin form posts. `fonts.googleapis.com` is
   allowlisted for the public home page. HSTS only outside dev mode;
   `Cache-Control: no-store` on `/api/v1/*`; never overrides headers an
   inner handler already set (portal keeps `private, no-store`).
4. **Login `next` is tightly allowlisted** (`safeMCPReturnTo`): only the MCP
   consent path (with or without `BasePath`), query preserved; everything
   else — absolute URLs, `//host`, other app paths, prefix tricks — falls
   back to the dashboard. Percent-encoded values are decoded by `FormValue`
   *before* validation, so `/mcp%2fauthorize` simply validates as its
   decoded, allowlisted self — safe. `must_change_password` rotation outranks
   `next`. The login page reflects only the sanitized value into the hidden
   field.
5. **Spec vocabulary**: `x-latasya-capability` (one of the 11 capabilities)
   and `x-latasya-auth` ∈ {public, any, cookie-only, bearer-only}. 59
   annotations in this phase (65 total with Phase 0's six). The contract
   test now asserts unique operationIds, valid values, and that every
   mutating operation is classified — no endpoint can ship silently
   un-gated. `syncGoogleCalendar` carries
   `invoices.manage` even though it is admin-only; the admin-only nuance
   stays in its description (the extension vocabulary encodes capabilities,
   and the server enforces the admin check regardless).
6. **Spec endpoint made actually public.** `GET /api/v1/openapi.yaml` was
   registered inside the auth-wrapped `apiMux` (unauthenticated → 401) while
   the spec itself declares `security: []`. Moved to the outer mux, where the
   more specific Go 1.22 pattern wins over the `/api/v1/` subtree — behavior,
   `security: []`, and the new `public` marker now agree. No limiter wraps
   it: it is an embedded-FS read with no DB access.
7. **Review round 1 (independent) found 2 must-fix + 2 should-fix, all
   applied**: (a) the `/api/v1/` mount line lost its leading tab — CI's
   gofmt gate would fail; restored. (b) CSP lacked `'unsafe-eval'`, which
   htmx needs for the journal form's `hx-vals="js:…"` (Function
   constructor) — without it the Add-Line button breaks in every browser;
   directive added with the reason documented in code and pinned by test.
   (c) `listCapabilities` was annotated `public` while sitting behind
   `BearerOrCookie` — same outer-mux treatment as the spec endpoint, so
   `security: []`, behavior, and marker agree. (d) failed-login re-renders
   dropped `next`, so one wrong password during an MCP handoff dumped the
   user on the dashboard — all four error paths now carry the sanitized
   `Next`, with a test proving `next` survives a failed login.

### Phase 2 — device handoff backend (migration 027, mcpauth, endpoints)

1. **Mint-at-consume, delete-before-mint.** The token exists only in the
   final poll response; if minting fails the handoff row is already gone, so
   the user restarts login rather than inheriting half-issued state. A
   replayed device code after a successful mint starts a *fresh* pending
   handoff (no re-mint path) — tested at the endpoint level.
2. **Two independent scope guards at mint time.** Consume re-derives the
   approver's role capabilities from the DB (LookupUserByIDForAuth), and
   `apitoken.Create` independently enforces scopes ⊆ current capabilities.
   A role shrunk in the approve/poll window fails with 403 `scope_changed`.
   The consent form additionally drops scopes beyond the submitter's current
   capabilities, so the stored decision is already clean.
3. **Lazy expiry sweep** on every Touch/Consume/Lookup (`DELETE WHERE
   expires_at < now`, indexed) — no background goroutine, unlike session
   cleanup. Rows live ≤ 10 minutes.
4. **Touch is an idempotent upsert** (`ON CONFLICT(device_code_hash) DO
   NOTHING`). A device hash arriving with a different user code means the
   client restarted login: reported as gone (410), the stale row dies at TTL.
5. **Device endpoint is unauthenticated by design** and rate-limited
   120/15 min per unspoofable IP with all calls consuming (covers one full
   10-minute poll cycle; caps junk-row creation). Payload length checks keep
   junk out of the table; the device code is stored only as SHA-256.
6. **Consent page sits inside the protected mux** (RegisterMCPRoutes), so it
   inherits RequireAuth + CSRF + the must-change-password gate. Expiry
   select: 30/90/never, default 90. Approve and deny are audited
   (`mcp.handoff.approve`/`deny`); token creation audits via the existing
   `api_token.create`. Token name `MCP <client≤40> <date>`.
7. **Spec**: `/mcp/device` added with `x-latasya-auth: public` — the
   contract-test rule (every mutating op classified) covers it.
8. **Review round 1 (independent) found 1 must-fix + 1 should-fix + 2
   nice-to-have, all applied**: (a) the deterministic mint-time token name
   collided with `api_tokens`' `UNIQUE(user_id, name)` — with soft-only
   revocation, a same-day re-login (logout→login, renewal, second machine
   with the same client string) 500'd for the rest of the UTC day; names
   now carry a 2-byte random suffix (`MCP <client> <date> <hex>`), pinned
   by a same-day-relogin regression test. (b) my collision fix had
   accidentally removed Touch's sweep call — restored (the public endpoint
   cleans up its own junk rows again). (c) client-string truncation is now
   UTF-8-safe. (d) Deny carries the same expiry predicate as Approve, so a
   just-expired handoff reports invalid-code instead of a phantom success.
   The reviewer also verified clean: the user_code-collision path (fixed
   mid-review, mapped to 410), double-mint impossibility
   (delete-before-mint + single connection), CSRF on the consent form, and
   the sweep's string-compare expiry check. Round 2 (self): gofmt/vet/full
   suite green.
