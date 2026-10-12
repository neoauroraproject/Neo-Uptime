package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/auth"
	"github.com/neoauroraproject/Neo-Uptime/internal/certs"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

var countryRe = regexp.MustCompile(`^[a-z]{2}$`)

func normCountry(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	if countryRe.MatchString(c) {
		return c
	}
	return ""
}

// handleAgentReport receives a report from neo-uptime-agent.
func (s *Server) handleAgentReport(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if blocked, _ := s.limiter.Blocked(ip); blocked {
		writeStatus(w, http.StatusTooManyRequests, agentproto.Ack{Error: "too many failed attempts"})
		return
	}
	token := bearer(r)
	if token == "" {
		writeStatus(w, http.StatusUnauthorized, agentproto.Ack{Error: "missing token"})
		return
	}
	a, err := s.db.GetAgentByTokenHash(r.Context(), auth.HashToken(token))
	if err != nil {
		s.limiter.Fail(ip)
		writeStatus(w, http.StatusUnauthorized, agentproto.Ack{Error: "unknown agent token"})
		return
	}
	var rep agentproto.Report
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&rep); err != nil {
		writeStatus(w, http.StatusBadRequest, agentproto.Ack{Error: "invalid report: " + err.Error()})
		return
	}
	reply := s.mgr.IngestAgentReport(r.Context(), a, &rep, ip)
	writeJSON(w, agentproto.Ack{OK: true, Interval: a.Interval, Name: a.Name, Probes: reply.Probes, Jobs: reply.Jobs})
}

type agentView struct {
	*store.Agent
	Online bool               `json:"online"`
	Info   *monitor.AgentInfo `json:"info,omitempty"`
}

func (s *Server) agentView(a *store.Agent) agentView {
	st := s.agents.Get(a.ID)
	v := agentView{Agent: a, Info: st.Info()}
	if v.Info != nil {
		interval := max(a.Interval, 5)
		v.Online = st.ReceivedAt.Unix() > 0 && int(time.Since(st.ReceivedAt).Seconds()) < max(3*interval, 60)
	}
	return v
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListAgents(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]agentView, 0, len(list))
	for _, a := range list {
		out = append(out, s.agentView(a))
	}
	writeJSON(w, out)
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	a, err := s.db.GetAgent(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := map[string]any{"agent": s.agentView(a)}
	if st := s.agents.Get(id); st != nil && st.Report != nil {
		out["backpack"] = st.Report.Backpack
		out["kariz"] = st.Report.Kariz
	}
	imported := map[string]int64{}
	if list, err := s.db.ListMonitors(r.Context()); err == nil {
		for _, m := range list {
			if m.Type == monitor.TypeAgentTunnel && m.Config.AgentID == id {
				imported[m.Config.Source+"/"+m.Config.TunnelName] = m.ID
			}
		}
	}
	out["imported"] = imported
	out["install"] = s.installCommand(r, "")
	out["addresses"] = s.addresses(r)
	writeJSON(w, out)
}

// installCommand is the one-liner that installs the agent on a server.
// agentAddresses are the ways an agent can reach this panel: straight to the
// server's IP and port, through the public address (a domain), or through a
// tunnel that forwards the panel's port to 127.0.0.1 on the agent's server.
type agentAddresses struct {
	Direct string `json:"direct"` // http://IP:port
	Public string `json:"public"` // the public address, if it differs
	Port   int    `json:"port"`
	IP     string `json:"ip"`
}

