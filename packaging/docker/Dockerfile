# Zephyr HiFi Kit — NAS / container server image (audio-capable).
# Multi-stage: CGO libFLAC server build, then a slim runtime with gnupg for
# enforce-mode signature verification. No systemd/apt assumptions: the binary
# runs directly, all state lives in the /data volume.
# syntax=docker/dockerfile:1
FROM golang:1.27-bookworm AS build
ARG TARGETARCH
ARG VERSION=0.4.3
RUN apt-get update \
 && apt-get install -y --no-install-recommends pkg-config libflac-dev \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
COPY zat/go.mod ./zat/
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-X hifihub/server/internal/metrics.Version=${VERSION}" -o /out/hifihub-server ./cmd/hifihub-server

FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends gnupg libflac12 curl ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid 1000 hifihub-server \
 && useradd --uid 1000 --gid 1000 --home-dir /data --shell /usr/sbin/nologin hifihub-server \
 && mkdir -p /data/dist /data/gnupg /mnt/zephyr \
 && chown -R hifihub-server:hifihub-server /data /mnt/zephyr
COPY --from=build /out/hifihub-server /usr/bin/hifihub-server
# Self-upgrade replaces the binary and restarts a systemd unit; containers have
# neither, so image refresh is the upgrade path (fleet convergence stays on).
ENV HIFIHUB_LISTEN=0.0.0.0:8080 \
    HIFIHUB_DATA_DIR=/data \
    HIFIHUB_DIST_DIR=/data/dist \
    HIFIHUB_GPG_HOME=/data/gnupg \
    HIFIHUB_SIGNATURE_MODE=enforce \
    HIFIHUB_PLAY_GATE=true \
    HIFIHUB_UPDATE_SELF=false \
    HIFIHUB_UPDATE_AUTO=false \
    HIFIHUB_UPDATE_FLEET=true \
    HIFIHUB_MOUNT_DIR=/mnt/zephyr
VOLUME /data
EXPOSE 8080 7800
USER hifihub-server
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/api/v1/health || exit 1
ENTRYPOINT ["/usr/bin/hifihub-server"]