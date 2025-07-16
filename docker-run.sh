#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}[*]${NC} $1"
}

print_error() {
    echo -e "${RED}[!]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[!]${NC} $1"
}

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    print_error "Docker is not running. Please start Docker first."
    exit 1
fi

# Create data directory if it doesn't exist
if [ ! -d "/home/z/lux-mainnet-data" ]; then
    print_status "Creating data directory..."
    mkdir -p /home/z/lux-mainnet-data
fi

# Ensure hanzo-network exists
if ! docker network ls | grep -q hanzo-network; then
    print_status "Creating hanzo-network..."
    docker network create hanzo-network
fi

# Build the Docker image
print_status "Building Lux node Docker image..."
cd /home/z/work/lux/node

# First, let's build the binary locally since we already have it
if [ ! -f "build/node" ]; then
    print_error "Node binary not found. Please build it first with ./scripts/build.sh"
    exit 1
fi

# Create a temporary Dockerfile that uses the pre-built binary
cat > Dockerfile.local << 'EOF'
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates bash curl jq

# Create user
RUN addgroup -g 1000 luxnode && \
    adduser -u 1000 -G luxnode -s /bin/bash -D luxnode

# Set up directories
RUN mkdir -p /luxd/configs/chains/C /luxd/staking /luxd/db && \
    chown -R luxnode:luxnode /luxd

# Copy pre-built binary
COPY build/node /usr/local/bin/luxd
RUN chmod +x /usr/local/bin/luxd

# Copy startup script
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Copy genesis file
COPY genesis/genesis_mainnet.json /genesis.json

# Switch to non-root user
USER luxnode
WORKDIR /luxd

# Expose ports
EXPOSE 9650 9651 8546

# Set entrypoint
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["luxd"]
EOF

# Build the image
docker build -f Dockerfile.local -t lux-node:latest .

# Clean up temporary Dockerfile
rm -f Dockerfile.local

# Stop existing container if running
if docker ps -a | grep -q luxnet-node; then
    print_warning "Stopping existing container..."
    docker-compose -f compose.luxnet.yml down
fi

# Start the services
print_status "Starting Lux node..."
docker-compose -f compose.luxnet.yml up -d luxd

# Wait for node to be healthy
print_status "Waiting for node to be healthy..."
for i in {1..30}; do
    if docker exec luxnet-node curl -s http://localhost:9650/ext/health | jq -e '.healthy' > /dev/null 2>&1; then
        print_status "Node is healthy!"
        break
    fi
    echo -n "."
    sleep 2
done

# Check final status
if docker exec luxnet-node curl -s http://localhost:9650/ext/health | jq -e '.healthy' > /dev/null 2>&1; then
    print_status "Lux node is running successfully!"
    
    # Show some info
    echo -e "\n${GREEN}Node Information:${NC}"
    echo "RPC Endpoint: http://localhost:9650/ext/bc/C/rpc"
    echo "WebSocket: ws://localhost:8546"
    echo "Health Check: http://localhost:9650/ext/health"
    
    # Test RPC
    echo -e "\n${GREEN}Testing RPC:${NC}"
    curl -s -X POST http://localhost:9650/ext/bc/C/rpc \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' | jq .
    
    # Show logs command
    echo -e "\n${GREEN}View logs:${NC}"
    echo "docker-compose -f compose.luxnet.yml logs -f luxd"
else
    print_error "Node failed to start properly. Check logs:"
    docker-compose -f compose.luxnet.yml logs luxd
    exit 1
fi