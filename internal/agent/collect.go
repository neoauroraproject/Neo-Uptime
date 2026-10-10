package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	probing "github.com/prometheus-community/pro-bing"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
)

// ---------- shared helpers ----------

// serviceActive asks systemd whether a unit runs. known is false when there
// is no systemd to ask (a container, a dev machine).
func serviceActive(unit string) (active, known bool) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false, false
	}
	err := exec.Command("systemctl", "is-active", "--quiet", unit).Run()
	return err == nil, true
}

func humanSecs(s int64) string {
	if s <= 0 {
		return ""
	}
	d := s / 86400
	h := (s % 86400) / 3600
	m := (s % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%ds", s)
}

func hostOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return strings.Trim(addr, "[]")
}

func portOf(addr string) string {
	if _, p, err := net.SplitHostPort(strings.TrimSpace(addr)); err == nil {
		return p
	}
	return ""
}

func usableHost(h string) bool {
	if h == "" || h == "0.0.0.0" || h == "::" || h == "localhost" {
		return false
	}
	ip := net.ParseIP(h)
	return ip == nil || !(ip.IsLoopback() || ip.IsUnspecified())
}

// pingHost measures round-trip time with one ICMP echo, falling back to a TCP
// connect when tcpAddr is given. -1 when nothing answered.
func pingHost(ctx context.Context, host, tcpAddr string) int {
	if usableHost(host) {
		if p, err := probing.NewPinger(host); err == nil {
			p.Count, p.Timeout, p.Interval = 2, 3*time.Second, 200*time.Millisecond
			p.SetPrivileged(os.Geteuid() == 0)
			if err := p.RunWithContext(ctx); err == nil {
				if st := p.Statistics(); st.PacketsRecv > 0 {
					return max(1, int(st.AvgRtt.Milliseconds()))
				}
			}
		}
	}
	if tcpAddr != "" && usableHost(hostOf(tcpAddr)) {
		start := time.Now()
		d := net.Dialer{Timeout: 3 * time.Second}
		if c, err := d.DialContext(ctx, "tcp", tcpAddr); err == nil {
			c.Close()
			return max(1, int(time.Since(start).Milliseconds()))
		}
	}
	return -1
}

func sortTunnels(t []backpack.Tunnel) {
	sort.Slice(t, func(i, j int) bool { return t[i].Name < t[j].Name })
}

// ---------- BackPack ----------

// bpSnapshot is the part of BackPack's per-tunnel metrics file the agent reads.
type bpSnapshot struct {
	Taken     time.Time           `json:"taken"`
	Uptime    string              `json:"uptime"`
	BytesIn   uint64              `json:"bytes_in"`
	BytesOut  uint64              `json:"bytes_out"`
	Peer      string              `json:"peer"`
	Connected *bool               `json:"connected"`
	KCP       *backpack.KCPStats  `json:"kcp"`
	Pool      *backpack.PoolStats `json:"pool"`
	Local     *struct {
		Addr string `json:"addr"`
		Why  string `json:"why"`
	} `json:"local_service"`
}

type bpSection struct {
	BindAddr   string   `toml:"bind_addr"`
	RemoteAddr string   `toml:"remote_addr"`
	Transport  string   `toml:"transport"`
	Ports      []string `toml:"ports"`
	Preset     string   `toml:"preset"`
	Addr       string   `toml:"addr"`
	Role       string   `toml:"role"`
	Mode       string   `toml:"mode"`
	Carrier    string   `toml:"carrier"`
}

type bpConfig struct {
	Server bpSection `toml:"server"`
	Client bpSection `toml:"client"`
	Direct bpSection `toml:"direct"`
	L3     bpSection `toml:"l3"`
}

func presetLabel(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
}

