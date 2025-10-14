# Standard MCP Server Dockerfile
# This follows typical MCP server patterns for containerized distribution
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application for STDIO mode (default MCP pattern)
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o last9-mcp-server .

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
# Set LAST9_HTTP=true to enable HTTP mode
ENV LAST9_HTTP=false

# MCP servers typically don't expose ports in STDIO mode
# EXPOSE directive omitted intentionally

# Run the MCP server in STDIO mode
ENTRYPOINT ["./last9-mcp-server"]