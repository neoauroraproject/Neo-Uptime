package api

import (
	"context"
	"errors"
	"html"
	"io/fs"
	"net"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/neoauroraproject/Neo-Uptime/internal/certs"
)

// A status page can have its own domain (status.example.com). Requests for
// that host only see the page: its data, static files and the public APIs —
// never the admin panel.

type pageHost struct {
	id    int64
	slug  string
	title string
}

type hostTable struct {
	mu    sync.RWMutex
	pages map[string]pageHost
	panel string
}

func (h *hostTable) page(host string) (pageHost, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.pages[host]
	return p, ok
}

// reloadHosts re-reads the status page domains and the panel domain.
func (s *Server) reloadHosts(ctx context.Context) {
	pages, err := s.db.ListStatusPages(ctx)
	if err != nil {
		return
	}
	m := map[string]pageHost{}
	for _, p := range pages {
		if p.Domain != "" {
			m[p.Domain] = pageHost{id: p.ID, slug: p.Slug, title: p.Title}
		}
	}
	panel := s.db.GetSetting(ctx, "panelDomain", "")
	s.hosts.mu.Lock()
	s.hosts.pages, s.hosts.panel = m, panel
	s.hosts.mu.Unlock()
}

// HTTPSSettings implements certs.Source.
func (s *Server) HTTPSSettings(ctx context.Context) (bool, string) {
	return s.db.GetSetting(ctx, "httpsEnabled", "false") == "true", s.db.GetSetting(ctx, "acmeEmail", "")
}

// Domains implements certs.Source.
func (s *Server) Domains() []certs.Domain {
	s.hosts.mu.RLock()
	defer s.hosts.mu.RUnlock()
	out := []certs.Domain{}
	if s.hosts.panel != "" {
		out = append(out, certs.Domain{Name: s.hosts.panel, Kind: "panel"})
	}
	for name, p := range s.hosts.pages {
		out = append(out, certs.Domain{Name: name, Kind: "status", PageID: p.id, Title: p.title})
	}
	return out
}

// EnableHTTPS starts automatic HTTPS for the configured domains; it stays
// idle until it is turned on under Settings → Domains & HTTPS.
func (s *Server) EnableHTTPS(ctx context.Context, cfg certs.Config) {
	s.certs = certs.New(cfg, s, s)
	go s.certs.Run(ctx)
}

var domainRe = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

// normDomain accepts "Status.Example.com", "https://status.example.com/" and
// the like, and returns "status.example.com" — or an error.
func normDomain(v string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(v))
	if d == "" {
		return "", nil
	}
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	d, _, _ = strings.Cut(d, "/")
	if h, _, err := net.SplitHostPort(d); err == nil {
		d = h
	}
	d = strings.TrimSuffix(d, ".")
	if net.ParseIP(d) != nil {
		return "", errors.New("use a domain name, not an IP address")
	}
	if len(d) > 253 || !domainRe.MatchString(d) {
		return "", errors.New(`"` + v + `" is not a valid domain — use the form status.example.com`)
	}
	return d, nil
}

// statusHostPath reports whether a request path may be served on a status
// page's own domain.
func statusHostPath(p, slug string) bool {
	switch {
	case p == "/" || p == "/robots.txt" || p == "/embed.js" || p == "/icon.svg" || p == "/favicon.ico":
		return true
	case strings.HasPrefix(p, "/assets/"), strings.HasPrefix(p, "/flags/"), strings.HasPrefix(p, "/uploads/"):
		return true
	case p == "/api/status-page/"+slug, strings.HasPrefix(p, "/api/status-page/"+slug+"/chart/"):
		return true
	case strings.HasPrefix(p, "/api/badge/"), strings.HasPrefix(p, "/api/v1/status/"), p == "/api/health":
		return true
	}
	return false
}

