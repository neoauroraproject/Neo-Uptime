// Package api is the HTTP layer: the admin REST API, the public status page
// API, push, badges, metrics, live events and the embedded web app.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/auth"
	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/certs"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// Server is the HTTP handler.
type Server struct {
	db      *store.DB
	mgr     *monitor.Manager
	bp      *backpack.Cache
	agents  *monitor.AgentHub
	hub     *Hub
	static  fs.FS
	limiter *auth.Limiter
	started time.Time
	mux     *http.ServeMux
	dataDir string
	hosts   hostTable
	certs   *certs.Manager // nil until EnableHTTPS
}

// New builds the server. static is the built web app.
func New(db *store.DB, mgr *monitor.Manager, bp *backpack.Cache, agents *monitor.AgentHub, hub *Hub, static fs.FS, dataDir string) *Server {
	s := &Server{
		db: db, mgr: mgr, bp: bp, agents: agents, hub: hub, static: static,
		limiter: auth.NewLimiter(8, 15*time.Minute),
		started: time.Now(),
		mux:     http.NewServeMux(),
		dataDir: dataDir,
	}
	s.routes()
	s.reloadHosts(context.Background())
	return s
}

func (s *Server) routes() {
	m := s.mux
	// public
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"ok": true}) })
	m.HandleFunc("GET /api/auth/state", s.handleAuthState)
	m.HandleFunc("POST /api/auth/setup", s.csrf(s.handleSetup))
	m.HandleFunc("POST /api/auth/login", s.csrf(s.handleLogin))
	m.HandleFunc("POST /api/auth/logout", s.csrf(s.handleLogout))
	m.HandleFunc("GET /api/status-page/{slug}", s.handlePublicStatusPage)
	m.HandleFunc("GET /api/status-page/{slug}/chart/{id}", s.handlePublicChart)
	m.HandleFunc("GET /api/push/{token}", s.handlePush)
	m.HandleFunc("POST /api/push/{token}", s.handlePush)
	m.HandleFunc("GET /api/badge/{id}/{kind}", s.handleBadge)
	m.HandleFunc("GET /metrics", s.need(roleViewer, s.handleMetrics))
	m.HandleFunc("GET /robots.txt", s.handleRobots)
	m.HandleFunc("POST /api/agent/report", s.handleAgentReport)
	m.HandleFunc("GET /api/v1/status/{slug}", s.handleStatusV1)
	m.HandleFunc("OPTIONS /api/v1/status/{slug}", s.handleStatusV1)
	m.HandleFunc("GET /uploads/{name}", s.handleGetUpload)

	// signed in — viewer
	m.HandleFunc("GET /api/me", s.need(roleViewer, s.handleMe))
	m.HandleFunc("GET /api/events", s.need(roleViewer, s.handleEvents))
	m.HandleFunc("GET /api/dashboard", s.need(roleViewer, s.handleDashboard))
	m.HandleFunc("GET /api/monitors", s.need(roleViewer, s.handleListMonitors))
	m.HandleFunc("GET /api/monitors/{id}", s.need(roleViewer, s.handleGetMonitor))
	m.HandleFunc("GET /api/monitors/{id}/heartbeats", s.need(roleViewer, s.handleMonitorHeartbeats))
	m.HandleFunc("GET /api/monitors/{id}/chart", s.need(roleViewer, s.handleMonitorChart))
	m.HandleFunc("GET /api/monitors/{id}/stats", s.need(roleViewer, s.handleMonitorStats))
	m.HandleFunc("GET /api/important-events", s.need(roleViewer, s.handleImportantEvents))
	m.HandleFunc("GET /api/tags", s.need(roleViewer, s.handleListTags))
	m.HandleFunc("GET /api/notifications", s.need(roleViewer, s.handleListNotifications))
	m.HandleFunc("GET /api/notification-providers", s.need(roleViewer, s.handleProviders))
	m.HandleFunc("GET /api/status-pages", s.need(roleViewer, s.handleListStatusPages))
	m.HandleFunc("GET /api/status-pages/{id}", s.need(roleViewer, s.handleGetStatusPage))
	m.HandleFunc("GET /api/status-pages/{id}/incidents", s.need(roleViewer, s.handleListIncidents))
	m.HandleFunc("GET /api/maintenance", s.need(roleViewer, s.handleListMaintenance))
	m.HandleFunc("GET /api/backpack/panels", s.need(roleViewer, s.handleListPanels))
	m.HandleFunc("GET /api/backpack/panels/{id}/snapshot", s.need(roleViewer, s.handlePanelSnapshot))
	m.HandleFunc("GET /api/settings/public", s.need(roleViewer, s.handlePublicSettings))
	m.HandleFunc("GET /api/agents", s.need(roleViewer, s.handleListAgents))
	m.HandleFunc("GET /api/agents/{id}", s.need(roleViewer, s.handleGetAgent))

	// own account — any signed-in browser user
	m.HandleFunc("PUT /api/me/password", s.session(s.handleChangePassword))
	m.HandleFunc("POST /api/me/totp/setup", s.session(s.handleTOTPSetup))
	m.HandleFunc("POST /api/me/totp/enable", s.session(s.handleTOTPEnable))
	m.HandleFunc("POST /api/me/totp/disable", s.session(s.handleTOTPDisable))
	m.HandleFunc("GET /api/me/sessions", s.session(s.handleListSessions))
	m.HandleFunc("DELETE /api/me/sessions/{id}", s.session(s.handleDeleteSession))
	m.HandleFunc("POST /api/me/sessions/revoke-others", s.session(s.handleRevokeOthers))

	// editor
	m.HandleFunc("POST /api/monitors", s.need(roleEditor, s.handleCreateMonitor))
	m.HandleFunc("PUT /api/monitors/{id}", s.need(roleEditor, s.handleUpdateMonitor))
	m.HandleFunc("DELETE /api/monitors/{id}", s.need(roleEditor, s.handleDeleteMonitor))
	m.HandleFunc("POST /api/monitors/{id}/pause", s.need(roleEditor, s.handlePauseMonitor))
	m.HandleFunc("POST /api/monitors/{id}/resume", s.need(roleEditor, s.handleResumeMonitor))
	m.HandleFunc("POST /api/monitors/{id}/check", s.need(roleEditor, s.handleCheckNow))
	m.HandleFunc("DELETE /api/monitors/{id}/heartbeats", s.need(roleEditor, s.handleClearHeartbeats))
	m.HandleFunc("DELETE /api/monitors/{id}/events", s.need(roleEditor, s.handleClearEvents))
	m.HandleFunc("POST /api/monitors/bulk", s.need(roleEditor, s.handleBulkMonitors))
	m.HandleFunc("POST /api/tags", s.need(roleEditor, s.handleSaveTag))
	m.HandleFunc("PUT /api/tags/{id}", s.need(roleEditor, s.handleSaveTag))
	m.HandleFunc("DELETE /api/tags/{id}", s.need(roleEditor, s.handleDeleteTag))
	m.HandleFunc("POST /api/notifications", s.need(roleEditor, s.handleSaveNotification))
	m.HandleFunc("PUT /api/notifications/{id}", s.need(roleEditor, s.handleSaveNotification))
	m.HandleFunc("DELETE /api/notifications/{id}", s.need(roleEditor, s.handleDeleteNotification))
	m.HandleFunc("POST /api/notifications/test", s.need(roleEditor, s.handleTestNotification))
	m.HandleFunc("POST /api/status-pages", s.need(roleEditor, s.handleSaveStatusPage))
	m.HandleFunc("PUT /api/status-pages/{id}", s.need(roleEditor, s.handleSaveStatusPage))
	m.HandleFunc("DELETE /api/status-pages/{id}", s.need(roleEditor, s.handleDeleteStatusPage))
	m.HandleFunc("POST /api/status-pages/{id}/incidents", s.need(roleEditor, s.handleSaveIncident))
	m.HandleFunc("PUT /api/status-pages/{id}/incidents/{iid}", s.need(roleEditor, s.handleSaveIncident))
	m.HandleFunc("DELETE /api/status-pages/{id}/incidents/{iid}", s.need(roleEditor, s.handleDeleteIncident))
	m.HandleFunc("POST /api/maintenance", s.need(roleEditor, s.handleSaveMaintenance))
	m.HandleFunc("PUT /api/maintenance/{id}", s.need(roleEditor, s.handleSaveMaintenance))
	m.HandleFunc("DELETE /api/maintenance/{id}", s.need(roleEditor, s.handleDeleteMaintenance))
	m.HandleFunc("POST /api/maintenance/{id}/pause", s.need(roleEditor, s.handlePauseMaintenance))
	m.HandleFunc("POST /api/maintenance/{id}/resume", s.need(roleEditor, s.handleResumeMaintenance))
	m.HandleFunc("POST /api/backpack/test", s.need(roleEditor, s.handleTestPanel))
	m.HandleFunc("POST /api/backpack/panels", s.need(roleEditor, s.handleSavePanel))
	m.HandleFunc("PUT /api/backpack/panels/{id}", s.need(roleEditor, s.handleSavePanel))
	m.HandleFunc("DELETE /api/backpack/panels/{id}", s.need(roleEditor, s.handleDeletePanel))
	m.HandleFunc("POST /api/backpack/panels/{id}/import", s.need(roleEditor, s.handleImportTunnels))
	m.HandleFunc("POST /api/agents/{id}/import", s.need(roleEditor, s.handleImportAgentTunnels))
	m.HandleFunc("POST /api/notifications/telegram/chats", s.need(roleEditor, s.handleTelegramChats))
	m.HandleFunc("POST /api/status-pages/preview", s.need(roleEditor, s.handlePreviewStatusPage))
	m.HandleFunc("POST /api/uploads", s.need(roleEditor, s.handleUpload))

	// admin, browser session only
	m.HandleFunc("GET /api/settings", s.admin(s.handleGetSettings))
	m.HandleFunc("PUT /api/settings", s.admin(s.handlePutSettings))
	m.HandleFunc("GET /api/settings/info", s.admin(s.handleInfo))
	m.HandleFunc("POST /api/settings/clear-statistics", s.admin(s.handleClearStatistics))
	m.HandleFunc("POST /api/settings/vacuum", s.admin(s.handleVacuum))
	m.HandleFunc("GET /api/users", s.admin(s.handleListUsers))
	m.HandleFunc("POST /api/users", s.admin(s.handleSaveUser))
	m.HandleFunc("PUT /api/users/{id}", s.admin(s.handleSaveUser))
	m.HandleFunc("DELETE /api/users/{id}", s.admin(s.handleDeleteUser))
	m.HandleFunc("GET /api/api-keys", s.admin(s.handleListAPIKeys))
	m.HandleFunc("POST /api/api-keys", s.admin(s.handleCreateAPIKey))
	m.HandleFunc("POST /api/api-keys/{id}/{action}", s.admin(s.handleToggleAPIKey))
	m.HandleFunc("DELETE /api/api-keys/{id}", s.admin(s.handleDeleteAPIKey))
	m.HandleFunc("GET /api/backup", s.admin(s.handleExport))
	m.HandleFunc("POST /api/backup/import", s.admin(s.handleImport))
	m.HandleFunc("POST /api/agents", s.admin(s.handleSaveAgent))
	m.HandleFunc("PUT /api/agents/{id}", s.admin(s.handleSaveAgent))
	m.HandleFunc("DELETE /api/agents/{id}", s.admin(s.handleDeleteAgent))
	m.HandleFunc("POST /api/agents/{id}/token", s.admin(s.handleAgentToken))
	m.HandleFunc("GET /api/https", s.admin(s.handleHTTPSStatus))
	m.HandleFunc("POST /api/https/issue", s.admin(s.handleHTTPSIssue))

	// owner only
	m.HandleFunc("GET /api/status-api-keys", s.owner(s.handleListStatusKeys))
	m.HandleFunc("POST /api/status-api-keys", s.owner(s.handleSaveStatusKey))
	m.HandleFunc("PUT /api/status-api-keys/{id}", s.owner(s.handleSaveStatusKey))
	m.HandleFunc("DELETE /api/status-api-keys/{id}", s.owner(s.handleDeleteStatusKey))

	m.HandleFunc("/", s.handleStatic)
}

