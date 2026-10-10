// Command mock-backpack imitates a BackPack web panel's read-only API, for
// developing and demoing Neo Uptime without real tunnels.
//
//	go run ./cmd/mock-backpack -addr :7777 -token test-token
package main

import (
	"encoding/json"
	"flag"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tunnel struct {
	Name         string           `json:"name"`
	Role         string           `json:"role"`
	Transport    string           `json:"transport"`
	Direction    string           `json:"direction"`
	Carrier      string           `json:"carrier"`
	Addr         string           `json:"addr"`
	Ports        string           `json:"ports"`
	State        string           `json:"state"`
	Ping         int              `json:"ping"`
	PeerLocation string           `json:"peerLocation"`
	PeerISP      string           `json:"peerISP"`
	PeerCountry  string           `json:"peerCountry"`
	TunnelPort   string           `json:"tunnelPort"`
	Uptime       string           `json:"uptime"`
	InBytes      uint64           `json:"inBytes"`
	OutBytes     uint64           `json:"outBytes"`
	TotalBytes   uint64           `json:"totalBytes"`
	Preset       string           `json:"preset"`
	KCPLoss      float64          `json:"kcpLossPercent,omitempty"`
	Rates        []map[string]any `json:"rates"`

	basePing int
	flaky    float64
	rate     float64
	since    time.Time
}

func main() {
	addr := flag.String("addr", ":7777", "listen address")
	token := flag.String("token", "test-token", "bearer token to accept")
	flag.Parse()

	now := time.Now()
	tunnels := []*tunnel{
		{Name: "DE2", Transport: "wssmux", Carrier: "wssmux", PeerCountry: "DE", PeerLocation: "Frankfurt, Germany", PeerISP: "Hetzner Online", TunnelPort: "3857", Ports: "6729", Preset: "Aggressive", basePing: 92, flaky: 0.02, rate: 2.1e6, InBytes: 2_980_000_000_000, OutBytes: 236_100_000_000},
		{Name: "NL", Transport: "tcp", Carrier: "tcp", PeerCountry: "NL", PeerLocation: "Amsterdam, Netherlands", PeerISP: "DigitalOcean", TunnelPort: "3850", Ports: "8893", Preset: "Aggressive", basePing: 90, flaky: 0.01, rate: 4.5e6, InBytes: 7_250_000_000_000, OutBytes: 680_900_000_000},
		{Name: "OM", Transport: "tcpmux", Carrier: "tcpmux", PeerCountry: "OM", PeerLocation: "Muscat, Oman", PeerISP: "Omantel", TunnelPort: "7847", Ports: "8958", Preset: "Turbo", basePing: 188, flaky: 0.05, rate: 9e4, InBytes: 34_400_000_000, OutBytes: 4_400_000_000},
		{Name: "TR", Transport: "kcp", Carrier: "kcp", PeerCountry: "TR", PeerLocation: "Istanbul, Türkiye", PeerISP: "Netvia Bilisim Yazilim Danismanlik Ticaret", TunnelPort: "7734", Ports: "1860", Preset: "Aggressive", basePing: 51, flaky: 0.15, rate: 10, KCPLoss: 1.8},
		{Name: "TR2", Transport: "tcpmux", Carrier: "tcpmux", PeerCountry: "TR", PeerLocation: "Istanbul, Türkiye", PeerISP: "Turkcell", TunnelPort: "4559", Ports: "3564", Preset: "Aggressive", basePing: 43, flaky: 0.03, rate: 6e5, InBytes: 5_600_000_000, OutBytes: 580_800_000},
		{Name: "UAE", Transport: "tcp", Carrier: "tcp", PeerCountry: "AE", PeerLocation: "Dubai, UAE", PeerISP: "Etisalat", TunnelPort: "4880", Ports: "6084", Preset: "Turbo", basePing: 189, flaky: 0.04, rate: 2.5e5, InBytes: 159_300_000_000, OutBytes: 14_000_000_000},
	}
	for _, t := range tunnels {
		t.Role, t.Direction, t.Addr, t.State, t.since = "server", "reverse", "0.0.0.0:"+t.TunnelPort, "online", now.Add(-40*time.Minute)
	}

	var mu sync.Mutex
	go func() {
		for range time.Tick(3 * time.Second) {
			mu.Lock()
			for _, t := range tunnels {
				switch {
				case t.State == "online" && rand.Float64() < t.flaky/4:
					t.State = "offline"
				case t.State == "offline" && rand.Float64() < 0.35:
					t.State, t.since = "online", time.Now()
				}
				r := t.rate * (0.4 + rand.Float64()*1.2)
				if t.State != "online" {
					r = 0
				}
				t.InBytes += uint64(r * 3 * 0.92)
				t.OutBytes += uint64(r * 3 * 0.08)
				t.TotalBytes = t.InBytes + t.OutBytes
				t.Rates = append(t.Rates, map[string]any{"t": time.Now().Unix(), "in": r * 0.92, "out": r * 0.08})
				if len(t.Rates) > 40 {
					t.Rates = t.Rates[len(t.Rates)-40:]
				}
			}
			mu.Unlock()
		}
	}()

	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+*token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	http.HandleFunc("/api/tunnels", auth(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		for _, t := range tunnels {
			if t.State == "online" {
				jitter := math.Sin(float64(time.Now().UnixNano()%1e10)/1e9) * float64(t.basePing) * 0.15
				t.Ping = t.basePing + int(jitter) + rand.IntN(max(3, t.basePing/8))
				t.Uptime = strings.TrimSuffix(time.Since(t.since).Round(time.Minute).String(), "0s")
			} else {
				t.Ping, t.Uptime = -1, ""
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tunnels)
	}))
	http.HandleFunc("/api/stats", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hostname": "iran-edge-1", "os": "Ubuntu 24.04", "uptime": "12d 4h",
			"cpuPercent": 18 + rand.Float64()*20, "cpuCores": 4, "memPercent": 41 + rand.Float64()*6,
			"diskPercent": 37.5, "totalRecvBytes": 9_500_000_000_000, "totalSentBytes": 1_100_000_000_000,
			"tunnelsTotal": len(tunnels), "tunnelsRunning": len(tunnels), "monitorRunning": true, "version": "v1.9.0",
		})
	}))
	log.Printf("mock BackPack panel on %s (token %q)", *addr, *token)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
