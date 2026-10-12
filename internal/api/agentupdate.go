package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agent"
	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/auth"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

// releasesURL is where release assets come from; NEO_UPTIME_RELEASES_URL
// points it at a mirror.
func releasesURL() string {
	if v := strings.TrimRight(os.Getenv("NEO_UPTIME_RELEASES_URL"), "/"); v != "" {
		return v
	}
	return "https://github.com/" + version.Repo + "/releases/download"
}

var releaseClient = &http.Client{Timeout: 10 * time.Minute}

// agentAsset names the release asset for a Go architecture.
func agentAsset(goarch string) (string, bool) {
	switch goarch {
	case "amd64", "arm64":
		return goarch, true
	case "arm":
		return "armv7", true
	}
	return "", false
}

// panelRelease is the version this panel hands out: its own.
func panelRelease() (string, bool) {
	v := version.Version
	return v, strings.HasPrefix(v, "v") && v != "dev"
}

// agentBinary returns the agent binary of a release for one architecture,
// downloading it once and checking it against the release's checksums.
func (s *Server) agentBinary(ctx context.Context, ver, asset string) (path, sum string, err error) {
	s.binMu.Lock()
	defer s.binMu.Unlock()
	dir := filepath.Join(s.dataDir, "agent-bin", ver, asset)
	path = filepath.Join(dir, "neo-uptime-agent")
	if b, err := os.ReadFile(path + ".sha256"); err == nil {
		if _, err := os.Stat(path); err == nil {
			return path, strings.TrimSpace(string(b)), nil
		}
	}
	name := "neo-uptime-agent-linux-" + asset + ".tar.gz"
	base := releasesURL() + "/" + ver + "/"
	sums, err := download(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return "", "", fmt.Errorf("checksums: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return "", "", fmt.Errorf("%s is not in the release's checksums", name)
	}
	tgz, err := download(ctx, base+name, 200<<20)
	if err != nil {
		return "", "", err
	}
	if got := sha256.Sum256(tgz); hex.EncodeToString(got[:]) != want {
		return "", "", errors.New("the downloaded archive does not match the release checksum")
	}
	bin, err := agent.ExtractBinary(tgz, "neo-uptime-agent")
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path+".tmp", bin, 0o644); err != nil {
		return "", "", err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return "", "", err
	}
	h := sha256.Sum256(bin)
	sum = hex.EncodeToString(h[:])
	if err := os.WriteFile(path+".sha256", []byte(sum), 0o644); err != nil {
		return "", "", err
	}
	return path, sum, nil
}

func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := releaseClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// agentFromToken identifies an agent by its bearer token.
func (s *Server) agentFromToken(w http.ResponseWriter, r *http.Request) (*store.Agent, bool) {
	ip := s.clientIP(r)
	if blocked, _ := s.limiter.Blocked(ip); blocked {
		writeStatus(w, http.StatusTooManyRequests, agentproto.Ack{Error: "too many failed attempts"})
		return nil, false
	}
	token := bearer(r)
	if token == "" {
		writeStatus(w, http.StatusUnauthorized, agentproto.Ack{Error: "missing token"})
		return nil, false
	}
	a, err := s.db.GetAgentByTokenHash(r.Context(), auth.HashToken(token))
	if err != nil {
		s.limiter.Fail(ip)
		writeStatus(w, http.StatusUnauthorized, agentproto.Ack{Error: "unknown agent token"})
		return nil, false
	}
	return a, true
}

// handleAgentBinary hands an agent the binary of this panel's release, so a
// server that cannot reach GitHub can still update.
func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.agentFromToken(w, r); !ok {
		return
	}
	ver, ok := panelRelease()
	asset := r.URL.Query().Get("arch")
	if !ok || (r.URL.Query().Get("version") != "" && r.URL.Query().Get("version") != ver) {
		writeErr(w, http.StatusNotFound, "this panel does not have that release")
		return
	}
	if asset != "amd64" && asset != "arm64" && asset != "armv7" {
		writeErr(w, http.StatusBadRequest, "unknown architecture")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	path, sum, err := s.agentBinary(ctx, ver, asset)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Checksum-Sha256", sum)
	st, _ := f.Stat()
	http.ServeContent(w, r, "neo-uptime-agent", st.ModTime(), f)
}

// handleUpdateAgent moves an agent to this panel's release: the agent
// downloads it (from here, or from GitHub), checks it, swaps its binary
// and restarts.
func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
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
	ver, ok := panelRelease()
	if !ok {
		writeErr(w, http.StatusBadRequest, "this panel is a development build and has no release to hand out")
		return
	}
	st := s.agents.Get(a.ID)
	if st == nil || st.Report == nil {
		writeErr(w, http.StatusConflict, a.Name+" is not reporting — the agent must be running to update itself")
		return
	}
	if a.Version == ver {
		writeJSON(w, map[string]any{"ok": true, "version": ver, "already": true})
		return
	}
	asset, ok := agentAsset(st.Report.System.Arch)
	if !ok {
		writeErr(w, http.StatusBadRequest, "no release for this server's CPU ("+st.Report.System.Arch+")")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Minute)
	defer cancel()
	_, sum, err := s.agentBinary(ctx, ver, asset)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not get "+ver+" of the agent: "+err.Error())
		return
	}
	res, err := s.mgr.RunJob(ctx, a.ID, agentproto.Job{Kind: "update", Version: ver, Arch: asset, SHA256: sum, Timeout: 300})
	if errors.Is(err, monitor.ErrAgentTooOld) || err == nil && strings.Contains(res.Error, "does not know job") {
		// Agents before v1.3.1 cannot update themselves: once by hand.
		writeJSON(w, map[string]any{"ok": false, "manual": true, "error": a.Name + " runs an agent too old to update itself — run the update command on it once"})
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.Error != "" {
		writeErr(w, http.StatusBadGateway, res.Error)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "version": ver, "restarting": res.Restart})
}
