# OAuth authorization server

`oauthas` is the ERP's first-party OAuth 2.1 authorization server for the
remote MCP endpoint ([`internal/mcpserver`](../mcpserver/README.md)). It mints
ordinary `lat_` API tokens through the [`apitoken`](../apitoken/README.md)
module, so OAuth logins show up, scope down, and revoke exactly like tokens
created at `/settings/api-tokens`.

## Flow

One grant only: authorization code + PKCE `S256`. Public clients only
(`token_endpoint_auth_method=none`); no client secrets, no refresh tokens.

| Route | Owner | Purpose |
|---|---|---|
| `GET /.well-known/oauth-protected-resource` | `internal/api/v1/oauthas` | RFC 9728 metadata, linked from every bearer 401 |
| `GET /.well-known/oauth-authorization-server` | `internal/api/v1/oauthas` | RFC 8414 metadata |
| `POST /oauth/register` | `internal/api/v1/oauthas` | RFC 7591 dynamic client registration |
| `GET`/`POST /oauth/authorize` | `internal/handler/oauth.go` | consent page behind the browser session |
| `POST /oauth/token` | `internal/api/v1/oauthas` | code + verifier exchange for a `lat_` token |

- **Registration** returns a random `client_id` and persists nothing. Any
  `client_id` is accepted at authorize time: the resource owner's consent is
  the trust decision.
- **Consent** sits on the outer mux with its own session lookup and
  constant-time CSRF check, so an unauthenticated authorize bounces through
  login with `next=/oauth/authorize` (the only `next` the MCP allowlist
  accepts). The scope grid offers only the user's current capabilities. Bad
  authorize params render an error page; they never redirect to an
  unvalidated URI.
- **Codes** are 32 random bytes, stored as SHA-256 in `mcp_oauth_codes`,
  single-use, 5-minute TTL, lazily swept.
- **Exchange** requires the exact authorize-time redirect URI and re-derives
  the consenter's capabilities from the DB. A role shrunk in between fails
  with `invalid_scope`. Tokens live 30 days; renewal is a new consent (one
  click with a live browser session).
- `/oauth/token` and `/oauth/register` are rate-limited to 10/min per IP each.

## Redirect URIs

`validRedirectURI` in `internal/handler/oauth.go` accepts:

- loopback `http://127.0.0.1:*` or `http://localhost:*` (native clients,
  RFC 8252; `::1` excluded because CSP cannot express it),
- same-origin URIs,
- exact-match callbacks of hosted clients (`hostedMCPRedirects`): claude.ai,
  claude.com, ChatGPT, Cursor, Gemini Enterprise.

Chromium applies CSP `form-action` to the redirect after the consent POST, so
every allowed redirect origin must also appear in `form-action` in
`internal/handler/security.go`. Adding a hosted client means editing both.
