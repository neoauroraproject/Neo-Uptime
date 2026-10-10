package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/auth"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

const sessionCookie = "neo_session"

type role int

const (
	roleViewer role = iota
	roleEditor
	roleAdmin
)

func parseRole(s string) role {
	switch s {
	case "admin":
		return roleAdmin
	case "editor":
		return roleEditor
	}
	return roleViewer
}

// caller is who made a request.
type caller struct {
	Kind        string // session | apikey
	UserID      int64
	Username    string
	Role        role
	SessionHash string
	KeyName     string
}

type ctxKey struct{}

func callerFrom(r *http.Request) *caller {
	c, _ := r.Context().Value(ctxKey{}).(*caller)
	return c
}

func isUnsafe(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
}

// sameOriginRequest is the CSRF defence. A cross-site page cannot attach a
// custom header without a CORS preflight, and this server never answers one,
// so the header's presence proves the request came from our own app. The
// Origin check on top catches anything that slips past that.
func (s *Server) sameOriginRequest(r *http.Request) bool {
	if r.Header.Get("X-Neo-Request") != "1" {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return true
	}
	o = strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://")
	// Behind a reverse proxy the Host we see may be the upstream address; the
	// forwarded host, or the configured public URL, is then what the browser used.
	if o == r.Host || o == r.Header.Get("X-Forwarded-Host") {
		return true
	}
	if base := s.db.GetSetting(r.Context(), "primaryBaseURL", ""); base != "" {
		base = strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(base, "/"), "https://"), "http://")
		if h, _, _ := strings.Cut(base, "/"); h == o {
			return true
		}
	}
	return false
}

func (s *Server) csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isUnsafe(r) && !s.sameOriginRequest(r) {
			writeErr(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		next(w, r)
	}
}

func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if t, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(t)
		}
		if strings.HasPrefix(h, "Basic ") {
			// Prometheus basic_auth: any username, the key as password.
			if _, pw, ok := r.BasicAuth(); ok {
				return pw
			}
		}
	}
	return ""
}

func (s *Server) identify(r *http.Request) (*caller, error) {
	ctx := r.Context()
	if key := bearer(r); key != "" {
		ip := s.clientIP(r)
		if blocked, _ := s.limiter.Blocked(ip); blocked {
			return nil, errTooMany
		}
		k, err := s.db.GetAPIKeyByHash(ctx, auth.HashToken(key))
		if err != nil || !k.Active || (k.ExpiresAt > 0 && time.Now().Unix() >= k.ExpiresAt) {
			s.limiter.Fail(ip)
			return nil, errBadCredential
		}
		s.db.TouchAPIKey(ctx, k.ID)
		rl := roleViewer
		if k.Scope == "write" {
			rl = roleEditor
		}
		return &caller{Kind: "apikey", UserID: k.UserID, Role: rl, KeyName: k.Name, Username: "key:" + k.Name}, nil
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, errNoCredential
	}
	hash := auth.HashToken(c.Value)
	sess, err := s.db.GetSession(ctx, hash)
	if err != nil {
		return nil, errNoCredential
	}
	u, err := s.db.GetUser(ctx, sess.UserID)
	if err != nil {
		return nil, errNoCredential
	}
	if time.Since(time.Unix(sess.LastSeen, 0)) > 10*time.Minute {
		exp := sess.ExpiresAt
		if sess.ExpiresAt-sess.LastSeen > int64(13*time.Hour/time.Second) {
			exp = time.Now().Add(30 * 24 * time.Hour).Unix()
		}
		s.db.TouchSession(ctx, hash, exp)
	}
	return &caller{Kind: "session", UserID: u.ID, Username: u.Username, Role: parseRole(u.Role), SessionHash: hash}, nil
}

var (
	errNoCredential  = errors.New("sign in required")
	errBadCredential = errors.New("invalid API key")
	errTooMany       = errors.New("too many failed attempts — try again later")
)

func (s *Server) guard(need role, sessionOnly bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		who, err := s.identify(r)
		if err != nil {
			code := http.StatusUnauthorized
			if errors.Is(err, errTooMany) {
				code = http.StatusTooManyRequests
			}
			writeErr(w, code, err.Error())
			return
		}
		if sessionOnly && who.Kind != "session" {
			writeErr(w, http.StatusForbidden, "this needs a signed-in browser session, not an API key")
			return
		}
		if who.Role < need {
			writeErr(w, http.StatusForbidden, "your account is not allowed to do this")
			return
		}
		if who.Kind == "session" && isUnsafe(r) && !s.sameOriginRequest(r) {
			writeErr(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, who)))
	}
}

func (s *Server) need(rl role, next http.HandlerFunc) http.HandlerFunc {
	return s.guard(rl, false, next)
}
func (s *Server) session(next http.HandlerFunc) http.HandlerFunc {
	return s.guard(roleViewer, true, next)
}
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc { return s.guard(roleAdmin, true, next) }

func (s *Server) secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return s.db.GetSetting(r.Context(), "trustProxy", "false") == "true" &&
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User, remember bool) error {
	token := auth.RandomToken(32)
	life := 12 * time.Hour
	if remember {
		life = 30 * 24 * time.Hour
	}
	n := time.Now()
	ua := r.UserAgent()
	if len(ua) > 250 {
		ua = ua[:250]
	}
	sess := &store.Session{
		TokenHash: auth.HashToken(token), UserID: u.ID, CreatedAt: n.Unix(), ExpiresAt: n.Add(life).Unix(),
		LastSeen: n.Unix(), IP: s.clientIP(r), UserAgent: ua,
	}
	if err := s.db.CreateSession(r.Context(), sess); err != nil {
		return err
	}
	c := &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.secureRequest(r),
	}
	if remember {
		c.MaxAge = int(life.Seconds())
	}
	http.SetCookie(w, c)
	s.db.TouchLogin(r.Context(), u.ID)
	return nil
}

