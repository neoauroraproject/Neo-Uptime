package monitor

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

func checkTCP(ctx context.Context, m *store.Monitor, _ *Env) Result {
	host := strings.TrimSpace(m.Config.Hostname)
	if host == "" || m.Config.Port <= 0 {
		return down("hostname and port are required")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(m.Config.Port))
	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return down(shortNetErr(err))
	}
	elapsed := time.Since(start)
	conn.Close()
	return up(fmt.Sprintf("port %d open", m.Config.Port), elapsed)
}

func checkPing(ctx context.Context, m *store.Monitor, _ *Env) Result {
	host := strings.TrimSpace(m.Config.Hostname)
	if host == "" {
		return down("hostname is required")
	}
	count := m.Config.PacketCount
	if count <= 0 {
		count = 3
	}
	if count > 20 {
		count = 20
	}
	timeout := time.Duration(m.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	res, err := icmpPing(ctx, host, count, timeout, true)
	if err != nil && strings.Contains(err.Error(), "permitted") {
		// Raw sockets need root or CAP_NET_RAW; datagram ICMP works for
		// unprivileged users where the kernel allows it.
		res, err = icmpPing(ctx, host, count, timeout, false)
	}
	if err != nil && runtime.GOOS != "windows" {
		// Last resort: the system ping binary, which is usually setuid.
		return systemPing(ctx, host, count, timeout)
	}
	if err != nil {
		return down(err.Error())
	}
	return res
}

func icmpPing(ctx context.Context, host string, count int, timeout time.Duration, privileged bool) (Result, error) {
	p, err := probing.NewPinger(host)
	if err != nil {
		return Result{}, fmt.Errorf("resolve %s: %w", host, err)
	}
	p.Count = count
	p.Interval = 300 * time.Millisecond
	p.Timeout = timeout
	p.SetPrivileged(privileged)
	if err := p.RunWithContext(ctx); err != nil {
		return Result{}, err
	}
	st := p.Statistics()
	if st.PacketsRecv == 0 {
		return down(fmt.Sprintf("100%% packet loss (%d sent)", st.PacketsSent)), nil
	}
	msg := fmt.Sprintf("%d/%d replies", st.PacketsRecv, st.PacketsSent)
	if st.PacketLoss > 0 {
		msg += fmt.Sprintf(", %.0f%% loss", st.PacketLoss)
	}
	return up(msg, st.AvgRtt), nil
}

var pingAvgRe = regexp.MustCompile(`= [\d.]+/([\d.]+)/`)

func systemPing(ctx context.Context, host string, count int, timeout time.Duration) Result {
	cctx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "ping", "-c", strconv.Itoa(count), "-W",
		strconv.Itoa(int(timeout.Seconds())), host).CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return down("ping failed: " + lines[len(lines)-1])
	}
	if mm := pingAvgRe.FindStringSubmatch(string(out)); mm != nil {
		if v, err := strconv.ParseFloat(mm[1], 64); err == nil {
			return Result{Up: true, Msg: "replies received", Ping: &v}
		}
	}
	return Result{Up: true, Msg: "replies received"}
}

func checkDNS(ctx context.Context, m *store.Monitor, _ *Env) Result {
	host := strings.TrimSpace(m.Config.Hostname)
	if host == "" {
		return down("hostname is required")
	}
	server := strings.TrimSpace(m.Config.DNSResolver)
	if server == "" {
		server = "1.1.1.1"
	}
	port := m.Config.Port
	if port <= 0 {
		port = 53
	}
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, strconv.Itoa(port))
	}
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, server)
		},
	}
	rtype := strings.ToUpper(m.Config.DNSRecordType)
	if rtype == "" {
		rtype = "A"
	}

	start := time.Now()
	var records []string
	var err error
	switch rtype {
	case "A", "AAAA":
		var ips []net.IPAddr
		ips, err = r.LookupIPAddr(ctx, host)
		for _, ip := range ips {
			is4 := ip.IP.To4() != nil
			if (rtype == "A") == is4 {
				records = append(records, ip.IP.String())
			}
		}
	case "CNAME":
		var c string
		c, err = r.LookupCNAME(ctx, host)
		if c != "" {
			records = append(records, c)
		}
	case "MX":
		var mx []*net.MX
		mx, err = r.LookupMX(ctx, host)
		for _, x := range mx {
			records = append(records, fmt.Sprintf("%d %s", x.Pref, x.Host))
		}
	case "NS":
		var ns []*net.NS
		ns, err = r.LookupNS(ctx, host)
		for _, x := range ns {
			records = append(records, x.Host)
		}
	case "TXT":
		records, err = r.LookupTXT(ctx, host)
	case "SRV":
		var srv []*net.SRV
		_, srv, err = r.LookupSRV(ctx, "", "", host)
		for _, x := range srv {
			records = append(records, fmt.Sprintf("%d %d %d %s", x.Priority, x.Weight, x.Port, x.Target))
		}
	default:
		return down("unsupported record type " + rtype)
	}
	elapsed := time.Since(start)
	if err != nil {
		return down(shortNetErr(err))
	}
	if len(records) == 0 {
		return down("no " + rtype + " records")
	}
	joined := strings.Join(records, ", ")
	if exp := strings.TrimSpace(m.Config.ExpectedValue); exp != "" && !strings.Contains(joined, exp) {
		return down(fmt.Sprintf("%s records %q do not contain %q", rtype, joined, exp))
	}
	if len(joined) > 200 {
		joined = joined[:200] + "…"
	}
	return up(rtype+": "+joined, elapsed)
}
