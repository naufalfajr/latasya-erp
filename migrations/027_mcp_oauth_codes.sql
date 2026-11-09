-- mcp_oauth_codes: OAuth 2.1 authorization codes for the remote MCP
-- endpoint. Codes are 32 random bytes stored only as SHA-256, single-use,
-- and live 5 minutes; they are swept lazily by the oauthas module.
CREATE TABLE mcp_oauth_codes (
    code_hash      TEXT PRIMARY KEY,      -- sha256 hex; plaintext never stored
    user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    username       TEXT NOT NULL,
    client_id      TEXT NOT NULL,
    redirect_uri   TEXT NOT NULL,         -- frozen at authorize time; matched at exchange
    code_challenge TEXT NOT NULL,         -- base64url(sha256(verifier)), S256 only
    scopes         TEXT NOT NULL,         -- JSON array, frozen subset of capabilities
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at     TEXT NOT NULL,         -- +5 minutes
    used_at        TEXT
);
CREATE INDEX idx_mcp_oauth_codes_expires ON mcp_oauth_codes(expires_at);
