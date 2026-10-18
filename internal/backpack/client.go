// Package backpack reads tunnels and host stats from a BackPack web panel.
//
// It uses the panel's own read-only API — the one BackPack documents for
// Prometheus scrapers — authenticated with a read-scoped token created under
// Settings → Security → API tokens. Nothing here can change a tunnel.
package backpack

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// KCPStats mirrors BackPack's KCP link-quality counters.
type KCPStats struct {
	PacketsIn     uint64 `json:"packets_in"`
	PacketsOut    uint64 `json:"packets_out"`
	Retransmitted uint64 `json:"retransmitted"`
	Lost          uint64 `json:"lost"`
	Duplicated    uint64 `json:"duplicated"`
	FECRecovered  uint64 `json:"fec_recovered"`
	FECErrors     uint64 `json:"fec_errors"`
}

// PoolStats mirrors BackPack's client connection pool state.
type PoolStats struct {
	Live       int `json:"live"`
	Target     int `json:"target"`
	Configured int `json:"configured"`
	Mbps       int `json:"mbps"`
}

// RatePoint is one throughput sample, bytes per second.
type RatePoint struct {
	T   int64   `json:"t"`
	In  float64 `json:"in"`
	Out float64 `json:"out"`
}

// Tunnel is one row of BackPack's /api/tunnels. It is also the shape every
// tunnel is reported in, whatever runs it: the agent converts Kariz and NeoTunnel
// tunnels into it too, with Source saying which.
type Tunnel struct {
	Source         string      `json:"source,omitempty"` // backpack | kariz | neotunnel
	LastError      string      `json:"lastError,omitempty"`
	Name           string      `json:"name"`
	Role           string      `json:"role"`
	Transport      string      `json:"transport"`
	Direction      string      `json:"direction"`
	Carrier        string      `json:"carrier"`
	Addr           string      `json:"addr"`
	Ports          string      `json:"ports"`
	State          string      `json:"state"` // online | offline | stopped
	Ping           int         `json:"ping"`  // ms, -1 = n/a
	PeerLocation   string      `json:"peerLocation"`
	PeerISP        string      `json:"peerISP"`
	Country        string      `json:"country"`
	PeerCountry    string      `json:"peerCountry"`
	TunnelPort     string      `json:"tunnelPort"`
	ServiceDown    string      `json:"serviceDown,omitempty"`
	Node           string      `json:"node,omitempty"`
	Uptime         string      `json:"uptime,omitempty"`
	BytesIn        string      `json:"bytesIn,omitempty"`
	BytesOut       string      `json:"bytesOut,omitempty"`
	BytesTotal     string      `json:"bytesTotal,omitempty"`
	InBytes        uint64      `json:"inBytes,omitempty"`
	OutBytes       uint64      `json:"outBytes,omitempty"`
	TotalBytes     uint64      `json:"totalBytes,omitempty"`
	QuotaLimit     uint64      `json:"quotaLimit,omitempty"`
	QuotaUsed      uint64      `json:"quotaUsed,omitempty"` // when not TotalBytes (NeoTunnel: this month)
	QuotaHit       bool        `json:"quotaHit,omitempty"`
	KCP            *KCPStats   `json:"kcp,omitempty"`
	KCPLossPercent float64     `json:"kcpLossPercent,omitempty"`
	Pool           *PoolStats  `json:"pool,omitempty"`
	Preset         string      `json:"preset,omitempty"`
	MaxConnections int         `json:"maxConnections,omitempty"`
	BandwidthMbps  int         `json:"bandwidthMbps,omitempty"`
	ProxyProtocol  bool        `json:"proxyProtocol,omitempty"`
	LoadBalance    bool        `json:"loadBalance,omitempty"`
	FallbackAddrs  []string    `json:"fallbackAddrs,omitempty"`
	CertDomain     string      `json:"certDomain,omitempty"`
	CertType       string      `json:"certType,omitempty"`
	CertExpiry     string      `json:"certExpiry,omitempty"`
	Rates          []RatePoint `json:"rates,omitempty"`
}

// System is BackPack's /api/stats: the panel host itself.
type System struct {
	Hostname          string  `json:"hostname"`
	OS                string  `json:"os"`
	Uptime            string  `json:"uptime"`
	IPv4              string  `json:"ipv4"`
	IPv6              string  `json:"ipv6"`
	Location          string  `json:"location"`
	ISP               string  `json:"isp"`
	CPUPercent        float64 `json:"cpuPercent"`
	CPUCores          int     `json:"cpuCores"`
	Load              string  `json:"load"`
	MemUsed           string  `json:"memUsed"`
	MemTotal          string  `json:"memTotal"`
	MemPercent        float64 `json:"memPercent"`
	SwapPercent       float64 `json:"swapPercent"`
	DiskUsed          string  `json:"diskUsed"`
	DiskTotal         string  `json:"diskTotal"`
	DiskPercent       float64 `json:"diskPercent"`
	TotalSent         string  `json:"totalSent"`
	TotalRecv         string  `json:"totalRecv"`
	TotalTraffic      string  `json:"totalTraffic"`
	UpSpeed           string  `json:"upSpeed"`
	DownSpeed         string  `json:"downSpeed"`
	UpBps             float64 `json:"upBps"`
	DownBps           float64 `json:"downBps"`
	TotalSentBytes    uint64  `json:"totalSentBytes"`
	TotalRecvBytes    uint64  `json:"totalRecvBytes"`
	TotalTrafficBytes uint64  `json:"totalTrafficBytes"`
	TunnelsTotal      int     `json:"tunnelsTotal"`
	TunnelsRunning    int     `json:"tunnelsRunning"`
	MonitorRunning    bool    `json:"monitorRunning"`
	Version           string  `json:"version,omitempty"`
}

// Client talks to one panel.
type Client struct {
	BaseURL   string
	Token     string
	IgnoreTLS bool
	Timeout   time.Duration
}

func (c *Client) httpClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: c.IgnoreTLS} //nolint:gosec // operator's choice for self-signed panels
	tr.MaxIdleConnsPerHost = 2
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		// The panel answers a bad credential with a redirect to its login form
		// for browsers; refusing to follow keeps that from reading as success.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (c *Client) get(ctx context.Context, path string, out any) (time.Duration, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NeoUptime/1 (+backpack)")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	start := time.Now()
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return elapsed, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return elapsed, errors.New("the panel refused the token (401) — check it under Settings → Security → API tokens")
	case resp.StatusCode == http.StatusForbidden:
		return elapsed, fmt.Errorf("the panel says the token may not read this (403): %s", strings.TrimSpace(string(body)))
	case resp.StatusCode == http.StatusTooManyRequests:
		return elapsed, errors.New("the panel is rate-limiting this address after failed attempts (429)")
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return elapsed, fmt.Errorf("the panel redirected to %q — the URL or base path is probably wrong", resp.Header.Get("Location"))
	case resp.StatusCode != http.StatusOK:
		return elapsed, fmt.Errorf("unexpected HTTP %d from the panel", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return elapsed, fmt.Errorf("the panel answered but not with BackPack JSON: %w", err)
	}
	return elapsed, nil
}

// Tunnels fetches the tunnel list.
func (c *Client) Tunnels(ctx context.Context) ([]Tunnel, time.Duration, error) {
	var out []Tunnel
	d, err := c.get(ctx, "/api/tunnels", &out)
	return out, d, err
}

// System fetches the panel host's stats.
func (c *Client) System(ctx context.Context) (*System, time.Duration, error) {
	var out System
	d, err := c.get(ctx, "/api/stats", &out)
	if err != nil {
		return nil, d, err
	}
	return &out, d, nil
}
