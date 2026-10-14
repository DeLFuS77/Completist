# Completist: one image for amd64 and arm64, built from source.
#   docker build -t completist .
#   docker buildx build --platform linux/amd64,linux/arm64 -t completist .

# ---- the interface, built once on the machine that builds ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui
WORKDIR /src/ui
COPY ui/package.json ui/package-lock.json* ./
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi
COPY ui/ ./
RUN mkdir -p ../internal/webui && node build.mjs

# ---- the server, cross-compiled for the target platform ----
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS server
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/DeLFuS77/completist/internal/server.Version=${VERSION}" \
    -o /out/completist ./cmd/completist

# ---- the image ----
FROM alpine:3
RUN apk add --no-cache tzdata su-exec

ENV COMPLETIST_DATA_DIR=/data \
    COMPLETIST_IN_DOCKER=1 \
    PUID=1000 \
    PGID=1000

RUN mkdir -p /data /roms
COPY --from=server /out/completist /usr/local/bin/completist
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
# the license and where the source code is (GNU AGPL-3.0)
COPY LICENSE NOTICE.md /usr/share/doc/completist/
RUN chmod 755 /usr/local/bin/completist /usr/local/bin/docker-entrypoint.sh

LABEL org.opencontainers.image.title="Completist" \
      org.opencontainers.image.description="The ROM library manager that tells you what's missing" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/DeLFuS77/completist"

VOLUME /data
VOLUME /roms
EXPOSE 3000

# /healthz answers without login
HEALTHCHECK --interval=30s --timeout=5s --start-period=1m --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:3000/healthz || exit 1

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["completist"]
