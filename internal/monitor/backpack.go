package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// PanelRef turns a stored panel into what the BackPack client needs.
func PanelRef(p *store.BackpackPanel) backpack.PanelRef {
	return backpack.PanelRef{ID: p.ID, URL: p.URL, Token: p.Token, IgnoreTLS: p.IgnoreTLS}
}

// checkBackpackTunnel reads one tunnel's state out of its panel's snapshot.
//
// The panel already knows whether the tunnel's peer is connected — it asks the
// same code its watchdog uses — so the honest check is to ask it, not to probe
// the port from here, which from outside Iran would test a different path.
func checkBackpackTunnel(ctx context.Context, m *store.Monitor, env *Env) Result {
	if m.Config.PanelID == 0 || m.Config.TunnelName == "" {
		return down("panel and tunnel are required")
	}
	p, err := env.DB.GetPanel(ctx, m.Config.PanelID)
	if err != nil {
		return down("BackPack panel no longer exists")
	}
	maxAge := time.Duration(m.Interval) * time.Second / 2
	if maxAge < 5*time.Second {
		maxAge = 5 * time.Second
	}
	snap := env.Backpack.Get(ctx, PanelRef(p), maxAge)
	if snap.Err != "" {
		return down("panel " + p.Name + ": " + snap.Err)
	}
	t, ok := snap.Find(m.Config.TunnelName)
	if !ok {
		return down(fmt.Sprintf("tunnel %q is not on panel %s any more", m.Config.TunnelName, p.Name))
	}

	return tunnelResult(t, m.Config.ServiceDownIsDown)
}

// checkBackpackPanel checks that the panel answers and reports its host.
func checkBackpackPanel(ctx context.Context, m *store.Monitor, env *Env) Result {
	if m.Config.PanelID == 0 {
		return down("panel is required")
	}
	p, err := env.DB.GetPanel(ctx, m.Config.PanelID)
	if err != nil {
		return down("BackPack panel no longer exists")
	}
	snap := env.Backpack.Get(ctx, PanelRef(p), 5*time.Second)
	if snap.Err != "" {
		return down(snap.Err)
	}
	online := 0
	for _, t := range snap.Tunnels {
		if t.State == "online" {
			online++
		}
	}
	lat := snap.LatencyMs
	r := Result{Up: true, Ping: &lat, System: snap.System}
	r.Msg = fmt.Sprintf("%d/%d tunnels online", online, len(snap.Tunnels))
	if s := snap.System; s != nil {
		r.Msg += fmt.Sprintf(" · CPU %.0f%% · RAM %.0f%%", s.CPUPercent, s.MemPercent)
		in, out := int64(s.TotalRecvBytes), int64(s.TotalSentBytes)
		r.BytesIn, r.BytesOut = &in, &out
	}
	return r
}
