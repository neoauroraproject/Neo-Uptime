package monitor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

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
	snap := env.Backpack.Get(ctx, p, maxAge)
	if snap.Err != "" {
		return down("panel " + p.Name + ": " + snap.Err)
	}
	t, ok := snap.Find(m.Config.TunnelName)
	if !ok {
		return down(fmt.Sprintf("tunnel %q is not on panel %s any more", m.Config.TunnelName, p.Name))
	}

	tc := *t
	r := Result{Tunnel: &tc}
	in, out := int64(t.InBytes), int64(t.OutBytes)
	r.BytesIn, r.BytesOut = &in, &out
	if t.Ping >= 0 {
		v := float64(t.Ping)
		r.Ping = &v
	}

	label := strings.ToUpper(firstNonEmpty(t.Carrier, t.Transport))
	switch {
	case t.QuotaHit:
		r.Msg = "traffic quota reached — tunnel stopped by BackPack"
	case t.State == "online" && t.ServiceDown != "" && m.Config.ServiceDownIsDown:
		r.Msg = "tunnel up but the forwarded service is refusing: " + t.ServiceDown
	case t.State == "online":
		r.Up = true
		r.Msg = fmt.Sprintf("%s online", label)
		if t.Uptime != "" {
			r.Msg += " · up " + t.Uptime
		}
		if t.ServiceDown != "" {
			r.Msg += " · service down: " + t.ServiceDown
		}
	case t.State == "stopped":
		r.Msg = "tunnel service is stopped"
	default:
		r.Msg = fmt.Sprintf("%s %s — peer not connected", label, firstNonEmpty(t.State, "offline"))
	}
	return r
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
	snap := env.Backpack.Get(ctx, p, 5*time.Second)
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
