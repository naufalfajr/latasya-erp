# Remote MCP server

`mcpserver` serves the ERP's [MCP](https://modelcontextprotocol.io) endpoint:
stateless streamable-HTTP JSON-RPC at `POST /mcp` (protocol `2025-06-18`,
also accepts `2025-03-26`; JSON responses only, no SSE). AI clients add
`https://<host>/mcp` as a remote MCP server and log in through the OAuth flow
in [`internal/oauthas`](../oauthas/README.md).

## Protocol shell, not a second API

Every `tools/call` is replayed through the `/api/v1` handler tree with the
caller's `Authorization` header. Authentication, scope intersection with the
caller's current role, capability gates, idempotency, rate limiting, and audit
are therefore the JSON API's own; this package owns only the protocol. Domain
handlers know nothing about MCP.

## Tools

- One tool per OpenAPI `operationId`, named verbatim. The table lives in
  [`internal/mcptools/tools_gen.go`](../mcptools/tools_gen.go), generated from
  `api/openapi.yaml` by `cmd/mcp-gen` (`make mcp-tools`). CI regenerates it and
  fails on any diff.
- `internal/mcptools/coverage_test.go` requires every spec operation to be
  generated or listed in `excludedReasons` (`cmd/mcp-gen/main.go`). Excluded: `login`, `logout`,
  `getCSRFToken` (cookie semantics; passwords never enter LLM context),
  `getOpenAPISpec`, `listCapabilities` (no ERP data), `createAPIToken`,
  `revokeAPIToken` (a token must never spawn a token).
- Path and path-item params become required properties; query params become
  optional properties; the request body is a `body` property.
- Annotations: `GET` read-only; `PUT` idempotent; `DELETE` and
  `delete*`/`void*`/`bulkDelete*` destructive.
- `getInvoicePDF` returns a base64 resource block (capped at 2 MiB).
- Mutations send a deterministic `Idempotency-Key`
  (`SHA-256(token_id ‖ operationId ‖ canonical args)`), so a retried call
  replays the original response instead of double-posting. An optional
  `_idempotency_key` argument overrides it.

Tools stay 1:1 with operations. A multi-step flow whose partial failure leaves
meaningful state becomes an API endpoint (one transaction, one audit row), then
arrives here through codegen. Lookups like "name to contact_id" stay with the
model.

## Security

- `tools/list` is filtered by the token's scopes. A tool the caller lacks and a
  nonexistent tool both return `-32602 unknown tool`, so there is no capability
  oracle.
- An `Origin` header, when present, must match the request host, and
  `Content-Type` must be `application/json`. Cross-site browser POSTs cannot
  satisfy both; bearer clients are unaffected.
- Unauthenticated calls get a 401 with an RFC 9728 `WWW-Authenticate` header
  pointing at the protected-resource metadata, which is how clients discover
  the OAuth flow.
- `/mcp` and `/api/v1` share one bearer rate limiter (120/min per credential).

## Headless use

Any client that allows custom headers can skip OAuth: create a token at
`/settings/api-tokens` and send `Authorization: Bearer lat_...`.

A local stdio client existed and was removed (no client in use needs it). To
bring it back, revert the commit that removed `cmd/mcp-server` and regenerate.
