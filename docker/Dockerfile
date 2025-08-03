FROM ubuntu:22.04

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    jq \
    openssl \
    && rm -rf /var/lib/apt/lists/*

# Create user
RUN groupadd -g 1000 luxnode && \
    useradd -u 1000 -g luxnode -s /bin/bash -m luxnode

# Set up directories
RUN mkdir -p /luxd && \
    chown -R luxnode:luxnode /luxd

# Copy pre-built binary
COPY build/node /usr/local/bin/luxd
RUN chmod +x /usr/local/bin/luxd

# Copy startup script
COPY docker/docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Switch to non-root user
USER luxnode
WORKDIR /luxd

# Expose ports
EXPOSE 9630 9631 8546

# Set entrypoint
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["luxd"]