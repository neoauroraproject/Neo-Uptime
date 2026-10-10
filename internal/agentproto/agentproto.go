// Package agentproto is the wire format between neo-uptime-agent and the
// server: one JSON report, pushed with the agent's token.
//
// Push rather than pull, because the server that runs the tunnels is often
// the one that cannot be reached from outside (an Iran-side edge behind a
// filter), while it can always reach out over HTTPS.
package agentproto

import "github.com/neoauroraproject/Neo-Uptime/internal/backpack"

// Version of the report format. Readers ignore fields they do not know.
const Version = 1

// Report is everything an agent knows, sent every interval.
type Report struct {
	Format       int     `json:"format"`
	AgentVersion string  `json:"agentVersion"`
	Time         int64   `json:"time"` // unix ms, agent clock
	Interval     int     `json:"interval"`
	RTTMs        float64 `json:"rttMs"` // round trip of the previous report

	System System `json:"system"`

	Backpack *Source `json:"backpack,omitempty"`
	Kariz    *Source `json:"kariz,omitempty"`
}

// System describes the host.
type System struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	Kernel      string  `json:"kernel"`
	Arch        string  `json:"arch"`
	UptimeSecs  int64   `json:"uptimeSecs"`
	CPUPercent  float64 `json:"cpuPercent"`
	CPUCores    int     `json:"cpuCores"`
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	MemTotal    uint64  `json:"memTotal"`
	MemUsed     uint64  `json:"memUsed"`
	SwapTotal   uint64  `json:"swapTotal"`
	SwapUsed    uint64  `json:"swapUsed"`
	DiskTotal   uint64  `json:"diskTotal"`
	DiskUsed    uint64  `json:"diskUsed"`
	NetRxBytes  uint64  `json:"netRxBytes"`
	NetTxBytes  uint64  `json:"netTxBytes"`
	NetRxRate   float64 `json:"netRxRate"` // bytes per second
	NetTxRate   float64 `json:"netTxRate"`
	TCPConns    int     `json:"tcpConns"`
	PublicIP    string  `json:"publicIp,omitempty"`
	Virtualized string  `json:"virt,omitempty"`
}

// Source is what one tunnel program reported.
type Source struct {
	Installed bool              `json:"installed"`
	Mode      string            `json:"mode,omitempty"` // files | api | socket
	Version   string            `json:"version,omitempty"`
	Error     string            `json:"error,omitempty"`
	Tunnels   []backpack.Tunnel `json:"tunnels"`
}

// Ack is the server's answer to a report.
type Ack struct {
	OK       bool   `json:"ok"`
	Interval int    `json:"interval,omitempty"` // the server may ask for another pace
	Name     string `json:"name,omitempty"`
	Error    string `json:"error,omitempty"`
}
