// Command neo-uptime-agent reports a server's health and its BackPack and
// Kariz tunnels to a Neo Uptime server.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agent"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func usage() {
	fmt.Fprintf(os.Stderr, `neo-uptime-agent %s — reports this server and its tunnels to Neo Uptime

Usage:
  neo-uptime-agent [run] [flags]   report every interval (default)
  neo-uptime-agent once [flags]    print one report as JSON and exit
  neo-uptime-agent test [flags]    send one report and say whether it was accepted
  neo-uptime-agent version

Flags (or environment):
  --server          NEO_AGENT_SERVER     Neo Uptime address, e.g. https://uptime.example.com
  --token           NEO_AGENT_TOKEN      the agent token from Neo Uptime → Servers
  --interval        NEO_AGENT_INTERVAL   seconds between reports (default 20)
  --insecure        NEO_AGENT_INSECURE   accept a self-signed Neo Uptime certificate
  --backpack-dir    BACKPACK_DIR         default /etc/backpack
  --backpack-api    BACKPACK_API         read BackPack's panel API instead of its files
  --backpack-token  BACKPACK_TOKEN       read token for --backpack-api
  --kariz-dir       KARIZ_DIR            default /etc/kariz
  --no-ping         NEO_AGENT_NO_PING    do not ping tunnel peers
`, version.Version)
}

func main() {
	args := os.Args[1:]
	cmd := "run"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("neo-uptime-agent", flag.ExitOnError)
	fs.Usage = usage
	server := fs.String("server", env("NEO_AGENT_SERVER", ""), "")
	token := fs.String("token", env("NEO_AGENT_TOKEN", ""), "")
	interval := fs.Int("interval", func() int { n, _ := strconv.Atoi(env("NEO_AGENT_INTERVAL", "20")); return n }(), "")
	insecure := fs.Bool("insecure", envBool("NEO_AGENT_INSECURE", false), "")
	bpDir := fs.String("backpack-dir", env("BACKPACK_DIR", "/etc/backpack"), "")
	bpAPI := fs.String("backpack-api", env("BACKPACK_API", ""), "")
	bpToken := fs.String("backpack-token", env("BACKPACK_TOKEN", ""), "")
	karizDir := fs.String("kariz-dir", env("KARIZ_DIR", "/etc/kariz"), "")
	noPing := fs.Bool("no-ping", envBool("NEO_AGENT_NO_PING", false), "")
	_ = fs.Parse(args)

	cfg := agent.Config{
		Server: *server, Token: *token, Interval: time.Duration(*interval) * time.Second, Insecure: *insecure,
		BackpackDir: *bpDir, BackpackAPI: *bpAPI, BackpackToken: *bpToken, BackpackInsecure: true,
		KarizDir: *karizDir, NoPing: *noPing,
	}
	a := agent.New(cfg)

	switch cmd {
	case "version", "--version", "-v":
		fmt.Println("neo-uptime-agent", version.Version, runtime.GOOS+"/"+runtime.GOARCH)
	case "once":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Two samples a second apart, so CPU and network rates are real.
		a.Collect(ctx)
		time.Sleep(time.Second)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(a.Collect(ctx))
	case "test":
		need(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		r := a.Collect(ctx)
		ack, err := a.Send(ctx, r)
		if err != nil {
			log.Fatalf("not accepted: %v", err)
		}
		bp, kz := 0, 0
		if r.Backpack != nil {
			bp = len(r.Backpack.Tunnels)
		}
		if r.Kariz != nil {
			kz = len(r.Kariz.Tunnels)
		}
		fmt.Printf("accepted as %q — %d BackPack and %d Kariz tunnel(s) reported\n", ack.Name, bp, kz)
	case "run":
		need(cfg)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		a.Run(ctx)
	default:
		usage()
		os.Exit(2)
	}
}

func need(cfg agent.Config) {
	if cfg.Server == "" || cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "--server and --token are required (create the agent in Neo Uptime → Servers)")
		os.Exit(2)
	}
}
