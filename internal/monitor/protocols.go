package monitor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gosnmp/gosnmp"
	_ "github.com/microsoft/go-mssqldb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"golang.org/x/net/http2"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
)

func hostPort(c store.MonitorConfig, def int) (string, error) {
	host := strings.TrimSpace(c.Hostname)
	if host == "" {
		return "", errors.New("hostname is required")
	}
	port := c.Port
	if port <= 0 {
		port = def
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func certInfo(cs *tls.ConnectionState) *CertInfo {
	if cs == nil || len(cs.PeerCertificates) == 0 {
		return nil
	}
	pc := cs.PeerCertificates[0]
	return &CertInfo{
		Subject: pc.Subject.CommonName, Issuer: firstNonEmpty(pc.Issuer.CommonName, strings.Join(pc.Issuer.Organization, ", ")),
		ValidFrom: pc.NotBefore.Unix(), ValidTo: pc.NotAfter.Unix(), DaysRemaining: int(time.Until(pc.NotAfter).Hours() / 24),
		Valid: time.Now().Before(pc.NotAfter) && time.Now().After(pc.NotBefore), DNSNames: pc.DNSNames,
	}
}

// ---------- SMTP ----------

func checkSMTP(ctx context.Context, m *store.Monitor, _ *Env) Result {
	c := m.Config
	def := 25
	switch c.Security {
	case "tls":
		def = 465
	case "starttls":
		def = 587
	}
	addr, err := hostPort(c, def)
	if err != nil {
		return down(err.Error())
	}
	tlsCfg := &tls.Config{ServerName: c.Hostname, InsecureSkipVerify: c.IgnoreTLS} //nolint:gosec
	start := time.Now()
	var conn net.Conn
	d := &net.Dialer{}
	if c.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return down(shortNetErr(err))
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	cl, err := smtp.NewClient(conn, c.Hostname)
	if err != nil {
		conn.Close()
		return down("no SMTP greeting: " + err.Error())
	}
	defer cl.Close()
	if err := cl.Hello("neo-uptime.local"); err != nil {
		return down("EHLO refused: " + err.Error())
	}
	var cert *CertInfo
	if c.Security == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return down("server does not offer STARTTLS")
		}
		if err := cl.StartTLS(tlsCfg); err != nil {
			return down("STARTTLS: " + err.Error())
		}
	}
	if cs, ok := cl.TLSConnectionState(); ok {
		cert = certInfo(&cs)
	}
	_ = cl.Quit()
	r := up("SMTP answered", time.Since(start))
	r.Cert = cert
	return r
}

// ---------- MongoDB / SQL Server ----------

func checkMongo(ctx context.Context, m *store.Monitor, _ *Env) Result {
	uri := strings.TrimSpace(m.Config.ConnectionString)
	if uri == "" {
		return down("connection string is required")
	}
	start := time.Now()
	opts := options.Client().ApplyURI(uri).SetServerSelectionTimeout(time.Duration(m.Timeout) * time.Second).SetMaxPoolSize(1)
	cl, err := mongo.Connect(opts)
	if err != nil {
		return down(err.Error())
	}
	defer cl.Disconnect(context.Background()) //nolint:errcheck
	if err := cl.Ping(ctx, readpref.Primary()); err != nil {
		return down(shortNetErr(err))
	}
	return up("ping ok", time.Since(start))
}

func checkMSSQL(ctx context.Context, m *store.Monitor, _ *Env) Result {
	if m.Config.ConnectionString == "" {
		return down("connection string is required")
	}
	return checkSQL(ctx, "sqlserver", m.Config.ConnectionString, m.Config.Query)
}

// ---------- MQTT ----------

