package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/auth"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// The private status API serves a status page's data as JSON to whoever holds
// a key for it — to show it on your own site. Only the owner account can make
// these keys: a key is a door into the data, so only the account that owns
// the instance hands them out.

func (s *Server) handleListStatusKeys(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListStatusAPIKeys(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, list)
}

func cleanOrigins(raw string) (string, error) {
	var out []string
	for _, o := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o == "" {
			continue
		}
		if o == "*" {
			return "*", nil
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			return "", fmt.Errorf("%q is not an origin — use the form https://example.com", o)
		}
		out = append(out, u.Scheme+"://"+u.Host)
	}
	return strings.Join(out, ","), nil
}

func (s *Server) handleSaveStatusKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name           string `json:"name"`
		StatusPageID   int64  `json:"statusPageId"`
		AllowedOrigins string `json:"allowedOrigins"`
		Active         *bool  `json:"active"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	origins, err := cleanOrigins(in.AllowedOrigins)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "give the key a name, e.g. \"my website\"")
		return
	}
	ctx := r.Context()
	if id, ok := pathID(r, "id"); ok {
		list, _ := s.db.ListStatusAPIKeys(ctx)
		var k *store.StatusAPIKey
		for _, x := range list {
			if x.ID == id {
				k = x
			}
		}
		if k == nil {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		k.Name, k.AllowedOrigins = in.Name, origins
		if in.Active != nil {
			k.Active = *in.Active
		}
		if err := s.db.UpdateStatusAPIKey(ctx, k); err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, k)
		return
	}
	if _, err := s.db.GetStatusPage(ctx, in.StatusPageID); err != nil {
		writeErr(w, http.StatusBadRequest, "choose a status page")
		return
	}
	secret := "sk_" + auth.RandomToken(24)
	k := &store.StatusAPIKey{Name: in.Name, Prefix: secret[:9], KeyHash: auth.HashToken(secret), StatusPageID: in.StatusPageID, AllowedOrigins: origins}
	if err := s.db.CreateStatusAPIKey(ctx, k); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"key": k, "secret": secret})
}

func (s *Server) handleDeleteStatusKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.db.DeleteStatusAPIKey(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---------- the API itself ----------

type v1Monitor struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Country    string   `json:"country,omitempty"`
	Status     string   `json:"status"` // up | down | pending | maintenance | paused | unknown
	Latency    *float64 `json:"latencyMs"`
	AvgLatency *float64 `json:"avgLatencyMs"`
	Uptime24h  *float64 `json:"uptime24h"`
	Uptime30d  *float64 `json:"uptime30d"`
	LastCheck  *int64   `json:"lastCheck"`
	Message    string   `json:"message,omitempty"`
	Tunnel     *v1Tunn  `json:"tunnel,omitempty"`
	History    []string `json:"history"` // last 30 checks, oldest first: u d p m
}

// v1Tunn leaves out what the status page hides.
type v1Tunn struct {
	Source    string  `json:"source,omitempty"`
	Transport string  `json:"transport,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Preset    string  `json:"preset,omitempty"`
	Uptime    string  `json:"uptime,omitempty"`
	BytesIn   *uint64 `json:"bytesIn,omitempty"`
	BytesOut  *uint64 `json:"bytesOut,omitempty"`
}

func statusWord(st int, active bool) string {
	if !active {
		return "paused"
	}
	switch st {
	case store.StatusUp:
		return "up"
	case store.StatusDown:
		return "down"
	case store.StatusPending:
		return "pending"
	case store.StatusMaintenance:
		return "maintenance"
	}
	return "unknown"
}

func ratioPtr(u store.Uptime, ok bool) *float64 {
	if !ok || u.Ratio < 0 {
		return nil
	}
	v := float64(int(u.Ratio*1e6)) / 1e6
	return &v
}

