-- Additional indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_accounts_type_active ON accounts(account_type, is_active);
CREATE INDEX IF NOT EXISTS idx_contacts_type_active ON contacts(contact_type, is_active);
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_contact ON invoices(contact_id);
CREATE INDEX IF NOT EXISTS idx_invoice_lines_invoice ON invoice_lines(invoice_id);
CREATE INDEX IF NOT EXISTS idx_bills_status ON bills(status);
CREATE INDEX IF NOT EXISTS idx_bills_contact ON bills(contact_id);
CREATE INDEX IF NOT EXISTS idx_bill_lines_bill ON bill_lines(bill_id);
