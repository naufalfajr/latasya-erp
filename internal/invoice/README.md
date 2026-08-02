# Invoice Module

The Invoice module is the business seam shared by the HTML/HTMX and JSON adapters. It owns invoice validation, authorization, lifecycle transitions, totals, database transactions, journal effects, and typed errors.

## Interface guarantees

- Every operation accepts `context.Context`.
- Mutations accept an authenticated actor and enforce the required capability.
- Create and update operations return complete results; callers never query SQLite connection state such as `last_insert_rowid()`.
- Currency uses integer IDR and quantity uses an integer scaled by 100.
- Lifecycle rules are identical for HTML, JSON, and future MCP callers.
- Audit recording is best-effort and does not determine transaction success.
- This module is the only invoice read and mutation entry point; `internal/model` retains data types but no invoice lifecycle API.

## Adapter responsibilities

HTTP adapters decode forms or JSON, construct commands, invoke the module, and translate results. Redirects, flash messages, HTTP status codes, PDF output, WhatsApp links, and template selection remain adapter concerns. Each adapter owns one invoice route-registration function used by both production and tests so route methods and middleware cannot drift.

## Persistence

SQLite is the concrete implementation. SQL stays private to this module, but no generic repository interface is introduced until another persistence adapter is actually required. Recurring generation uses a database-backed `(contact, month)` claim in the same transaction as invoice creation, preventing duplicate monthly invoices across module instances or processes.

## Migration sequence

The pilot moved mutations first, then queries, then removed duplicated behavior from both HTTP adapters and the legacy model API. Existing routes remain compatible, and representative invoice responses are validated against the OpenAPI schemas.
