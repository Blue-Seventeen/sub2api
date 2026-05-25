# =============================================================================
# Sub2API Multi-Stage Dockerfile
# =============================================================================
# Stage 1: Build frontend
# Stage 2: Build Go backend with embedded frontend
# Stage 3: Final minimal image
# =============================================================================

ARG NODE_IMAGE=node:24-alpine
ARG GOLANG_IMAGE=golang:1.26.3-alpine
ARG ALPINE_IMAGE=alpine:3.20
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn
ARG MIHOMO_VERSION=1.19.24
ARG MIHOMO_AMD64_SHA256=7c8678fc20a1d3af9bfb87ccee428112b74398f7e04b6ddb044f1e7b94539870
ARG MIHOMO_ARM64_SHA256=6d4a278e7ace1016f23d7245f6f8911f6f7a4adfba2095704473d937e1911840

# -----------------------------------------------------------------------------
# Stage 1: Frontend Builder
# -----------------------------------------------------------------------------
FROM ${NODE_IMAGE} AS frontend-builder

WORKDIR /app/frontend

# Install pnpm
RUN corepack enable && corepack prepare pnpm@latest --activate

# Install dependencies first (better caching)
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# Copy frontend source and build
COPY frontend/ ./
RUN pnpm run build

# -----------------------------------------------------------------------------
# Stage 2: Backend Builder
# -----------------------------------------------------------------------------
FROM ${GOLANG_IMAGE} AS backend-builder

# Build arguments for version info (set by CI)
ARG VERSION=docker
ARG COMMIT=docker
ARG DATE
ARG GOPROXY
ARG GOSUMDB

ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app/backend

# Copy go mod files first (better caching)
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy backend source first
COPY backend/ ./

# Copy frontend dist from previous stage (must be after backend copy to avoid being overwritten)
COPY --from=frontend-builder /app/backend/internal/web/dist ./internal/web/dist

# Build the binary (BuildType=release for CI builds, embed frontend)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -tags embed \
    -ldflags="-s -w -X main.Commit=${COMMIT} -X main.Date=${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)} -X main.BuildType=release" \
    -o /app/sub2api \
    ./cmd/server

# -----------------------------------------------------------------------------
# Stage 3: mihomo managed proxy core
# -----------------------------------------------------------------------------
FROM ${ALPINE_IMAGE} AS mihomo-core

ARG TARGETARCH
ARG MIHOMO_VERSION
ARG MIHOMO_AMD64_SHA256
ARG MIHOMO_ARM64_SHA256

RUN apk add --no-cache ca-certificates wget gzip && \
    case "${TARGETARCH}" in \
      amd64) MIHOMO_ASSET="mihomo-linux-amd64-compatible-v${MIHOMO_VERSION}.gz"; MIHOMO_SHA256="${MIHOMO_AMD64_SHA256}" ;; \
      arm64) MIHOMO_ASSET="mihomo-linux-arm64-v${MIHOMO_VERSION}.gz"; MIHOMO_SHA256="${MIHOMO_ARM64_SHA256}" ;; \
      *) echo "unsupported TARGETARCH for bundled mihomo: ${TARGETARCH}" >&2; exit 1 ;; \
    esac && \
    wget -O /tmp/mihomo.gz "https://github.com/MetaCubeX/mihomo/releases/download/v${MIHOMO_VERSION}/${MIHOMO_ASSET}" && \
    echo "${MIHOMO_SHA256}  /tmp/mihomo.gz" | sha256sum -c - && \
    mkdir -p /app/bin && \
    gzip -dc /tmp/mihomo.gz > /app/bin/mihomo && \
    chmod 755 /app/bin/mihomo && \
    /app/bin/mihomo -v

# -----------------------------------------------------------------------------
# Stage 4: Final Runtime Image
# -----------------------------------------------------------------------------
FROM ${ALPINE_IMAGE}

# Labels
LABEL maintainer="Wei-Shaw <github.com/Wei-Shaw>"
LABEL description="Sub2API - AI API Gateway Platform"
LABEL org.opencontainers.image.source="https://github.com/Wei-Shaw/sub2api"

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    su-exec \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1000 sub2api && \
    adduser -u 1000 -G sub2api -s /bin/sh -D sub2api

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=backend-builder /app/sub2api /app/sub2api
COPY --from=mihomo-core /app/bin/mihomo /app/bin/mihomo

# Create data directory
RUN mkdir -p /app/data && chown -R sub2api:sub2api /app

# Copy entrypoint script (fixes volume permissions then drops to sub2api)
COPY deploy/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Expose port (can be overridden by SERVER_PORT env var)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget -q -T 5 -O /dev/null http://localhost:${SERVER_PORT:-8080}/health || exit 1

# Run the application (entrypoint fixes /app/data ownership then execs as sub2api)
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/sub2api"]
