package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestNeoTunnelFromFiles(t *testing.T) {
	dir := t.TempDir()
	// A main server "iran" with a 6to4 stack to "de" (ipip6 over a SIT on UDP
	// 3001), a WireGuard tunnel that is turned off, a GRE tunnel over its
	// quota, and a tunnel whose far end stopped answering.
	write(t, filepath.Join(dir, "state.json"), `{
  "schema": 1, "self": "iran",
  "tunnels": [
    {"name": "de-4", "kind": "sit", "port": 3001, "sides": [{"node": "iran", "endpoint": "203.0.113.10"}, {"node": "de", "endpoint": "198.51.100.24"}]},
    {"name": "de", "kind": "ipip6", "over": "de-4", "mode": "6to4", "profile": "max", "rate_mbit": 200,
     "sides": [{"node": "iran"}, {"node": "de"}], "net4": "10.88.0.0/30"},
    {"name": "nl", "kind": "wireguard", "disabled": true, "sides": [{"node": "iran", "endpoint": "203.0.113.10"}, {"node": "nl", "endpoint": "203.0.113.57"}]},
    {"name": "fi", "kind": "gre", "quota_gb": 1, "sides": [{"node": "iran", "endpoint": "203.0.113.10"}, {"node": "fi", "endpoint": "192.0.2.7"}]},
    {"name": "uk", "kind": "gre", "sides": [{"node": "uk", "endpoint": "192.0.2.9"}, {"node": "iran", "endpoint": "203.0.113.10"}]},
    {"name": "x", "kind": "gre", "sides": [{"node": "de", "endpoint": "1.1.1.1"}, {"node": "nl", "endpoint": "2.2.2.2"}]}
  ],
  "forwards": [{"node": "iran", "tunnel": "de", "proto": "tcp", "ports": "443"}, {"node": "iran", "tunnel": "de", "proto": "both", "ports": "2053-2060"}]
}`)
	now := time.Now().Unix()
	write(t, filepath.Join(dir, "health.json"), fmt.Sprintf(`{
  "de": {"rtt_ms": 41.2, "avg_ms": 40.6, "loss": 0, "last": %d, "probes": 20, "passes": true},
  "de-4": {"avg_ms": 40, "last": %d, "probes": 20, "passes": true},
  "uk": {"avg_ms": 0, "loss": 100, "last": %d, "probes": 20, "passes": false}
}`, now, now, now-300))
	month := time.Now().Format("2006-01")
	write(t, filepath.Join(dir, "traffic.json"), `{"tunnels": {
  "de": {"total": {"rx": 5000, "tx": 700}, "days": {"`+month+`-01": {"rx": 10, "tx": 5}}},
  "fi": {"total": {"rx": 2147483648, "tx": 0}, "days": {"`+month+`-02": {"rx": 1073741824, "tx": 1}}}
}}`)
	old := ntIfUp
	ntIfUp = func(name string) (bool, bool) { return true, true }
	defer func() { ntIfUp = old }()

	src := collectNeoTunnel(context.Background(), Config{NeoTunnelDir: dir})
	if src == nil || !src.Installed || src.Mode != "files" || src.Error != "" {
		t.Fatalf("unexpected source: %+v", src)
	}
	got := map[string]int{}
	for i, tn := range src.Tunnels {
		got[tn.Name] = i
	}
	if len(src.Tunnels) != 4 {
		t.Fatalf("want de, fi, nl, uk (no carrier, no foreign tunnel), got %+v", src.Tunnels)
	}
	if _, ok := got["de-4"]; ok {
		t.Error("the SIT under the 6to4 tunnel should not be a tunnel of its own")
	}
	de := src.Tunnels[got["de"]]
	if de.Source != "neotunnel" || de.Carrier != "6to4" || de.Role != "main" || de.Node != "de" {
		t.Errorf("de fields: %+v", de)
	}
	if de.Addr != "198.51.100.24:3001" || de.TunnelPort != "3001" || de.Ports != "443, 2053-2060" || de.Preset != "Max" || de.BandwidthMbps != 200 {
		t.Errorf("de stack fields: %+v", de)
	}
	if _, known := serviceActive("x"); !known {
		if de.State != "online" || de.Ping != 41 || de.TotalBytes != 5700 {
			t.Errorf("de state: %+v", de)
		}
		if nl := src.Tunnels[got["nl"]]; nl.State != "stopped" || nl.LastError == "" {
			t.Errorf("nl should be stopped (turned off): %+v", nl)
		}
		if fi := src.Tunnels[got["fi"]]; !fi.QuotaHit || fi.State != "stopped" || fi.QuotaLimit != 1<<30 || fi.QuotaUsed != 1<<30+1 {
			t.Errorf("fi should be over its quota: %+v", fi)
		}
		uk := src.Tunnels[got["uk"]]
		if uk.State != "offline" || uk.Ping != -1 || uk.Addr != "192.0.2.9" || uk.LastError != "no reply through the tunnel for 5m" {
			t.Errorf("uk should be offline: %+v", uk)
		}
	}

	if collectNeoTunnel(context.Background(), Config{NeoTunnelDir: filepath.Join(dir, "missing")}) != nil {
		t.Error("a missing NeoTunnel directory should report nothing")
	}
}

