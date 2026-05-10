-- api_tokens: scoped Bearer tokens for bots/MCP/Telegram integrations
CREATE TABLE api_tokens (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    token_prefix  TEXT NOT NULL,
    token_hash    TEXT NOT NULL UNIQUE,
    scopes        TEXT NOT NULL DEFAULT '[]',
    expires_at    TEXT,
    last_used_at  TEXT,
    revoked_at    TEXT,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, name)
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_api_tokens_hash ON api_tokens(token_hash) WHERE revoked_at IS NULL;

-- idempotency_keys: replay protection for financial mutations
CREATE TABLE idempotency_keys (
    key             TEXT NOT NULL,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_hash    TEXT NOT NULL,
    response_status INTEGER NOT NULL,
    response_body   BLOB NOT NULL,
    expires_at      TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (key, user_id)
);
CREATE INDEX idx_idem_expires ON idempotency_keys(expires_at);

-- Add actor_token_id to audit_log for Bearer-token request attribution
ALTER TABLE audit_log ADD COLUMN actor_token_id INTEGER;
