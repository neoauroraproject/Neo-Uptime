VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/neoauroraproject/Neo-Uptime/internal/version.Version=$(VERSION)
GO      ?= go

.PHONY: all web build dev test linux clean

all: web build

web:
	cd web && npm ci && npm run build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o neo-uptime ./cmd/neo-uptime
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o neo-uptime-agent ./cmd/neo-uptime-agent

test:
	$(GO) vet ./...
	$(GO) test ./...

# Release archives for every supported Linux CPU, as the release workflow builds them.
linux: web
	mkdir -p dist
	for t in amd64 arm64 armv7; do \
	  arch=$$t; goarm=; [ $$t = armv7 ] && arch=arm && goarm=7; \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$arch GOARM=$$goarm $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/neo-uptime ./cmd/neo-uptime && \
	  tar -czf dist/neo-uptime-linux-$$t.tar.gz -C dist neo-uptime -C .. README.md LICENSE && rm dist/neo-uptime; \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$arch GOARM=$$goarm $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/neo-uptime-agent ./cmd/neo-uptime-agent && \
	  tar -czf dist/neo-uptime-agent-linux-$$t.tar.gz -C dist neo-uptime-agent -C .. LICENSE agent.sh && rm dist/neo-uptime-agent; \
	done
	cd dist && sha256sum *.tar.gz > checksums.txt

# Backend on :7788 with a mock BackPack panel on :7777, UI with hot reload on :5173.
dev:
	$(GO) run ./cmd/mock-backpack -addr 127.0.0.1:7777 -token test-token & \
	$(GO) run ./cmd/neo-uptime serve --host 127.0.0.1 --data ./data & \
	cd web && npm run dev

clean:
	rm -rf neo-uptime dist web/dist
