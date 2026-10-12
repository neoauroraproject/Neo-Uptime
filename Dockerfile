# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Go cross-compiles: building under emulation takes hours with Xray-core.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG VERSION=dev
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/neoauroraproject/Neo-Uptime/internal/version.Version=${VERSION}" \
    -o /out/neo-uptime ./cmd/neo-uptime

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata iputils
COPY --from=build /out/neo-uptime /usr/local/bin/neo-uptime
ENV NEO_UPTIME_DATA=/data NEO_UPTIME_HOST=0.0.0.0 NEO_UPTIME_PORT=7788
VOLUME /data
EXPOSE 7788
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:7788/api/health || exit 1
ENTRYPOINT ["neo-uptime"]
CMD ["serve"]