func (s *Server) statusV1(ctx context.Context, p *store.StatusPage) map[string]any {
	var ids []int64
	for _, g := range p.Groups {
		for _, m := range g.Monitors {
			ids = append(ids, m.MonitorID)
		}
	}
	all, _ := s.db.ListMonitors(ctx)
	byID := map[int64]*store.Monitor{}
	for _, m := range all {
		byID[m.ID] = m
	}
	u24, _ := s.db.UptimeSince(ctx, ids, 24*time.Hour)
	u30, _ := s.db.UptimeSince(ctx, ids, 30*24*time.Hour)
	beats := map[int64][]*store.Heartbeat{}
	if len(ids) > 0 {
		beats, _ = s.db.RecentHeartbeats(ctx, ids, 30)
	}
	states := s.mgr.States()
	letters := map[int]string{0: "d", 1: "u", 2: "p", 3: "m"}
	nDown, nMaint, nTotal := 0, 0, 0
	groups := []map[string]any{}
	for _, g := range p.Groups {
		mons := []v1Monitor{}
		for _, ref := range g.Monitors {
			m, ok := byID[ref.MonitorID]
			if !ok {
				continue
			}
			st := states[m.ID]
			v := v1Monitor{ID: m.ID, Name: m.Name, Type: m.Type, Country: m.Country, Status: "unknown", History: []string{}}
			if p.Hidden("flag") {
				v.Country = ""
			}
			if st.Last != nil {
				v.Status = statusWord(st.Last.Status, m.Active)
				v.Latency = st.Last.Ping
				t := st.Last.Time
				v.LastCheck = &t
				if st.Last.Status != store.StatusUp {
					v.Message = st.Last.Msg
				}
			} else if !m.Active {
				v.Status = "paused"
			}
			u, ok := u24[m.ID]
			v.Uptime24h = ratioPtr(u, ok)
			if ok && u.AvgPing >= 0 {
				a := float64(int(u.AvgPing*10)) / 10
				v.AvgLatency = &a
			}
			u2, ok2 := u30[m.ID]
			v.Uptime30d = ratioPtr(u2, ok2)
			if t := st.Tunnel; t != nil {
				pt := publicTunnelOf(p, t)
				if v.Country == "" {
					// Same fallback as the status page: the tunnel peer's country.
					v.Country = normCountry(pt.PeerCountry)
				}
				v.Tunnel = &v1Tunn{Source: pt.Source, Transport: pt.Carrier, Direction: pt.Direction, Preset: pt.Preset, Uptime: pt.Uptime, BytesIn: pt.InBytes, BytesOut: pt.OutBytes}
			}
			for _, h := range beats[m.ID] {
				v.History = append(v.History, letters[h.Status])
			}
			if m.Active {
				nTotal++
				switch v.Status {
				case "down":
					nDown++
				case "maintenance":
					nMaint++
				}
			}
			mons = append(mons, v)
		}
		groups = append(groups, map[string]any{"name": g.Name, "monitors": mons})
	}
	overall := "operational"
	switch {
	case nTotal == 0:
		overall = "unknown"
	case nDown > 0 && nDown == nTotal:
		overall = "major_outage"
	case nDown > 0:
		overall = "partial_outage"
	case nMaint > 0:
		overall = "maintenance"
	}
	incidents, _ := s.db.ListIncidents(ctx, p.ID, true)
	maint := []map[string]any{}
	now := time.Now()
	for _, mt := range s.mgr.Maintenances() {
		linked := slices.Contains(mt.StatusPageIDs, p.ID)
		for _, id := range mt.MonitorIDs {
			linked = linked || slices.Contains(ids, id)
		}
		if !linked {
			continue
		}
		st, start, end := monitor.MaintenanceStatus(mt, now)
		if st != monitor.MaintUnder && st != monitor.MaintScheduled {
			continue
		}
		item := map[string]any{"title": mt.Title, "description": mt.Description, "status": st}
		if !start.IsZero() {
			item["start"] = start.UnixMilli()
		}
		if !end.IsZero() {
			item["end"] = end.UnixMilli()
		}
		maint = append(maint, item)
	}
	return map[string]any{
		"version": 1,
		"page":    map[string]any{"slug": p.Slug, "title": p.Title, "description": p.Description, "language": p.Language},
		"overall": overall, "updatedAt": now.UnixMilli(), "groups": groups,
		"incidents": incidents, "maintenance": maint,
	}
}

