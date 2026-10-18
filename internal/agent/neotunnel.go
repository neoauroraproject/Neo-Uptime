package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
)

// ---------- NeoTunnel ----------

// NeoTunnel keeps everything in its state directory: state.json (the
// tunnels, local and pushed by the main server), health.json (the daemon
// pings the far inner address of every tunnel every 8 s) and traffic.json
// (totals that survive reboots). The agent reads those files, so it works
// with any NeoTunnel release and needs no panel or password.

type ntSide struct {
	Node     string `json:"node"`
	Endpoint string `json:"endpoint"`
}

type ntTunnel struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Over     string    `json:"over"`
	Sides    [2]ntSide `json:"sides"`
	Port     uint16    `json:"port"`
	Mode     string    `json:"mode"`
	Encrypt  string    `json:"encrypt"`
	Disabled bool      `json:"disabled"`
	RateMbit int       `json:"rate_mbit"`
	QuotaGB  float64   `json:"quota_gb"`
	Profile  string    `json:"profile"`
}

type ntForward struct {
	Node   string `json:"node"`
	Tunnel string `json:"tunnel"`
	Ports  string `json:"ports"`
}

type ntState struct {
	Self     string      `json:"self"`
	Tunnels  []ntTunnel  `json:"tunnels"`
	Forwards []ntForward `json:"forwards"`
	Managed  *struct {
		Self     string      `json:"self"`
		Tunnels  []ntTunnel  `json:"tunnels"`
		Forwards []ntForward `json:"forwards"`
	} `json:"managed"`
}

func (s *ntState) isSelf(node string) bool {
	return node != "" && (node == s.Self || (s.Managed != nil && node == s.Managed.Self))
}

type ntHealth struct {
	AvgMs  float64 `json:"avg_ms"`
	Loss   float64 `json:"loss"`
	Last   int64   `json:"last"`
	Probes int     `json:"probes"`
	Passes bool    `json:"passes"`
}

type ntPair struct {
	Rx uint64 `json:"rx"`
	Tx uint64 `json:"tx"`
}

type ntTraffic struct {
	Tunnels map[string]struct {
		Since time.Time         `json:"since"`
		Total ntPair            `json:"total"`
		Days  map[string]ntPair `json:"days"`
	} `json:"tunnels"`
}

// ntIfUp reports whether NeoTunnel's interface for a tunnel exists and is up.
var ntIfUp = func(name string) (exists, up bool) {
	b, err := os.ReadFile(filepath.Join("/sys/class/net", "nt-"+name, "flags"))
	if err != nil {
		return false, false
	}
	f, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(string(b)), "0x"), 16, 32)
	return true, err == nil && f&1 != 0 // IFF_UP
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

