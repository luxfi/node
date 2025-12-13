# Genesis Builder

This package provides genesis building functionality for Lux networks. It depends on node types and is responsible for converting genesis configuration into actual genesis bytes.

## Architecture

The genesis builder bridges the gap between the decoupled `github.com/luxfi/genesis` package (which provides JSON-based configuration) and the node's internal types:

```
github.com/luxfi/genesis          →  github.com/luxfi/node/genesis/builder
(JSON config, no node deps)           (Type conversion, genesis building)
```

## Types

### StakingConfig

```go
type StakingConfig struct {
    UptimeRequirement float64
    MinValidatorStake uint64
    MaxValidatorStake uint64
    MinDelegatorStake uint64
    MinDelegationFee  uint32
    MinStakeDuration  time.Duration  // Converted from uint64 seconds
    MaxStakeDuration  time.Duration  // Converted from uint64 seconds
    RewardConfig      reward.Config  // Uses platformvm/reward.Config

    // BLS key information for genesis replay
    NodeID               string
    BLSPublicKey         []byte
    BLSProofOfPossession []byte
}
```

### TxFeeConfig

```go
type TxFeeConfig struct {
    TxFee              uint64
    CreateAssetTxFee   uint64
    DynamicFeeConfig   gas.Config  // From vms/components/gas
    ValidatorFeeConfig fee.Config  // From vms/platformvm/validators/fee
}
```

### Bootstrapper

```go
type Bootstrapper struct {
    ID ids.NodeID      // Parsed from string
    IP netip.AddrPort  // Parsed from string
}
```

## Functions

### Configuration Retrieval

```go
// Get staking config with time.Duration types
func GetStakingConfig(networkID uint32) StakingConfig

// Get tx fee config with gas.Config and fee.Config
func GetTxFeeConfig(networkID uint32) TxFeeConfig

// Get parsed bootstrappers
func GetBootstrappers(networkID uint32) ([]Bootstrapper, error)

// Sample random bootstrappers
func SampleBootstrappers(networkID uint32, count int) ([]Bootstrapper, error)

// Get genesis config (delegates to genesis package)
func GetConfig(networkID uint32) *genesiscfg.Config
```

### Genesis Building

```go
// Build genesis bytes from config
func FromConfig(config *genesiscfg.Config) ([]byte, ids.ID, error)

// Build genesis from file
func FromFile(networkID uint32, filepath string, stakingCfg *StakingConfig) ([]byte, ids.ID, error)

// Build genesis from base64 content
func FromFlag(networkID uint32, genesisContent string, stakingCfg *StakingConfig) ([]byte, ids.ID, error)

// Build genesis for database replay mode
func FromDatabase(networkID uint32, dbPath string, dbType string, stakingCfg *StakingConfig) ([]byte, ids.ID, error)
```

### Helpers

```go
// Get VM genesis transaction
func VMGenesis(genesisBytes []byte, vmID ids.ID) (*pchaintxs.Tx, error)

// Get chain and API aliases
func Aliases(genesisBytes []byte) (map[string][]string, map[ids.ID][]string, error)

// Get LUX asset ID from XVM genesis
func XAssetID(xvmGenesisBytes []byte) (ids.ID, error)
```

## Default Fee Configurations

The package provides default fee configurations for each network:

```go
// Dynamic fee configs
var MainnetDynamicFeeConfig gas.Config
var TestnetDynamicFeeConfig gas.Config
var LocalDynamicFeeConfig   gas.Config

// Validator fee configs
var MainnetValidatorFeeConfig fee.Config
var TestnetValidatorFeeConfig fee.Config
var LocalValidatorFeeConfig   fee.Config
```

## VM Aliases

```go
var VMAliases = map[ids.ID][]string{
    constants.PlatformVMID: {"platform"},
    constants.XVMID:        {"xvm"},
    constants.EVMID:        {"evm"},
    secp256k1fx.ID:         {"secp256k1fx"},
    nftfx.ID:               {"nftfx"},
    propertyfx.ID:          {"propertyfx"},
}
```

## Usage Example

```go
package main

import (
    "github.com/luxfi/node/genesis/builder"
    "github.com/luxfi/constants"
)

func main() {
    // Get network configuration
    stakingCfg := builder.GetStakingConfig(constants.MainnetID)
    txFeeCfg := builder.GetTxFeeConfig(constants.MainnetID)

    // Get bootstrappers
    bootstrappers, err := builder.SampleBootstrappers(constants.MainnetID, 5)
    if err != nil {
        panic(err)
    }

    // Build genesis bytes
    config := builder.GetConfig(constants.MainnetID)
    genesisBytes, luxAssetID, err := builder.FromConfig(config)
    if err != nil {
        panic(err)
    }

    // Get chain aliases
    apiAliases, chainAliases, err := builder.Aliases(genesisBytes)
    if err != nil {
        panic(err)
    }
}
```

## Migration from genesis package

If you were using `github.com/luxfi/genesis` v1.2.x for building genesis bytes, migrate to this package:

| Old (genesis v1.2.x) | New (builder) |
|---------------------|---------------|
| `genesis.FromConfig(...)` | `builder.FromConfig(...)` |
| `genesis.FromFile(...)` | `builder.FromFile(...)` |
| `genesis.FromFlag(...)` | `builder.FromFlag(...)` |
| `genesis.VMGenesis(...)` | `builder.VMGenesis(...)` |
| `genesis.Aliases(...)` | `builder.Aliases(...)` |
| `genesis.VMAliases` | `builder.VMAliases` |
| `genesis.GetStakingConfig(...)` | `builder.GetStakingConfig(...)` |
| `genesis.GetTxFeeConfig(...)` | `builder.GetTxFeeConfig(...)` |

For JSON-based configuration only (without building), continue using `github.com/luxfi/genesis` v1.3.x.

## License

Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
See the file LICENSE for licensing terms.
