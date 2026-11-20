# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

- **Staff (primary).** Latasya Trans office team using the ERP in a mixed setting: a bookkeeper at a desk for invoices, bills, journals, and reports; the owner and other staff on phones for quick income/expense entries and status checks. Roles are capability-based (admin, bookkeeper, viewer, plus custom roles). Users are not accounting specialists; the app must make double-entry bookkeeping usable without accounting expertise.
- **Parents (portal).** Families of students on the school shuttle open a per-family invoice link (usually shared over WhatsApp) to see invoices per child, what is still owed, and how to pay by bank transfer. No device or accessibility needs are confirmed for this audience.
- **Public visitors (company profile at `/`).** Prospective customers learning about the company's services and contacting it via WhatsApp.

## Product Purpose

Simple bookkeeping for a small Indonesian transport business (school shuttle, tour agency, inter-city travel). Every transaction produces balanced journal entries, so staff record everyday income, expenses, invoices, and bills through simplified forms while the books stay correct. Success: staff keep accurate books and send/collect invoices without needing an accountant, and parents can see and pay what they owe without calling the office.

## Positioning

Built for one specific operator rather than as generic accounting software: chart of accounts preset for Indonesian transport costs (fuel, tolls, KIR, PKB/STNK, THR), school-shuttle pricing from route distance with sibling and return-only rules, school-calendar-aware recurring invoices, per-family invoice portal links, and the same operations available from the web UI, a JSON API, a Telegram bot, and an MCP server.

## Operating Context

- Staff work in the web app (desktop and mobile), and also record income/expenses, mark invoices sent, and record payments from Telegram with per-user API tokens. AI clients reach the same operations over MCP (OAuth 2.1).
- Invoice links and contact go through WhatsApp; WhatsApp sharing stays in the web UI.
- Parents pay by bank transfer, quoting the transfer reference shown on each invoice so staff can match payments.
- Invoices are printable (`templates/invoices/print.html`).
- School shuttle runs Monday–Saturday; school closures adjust effective school days for billing.

## Capabilities and Constraints

- Modules: invoices, bills, credit notes, income, expenses, manual journals, chart of accounts, contacts (customers/suppliers, routes, pricing, portal codes), company profile and invoice defaults, users/roles, API tokens, audit log, school calendar.
- Reports: Trial Balance, Profit & Loss, Balance Sheet, Cash Flow, General Ledger; dashboard with cash balance, monthly revenue/expenses, outstanding invoices/bills, and financial history charts.
- Stack: Go stdlib (`net/http`, `html/template`), HTMX, Tailwind CSS with Astabyte UI (DaisyUI being phased out), SQLite. No Node, npm, or JS framework; single binary with embedded templates and static assets, deployed via systemd behind Cloudflare Tunnel.
- Currency is IDR (integer rupiah). Dates are local Indonesian business dates.
- Language: staff ERP copy stays in English (confirmed). Parent portal and public company profile are in Bahasa Indonesia.

## Brand Commitments

- Trading name **Latasya Trans** (wordmark "LATASYA TRANS"); legal entity **PT Latansa Asyariyah Linarchadi**. The portal refers to the company as "Latasya Transport"; the staff app is titled "Latasya ERP".
- Service lines: Antar Jemput Sekolah (for Sekolah Plus Latansa students), Agen Wisata, Travel Antar Daerah.
- Stated values on the public page: Keselamatan, Kenyamanan, Ketepatan Waktu.

## Evidence on Hand

- Real service descriptions and values copy in `templates/home/index.html`.
- Company name, phone, and bank details come from the company settings record at runtime, not from templates.
- Fleet (confirmed for the homepage): 1 Toyota Hiace (16 seats), 2 Nissan Evalia (12 seats), 1 Daihatsu Gran Max (12 seats). This is public copy, kept separate from the ERP vehicles table, which tracks costs and school routes.
- No testimonials, customer counts, fleet photos, or logo files exist in the repo (`static/img/` is empty). Do not fabricate them.

## Product Principles

1. Correct books by construction: simplified entry must never bypass double-entry balance.
2. Plain over expert: staff are not accountants, so labels, defaults, and flows explain themselves.
3. Every surface fits its moment: desk-depth for the bookkeeper, quick-entry for staff on phones, zero-friction for parents arriving from WhatsApp.
4. One business truth across channels: web, API, Telegram, and MCP act on the same rules.
5. Small and dependable: one binary, no frontend build chain, nothing that adds operational weight.