func (s *Server) handleAuthState(w http.ResponseWriter, r *http.Request) {
	n, err := s.db.CountUsers(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := map[string]any{
		"setupRequired": n == 0,
		"siteTitle":     s.db.GetSetting(r.Context(), "siteTitle", "Neo Uptime"),
		"version":       version.Version,
	}
	if who, err := s.identify(r); err == nil && who.Kind == "session" {
		if u, err := s.db.GetUser(r.Context(), who.UserID); err == nil {
			out["user"] = u
		}
	}
	writeJSON(w, out)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
	Remember bool   `json:"remember"`
}

func validUsername(u string) error {
	if len(u) < 3 || len(u) > 40 {
		return errors.New("username must be 3–40 characters")
	}
	for _, c := range u {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return errors.New("username may only contain letters, digits, dot, dash and underscore")
		}
	}
	return nil
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if err := validUsername(in.Username); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.db.CountUsers(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if n > 0 {
		writeErr(w, http.StatusConflict, "setup has already been completed")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hashing failed")
		return
	}
	u := &store.User{Username: in.Username, PasswordHash: hash, Role: "admin"}
	if err := s.db.CreateUser(r.Context(), u); err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := s.startSession(w, r, u, true); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"user": u})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if blocked, left := s.limiter.Blocked(ip); blocked {
		writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("too many failed attempts — try again in %d minutes", int(left.Minutes())+1))
		return
	}
	var in credentials
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.db.GetUserByName(r.Context(), strings.TrimSpace(in.Username))
	if err != nil {
		auth.BurnTime(in.Password)
		s.limiter.Fail(ip)
		writeErr(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		s.limiter.Fail(ip)
		writeErr(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	if u.TOTPEnabled {
		if strings.TrimSpace(in.TOTP) == "" {
			writeStatus(w, http.StatusUnauthorized, apiError{Error: "enter the code from your authenticator app", Code: "totp_required"})
			return
		}
		if !auth.VerifyTOTP(u.TOTPSecret, in.TOTP) {
			s.limiter.Fail(ip)
			writeStatus(w, http.StatusUnauthorized, apiError{Error: "that code is not valid", Code: "totp_invalid"})
			return
		}
	}
	s.limiter.Succeed(ip)
	if err := s.startSession(w, r, u, in.Remember); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"user": u})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.db.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	who := callerFrom(r)
	out := map[string]any{"kind": who.Kind, "role": []string{"viewer", "editor", "admin"}[who.Role]}
	if who.Kind == "session" {
		if u, err := s.db.GetUser(r.Context(), who.UserID); err == nil {
			out["user"] = u
		}
	} else {
		out["key"] = who.KeyName
	}
	writeJSON(w, out)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	who := callerFrom(r)
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.db.GetUser(r.Context(), who.UserID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Current) {
		s.limiter.Fail(s.clientIP(r))
		writeErr(w, http.StatusBadRequest, "current password is wrong")
		return
	}
	if err := auth.ValidatePassword(in.New); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if u.PasswordHash, err = auth.HashPassword(in.New); err != nil {
		writeErr(w, http.StatusInternalServerError, "hashing failed")
		return
	}
	if err := s.db.UpdateUser(r.Context(), u); err != nil {
		writeStoreErr(w, err)
		return
	}
	_ = s.db.DeleteUserSessions(r.Context(), u.ID, who.SessionHash)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	who := callerFrom(r)
	u, err := s.db.GetUser(r.Context(), who.UserID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if u.TOTPEnabled {
		writeErr(w, http.StatusConflict, "two-factor sign-in is already on")
		return
	}
	u.TOTPSecret = auth.NewTOTPSecret()
	if err := s.db.UpdateUser(r.Context(), u); err != nil {
		writeStoreErr(w, err)
		return
	}
	issuer := s.db.GetSetting(r.Context(), "siteTitle", "Neo Uptime")
	writeJSON(w, map[string]any{"secret": u.TOTPSecret, "uri": auth.TOTPURI(issuer, u.Username, u.TOTPSecret)})
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	who := callerFrom(r)
	var in struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.db.GetUser(r.Context(), who.UserID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if u.TOTPSecret == "" || !auth.VerifyTOTP(u.TOTPSecret, in.Code) {
		writeErr(w, http.StatusBadRequest, "that code does not match — check the time on your phone and try again")
		return
	}
	u.TOTPEnabled = true
	if err := s.db.UpdateUser(r.Context(), u); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	who := callerFrom(r)
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.db.GetUser(r.Context(), who.UserID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		s.limiter.Fail(s.clientIP(r))
		writeErr(w, http.StatusBadRequest, "password is wrong")
		return
	}
	u.TOTPEnabled, u.TOTPSecret = false, ""
	if err := s.db.UpdateUser(r.Context(), u); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	who := callerFrom(r)
	list, err := s.db.ListSessions(r.Context(), who.UserID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	type view struct {
		*store.Session
		Current bool `json:"current"`
	}
	out := []view{}
	for _, ss := range list {
		out = append(out, view{ss, ss.TokenHash == who.SessionHash})
	}
	writeJSON(w, out)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	who := callerFrom(r)
	if err := s.db.DeleteSessionByPublicID(r.Context(), who.UserID, r.PathValue("id")); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleRevokeOthers(w http.ResponseWriter, r *http.Request) {
	who := callerFrom(r)
	if err := s.db.DeleteUserSessions(r.Context(), who.UserID, who.SessionHash); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