func collectBackpack(ctx context.Context, cfg Config) *agentproto.Source {
	if cfg.BackpackAPI != "" {
		cl := &backpack.Client{BaseURL: cfg.BackpackAPI, Token: cfg.BackpackToken, IgnoreTLS: cfg.BackpackInsecure, Timeout: 15 * time.Second}
		src := &agentproto.Source{Installed: true, Mode: "api", Tunnels: []backpack.Tunnel{}}
		t, _, err := cl.Tunnels(ctx)
		if err != nil {
			src.Error = err.Error()
			return src
		}
		for i := range t {
			t[i].Source = "backpack"
		}
		if sys, _, err := cl.System(ctx); err == nil {
			src.Version = sys.Version
		}
		src.Tunnels = t
		return src
	}

	matches, _ := filepath.Glob(filepath.Join(cfg.BackpackDir, "*.toml"))
	if len(matches) == 0 {
		if _, err := os.Stat(cfg.BackpackDir); err != nil {
			return nil // not installed here
		}
	}
	src := &agentproto.Source{Installed: true, Mode: "files", Tunnels: []backpack.Tunnel{}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, path := range matches {
		var c bpConfig
		if _, err := toml.DecodeFile(path, &c); err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(path), ".toml")
		t := backpack.Tunnel{Source: "backpack", Name: name, Ping: -1}
		switch {
		case c.Server.BindAddr != "":
			t.Role, t.Transport, t.Addr, t.Preset = "server", c.Server.Transport, c.Server.BindAddr, presetLabel(c.Server.Preset)
			t.Ports = strings.Join(c.Server.Ports, ", ")
		case c.Client.RemoteAddr != "":
			t.Role, t.Transport, t.Addr, t.Preset = "client", c.Client.Transport, c.Client.RemoteAddr, presetLabel(c.Client.Preset)
		case c.Direct.Addr != "" || c.Direct.Role != "":
			t.Role = firstSet(c.Direct.Role, "direct")
			t.Transport = "direct/" + firstSet(c.Direct.Transport, "tcp")
			t.Addr, t.Preset = c.Direct.Addr, presetLabel(c.Direct.Preset)
			t.Ports = strings.Join(c.Direct.Ports, ", ")
		case c.L3.Addr != "" || c.L3.Mode != "" || c.L3.Carrier != "":
			t.Role = firstSet(c.L3.Mode, "l3")
			t.Transport = "l3/" + firstSet(c.L3.Carrier, "udp")
			t.Addr, t.Preset = c.L3.Addr, presetLabel(c.L3.Preset)
			t.Ports = strings.Join(c.L3.Ports, ", ")
		default:
			continue
		}
		if t.Transport == "" {
			t.Transport = "tcp"
		}
		t.Direction = "reverse"
		t.Carrier = t.Transport
		if _, rest, ok := strings.Cut(t.Transport, "/"); ok {
			t.Direction, t.Carrier = "direct", rest
		}
		t.TunnelPort = portOf(t.Addr)

		var snap bpSnapshot
		hasSnap := false
		if data, err := os.ReadFile(filepath.Join(cfg.BackpackDir, name+".metrics.json")); err == nil {
			hasSnap = json.Unmarshal(data, &snap) == nil
		}
		active, known := serviceActive("backpack-" + name + ".service")
		if !known && hasSnap {
			active = time.Since(snap.Taken) < 3*time.Minute
		}
		switch {
		case !active:
			t.State = "stopped"
		case snap.Connected != nil:
			t.State = map[bool]string{true: "online", false: "offline"}[*snap.Connected]
		case snap.Peer != "":
			t.State = "online"
		default:
			t.State = "offline"
		}
		if hasSnap {
			t.Uptime = snap.Uptime
			t.InBytes, t.OutBytes = snap.BytesIn, snap.BytesOut
			t.TotalBytes = snap.BytesIn + snap.BytesOut
			t.KCP, t.Pool = snap.KCP, snap.Pool
			if k := snap.KCP; k != nil && k.PacketsOut > 0 {
				t.KCPLossPercent = float64(k.Retransmitted+k.Lost) / float64(k.PacketsOut) * 100
			}
			if snap.Local != nil && t.State == "online" {
				t.ServiceDown = fmt.Sprintf("%s (%s)", snap.Local.Addr, snap.Local.Why)
			}
		}
		peerHost := hostOf(snap.Peer)
		tcpAddr := ""
		if t.Role == "client" {
			peerHost, tcpAddr = hostOf(t.Addr), t.Addr
		}
		mu.Lock()
		src.Tunnels = append(src.Tunnels, t)
		idx := len(src.Tunnels) - 1
		mu.Unlock()
		if t.State == "online" && !cfg.NoPing {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p := pingHost(ctx, peerHost, tcpAddr)
				mu.Lock()
				src.Tunnels[idx].Ping = p
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	sortTunnels(src.Tunnels)
	if out, err := exec.Command("backpack", "version").Output(); err == nil {
		src.Version = strings.TrimSpace(string(out))
	}
	return src
}

func firstSet(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// ---------- Kariz ----------

// karizStatus is Kariz's control-socket status document (status_version 1).
type karizStatus struct {
	StatusVersion int    `json:"status_version"`
	Kariz         string `json:"kariz"`
	Role          string `json:"role"`
	Mode          string `json:"mode"`
	Transport     string `json:"transport"`
	Profile       string `json:"profile"`
	UptimeSecs    int64  `json:"uptime_secs"`
	Peer          struct {
		Connected      bool     `json:"connected"`
		Sessions       *int     `json:"sessions"`
		SessionsWanted *int     `json:"sessions_wanted"`
		ConnectedSecs  *int64   `json:"connected_secs"`
		RTTMs          *float64 `json:"rtt_ms"`
		LastError      *struct {
			SecsAgo int64  `json:"secs_ago"`
			Text    string `json:"text"`
		} `json:"last_error"`
	} `json:"peer"`
	Totals struct {
		BytesUp   uint64 `json:"bytes_up"`
		BytesDown uint64 `json:"bytes_down"`
		TCPOpen   int    `json:"tcp_open"`
		UDPFlows  int    `json:"udp_flows"`
	} `json:"totals"`
	Forwards []struct {
		Listen   string `json:"listen"`
		Target   string `json:"target"`
		Protocol string `json:"protocol"`
	} `json:"forwards"`
	Exit *struct {
		LastDialError *struct {
			SecsAgo int64  `json:"secs_ago"`
			Text    string `json:"text"`
		} `json:"last_dial_error"`
	} `json:"exit"`
}

type karizConfig struct {
	Role    string `toml:"role"`
	Mode    string `toml:"mode"`
	Profile string `toml:"profile"`
	Tunnel  struct {
		Transport string `toml:"transport"`
		Listen    string `toml:"listen"`
		Connect   string `toml:"connect"`
		Remote    string `toml:"remote"`
		Server    string `toml:"server"`
	} `toml:"tunnel"`
	Control struct {
		Socket string `toml:"socket"`
	} `toml:"control"`
	Forward []struct {
		Listen string `toml:"listen"`
	} `toml:"forward"`
}

// karizQuery asks a Kariz daemon for its status over its control socket.
func karizQuery(ctx context.Context, sock string) (*karizStatus, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("status\n")); err != nil {
		return nil, err
	}
	var buf strings.Builder
	tmp := make([]byte, 32<<10)
	for {
		n, err := conn.Read(tmp)
		buf.Write(tmp[:n])
		if err != nil || buf.Len() > 4<<20 {
			break
		}
	}
	answer := buf.String()
	if strings.HasPrefix(answer, "!") {
		return nil, fmt.Errorf("kariz: %s", strings.TrimSpace(strings.TrimPrefix(answer, "!")))
	}
	doc, ok := strings.CutPrefix(answer, "---\n")
	if !ok {
		return nil, fmt.Errorf("kariz answered something unexpected")
	}
	var st karizStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(doc)), &st); err != nil {
		return nil, fmt.Errorf("kariz status is not JSON: %w", err)
	}
	return &st, nil
}

