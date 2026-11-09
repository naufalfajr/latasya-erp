# MCP login module

This module owns the device-code login handoff between the browser (consent
page) and the local MCP client (`cmd/mcp-server`). The client polls with a
hashed device code; the approving user's scope decision is stored — never the
token. The token is minted at consume time via the apitoken module, with the
approver's capabilities re-derived from the database at that moment, so a role
change in the approve/poll window cannot smuggle scopes through.

Invariants:

- Plaintext tokens exist only in the final poll response; rows store the
  device code as SHA-256 only.
- Mint-at-consume with delete-before-mint: a failed mint consumes the
  handoff, so login restarts cleanly — there is no half-issued state.
- Token names carry a random suffix: `api_tokens` enforces
  `UNIQUE(user_id, name)` and revocation is soft-only, so a same-day
  re-login must not collide.
- Rows live at most 10 minutes and are swept lazily by the traffic that
  creates them (Touch/Consume/Lookup) — no background goroutine.

The JSON transport lives in `internal/api/v1/mcpauth` (the rate-limited
public poll endpoint); the consent page in `internal/handler/mcp.go`.
Design and decision log: `MCP_PLAN.md`.
