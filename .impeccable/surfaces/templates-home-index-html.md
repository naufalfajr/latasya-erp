---
version: 1
slug: "templates-home-index-html"
primary_target: "templates/home/index.html"
related_targets: []
---

# Homepage (/)

Mode: Persuade. Company profile for Latasya Trans, Bahasa Indonesia, phones first.

Audience: parents of Sekolah Plus Latansa students, families, schools and communities planning trips, people needing inter-city travel.
Action: send a WhatsApp message about one service. WhatsApp is the only action (no portal path).
Proof/content: the three services and their real facts, the fleet (static: 1 Toyota Hiace 16 seats, 2 Nissan Evalia 12 seats, 1 Daihatsu Gran Max 12 seats), the stated values, legal entity, office address from company settings. No photos (typographic only), no testimonials, no invented numbers or pricing beyond "tarif berbasis jarak" for the school run.
Constraints: Asta UI world (Geist, paper/ink, Latasya navy as the one accent), light and dark with a toggle. No "Cara pesan" section (user removed it). Fleet is static homepage copy in home.go, never read from the ERP.

## Direction contract

THESIS: The first screen is a working travel pass: choose a service and the pass writes the WhatsApp message for you. Refuses the centred headline over a row of equal cards.
OWN-WORLD: Asta UI kit. Paper ground, paper-2 raised surfaces, ink type, Latasya navy for the pass stub, the active segment and the contact band. Geist 500 display at tight tracking, Geist Mono for labels and facts. Soft 8-14px corners, inset outlines, no borders, no gradients, no photos.
STORY: A visitor reads what Latasya Trans runs, sees each service's who/when/how-priced, sees the real fleet by model, trusts the values, and sends one prefilled WhatsApp message.
FIRST VIEWPORT: Sticky 56px header (wordmark, nav, theme toggle, WhatsApp button). Left: three-line headline (Geist 500, clamp 44–66px) with the last service in navy, one lede; no kicker. Right (540px): the pass on paper-2: service segmented control (radios), live message preview, navy stub with who/when facts and a white "Kirim ke WhatsApp" submit. On phones the pass stacks under the headline and its stub becomes a bottom bar.
FORM: Travel pass + service cards, position 5 on my ranked list, steered by the user into a company profile. seed:815c86c78181ea28. Signature interaction: switching the pass's service swaps the message, stub facts and accent segment with no JavaScript (radio + :has), entering with one expo-out transform.
FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

Unresolved: none.
