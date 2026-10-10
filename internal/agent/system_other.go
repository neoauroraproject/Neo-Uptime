//go:build !linux

package agent

import (
	"os"
	"runtime"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
)

// The agent is meant for Linux servers; elsewhere it reports what Go knows,
// which is enough to develop and test against.
type systemSampler struct{}

func newSystemSampler() *systemSampler { return &systemSampler{} }

func (s *systemSampler) sample() agentproto.System {
	h, _ := os.Hostname()
	return agentproto.System{Hostname: h, OS: runtime.GOOS, Arch: runtime.GOARCH, CPUCores: runtime.NumCPU()}
}
