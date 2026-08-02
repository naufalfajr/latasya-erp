# Template and HTMX Conventions

Templates are the HTML adapter's presentation layer. Business rules and database access do not belong here.

## Full pages

Full-page responses execute `templates/base.html` with shared navigation, flash, CSRF, and pagination partials. Page data should be typed when practical.

## HTMX fragments

Handlers return a fragment only when `HX-Target` matches that fragment's stable
outer ID. `HX-Request` alone is insufficient because boosted sidebar navigation
also sets it and requires the complete application shell.

Each fragment must have a stable target ID, document its swap behavior, and remain valid when rendered independently. Mutation fragments must preserve CSRF and authorization behavior.
# Template contracts

List-page HTMX requests return independently renderable fragments with stable
outer IDs: `account-table`, `contact-table`, and `journal-table`. Their controls
replace the matching outer element directly; they do not extract content from a
full-page response.
