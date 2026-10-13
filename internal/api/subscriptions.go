package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/xray"
)

// checkTestURL accepts an empty value (the default) or an http(s) address.
func checkTestURL(s string) error {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("the test address must start with http:// or https://")
	}
	return nil
}

type subIn struct {
	Name           string   `json:"name"`
	URL            string   `json:"url"`
	AgentID        int64    `json:"agentId"`
	Interval       int      `json:"interval"`
	Timeout        int      `json:"timeout"`
	TestURL        string   `json:"testUrl"`
	AutoImport     *bool    `json:"autoImport"`
	RefreshMinutes *int     `json:"refreshMinutes"`
	Keys           []string `json:"keys"`
	monitor.ImportOptions
}

func (s *Server) applySubIn(ctx context.Context, x *store.Subscription, in *subIn) error {
	x.Name = strings.TrimSpace(in.Name)
	x.URL = strings.TrimSpace(in.URL)
	u, err := url.Parse(x.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("the subscription link must start with http:// or https://")
	}
	if x.Name == "" {
		x.Name = u.Hostname()
	}
	if len(x.Name) > 100 {
		return errors.New("the name is too long")
	}
	if in.AgentID != 0 {
		if _, err := s.db.GetAgent(ctx, in.AgentID); err != nil {
			return errors.New("the server chosen to run the tests does not exist")
		}
	}
	x.AgentID = in.AgentID
	x.Interval = in.Interval
	if x.Interval == 0 {
		x.Interval = 60
	}
	if x.Interval < 20 || x.Interval > 86400 {
		return errors.New("test every 20 seconds to 24 hours")
	}
	x.Timeout = in.Timeout
	if x.Timeout <= 0 {
		x.Timeout = 15
	}
	x.Timeout = max(5, min(60, x.Interval, x.Timeout))
	x.TestURL = strings.TrimSpace(in.TestURL)
	if err := checkTestURL(x.TestURL); err != nil {
		return err
	}
	if in.AutoImport != nil {
		x.AutoImport = *in.AutoImport
	}
	if in.RefreshMinutes != nil {
		x.RefreshMinutes = max(0, min(10080, *in.RefreshMinutes))
	}
	return nil
}

func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListSubscriptions(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if who := callerFrom(r); who != nil && who.Role < roleEditor {
		for _, x := range list {
			x.URL = "" // it is the account's key
		}
	}
	writeJSON(w, list)
}