func checkMQTT(ctx context.Context, m *store.Monitor, _ *Env) Result {
	c := m.Config
	host := strings.TrimSpace(c.Hostname)
	if host == "" {
		return down("hostname is required")
	}
	scheme := "tcp"
	if c.Security == "tls" {
		scheme = "ssl"
	}
	if strings.Contains(host, "://") {
		scheme, host, _ = strings.Cut(host, "://")
	}
	port := c.Port
	if port <= 0 {
		port = map[string]int{"ssl": 8883, "tls": 8883, "wss": 443, "ws": 80}[scheme]
		if port == 0 {
			port = 1883
		}
	}
	timeout := time.Duration(max(m.Timeout, 3)) * time.Second
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, strconv.Itoa(port)))).
		SetClientID("neo-uptime-" + randHex(6)).
		SetConnectTimeout(timeout).SetAutoReconnect(false).SetConnectRetry(false).
		SetTLSConfig(&tls.Config{InsecureSkipVerify: c.IgnoreTLS}) //nolint:gosec
	if c.Username != "" {
		opts.SetUsername(c.Username).SetPassword(c.Password)
	}
	start := time.Now()
	cl := mqtt.NewClient(opts)
	tok := cl.Connect()
	if !tok.WaitTimeout(timeout) {
		return down("timeout connecting to the broker")
	}
	if err := tok.Error(); err != nil {
		return down(err.Error())
	}
	defer cl.Disconnect(100)
	if c.Topic == "" {
		return up("connected", time.Since(start))
	}
	got := make(chan string, 1)
	sub := cl.Subscribe(c.Topic, 0, func(_ mqtt.Client, msg mqtt.Message) {
		select {
		case got <- string(msg.Payload()):
		default:
		}
	})
	if !sub.WaitTimeout(timeout) || sub.Error() != nil {
		return down(fmt.Sprintf("could not subscribe to %s: %v", c.Topic, sub.Error()))
	}
	select {
	case payload := <-got:
		if c.Keyword != "" && !strings.Contains(payload, c.Keyword) {
			return down(fmt.Sprintf("message on %s lacks %q: %s", c.Topic, c.Keyword, truncate(payload, 80)))
		}
		return up("message: "+truncate(payload, 120), time.Since(start))
	case <-ctx.Done():
		return down("no message on " + c.Topic + " in time")
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// ---------- gRPC health ----------

// checkGRPC calls grpc.health.v1.Health/Check over HTTP/2 directly, so the
// whole gRPC stack is not needed for one well-known request.
func checkGRPC(ctx context.Context, m *store.Monitor, _ *Env) Result {
	c := m.Config
	addr, err := hostPort(c, 443)
	if err != nil {
		return down(err.Error())
	}
	secure := c.Security == "tls"
	tr := &http2.Transport{}
	if secure {
		tr.TLSClientConfig = &tls.Config{ServerName: c.Hostname, InsecureSkipVerify: c.IgnoreTLS} //nolint:gosec
	} else {
		tr.AllowHTTP = true
		tr.DialTLSContext = func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}
	}
	defer tr.CloseIdleConnections()
	// HealthCheckRequest{service = 1: string}
	var msg []byte
	if c.Service != "" {
		msg = append([]byte{0x0a, byte(len(c.Service))}, c.Service...)
	}
	frame := make([]byte, 5+len(msg))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(msg)))
	copy(frame[5:], msg)
	scheme := "http"
	if secure {
		scheme = "https"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, scheme+"://"+addr+"/grpc.health.v1.Health/Check", bytes.NewReader(frame))
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")
	start := time.Now()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return down(shortNetErr(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	elapsed := time.Since(start)
	status := resp.Trailer.Get("Grpc-Status")
	if status == "" {
		status = resp.Header.Get("Grpc-Status")
	}
	if status != "" && status != "0" {
		return down(fmt.Sprintf("grpc-status %s: %s", status, resp.Trailer.Get("Grpc-Message")))
	}
	if resp.StatusCode != http.StatusOK || len(body) < 5 {
		return down(fmt.Sprintf("unexpected answer (HTTP %d)", resp.StatusCode))
	}
	payload := body[5:]
	serving := -1
	if len(payload) >= 2 && payload[0] == 0x08 {
		serving = int(payload[1])
	}
	names := map[int]string{0: "UNKNOWN", 1: "SERVING", 2: "NOT_SERVING", 3: "SERVICE_UNKNOWN"}
	if serving != 1 {
		return down("health: " + firstNonEmpty(names[serving], "no status"))
	}
	r := up("SERVING", elapsed)
	r.Cert = certInfo(resp.TLS)
	return r
}

// ---------- RADIUS ----------

func checkRadius(ctx context.Context, m *store.Monitor, _ *Env) Result {
	c := m.Config
	addr, err := hostPort(c, 1812)
	if err != nil {
		return down(err.Error())
	}
	if c.Secret == "" {
		return down("shared secret is required")
	}
	p := radius.New(radius.CodeAccessRequest, []byte(c.Secret))
	_ = rfc2865.UserName_SetString(p, c.Username)
	_ = rfc2865.UserPassword_SetString(p, c.Password)
	_ = rfc2865.NASIdentifier_SetString(p, "neo-uptime")
	start := time.Now()
	resp, err := radius.Exchange(ctx, p, addr)
	if err != nil {
		return down(shortNetErr(err))
	}
	if resp.Code != radius.CodeAccessAccept {
		return down("RADIUS answered " + resp.Code.String())
	}
	return up("Access-Accept", time.Since(start))
}

// ---------- SNMP ----------

func checkSNMP(ctx context.Context, m *store.Monitor, _ *Env) Result {
	c := m.Config
	if strings.TrimSpace(c.Hostname) == "" || strings.TrimSpace(c.OID) == "" {
		return down("hostname and OID are required")
	}
	port := c.Port
	if port <= 0 {
		port = 161
	}
	g := &gosnmp.GoSNMP{
		Target: c.Hostname, Port: uint16(port), Community: firstNonEmpty(c.Community, "public"),
		Version: gosnmp.Version2c, Timeout: time.Duration(max(m.Timeout, 2)) * time.Second, Retries: 1, Context: ctx,
	}
	if c.SNMPVersion == "1" {
		g.Version = gosnmp.Version1
	}
	start := time.Now()
	if err := g.Connect(); err != nil {
		return down(err.Error())
	}
	defer g.Conn.Close()
	res, err := g.Get([]string{c.OID})
	if err != nil {
		return down(shortNetErr(err))
	}
	if len(res.Variables) == 0 {
		return down("no value returned")
	}
	v := res.Variables[0]
	var val string
	switch v.Type {
	case gosnmp.OctetString:
		if b, ok := v.Value.([]byte); ok {
			val = string(b)
		}
	case gosnmp.NoSuchObject, gosnmp.NoSuchInstance:
		return down("no such OID: " + c.OID)
	default:
		val = gosnmp.ToBigInt(v.Value).String()
		if val == "0" && v.Value != nil {
			val = fmt.Sprint(v.Value)
		}
	}
	elapsed := time.Since(start)
	if c.ExpectedValue != "" || (c.JSONOperator != "" && c.JSONOperator != "==") {
		ok, err := compareValues(val, c.JSONOperator, c.ExpectedValue)
		if err != nil {
			return down(err.Error())
		}
		if !ok {
			return down(fmt.Sprintf("%s = %s, expected %s %s", c.OID, val, opLabel(c.JSONOperator), c.ExpectedValue))
		}
	}
	return up(fmt.Sprintf("%s = %s", c.OID, truncate(val, 80)), elapsed)
}

// ---------- Steam / Source game server (A2S_INFO) ----------

func checkSteam(ctx context.Context, m *store.Monitor, _ *Env) Result {
	addr, err := hostPort(m.Config, 27015)
	if err != nil {
		return down(err.Error())
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return down(err.Error())
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	query := append([]byte{0xFF, 0xFF, 0xFF, 0xFF, 'T'}, []byte("Source Engine Query\x00")...)
	start := time.Now()
	buf := make([]byte, 1400)
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := conn.Write(query); err != nil {
			return down(err.Error())
		}
		n, err := conn.Read(buf)
		if err != nil {
			return down("no answer: " + shortNetErr(err))
		}
		b := buf[:n]
		if len(b) < 5 {
			return down("short answer")
		}
		switch b[4] {
		case 0x41: // challenge: repeat with it appended
			if len(b) < 9 {
				return down("bad challenge")
			}
			query = append(query[:25], b[5:9]...)
			continue
		case 0x49:
			name, map_, players, maxp := parseA2S(b[5:])
			return up(fmt.Sprintf("%s · %s · %d/%d players", name, map_, players, maxp), time.Since(start))
		default:
			return down(fmt.Sprintf("unexpected reply type 0x%02x", b[4]))
		}
	}
	return down("server kept asking for a challenge")
}

func parseA2S(b []byte) (name, mapName string, players, maxPlayers int) {
	if len(b) < 1 {
		return
	}
	b = b[1:] // protocol
	cstr := func() string {
		i := bytes.IndexByte(b, 0)
		if i < 0 {
			s := string(b)
			b = nil
			return s
		}
		s := string(b[:i])
		b = b[i+1:]
		return s
	}
	name, mapName = cstr(), cstr()
	_, _ = cstr(), cstr() // folder, game
	if len(b) >= 4 {
		players, maxPlayers = int(b[2]), int(b[3])
	}
	return
}

// ---------- Kafka ----------

// checkKafka sends an ApiVersions request (key 18, v0) to the first broker:
// the cheapest call every broker answers, which proves it speaks Kafka.
func checkKafka(ctx context.Context, m *store.Monitor, _ *Env) Result {
	brokers := strings.Split(strings.TrimSpace(m.Config.Hostname), ",")
	addr := strings.TrimSpace(brokers[0])
	if addr == "" {
		return down("broker address is required")
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		port := m.Config.Port
		if port <= 0 {
			port = 9092
		}
		addr = net.JoinHostPort(addr, strconv.Itoa(port))
	}
	start := time.Now()
	var conn net.Conn
	var err error
	d := &net.Dialer{}
	if m.Config.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: d, Config: &tls.Config{InsecureSkipVerify: m.Config.IgnoreTLS}}).DialContext(ctx, "tcp", addr) //nolint:gosec
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return down(shortNetErr(err))
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	client := "neo-uptime"
	req := make([]byte, 0, 32)
	req = binary.BigEndian.AppendUint16(req, 18) // ApiVersions
	req = binary.BigEndian.AppendUint16(req, 0)
	req = binary.BigEndian.AppendUint32(req, 7) // correlation id
	req = binary.BigEndian.AppendUint16(req, uint16(len(client)))
	req = append(req, client...)
	frame := binary.BigEndian.AppendUint32(nil, uint32(len(req)))
	if _, err := conn.Write(append(frame, req...)); err != nil {
		return down(err.Error())
	}
	head := make([]byte, 10)
	if _, err := io.ReadFull(conn, head); err != nil {
		return down("no answer: " + shortNetErr(err))
	}
	if binary.BigEndian.Uint32(head[4:8]) != 7 {
		return down("answer is not from a Kafka broker")
	}
	if code := int16(binary.BigEndian.Uint16(head[8:10])); code != 0 {
		return down(fmt.Sprintf("broker returned error code %d", code))
	}
	return up("broker answered ApiVersions", time.Since(start))
}

