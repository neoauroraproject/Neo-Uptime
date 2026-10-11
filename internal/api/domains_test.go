package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

func TestNormDomain(t *testing.T) {
	ok := map[string]string{
		"":                               "",
		"Status.Example.com":             "status.example.com",
		"https://status.example.com/x/y": "status.example.com",
		"status.example.com:443":         "status.example.com",
		"status.example.com.":            "status.example.com",
		"xn--mgbh0fb.example":            "xn--mgbh0fb.example",
	}
	for in, want := range ok {
		if got, err := normDomain(in); err != nil || got != want {
			t.Errorf("normDomain(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"localhost", "1.2.3.4", "exa mple.com", "-bad.example.com", "a..b.com", "example.123"} {
		if got, err := normDomain(bad); err == nil {
			t.Errorf("normDomain(%q) = %q, want an error", bad, got)
		}
	}
}

// raw sends a request with its own Host header and returns status, headers and body.
func (h *harness) raw(client *http.Client, method, host, path, body string) (int, http.Header, string) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	if host != "" {
		req.Host = host
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Neo-Request", "1")
	resp, err := client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(data)
}

func TestStatusPageDomainAndPreview(t *testing.T) {
	h := newHarness(t)
	if code, _ := h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true); code != 200 {
		t.Fatal("setup failed")
	}
	_, mon := h.do("POST", "/api/monitors", `{"name":"job","type":"push","interval":60}`, true)
	mid := int(mon["id"].(float64))
	h.do("GET", "/api/push/"+mon["pushToken"].(string)+"?status=up", "", false)

	page := fmt.Sprintf(`{"title":"Neo <Status>","slug":"main","published":true,"domain":"Status.Example.com",
		"groups":[{"name":"","monitors":[]},{"name":"Services","monitors":[{"monitorId":%d}]}]}`, mid)
	code, out := h.do("POST", "/api/status-pages", page, true)
	if code != 200 || out["domain"] != "status.example.com" {
		t.Fatalf("create page: %d %v", code, out)
	}
	pid := int(out["id"].(float64))

	noRedirect := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// The page's own domain shows the page, whatever the path…
	for _, p := range []string{"/", "/settings/users", "/dashboard"} {
		code, _, body := h.raw(noRedirect, "GET", "status.example.com", p, "")
		if code != 200 || !strings.Contains(body, `<meta name="neo-status-page" content="main">`) || !strings.Contains(body, "<title>Neo &lt;Status&gt;</title>") {
			t.Fatalf("%s on the status domain: %d %s", p, code, body)
		}
	}
	if code, hdr, _ := h.raw(noRedirect, "GET", "status.example.com", "/status/main", ""); code != 301 || hdr.Get("Location") != "/" {
		t.Fatalf("/status/main should redirect to /: %d %s", code, hdr.Get("Location"))
	}
	// …its data, without the empty group, and never cached by browsers…
	code, hdr, body := h.raw(noRedirect, "GET", "status.example.com:443", "/api/status-page/main", "")
	if code != 200 || hdr.Get("Cache-Control") != "no-cache" {
		t.Fatalf("page data: %d %s", code, hdr.Get("Cache-Control"))
	}
	var pub struct {
		Overall string `json:"overall"`
		Groups  []struct {
			Name string `json:"name"`
		} `json:"groups"`
	}
	_ = json.Unmarshal([]byte(body), &pub)
	if len(pub.Groups) != 1 || pub.Groups[0].Name != "Services" || pub.Overall == "unknown" {
		t.Fatalf("public page: %s", body)
	}
	// …and nothing of the admin.
	if _, _, body := h.raw(h.client, "GET", "status.example.com", "/metrics", ""); strings.Contains(body, "monitor_status") || !strings.Contains(body, "neo-status-page") {
		t.Errorf("/metrics on the status domain must show the page, not metrics: %.80s", body)
	}
	for _, p := range []string{"/api/dashboard", "/api/status-page/other", "/api/settings", "/api/backup"} {
		if code, _, _ := h.raw(h.client, "GET", "status.example.com", p, ""); code != 404 {
			t.Errorf("%s on the status domain: %d, want 404", p, code)
		}
	}
	if code, _, _ := h.raw(h.client, "POST", "status.example.com", "/api/auth/login", `{"username":"admin","password":"long-enough-pw"}`); code != 404 {
		t.Errorf("sign-in on the status domain: %d, want 404", code)
	}

	// A domain belongs to one page, and not to the panel too.
	if code, _ := h.do("POST", "/api/status-pages", `{"title":"Two","slug":"two","domain":"status.example.com"}`, true); code != 400 {
		t.Fatalf("second page with the same domain: %d", code)
	}
	if code, _ := h.do("PUT", "/api/settings", `{"panelDomain":"status.example.com"}`, true); code != 400 {
		t.Fatalf("panel domain equal to a page domain: %d", code)
	}
	if code, out := h.do("PUT", "/api/settings", `{"panelDomain":"https://Uptime.Example.com/"}`, true); code != 200 || out["panelDomain"] != "uptime.example.com" {
		t.Fatalf("panel domain: %d %v", code, out)
	}

	// Only an admin may change a domain.
	if code, _ := h.do("POST", "/api/users", `{"username":"editor1","password":"long-enough-pw","role":"editor"}`, true); code != 200 {
		t.Fatal("create editor failed")
	}
	jar, _ := cookiejar.New(nil)
	ed := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	if code, _, _ := h.raw(ed, "POST", "", "/api/auth/login", `{"username":"editor1","password":"long-enough-pw"}`); code != 200 {
		t.Fatal("editor sign-in failed")
	}
	if code, _, _ := h.raw(ed, "PUT", "", fmt.Sprintf("/api/status-pages/%d", pid), `{"title":"Neo","slug":"main","domain":"other.example.com"}`); code != 403 {
		t.Fatalf("editor changed the domain: %d", code)
	}
	if code, _, _ := h.raw(ed, "PUT", "", fmt.Sprintf("/api/status-pages/%d", pid), `{"title":"Neo","slug":"main","domain":"status.example.com"}`); code != 200 {
		t.Fatalf("editor saving with the same domain: %d", code)
	}

	// The editor's preview renders the unsaved draft.
	draft := fmt.Sprintf(`{"title":"Draft title","slug":"Not A Slug","barMode":"days","barDays":7,"groups":[{"name":"Draft","monitors":[{"monitorId":%d}]}]}`, mid)
	code, out = h.do("POST", "/api/status-pages/preview", draft, true)
	pg, _ := out["page"].(map[string]any)
	if code != 200 || pg["title"] != "Draft title" || pg["barDays"] != float64(7) || len(out["groups"].([]any)) != 1 {
		t.Fatalf("preview: %d %v", code, out)
	}
}

func TestUploads(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)

	upload := func(name string, data []byte) (int, map[string]any) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write(data)
		_ = mw.Close()
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/uploads", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Neo-Request", "1")
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	code, out := upload("logo.png", png)
	url, _ := out["url"].(string)
	if code != 200 || !strings.HasPrefix(url, "/uploads/") || !strings.HasSuffix(url, ".png") {
		t.Fatalf("upload png: %d %v", code, out)
	}
	code, hdr, body := h.raw(&http.Client{}, "GET", "", url, "")
	if code != 200 || hdr.Get("Content-Type") != "image/png" || !strings.Contains(hdr.Get("Content-Security-Policy"), "sandbox") || body != string(png) {
		t.Fatalf("get upload: %d %v", code, hdr)
	}
	if code, _ := upload("x.svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`)); code != 200 {
		t.Fatalf("svg upload: %d", code)
	}
	if code, _ := upload("notes.txt", []byte("hello, not an image")); code != 400 {
		t.Fatalf("text upload accepted: %d", code)
	}
	if code, _ := upload("big.png", append(png, make([]byte, 3<<20)...)); code != 413 {
		t.Fatalf("oversized upload: %d", code)
	}

	// The page keeps its uploaded logo through a backup.
	h.do("POST", "/api/status-pages", `{"title":"P","slug":"p","logo":"`+url+`"}`, true)
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/backup", nil)
	req.Header.Set("X-Neo-Request", "1")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b struct {
		Uploads []struct {
			Name string `json:"name"`
		} `json:"uploads"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&b)
	if len(b.Uploads) != 1 || "/uploads/"+b.Uploads[0].Name != url {
		t.Fatalf("backup uploads: %+v", b.Uploads)
	}
}
