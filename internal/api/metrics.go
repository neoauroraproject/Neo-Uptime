package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

func promLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

// handleMetrics exposes Prometheus metrics, named like Uptime Kuma's so
// existing Grafana dashboards keep working, plus BackPack tunnel traffic.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.db.ListMonitors(ctx)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	states := s.mgr.States()
	u24, _ := s.db.UptimeSince(ctx, nil, 24*time.Hour)
	var b strings.Builder

	head := func(name, typ, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	}
	labels := func(m *store.Monitor) string {
		return fmt.Sprintf(`monitor_id="%d",monitor_name="%s",monitor_type="%s",monitor_url="%s"`,
			m.ID, promLabel(m.Name), m.Type, promLabel(monitor.Target(m)))
	}

	head("monitor_status", "gauge", "Monitor status (1 = up, 0 = down, 2 = pending, 3 = maintenance)")
	for _, m := range list {
		if st := states[m.ID]; m.Active && st.Last != nil {
			fmt.Fprintf(&b, "monitor_status{%s} %d\n", labels(m), st.Last.Status)
		}
	}
	head("monitor_response_time", "gauge", "Last response time in milliseconds")
	for _, m := range list {
		if st := states[m.ID]; m.Active && st.Last != nil && st.Last.Ping != nil {
			fmt.Fprintf(&b, "monitor_response_time{%s} %.3f\n", labels(m), *st.Last.Ping)
		}
	}
	head("monitor_uptime_ratio_24h", "gauge", "Share of checks in the last 24 hours that were up")
	for _, m := range list {
		if u, ok := u24[m.ID]; ok && u.Ratio >= 0 {
			fmt.Fprintf(&b, "monitor_uptime_ratio_24h{%s} %.5f\n", labels(m), u.Ratio)
		}
	}
	head("monitor_cert_days_remaining", "gauge", "Days until the TLS certificate expires")
	for _, m := range list {
		if st := states[m.ID]; st.Cert != nil {
			fmt.Fprintf(&b, "monitor_cert_days_remaining{%s} %d\n", labels(m), st.Cert.DaysRemaining)
		}
	}
	head("monitor_cert_is_valid", "gauge", "1 when the TLS certificate is currently valid")
	for _, m := range list {
		if st := states[m.ID]; st.Cert != nil {
			v := 0
			if st.Cert.Valid {
				v = 1
			}
			fmt.Fprintf(&b, "monitor_cert_is_valid{%s} %d\n", labels(m), v)
		}
	}
	head("backpack_tunnel_bytes_in_total", "counter", "Bytes received over a BackPack tunnel")
	for _, m := range list {
		if st := states[m.ID]; st.Tunnel != nil {
			fmt.Fprintf(&b, "backpack_tunnel_bytes_in_total{%s} %d\n", labels(m), st.Tunnel.InBytes)
		}
	}
	head("backpack_tunnel_bytes_out_total", "counter", "Bytes sent over a BackPack tunnel")
	for _, m := range list {
		if st := states[m.ID]; st.Tunnel != nil {
			fmt.Fprintf(&b, "backpack_tunnel_bytes_out_total{%s} %d\n", labels(m), st.Tunnel.OutBytes)
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