// ---------- UDP ----------

func checkUDP(ctx context.Context, m *store.Monitor, _ *Env) Result {
	addr, err := hostPort(m.Config, 0)
	if err != nil {
		return down(err.Error())
	}
	if m.Config.Port <= 0 {
		return down("port is required")
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return down(err.Error())
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	payload := []byte(m.Config.Payload)
	if len(payload) == 0 {
		payload = []byte{0}
	}
	start := time.Now()
	if _, err := conn.Write(payload); err != nil {
		return down(err.Error())
	}
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	elapsed := time.Since(start)
	expect := m.Config.ExpectedValue
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() && expect == "" {
			// UDP never confirms receipt: silence is all an open port gives.
			return Result{Up: true, Msg: "no reply (open or filtered) — port not refused"}
		}
		if strings.Contains(err.Error(), "refused") {
			return down("port unreachable (ICMP refused)")
		}
		return down(shortNetErr(err))
	}
	reply := string(buf[:n])
	if expect != "" && !strings.Contains(reply, expect) {
		return down(fmt.Sprintf("reply lacks %q", expect))
	}
	return up(fmt.Sprintf("replied with %d bytes", n), elapsed)
}

// ---------- WebSocket ----------

func checkWebSocket(ctx context.Context, m *store.Monitor, _ *Env) Result {
	u := strings.TrimSpace(m.Config.URL)
	switch {
	case strings.HasPrefix(u, "ws://"):
		u = "http://" + strings.TrimPrefix(u, "ws://")
	case strings.HasPrefix(u, "wss://"):
		u = "https://" + strings.TrimPrefix(u, "wss://")
	default:
		return down("URL must start with ws:// or wss://")
	}
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return down(err.Error())
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", base64.StdEncoding.EncodeToString(key))
	if m.Config.Headers != "" {
		var hs map[string]string
		if jsonErr := jsonUnmarshal(m.Config.Headers, &hs); jsonErr == nil {
			for k, v := range hs {
				req.Header.Set(k, v)
			}
		}
	}
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: m.Config.IgnoreTLS}, //nolint:gosec
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		DisableKeepAlives: true,
	}
	start := time.Now()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return down(shortNetErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return down(fmt.Sprintf("no upgrade: HTTP %d", resp.StatusCode))
	}
	r := up("101 Switching Protocols", time.Since(start))
	r.Cert = certInfo(resp.TLS)
	return r
}