func firstSet(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (s *Server) handleStatusV1(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := bearer(r)
	if key == "" {
		key = r.URL.Query().Get("key")
	}
	origin := r.Header.Get("Origin")
	h := w.Header()
	h.Set("Vary", "Origin")
	if r.Method == http.MethodOptions {
		// Preflight: the real request is authorised; this only says what a
		// browser may send.
		if origin != "" {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization")
			h.Set("Access-Control-Max-Age", "600")
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ip := s.clientIP(r)
	if blocked, _ := s.limiter.Blocked(ip); blocked {
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts")
		return
	}
	k, err := s.db.GetStatusAPIKeyByHash(ctx, auth.HashToken(key))
	if key == "" || err != nil || !k.Active {
		if key != "" {
			s.limiter.Fail(ip)
		}
		writeErr(w, http.StatusUnauthorized, "a valid status API key is required")
		return
	}
	p, err := s.db.GetStatusPage(ctx, k.StatusPageID)
	if err != nil || !strings.EqualFold(p.Slug, r.PathValue("slug")) {
		writeErr(w, http.StatusForbidden, "this key is for another status page")
		return
	}
	if origin != "" {
		allowed := k.AllowedOrigins == "" || k.AllowedOrigins == "*"
		for _, o := range strings.Split(k.AllowedOrigins, ",") {
			allowed = allowed || strings.EqualFold(o, origin)
		}
		if !allowed {
			writeErr(w, http.StatusForbidden, "this key may not be used from "+origin)
			return
		}
		h.Set("Access-Control-Allow-Origin", origin)
	}
	s.db.TouchStatusAPIKey(ctx, k.ID)
	body, _ := json.Marshal(s.statusV1(ctx, p))
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "private, max-age=15")
	_, _ = w.Write(body)
}

// ---------- Telegram helper ----------

// handleTelegramChats lists the chats a bot has seen, so the chat id does not
// have to be dug out by hand — Uptime Kuma's "auto get".
func (s *Server) handleTelegramChats(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BotToken string `json:"botToken"`
		APIBase  string `json:"apiBase"`
		Proxy    string `json:"proxy"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.BotToken) == "" {
		writeErr(w, http.StatusBadRequest, "bot token is required")
		return
	}
	base := strings.TrimRight(strings.TrimSpace(in.APIBase), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if in.Proxy != "" {
		if u, err := url.Parse(in.Proxy); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	cl := &http.Client{Transport: tr, Timeout: 20 * time.Second}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, base+"/bot"+strings.TrimSpace(in.BotToken)+"/getUpdates?limit=100", nil)
	resp, err := cl.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not reach Telegram: "+shortErr(err))
		return
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      []struct {
			Message      *tgWrap `json:"message"`
			ChannelPost  *tgWrap `json:"channel_post"`
			MyChatMember *tgWrap `json:"my_chat_member"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil || !out.OK {
		writeErr(w, http.StatusBadGateway, "Telegram said: "+firstSet(out.Description, "bad answer"))
		return
	}
	seen := map[int64]bool{}
	chats := []map[string]any{}
	for _, u := range out.Result {
		for _, c := range []*tgChat{chatOf(u.Message), chatOf(u.ChannelPost), chatOf(u.MyChatMember)} {
			if c == nil || seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			name := strings.TrimSpace(c.Title + " " + c.FirstName + " " + c.LastName)
			if c.Username != "" {
				name += " (@" + c.Username + ")"
			}
			chats = append(chats, map[string]any{"id": fmt.Sprint(c.ID), "type": c.Type, "name": strings.TrimSpace(name)})
		}
	}
	writeJSON(w, chats)
}

type tgChat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type tgWrap struct {
	Chat tgChat `json:"chat"`
}

func chatOf(v *tgWrap) *tgChat {
	if v == nil {
		return nil
	}
	return &v.Chat
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i > 0 && i < len(s)-2 {
		return s[i+2:]
	}
	return s
}
