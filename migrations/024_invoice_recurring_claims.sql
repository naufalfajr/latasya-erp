CREATE TABLE invoice_recurring_claims (
    contact_id    INTEGER NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    invoice_month TEXT    NOT NULL,
    invoice_id    INTEGER REFERENCES invoices(id) ON DELETE CASCADE,
    PRIMARY KEY (contact_id, invoice_month)
);
