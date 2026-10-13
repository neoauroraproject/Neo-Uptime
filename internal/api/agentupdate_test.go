package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

// The panel gets the agent of its own release from GitHub, checks it, and
// hands it to agents; agents too old to update themselves are told so.
func TestAgentUpdate(t *testing.T) {
	old := version.Version
	version.Version = "v9.9.9"
	t.Cleanup(func() { version.Version = old })

	bin := []byte("agent v9.9.9 binary")
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: "neo-uptime-agent", Mode: 0o755, Size: int64(len(bin)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(bin)
	tw.Close()
	zw.Close()
	tgz := buf.Bytes()
	tsum := sha256.Sum256(tgz)
	downloads := 0
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v9.9.9/checksums.txt":
			fmt.Fprintf(w, "%s  neo-uptime-agent-linux-amd64.tar.gz\n%s  neo-uptime-linux-amd64.tar.gz\n", hex.EncodeToString(tsum[:]), strings.Repeat("0", 64))
		case "/v9.9.9/neo-uptime-agent-linux-amd64.tar.gz":
			downloads++
			_, _ = w.Write(tgz)
		default:
			http.NotFound(w, r)
		}
	}))
	defer releases.Close()
	t.Setenv("NEO_UPTIME_RELEASES_URL", releases.URL)

	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	_, out := h.do("POST", "/api/agents", `{"name":"Iran","interval":10,"autoImport":false}`, true)
	id := int(out["agent"].(map[string]any)["id"].(float64))
	token := out["token"].(string)

	// The binary, for the agent's token only.
	get := func(tok string) (int, []byte, http.Header) {
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/agent/binary?arch=amd64&version=v9.9.9", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b, resp.Header
	}
	if code, _, _ := get("na_wrong"); code != 401 {
		t.Fatalf("binary without a valid token: %d", code)
	}
	code, got, hdr := get(token)
	bsum := sha256.Sum256(bin)
	if code != 200 || !bytes.Equal(got, bin) || hdr.Get("X-Checksum-Sha256") != hex.EncodeToString(bsum[:]) {
		t.Fatalf("binary: %d %q %v", code, got, hdr)
	}
	get(token)
	if downloads != 1 {
		t.Fatalf("the release should be downloaded once, got %d", downloads)
	}

	// Not reporting: nothing to update.
	if code, _ := h.do("POST", fmt.Sprintf("/api/agents/%d/update", id), "", true); code != 409 {
		t.Fatalf("update of a silent agent: %d", code)
	}
	// An old agent (no jobs): it has to be updated by hand once.
	report := `{"format":1,"agentVersion":"v1.2.3","interval":10,"system":{"hostname":"ir","arch":"amd64"}}`
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/agent/report", strings.NewReader(report))
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("report: %v", err)
	}
	c := &http.Client{Jar: h.client.Jar, Timeout: 30 * time.Second}
	ureq, _ := http.NewRequest("POST", h.srv.URL+fmt.Sprintf("/api/agents/%d/update", id), nil)
	ureq.Header.Set("X-Neo-Request", "1")
	resp, err := c.Do(ureq)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"manual":true`) {
		t.Fatalf("old agent: %d %s", resp.StatusCode, body)
	}
}

// An old agent that keeps sending a wrong token must not lock out the new
// agent installed on the same server, nor anyone signing in.
func TestOldAgentTokenDoesNotBlockNewAgent(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/auth/setup", `{"username":"admin","password":"long-enough-pw"}`, true)
	_, out := h.do("POST", "/api/agents", `{"name":"Iran","interval":10,"autoImport":false}`, true)
	good := out["token"].(string)
	report := func(tok string) int {
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/agent/report", strings.NewReader(`{"format":1,"interval":10,"system":{"hostname":"ir"}}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if report(good) != 200 {
		t.Fatal("the right token must work")
	}
	var last int
	for i := 0; i < 150; i++ { // an old agent, every few seconds, for hours
		last = report("na_old_token_that_was_replaced")
	}
	if last != 401 && last != 429 {
		t.Fatalf("a wrong token: %d", last)
	}
	if code := report(good); code != 200 {
		t.Fatalf("the new agent was locked out: HTTP %d", code)
	}
	// Signing in is a separate matter.
	if code, _ := h.do("POST", "/api/auth/login", `{"username":"admin","password":"long-enough-pw"}`, true); code != 200 {
		t.Fatalf("sign-in after the agent noise: %d", code)
	}
}
