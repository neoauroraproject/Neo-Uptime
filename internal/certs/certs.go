// Package certs serves HTTPS for the panel's and the status pages' own domains,
// with certificates from Let's Encrypt (ACME) that are issued and renewed
// automatically. It listens on ports 80 and 443 next to the normal listener.
package certs

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Domain is a host name that gets a certificate.
type Domain struct {
	Name   string `json:"domain"`
	Kind   string `json:"kind"` // panel | status
	PageID int64  `json:"pageId,omitempty"`
	Title  string `json:"title,omitempty"`
}

// Source tells the manager whether HTTPS is on and for which names.
type Source interface {
	HTTPSSettings(ctx context.Context) (enabled bool, email string)
	Domains() []Domain
}

// Config holds what cannot change while running.
type Config struct {
	HTTPAddr  string // ":80" — redirects and HTTP-01 challenges
	HTTPSAddr string // ":443"
	CacheDir  string // where certificates and the ACME account key are kept
	Directory string // ACME directory URL; empty means Let's Encrypt
	CARoots   string // optional PEM file of extra roots for a private ACME server

	// Shared is set when the panel itself listens on the HTTPS port: TLS
	// connections then arrive through it instead of a second listener.
	Shared *Splitter
	// SharedPlain is set when the panel listens on the HTTP port; the panel
	// then answers ACME challenges through PlainHandler.
	SharedPlain bool
}

// DomainStatus is one row of the HTTPS settings page.
type DomainStatus struct {
	Domain
	IPs        []string `json:"ips"`
	PointsHere bool     `json:"pointsHere"`
	Issued     bool     `json:"issued"`
	NotAfter   int64    `json:"notAfter,omitempty"`
	Issuer     string   `json:"issuer,omitempty"`
	Issuing    bool     `json:"issuing"`
	Error      string   `json:"error,omitempty"`
	CheckedAt  int64    `json:"checkedAt,omitempty"`
}

// Status describes the HTTPS listeners and every domain.
type Status struct {
	Enabled     bool           `json:"enabled"`
	Running     bool           `json:"running"`
	HTTPAddr    string         `json:"httpAddr"`
	HTTPSAddr   string         `json:"httpsAddr"`
	ListenError string         `json:"listenError,omitempty"`
	HTTPError   string         `json:"httpError,omitempty"`
	ServerIPs   []string       `json:"serverIps"`
	Domains     []DomainStatus `json:"domains"`
}

type attempt struct {
	busy      bool
	forceNext bool // a forced request came in while a checked one ran
	err       string
	at        int64
}

// Manager runs the HTTPS listeners.
type Manager struct {
	cfg     Config
	src     Source
	handler http.Handler

	applyMu sync.Mutex // one Apply at a time: two would race for the same ports

	mu        sync.Mutex
	ac        *autocert.Manager
	email     string
	tlsSrv    *http.Server
	plainSrv  *http.Server
	listenErr string
	httpErr   string
	attempts  map[string]*attempt
	haveCert  map[string]bool
	hints     []string // addresses the admin reached this server on

	checks sync.Map // self-check tokens served on port 80
}

// New returns a manager; nothing listens until Apply finds HTTPS turned on.
func New(cfg Config, src Source, handler http.Handler) *Manager {
	return &Manager{cfg: cfg, src: src, handler: handler, attempts: map[string]*attempt{}, haveCert: map[string]bool{}}
}

// Run applies the settings, then retries missing certificates every 15
// minutes (for a DNS record that was added later) until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	m.Apply(ctx)
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.stop()
			return
		case <-t.C:
			m.warm(false)
		}
	}
}