func (s *Server) serveStatusHost(w http.ResponseWriter, r *http.Request, pg pageHost) {
	p := path.Clean("/" + r.URL.Path)
	if p == "/status/"+pg.slug {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
		return
	}
	if statusHostPath(p, pg.slug) && p != "/" {
		s.mux.ServeHTTP(w, r)
		return
	}
	if strings.HasPrefix(p, "/api/") || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	// Any other path shows the page itself.
	s.serveIndex(w, pg)
}

// serveIndex sends the web app. With a page, the app shows only that status
// page, whatever the path.
func (s *Server) serveIndex(w http.ResponseWriter, pg pageHost) {
	data, err := fs.ReadFile(s.static, "index.html")
	if err != nil {
		http.Error(w, "web app not built — run `make web` before building the binary", http.StatusServiceUnavailable)
		return
	}
	if pg.slug != "" {
		doc := string(data)
		meta := `<meta name="neo-status-page" content="` + html.EscapeString(pg.slug) + `">`
		if i, j := strings.Index(doc, "<title>"), strings.Index(doc, "</title>"); i >= 0 && j > i {
			doc = doc[:i] + meta + "<title>" + html.EscapeString(pg.title) + doc[j:]
		} else if strings.Contains(doc, "</head>") {
			doc = strings.Replace(doc, "</head>", meta+"</head>", 1)
		} else {
			doc = meta + doc
		}
		data = []byte(doc)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

// ---------- Settings → Domains & HTTPS ----------

func (s *Server) handleHTTPSStatus(w http.ResponseWriter, r *http.Request) {
	if s.certs == nil {
		writeJSON(w, certs.Status{ServerIPs: []string{}, Domains: []certs.DomainStatus{}, ListenError: "automatic HTTPS is not available in this build"})
		return
	}
	s.certs.Hint(r.Host)
	writeJSON(w, s.certs.Status(r.Context()))
}

// handleHTTPSIssue requests a certificate for one domain now. With enable it
// first turns automatic HTTPS on, so a status page can get its certificate
// straight from the editor.
func (s *Server) handleHTTPSIssue(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Domain string `json:"domain"`
		Enable bool   `json:"enable"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.certs == nil {
		writeErr(w, http.StatusConflict, "automatic HTTPS is not available")
		return
	}
	known := false
	for _, d := range s.Domains() {
		known = known || d.Name == in.Domain
	}
	if !known {
		writeErr(w, http.StatusBadRequest, "that domain is not set on the panel or a status page")
		return
	}
	ctx := r.Context()
	if in.Enable && s.db.GetSetting(ctx, "httpsEnabled", "false") != "true" {
		if err := s.db.SetSettings(ctx, map[string]string{"httpsEnabled": "true"}); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	if !s.certs.Running() {
		s.certs.Apply(context.WithoutCancel(ctx))
	}
	s.certs.Hint(r.Host)
	if err := s.certs.Issue(in.Domain, true); err != nil {
		msg := s.certs.ListenError()
		if msg == "" {
			msg = "turn on automatic HTTPS first"
		}
		writeErr(w, http.StatusConflict, msg)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// httpsRedirect sends a browser that opened a domain over plain HTTP (for
// example status.example.com:7788) to its HTTPS address, without a port, once
// there is a certificate. API calls are left alone: agents, push scripts and
// scrapers keep working on any address.
func (s *Server) httpsRedirect(w http.ResponseWriter, r *http.Request) bool {
	if s.certs == nil || r.TLS != nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/.well-known/") || r.URL.Path == "/metrics" {
		return false
	}
	host := certs.HostOnly(r.Host)
	if net.ParseIP(host) != nil || s.secureRequest(r) || !s.certs.Secure(host) {
		return false
	}
	http.Redirect(w, r, s.certs.HTTPSURL(host)+r.URL.RequestURI(), http.StatusMovedPermanently)
	return true
}

// applyHTTPS reloads the domain table and lets the HTTPS listeners follow.
func (s *Server) applyHTTPS(ctx context.Context) {
	s.reloadHosts(ctx)
	if s.certs != nil {
		go s.certs.Apply(context.WithoutCancel(ctx))
	}
}
