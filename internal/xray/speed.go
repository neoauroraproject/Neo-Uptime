package xray

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
)

// Speed is what a speed test measured through one config.
type Speed struct {
	Down   float64 `json:"down"`   // Mbps
	Up     float64 `json:"up"`     // Mbps; 0 when the upload could not be measured
	Ping   float64 `json:"ping"`   // ms, a small request once connected
	Server string  `json:"server"` // where the data came from
	Bytes  int64   `json:"bytes"`  // moved in all, both ways: what it cost the account
	// UpError says why the upload was skipped, when it was.
	UpError string `json:"upError,omitempty"`
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

// MeasureSpeed connects through the link and measures download and upload
// speed with several parallel connections, for a few seconds each way.
func MeasureSpeed(ctx context.Context, l *Link) (*Speed, error) {
	select {
	case tests <- struct{}{}:
		defer func() { <-tests }()
	case <-ctx.Done():
		return nil, errors.New("timed out waiting for other tests")
	}
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

	// Download: the first server that answers a small request.
	pick := -1
	var lastErr error
	servers := speedList()
	for i, srv := range servers {
		d, err := smallRequest(ctx, client, srv.down(1))
		if err != nil {
			lastErr = err
			continue
		}
		pick = i
		if res.Ping == 0 || d < res.Ping {
			res.Ping = d
		}
		break
	}
	if pick < 0 {
		if why := diagnose(l); why != "" {
			return nil, errors.New(why)
		}
		return nil, fmt.Errorf("no speed test server could be reached through this config: %w", explain(ctx, lastErr))
	}
	srv := servers[pick]
	res.Server = srv.name
	for range 2 { // the connection is warm now: a better ping
		if d, err := smallRequest(ctx, client, srv.down(1)); err == nil && d < res.Ping {
			res.Ping = d
		}
	}

	down, err := run(ctx, speedDownFor, speedMaxDown, &total, func(c context.Context, n *atomic.Int64) error {
		return download(c, client, srv.down(speedChunk), n)
	})
	if err != nil {
		return nil, fmt.Errorf("download: %w", explain(ctx, err))
	}
	res.Down = down

	if srv.up == "" {
		res.UpError = "this speed test server cannot measure upload"
	} else if up, err := run(ctx, speedUpFor, speedMaxUp, &total, func(c context.Context, n *atomic.Int64) error {
		return upload(c, client, srv.up, n)
	}); err != nil {
		res.UpError = explain(ctx, err).Error()
	} else {
		res.Up = up
	}
	res.Bytes = total.Load()
	return res, nil
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
// counting the first moments while the connections ramp up. Mbps.
func run(ctx context.Context, d time.Duration, limit int64, total *atomic.Int64, stream func(context.Context, *atomic.Int64) error) (float64, error) {
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
	warm := time.NewTimer(speedWarm)
	defer warm.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for loop := true; loop; {
		select {
		case <-warm.C:
			atWarm = moved.Load()
		case <-tick.C:
			// A fast line must not make the test move gigabytes.
			if moved.Load() >= limit {
				cancel()
			}
		case <-rctx.Done():
			loop = false
		}
	}
	elapsed := time.Since(t0)
	wg.Wait()
	end := moved.Load()
	total.Add(end)
	if end == 0 {
		select {
		case err := <-errs:
			return 0, err
		default:
		}
		return 0, errors.New("no data came through")
	}
	if elapsed <= speedWarm+time.Second {
		// A line so fast the phase ended during the warm-up: measure it all.
		return float64(end) * 8 / elapsed.Seconds() / 1e6, nil
	}
	return float64(end-atWarm) * 8 / (elapsed - speedWarm).Seconds() / 1e6, nil
}

func download(ctx context.Context, client *http.Client, url string, n *atomic.Int64) error {
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
	left int64
	n    *atomic.Int64
	off  int
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
	return k, nil
}

func upload(ctx context.Context, client *http.Client, url string, n *atomic.Int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &counted{left: speedUpChunk, n: n})
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