// Apply starts, restarts or stops the listeners to match the settings.
func (m *Manager) Apply(ctx context.Context) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	enabled, email := m.src.HTTPSSettings(ctx)
	m.mu.Lock()
	if !enabled {
		m.mu.Unlock()
		m.stop()
		return
	}
	if m.ac != nil && m.email == email {
		m.mu.Unlock()
		m.warm(false)
		return
	}
	m.mu.Unlock()
	m.stop()

	ac := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(m.cfg.CacheDir),
		HostPolicy: m.hostPolicy,
		Email:      email,
		Client:     m.client(),
	}
	var tlsLn net.Listener
	if m.cfg.Shared != nil {
		tlsLn = m.cfg.Shared.TLS()
	} else {
		ln, err := net.Listen("tcp", m.cfg.HTTPSAddr)
		if err != nil {
			m.mu.Lock()
			m.listenErr = listenError(m.cfg.HTTPSAddr, err)
			m.mu.Unlock()
			log.Printf("https: %v", err)
			return
		}
		tlsLn = ln
	}
	tlsCfg := ac.TLSConfig()
	tlsCfg.MinVersion = tls.VersionTLS12
	tlsSrv := &http.Server{
		Handler:           http.HandlerFunc(m.serveTLS),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          log.New(io.Discard, "", 0), // handshakes from scanners
	}
	go func() {
		if err := tlsSrv.ServeTLS(tlsLn, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("https: %v", err)
		}
	}()

	// Port 80 is optional: TLS-ALPN challenges work on 443 alone.
	var plainSrv *http.Server
	httpErr := ""
	if m.cfg.SharedPlain {
		// The panel already listens on the HTTP port and answers challenges.
	} else if plainLn, err := net.Listen("tcp", m.cfg.HTTPAddr); err != nil {
		httpErr = listenError(m.cfg.HTTPAddr, err) + " — HTTP will not redirect to HTTPS"
		log.Printf("https: %s", httpErr)
	} else {
		plainSrv = &http.Server{
			Handler:           ac.HTTPHandler(http.HandlerFunc(m.servePlain)),
			ReadHeaderTimeout: 15 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		go func() {
			if err := plainSrv.Serve(plainLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("http: %v", err)
			}
		}()
	}

	m.mu.Lock()
	m.ac, m.email, m.tlsSrv, m.plainSrv = ac, email, tlsSrv, plainSrv
	m.listenErr, m.httpErr = "", httpErr
	m.mu.Unlock()
	log.Printf("https: automatic certificates on %s (and %s)", m.cfg.HTTPSAddr, m.cfg.HTTPAddr)
	m.warm(false)
}

// listenError explains a failed listen; a busy port is the usual cause.
func listenError(addr string, err error) string {
	_, port, _ := net.SplitHostPort(addr)
	if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") || strings.Contains(err.Error(), "Only one usage") {
		return fmt.Sprintf("port %s is used by another program (find it with: ss -ltnp 'sport = :%s'). Stop that program, or move Neo Uptime's panel to port %s so they share it", port, port, port)
	}
	return fmt.Sprintf("cannot listen on %s: %v", addr, err)
}

// PlainHandler wraps the panel when it listens on the HTTP port itself: ACME
// HTTP-01 challenges and the DNS self-check are answered there, and the rest
// goes to the panel (which redirects configured domains to HTTPS).
func (m *Manager) PlainHandler(app http.Handler) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok, ok := strings.CutPrefix(r.URL.Path, checkPrefix); ok {
			if _, ok := m.checks.Load(tok); ok {
				_, _ = io.WriteString(w, tok)
				return
			}
		}
		app.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		ac := m.ac
		m.mu.Unlock()
		if ac == nil {
			inner.ServeHTTP(w, r)
			return
		}
		ac.HTTPHandler(inner).ServeHTTP(w, r)
	})
}

func (m *Manager) stop() {
	m.mu.Lock()
	tlsSrv, plainSrv := m.tlsSrv, m.plainSrv
	m.ac, m.tlsSrv, m.plainSrv = nil, nil, nil
	m.listenErr, m.httpErr = "", ""
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if tlsSrv != nil {
		_ = tlsSrv.Shutdown(ctx)
	}
	if plainSrv != nil {
		_ = plainSrv.Shutdown(ctx)
	}
}

func (m *Manager) client() *acme.Client {
	if m.cfg.Directory == "" && m.cfg.CARoots == "" {
		return nil // Let's Encrypt
	}
	c := &acme.Client{DirectoryURL: m.cfg.Directory}
	if c.DirectoryURL == "" {
		c.DirectoryURL = autocert.DefaultACMEDirectory
	}
	if m.cfg.CARoots != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if data, err := os.ReadFile(m.cfg.CARoots); err == nil {
			pool.AppendCertsFromPEM(data)
		} else {
			log.Printf("https: reading %s: %v", m.cfg.CARoots, err)
		}
		c.HTTPClient = &http.Client{
			Timeout:   time.Minute,
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{RootCAs: pool}},
		}
	}
	return c
}

func (m *Manager) known(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, d := range m.src.Domains() {
		if d.Name == host {
			return true
		}
	}
	return false
}

func (m *Manager) hostPolicy(_ context.Context, host string) error {
	if m.known(host) {
		return nil
	}
	return fmt.Errorf("%s is not set up in Neo Uptime", host)
}

// HostOnly strips the port from a Host header.
func HostOnly(hostport string) string {
	h := hostport
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}

func (m *Manager) serveTLS(w http.ResponseWriter, r *http.Request) {
	if !m.known(HostOnly(r.Host)) {
		http.Error(w, "Neo Uptime: this address is not set up here.", http.StatusMisdirectedRequest)
		return
	}
	m.handler.ServeHTTP(w, r)
}

