# Journal Module

The Journal module is the shared business seam for manual journal entries,
income, and expenses. Income and expenses are intentionally part of this
module because they are constrained, user-facing shapes of a journal entry.

## Interface guarantees

- Every operation accepts `context.Context`.
- Mutations require an authenticated actor and the capability for that exact
  operation: journals, income, or expenses.
- Manual operations cannot change entries created by another source.
- Income and expense operations enforce their account direction and source.
- Every write validates balanced, positive journal lines in one transaction.
- Audit recording is best-effort and occurs after the transaction commits.

## Persistence

SQLite is the concrete persistence implementation. SQL for manual journals,
income, and expenses remains private to this module; no repository interface
is introduced before a second adapter exists. Bill and credit-note posting
still uses a private transitional writer in `internal/model`; that bridge is
removed when those two domains move into their modules.

## Adapter responsibilities

HTML and JSON adapters parse transport input, invoke this module, and translate
typed errors. They do not construct journal lines, choose source types, perform
SQL, or emit audit events.
