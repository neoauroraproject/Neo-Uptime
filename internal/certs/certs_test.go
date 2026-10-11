package certs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSource struct{ names []string }

func (f fakeSource) HTTPSSettings(context.Context) (bool, string) { return false, "" }
func (f fakeSource) Domains() []Domain {
	out := []Domain{}
	for _, n := range f.names {
		out = append(out, Domain{Name: n, Kind: "status"})
	}
	return out
}

func TestHostOnly(t *testing.T) {
	for in, want := range map[string]string{
		"Status.Example.com:443": "status.example.com",
		"status.example.com.":    "status.example.com",
		"[::1]:7788":             "::1",
		"10.0.0.1":               "10.0.0.1",
	} {
		if got := HostOnly(in); got != want {
			t.Errorf("HostOnly(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlainAndTLSRouting(t *testing.T) {
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "app:"+r.Host) })
	m := New(Config{HTTPAddr: ":80", HTTPSAddr: ":8443", CacheDir: t.TempDir()}, fakeSource{names: []string{"status.example.com"}}, app)

	get := func(h http.HandlerFunc, host, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://"+host+path, nil)
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}

	// Unknown names get nothing — not the panel.
	if w := get(m.servePlain, "evil.example.com", "/"); w.Code != 404 {
		t.Fatalf("unknown host on port 80: %d", w.Code)
	}
	if w := get(m.serveTLS, "evil.example.com", "/"); w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("unknown host on 443: %d", w.Code)
	}
	if err := m.hostPolicy(context.Background(), "evil.example.com"); err == nil {
		t.Fatal("host policy allowed an unknown name")
	}
	if err := m.hostPolicy(context.Background(), "STATUS.example.com"); err != nil {
		t.Fatalf("host policy refused a known name: %v", err)
	}

	// Without a certificate the page stays reachable over HTTP…
	if w := get(m.servePlain, "status.example.com", "/x?y=1"); w.Code != 200 || w.Body.String() != "app:status.example.com" {
		t.Fatalf("plain HTTP before a certificate: %d %q", w.Code, w.Body.String())
	}
	// …and moves to HTTPS once there is one.
	m.haveCert["status.example.com"] = true
	w := get(m.servePlain, "status.example.com", "/x?y=1")
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "https://status.example.com:8443/x?y=1" {
		t.Fatalf("redirect: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := get(m.serveTLS, "status.example.com", "/"); w.Code != 200 || !strings.HasPrefix(w.Body.String(), "app:") {
		t.Fatalf("known host on 443: %d", w.Code)
	}

	// The DNS self-check token is answered only while it is pending.
	m.checks.Store("abc", struct{}{})
	if w := get(m.servePlain, "anything", checkPrefix+"abc"); w.Code != 200 || w.Body.String() != "abc" {
		t.Fatalf("self-check: %d %q", w.Code, w.Body.String())
	}
	if w := get(m.servePlain, "anything", checkPrefix+"nope"); w.Code != 404 {
		t.Fatalf("unknown self-check token: %d", w.Code)
	}
}
