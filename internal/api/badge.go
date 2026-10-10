package api

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// handleBadge draws a shields.io-style SVG badge for a monitor that is on a
// published status page — the same rule Kuma uses, so a badge never reveals
// a monitor nobody chose to publish.
func (s *Server) handleBadge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	public, _ := s.db.PublicMonitorIDs(ctx)
	if !public[id] && !s.signedIn(r) {
		writeBadge(w, "monitor", "not public", "#9ca3af")
		return
	}
	m, err := s.db.GetMonitor(ctx, id)
	if err != nil {
		writeBadge(w, "monitor", "not found", "#9ca3af")
		return
	}
	q := r.URL.Query()
	hours := queryInt(r, "duration", 24, 1, 24*365)
	st := s.mgr.StateOf(id)
	var label, value, color string
	switch r.PathValue("kind") {
	case "status":
		label = "status"
		switch {
		case !m.Active:
			value, color = "paused", "#9ca3af"
		case st.Last == nil:
			value, color = "pending", "#f59e0b"
		case st.Last.Status == store.StatusUp:
			value, color = "up", "#22c55e"
		case st.Last.Status == store.StatusDown:
			value, color = "down", "#ef4444"
		case st.Last.Status == store.StatusMaintenance:
			value, color = "maintenance", "#3b82f6"
		default:
			value, color = "pending", "#f59e0b"
		}
	case "uptime":
		label = "uptime " + durationLabel(hours)
		u, _ := s.db.UptimeSince(ctx, []int64{id}, time.Duration(hours)*time.Hour)
		if v, ok := u[id]; ok && v.Ratio >= 0 {
			pct := v.Ratio * 100
			value = strconv.FormatFloat(pct, 'f', 2, 64) + "%"
			switch {
			case pct >= 99:
				color = "#22c55e"
			case pct >= 95:
				color = "#84cc16"
			case pct >= 90:
				color = "#f59e0b"
			default:
				color = "#ef4444"
			}
		} else {
			value, color = "n/a", "#9ca3af"
		}
	case "ping", "response":
		label = "response " + durationLabel(hours)
		u, _ := s.db.UptimeSince(ctx, []int64{id}, time.Duration(hours)*time.Hour)
		if v, ok := u[id]; ok && v.AvgPing >= 0 {
			value, color = fmt.Sprintf("%.0f ms", v.AvgPing), "#3b82f6"
		} else {
			value, color = "n/a", "#9ca3af"
		}
	case "cert-exp":
		label = "cert expires"
		if st.Cert != nil {
			d := st.Cert.DaysRemaining
			value = fmt.Sprintf("%d days", d)
			switch {
			case d <= 7:
				color = "#ef4444"
			case d <= 21:
				color = "#f59e0b"
			default:
				color = "#22c55e"
			}
		} else {
			value, color = "n/a", "#9ca3af"
		}
	default:
		http.NotFound(w, r)
		return
	}
	if l := q.Get("label"); l != "" {
		label = l
	}
	if m.Type == monitor.TypeGroup && q.Get("label") == "" && r.PathValue("kind") == "status" {
		label = m.Name
	}
	writeBadge(w, label, value, color)
}

func durationLabel(h int) string {
	switch {
	case h%24 == 0 && h >= 24:
		return strconv.Itoa(h/24) + "d"
	default:
		return strconv.Itoa(h) + "h"
	}
}

func textWidth(s string) int {
	w := 0
	for _, c := range s {
		switch {
		case strings.ContainsRune("ijl.,:;'|! ", c):
			w += 4
		case c >= 'A' && c <= 'Z', strings.ContainsRune("mwMW%", c):
			w += 9
		default:
			w += 7
		}
	}
	return w + 12
}

func writeBadge(w http.ResponseWriter, label, value, color string) {
	lw, vw := textWidth(label), textWidth(value)
	total := lw + vw
	l, v := html.EscapeString(label), html.EscapeString(value)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
<title>%s: %s</title>
<linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>
<clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>
<g clip-path="url(#r)"><rect width="%d" height="20" fill="#2b2f33"/><rect x="%d" width="%d" height="20" fill="%s"/><rect width="%d" height="20" fill="url(#s)"/></g>
<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
<text x="%d" y="14" fill="#010101" fill-opacity=".3">%s</text><text x="%d" y="13">%s</text>
<text x="%d" y="14" fill="#010101" fill-opacity=".3">%s</text><text x="%d" y="13">%s</text></g></svg>`,
		total, l, v, l, v, total, lw, lw, vw, color, total,
		lw/2, l, lw/2, l, lw+vw/2, v, lw+vw/2, v)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "max-age=60")
	_, _ = w.Write([]byte(svg))
}
