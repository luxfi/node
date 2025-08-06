# Runtime stage - using pre-built binary
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -u 1000 -s /bin/bash luxd

# Create necessary directories
RUN mkdir -p /app/plugins /data/db /app/configs /logs /keys/staking /blockchain-data && \
    chown -R luxd:luxd /app /data /logs /keys /blockchain-data

WORKDIR /app

# Copy the pre-built binary from the host
COPY build/luxd /app/luxd

# Make sure the binary is executable
RUN chmod +x /app/luxd

# Copy entrypoint script
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Switch to non-root user
USER luxd

# Expose ports
EXPOSE 9630 9631

# Environment variables for imported blockchain
ENV NETWORK_ID=96369
ENV HTTP_HOST=0.0.0.0
ENV HTTP_PORT=9630
ENV STAKING_PORT=9631
ENV LOG_LEVEL=info

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:9630/ext/health || exit 1

# Default command
ENTRYPOINT ["/app/docker-entrypoint.sh"]