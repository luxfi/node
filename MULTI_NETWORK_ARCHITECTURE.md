# Multi-Network Validation Architecture

## Problem Statement
Currently, luxd can only validate one network at a time. Each node instance is bound to a single NetworkID, requiring separate processes to validate multiple networks. This creates operational overhead and prevents unified RPC access across networks.

## Goal
Enable a single luxd process to validate multiple networks simultaneously, providing:
- Single RPC endpoint for all networks
- Shared P2P networking layer
- Unified database management with network isolation
- Cross-network query capabilities

## Proposed Architecture

### 1. Network Registry Pattern
Replace single NetworkID with a NetworkRegistry that manages multiple networks:

```go
// node/node.go
type Node struct {
    // OLD: NetworkID uint32
    // NEW:
    Networks *NetworkRegistry

    // Network-specific components
    chainManagers map[uint32]*chains.Manager  // Per-network chain managers
    validators    map[uint32]validators.Manager // Per-network validators
    databases     map[uint32]database.Database  // Per-network databases
}

type NetworkRegistry struct {
    mu sync.RWMutex
    networks map[uint32]*NetworkContext
    primary uint32 // Primary network ID for backwards compatibility
}

type NetworkContext struct {
    NetworkID    uint32
    ChainManager *chains.Manager
    Validators   validators.Manager
    Database     database.Database
    Bootstrappers validators.Manager
    Config       NetworkConfig
}
```

### 2. RPC Routing Enhancement
Modify RPC paths to include network context:

```
Current: /ext/bc/C/rpc
Proposed: /ext/network/{networkID}/bc/C/rpc
Fallback: /ext/bc/C/rpc (uses primary network)
```

### 3. Database Isolation
Each network gets its own database namespace:

```go
// node/node.go - initDatabase()
func (n *Node) initDatabaseForNetwork(networkID uint32) error {
    networkPath := filepath.Join(
        n.Config.DatabaseConfig.Path,
        fmt.Sprintf("network-%d", networkID),
    )

    db, err := database.New(
        n.Config.DatabaseConfig.Name,
        networkPath,
        n.Config.ReadOnly,
    )

    n.databases[networkID] = db
    return err
}
```

### 4. Chain Manager Per Network
Each network maintains its own chain manager:

```go
// chains/manager.go
type ManagerConfig struct {
    // ... existing fields ...
    NetworkID uint32 // Network this manager belongs to
}

// node/node.go - initChainManager()
func (n *Node) initChainManagerForNetwork(networkID uint32) error {
    config := n.Networks.Get(networkID)

    chainManager := chains.NewManager(chains.ManagerConfig{
        NetworkID: networkID,
        Validators: config.Validators,
        ChainDataDir: filepath.Join(n.Config.ChainDataDir, fmt.Sprintf("network-%d", networkID)),
        // ... other config ...
    })

    config.ChainManager = chainManager
    return nil
}
```

### 5. P2P Network Multiplexing
Share P2P connections but route messages by network:

```go
// network/network.go
type Network struct {
    // ... existing fields ...
    networkRouters map[uint32]*Router // Per-network message routing
}

func (n *Network) RouteMessage(msg Message) error {
    networkID := msg.GetNetworkID()
    router, ok := n.networkRouters[networkID]
    if !ok {
        return ErrUnknownNetwork
    }
    return router.Handle(msg)
}
```

### 6. Configuration
Support multiple network configurations:

```yaml
# config.yml
networks:
  - network-id: 96369    # Lux Mainnet
    primary: true
    bootstrap-ips: ["18.142.247.237:9631"]
    bootstrap-ids: ["NodeID-4kCLS16Wy73nt1Zm54jFZsL7Msrv3UCeJ"]

  - network-id: 96368    # Lux Testnet
    bootstrap-ips: ["testnet1.lux.network:9651"]
    bootstrap-ids: ["NodeID-TestnetBootstrap1"]

  - network-id: 200200   # Zoo Network
    bootstrap-ips: ["zoo1.zoo.network:2001"]
    bootstrap-ids: ["NodeID-ZooBootstrap1"]

  - network-id: 36963    # Hanzo Network
    bootstrap-ips: ["hanzo1.hanzo.network:3691"]
    bootstrap-ids: ["NodeID-HanzoBootstrap1"]
```

