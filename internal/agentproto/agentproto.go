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

	// Caps lists what this agent can do beyond reporting, e.g. "xray".
	Caps []string `json:"caps,omitempty"`
	// Probes holds the latest result of each recurring test the server asked for.
	Probes []ProbeResult `json:"probes,omitempty"`
	// Jobs holds the results of one-off jobs since the last report.
	Jobs []JobResult `json:"jobs,omitempty"`
	// Early marks a report sent ahead of time to deliver results; the server
	// takes the results and leaves the rest for the regular report.
	Early bool `json:"early,omitempty"`
}

// CapXray means the agent tests V2Ray/Xray configs with a built-in Xray core.
const CapXray = "xray"

// Probe is a test the agent runs again and again, from where it is: today a
// V2Ray/Xray config tested through a real Xray core.
type Probe struct {
	ID      int64  `json:"id"` // the monitor
	Kind    string `json:"kind"`
	Link    string `json:"link"`
	URL     string `json:"url,omitempty"` // fetched through the config
	Every   int    `json:"every"`         // seconds
	Timeout int    `json:"timeout"`       // seconds
}

// ProbeResult is one run of a probe.
type ProbeResult struct {
	ID      int64   `json:"id"`
	At      int64   `json:"at"` // unix ms, agent clock
	OK      bool    `json:"ok"`
	Ping    float64 `json:"ping,omitempty"`    // ms, the round trip once connected
	Connect float64 `json:"connect,omitempty"` // ms, the first request
	Msg     string  `json:"msg,omitempty"`
}

// Job is a one-off request: fetch a subscription, test a config now, or
// update the agent itself.
type Job struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // fetch | test | speed | update
	URL     string `json:"url,omitempty"`
	Link    string `json:"link,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
	// update: the release to switch to, the asset's architecture
	// (amd64 | arm64 | armv7) and the SHA-256 of its agent binary.
	Version string `json:"version,omitempty"`
	Arch    string `json:"arch,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

// JobResult answers a job.
type JobResult struct {
	ID       string  `json:"id"`
	Error    string  `json:"error,omitempty"`
	Body     string  `json:"body,omitempty"`     // fetch: the response body
	Userinfo string  `json:"userinfo,omitempty"` // fetch: subscription-userinfo
	Title    string  `json:"title,omitempty"`    // fetch: profile-title
	Ping     float64 `json:"ping,omitempty"`     // test
	Connect  float64 `json:"connect,omitempty"`  // test
	Restart  bool    `json:"restart,omitempty"`  // update: done; the agent restarts into it

	// speed: Mbps both ways, where the data came from, and how much moved.
	Down    float64 `json:"down,omitempty"`
	Up      float64 `json:"up,omitempty"`
	Server  string  `json:"server,omitempty"`
	Bytes   int64   `json:"bytes,omitempty"`
	UpError string  `json:"upError,omitempty"`
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
	// Probes is the full set of recurring tests; others stop.
	Probes []Probe `json:"probes,omitempty"`
	Jobs   []Job   `json:"jobs,omitempty"`
}
