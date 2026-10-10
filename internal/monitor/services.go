package monitor

import (
	"bufio"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

// ---------- Docker ----------

func checkDocker(ctx context.Context, m *store.Monitor, _ *Env) Result {
	name := strings.TrimSpace(m.Config.ContainerName)
	if name == "" {
		return down("container name is required")
	}
	host := strings.TrimSpace(m.Config.DockerHost)
	if host == "" {
		host = "/var/run/docker.sock"
	}
	base := "http://docker"
	tr := &http.Transport{DisableKeepAlives: true}
	switch {
	case strings.HasPrefix(host, "tcp://"), strings.HasPrefix(host, "http://"), strings.HasPrefix(host, "https://"):
		base = strings.TrimRight(strings.Replace(host, "tcp://", "http://", 1), "/")
	default:
		sock := strings.TrimPrefix(host, "unix://")
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/containers/"+url.PathEscape(name)+"/json", nil)
	start := time.Now()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return down("docker: " + shortNetErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return down("container " + name + " not found")
	}
	if resp.StatusCode != http.StatusOK {
		return down(fmt.Sprintf("docker API answered HTTP %d", resp.StatusCode))
	}
	var info struct {
		State struct {
			Status  string `json:"Status"`
			Running bool   `json:"Running"`
			Health  *struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&info); err != nil {
		return down("docker: unreadable answer")
	}
	elapsed := time.Since(start)
	if !info.State.Running {
		return down("container is " + info.State.Status)
	}
	if h := info.State.Health; h != nil {
		switch h.Status {
		case "healthy":
			return up("running, healthy", elapsed)
		case "unhealthy":
			return down("running but unhealthy")
		default:
			return up("running, health "+h.Status, elapsed)
		}
	}
	return up("running", elapsed)
}

// ---------- SQL databases ----------

func checkSQL(ctx context.Context, driver, dsn, query string) Result {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return down(err.Error())
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	start := time.Now()
	if err := db.PingContext(ctx); err != nil {
		return down(shortNetErr(err))
	}
	if strings.TrimSpace(query) == "" {
		return up("connected", time.Since(start))
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return down("query failed: " + err.Error())
	}
	n := 0
	for rows.Next() {
		n++
	}
	rows.Close()
	return up(fmt.Sprintf("query returned %d row(s)", n), time.Since(start))
}

func checkPostgres(ctx context.Context, m *store.Monitor, _ *Env) Result {
	if m.Config.ConnectionString == "" {
		return down("connection string is required")
	}
	return checkSQL(ctx, "pgx", m.Config.ConnectionString, m.Config.Query)
}

func checkMySQL(ctx context.Context, m *store.Monitor, _ *Env) Result {
	dsn := m.Config.ConnectionString
	if dsn == "" {
		return down("connection string is required")
	}
	// Accept the URL form Kuma uses, mysql://user:pass@host:port/db.
	if strings.HasPrefix(dsn, "mysql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return down("invalid connection URL")
		}
		cfg := mysql.NewConfig()
		cfg.User = u.User.Username()
		cfg.Passwd, _ = u.User.Password()
		cfg.Net = "tcp"
		cfg.Addr = u.Host
		if u.Port() == "" {
			cfg.Addr = net.JoinHostPort(u.Hostname(), "3306")
		}
		cfg.DBName = strings.TrimPrefix(u.Path, "/")
		cfg.Timeout = 15 * time.Second
		dsn = cfg.FormatDSN()
	}
	return checkSQL(ctx, "mysql", dsn, m.Config.Query)
}

// ---------- Redis ----------

// checkRedis speaks just enough RESP to authenticate and PING, which is all a
// liveness check needs and saves pulling in a client library.
func checkRedis(ctx context.Context, m *store.Monitor, _ *Env) Result {
	raw := strings.TrimSpace(m.Config.ConnectionString)
	if raw == "" {
		return down("connection string is required")
	}
	if !strings.Contains(raw, "://") {
		raw = "redis://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return down("invalid Redis URL")
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "6379")
	}
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return down(shortNetErr(err))
	}
	if u.Scheme == "rediss" {
		tc := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), InsecureSkipVerify: m.Config.IgnoreTLS}) //nolint:gosec
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			return down("TLS: " + err.Error())
		}
		conn = tc
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	rd := bufio.NewReader(conn)
	send := func(args ...string) (string, error) {
		var b strings.Builder
		fmt.Fprintf(&b, "*%d\r\n", len(args))
		for _, a := range args {
			fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
		}
		if _, err := io.WriteString(conn, b.String()); err != nil {
			return "", err
		}
		line, err := rd.ReadString('\n')
		return strings.TrimSpace(line), err
	}
	if pass, ok := u.User.Password(); ok {
		args := []string{"AUTH", pass}
		if name := u.User.Username(); name != "" && name != "default" {
			args = []string{"AUTH", name, pass}
		}
		if r, err := send(args...); err != nil || !strings.HasPrefix(r, "+") {
			return down("AUTH failed: " + firstNonEmpty(strings.TrimPrefix(r, "-"), errString(err)))
		}
	}
	if dbn := strings.TrimPrefix(u.Path, "/"); dbn != "" {
		if _, err := strconv.Atoi(dbn); err == nil {
			if r, err := send("SELECT", dbn); err != nil || !strings.HasPrefix(r, "+") {
				return down("SELECT failed: " + firstNonEmpty(strings.TrimPrefix(r, "-"), errString(err)))
			}
		}
	}
	r, err := send("PING")
	if err != nil {
		return down(shortNetErr(err))
	}
	if r != "+PONG" {
		return down("unexpected reply: " + strings.TrimPrefix(r, "-"))
	}
	return up("PONG", time.Since(start))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
