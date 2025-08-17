# Lux Node API Reference

## Overview

The Lux node provides JSON-RPC APIs for interacting with the network. All APIs are available via HTTP POST requests to the node's API endpoint (default: `http://localhost:9650`).

## Table of Contents

- [Info API](#info-api)
- [Platform API (P-Chain)](#platform-api-p-chain)
- [Exchange API (X-Chain)](#exchange-api-x-chain)
- [Contract API (C-Chain)](#contract-api-c-chain)
- [Admin API](#admin-api)
- [Health API](#health-api)
- [Metrics API](#metrics-api)

## Info API

**Endpoint**: `/ext/info`

### info.getNodeID

Get the node's ID.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "info.getNodeID",
    "params": {},
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "nodeID": "NodeID-5mb46qkSBj81k9g9e4VFjGGSbaaSLFRzD"
    },
    "id": 1
}
```

### info.getNetworkID

Get the network ID.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "info.getNetworkID",
    "params": {},
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "networkID": "96369"
    },
    "id": 1
}
```

### info.getBlockchainID

Get a blockchain's ID by its alias.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "info.getBlockchainID",
    "params": {
        "alias": "C"
    },
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "blockchainID": "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5"
    },
    "id": 1
}
```

### info.peers

Get information about peers.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "info.peers",
    "params": {},
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "numPeers": 3,
        "peers": [
            {
                "ip": "192.168.1.1:9651",
                "nodeID": "NodeID-8PYXX47kqLDe2wD4oPbvRRchcnSzMA4J4",
                "version": "lux/1.0.0"
            }
        ]
    },
    "id": 1
}
```

## Platform API (P-Chain)

**Endpoint**: `/ext/P` or `/ext/bc/P`

### platform.getCurrentValidators

Get the current validators for a subnet.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "platform.getCurrentValidators",
    "params": {
        "subnetID": null
    },
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "validators": [
            {
                "nodeID": "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg",
                "startTime": "1600000000",
                "endTime": "1700000000",
                "stakeAmount": "2000000000000",
                "weight": "2000000000000"
            }
        ]
    },
    "id": 1
}
```

### platform.getHeight

Get the current P-Chain height.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "platform.getHeight",
    "params": {},
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "height": "12345"
    },
    "id": 1
}
```

### platform.getBalance

Get an address's balance.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "platform.getBalance",
    "params": {
        "addresses": ["P-lux1g65uqn6t77p656w64023nh8nd9updzmxwd59gh"]
    },
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "balance": "100000000000",
        "utxoIDs": [
            {
                "txID": "2QYG5yR6YW55ixmBvR4zXLCZKV9we9bmSWHHiGppF4Ko17bTPn",
                "outputIndex": 0
            }
        ]
    },
    "id": 1
}
```

### platform.createSubnet

Create a new subnet.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "platform.createSubnet",
    "params": {
        "controlKeys": ["P-lux1g65uqn6t77p656w64023nh8nd9updzmxwd59gh"],
        "threshold": 1
    },
    "id": 1
}
```

## Exchange API (X-Chain)

**Endpoint**: `/ext/X` or `/ext/bc/X`

### xchain.getAssetDescription

Get information about an asset.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "xchain.getAssetDescription",
    "params": {
        "assetID": "LUX"
    },
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "assetID": "2KWLkRYFjJqPBiXH5ozbPxLF7QYKZ5GB8bDp3f32kBqzfYtLLB",
        "name": "Lux",
        "symbol": "LUX",
        "denomination": 9
    },
    "id": 1
}
```

### xchain.getBalance

Get an address's balance of an asset.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "xchain.getBalance",
    "params": {
        "address": "X-lux1g65uqn6t77p656w64023nh8nd9updzmxwd59gh",
        "assetID": "LUX"
    },
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "balance": "50000000000"
    },
    "id": 1
}
```

## Contract API (C-Chain)

**Endpoint**: `/ext/bc/C/rpc`

The C-Chain implements the Ethereum JSON-RPC API. Here are common methods:

### eth_chainId

Get the chain ID.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "eth_chainId",
    "params": [],
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": "0x17911",
    "id": 1
}
```

### eth_blockNumber

Get the latest block number.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "eth_blockNumber",
    "params": [],
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": "0x3039",
    "id": 1
}
```

### eth_getBalance

