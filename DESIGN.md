---
name: Latasya
description: One quiet paper-and-ink system for Latasya Trans, from the public profile to the ERP, the parent portal and the printed invoice.
colors:
  brand: "#1b2a55"
  brand-dark: "#5a72c0"
  paper: "#fcfcfb"
  paper-2: "#f2f2f0"
  ink: "#0a0a0a"
  ink-2: "#3b3b3a"
  mute: "#6f6f6b"
  paper-dark: "#0b0b0c"
  paper-2-dark: "#151516"
  ink-dark: "#f2f2f0"
  ink-2-dark: "#cfcfcc"
  mute-dark: "#8a8a8e"
  on-brand: "#ffffff"
  status-success: "#30a46c"
  status-warning: "#f5a524"
  status-danger: "#e5484d"
typography:
  display:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "clamp(44px, 4.6vw, 66px)"
    fontWeight: 500
    lineHeight: 0.98
    letterSpacing: "-0.045em"
  headline:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "clamp(32px, 4vw, 48px)"
    fontWeight: 500
    lineHeight: 1
    letterSpacing: "-0.045em"
  title:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "30px"
    fontWeight: 500
    lineHeight: 1.05
    letterSpacing: "-0.04em"
  body-lg:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "18px"
    fontWeight: 400
    lineHeight: 1.7
  body:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.7
  control:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 500
    letterSpacing: "-0.01em"
  page-title:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "24px"
    fontWeight: 500
    lineHeight: 1.15
    letterSpacing: "-0.03em"
  stat:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "22px"
    fontWeight: 500
    lineHeight: 1
    letterSpacing: "-0.04em"
  card-title:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 500
    letterSpacing: "-0.01em"
  small:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "Geist Mono, ui-monospace, SFMono-Regular, monospace"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.45
    fontFeature: "\"tnum\" 1"
rounded:
  sm: "6px"
  md: "8px"
  lg: "10px"
  card: "12px"
  surface: "14px"
spacing:
  gutter: "clamp(16px, 2.8vw, 40px)"
  stack: "12px"
  surface-pad: "28px"
  panel-pad: "36px"
  column-gap: "64px"
  section: "136px"
components:
  button-primary:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.paper}"
    typography: "{typography.control}"
    rounded: "{rounded.md}"
    padding: "0 16px"
    height: "40px"
  button-accent:
    backgroundColor: "{colors.brand}"
    textColor: "{colors.on-brand}"
    typography: "{typography.control}"
    rounded: "{rounded.md}"
    padding: "0 16px"
    height: "40px"
  button-outline:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    typography: "{typography.control}"
    rounded: "{rounded.md}"
    padding: "0 16px"
    height: "40px"
  button-ghost:
    textColor: "{colors.ink-2}"
    typography: "{typography.control}"
    rounded: "{rounded.sm}"
    padding: "0 12px"
    height: "32px"
  button-on-brand:
    backgroundColor: "{colors.on-brand}"
    textColor: "{colors.brand}"
    rounded: "{rounded.md}"
    padding: "0 20px"
    height: "48px"
  input:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "0 12px"
    height: "40px"
  surface:
    backgroundColor: "{colors.paper-2}"
    textColor: "{colors.ink}"
    rounded: "{rounded.surface}"
    padding: "{spacing.panel-pad}"
  card:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.card}"
    padding: "20px"
  segmented:
    backgroundColor: "{colors.paper-2}"
    textColor: "{colors.mute}"
    rounded: "{rounded.lg}"
    padding: "4px"
  badge-success:
    textColor: "{colors.status-success}"
    rounded: "{rounded.sm}"
    padding: "0 10px"
    height: "24px"
  badge-warning:
    textColor: "{colors.status-warning}"
    rounded: "{rounded.sm}"
    padding: "0 10px"
    height: "24px"
  badge-danger:
    textColor: "{colors.status-danger}"
    rounded: "{rounded.sm}"
    padding: "0 10px"
    height: "24px"
---

# Design System: Latasya

## Overview

**Creative North Star: "The Working Ledger"**

Paper, ink, and one navy. Every Latasya surface (the public profile, the staff ERP, the parent invoice portal, login, and the printed invoice) is built from the vendored Astabyte UI 0.9.1 kit, unmodified, with three token additions: the kit's `--brand` becomes Latasya navy, `--mute` is darkened to pass contrast at the ERP's small sizes, and `--danger` names the kit's own red (#e5484d) for errors and destructive text. DaisyUI is gone from the build. Nothing is decorated. Hierarchy comes from Geist set large and tight, from Geist Mono for the facts, and from the two-step tonal shift between paper and raised paper.