// NeoTunnel 0.8.3 display names: the tunnel's label, the far server's
// label, and tunnels pushed by more than one main server.
func TestNeoTunnelDisplayNames(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "state.json"), `{
  "schema": 1, "self": "ir1", "label": "تهران آسیاتک",
  "peers": [{"name": "de", "label": "Hetzner FSN"}, {"name": "nl", "address": "203.0.113.57"}],
  "tunnels": [
    {"name": "de", "label": "  آلمان VIP  ", "kind": "gre", "sides": [{"node": "ir1", "endpoint": "203.0.113.10"}, {"node": "de", "endpoint": "198.51.100.24"}]},
    {"name": "nl", "kind": "gre", "sides": [{"node": "ir1", "endpoint": "203.0.113.10"}, {"node": "nl", "endpoint": "203.0.113.57"}]}
  ],
  "managed": {"master": {"name": "hub-a"}, "self": "iran-a", "tunnels": [
    {"name": "a1", "label": "Hub A link", "kind": "vxlan", "sides": [{"node": "hub-a", "endpoint": "192.0.2.1"}, {"node": "iran-a", "endpoint": "203.0.113.10"}]}
  ]},
  "more_masters": [{"master": {"name": "hub-b"}, "self": "iran-b", "tunnels": [
    {"name": "b1", "kind": "gre", "sides": [{"node": "iran-b", "endpoint": "203.0.113.10"}, {"node": "hub-b", "endpoint": "192.0.2.2"}]}
  ]}]
}`)
	old := ntIfUp
	ntIfUp = func(string) (bool, bool) { return true, true }
	defer func() { ntIfUp = old }()

	src := collectNeoTunnel(context.Background(), Config{NeoTunnelDir: dir})
	by := map[string]int{}
	for i, tn := range src.Tunnels {
		by[tn.Name] = i
	}
	if len(src.Tunnels) != 4 {
		t.Fatalf("want de, nl, a1 and b1 (b1 from the second main server), got %+v", src.Tunnels)
	}
	de := src.Tunnels[by["de"]]
	if de.Label != "آلمان VIP" || de.Node != "de" || de.NodeLabel != "Hetzner FSN" {
		t.Errorf("de: label %q, node %q / %q", de.Label, de.Node, de.NodeLabel)
	}
	if nl := src.Tunnels[by["nl"]]; nl.Label != "" || nl.NodeLabel != "" {
		t.Errorf("nl has no display names: %+v", nl)
	}
	if a1 := src.Tunnels[by["a1"]]; a1.Label != "Hub A link" || a1.Role != "destination" || a1.Node != "hub-a" {
		t.Errorf("a1: %+v", a1)
	}
	if b1 := src.Tunnels[by["b1"]]; b1.Role != "destination" || b1.Node != "hub-b" {
		t.Errorf("b1: %+v", b1)
	}
}
