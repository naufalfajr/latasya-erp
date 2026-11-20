# Template and HTMX Conventions

Templates are the HTML adapter's presentation layer. Business rules and database access do not belong here.

## Full pages

Full-page responses execute `templates/base.html` with the shared partials in `templates/partials/` (navigation, sidebar, flash, CSRF, pagination, theme, UI fragments). Page data should be typed when practical.

Signed-in pages get the ERP shell from `base.html`: the kit sidebar on desktop, a top bar and drawer on phones, and `body.latasya`. Pages that do not use `base.html` (the public homepage) include `{{template "theme-head" .}}` in `<head>` and put `latasya` on `<body>`. `theme-head` loads the stylesheet, Geist fonts, and the Asta runtime, and applies dark mode before first paint. `{{template "theme-toggle" "<label>"}}` renders the light/dark switch; the label is its accessible name in the page's language. The login page, MCP consent page, parent portal, and invoice print still use DaisyUI until they migrate.

Link static files with `{{asset "css/app.css"}}`, never a bare `/static/…` path. `asset` adds a content hash (`?v=…`), so a deploy changes the URL. Cloudflare and browsers cache `/static/` for 4 hours, and a bare path can pair new HTML with old CSS.

## Markup: Astabyte UI

Markup uses Astabyte UI's `asta-*` classes and the tokens in [DESIGN.md](../DESIGN.md); Tailwind utilities handle layout (grid, flex, gap, width) and tokens (`text-ink`, `text-ink-2`, `text-mute`, `bg-paper`, `bg-paper-2`). Keep the `data-asta-*` and ARIA attributes from the kit's examples: `asta.js` drives dialogs, menus, tabs, and segmented controls through them. Never use DaisyUI classes on a migrated page.

Copy is English sentence case in the ERP ("New invoice", not "New Invoice"). Update tests that assert on changed copy.

| Need | Markup |
|---|---|
| Page header | `<div class="page-head"><div><h1 class="page-title">…</h1><p class="page-sub">…</p></div><div class="page-actions">buttons</div></div>` |
| Section heading | `<h2 class="section-title">` (or `asta-card-title` inside a card) |
| Button | `<a\|button class="asta-btn asta-btn-{accent\|outline\|ghost} asta-btn-{xs\|sm\|lg}"><span class="asta-btn-label">Label</span></…>`. One `accent` (the primary action) per area; destructive actions use `asta-btn-outline erp-danger` behind a confirm; everything else `outline` or `ghost`. Icon-only: add `asta-btn-icon` and an `aria-label`. |
| Panel | `<section class="asta-card">` with `asta-card-header` > `asta-card-title` / `asta-card-description`, and `asta-card-footer` for form buttons. Never nest cards. |
| Phone list | Below 640px, daily-use lists show two columns: a stacked first cell (link, `text-[12.5px] text-ink-2 sm:hidden` meta line, badge) and the amount. Hide other columns with `hidden sm:table-cell`, and repeat any action they held (open, delete) in the stacked cell. See `templates/invoices/index.html`. |
| Table | `<div class="asta-table-wrap"><div class="asta-table-scroll"><table class="asta-table">`; `th.asta-th` (`asta-th-right` for numbers, `scope="col"`), `tr.asta-tr`, `td.asta-td` (`asta-td-right`, `asta-td-wrap` for long text). Money and counts: `font-mono tabular-nums`. Empty row: `td.asta-td` with `py-10 text-center text-mute`. |
| Field | `<div class="asta-field"><div class="asta-field-head"><label class="asta-label" for="x">Label<span class="asta-label-required" aria-hidden="true">*</span></label></div>` control `<p class="asta-hint">` or `<p class="asta-error" role="alert">`</div>`. Mark invalid controls `aria-invalid="true"`. |
| Text input | `<label class="asta-input [asta-input-sm]"><input class="asta-input-control" …></label>` |
| Select | `<div class="asta-select [asta-select-sm]"><select class="asta-select-control">…</select>{{template "select-chevron"}}</div>` |
| Textarea | `<div class="asta-textarea"><textarea class="asta-textarea-control">…</textarea></div>` |
| Checkbox | `<label class="asta-checkbox"><span class="asta-checkbox-box"><input type="checkbox" class="asta-checkbox-input">{{template "check-icon"}}</span><span><span class="asta-checkbox-label">Label</span></span></label>`; a bare `asta-checkbox-box` works in tables. |
| Status badge | `{{template "status-badge" .Status}}` for document statuses; other labels `<span class="asta-badge asta-badge-sm">`. |
| Alert | `<div class="asta-alert asta-alert-{success\|warning\|neutral\|danger}" role="status\|alert">{{template "alert-icon"}}<div class="asta-alert-body"><p class="asta-alert-title">…</p><p class="asta-alert-text">…</p></div></div>`; `danger` (Latasya extension) for errors. |
| Stat | `<div class="asta-stat-card"><p class="asta-stat-card-label">…</p><p class="asta-stat-card-value font-mono text-[22px]">…</p></div>` (mono for numbers; drop it for a word like "Unavailable") |
| Filter / period links | `<nav class="asta-segmented asta-segmented-sm" aria-label="…"><a class="asta-segmented-item" aria-current="page">…</a></nav>` |
| Record details | `<dl class="erp-dl"><div><dt>Label</dt><dd>Value</dd></div></dl>` |
| Inline link | `<a class="erp-link">` |

Colour: navy (`brand`) is the only accent. Amounts never turn green or red; a negative amount reads by its minus sign. Red appears only in `erp-danger` buttons, `asta-error`, `asta-alert-danger`, and the danger/success/warning badges.

Line-row partials (`*/line_partial.html`) are also rendered standalone for HTMX, sometimes without the shared partials, so they inline their SVGs instead of calling `select-chevron` or `check-icon`.

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
