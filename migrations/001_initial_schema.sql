-- Users
CREATE TABLE IF NOT EXISTS users (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    username    TEXT    NOT NULL UNIQUE,
    password    TEXT    NOT NULL,
    full_name   TEXT    NOT NULL,
    role        TEXT    NOT NULL CHECK (role IN ('admin', 'viewer')),
    is_active   INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    expires_at TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- Chart of Accounts
CREATE TABLE IF NOT EXISTS accounts (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    code           TEXT    NOT NULL UNIQUE,
    name           TEXT    NOT NULL,
    account_type   TEXT    NOT NULL CHECK (account_type IN (
                       'asset', 'liability', 'equity', 'revenue', 'expense'
                   )),
    normal_balance TEXT    NOT NULL CHECK (normal_balance IN ('debit', 'credit')),
    parent_id      INTEGER REFERENCES accounts(id),
    is_system      INTEGER NOT NULL DEFAULT 0,
    is_active      INTEGER NOT NULL DEFAULT 1,
    description    TEXT,
    created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Contacts (customers & suppliers)
CREATE TABLE IF NOT EXISTS contacts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    contact_type TEXT    NOT NULL CHECK (contact_type IN ('customer', 'supplier', 'both')),
    phone        TEXT,
    email        TEXT,
    address      TEXT,
    notes        TEXT,
    is_active    INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Journal Entries
CREATE TABLE IF NOT EXISTS journal_entries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_date    TEXT    NOT NULL,
    reference     TEXT,
    description   TEXT    NOT NULL,
    source_type   TEXT,
    source_id     INTEGER,
    is_posted     INTEGER NOT NULL DEFAULT 1,
    created_by    INTEGER NOT NULL REFERENCES users(id),
    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_je_entry_date ON journal_entries(entry_date);
CREATE INDEX IF NOT EXISTS idx_je_source ON journal_entries(source_type, source_id);

-- Journal Lines
CREATE TABLE IF NOT EXISTS journal_lines (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id   INTEGER NOT NULL REFERENCES journal_entries(id) ON DELETE CASCADE,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    debit      INTEGER NOT NULL DEFAULT 0,
    credit     INTEGER NOT NULL DEFAULT 0,
    memo       TEXT,
    CONSTRAINT chk_debit_or_credit CHECK (
        (debit > 0 AND credit = 0) OR (debit = 0 AND credit > 0)
    )
);

CREATE INDEX IF NOT EXISTS idx_jl_entry_id ON journal_lines(entry_id);
CREATE INDEX IF NOT EXISTS idx_jl_account_id ON journal_lines(account_id);

-- Invoices
CREATE TABLE IF NOT EXISTS invoices (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_number TEXT    NOT NULL UNIQUE,
    contact_id     INTEGER NOT NULL REFERENCES contacts(id),
    invoice_date   TEXT    NOT NULL,
    due_date       TEXT    NOT NULL,
    status         TEXT    NOT NULL DEFAULT 'draft' CHECK (status IN (
                       'draft', 'sent', 'paid', 'partial', 'overdue', 'cancelled'
                   )),
    subtotal       INTEGER NOT NULL DEFAULT 0,
    tax_amount     INTEGER NOT NULL DEFAULT 0,
    total          INTEGER NOT NULL DEFAULT 0,
    amount_paid    INTEGER NOT NULL DEFAULT 0,
    notes          TEXT,
    journal_id     INTEGER REFERENCES journal_entries(id),
    created_by     INTEGER NOT NULL REFERENCES users(id),
    created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Invoice Lines
CREATE TABLE IF NOT EXISTS invoice_lines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_id  INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    description TEXT    NOT NULL,
    quantity    INTEGER NOT NULL DEFAULT 100,
    unit_price  INTEGER NOT NULL,
    amount      INTEGER NOT NULL,
    account_id  INTEGER NOT NULL REFERENCES accounts(id)
);

-- Bills
CREATE TABLE IF NOT EXISTS bills (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    bill_number  TEXT    NOT NULL UNIQUE,
    contact_id   INTEGER NOT NULL REFERENCES contacts(id),
    bill_date    TEXT    NOT NULL,
    due_date     TEXT    NOT NULL,
    status       TEXT    NOT NULL DEFAULT 'draft' CHECK (status IN (
                     'draft', 'received', 'paid', 'partial', 'overdue', 'cancelled'
                 )),
    subtotal     INTEGER NOT NULL DEFAULT 0,
    tax_amount   INTEGER NOT NULL DEFAULT 0,
    total        INTEGER NOT NULL DEFAULT 0,
    amount_paid  INTEGER NOT NULL DEFAULT 0,
    notes        TEXT,
    journal_id   INTEGER REFERENCES journal_entries(id),
    created_by   INTEGER NOT NULL REFERENCES users(id),
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Bill Lines
CREATE TABLE IF NOT EXISTS bill_lines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    bill_id     INTEGER NOT NULL REFERENCES bills(id) ON DELETE CASCADE,
    description TEXT    NOT NULL,
    quantity    INTEGER NOT NULL DEFAULT 100,
    unit_price  INTEGER NOT NULL,
    amount      INTEGER NOT NULL,
    account_id  INTEGER NOT NULL REFERENCES accounts(id)
);

-- Payments
CREATE TABLE IF NOT EXISTS payments (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    payment_date   TEXT    NOT NULL,
    amount         INTEGER NOT NULL,
    payment_type   TEXT    NOT NULL CHECK (payment_type IN ('invoice', 'bill')),
    reference_id   INTEGER NOT NULL,
    payment_method TEXT,
    account_id     INTEGER NOT NULL REFERENCES accounts(id),
    journal_id     INTEGER REFERENCES journal_entries(id),
    notes          TEXT,
    created_by     INTEGER NOT NULL REFERENCES users(id),
    created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_payments_ref ON payments(payment_type, reference_id);