func (s *Server) addresses(r *http.Request) agentAddresses {
	a := agentAddresses{Port: s.port}
	public := strings.TrimRight(s.db.GetSetting(r.Context(), "primaryBaseURL", ""), "/")
	if public == "" {
		scheme := "http"
		if s.secureRequest(r) {
			scheme = "https"
		}
		host := r.Host
		if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
			host = fh
		}
		public = scheme + "://" + host
	}
	// The IP: the one the admin is using, else this machine's public one.
	if ip := net.ParseIP(certs.HostOnly(r.Host)); ip != nil && !ip.IsLoopback() {
		a.IP = ip.String()
	}
	if a.IP == "" || net.ParseIP(a.IP).IsPrivate() {
		var ips []string
		if s.certs != nil {
			ips = s.certs.ServerIPs()
		} else if addrs, err := net.InterfaceAddrs(); err == nil {
			for _, ad := range addrs {
				if n, ok := ad.(*net.IPNet); ok && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
					ips = append(ips, n.IP.String())
				}
			}
		}
		for _, v := range ips {
			if ip := net.ParseIP(v); ip != nil && ip.To4() != nil && (a.IP == "" || !ip.IsPrivate()) {
				a.IP = v
				break
			}
		}
	}
	if a.Port == 0 {
		if _, p, err := net.SplitHostPort(r.Host); err == nil {
			a.Port, _ = strconv.Atoi(p)
		}
	}
	if a.IP != "" && a.Port > 0 {
		host := a.IP
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		a.Direct = fmt.Sprintf("http://%s:%d", host, a.Port)
	}
	if public != a.Direct {
		a.Public = public
	}
	return a
}

// installCommand is the one-line agent install. Reports go to the server's IP
// first and to the public address if the IP cannot be reached.
func (s *Server) installCommand(r *http.Request, token string) string {
	a := s.addresses(r)
	var to []string
	for _, v := range []string{a.Direct, a.Public} {
		if v != "" {
			to = append(to, v)
		}
	}
	if token == "" {
		token = "<TOKEN>"
	}
	return fmt.Sprintf("bash <(curl -fsSL https://raw.githubusercontent.com/%s/main/agent.sh) --server %s --token %s", version.Repo, strings.Join(to, ","), token)
}

func newAgentToken() (string, string, string) {
	secret := "na_" + auth.RandomToken(24)
	return secret, auth.HashToken(secret), secret[:9]
}

func (s *Server) handleSaveAgent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string `json:"name"`
		Country    string `json:"country"`
		Interval   int    `json:"interval"`
		AutoImport *bool  `json:"autoImport"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		writeErr(w, http.StatusBadRequest, "give the server a name (up to 80 characters)")
		return
	}
	if in.Interval == 0 {
		in.Interval = 20
	}
	in.Interval = max(5, min(300, in.Interval))
	ctx := r.Context()
	if id, ok := pathID(r, "id"); ok {
		a, err := s.db.GetAgent(ctx, id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		a.Name, a.Country, a.Interval = in.Name, normCountry(in.Country), in.Interval
		if in.AutoImport != nil {
			a.AutoImport = *in.AutoImport
		}
		if err := s.db.SaveAgent(ctx, a); err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, s.agentView(a))
		return
	}
	secret, hash, prefix := newAgentToken()
	a := &store.Agent{Name: in.Name, Country: normCountry(in.Country), Interval: in.Interval, AutoImport: true, TokenHash: hash, TokenPrefix: prefix}
	if in.AutoImport != nil {
		a.AutoImport = *in.AutoImport
	}
	if err := s.db.SaveAgent(ctx, a); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"agent": s.agentView(a), "token": secret, "install": s.installCommand(r, secret), "addresses": s.addresses(r)})
}

func (s *Server) handleAgentToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	a, err := s.db.GetAgent(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	secret, hash, prefix := newAgentToken()
	a.TokenHash, a.TokenPrefix = hash, prefix
	if err := s.db.SaveAgent(r.Context(), a); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"token": secret, "install": s.installCommand(r, secret), "addresses": s.addresses(r)})
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ctx := r.Context()
	if r.URL.Query().Get("deleteMonitors") == "1" {
		a, _ := s.db.GetAgent(ctx, id)
		if list, err := s.db.ListMonitors(ctx); err == nil {
			for _, m := range list {
				mine := (m.Type == monitor.TypeAgent || m.Type == monitor.TypeAgentTunnel) && m.Config.AgentID == id
				group := a != nil && a.GroupMonitorID != nil && *a.GroupMonitorID == m.ID
				if mine || group {
					if err := s.db.DeleteMonitor(ctx, m.ID); err == nil {
						s.mgr.Remove(m.ID)
					}
				}
			}
		}
	}
	if err := s.db.DeleteAgent(ctx, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.agents.Forget(id)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleImportAgentTunnels(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var in struct {
		Keys []string `json:"keys"`
		monitor.ImportOptions
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.mgr.ImportAgentTunnels(r.Context(), id, in.Keys, &in.ImportOptions)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, res)
}