func collectNeoTunnel(_ context.Context, cfg Config) *agentproto.Source {
	var st ntState
	data, err := os.ReadFile(filepath.Join(cfg.NeoTunnelDir, "state.json"))
	if err != nil {
		if _, e := os.Stat(cfg.NeoTunnelDir); e != nil {
			return nil // not installed here
		}
		return &agentproto.Source{Installed: true, Mode: "files", Tunnels: []backpack.Tunnel{}}
	}
	src := &agentproto.Source{Installed: true, Mode: "files", Tunnels: []backpack.Tunnel{}}
	if err := json.Unmarshal(data, &st); err != nil {
		src.Error = "state.json: " + err.Error()
		return src
	}
	if out, err := exec.Command("neotunnel", "version").Output(); err == nil {
		src.Version = strings.TrimSpace(string(out))
	}

	// The tunnels this server runs: its own, and those its main server pushed.
	type entry struct {
		t       ntTunnel
		managed bool
	}
	var all []entry
	var forwards []ntForward
	for _, t := range st.Tunnels {
		if st.isSelf(t.Sides[0].Node) || st.isSelf(t.Sides[1].Node) {
			all = append(all, entry{t, false})
		}
	}
	forwards = append(forwards, st.Forwards...)
	if m := st.Managed; m != nil {
		for _, t := range m.Tunnels {
			if st.isSelf(t.Sides[0].Node) || st.isSelf(t.Sides[1].Node) {
				all = append(all, entry{t, true})
			}
		}
		forwards = append(forwards, m.Forwards...)
	}
	byName := map[string]ntTunnel{}
	carriers := map[string]bool{}
	for _, e := range all {
		byName[e.t.Name] = e.t
		if e.t.Over != "" {
			carriers[e.t.Over] = true
		}
	}

	health := map[string]ntHealth{}
	readJSON(filepath.Join(cfg.NeoTunnelDir, "health.json"), &health)
	var tr ntTraffic
	readJSON(filepath.Join(cfg.NeoTunnelDir, "traffic.json"), &tr)
	month := time.Now().Format("2006-01")

	active, known := serviceActive("neotunnel.service")
	for _, e := range all {
		t := e.t
		if carriers[t.Name] {
			continue // a layer under another tunnel (e.g. the SIT of a 6to4), not one of its own
		}
		me := 0
		if st.isSelf(t.Sides[1].Node) {
			me = 1
		}
		// The bottom of the stack is the layer that runs between the servers.
		bottom := t
		for i := 0; bottom.Over != "" && i < 8; i++ {
			b, ok := byName[bottom.Over]
			if !ok {
				break
			}
			bottom = b
		}
		kind := firstSet(t.Mode, t.Kind)
		out := backpack.Tunnel{
			Source: "neotunnel", Name: t.Name, Role: map[bool]string{false: "main", true: "destination"}[e.managed],
			Transport: kind, Carrier: kind, Direction: "direct", Ping: -1,
			Node: t.Sides[1-me].Node, Addr: bottom.Sides[1-me].Endpoint, Preset: presetLabel(firstSet(t.Profile, "balanced")),
			BandwidthMbps: t.RateMbit,
		}
		if bottom.Port != 0 {
			out.TunnelPort = strconv.Itoa(int(bottom.Port))
			out.Addr = joinHostPort(out.Addr, out.TunnelPort)
		}
		var ports []string
		for _, f := range forwards {
			if f.Tunnel == t.Name && st.isSelf(f.Node) && f.Ports != "" {
				ports = append(ports, f.Ports)
			}
		}
		out.Ports = strings.Join(ports, ", ")

		var monthBytes uint64
		if c, ok := tr.Tunnels[t.Name]; ok {
			out.InBytes, out.OutBytes = c.Total.Rx, c.Total.Tx
			out.TotalBytes = c.Total.Rx + c.Total.Tx
			for d, p := range c.Days {
				if strings.HasPrefix(d, month) {
					monthBytes += p.Rx + p.Tx
				}
			}
		}
		if t.QuotaGB > 0 {
			out.QuotaLimit = uint64(t.QuotaGB * (1 << 30))
			out.QuotaUsed = monthBytes
			out.QuotaHit = monthBytes >= out.QuotaLimit
		}

		h, probed := health[t.Name]
		probed = probed && h.Probes > 0
		exists, up := ntIfUp(t.Name)
		switch {
		case known && !active:
			out.State = "stopped"
			out.LastError = "the neotunnel service is not running"
		case t.Disabled:
			out.State = "stopped"
			out.LastError = "turned off in NeoTunnel"
		case out.QuotaHit:
			out.State = "stopped"
		case !exists:
			out.State = "offline"
			out.LastError = "interface nt-" + t.Name + " is missing"
		case !up:
			out.State = "offline"
			out.LastError = "interface nt-" + t.Name + " is down"
		case probed && h.Passes && time.Since(time.Unix(h.Last, 0)) < 90*time.Second:
			out.State = "online"
			out.Ping = max(1, int(h.AvgMs+0.5))
		case probed:
			out.State = "offline"
			out.LastError = "no reply through the tunnel"
			if h.Last > 0 {
				out.LastError += " for " + humanSecs(max(1, int64(time.Since(time.Unix(h.Last, 0)).Seconds())))
			}
		default:
			// Not pinged (no inner address, or the daemon has not got to it
			// yet): the interface being up is all there is to go on.
			out.State = "online"
		}
		src.Tunnels = append(src.Tunnels, out)
	}
	sortTunnels(src.Tunnels)
	return src
}

func joinHostPort(host, port string) string {
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}
