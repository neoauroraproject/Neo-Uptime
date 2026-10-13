package xray

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
)

// Speed is what a speed test measured through one config.
type Speed struct {
	Down   float64 `json:"down"`   // Mbps
	Up     float64 `json:"up"`     // Mbps; 0 when the upload could not be measured
	Ping   float64 `json:"ping"`   // ms, average of a few small requests
	Jitter float64 `json:"jitter"` // ms, how much those vary
	Server string  `json:"server"` // where the data came from
	Bytes  int64   `json:"bytes"`  // moved in all, both ways: what it cost the account
	// UpError says why the upload was skipped, when it was.
	UpError string `json:"upError,omitempty"`
	// Mbps over time, one sample every 200 ms, for drawing the run.
	DownSamples []float64 `json:"downSamples,omitempty"`
	UpSamples   []float64 `json:"upSamples,omitempty"`
}

type speedServer struct {
	name string
	down func(n int64) string
	up   string
}

// speedList is the speed test servers in the order they are tried: one of
// your own first, when NEO_UPTIME_SPEED_URL names it (it answers like
// Cloudflare's: GET <url>/__down?bytes=N and POST <url>/__up).
func speedList() []speedServer {
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("NEO_UPTIME_SPEED_URL")), "/"); base != "" {
		host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
		own := speedServer{host, func(n int64) string { return fmt.Sprintf("%s/__down?bytes=%d", base, n) }, base + "/__up"}
		return append([]speedServer{own}, speedServers...)
	}
	return speedServers
}

// speedServers are the built-in ones. The first that answers a small
// request is used for the download. Each takes a request for n bytes.
var speedServers = []speedServer{
	{"speed.cloudflare.com", func(n int64) string { return fmt.Sprintf("https://speed.cloudflare.com/__down?bytes=%d", n) }, "https://speed.cloudflare.com/__up"},
	{"proof.ovh.net", func(int64) string { return "https://proof.ovh.net/files/1Gb.dat" }, ""},
}

const (
	speedStreams = 4
	speedChunk   = 100 << 20 // one download request; ends early at the deadline
	speedUpChunk = 50 << 20
	speedMaxDown = 400 << 20 // a phase ends at this much, however fast the line is
	speedMaxUp   = 150 << 20
	speedPings   = 8
	sampleEvery  = 200 * time.Millisecond
)

// How long each way is measured (variables so tests can be short).
var (
	speedWarm    = 1500 * time.Millisecond // ramp-up, not counted
	speedDownFor = 8 * time.Second
	speedUpFor   = 6 * time.Second
)

// httpClient sends requests through an Xray instance.
func httpClient(inst *core.Instance) (*http.Client, func()) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return core.Dial(ctx, inst, dest)
		},
		MaxIdleConnsPerHost: speedStreams,
		DisableCompression:  true,
	}
	return &http.Client{Transport: tr}, tr.CloseIdleConnections
}

// MeasureSpeed connects through the link and measures latency, then download
// and upload speed with several parallel connections, a few seconds each
// way. report, when set, hears how it goes about five times a second.
func MeasureSpeed(ctx context.Context, l *Link, report func(agentproto.SpeedProgress)) (*Speed, error) {
	select {
	case tests <- struct{}{}:
		defer func() { <-tests }()
	case <-ctx.Done():
		return nil, errors.New("timed out waiting for other tests")
	}
	prog := agentproto.SpeedProgress{Phase: "ping"}
	emit := func() {
		if report != nil {
			report(prog)
		}
	}
	emit()
	cfg, err := build(l)
	if err != nil {
		return nil, fmt.Errorf("xray rejected the config: %w", err)
	}
	inst, err := Start(cfg)
	if err != nil {
		return nil, fmt.Errorf("xray could not start the config: %w", err)
	}
	defer inst.Close()
	client, done := httpClient(inst)
	defer done()

	res := &Speed{}
	var total atomic.Int64

	// The first server that answers a small request is the one used.
	pick := -1
	var lastErr error
	servers := speedList()
	var pings []float64
	for i, srv := range servers {
		d, err := smallRequest(ctx, client, srv.down(1))
		if err != nil {
			lastErr = err
			continue
		}
		pick = i
		pings = append(pings, d)
		break
	}
	if pick < 0 {
		if why := diagnose(l); why != "" {
			return nil, errors.New(why)
		}
		return nil, fmt.Errorf("no speed test server could be reached through this config: %w", explain(ctx, lastErr))
	}
	srv := servers[pick]
	res.Server, prog.Server = srv.name, srv.name
	prog.Cur, prog.Pct = pings[0], 1/float64(speedPings)
	prog.Ping, prog.Jitter = pingStats(pings)
	emit()
	for len(pings) < speedPings {
		d, err := smallRequest(ctx, client, srv.down(1))
		if err != nil {
			if ctx.Err() != nil {
				return nil, explain(ctx, err)
			}
			break // a lost request must not end the test
		}
		pings = append(pings, d)
		prog.Cur, prog.Pct = d, float64(len(pings))/float64(speedPings)
		prog.Ping, prog.Jitter = pingStats(pings)
		emit()
	}
	res.Ping, res.Jitter = pingStats(pings)
	prog.Ping, prog.Jitter = res.Ping, res.Jitter

	prog.Phase, prog.Cur, prog.Pct = "download", 0, 0
	emit()
	down, samples, err := run(ctx, speedDownFor, speedMaxDown, func(cur, pct float64, s []float64) {
		prog.Cur, prog.Pct, prog.DownSamples = cur, pct, s
		emit()
	}, func(c context.Context, n *atomic.Int64) error {
		return download(c, client, srv.down(speedChunk), n, &total)
	})
	if err != nil {
		return nil, fmt.Errorf("download: %w", explain(ctx, err))
	}
	res.Down, res.DownSamples = down, samples
	prog.Down, prog.DownSamples = down, samples

	prog.Phase, prog.Cur, prog.Pct = "upload", 0, 0
	emit()
	if srv.up == "" {
		res.UpError = "this speed test server cannot measure upload"
	} else if up, samples, err := run(ctx, speedUpFor, speedMaxUp, func(cur, pct float64, s []float64) {
		prog.Cur, prog.Pct, prog.UpSamples = cur, pct, s
		emit()
	}, func(c context.Context, n *atomic.Int64) error {
		return upload(c, client, srv.up, n, &total)
	}); err != nil {
		res.UpError = explain(ctx, err).Error()
	} else {
		res.Up, res.UpSamples = up, samples
		prog.Up, prog.UpSamples = up, samples
	}
	res.Bytes = total.Load()
	return res, nil
}

