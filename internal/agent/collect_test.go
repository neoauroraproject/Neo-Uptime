package agent

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBackpackFromFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "de2.toml"), `
[server]
bind_addr = "0.0.0.0:3857"
transport = "wssmux"
ports = ["6729", "6730"]
preset = "aggressive"
`)
	write(t, filepath.Join(dir, "nl.toml"), `
[client]
remote_addr = "203.0.113.9:3850"
transport = "tcp"
`)
	write(t, filepath.Join(dir, "panel.toml"), `title = "not a tunnel"`)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	write(t, filepath.Join(dir, "de2.metrics.json"), `{"taken":"`+now+`","uptime":"40m","bytes_in":3000,"bytes_out":200,"peer":"198.51.100.4:51000","connected":true,"local_service":{"addr":"127.0.0.1:443","why":"refused"}}`)
	write(t, filepath.Join(dir, "nl.metrics.json"), `{"taken":"`+now+`","connected":false}`)

	src := collectBackpack(context.Background(), Config{BackpackDir: dir, NoPing: true})
	if src == nil || !src.Installed || src.Mode != "files" {
		t.Fatalf("unexpected source: %+v", src)
	}
	if len(src.Tunnels) != 2 {
		t.Fatalf("want 2 tunnels, got %d: %+v", len(src.Tunnels), src.Tunnels)
	}
	de2, nl := src.Tunnels[0], src.Tunnels[1]
	if de2.Name != "de2" || de2.Carrier != "wssmux" || de2.Direction != "reverse" || de2.Preset != "Aggressive" || de2.TunnelPort != "3857" || de2.Ports != "6729, 6730" {
		t.Errorf("de2 fields: %+v", de2)
	}
	if de2.TotalBytes != 3200 || de2.Source != "backpack" {
		t.Errorf("de2 bytes/source: %+v", de2)
	}
	// Without systemd the agent falls back to how fresh the snapshot is.
	if _, known := serviceActive("x"); !known {
		if de2.State != "online" || nl.State != "offline" {
			t.Errorf("states: de2=%s nl=%s", de2.State, nl.State)
		}
		if de2.ServiceDown == "" {
			t.Error("service-down note missing")
		}
	}
	if nl.Role != "client" || nl.TunnelPort != "3850" {
		t.Errorf("nl fields: %+v", nl)
	}

	if collectBackpack(context.Background(), Config{BackpackDir: filepath.Join(dir, "missing")}) != nil {
		t.Error("a missing BackPack directory should report nothing")
	}
}

func TestKarizFromSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		if _, err := net.Listen("unix", filepath.Join(t.TempDir(), "probe.sock")); err != nil {
			t.Skip("unix sockets unavailable:", err)
		}
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.toml"), `
role = "entry"
mode = "reverse"
profile = "balanced"
[tunnel]
transport = "tcpmux"
listen = "0.0.0.0:3080"
[[forward]]
listen = "0.0.0.0:443"
target = "127.0.0.1:443"
`)
	write(t, filepath.Join(dir, "down.toml"), `
role = "exit"
[tunnel]
transport = "kcp"
connect = "203.0.113.5:3080"
`)
	sock := filepath.Join(dir, "main.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadString('\n')
			if strings.TrimSpace(line) == "status" {
				c.Write([]byte("---\n" + `{"status_version":1,"kariz":"0.7.0","role":"entry","mode":"reverse","transport":"tcpmux","profile":"balanced","uptime_secs":93600,
"peer":{"connected":true,"sessions":2,"sessions_wanted":null,"connected_secs":3600,"rtt_ms":38.4,"last_error":null},
"totals":{"bytes_up":100,"bytes_down":900,"tcp_open":12,"udp_flows":3},
"forwards":[{"listen":"[::]:443","target":"127.0.0.1:443","protocol":"tcp"}]}` + "\n"))
			}
			c.Close()
		}
	}()

	src := collectKariz(context.Background(), Config{KarizDir: dir, NoPing: true})
	if src == nil || len(src.Tunnels) != 2 {
		t.Fatalf("unexpected: %+v", src)
	}
	down, main := src.Tunnels[0], src.Tunnels[1]
	if main.Name != "main" || main.State != "online" || main.Ping != 38 || main.Carrier != "tcpmux" || main.Uptime != "1h 0m" {
		t.Errorf("main: %+v", main)
	}
	if main.TotalBytes != 1000 || main.Pool == nil || main.Pool.Live != 2 || main.Ports != "443" || main.Preset != "Balanced" {
		t.Errorf("main counters: %+v", main)
	}
	if src.Version != "0.7.0" {
		t.Errorf("version %q", src.Version)
	}
	if down.State == "online" || down.Source != "kariz" {
		t.Errorf("down: %+v", down)
	}
}
