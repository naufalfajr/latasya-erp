---
version: 1
slug: "templates-base-html"
primary_target: "templates/base.html"
related_targets: []
---

# Staff ERP (signed-in screens)

Mode: Operate. English, desk and phone. Every signed-in screen rendered through templates/base.html.

Audience: Latasya Trans office staff (bookkeeper at a desk, owner and staff on phones), not accountants.
Task: record income, expenses, journals; create, send and collect invoices; receive and pay bills; credit notes; contacts; reports; settings.
Constraints: same information as before the migration (tables, charts, fields, actions); no handler or data changes; behaviour of every hx-* interaction preserved. Asta UI kit only, navy the one accent, status badges per DESIGN.md mapping, mono charts. Light and dark with the toggle in the sidebar foot and the phone top bar. Login and MCP consent share the signed-out branch of base.html (see DESIGN.md Layout).

## Direction contract

THESIS: The ledger at work: a quiet kit sidebar and dense kit tables where the numbers are the loudest thing on screen. Refuses the coloured-dashboard default (green/red amounts, rainbow quick actions, card soup).
OWN-WORLD: Asta UI kit on paper: paper-2 sidebar, kit cards with 10% inset outlines, paper-2 table headers, Geist 13.5px rows, Geist Mono tabular amounts, one navy accent button per area, status only in kit badges.
STORY: A staff member lands on the dashboard, reads cash, revenue, expenses and outstanding totals at a glance, follows the sidebar to the record they need, and acts with one obvious primary button.
FIRST VIEWPORT: Desktop: 248px sidebar left (grouped links, current section indicated, user and theme toggle at the foot); page header with a 24px title left and actions right (one accent); then the screen's primary data (stat row, filter segment, table or form card). Phone: 56px top bar (menu, title, toggle) opening a left drawer with the same sidebar; content stacks, wide tables scroll inside their wrap.
FORM: Extension of the established Latasya/Asta world onto the existing ERP information architecture. seed: none (pinned) — user: "i want to revamp all the UI on this project to use https://github.com/astabyteidn/asta-ui" and "the ERP stay simple, keep the information shown like the table, graph etc."
FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

Unresolved: none.
