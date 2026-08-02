# Reporting module

`reporting` owns the read-only financial projections used by the dashboard,
HTML reports, and JSON report endpoints. It keeps SQLite joins, aggregation,
cash-classification behavior, and Jakarta business-period rules behind one
module shared by both transports.

## Boundary

- Transport handlers parse request parameters and format HTML or JSON.
- `Module` executes dashboard and financial-report queries.
- Account selection remains in `internal/account`; reporting accepts an
  account ID for general-ledger projections.
- The module is read-only and uses the concrete SQLite schema directly.
