# Bill module

`internal/bill` owns supplier-bill validation, queries, draft changes, receiving,
payments, accounting postings, and audit events. Receiving and payment each
commit the bill, payment, and balanced journal rows in one SQLite transaction.

HTTP handlers translate form or JSON input and map module errors; they must not
write bill, payment, or journal tables directly.