// handlePreviewSubscription fetches a subscription — or reads configs pasted
// as they are — and lists them, for choosing which to watch.
func (s *Server) handlePreviewSubscription(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL     string `json:"url"`
		AgentID int64  `json:"agentId"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var sub *xray.Sub
	var via string
	var err error
	raw := strings.TrimSpace(in.URL)
	pasted := !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://")
	if pasted {
		links, errs := xray.ParseList([]byte(raw))
		sub = &xray.Sub{Links: links, Errors: errs}
		if len(links) == 0 {
			err = errors.New("no usable configs in what was pasted")
		}
	} else {
		sub, via, err = s.mgr.FetchSubscription(ctx, raw, in.AgentID)
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	type item struct {
		Key      string `json:"key"`
		Name     string `json:"name"`
		Summary  string `json:"summary"`
		Protocol string `json:"protocol"`
		Endpoint string `json:"endpoint"`
		Country  string `json:"country,omitempty"`
		Label    string `json:"label"`          // the name without flag emoji
		Link     string `json:"link,omitempty"` // pasted configs only: each becomes its own monitor
	}
	items := []item{}
	for _, kl := range monitor.KeyLinks(sub.Links) {
		it := item{Key: kl.Key, Name: kl.Link.Name, Summary: kl.Link.Summary(), Protocol: kl.Link.Protocol,
			Endpoint: kl.Link.Endpoint(), Country: monitor.FlagCountry(kl.Key), Label: monitor.CleanName(kl.Key)}
		if pasted {
			it.Link = kl.Link.Raw
		}
		items = append(items, it)
	}
	if sub.Errors == nil {
		sub.Errors = []xray.LinkError{}
	}
	writeJSON(w, map[string]any{"title": sub.Title, "usage": sub.Usage, "configs": items, "unusable": sub.Errors, "via": via})
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	var in subIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	x := &store.Subscription{AutoImport: true, RefreshMinutes: 60, Skip: []string{}}
	autoName := strings.TrimSpace(in.Name) == ""
	if err := s.applySubIn(r.Context(), x, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SaveSubscription(r.Context(), x); err != nil {
		writeStoreErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	res, err := s.mgr.SyncSubscription(ctx, x.ID, in.Keys, &in.ImportOptions)
	saved, _ := s.db.GetSubscription(r.Context(), x.ID)
	// Unnamed, it takes the title the subscription gives itself.
	if title := strings.TrimSpace(saved.Info.Title); autoName && title != "" && len(title) <= 100 {
		saved.Name = title
		if s.db.SaveSubscription(r.Context(), saved) == nil {
			_ = s.mgr.ApplySubscription(r.Context(), saved)
		}
	}
	if err != nil {
		// The subscription stays and is tried again on its schedule.
		writeJSON(w, map[string]any{"subscription": saved, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"subscription": saved, "result": res})
}

func (s *Server) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	x, err := s.db.GetSubscription(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var in subIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	urlChanged := strings.TrimSpace(in.URL) != x.URL
	if err := s.applySubIn(r.Context(), x, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SaveSubscription(r.Context(), x); err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := s.mgr.ApplySubscription(r.Context(), x); err != nil {
		writeStoreErr(w, err)
		return
	}
	out := map[string]any{"subscription": x}
	if urlChanged {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		if res, err := s.mgr.SyncSubscription(ctx, x.ID, nil, nil); err != nil {
			out["error"] = err.Error()
		} else {
			out["result"] = res
		}
		out["subscription"], _ = s.db.GetSubscription(r.Context(), x.ID)
	}
	writeJSON(w, out)
}

func (s *Server) handleRefreshSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var in struct {
		Keys []string `json:"keys"`
	}
	_ = readJSON(r, &in)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	res, err := s.mgr.SyncSubscription(ctx, id, in.Keys, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	x, _ := s.db.GetSubscription(r.Context(), id)
	writeJSON(w, map[string]any{"subscription": x, "result": res})
}

// handleDeleteSubscription removes a subscription with its monitors, or
// keeps them (?keepMonitors=1) as plain config monitors.
func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ctx := r.Context()
	x, err := s.db.GetSubscription(ctx, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	keep := r.URL.Query().Get("keepMonitors") == "1"
	all, err := s.db.ListMonitors(ctx)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, m := range all {
		if m.Type != monitor.TypeXray || m.Config.SubscriptionID != x.ID {
			continue
		}
		if keep {
			m.Config.SubscriptionID, m.Config.ConfigName = 0, ""
			if err := s.db.UpdateMonitor(ctx, m); err != nil {
				writeStoreErr(w, err)
				return
			}
			s.mgr.Upsert(m)
			continue
		}
		if err := s.db.DeleteMonitor(ctx, m.ID); err != nil {
			writeStoreErr(w, err)
			return
		}
		s.mgr.Remove(m.ID)
	}
	if !keep && x.GroupMonitorID != nil {
		if g, err := s.db.GetMonitor(ctx, *x.GroupMonitorID); err == nil && g.Type == monitor.TypeGroup {
			if err := s.db.DeleteMonitor(ctx, g.ID); err == nil {
				s.mgr.Remove(g.ID)
			}
		}
	}
	if err := s.db.DeleteSubscription(ctx, x.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleTestConfig tests one config now, from this panel or a server.
func (s *Server) handleTestConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Link      string `json:"link"`
		MonitorID int64  `json:"monitorId"` // or a saved monitor's config
		AgentID   int64  `json:"agentId"`
		TestURL   string `json:"testUrl"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.MonitorID != 0 {
		m, err := s.db.GetMonitor(r.Context(), in.MonitorID)
		if err != nil || m.Type != monitor.TypeXray {
			writeErr(w, http.StatusNotFound, "no such config monitor")
			return
		}
		in.Link, in.AgentID, in.TestURL = m.Config.Link, m.Config.AgentID, m.Config.TestURL
	}
	l, err := xray.Parse(in.Link)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := checkTestURL(in.TestURL); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	out := map[string]any{"name": l.Name, "summary": l.Summary(), "endpoint": l.Endpoint()}
	delay, connect, err := s.mgr.TestConfig(ctx, in.Link, in.TestURL, in.AgentID)
	if err != nil {
		out["ok"], out["error"] = false, err.Error()
	} else {
		out["ok"], out["ping"], out["connect"] = true, float64(delay.Microseconds())/1000, float64(connect.Microseconds())/1000
	}
	writeJSON(w, out)
}

// handleParseConfig reads a link without testing it.
func (s *Server) handleParseConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Link string `json:"link"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	l, err := xray.Parse(in.Link)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"name": l.Name, "label": monitor.CleanName(l.Name), "summary": l.Summary(), "endpoint": l.Endpoint(), "protocol": l.Protocol,
		"network": l.Network, "security": l.Security, "country": monitor.FlagCountry(l.Name)})
}

// handleSpeedTest starts a speed test of a config, from this panel or from a
// server, and returns at once with the test's id; the page then polls
// handleSpeedState for how it goes. A test takes about half a minute and
// moves up to a few hundred megabytes through the config: a person starts
// it, never a schedule.
func (s *Server) handleSpeedTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MonitorID int64  `json:"monitorId"`
		Link      string `json:"link"`
		AgentID   *int64 `json:"agentId"` // empty: the monitor's own server
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var agentID int64
	if in.MonitorID != 0 {
		m, err := s.db.GetMonitor(r.Context(), in.MonitorID)
		if err != nil || m.Type != monitor.TypeXray {
			writeErr(w, http.StatusNotFound, "no such config monitor")
			return
		}
		in.Link, agentID = m.Config.Link, m.Config.AgentID
	}
	if in.AgentID != nil {
		agentID = *in.AgentID
	}
	if _, err := xray.Parse(in.Link); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.mgr.StartSpeedJob(in.MonitorID, in.Link, agentID)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

// handleSpeedState says how a speed test is going.
func (s *Server) handleSpeedState(w http.ResponseWriter, r *http.Request) {
	j, ok := s.mgr.SpeedJobState(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "no such speed test")
		return
	}
	writeJSON(w, j)
}