const checkPrefix = "/.well-known/neo-uptime/"

func (m *Manager) servePlain(w http.ResponseWriter, r *http.Request) {
	if tok, ok := strings.CutPrefix(r.URL.Path, checkPrefix); ok {
		if _, ok := m.checks.Load(tok); ok {
			_, _ = io.WriteString(w, tok)
			return
		}
		http.NotFound(w, r)
		return
	}
	host := HostOnly(r.Host)
	if !m.known(host) {
		http.Error(w, "Neo Uptime: this address is not set up here.", http.StatusNotFound)
		return
	}
	m.mu.Lock()
	secure := m.haveCert[host]
	m.mu.Unlock()
	if !secure {
		// No certificate yet: keep the page reachable over plain HTTP.
		m.handler.ServeHTTP(w, r)
		return
	}
	target := m.HTTPSURL(host) + r.URL.RequestURI()
	code := http.StatusMovedPermanently
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		code = http.StatusPermanentRedirect
	}
	http.Redirect(w, r, target, code)
}

func portSuffix(addr, def string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" || port == def {
		return ""
	}
	return ":" + port
}

// warm loads or requests a certificate for every domain. Loading one also
// starts its automatic renewal.
func (m *Manager) warm(force bool) {
	for _, d := range m.src.Domains() {
		_ = m.Issue(d.Name, force)
	}
}

// ErrNotRunning is returned when HTTPS is off or could not listen.
var ErrNotRunning = errors.New("automatic HTTPS is not running")

// Running reports whether the HTTPS listener is up.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ac != nil
}

// ListenError is why the HTTPS listener is not up, if it failed.
func (m *Manager) ListenError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listenErr
}

// Secure reports whether host can be served over HTTPS right now.
func (m *Manager) Secure(host string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ac != nil && m.haveCert[host]
}

// HTTPSURL is the https:// address of host, with the port only when it is not 443.
func (m *Manager) HTTPSURL(host string) string {
	return "https://" + host + portSuffix(m.cfg.HTTPSAddr, "443")
}

// Issue gets a certificate for name in the background. Without force it first
// checks that the name points at this server, so a missing DNS record does not
// use up Let's Encrypt's failed-validation allowance.
func (m *Manager) Issue(name string, force bool) error {
	m.mu.Lock()
	ac := m.ac
	a := m.attempts[name]
	if a == nil {
		a = &attempt{}
		m.attempts[name] = a
	}
	if ac == nil {
		m.mu.Unlock()
		return ErrNotRunning
	}
	if a.busy {
		// Do not drop an explicit request: run it when the current one ends.
		a.forceNext = a.forceNext || force
		m.mu.Unlock()
		return nil
	}
	a.busy = true
	m.mu.Unlock()

	go func() {
		for {
			err := m.obtain(ac, name, force)
			m.mu.Lock()
			if err != nil && a.forceNext && !force {
				a.forceNext, force = false, true
				m.mu.Unlock()
				continue
			}
			a.forceNext = false
			m.finish(a, name, err)
			m.mu.Unlock()
			return
		}
	}()
	return nil
}

// obtain gets (or loads) the certificate for name.
func (m *Manager) obtain(ac *autocert.Manager, name string, force bool) error {
	if !m.cached(name) && !force {
		if ips, here := m.pointsHere(name); !here {
			if len(ips) == 0 {
				return fmt.Errorf("%s has no DNS record yet", name)
			}
			return fmt.Errorf("%s points to %s, which does not look like this server", name, strings.Join(ips, ", "))
		}
	}
	_, err := ac.GetCertificate(ecdsaHello(name))
	return err
}

// finish records an attempt's outcome. The caller holds m.mu.
func (m *Manager) finish(a *attempt, name string, err error) {
	a.busy, a.at, a.err = false, time.Now().Unix(), ""
	if err != nil {
		a.err = err.Error()
		log.Printf("https: %s: %v", name, err)
		return
	}
	m.haveCert[name] = true
}

func ecdsaHello(name string) *tls.ClientHelloInfo {
	return &tls.ClientHelloInfo{
		ServerName:        name,
		CipherSuites:      []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_AES_128_GCM_SHA256},
		SignatureSchemes:  []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256},
		SupportedCurves:   []tls.CurveID{tls.CurveP256},
		SupportedVersions: []uint16{tls.VersionTLS13, tls.VersionTLS12},
	}
}

