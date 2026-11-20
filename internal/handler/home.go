package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/naufal/latasya-erp/internal/model"
)

type homeData struct {
	Company      *model.CompanyProfile
	ContactWA    string
	PhoneDisplay string
	WA           map[string]string // service key -> prefilled WhatsApp link
	Messages     map[string]string // service key -> the message that link sends
	MapsURL      string
	Fleet        []fleetModel
	FleetUnits   int
	FleetTotal   int
	FleetRange   string // "12" or "12–16"
}

// fleetModel is one vehicle model on the public homepage.
type fleetModel struct {
	Name  string
	Seats int
	Units int
	Plan  [][]string // see seatPlan
}

// homeFleet is marketing copy, kept static on purpose: the ERP's vehicles
// table tracks costs and school routes, not the fleet the public sees.
var homeFleet = []fleetModel{
	{Name: "Toyota Hiace", Seats: 16, Units: 1},
	{Name: "Nissan Evalia", Seats: 12, Units: 2},
	{Name: "Daihatsu Gran Max", Seats: 12, Units: 1},
}

// Fleet totals for the homepage line, derived once from homeFleet.
var homeFleetUnits, homeFleetTotal, homeFleetRange = func() (int, int, string) {
	units, total, lo, hi := 0, 0, 0, 0
	for i := range homeFleet {
		m := &homeFleet[i]
		m.Plan = seatPlan(m.Seats)
		units += m.Units
		total += m.Seats * m.Units
		if i == 0 || m.Seats < lo {
			lo = m.Seats
		}
		hi = max(hi, m.Seats)
	}
	r := strconv.Itoa(lo)
	if hi != lo {
		r += "–" + strconv.Itoa(hi)
	}
	return units, total, r
}()

// homeMessages prefill the WhatsApp chat for each service the homepage offers.
var homeMessages = map[string]string{
	"sekolah": "Halo Latasya Trans, saya ingin tanya layanan antar jemput sekolah untuk peserta didik Sekolah Plus Latansa.",
	"wisata":  "Halo Latasya Trans, saya ingin tanya paket perjalanan wisata untuk rombongan kami.",
	"travel":  "Halo Latasya Trans, saya ingin tanya layanan travel antar daerah.",
}

// PublicHome is the bare-domain landing page: a read-only company profile
// for anyone who lands there without an invoice link, plus a discreet path
// to staff login. Parents reach their invoices via their own /p/{code} link,
// never through this page.
func (h *Handler) PublicHome(w http.ResponseWriter, r *http.Request) {
	company, err := h.Company.Get(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	wa := make(map[string]string, len(homeMessages))
	for key, msg := range homeMessages {
		wa[key] = buildWALink(company.Phone, msg)
	}
	var mapsURL string
	if company.Address != "" {
		mapsURL = "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(company.Address)
	}

	t, err := h.getTemplate("templates/home/index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	t.ExecuteTemplate(w, "index.html", PageData{
		Title:    company.Name,
		BasePath: h.BasePath,
		Data: homeData{
			Company:      company,
			ContactWA:    buildWALink(company.Phone, "Halo, saya ingin bertanya soal layanan Latasya Trans."),
			PhoneDisplay: "+" + model.NormalizePhoneID(company.Phone),
			WA:           wa,
			Messages:     homeMessages,
			MapsURL:      mapsURL,
			Fleet:        homeFleet,
			FleetUnits:   homeFleetUnits,
			FleetTotal:   homeFleetTotal,
			FleetRange:   homeFleetRange,
		},
	})
}

// seatPlan draws n seats as a right-hand-drive minibus from above, 4 cells per
// row ("d" driver, "s" seat, "" aisle); layout rules live in DESIGN.md.
func seatPlan(n int) [][]string {
	if n <= 0 {
		return nil
	}
	bench := min(n, 4)
	rest := n - bench
	front := []string{"", "", "", "d"}
	for i := 0; i < rest%3; i++ {
		front[i] = "s"
	}
	plan := [][]string{front}
	for i := 0; i < rest/3; i++ {
		plan = append(plan, []string{"s", "", "s", "s"})
	}
	back := make([]string, 4)
	for i := 0; i < bench; i++ {
		back[i] = "s"
	}
	return append(plan, back)
}
