// Package monitor runs the checks: one goroutine per active monitor, a retry
// policy, maintenance windows, important-event detection and notifications.
package monitor

import (
	"context"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// Monitor types.
const (
	TypeHTTP         = "http"
	TypeKeyword      = "keyword"
	TypeJSONQuery    = "json-query"
	TypeTCP          = "tcp"
	TypePing         = "ping"
	TypeDNS          = "dns"
	TypePush         = "push"
	TypeGroup        = "group"
	TypeDocker       = "docker"
	TypePostgres     = "postgres"
	TypeMySQL        = "mysql"
	TypeRedis        = "redis"
	TypeBackpackTun  = "backpack-tunnel"
	TypeBackpackHost = "backpack-panel"
	TypeAgent        = "agent"
	TypeAgentTunnel  = "agent-tunnel"
	TypeSMTP         = "smtp"
	TypeMongo        = "mongodb"
	TypeMSSQL        = "mssql"
	TypeMQTT         = "mqtt"
	TypeGRPC         = "grpc"
	TypeRadius       = "radius"
	TypeSNMP         = "snmp"
	TypeSteam        = "steam"
	TypeKafka        = "kafka"
	TypeUDP          = "udp"
	TypeWebSocket    = "websocket"
	TypeTLS          = "tls"
	TypeXray         = "xray" // a V2Ray/Xray config, tested through a real Xray core
)

// KnownType reports whether t is a monitor type this build can run.
func KnownType(t string) bool {
	switch t {
	case TypeHTTP, TypeKeyword, TypeJSONQuery, TypeTCP, TypePing, TypeDNS, TypePush, TypeGroup,
		TypeDocker, TypePostgres, TypeMySQL, TypeRedis, TypeBackpackTun, TypeBackpackHost,
		TypeAgent, TypeAgentTunnel, TypeSMTP, TypeMongo, TypeMSSQL, TypeMQTT, TypeGRPC, TypeRadius,
		TypeSNMP, TypeSteam, TypeKafka, TypeUDP, TypeWebSocket, TypeTLS, TypeXray:
		return true
	}
	return false
}

// CertInfo describes the TLS certificate an HTTPS monitor saw.
type CertInfo struct {
	Subject       string   `json:"subject"`
	Issuer        string   `json:"issuer"`
	ValidFrom     int64    `json:"validFrom"`
	ValidTo       int64    `json:"validTo"`
	DaysRemaining int      `json:"daysRemaining"`
	Valid         bool     `json:"valid"`
	DNSNames      []string `json:"dnsNames,omitempty"`
}

// Result is what a single check found.
type Result struct {
	Up       bool
	Ping     *float64
	Msg      string
	BytesIn  *int64
	BytesOut *int64
	Cert     *CertInfo
	Tunnel   *backpack.Tunnel
	System   *backpack.System
	Agent    *AgentInfo
	Xray     *XrayInfo
	// Skip records nothing: the result is not in yet (a test running on
	// another server).
	Skip bool
}

func up(msg string, ping time.Duration) Result {
	p := float64(ping.Microseconds()) / 1000
	return Result{Up: true, Msg: msg, Ping: &p}
}

func down(msg string) Result { return Result{Up: false, Msg: msg} }

func ms(d time.Duration) *float64 {
	p := float64(d.Microseconds()) / 1000
	return &p
}

// Env is what checkers may reach besides the monitor itself.
type Env struct {
	DB       *store.DB
	Backpack *backpack.Cache
	Agents   *AgentHub
}

// Checker runs one check.
type Checker func(ctx context.Context, m *store.Monitor, env *Env) Result

var checkers = map[string]Checker{
	TypeHTTP:         checkHTTP,
	TypeKeyword:      checkHTTP,
	TypeJSONQuery:    checkHTTP,
	TypeTCP:          checkTCP,
	TypePing:         checkPing,
	TypeDNS:          checkDNS,
	TypeDocker:       checkDocker,
	TypePostgres:     checkPostgres,
	TypeMySQL:        checkMySQL,
	TypeRedis:        checkRedis,
	TypeBackpackTun:  checkBackpackTunnel,
	TypeBackpackHost: checkBackpackPanel,
	TypeAgent:        checkAgent,
	TypeAgentTunnel:  checkAgentTunnel,
	TypeSMTP:         checkSMTP,
	TypeMongo:        checkMongo,
	TypeMSSQL:        checkMSSQL,
	TypeMQTT:         checkMQTT,
	TypeGRPC:         checkGRPC,
	TypeRadius:       checkRadius,
	TypeSNMP:         checkSNMP,
	TypeSteam:        checkSteam,
	TypeKafka:        checkKafka,
	TypeUDP:          checkUDP,
	TypeWebSocket:    checkWebSocket,
	TypeTLS:          checkTLS,
	TypeXray:         checkXray,
}
