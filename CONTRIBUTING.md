# Contributing

## Workflow

- Track work in GitHub Issues on `naufalfajr/latasya-erp`.
- Do not commit or push changes until the repository owner approves it.
- Keep code comments rare and useful; comments should normally be no more than two lines.
- Keep unrelated working-tree changes intact.

## Pull requests

`main` is protected by a repository ruleset:

- Changes land through a pull request with at least one approving review, and every review thread resolved. Code-owner review is also required; owners are listed in [`.github/CODEOWNERS`](.github/CODEOWNERS).
- `main` cannot be deleted or force-pushed.
- The `lint` and `test` CI checks must pass on a branch that is up to date with `main`.
- History stays linear: merge with squash (the default) or rebase. Merged branches are deleted automatically.
- Copilot reviews every push to an open pull request.

## Verification

Before requesting review:

1. Run `gofmt` on changed Go files.
2. Run targeted tests for the affected module.
3. Run `go test ./...`.
4. Review the complete diff for behavior, security, and contract compatibility.

Changes to HTTP contracts must also preserve `api/openapi.yaml` and its contract tests. After changing classes in templates, run `make css` and commit the rebuilt `static/css/app.css`. Changes to critical browser flows should be verified with Playwright before deployment.

## Architecture

Latasya ERP is a Go monolith with HTML/HTMX and JSON HTTP adapters over shared business modules. SQLite is the current persistence implementation. Do not add persistence interfaces until a second implementation is genuinely required.

See [MODULES.md](MODULES.md) for the module index and documentation links.

## Design

UI follows [DESIGN.md](DESIGN.md) (tokens, components, rules) and [PRODUCT.md](PRODUCT.md) (users, voice, product facts). The component kit is Astabyte UI, vendored unmodified in `static/css/asta/`; upgrade it by replacing those files with a newer pinned release, never by editing them.
