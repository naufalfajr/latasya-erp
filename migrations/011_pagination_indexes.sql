-- Indexes supporting the ORDER BY <date> DESC on paginated list pages.
-- journal_entries(entry_date) already exists (003_add_indexes.sql via idx_je_entry_date).
CREATE INDEX IF NOT EXISTS idx_invoices_invoice_date ON invoices(invoice_date);
CREATE INDEX IF NOT EXISTS idx_bills_bill_date ON bills(bill_date);
CREATE INDEX IF NOT EXISTS idx_credit_notes_cn_date ON credit_notes(cn_date);