Get the balance of an address.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "eth_getBalance",
    "params": [
        "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC",
        "latest"
    ],
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": "0x56bc75e2d63100000",
    "id": 1
}
```

### eth_sendRawTransaction

Send a signed transaction.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "eth_sendRawTransaction",
    "params": ["0xf86c..."],
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": "0xe670ec64341771606e55d6b4ca35a1a6b75ee3d5145a99d05921026d1527331",
    "id": 1
}
```

### eth_call

Execute a call without creating a transaction.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "eth_call",
    "params": [
        {
            "to": "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC",
            "data": "0x70a08231000000000000000000000000000000000000000000000000000000000000dead"
        },
        "latest"
    ],
    "id": 1
}
```

## Admin API

**Endpoint**: `/ext/admin`

⚠️ **Warning**: Admin API should be disabled in production environments.

### admin.getNodeVersion

Get the node's version.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "admin.getNodeVersion",
    "params": {},
    "id": 1
}
```

### admin.lockProfile

Generate a CPU/memory profile.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "admin.lockProfile",
    "params": {},
    "id": 1
}
```

## Health API

**Endpoint**: `/ext/health`

### health.health

Get the node's health status.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "health.health",
    "params": {},
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "result": {
        "checks": {
            "network.validators.heartbeat": {
                "timestamp": "2024-01-01T12:00:00Z",
                "duration": 1000000,
                "contiguousFailures": 0,
                "timeOfFirstFailure": null
            }
        },
        "healthy": true
    },
    "id": 1
}
```

## Metrics API

**Endpoint**: `/ext/metrics`

Returns Prometheus-formatted metrics.

**Request:**
```
GET /ext/metrics
```

**Response:**
```
# HELP lux_network_peers Number of connected peers
# TYPE lux_network_peers gauge
lux_network_peers 5
# HELP lux_chain_height Current chain height
# TYPE lux_chain_height gauge
lux_chain_height{chain="P"} 12345
lux_chain_height{chain="X"} 23456
lux_chain_height{chain="C"} 34567
```

## WebSocket Support

The node supports WebSocket connections for real-time updates:

```javascript
const ws = new WebSocket('ws://localhost:9650/ext/bc/C/ws');

ws.on('open', () => {
    ws.send(JSON.stringify({
        jsonrpc: '2.0',
        method: 'eth_subscribe',
        params: ['newHeads'],
        id: 1
    }));
});

ws.on('message', (data) => {
    console.log('New block:', JSON.parse(data));
});
```

## Error Codes

Standard JSON-RPC error codes:

| Code | Message | Description |
|------|---------|-------------|
| -32700 | Parse error | Invalid JSON |
| -32600 | Invalid request | Invalid method |
| -32601 | Method not found | Method does not exist |
| -32602 | Invalid params | Invalid parameters |
| -32603 | Internal error | Internal JSON-RPC error |
| -32000 | Server error | Generic server error |

## Rate Limiting

Default rate limits:
- 1000 requests per minute per IP
- 100 concurrent connections per IP
- 10MB maximum request size

## Authentication

For authenticated endpoints, use HTTP Basic Authentication:

```bash
curl -u username:password \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"admin.lockProfile","params":{},"id":1}' \
  http://localhost:9650/ext/admin
```

## CORS Configuration

To enable CORS for browser-based applications:

```json
{
    "api-allowed-origins": "*",
    "api-cors-allowed-headers": "*"
}
```

## Best Practices

1. **Use batch requests** for multiple calls:
```json
[
    {"jsonrpc": "2.0", "method": "eth_blockNumber", "params": [], "id": 1},
    {"jsonrpc": "2.0", "method": "eth_chainId", "params": [], "id": 2}
]
```

2. **Enable compression** for large responses:
```bash
curl -H "Accept-Encoding: gzip" http://localhost:9650/ext/bc/C/rpc
```

3. **Use WebSockets** for real-time data instead of polling

4. **Implement retry logic** with exponential backoff

5. **Cache responses** when appropriate

## SDK Support

Official SDKs available:
- [JavaScript/TypeScript](https://github.com/luxfi/js)
- [Go](https://github.com/luxfi/go)
- [Python](https://github.com/luxfi/python)

## Additional Resources

- [API Playground](https://api.lux.network)
- [Postman Collection](https://github.com/luxfi/postman)
- [OpenAPI Specification](https://github.com/luxfi/openapi)