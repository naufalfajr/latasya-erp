# Contributing

## Workflow

- Track work in GitHub Issues on `naufalfajr/latasya-erp`.
- Do not commit or push changes until the repository owner approves it.
- Keep code comments rare and useful; comments should normally be no more than two lines.
- Keep unrelated working-tree changes intact.

## Verification

Before requesting review:

1. Run `gofmt` on changed Go files.
2. Run targeted tests for the affected module.
3. Run `go test ./...`.
4. Review the complete diff for behavior, security, and contract compatibility.

Changes to HTTP contracts must also preserve `api/openapi.yaml` and its contract tests. Changes to critical browser flows should be verified with Playwright before deployment.

## Architecture

Latasya ERP is a Go monolith with HTML/HTMX and JSON HTTP adapters over shared business modules. SQLite is the current persistence implementation. Do not add persistence interfaces until a second implementation is genuinely required.

See [MODULES.md](MODULES.md) for the module index and documentation links.