// ---------- TLS certificate ----------

func checkTLS(ctx context.Context, m *store.Monitor, _ *Env) Result {
	addr, err := hostPort(m.Config, 443)
	if err != nil {
		return down(err.Error())
	}
	start := time.Now()
	conn, err := (&tls.Dialer{Config: &tls.Config{ServerName: m.Config.Hostname, InsecureSkipVerify: m.Config.IgnoreTLS}}).DialContext(ctx, "tcp", addr) //nolint:gosec
	if err != nil {
		return down(shortNetErr(err))
	}
	defer conn.Close()
	cs := conn.(*tls.Conn).ConnectionState()
	ci := certInfo(&cs)
	elapsed := time.Since(start)
	if ci == nil {
		return down("no certificate presented")
	}
	warn := m.Config.WarnDays
	if warn <= 0 {
		warn = 14
	}
	var r Result
	switch {
	case !ci.Valid:
		r = down("certificate is not valid now")
	case ci.DaysRemaining < warn:
		r = down(fmt.Sprintf("certificate expires in %d days (warning below %d)", ci.DaysRemaining, warn))
	default:
		r = up(fmt.Sprintf("valid for %d more days · %s", ci.DaysRemaining, ci.Issuer), elapsed)
	}
	r.Cert = ci
	return r
}
