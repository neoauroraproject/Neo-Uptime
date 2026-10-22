package radar

import (
	"testing"
	"time"
)

// A radar with two datacenters in Iran and one abroad, and checks over the
// last hour.
func sample(now time.Time) *raw {
	r := &raw{
		Agents: []rawAgent{
			{ID: 1, Name: "Asiatech Tehran", Slug: "asiatech-tehran", Internal: true, Online: true, Order: 1},
			{ID: 2, Name: "Hiweb Tehran", Slug: "hiweb-tehran", Internal: true, Online: true, Order: 0},
			{ID: 3, Name: "Hetzner Finland", Slug: "hetzner-finland", Internal: false, Online: true},
			{ID: 4, Name: "Shatel Karaj", Slug: "shatel-karaj", Internal: true, Online: false},
		},
		Targets: []rawTarget{
			{ID: 10, Name: "Google", Slug: "google"}, {ID: 11, Name: "Github", Slug: "github"},
			{ID: 12, Name: "Cloudflare", Slug: "cloudflare"}, {ID: 13, Name: "Whatsapp", Slug: "whatsapp"},
			{ID: 20, Name: "Aparat", Slug: "aparat"}, {ID: 21, Name: "Digikala", Slug: "digikala"},
		},
		Stats: map[string]map[string][]rawPoint{},
	}
	add := func(agent, target string, up func(min int) bool) {
		if r.Stats[agent] == nil {
			r.Stats[agent] = map[string][]rawPoint{}
		}
		for m := 59; m >= 0; m-- {
			r.Stats[agent][target] = append(r.Stats[agent][target], rawPoint{At: now.Add(-time.Duration(m) * time.Minute), Latency: 200, Up: up(m)})
		}
	}
	always := func(int) bool { return true }
	never := func(int) bool { return false }
	// Asiatech: the international internet went away 20 minutes ago.
	for _, t := range []string{"10", "11", "12"} {
		add("1", t, func(m int) bool { return m >= 20 })
	}
	add("1", "13", never) // WhatsApp is filtered everywhere in Iran
	add("1", "20", always)
	add("1", "21", always)
	// Hiweb: fine, but WhatsApp is filtered (never answers), and Google
	// missed only the very last check.
	add("2", "10", func(m int) bool { return m != 0 })
	add("2", "11", always)
	add("2", "12", always)
	add("2", "13", never)
	// Finland reaches Iran's services.
	add("3", "20", always)
	add("3", "21", always)
	add("3", "10", never) // a foreign service failing abroad does not count
	return r
}

func TestSummarize(t *testing.T) {
	now := time.Now()
	snap := Summarize(sample(now), now)
	if len(snap.Datacenters) != 4 {
		t.Fatalf("datacenters: %+v", snap.Datacenters)
	}
	by := map[string]Datacenter{}
	for _, d := range snap.Datacenters {
		by[d.Slug] = d
	}
	// Iran first, in the radar's own order.
	if snap.Datacenters[0].Slug != "hiweb-tehran" || snap.Datacenters[3].Slug != "hetzner-finland" {
		t.Errorf("order: %s … %s", snap.Datacenters[0].Slug, snap.Datacenters[3].Slug)
	}
	a := by["asiatech-tehran"]
	if a.State != "down" || a.Up != 0 || a.Total != 3 || len(a.Down) != 3 {
		t.Errorf("asiatech should have lost the international internet: %+v", a)
	}
	if a.History[0] != 1 || a.History[11] != 0 {
		t.Errorf("asiatech history should go from all up to all down: %v", a.History)
	}
	h := by["hiweb-tehran"]
	if h.State != "ok" || h.Up != 3 || h.Total != 3 || len(h.Down) != 0 {
		t.Errorf("hiweb: one lost check is not an outage, and WhatsApp (blocked everywhere) does not count: %+v", h)
	}
	f := by["hetzner-finland"]
	if f.Internal || f.State != "ok" || f.Total != 2 {
		t.Errorf("abroad, what counts is Iran's services: %+v", f)
	}
	if s := by["shatel-karaj"]; s.State != "unknown" {
		t.Errorf("an offline datacenter is unknown: %+v", s)
	}

	if p := snap.Pick([]string{"hiweb-tehran"}); p == nil || len(p.Datacenters) != 1 || p.Datacenters[0].Slug != "hiweb-tehran" {
		t.Errorf("pick: %+v", p)
	}
	if p := snap.Pick([]string{"nope"}); p != nil {
		t.Errorf("pick of nothing: %+v", p)
	}
	if p := snap.Pick(nil); p == nil || len(p.Datacenters) != 4 {
		t.Errorf("pick all: %+v", p)
	}
}