// certInfo reads the cached certificate for name.
func (m *Manager) certInfo(name string) (notAfter time.Time, issuer string, ok bool) {
	cache := autocert.DirCache(m.cfg.CacheDir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, key := range []string{name, name + "+rsa"} {
		data, err := cache.Get(ctx, key)
		if err != nil {
			continue
		}
		for len(data) > 0 {
			var b *pem.Block
			b, data = pem.Decode(data)
			if b == nil {
				break
			}
			if b.Type != "CERTIFICATE" {
				continue
			}
			c, err := x509.ParseCertificate(b.Bytes)
			if err != nil {
				break
			}
			issuer = c.Issuer.CommonName
			if len(c.Issuer.Organization) > 0 {
				issuer = c.Issuer.Organization[0] + " " + issuer
			}
			return c.NotAfter, strings.TrimSpace(issuer), true
		}
	}
	return time.Time{}, "", false
}

func (m *Manager) cached(name string) bool {
	na, _, ok := m.certInfo(name)
	return ok && time.Until(na) > 0
}

// pointsHere resolves name and checks it against this server's addresses,
// then — if port 80 is ours — by fetching a one-off token through the name.
func (m *Manager) pointsHere(name string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, _ := net.DefaultResolver.LookupHost(ctx, name)
	if len(ips) == 0 {
		return nil, false
	}
	mine := m.serverIPs()
	for _, ip := range ips {
		if slices.Contains(mine, ip) {
			return ips, true
		}
	}
	return ips, m.selfCheck(ctx, name)
}

func (m *Manager) selfCheck(ctx context.Context, name string) bool {
	m.mu.Lock()
	running := m.plainSrv != nil || (m.cfg.SharedPlain && m.ac != nil)
	m.mu.Unlock()
	if !running {
		return false
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	m.checks.Store(tok, struct{}{})
	defer m.checks.Delete(tok)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+name+portSuffix(m.cfg.HTTPAddr, "80")+checkPrefix+tok, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	return resp.StatusCode == http.StatusOK && string(body) == tok
}

// serverIPs lists this machine's addresses plus the ones the admin used, the
// likeliest public address first: that is the one an A record should use.
func (m *Manager) serverIPs() []string {
	m.mu.Lock()
	out := slices.Clone(m.hints)
	m.mu.Unlock()
	var ips []net.IP
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
				ips = append(ips, ipn.IP)
			}
		}
	}
	rank := func(ip net.IP) int {
		switch {
		case !ip.IsPrivate() && ip.To4() != nil:
			return 0
		case !ip.IsPrivate():
			return 1
		case ip.To4() != nil:
			return 2
		}
		return 3
	}
	slices.SortStableFunc(ips, func(a, b net.IP) int { return rank(a) - rank(b) })
	for _, ip := range ips {
		if s := ip.String(); !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// Hint records an address this server was reached on (the admin's Host header).
func (m *Manager) Hint(host string) {
	ip := net.ParseIP(HostOnly(host))
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() {
		return
	}
	m.mu.Lock()
	if !slices.Contains(m.hints, ip.String()) {
		m.hints = append(m.hints, ip.String())
	}
	m.mu.Unlock()
}

// Status reports the listeners and checks every domain.
func (m *Manager) Status(ctx context.Context) Status {
	enabled, _ := m.src.HTTPSSettings(ctx)
	m.mu.Lock()
	st := Status{
		Enabled: enabled, Running: m.ac != nil, HTTPAddr: m.cfg.HTTPAddr, HTTPSAddr: m.cfg.HTTPSAddr,
		ListenError: m.listenErr, HTTPError: m.httpErr,
	}
	m.mu.Unlock()
	st.ServerIPs = m.serverIPs()
	st.Domains = []DomainStatus{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, d := range m.src.Domains() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ds := DomainStatus{Domain: d, IPs: []string{}}
			ips, here := m.pointsHere(d.Name)
			if ips != nil {
				ds.IPs = ips
			}
			ds.PointsHere = here
			if na, issuer, ok := m.certInfo(d.Name); ok {
				ds.Issued, ds.NotAfter, ds.Issuer = time.Until(na) > 0, na.Unix(), issuer
			}
			m.mu.Lock()
			if a := m.attempts[d.Name]; a != nil {
				ds.Issuing, ds.CheckedAt = a.busy, a.at
				if !ds.Issued {
					ds.Error = a.err
				}
			}
			if ds.Issued {
				m.haveCert[d.Name] = true
			}
			m.mu.Unlock()
			mu.Lock()
			st.Domains = append(st.Domains, ds)
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.SortFunc(st.Domains, func(a, b DomainStatus) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind) // panel first
		}
		return strings.Compare(a.Name, b.Name)
	})
	return st
}