// pingStats: the average of the requests, and how much they differ from
// one to the next (jitter).
func pingStats(p []float64) (avg, jitter float64) {
	if len(p) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range p {
		sum += v
	}
	avg = sum / float64(len(p))
	if len(p) > 1 {
		var d float64
		for i := 1; i < len(p); i++ {
			d += math.Abs(p[i] - p[i-1])
		}
		jitter = d / float64(len(p)-1)
	}
	return avg, jitter
}

// smallRequest times a request that moves next to no data.
func smallRequest(ctx context.Context, client *http.Client, url string) (float64, error) {
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	if strings.Contains(url, "proof.ovh.net") {
		req.Header.Set("Range", "bytes=0-0")
	}
	t0 := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return float64(time.Since(t0).Microseconds()) / 1000, nil
}

// run starts the streams and measures what they move over duration, not
// counting the first moments while the connections ramp up. It returns the
// speed in Mbps and the speed over time (one value per sampleEvery), and
// tells tick what the speed is right now, about every sampleEvery.
func run(ctx context.Context, d time.Duration, limit int64, tick func(cur, pct float64, samples []float64), stream func(context.Context, *atomic.Int64) error) (float64, []float64, error) {
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	var moved atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, speedStreams)
	for range speedStreams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rctx.Err() == nil {
				if moved.Load() >= limit {
					return
				}
				if err := stream(rctx, &moved); err != nil && rctx.Err() == nil {
					errs <- err
					return
				}
			}
		}()
	}
	t0 := time.Now()
	var atWarm int64
	warmed := false
	var samples []float64
	// Speed right now: what moved over the last second.
	type mark struct {
		at    time.Time
		bytes int64
	}
	window := []mark{{t0, 0}}
	t := time.NewTicker(sampleEvery)
	defer t.Stop()
	for loop := true; loop; {
		select {
		case <-rctx.Done():
			loop = false
		case now := <-t.C:
			b := moved.Load()
			if !warmed && now.Sub(t0) >= speedWarm {
				warmed, atWarm = true, b
			}
			window = append(window, mark{now, b})
			for len(window) > 2 && now.Sub(window[0].at) > time.Second {
				window = window[1:]
			}
			first := window[0]
			cur := float64(b-first.bytes) * 8 / now.Sub(first.at).Seconds() / 1e6
			samples = append(samples, math.Round(cur*10)/10)
			if tick != nil {
				tick(cur, math.Min(1, float64(now.Sub(t0))/float64(d)), slices.Clone(samples))
			}
			// A fast line must not make the test move gigabytes.
			if b >= limit {
				cancel()
			}
		}
	}
	elapsed := time.Since(t0)
	wg.Wait()
	end := moved.Load()
	if end == 0 {
		select {
		case err := <-errs:
			return 0, nil, err
		default:
		}
		return 0, nil, errors.New("no data came through")
	}
	if !warmed || elapsed <= speedWarm+time.Second {
		// A line so fast the phase ended during the warm-up: measure it all.
		return float64(end) * 8 / elapsed.Seconds() / 1e6, samples, nil
	}
	return float64(end-atWarm) * 8 / (elapsed - speedWarm).Seconds() / 1e6, samples, nil
}

func download(ctx context.Context, client *http.Client, url string, n, total *atomic.Int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, 64<<10)
	for {
		k, err := resp.Body.Read(buf)
		n.Add(int64(k))
		total.Add(int64(k))
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// noise is what uploads send: random, so nothing compresses it away.
var noise = sync.OnceValue(func() []byte {
	b := make([]byte, 1<<20)
	for i := range b {
		b[i] = byte(rand.IntN(256))
	}
	return b
})

type counted struct {
	left     int64
	n, total *atomic.Int64
	off      int
}

func (c *counted) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, io.EOF
	}
	data := noise()
	k := copy(p, data[c.off:])
	if int64(k) > c.left {
		k = int(c.left)
	}
	c.off = (c.off + k) % len(data)
	c.left -= int64(k)
	c.n.Add(int64(k))
	c.total.Add(int64(k))
	return k, nil
}

func upload(ctx context.Context, client *http.Client, url string, n, total *atomic.Int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &counted{left: speedUpChunk, n: n, total: total})
	if err != nil {
		return err
	}
	req.ContentLength = speedUpChunk
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
