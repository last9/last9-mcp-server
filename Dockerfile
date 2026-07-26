# Standard MCP Server Dockerfile
# This follows typical MCP server patterns for containerized distribution
#
# Multi-arch: CI builds this once per architecture on a native runner
# (amd64 -> ubuntu-latest, arm64 -> ubuntu-24.04-arm) and merges the results
# into a manifest list — no QEMU emulation (see ENG-1074). On a native build
# BUILDPLATFORM == TARGETPLATFORM, so the builder is native and the pure-Go
# (CGO_ENABLED=0) binary is compiled for TARGETARCH without emulation. The ARGs
# also keep a plain `docker buildx build --platform` cross-build working locally.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

# Target platform, injected automatically by buildx (defaults to the build
# host's arch for a plain `docker build`).
ARG TARGETOS=linux
ARG TARGETARCH

# Build the application for STDIO mode (default MCP pattern)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -installsuffix cgo \
    -ldflags "-s -w -X main.Version=${VERSION} -X main.CommitSHA=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o last9-mcp-server .

# Final stage - minimal image for MCP server
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN adduser -D -s /bin/sh mcp

# Set working directory
WORKDIR /home/mcp/

# Copy the binary from builder
COPY --from=builder /app/last9-mcp-server .

# Change ownership to non-root user
RUN chown -R mcp:mcp /home/mcp/

# Switch to non-root user
USER mcp

# Default to STDIO mode (standard MCP pattern)
ENV LAST9_HTTP=false

# MCP servers typically don't expose ports in STDIO mode
# EXPOSE directive omitted intentionally

# Run the MCP server in STDIO mode
ENTRYPOINT ["./last9-mcp-server"]