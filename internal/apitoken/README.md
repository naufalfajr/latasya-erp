# API token module

This module owns long-lived bearer credentials used by MCP, bots, and other API
clients. Plaintext is returned exactly once, only a SHA-256 hash is stored, and
requested scopes must be a subset of the owner's current capabilities.

Cookie-versus-bearer restrictions are transport concerns. Ownership, expiry,
revocation, hashing, and audit metadata are enforced here.
