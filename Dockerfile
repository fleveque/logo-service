# Multi-stage Dockerfile for logo-service.
# Stage 1: Build the Go binary with CGO (needed for SQLite and libvips).
# Stage 2: Minimal runtime image with only the binary and shared libraries.
#
# Go note: unlike interpreted languages, Go compiles to a single binary.
# The runtime image doesn't need Go installed — just the binary + C libraries.

# ---------------------------------------------------------------------------
# Stage 1: Build
# ---------------------------------------------------------------------------
FROM golang:1.24-alpine AS builder

# CGO is required for go-sqlite3 (C library) and bimg (libvips C bindings).
# Alpine uses musl libc, so we need the C compiler and libvips dev headers.
RUN apk add --no-cache gcc musl-dev vips-dev

WORKDIR /build

# Copy go.mod and go.sum first — Docker caches this layer.
# Dependencies only re-download when these files change, not on every code change.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build both binaries.
# CGO_ENABLED=1 is required for go-sqlite3 and bimg.
# -ldflags="-s -w" strips debug info, reducing binary size by ~30%.
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /bin/logo-service ./cmd/server
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /bin/logo-cli ./cmd/cli

# ---------------------------------------------------------------------------
# Stage 2: Runtime
# ---------------------------------------------------------------------------
FROM alpine:3.21

# Install only the runtime libraries (not dev headers).
# vips: image processing runtime (libvips)
# ca-certificates: for HTTPS calls to GitHub API and LLM providers
# tzdata: timezone data for proper timestamps
RUN apk add --no-cache vips ca-certificates tzdata

# Run as non-root user for security — same pattern as the dividend-portfolio Dockerfile.
RUN addgroup -S app && adduser -S -G app app

# Create storage directories owned by the app user
RUN mkdir -p /app/storage/logos && chown -R app:app /app

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /bin/logo-service /bin/logo-service
COPY --from=builder /bin/logo-cli /bin/logo-cli

# Copy example config (will be overridden by env vars or mounted config)
COPY config.example.yaml /app/config.example.yaml

USER app

EXPOSE 8080

# Health check — hits the unauthenticated /healthz endpoint.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/bin/logo-service"]
