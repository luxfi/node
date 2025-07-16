# Multi-stage build for Lux node
FROM golang:1.21.12-alpine AS builder

# Install build dependencies
RUN apk add --no-cache make gcc musl-dev linux-headers git

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the node binary
RUN ./scripts/build.sh

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates bash curl jq

# Create user
RUN addgroup -g 1000 luxnode && \
    adduser -u 1000 -G luxnode -s /bin/bash -D luxnode

# Set up directories
RUN mkdir -p /luxd/configs/chains/C /luxd/staking /luxd/db && \
    chown -R luxnode:luxnode /luxd

# Copy binary from builder
COPY --from=builder /build/build/node /usr/local/bin/luxd

# Copy startup script
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Switch to non-root user
USER luxnode
WORKDIR /luxd

# Expose ports
EXPOSE 9650 9651 8546

# Set entrypoint
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["luxd"]