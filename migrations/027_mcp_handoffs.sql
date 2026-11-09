-- mcp_handoffs: device-code login handoffs for the local MCP client
-- (cmd/mcp-server). The browser approves a scope decision; the client's next
-- poll mints the token and deletes the row. Rows live at most 10 minutes and
-- are swept lazily by the mcpauth module on every Touch/Consume.
CREATE TABLE mcp_handoffs (
    device_code_hash  TEXT PRIMARY KEY,      -- sha256 hex; plaintext never stored
    user_code         TEXT NOT NULL UNIQUE,  -- 8-char code shown in the consent URL
    client            TEXT NOT NULL,         -- e.g. "latasya-mcp/1.0 (darwin/arm64)"
    status            TEXT NOT NULL,         -- pending | approved | denied
    approved_user_id  INTEGER REFERENCES users(id) ON DELETE CASCADE,
    approved_username TEXT NOT NULL DEFAULT '',
    scopes            TEXT NOT NULL DEFAULT '[]',
    token_expires_at  TEXT,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at        TEXT NOT NULL          -- created_at + 10 minutes
);
CREATE INDEX idx_mcp_handoffs_expires ON mcp_handoffs(expires_at);
