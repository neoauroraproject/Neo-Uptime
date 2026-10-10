//go:build linux

package agent

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
)

type cpuTimes struct{ idle, total uint64 }

type systemSampler struct {
	mu      sync.Mutex
	lastCPU cpuTimes
	lastRx  uint64
	lastTx  uint64
	lastAt  time.Time
}

func newSystemSampler() *systemSampler {
	s := &systemSampler{}
	s.lastCPU = readCPU()
	s.lastRx, s.lastTx = readNet()
	s.lastAt = time.Now()
	return s
}

func readCPU() cpuTimes {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return cpuTimes{}
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}
	}
	var t cpuTimes
	for i, v := range fields[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		t.total += n
		if i == 3 || i == 4 { // idle + iowait
			t.idle += n
		}
	}
	return t
}

func readNet() (rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(f[0], 10, 64)
		t, _ := strconv.ParseUint(f[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx
}

func readMeminfo() map[string]uint64 {
	out := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fields[0], 10, 64)
		out[k] = n * 1024
	}
	return out
}

func countEstablished() int {
	n := 0
	for _, p := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Scan() // header
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) > 3 && fields[3] == "01" {
				n++
			}
		}
		f.Close()
	}
	return n
}

func osRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return "Linux"
}

func (s *systemSampler) sample() agentproto.System {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := agentproto.System{OS: osRelease(), Arch: runtime.GOARCH, CPUCores: runtime.NumCPU()}
	out.Hostname, _ = os.Hostname()
	if k, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		out.Kernel = strings.TrimSpace(string(k))
	}
	if u, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(u)); len(f) > 0 {
			v, _ := strconv.ParseFloat(f[0], 64)
			out.UptimeSecs = int64(v)
		}
	}
	if l, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(l)); len(f) >= 3 {
			out.Load1, _ = strconv.ParseFloat(f[0], 64)
			out.Load5, _ = strconv.ParseFloat(f[1], 64)
			out.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}

	cpu := readCPU()
	if dt := cpu.total - s.lastCPU.total; dt > 0 && cpu.total > s.lastCPU.total {
		idle := cpu.idle - s.lastCPU.idle
		out.CPUPercent = 100 * float64(dt-idle) / float64(dt)
	}
	s.lastCPU = cpu

	mem := readMeminfo()
	out.MemTotal = mem["MemTotal"]
	if avail, ok := mem["MemAvailable"]; ok && avail <= out.MemTotal {
		out.MemUsed = out.MemTotal - avail
	}
	out.SwapTotal = mem["SwapTotal"]
	if mem["SwapFree"] <= out.SwapTotal {
		out.SwapUsed = out.SwapTotal - mem["SwapFree"]
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err == nil {
		bs := uint64(st.Bsize)
		out.DiskTotal = st.Blocks * bs
		out.DiskUsed = (st.Blocks - st.Bfree) * bs
	}

	rx, tx := readNet()
	now := time.Now()
	if secs := now.Sub(s.lastAt).Seconds(); secs > 0.5 && rx >= s.lastRx && tx >= s.lastTx {
		out.NetRxRate = float64(rx-s.lastRx) / secs
		out.NetTxRate = float64(tx-s.lastTx) / secs
	}
	out.NetRxBytes, out.NetTxBytes = rx, tx
	s.lastRx, s.lastTx, s.lastAt = rx, tx, now
	out.TCPConns = countEstablished()
	return out
}
