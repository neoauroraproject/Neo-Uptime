package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

// Hooks for tests: where the running binary is, whether it runs as a
// service, and how a new binary is checked before it replaces the old one.
var (
	executable = func() (string, error) {
		p, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(p)
	}
	underService = func() bool { return os.Getenv("INVOCATION_ID") != "" }
	checkBinary  = func(ctx context.Context, path, want string) error {
		out, err := exec.CommandContext(ctx, path, "version").Output()
		if err != nil {
			return fmt.Errorf("the new binary does not run: %w", err)
		}
		if !strings.Contains(string(out), want) {
			return fmt.Errorf("the new binary says %q, not %s", strings.TrimSpace(string(out)), want)
		}
		return nil
	}
	githubReleases = "https://github.com/" + version.Repo + "/releases/download"
)

// selfUpdate replaces the running binary with the release the server
// names. It is fetched from the Neo Uptime server itself — so servers that
// cannot reach GitHub update too — or from GitHub when that fails, and must
// match the checksum the server took from the release.
func (a *Agent) selfUpdate(ctx context.Context, j agentproto.Job) error {
	if j.Version == "" || len(j.SHA256) != 64 || j.Arch == "" {
		return errors.New("the update request names no release")
	}
	if !underService() {
		return errors.New("this agent is not running as its systemd service — update it with the install command")
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("cannot find the agent binary: %w", err)
	}
	bin, err := a.binaryFromServer(ctx, j)
	if err != nil {
		var gerr error
		if bin, gerr = binaryFromGitHub(ctx, j); gerr != nil {
			return fmt.Errorf("download failed — from Neo Uptime: %v; from GitHub: %v", err, gerr)
		}
	}
	sum := sha256.Sum256(bin)
	if hex.EncodeToString(sum[:]) != strings.ToLower(j.SHA256) {
		return errors.New("the downloaded binary does not match the release checksum")
	}
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return fmt.Errorf("cannot write the new binary: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := checkBinary(cctx, tmp, j.Version); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("cannot replace the binary: %w", err)
	}
	return nil
}

var downloads = &http.Client{Timeout: 10 * time.Minute}

func (a *Agent) binaryFromServer(ctx context.Context, j agentproto.Job) ([]byte, error) {
	servers := a.Servers()
	if cur := a.Current(); cur != "" {
		servers = append([]string{cur}, servers...)
	}
	var last error = errors.New("no server address")
	tried := map[string]bool{}
	for _, srv := range servers {
		if tried[srv] {
			continue
		}
		tried[srv] = true
		u := srv + "/api/agent/binary?arch=" + url.QueryEscape(j.Arch) + "&version=" + url.QueryEscape(j.Version)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			last = err
			continue
		}
		req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
		resp, err := a.downloadClient().Do(req)
		if err != nil {
			last = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
		resp.Body.Close()
		if err == nil && resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if err != nil {
			last = err
			continue
		}
		return body, nil
	}
	return nil, last
}

// downloadClient is the agent's client (its TLS settings) without the
// short timeout meant for reports.
func (a *Agent) downloadClient() *http.Client {
	c := *a.client
	c.Timeout = downloads.Timeout
	return &c
}

func binaryFromGitHub(ctx context.Context, j agentproto.Job) ([]byte, error) {
	u := githubReleases + "/" + j.Version + "/neo-uptime-agent-linux-" + j.Arch + ".tar.gz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := downloads.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tgz, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return nil, err
	}
	return ExtractBinary(tgz, "neo-uptime-agent")
}

// ExtractBinary takes one file out of a release archive (.tar.gz).
func ExtractBinary(tgz []byte, name string) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s is not in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == name && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}
