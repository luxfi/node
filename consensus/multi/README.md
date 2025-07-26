# Multi-Consensus Implementation Guide

## Overview

This package implements the Multi-Consensus architecture for Lux v2.0.0, enabling parallel validation of multiple L1 blockchains.

## Package Structure

```
multiconsensus/
├── manager.go          # Core consensus manager
├── module.go           # Consensus module interface
├── router.go           # Network routing logic
├── modules/
│   ├── lux/            # Lux v2 consensus module
│   └── avalanche/      # Avalanche compatibility module
├── config/
│   └── config.go       # Configuration structures
└── tests/
    └── integration/    # Integration tests
```

## Core Interfaces

### ConsensusModule

```go
package multiconsensus

import (
    "context"
    "github.com/luxfi/node/ids"
    "github.com/luxfi/node/consensus/engine/common"
)

// ConsensusModule represents a single blockchain consensus implementation
type ConsensusModule interface {
    // Initialize prepares the module with configuration
    Initialize(config ModuleConfig) error

    // Start begins consensus participation
    Start(ctx context.Context) error

    // Stop gracefully shuts down the module
    Stop() error

    // GetChainID returns the blockchain identifier
    GetChainID() ids.ID

    // GetNetworkID returns the network identifier
    GetNetworkID() uint32

    // GetEngine returns the consensus engine
    GetEngine() common.Engine

    // Health returns the module's health status
    Health() (interface{}, error)

    // Bootstrapped returns whether the module has finished bootstrapping
    Bootstrapped() bool
}
```

### ModuleConfig

```go
type ModuleConfig struct {
    Name            string                 `json:"name"`
    NetworkID       uint32                 `json:"network-id"`
    Version         string                 `json:"version"`
    DatabasePath    string                 `json:"database-path"`
    DatabaseType    string                 `json:"database-type"`
    APIPort         uint16                 `json:"api-port"`
    StakingPort     uint16                 `json:"staking-port"`
    ChainConfigs    map[string]string      `json:"chain-configs"`
    StakingKey      string                 `json:"staking-key"`
    StakingCert     string                 `json:"staking-cert"`
    EnabledAPIs     []string               `json:"enabled-apis"`
    LogLevel        string                 `json:"log-level"`
    ResourceLimits  ResourceLimits         `json:"resource-limits"`
}

type ResourceLimits struct {
    MaxCPUPercent    int    `json:"max-cpu-percent"`
    MaxMemoryMB      int    `json:"max-memory-mb"`
    MaxDiskIOPS      int    `json:"max-disk-iops"`
    MaxNetworkMbps   int    `json:"max-network-mbps"`
}
```

## Implementation Examples

### Avalanche Module

```go
package avalanche

import (
    "context"
    "github.com/luxfi/node/multiconsensus"
    avalanchenode "github.com/luxfi/node@v1.13.x/node"
)

type AvalancheModule struct {
    config   multiconsensus.ModuleConfig
    node     *avalanchenode.Node
    ctx      context.Context
    cancel   context.CancelFunc
}

func NewAvalancheModule() multiconsensus.ConsensusModule {
    return &AvalancheModule{}
}

func (m *AvalancheModule) Initialize(config multiconsensus.ModuleConfig) error {
    m.config = config

    // Create Avalanche node configuration
    nodeConfig := avalanchenode.Config{
        NetworkID:    config.NetworkID,
        DBPath:       config.DatabasePath,
        DBType:       config.DatabaseType,
        HTTPPort:     config.APIPort,
        StakingPort:  config.StakingPort,
        LogLevel:     config.LogLevel,
    }

    // Initialize Avalanche node
    node, err := avalanchenode.New(&nodeConfig)
    if err != nil {
        return err
    }

    m.node = node
    m.ctx, m.cancel = context.WithCancel(context.Background())

    return nil
}

func (m *AvalancheModule) Start(ctx context.Context) error {
    return m.node.Start()
}
```

### Consensus Manager

```go
package multiconsensus

import (
    "context"
    "sync"
    "github.com/luxfi/node/ids"
)

type Manager struct {
    modules   map[string]ConsensusModule
    router    *NetworkRouter
    config    *Config
    ctx       context.Context
    cancel    context.CancelFunc
    wg        sync.WaitGroup
    mu        sync.RWMutex
}

func NewManager(config *Config) (*Manager, error) {
    ctx, cancel := context.WithCancel(context.Background())

    return &Manager{
        modules: make(map[string]ConsensusModule),
        config:  config,
        ctx:     ctx,
        cancel:  cancel,
    }, nil
}

func (m *Manager) RegisterModule(name string, module ConsensusModule) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if _, exists := m.modules[name]; exists {
        return ErrModuleAlreadyRegistered
    }

    m.modules[name] = module
    return nil
}

func (m *Manager) Start() error {
    m.mu.RLock()
    defer m.mu.RUnlock()

    // Start each module in a separate goroutine
    for name, module := range m.modules {
        m.wg.Add(1)
        go func(n string, mod ConsensusModule) {
            defer m.wg.Done()

            log.Info("Starting consensus module", "name", n)
            if err := mod.Start(m.ctx); err != nil {
                log.Error("Failed to start module", "name", n, "error", err)
            }
        }(name, module)
    }

    return nil
}
```