// ServeHTTP adds security headers and recovers from panics.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, p)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	}()
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	csp := "default-src 'self'; img-src 'self' data: https: http:; style-src 'self' 'unsafe-inline'; " +
		"script-src 'self'; connect-src 'self'; font-src 'self' data:; object-src 'none'; base-uri 'self'"
	if strings.HasPrefix(r.URL.Path, "/status/") || strings.HasPrefix(r.URL.Path, "/api/badge/") || strings.HasPrefix(r.URL.Path, "/embed") {
		// Status pages and badges are made to be embedded elsewhere.
		h.Set("Content-Security-Policy", csp)
	} else {
		h.Set("Content-Security-Policy", csp+"; frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")
	}
	if s.httpsRedirect(w, r) {
		return
	}
	if pg, ok := s.hosts.page(certs.HostOnly(r.Host)); ok {
		s.serveStatusHost(w, r, pg)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// ---------- static web app ----------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "no such endpoint")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.URL.Path == "/" {
		if entry := s.db.GetSetting(r.Context(), "entryPage", "dashboard"); strings.HasPrefix(entry, "status:") {
			http.Redirect(w, r, "/status/"+strings.TrimPrefix(entry, "status:"), http.StatusFound)
			return
		}
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p != "" && p != "index.html" {
		if f, err := s.static.Open(p); err == nil {
			st, err := f.Stat()
			f.Close()
			if err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				http.ServeFileFS(w, r, s.static, p)
				return
			}
		}
		if strings.HasPrefix(p, "assets/") {
			http.NotFound(w, r)
			return
		}
	}
	s.serveIndex(w, pageHost{})
}

func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if s.db.GetSetting(r.Context(), "searchEngineIndex", "false") == "true" {
		_, _ = io.WriteString(w, "User-agent: *\nAllow: /status/\nDisallow: /\n")
		return
	}
	_, _ = io.WriteString(w, "User-agent: *\nDisallow: /\n")
}

// ---------- helpers ----------

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeStatus(w, code, apiError{Error: msg})
}

func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		writeErr(w, http.StatusConflict, "that name is already taken")
		return
	}
	log.Printf("api: %v", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func queryInt(r *http.Request, name string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return max(lo, min(hi, v))
}

func (s *Server) clientIP(r *http.Request) string {
	if s.db.GetSetting(r.Context(), "trustProxy", "false") == "true" {
		for _, h := range []string{"CF-Connecting-IP", "X-Real-IP"} {
			if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
				return v
			}
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			first, _, _ := strings.Cut(v, ",")
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func bgCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