func collectKariz(ctx context.Context, cfg Config) *agentproto.Source {
	matches, _ := filepath.Glob(filepath.Join(cfg.KarizDir, "*.toml"))
	if len(matches) == 0 {
		return nil
	}
	src := &agentproto.Source{Installed: true, Mode: "socket", Tunnels: []backpack.Tunnel{}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, path := range matches {
		var c karizConfig
		if _, err := toml.DecodeFile(path, &c); err != nil || (c.Role == "" && c.Tunnel.Transport == "") {
			continue // not a tunnel config
		}
		name := strings.TrimSuffix(filepath.Base(path), ".toml")
		sock := c.Control.Socket
		if sock == "" {
			sock = strings.TrimSuffix(path, ".toml") + ".sock"
		}
		unit := "kariz@" + name + ".service"
		if name == "config" {
			unit = "kariz.service"
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := backpack.Tunnel{
				Source: "kariz", Name: name, Role: c.Role, Transport: c.Tunnel.Transport, Carrier: c.Tunnel.Transport,
				Direction: firstSet(c.Mode, "reverse"), Preset: presetLabel(c.Profile), Ping: -1,
				Addr: firstSet(c.Tunnel.Listen, c.Tunnel.Connect, c.Tunnel.Remote, c.Tunnel.Server),
			}
			t.TunnelPort = portOf(t.Addr)
			var ports []string
			for _, f := range c.Forward {
				if p := portOf(f.Listen); p != "" {
					ports = append(ports, p)
				}
			}
			t.Ports = strings.Join(ports, ", ")

			st, err := karizQuery(ctx, sock)
			if err != nil {
				active, known := serviceActive(unit)
				if known && !active {
					t.State = "stopped"
				} else {
					t.State = "offline"
					t.LastError = "control socket did not answer: " + err.Error()
				}
				mu.Lock()
				src.Tunnels = append(src.Tunnels, t)
				mu.Unlock()
				return
			}
			mu.Lock()
			if src.Version == "" {
				src.Version = st.Kariz
			}
			mu.Unlock()
			t.Role = firstSet(st.Role, t.Role)
			t.Direction = firstSet(st.Mode, t.Direction)
			t.Transport = firstSet(st.Transport, t.Transport)
			t.Carrier = t.Transport
			t.Preset = presetLabel(firstSet(st.Profile, c.Profile))
			t.State = "offline"
			if st.Peer.Connected {
				t.State = "online"
			}
			if st.Peer.ConnectedSecs != nil {
				t.Uptime = humanSecs(*st.Peer.ConnectedSecs)
			} else if st.Peer.Connected {
				t.Uptime = humanSecs(st.UptimeSecs)
			}
			t.InBytes, t.OutBytes = st.Totals.BytesDown, st.Totals.BytesUp
			t.TotalBytes = t.InBytes + t.OutBytes
			if st.Peer.Sessions != nil {
				p := &backpack.PoolStats{Live: *st.Peer.Sessions, Target: *st.Peer.Sessions}
				if st.Peer.SessionsWanted != nil {
					p.Target, p.Configured = *st.Peer.SessionsWanted, *st.Peer.SessionsWanted
				}
				t.Pool = p
			}
			if e := st.Peer.LastError; e != nil && e.SecsAgo < 600 {
				t.LastError = fmt.Sprintf("%s (%s ago)", e.Text, humanSecs(max(1, e.SecsAgo)))
			}
			if st.Exit != nil && st.Exit.LastDialError != nil && st.Exit.LastDialError.SecsAgo < 120 && t.State == "online" {
				t.ServiceDown = st.Exit.LastDialError.Text
			}
			if len(st.Forwards) > 0 {
				ports = ports[:0]
				for _, f := range st.Forwards {
					if p := portOf(f.Listen); p != "" {
						ports = append(ports, p)
					}
				}
				t.Ports = strings.Join(ports, ", ")
			}
			if st.Peer.RTTMs != nil {
				t.Ping = max(1, int(*st.Peer.RTTMs+0.5))
			} else if t.State == "online" && !cfg.NoPing && c.Tunnel.Connect != "" {
				t.Ping = pingHost(ctx, hostOf(c.Tunnel.Connect), c.Tunnel.Connect)
			}
			mu.Lock()
			src.Tunnels = append(src.Tunnels, t)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sortTunnels(src.Tunnels)
	return src
}
