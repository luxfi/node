# Lux Node Docker Documentation

## Quick Start

```bash
# Pull the latest image
docker pull ghcr.io/luxfi/node:latest

# Run a single node
docker run -d \
  --name luxd \
  -p 9630:9630 \
  -p 9631:9631 \
  -v luxd-data:/data \
  -e NETWORK_ID=96369 \
  ghcr.io/luxfi/node:latest
```

## Building the Image

```bash
# Build locally
docker build -t luxfi/node:local .

# Build and push to GitHub Container Registry
docker build -t ghcr.io/luxfi/node:latest .
docker push ghcr.io/luxfi/node:latest
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NETWORK_ID` | Network ID | `96369` |
| `HTTP_HOST` | HTTP API host | `0.0.0.0` |
| `HTTP_PORT` | HTTP API port | `9630` |
| `STAKING_PORT` | Staking port | `9631` |
| `LOG_LEVEL` | Log level | `info` |
| `BOOTSTRAP_IPS` | Bootstrap node IPs | - |
| `BOOTSTRAP_IDS` | Bootstrap node IDs | - |
| `STAKING_KEY_KMS_ID` | AWS KMS key ID for staking | - |

## Volume Mounts

- `/data` - Persistent data directory
- `/logs` - Log files
- `/keys/staking` - Staking certificates (for validators)
- `/blockchain-data` - Pre-loaded blockchain data (optional)

## Docker Compose

### Basic Setup
```bash
docker-compose up -d
```

### With Monitoring
```bash
docker-compose --profile monitoring up -d
```

## Kubernetes Deployment

```bash
# Create namespace and deploy
kubectl apply -f k8s/luxd-statefulset.yaml

# Scale replicas
kubectl scale statefulset luxd -n lux-network --replicas=5

# Check status
kubectl get pods -n lux-network
```

## Docker Swarm

```bash
# Initialize swarm (if not already)
docker swarm init

# Create secrets
echo "your-staking-key" | docker secret create staking_key -
echo "your-staking-cert" | docker secret create staking_cert -

# Deploy stack
docker stack deploy -c docker-stack.yml lux

# Check services
docker service ls
docker service ps lux_luxd
```

## Security Best Practices

### 1. Staking Keys Management

**Never** store staking keys in environment variables for production. Use one of:

- **AWS KMS**: Set `STAKING_KEY_KMS_ID`
- **Docker Secrets**: Mount via secrets (Swarm mode)
- **Kubernetes Secrets**: Use K8s secrets with proper RBAC
- **HashiCorp Vault**: Integrate with Vault for key management

### 2. Network Security

```yaml
# Restrict network access
networks:
  lux-network:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.0.0/16
    internal: true  # No external access
```

### 3. Resource Limits

Always set resource limits to prevent resource exhaustion:

```yaml
deploy:
  resources:
    limits:
      cpus: '4'
      memory: 8G
    reservations:
      cpus: '2'
      memory: 4G
```

## Monitoring

### Prometheus Metrics

The node exposes metrics at `http://localhost:9630/ext/metrics`

### Health Checks

- Liveness: `http://localhost:9630/ext/health`
- Readiness: `http://localhost:9630/ext/info`

### Grafana Dashboard

Import dashboard from `monitoring/grafana/dashboards/luxd.json`

## Loading Blockchain Data

To use pre-loaded blockchain data (1,082,781 blocks):

```bash
docker run -d \
  --name luxd \
  -p 9630:9630 \
  -v luxd-data:/data \
  -v ./state/cchain:/blockchain-data:ro \
  -e NETWORK_ID=96369 \
  ghcr.io/luxfi/node:latest
```

The entrypoint script will automatically detect and load the blockchain data on first run.

## Troubleshooting

### Check Logs
```bash
docker logs luxd
docker exec luxd tail -f /logs/luxd.log
```

### Access Shell
```bash
docker exec -it luxd /bin/bash
```

### Test RPC
```bash
# Check if bootstrapped
curl -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
  http://localhost:9630/ext/info

# Get block number
curl -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  http://localhost:9630/ext/bc/C/rpc
```

## CI/CD

The GitHub Actions workflow automatically:
1. Builds the Docker image on push to main
2. Tags with version, branch, and SHA
3. Pushes to GitHub Container Registry
4. Generates SBOM for security scanning
5. Supports multi-platform builds (amd64, arm64)

## License

See LICENSE file in the repository root.