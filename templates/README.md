# Template and HTMX Conventions

Templates are the HTML adapter's presentation layer. Business rules and database access do not belong here.

## Full pages

Full-page responses execute `templates/base.html` with shared navigation, flash, CSRF, and pagination partials. The theme partials (`templates/partials/theme.html`) are parsed alongside them for every page. Page data should be typed when practical.

Pages built on Astabyte UI include `{{template "theme-head" .}}` in `<head>`; today that is the public homepage, and the ERP, parent portal, and invoice print adopt it as they migrate. It loads the stylesheet, Geist fonts, and the Asta runtime, and applies dark mode before first paint. `{{template "theme-toggle" "<label>"}}` renders the light/dark switch; the label is its accessible name in the page's language.

Markup uses Astabyte UI's `asta-*` classes and the tokens in [DESIGN.md](../DESIGN.md). Keep the `data-asta-*` and ARIA attributes from the kit's examples: `asta.js` drives dialogs, menus, tabs, and segmented controls through them.

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
