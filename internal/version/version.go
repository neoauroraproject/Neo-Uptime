// Package version carries the build's version, set at link time:
//
//	go build -ldflags "-X github.com/neoauroraproject/Neo-Uptime/internal/version.Version=v1.2.3"
package version

// Version is the release tag this binary was built from.
var Version = "dev"

// Repo is where releases are published.
const Repo = "neoauroraproject/Neo-Uptime"