The system is typographic. There are no photos, illustrations or stock imagery anywhere; the only drawings allowed are ones generated from product facts, like the fleet seat plans. Light is the default stage, Asta's dark stage is the alternate, and every surface carries the toggle except the printed invoice, which is always light. The device setting decides first paint, a saved choice wins after that, and the `theme-head` partial applies `.dark` before paint so nothing flashes.

The homepage is persuasive and roomy; the ERP is operational and dense. Both use the same tokens, components, and rules. Only spacing and type size change with the mode.

**Key Characteristics:**
- One kit (Asta UI `asta-*` classes), one accent (navy), one family pair (Geist + Geist Mono).
- Depth from tone (paper → paper-2) and soft kit shadows; outlines are inset 1px shadows, never CSS borders.
- Mono for amounts, ids and codes (invoice numbers, references, account codes) and stat values; dates, field labels and names stay in Geist with tabular figures.
- Sentence case everywhere; short, plain copy (Bahasa Indonesia on public and parent surfaces, English in the ERP).
- Light and dark are equals: every rule below must hold on both stages.

## Colors

A near-neutral paper-and-ink scale from the Asta kit, cut by a single deep navy.

### Primary
- **Latasya Navy** (`brand`): the only accent. Accent buttons, the active segment, the hero emphasis word, the pass stub and the contact band, focus rings (the kit's own rings on `asta-*` controls; a 2px outline at 2px offset on everything else), and the selection tint (22% mix). Applied by overriding `--brand` on `:root`; never by hard-coding navy into component CSS.
- **Lifted Navy** (`brand-dark`): the same accent on the dark stage, lifted so white text on it keeps 4.56:1. As text on dark paper it reaches only ~4.3:1, so in dark mode navy text appears at display and headline sizes only.
- **On-Brand White** (`on-brand`): text on navy fills, and the fill of the one button that sits on a navy panel. That button keeps navy as its label colour in both themes, through the fixed `--navy` token (#1b2a55) rather than the theme-lifted `--brand`.

### Neutral
- **Paper** (`paper` / `paper-dark`): page ground, also painted on `html` so overscroll stays on paper.
- **Raised Paper** (`paper-2` / `paper-2-dark`): every raised surface (service rows, the pass, seat plans, segmented tracks, hover fills).
- **Ink** (`ink` / `ink-dark`): headings, values, primary text; also the default (primary) button fill.
- **Soft Ink** (`ink-2` / `ink-2-dark`): body copy, ledes, mono labels. 10:1 on raised paper.
- **Mute** (`mute` / `mute-dark`): secondary text: table headers, hints, captions, timestamps, placeholders, icon tint. Moved from the kit's values (#8a8a87 light, #7d7d80 dark) to #6f6f6b light and #8a8a8e dark so 12px text keeps 4.5:1 on both paper and raised paper.

### Status (kit-owned)
- **Success / Warning / Danger** (`status-*`): the kit's own badge hues, used only through `asta-badge-success / -warning / -danger` for record status in the ERP and portal, and danger for errors and destructive text (`asta-error`, `asta-alert-danger`, `erp-danger`). There is no filled red button. They are signals, not accents.
- **Document status mapping** (`{{template "status-badge"}}`): draft grey, sent / received / issued / unpaid navy (`asta-badge-accent`), partial amber, paid / applied green, overdue red, cancelled / void grey outline; any other status falls back to grey. Labels are sentence case through `humanize`. The portal shows the same tones with Indonesian labels: Belum lunas (navy, also for a legacy overdue row, so parents never see "Terlambat"), Sebagian, Lunas, Dibatalkan; any other status shows navy with its humanized name.

### Named Rules
**The One Navy Rule.** Navy is the only hue with a voice. No second accent; the retired gold does not return. Status colours appear only inside status badges.

**The Mono Chart Rule.** Charts and amounts carry no status colour: revenue is `brand`, expenses are textured neutral grey, the net and closing-cash line is `ink`, and a loss shows as a minus sign or label, never as red. Chart colours are CSS variables, so charts follow the theme without a redraw.

## Typography

**Display Font:** Geist (with ui-sans-serif, system-ui)
**Body Font:** Geist
**Label/Mono Font:** Geist Mono (with ui-monospace, SFMono-Regular)

**Character:** A geometric grotesk at medium weight, tracked tight, beside a quiet mono that reads like a dispatcher's slip. Loaded from Google Fonts in `theme-head` (Geist 400/500/600, Geist Mono 400/500).

### Hierarchy
- **Display** (500, clamp(44px, 4.6vw, 66px), 0.98, -0.045em): one per page, the hero line. `text-wrap: balance`. One phrase may carry navy for emphasis (upright, never italic).
- **Headline** (500, clamp(32px, 4vw, 48px), 1, -0.045em): section heads.
- **Title** (500, 30px, 1.05, -0.04em): item heads such as a service row; value terms use the same 30px title step under a 48px section heading.
- **Body** (400, 16px, 1.7, `ink-2`): running copy, max ~460-520px wide, `text-wrap: pretty`. Ledes go to 18px.
- **Control** (500, 14px, -0.01em): buttons and segments (13px small, 15px large), from the kit.
- **Label** (Geist Mono 400, 11-12px, `ink-2`, sentence case): fact terms on public pages (service rows, pass, contact band). In the ERP, field labels are the kit's Geist `asta-label` and record terms are Geist 12px `mute`. Numbers anywhere get `tabular-nums`.
- **ERP steps** (fixed, not fluid): page title 24px / 500 / -0.03em; stat value 22px; card and section titles 15-16px / 500; table and control text 13-13.5px (kit); small meta 12-13px in `mute`.

### Named Rules
**The Two Families Rule.** Geist and Geist Mono, nothing else: no serif, no system display face, no icon font.

**The Sentence Case Rule.** Headings, labels and buttons are sentence case. No all-caps tracking, no eyebrow line above a heading.

## Layout

Content sits in a 1320px max container with a fluid gutter (`spacing.gutter`). Public pages use two-column splits (roughly 0.9fr / 1.1fr, 64px gap) for heading-plus-content sections, and full-width stacked rows (12px apart) for lists of equals, so items read as rows rather than a grid of identical cards. Sections are separated by space alone (136px, 96px below 900px); there are no rules or dividers between them.

Breakpoints: 1080px (multi-column rows fold to two), 900px (splits stack), 720px (nav collapses, raised panels drop to 20-24px padding, side panels become bottom bars). Phones are the primary public viewport.

The header is sticky at 56px: wordmark, nav as small ghost buttons, theme toggle, one accent action. It is transparent at the top and fills with paper over the first 48px of scroll (scroll-driven animation, static fill where unsupported).

ERP and portal screens keep the same tokens and kit controls at operational density: kit cards, tables and 32-40px controls, with the large display sizes reserved for public pages.

**ERP shell** (`templates/base.html`): the kit sidebar (`asta-sidebar`, 248px, paper-2) sticks at full viewport height on desktop, its link list scrolling on its own, with the user, theme toggle and log out at the foot. Below 1024px it becomes a 56px top bar (menu, title, toggle) and a left `asta-drawer` holding the same sidebar. Content fills the width beside the sidebar (32px / 40px padding, 24px / 16px on phones). Every screen opens with a page header: title (and optional subtitle) on the left, actions on the right with one accent button.

**Signed-out pages** (login and MCP consent, the signed-out branch of `templates/base.html`): a 56px bar with the "Latasya ERP" mark and the theme toggle, over one kit card centred in the remaining height.

**Parent portal** (`templates/portal/index.html`, Bahasa Indonesia): the same bar with the company name and a toggle labelled "Ganti mode gelap", over a 640px column of kit cards 12px apart. Invoice facts are a compact `erp-dl` that sits side by side on phones. Values a parent copies (bank account, transfer note) sit on raised paper with a ghost "Salin" button. Each invoice's one accent action is "Konfirmasi sudah bayar"; "Unduh PDF" is outline.

**Print** (`templates/invoices/print.html`): paper, so always the light stage, with no toggle and no theme script. Geist and the kit tokens, the word "Invoice" in navy, Geist Mono for numbers and amounts, raised-paper table head and notes.

## Elevation & Depth

Tonal first: raised paper on paper is the main signal of a raised region. Shadows come from the kit and stay soft: buttons carry a faint inset highlight plus a short drop, the active segment pill floats on a small blur, and outlines are 1px inset shadows mixed from ink (6-14%, 24% on hover). One signature panel per page may lift further with a long soft shadow.

### Shadow Vocabulary
- **Inset outline** (`box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ink) 14%, transparent)`): fields and outline buttons; 6-10% for cards, tracks and plans.
- **Kit button** (`box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.18), 0 1px 2px rgb(0 0 0 / 0.16), 0 6px 14px -8px rgb(0 0 0 / 0.45)`): the solid ink button.
- **Signature lift** (`box-shadow: 0 1px 2px rgb(0 0 0 / 0.04), 0 24px 48px -32px rgb(0 0 0 / 0.35)`): the single hero panel (the travel pass). One per page.
- **Segment pill / current link** (`box-shadow: 0 1px 2px rgb(0 0 0 / 0.08), 0 3px 10px -4px rgb(0 0 0 / 0.18), inset 0 0 0 1px color-mix(in srgb, var(--ink) 8%, transparent)`): the kit's checked segment, reused for the current filter link and (lighter, `0 1px 2px rgb(0 0 0 / 0.06)`) the current sidebar link.
- **Focus** (`outline: 2px solid var(--brand); outline-offset: 2px`, white on navy panels): every focusable element the kit does not ring itself. Kit controls (buttons, fields, checkboxes, sidebar links) keep their own navy ring or inset shadow, so the two never stack. Filter links (`a.asta-segmented-item`) take the outline, because the kit rings a segment only through a focused radio inside it.

### Named Rules
**The Inset Outline Rule.** Edges are drawn with inset shadows, not borders, so they follow the radius and never shift layout.

## Shapes

Soft rectangles, nothing pill-shaped except the kit's dots. Small controls 6px, standard controls and fields 8px, large buttons 9px, segmented tracks 10px (7px pills inside), kit cards 12px, raised panels and bands 14px. Data drawings may take their own silhouette: a seat plan is a minibus from above in a 14px raised panel with 6px seats. A cut-out (the pass's ticket perforation, a repeating radial mask in the surface colour) is part of the form language.

## Components

### Buttons
Kit buttons only (`asta-btn` with a `asta-btn-label` span).
- **Shape:** 8px at 40px height, 6px at 32px (small), 9px at 48px (large).
- **Primary (ink):** solid ink with paper text and the kit button shadow; hover mixes 84% ink toward paper.
- **Accent:** navy fill, white text; hover darkens 12%. The one action per view (for example "Chat WhatsApp").
- **Outline:** paper fill with an inset 14% ink outline; hover raises to paper-2 and a 24% outline. Secondary per-item actions.
- **Ghost:** transparent, `ink-2` text, paper-2 on hover. Nav links and the theme toggle (small icon button, inline 16px stroke SVG sun/moon).
- **On brand:** white fill with navy text on navy panels; hover tints 8% navy.
- **Destructive (`asta-btn-outline erp-danger`):** the outline button with danger text and a danger-tinted hover, always behind a confirm prompt. Never filled red, so navy stays the only filled colour.
- **States:** 200ms ease-out on colour and shadow, press scales to 0.97, focus ring in navy, disabled at 45% opacity.

### Badges (status)
- **Style:** 24px tall, 6px radius, medium 12px text, a 12% tint of the status hue with the text mixed 80% toward ink. Document statuses go through `{{template "status-badge"}}` (see Status above) and always carry the dot. Other labels (type, source, role) are the neutral `asta-badge asta-badge-sm` with a humanized label.

### Cards / Containers
- **Raised panel:** paper-2, 14px radius, 28-36px padding (20-24px on phones), no outline, no shadow (except the one signature panel).
- **Kit card (`asta-card`):** paper, 12px radius, 20px padding, inset 10% ink outline. The ERP's default container.
- **Navy band:** a full raised panel in `brand` with `on-brand` white type and solid white mono labels (90% white drops under 4.5:1 on the dark-stage navy); at most one per page, as the closing contact or call.
- Cards never nest.

### Inputs / Fields
- **Style:** `asta-input`, paper fill, 8px radius, 40px tall, inset 14% ink outline (24% on hover).
- **Focus:** outline turns navy with a 4px 16%-navy halo.
- **Error / Disabled:** red inset outline and halo (kit danger), disabled at 50% opacity.

### Segmented control
`asta-segmented` on native radios: paper-2 track, 10px radius, items in `mute` that turn ink when checked. Inside a navy-led component the pill fills with `brand` and the checked label turns white. Works without JavaScript (`:has(:checked)`); asta.js only animates the pill.

### Navigation
Public: sticky 56px bar, wordmark in Geist 600 16px at -0.02em, links as small ghost buttons, theme toggle, one accent button at the end. Below 720px the links hide and the wordmark, toggle and accent action remain.

ERP: the kit sidebar, grouped under mute labels (Transactions, Sales and purchases, Reports, Settings, Account), 16px stroke icons. The current section (the index and every page under it) gets a static paper indicator with a 8% inset outline and a 1px drop, because pages are server-rendered.

### Tables (ERP)
Kit tables: paper wrap with a 10% inset outline, paper-2 sticky header in 12px `mute`, 13.5px rows, numbers right-aligned in Geist Mono with `tabular-nums`, totals in `font-medium text-ink`. Rows keep the kit hover tint but skip its load fade. Wide tables scroll inside their wrap, never the page.

**Phone lists** (below 640px): every daily-use list (invoices, bills, credit notes, income, expenses, journals, contacts) collapses to two columns. The first cell stacks the record (number or description as a link), a muted meta line (reference · date, customer, route) and the status or type badge; the amount stays right-aligned in mono. Columns hidden on phones use `hidden sm:table-cell`, and any action they held (open, delete) is repeated in the stacked cell so nothing becomes unreachable. Reports, settings tables, the audit log, and record line items keep their columns and scroll inside the wrap.

### Filters and period switches
The segmented track made of links: `asta-segmented-item` anchors, the current one marked `aria-current="page"` and drawn as the kit pill (paper, small drop, 8% inset outline).

### Alerts
Kit alerts, monochrome: success and neutral on paper-2, warning with an inset ink outline. `asta-alert-danger` (Latasya extension) is the warning alert with a 45% danger outline and a danger icon, for errors and imbalances. Long server messages wrap anywhere.

### Definition lists (facts)
The house way to state facts: mono `ink-2` 12px term beside a 14.5-16px ink value, on a 72-110px term column. Used for service facts, contact details and pass facts; the ERP's record headers should follow it.

### Travel pass (signature)
A raised panel split into a body (service segmented control, live message preview) and a 200px navy stub (mono facts and a white submit), joined by a perforated edge. Switching the service swaps the message and facts with CSS only, entering on one 0.42s expo-out rise (`cubic-bezier(0.16, 1, 0.3, 1)`, 6px), switched off under reduced motion. On phones the stub becomes a bottom bar and the perforation runs along its top.

### Seat plan (data drawing)
One minibus outline per vehicle model, drawn from the static fleet list in `internal/handler/home.go` (model, seats, units), captioned with the model name and a mono "N kursi · N unit" line: raised paper body with a 6% inset outline, seats in 80% navy, the driver as a ringed circle at front right (Indonesian minibuses are right-hand drive), passenger rows 1 | aisle | 2 with the aisle on the door side, rear bench of 4. The public fleet is marketing copy, never read from the ERP vehicles table.

## Do's and Don'ts

### Do:
- **Do** build every surface from `asta-*` kit classes and the tokens above; never bring back DaisyUI or a second kit.
- **Do** change the accent only by overriding `--brand` (`#1b2a55` light, `#5a72c0` dark).
- **Do** include the `theme-head` partial and the theme toggle on every surface (in the ERP, the sidebar foot and the phone top bar), and check every screen on both stages. The printed invoice is the one exception: light only, no theme script.
- **Do** set amounts, ids and codes in Geist Mono with `tabular-nums`; dates and names in Geist with `tabular-nums`.
- **Do** show document status with `{{template "status-badge"}}` and nothing else.
- **Do** keep charts mono: revenue in brand, expenses in neutral grey, losses by sign or label.
- **Do** separate sections with space and tone, and draw edges with inset outlines.
- **Do** honour `prefers-reduced-motion` for every entrance animation.

### Don't:
- **Don't** add photos, illustrations, stock imagery or decorative icons; only data-driven drawings.
- **Don't** add a second accent colour, revive the gold, or use status hues outside badges.
- **Don't** put an eyebrow or kicker label above a heading, or number sections decoratively.
- **Don't** nest cards inside cards.
- **Don't** use decorative gradients or glass/backdrop blur. A gradient used as a cut-out mask in the surface colour (the pass perforation) is not decoration.
- **Don't** use a one-sided accent bar (a coloured left or top border) to mark a card or alert.
- **Don't** use `mute` for body copy or values; it is for secondary labels and hints. Use `ink-2` for running text.
- **Don't** load any family other than Geist and Geist Mono.
- **Don't** write copy in all caps or title case.
