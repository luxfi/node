# Build stage
FROM golang:1.21.12-bookworm AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build luxd with the consensus modification
RUN go build -o luxd ./luxd

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -u 1000 -s /bin/bash luxd

# Create necessary directories
RUN mkdir -p /app/plugins /data/db /app/configs && \
    chown -R luxd:luxd /app /data

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/luxd /app/luxd

# Copy plugins (geth) if available
COPY --from=builder /build/plugins/* /app/plugins/ 2>/dev/null || true

# Switch to non-root user
USER luxd

# Expose ports
EXPOSE 9630 9631

# Environment variables for imported blockchain
ENV LUX_IMPORTED_BLOCK_ID=""
ENV LUX_IMPORTED_HEIGHT=""
ENV LUX_IMPORTED_TIMESTAMP=""

# Default command
ENTRYPOINT ["/app/luxd"]
CMD ["--http-host=0.0.0.0", "--http-port=9630"]