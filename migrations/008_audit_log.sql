-- Audit log: one row per business-meaningful mutation or security event.
-- Actor columns are denormalized snapshots (username, target_label) so rows
-- remain legible after the referenced user/entity is renamed or deleted;
-- deliberately no FK to users(id) or any domain table.
CREATE TABLE IF NOT EXISTS audit_log (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    request_id     TEXT,
    actor_id       INTEGER,
    actor_username TEXT,
    action         TEXT    NOT NULL,
    target_type    TEXT,
    target_id      INTEGER,
    target_label   TEXT,
    result         TEXT    NOT NULL DEFAULT 'ok' CHECK (result IN ('ok', 'fail')),
    error_message  TEXT,
    ip             TEXT,
    metadata       TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_log_occurred_at ON audit_log(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_actor       ON audit_log(actor_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_target      ON audit_log(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_action      ON audit_log(action, occurred_at DESC);
