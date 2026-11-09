# MCP Server

A Model Context Protocol server for Latasya ERP, built as a local binary the
AI client runs (Claude Desktop, Claude Code, Codex, …). It speaks MCP
2025-06-18 over stdio and calls the ERP's existing JSON API (`/api/v1`) with
the user's own Bearer token — the same surface the Telegram bot uses. It
imports no ERP business logic: every rule, capability gate, and audit row is
the server's.

## How to connect

1. Get the binary: download a CI artifact (`latasya-mcp-<sha>`, job
   `mcp-client`) or build one — `make mcp-release`.
2. Log in once:

   ```
   latasya-mcp login --base-url https://erp.example.com
   ```

   Your browser opens the ERP's consent page (normal ERP login applies).
   Tick the capabilities to grant — only your own — pick an expiry
   (default 90 days), approve. The terminal prints "Connected as …".
3. Register the command with your AI client, e.g. for Claude Desktop:

   ```json
   { "mcpServers": { "latasya": { "command": "/usr/local/bin/latasya-mcp" } } }
   ```

**Remote alternative (no binary):** the ERP also serves MCP directly at
`https://<erp>/mcp` with OAuth 2.1 browser login — add it as a remote MCP
server URL in clients that support them (Claude Desktop connectors, Cursor,
…). This stdio binary remains the offline-capable option; both use the same
generated tools and the same tokens.

Each person connects with their **own** token, so every tool call is
authorized and audited as that ERP user (`audit_log.actor_token_id`).

## Commands

| Command | Does |
|---|---|
| `login --base-url <url>` | Device-code browser login; saves the token |
| `status` | Who you are connected as, capabilities, days to expiry |
| `logout` | Self-revokes the token server-side and wipes the config |
| `serve` (default) | MCP over stdio — what AI clients launch |

## Tools

77 tools, one per API operation, named by its `operationId` (`listInvoices`,
`createJournal`, `recordInvoicePayment`, …). The table is **generated** from
`api/openapi.yaml` (`make mcp-tools`) into `internal/mcptools/tools_gen.go`;
CI fails if it is stale or an operation is unclassified. `tools/list` shows
only what your token's scopes allow — the server re-enforces every call
regardless.

`getInvoicePDF` returns the document as a base64 resource (≤ 2 MiB). Amounts
are integer IDR strings, quantities `"1.50"`-style decimals, dates
`YYYY-MM-DD`.

## Safety behavior

- **HTTPS only.** The client refuses to send the device code or token over
  plain HTTP outside localhost (`LATASYA_MCP_ALLOW_HTTP=true` overrides for
  dev). Enforced on login, serve, and logout.
- **Retries never duplicate money.** Every mutation derives a deterministic
  `Idempotency-Key` from (token, operation, arguments): an identical retry
  replays the original response verbatim. Pass `_idempotency_key` yourself
  when you want deliberate cross-retry dedup.
- **Token hygiene.** Config at `os.UserConfigDir()/latasya/mcp.json`, mode
  0600, atomic writes; token redacted from every error string; `LATASYA_MCP_TOKEN`
  supports headless/CI without a config file. Revocation is immediate: the
  settings UI (any `users.manage` holder), `latasya-mcp logout`, or the
  `revokeCurrentToken` tool.
- **Bounded responses.** Text results truncate at 64 KiB; response bodies cap
  at 4 MiB; stdin lines cap at 4 MiB (oversize gets a JSON-RPC error, not a
  hang).

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `LATASYA_API_URL` | `http://127.0.0.1:8080` | ERP base URL |
| `LATASYA_MCP_CONFIG` | `<user config dir>/latasya/mcp.json` | Credential file (0600) |
| `LATASYA_MCP_TOKEN` | — | Headless token override |
| `LATASYA_MCP_ALLOW_HTTP` | — | `true` to allow plain HTTP off-loopback (dev) |

## Development

```bash
make run                  # terminal 1: ERP on :8080
make run-mcp              # terminal 2: the client against it
make mcp-tools            # regenerate the tool table after spec changes
go test ./cmd/mcp-server/ # incl. e2e against the in-process ERP
```

The e2e suite covers the device login through the real endpoint, tools/list
scoping, server-side capability enforcement, idempotent replay, and the
stdio protocol loop. Architecture and decision log: `MCP_PLAN.md`.
