-- Credit Notes (Nota Kredit)
-- A credit note offsets a previously sent invoice by posting a reversing
-- journal entry. The original invoice stays untouched in the books; the
-- credit note records a separate, mirror-image transaction so the audit
-- trail is preserved.
CREATE TABLE IF NOT EXISTS credit_notes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    cn_number   TEXT    NOT NULL UNIQUE,
    contact_id  INTEGER NOT NULL REFERENCES contacts(id),
    invoice_id  INTEGER REFERENCES invoices(id),
    cn_date     TEXT    NOT NULL,
    reason      TEXT    NOT NULL CHECK (reason IN ('cancellation', 'return', 'discount', 'other')),
    status      TEXT    NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'issued', 'void')),
    subtotal    INTEGER NOT NULL DEFAULT 0,
    tax_amount  INTEGER NOT NULL DEFAULT 0,
    total       INTEGER NOT NULL DEFAULT 0,
    notes       TEXT,
    journal_id  INTEGER REFERENCES journal_entries(id),
    created_by  INTEGER NOT NULL REFERENCES users(id),
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_credit_notes_invoice_id ON credit_notes(invoice_id);
CREATE INDEX IF NOT EXISTS idx_credit_notes_contact_id ON credit_notes(contact_id);

CREATE TABLE IF NOT EXISTS credit_note_lines (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    credit_note_id  INTEGER NOT NULL REFERENCES credit_notes(id) ON DELETE CASCADE,
    description     TEXT    NOT NULL,
    quantity        INTEGER NOT NULL DEFAULT 100,
    unit_price      INTEGER NOT NULL,
    amount          INTEGER NOT NULL,
    account_id      INTEGER NOT NULL REFERENCES accounts(id)
);

-- Track how much of each invoice has been credited so the outstanding
-- balance is calculated as total - amount_paid - amount_credited.
ALTER TABLE invoices ADD COLUMN amount_credited INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_credit_note_lines_cn ON credit_note_lines(credit_note_id);
CREATE INDEX IF NOT EXISTS idx_credit_notes_status ON credit_notes(status);