## Usage Example

```go
package main

import (
    "github.com/luxfi/node/multiconsensus"
    "github.com/luxfi/node/multiconsensus/modules/lux"
    "github.com/luxfi/node/multiconsensus/modules/avalanche"
)

func main() {
    // Load configuration
    config, err := multiconsensus.LoadConfig("config.json")
    if err != nil {
        panic(err)
    }

    // Create manager
    manager, err := multiconsensus.NewManager(config)
    if err != nil {
        panic(err)
    }

    // Register Lux v2 module
    luxModule := lux.NewLuxModule()
    if err := luxModule.Initialize(config.Modules["lux-v2"]); err != nil {
        panic(err)
    }
    manager.RegisterModule("lux-v2", luxModule)

    // Register Avalanche module
    avalancheModule := avalanche.NewAvalancheModule()
    if err := avalancheModule.Initialize(config.Modules["avalanche"]); err != nil {
        panic(err)
    }
    manager.RegisterModule("avalanche", avalancheModule)

    // Start all modules
    if err := manager.Start(); err != nil {
        panic(err)
    }

    // Wait for shutdown
    manager.Wait()
}
```

## Network Routing

```go
package multiconsensus

type NetworkRouter struct {
    modules map[string]NetworkConfig
    mu      sync.RWMutex
}

type NetworkConfig struct {
    APIPort     uint16
    StakingPort uint16
    P2PPort     uint16
}

func (r *NetworkRouter) Route(port uint16) (string, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    for module, config := range r.modules {
        if config.APIPort == port ||
           config.StakingPort == port ||
           config.P2PPort == port {
            return module, nil
        }
    }

    return "", ErrNoRouteFound
}
```

## Testing

### Unit Tests

```go
func TestMultiConsensusManager(t *testing.T) {
    config := &Config{
        Modules: map[string]ModuleConfig{
            "test": {
                Name:      "test",
                NetworkID: 12345,
            },
        },
    }

    manager, err := NewManager(config)
    require.NoError(t, err)

    mockModule := &MockConsensusModule{}
    err = manager.RegisterModule("test", mockModule)
    require.NoError(t, err)

    err = manager.Start()
    require.NoError(t, err)

    // Verify module was started
    assert.True(t, mockModule.Started)
}
```

### Integration Tests

```bash
# Run integration tests
go test -tags=integration ./multiconsensus/tests/integration/...

# Run with race detection
go test -race -tags=integration ./multiconsensus/tests/integration/...
```

## Monitoring and Metrics

```go
// Expose metrics for each module
type ModuleMetrics struct {
    Height          prometheus.Gauge
    Peers           prometheus.Gauge
    TxPoolSize      prometheus.Gauge
    IsBootstrapped  prometheus.Gauge
    HealthScore     prometheus.Gauge
}

// Combined health endpoint
func (m *Manager) Health() map[string]interface{} {
    health := make(map[string]interface{})

    m.mu.RLock()
    defer m.mu.RUnlock()

    for name, module := range m.modules {
        moduleHealth, err := module.Health()
        if err != nil {
            health[name] = map[string]interface{}{
                "healthy": false,
                "error":   err.Error(),
            }
        } else {
            health[name] = moduleHealth
        }
    }

    return health
}
```

## Security Considerations

1. **Process Isolation**: Consider running modules in separate processes
2. **Resource Limits**: Enforce CPU, memory, and I/O limits per module
3. **Key Separation**: Use different validator keys for each network
4. **API Authentication**: Separate API credentials per module

## Future Enhancements

1. **Dynamic Module Loading**: Add/remove modules without restart
2. **Cross-Module Communication**: Enable secure inter-module messaging
3. **Unified Monitoring**: Single dashboard for all modules
4. **Shared Components**: Reuse common components across modules

## References

- [Multi-Consensus Architecture](../../docs/MULTI_CONSENSUS_ARCHITECTURE.md)
- [Lux Node Documentation](../README.md)
- [Avalanche Documentation](https://docs.avax.network)
