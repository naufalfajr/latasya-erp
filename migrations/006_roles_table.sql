-- Roles table with JSON-encoded capabilities list per role.
-- The users-table rebuild below relies on FK enforcement being disabled by
-- the migration runner — IDs are preserved during the rebuild so integrity
-- is maintained, and the runner runs `PRAGMA foreign_key_check` afterwards.
CREATE TABLE IF NOT EXISTS roles (
    name         TEXT    PRIMARY KEY,
    description  TEXT    NOT NULL DEFAULT '',
    is_system    INTEGER NOT NULL DEFAULT 0,
    capabilities TEXT    NOT NULL DEFAULT '[]',
    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Seed the three system roles. Admin capability list is unused at runtime
-- (admin is special-cased to have every capability), but we record it for
-- clarity in the UI.
INSERT INTO roles (name, description, is_system, capabilities) VALUES
    ('admin',
     'Full system access',
     1,
     '["accounts.manage","users.manage","roles.manage","contacts.manage","journals.manage","income.manage","expenses.manage","invoices.manage","bills.manage","reports.view"]'),
    ('bookkeeper',
     'Manages transactions, sales, and purchases',
     1,
     '["contacts.manage","journals.manage","income.manage","expenses.manage","invoices.manage","bills.manage","reports.view"]'),
    ('viewer',
     'Read-only access to reports',
     1,
     '["reports.view"]');

-- Rebuild the users table to drop the CHECK(role IN ('admin','viewer')) constraint.
-- Role validation now happens in the application layer against the roles table.
CREATE TABLE users_new (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    username              TEXT    NOT NULL UNIQUE,
    password              TEXT    NOT NULL,
    full_name             TEXT    NOT NULL,
    role                  TEXT    NOT NULL,
    is_active             INTEGER NOT NULL DEFAULT 1,
    must_change_password  INTEGER NOT NULL DEFAULT 0,
    created_at            TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at            TEXT    NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO users_new (id, username, password, full_name, role, is_active, must_change_password, created_at, updated_at)
    SELECT id, username, password, full_name, role, is_active, must_change_password, created_at, updated_at
    FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;
