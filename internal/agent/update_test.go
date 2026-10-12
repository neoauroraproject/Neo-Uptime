package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
)

func tgzWith(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: "LICENSE", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("MIT"))
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(data)
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func TestSelfUpdate(t *testing.T) {
	newBin := []byte("#!neo-uptime-agent v9.9.9")
	sum := sha256.Sum256(newBin)
	job := agentproto.Job{Kind: "update", Version: "v9.9.9", Arch: "amd64", SHA256: hex.EncodeToString(sum[:])}

	exe := filepath.Join(t.TempDir(), "neo-uptime-agent")
	reset := func() { _ = os.WriteFile(exe, []byte("old"), 0o755) }
	reset()
	oldExe, oldSvc, oldCheck, oldGH := executable, underService, checkBinary, githubReleases
	t.Cleanup(func() { executable, underService, checkBinary, githubReleases = oldExe, oldSvc, oldCheck, oldGH })
	executable = func() (string, error) { return exe, nil }
	underService = func() bool { return true }
	checkBinary = func(_ context.Context, path, want string) error {
		b, _ := os.ReadFile(path)
		if !strings.Contains(string(b), want) {
			t.Errorf("checked %q for %s", b, want)
		}
		return nil
	}

	served := newBin
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/binary" || r.Header.Get("Authorization") != "Bearer na_x" || r.URL.Query().Get("arch") != "amd64" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		_, _ = w.Write(served)
	}))
	defer panel.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// From the Neo Uptime server.
	a := New(Config{Server: panel.URL, Token: "na_x"})
	if err := a.selfUpdate(ctx, job); err != nil {
		t.Fatalf("update: %v", err)
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, newBin) {
		t.Fatalf("binary not replaced: %q", b)
	}

	// A binary that does not match the checksum is refused.
	reset()
	served = []byte("tampered")
	gh := httptest.NewServer(http.NotFoundHandler())
	githubReleases = gh.URL
	if err := a.selfUpdate(ctx, job); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered binary: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("the old binary must stay")
	}
	gh.Close()

	// The server cannot hand it out: GitHub instead.
	gh = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v9.9.9/neo-uptime-agent-linux-amd64.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(tgzWith(t, "neo-uptime-agent", newBin))
	}))
	defer gh.Close()
	githubReleases = gh.URL
	b := New(Config{Server: "http://127.0.0.1:1", Token: "na_x"})
	if err := b.selfUpdate(ctx, job); err != nil {
		t.Fatalf("update from GitHub: %v", err)
	}

	// Started by hand rather than by systemd: nothing would restart it.
	underService = func() bool { return false }
	if err := a.selfUpdate(ctx, job); err == nil || !strings.Contains(err.Error(), "systemd") {
		t.Fatalf("outside systemd: %v", err)
	}
}
