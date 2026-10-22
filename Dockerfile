# syntax=docker/dockerfile:1

# Build natively on the builder's platform and cross-compile, so multi-arch
# images don't need emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=""
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/wutlu/boltcache/appinfo.Version=${VERSION} -X github.com/wutlu/boltcache/appinfo.Commit=${COMMIT}" \
      -o /out/boltcache . \
 && mkdir -p /out/data

# Minimal, shell-less runtime running as an unprivileged user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/boltcache /usr/local/bin/boltcache
COPY deploy/docker/config.yaml /etc/boltcache/config.yaml
COPY --from=build --chown=65532:65532 /out/data /data
ENV BOLTCACHE_CONFIG=/etc/boltcache/config.yaml
VOLUME ["/data"]
# 6380: TCP (text + RESP), 6382: RESP (Redis clients), 8090: REST/WebSocket/metrics
EXPOSE 6380 6382 8090
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/boltcache"]
CMD ["server"]
