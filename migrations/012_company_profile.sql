-- company_profile: single-row table holding the seller's identity and payment
-- details shown on invoices (the HTML print view, and the PDF once added).
-- The CHECK (id = 1) constraint enforces exactly one row; the app always
-- reads and writes id = 1.
CREATE TABLE company_profile (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    name                TEXT NOT NULL DEFAULT '',
    tagline             TEXT NOT NULL DEFAULT '',
    address             TEXT NOT NULL DEFAULT '',
    phone               TEXT NOT NULL DEFAULT '',
    email               TEXT NOT NULL DEFAULT '',
    npwp                TEXT NOT NULL DEFAULT '',
    bank_name           TEXT NOT NULL DEFAULT '',
    bank_account_number TEXT NOT NULL DEFAULT '',
    bank_account_holder TEXT NOT NULL DEFAULT '',
    invoice_footer      TEXT NOT NULL DEFAULT '',
    updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Seed the single row pre-filled with the values previously hardcoded in the
-- invoice print template, so the print view renders identically until edited.
INSERT INTO company_profile (id, name, tagline) VALUES
    (1, 'Latasya Transport', 'School Bus & Travel Service');
