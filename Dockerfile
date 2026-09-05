# syntax=docker/dockerfile:1
#
# Multi-arch build for the Go API + built React UI, served as a single
# binary. Build with `make docker-build` / push with `make docker-push`
# (see the Makefile) rather than invoking `docker build` directly, since
# a real multi-platform result requires buildx with a docker-container
# builder — a plain `docker build` will silently only build for your
# host's own platform.

# ---- Build the frontend -------------------------------------------------
# The UI build's output is the same regardless of the final image's
# target platform, so this stage is pinned to the build machine's own
# platform (BUILDPLATFORM) rather than running once per --platform
# target under QEMU emulation for no benefit.
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci
COPY frontend/ ./
RUN npm run build

# ---- Cross-compile the Go server ---------------------------------------
# Also pinned to BUILDPLATFORM: Go's own cross-compiler produces the
# target-arch binary directly (CGO_ENABLED=0, no cgo anywhere in this
# module — modernc.org/sqlite is a pure-Go driver), so running the Go
# toolchain itself under emulation per target platform would only make
# the build slower, not more correct.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /app/server ./cmd/server

# An empty, writable data directory for the SQLite database (see
# internal/config.Config.DBPath). Created here, where a shell and root
# exist, and copied into the shell-less runtime image below with the
# right ownership for its non-root user — the runtime image can't mkdir
# this itself.
RUN mkdir -p /data

# ---- Runtime image ------------------------------------------------------
# distroless "static" (not "scratch"): this app makes outbound HTTPS
# calls to Twitch/Kick/StreamElements, so it needs the CA certificate
# bundle distroless ships; "nonroot" runs the whole container as an
# unprivileged user by default.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

LABEL org.opencontainers.image.source="https://github.com/danotchuuy/multi-stream-subathon" \
      org.opencontainers.image.description="Multi-platform subathon clock: tracks Twitch/Kick subs, bits/Kicks, and StreamElements donations across multiple timers, each with its own dashboard and OBS overlay."

COPY --from=go-builder --chown=nonroot:nonroot /app /app
COPY --from=frontend-builder --chown=nonroot:nonroot /src/frontend/dist /app/frontend/dist
COPY --from=go-builder --chown=nonroot:nonroot /data /data

WORKDIR /app
USER nonroot:nonroot

# Mount a volume at /data so timer state and contributor history survive
# container restarts/upgrades; without one, the app still runs, it just
# starts from an empty database every time the container is recreated.
ENV ADDR=:8080 \
    UI_DIST_DIR=/app/frontend/dist \
    DB_PATH=/data/subathon.db

EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/app/server"]
