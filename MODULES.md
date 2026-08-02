# Module Index

This file is the entry point for the architecture. Detailed behavior lives beside the implementation it describes.

| Area | Responsibility | Documentation |
|---|---|---|
| Invoice | Invoice lifecycle, validation, accounting mutations, and queries | [`internal/invoice/README.md`](internal/invoice/README.md) |
| Journal | Manual journals, income, expenses, and accounting-entry invariants | [`internal/journal/README.md`](internal/journal/README.md) |
| Bill | Supplier bills, receiving, payments, and payable accounting | [`internal/bill/README.md`](internal/bill/README.md) |
| Credit note | Customer credits, linked-invoice settlement, and reversing accounting | [`internal/creditnote/README.md`](internal/creditnote/README.md) |
| Account | Chart of accounts, cash classification, and deletion protection | [`internal/account/README.md`](internal/account/README.md) |
| Contact | Customers, suppliers, pricing attributes, and portal family identity | [`internal/contact/README.md`](internal/contact/README.md) |
| Company | Seller profile and validated invoice defaults | [`internal/company/README.md`](internal/company/README.md) |
| Access | Users, roles, password storage, and authorization invariants | [`internal/access/README.md`](internal/access/README.md) |
| API tokens | Scoped bearer credential issuance, lookup, and revocation | [`internal/apitoken/README.md`](internal/apitoken/README.md) |
| School calendar | Closures, billing-day pricing, and Google Calendar connection state | [`internal/schoolcalendar/README.md`](internal/schoolcalendar/README.md) |
| Audit | Best-effort business and security event recording | [`internal/audit/README.md`](internal/audit/README.md) |
| JSON API | Versioned JSON transport, authentication, errors, pagination, and idempotency | [`internal/api/README.md`](internal/api/README.md) |
| Templates | Full-page and HTMX fragment rendering conventions | [`templates/README.md`](templates/README.md) |
| Invoice templates | Invoice pages and HTMX fragment contracts | [`templates/invoices/README.md`](templates/invoices/README.md) |

## Dependency direction

HTML/HTMX and JSON code are adapters. They translate HTTP input, call a business module, and translate its result. Business modules own validation, authorization, lifecycle rules, transactions, and typed errors. SQLite details remain private to module implementations.

Business modules may call external integrations through a narrow interface when production and test adapters both exist. Modules must not import HTML templates or JSON response types.