### 7. API Changes
Extend Info API to list active networks:

```go
// api/info/service.go
type GetNetworksReply struct {
    Networks []NetworkInfo `json:"networks"`
}

type NetworkInfo struct {
    NetworkID   uint32 `json:"networkID"`
    NetworkName string `json:"networkName"`
    IsValidating bool  `json:"isValidating"`
    NumValidators int   `json:"numValidators"`
}

func (s *Service) GetNetworks(r *http.Request, args *struct{}, reply *GetNetworksReply) error {
    for networkID, ctx := range s.node.Networks.All() {
        reply.Networks = append(reply.Networks, NetworkInfo{
            NetworkID: networkID,
            NetworkName: GetNetworkName(networkID),
            IsValidating: ctx.Validators.Contains(s.node.ID),
            NumValidators: ctx.Validators.Len(),
        })
    }
    return nil
}
```

### 8. Cross-Network Queries
Enable querying across networks from single RPC:

```go
// Cross-network balance query
curl -X POST http://localhost:9630/ext/crossnet \
  -d '{
    "method": "crossnet.getBalances",
    "params": {
      "address": "0x...",
      "networks": [96369, 96368, 200200, 36963]
    }
  }'

// Response
{
  "balances": {
    "96369": "1000000000000000000",  // Lux Mainnet
    "96368": "500000000000000000",   // Lux Testnet
    "200200": "2000000000000000000",  // Zoo
    "36963": "750000000000000000"    // Hanzo
  }
}
```

## Implementation Steps

### Phase 1: Core Refactoring
1. Create NetworkRegistry and NetworkContext structures
2. Refactor Node to support multiple networks
3. Update database initialization for network isolation
4. Modify chain manager to be network-aware

### Phase 2: RPC Enhancement
1. Add network routing to RPC paths
2. Implement fallback for backwards compatibility
3. Add cross-network query APIs
4. Update Info API with network listing

### Phase 3: P2P Multiplexing
1. Implement message routing by network ID
2. Share connections across networks
3. Handle network-specific handshakes
4. Optimize bandwidth usage

### Phase 4: Configuration & CLI
1. Support multi-network configuration file
2. Add CLI flags for network management
3. Implement network add/remove at runtime
4. Add network status monitoring

## Benefits
- **Operational Simplicity**: Single process to manage
- **Resource Efficiency**: Shared memory and connections
- **Unified Access**: Single RPC endpoint for all networks
- **Cross-Network Features**: Query and compare across networks
- **Dynamic Management**: Add/remove networks without restart

## Challenges
- **Code Complexity**: Significant refactoring required
- **State Isolation**: Ensure no cross-network contamination
- **Performance**: Managing multiple consensus instances
- **Backwards Compatibility**: Existing single-network deployments

## Alternative Approaches

### 1. Process Supervisor (Current Workaround)
- Use systemd/supervisor to manage multiple luxd instances
- Implement RPC proxy to unify endpoints
- **Pros**: No code changes needed
- **Cons**: Higher resource usage, complex operations

### 2. Microservices Architecture
- Split luxd into network-specific microservices
- Use message queue for inter-service communication
- **Pros**: Better isolation, horizontal scaling
- **Cons**: Operational complexity, latency overhead

### 3. Container Orchestration
- Run each network in separate container
- Use Kubernetes for orchestration
- **Pros**: Industry standard, good tooling
- **Cons**: Infrastructure overhead, not suitable for edge nodes

## Recommendation
Implement the Network Registry pattern in phases, starting with database and chain manager isolation. This provides the foundation for true multi-network validation while maintaining backwards compatibility.