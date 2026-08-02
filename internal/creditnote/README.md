# Credit-note module

`internal/creditnote` owns customer credit-note validation, queries, draft
changes, issuing, voiding, accounting postings, linked-invoice balance changes,
and audit events. Issue and void commit every affected row atomically.

HTTP handlers translate form or JSON input and map module errors; they must not
write credit-note, invoice, or journal tables directly.
