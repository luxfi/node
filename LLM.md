# LLM.md - AI Development Guide

This file provides guidance for AI assistants working with the Lux node codebase.

## Repository Overview

Lux blockchain node implementation - a high-performance, multi-chain blockchain platform written in Go. Features multiple consensus engines (Chain, DAG, PQ), EVM compatibility, and a multi-chain architecture with specialized capabilities.

**Key Context:**
- Fork of Avalanche with Lux-specific enhancements
- Network ID: 96369 (Lux Mainnet), 96368 (Testnet)
- Go Version: 1.23.9+
- Database: BadgerDB (primary), PebbleDB support

## Essential Commands

### Building
```bash
# Build node binary
./scripts/run_task.sh build
# Output: ./build/luxd

# Build specific components
go build -o luxd ./app
```

### Testing
```bash
# Run all tests
go test ./... -count=1

# Run specific package
go test ./vms/platformvm/state -count=1

# With race detection
go test -race ./...
```

### Code Generation
```bash
# Generate mocks
go generate ./...

# Regenerate protobuf
./scripts/run_task.sh generate-protobuf
```

### Running
```bash
# Mainnet
./build/luxd

# Testnet
./build/luxd --network-id=testnet

# Local network
lux network start
```

## Architecture

### Multi-Chain Design

| Chain | Purpose | VM |
|-------|---------|-----|
| **P-Chain** | Staking, validators, L1 validators | PlatformVM |
| **X-Chain** | UTXO-based asset exchange | ExchangeVM (AVM) |
| **C-Chain** | EVM smart contracts | EVM |
| **D-Chain** | DEX (order book, perpetuals) | DexVM |
| **T-Chain** | Threshold FHE operations | ThresholdVM |
| **Q-Chain** | Post-quantum cryptography | Ringtail signatures |

### Consensus Layer
Located in `/consensus/` (separate package via go.mod replace):
- **Chain Engine**: Linear blockchain consensus
- **DAG Engine**: Directed acyclic graph for parallel processing
- **PQ Engine**: Post-quantum consensus

### Virtual Machines
Located in `/vms/`:
- **platformvm**: Staking, validation, network management
- **avm/exchangevm**: Asset transfers, UTXO model
- **dexvm**: DEX with order book, perpetuals, AMM
- **thresholdvm**: Threshold FHE for confidential computing
- **proposervm**: Block proposer wrapper VM

### Key Interfaces

**p2p.Sender** (from `github.com/luxfi/p2p`):
```go
type Sender interface {
    SendRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error
    SendResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error
    SendError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error
    SendGossip(ctx context.Context, config SendConfig, msg []byte) error
}
```

**Keychain Interfaces** (from `github.com/luxfi/keychain`):
```go
type Signer interface {
    SignHash([]byte) ([]byte, error)
    Sign([]byte) ([]byte, error)
    Address() ids.ShortID
}

type Keychain interface {
    Get(addr ids.ShortID) (Signer, bool)
    Addresses() set.Set[ids.ShortID]
}
```

## Package Dependencies

### CRITICAL: Use Lux packages only
- ✅ `github.com/luxfi/node`
- ✅ `github.com/luxfi/geth` (NOT go-ethereum)
- ✅ `github.com/luxfi/consensus`
- ✅ `github.com/luxfi/keychain`
- ✅ `github.com/luxfi/ledger`
- ✅ `github.com/luxfi/lattice` (FHE)
- ❌ `github.com/ava-labs/*`
- ❌ `github.com/ethereum/go-ethereum`

### Import Aliasing
Avoid conflicts with consensus packages:
```go
import (
    platformblock "github.com/luxfi/node/vms/platformvm/block"
    consensusblock "github.com/luxfi/consensus/engine/chain/block"
)
```

## Token Denomination

LUX uses **6 decimals** (microLUX base unit) on P-Chain/X-Chain:

| Unit | Value |
|------|-------|
| µLUX (MicroLux) | 1 (base) |
| mLUX (MilliLux) | 1,000 |
| LUX | 1,000,000 |
| TLUX (TeraLux) | 10^18 |

**Supply Cap**: 2 trillion LUX (2 × 10^18 µLUX)

C-Chain uses standard EVM 18 decimals (Wei).

See `utils/units/lux.go` for constants.

## Key Technical Decisions

### Genesis Architecture
```
github.com/luxfi/genesis (JSON config)  →  github.com/luxfi/node/genesis/builder (type conversion)
```
- Genesis package has no node dependencies
- Builder package handles type conversions (string → ids.NodeID, uint64 → time.Duration)

### CGO Dependencies
These require CGO for full functionality (graceful fallback when disabled):
- `consensus/quasar` - GPU NTT acceleration
- `vms/thresholdvm/fhe` - GPU FHE operations
- `x/blockdb` - zstd compression

### FHE (Fully Homomorphic Encryption)
Located in `vms/thresholdvm/fhe/`:
- Uses `github.com/luxfi/lattice/multiparty` for DKG
- Lattice-based cryptography only (no fallbacks)
- Threshold decryption via Warp messaging

**Precompile Addresses:**
| Precompile | Address |
|------------|---------|
| Fheos | `0x0200000000000000000000000000000000000080` |
| ACL | `0x0200000000000000000000000000000000000081` |
| InputVerifier | `0x0200000000000000000000000000000000000082` |
| Gateway | `0x0200000000000000000000000000000000000083` |

## Common Gotchas

### 1. AppSender Interface
Node's rpcchainvm must implement `consensuscore.AppSender`, not just `warp.Sender`.

### 2. Subnet Tracking
Nodes don't automatically track subnets. Use:
```bash
--track-subnets=<SubnetID>
```
Or create config: `~/.lux/runs/.../node*/subnetConfigs/<SubnetID>.json`

### 3. Genesis blobSchedule
Mainnet genesis requires Cancun fork config:
```json
"blobSchedule": {
  "cancun": {
    "max": 6,
    "target": 3,
    "baseFeeUpdateFraction": 3338477
  }
}
```

### 4. Network Snapshots
CLI creates new directories on restart. Use snapshots:
```bash
lux network save --snapshot-name <name>
lux network start --snapshot-name <name>
```

### 5. EIP-3860 Historic Blocks
For importing pre-merge blocks, Shanghai must be active based on `ShanghaiTime`, not merge status.

## File Locations

| Item | Path |
|------|------|
| luxd binary | `~/.lux/bin/luxd/luxdv*/luxd` |
| VM plugins | `~/.lux/plugins/<VMID>` |
| Network runs | `~/.lux/runs/local_network/network_*` |
| Snapshots | `~/.lux/snapshots/` |
| Chain configs | `~/.lux/chain-configs/<BlockchainID>/` |

## Build Order

1. Build node: `cd ~/work/lux/node && go build -o /tmp/luxd ./main`
2. Install: `cp /tmp/luxd ~/.lux/bin/luxd/luxdv1.21.0/luxd`
3. Build EVM: `cd ~/work/lux/evm && go build -o ~/.lux/plugins/<VMID> ./plugin`
4. Start: `lux network start --mainnet`

## Related Repositories

| Repo | Purpose |
|------|---------|
| `~/work/lux/consensus` | Consensus engines (Chain, DAG, PQ) |
| `~/work/lux/geth` | C-Chain EVM implementation |
| `~/work/lux/evm` | EVM plugin |
| `~/work/lux/genesis` | Genesis configurations |
| `~/work/lux/cli` | Management CLI |
| `~/work/lux/netrunner` | Network testing |
| `~/work/lux/dex` | DEX implementation |
| `~/work/lux/standard` | Solidity contracts (including FHE) |
| `~/work/lux/lattice` | Lattice cryptography |

## Security Notes

### Mainnet Readiness (2025-12-31)
- Memory exhaustion protection (IP tracker limits, bloom filter caps)
- BLS signature CGO/pure-Go consistency
- Replay attack prevention with timestamp validation
- Safe math in DEX operations

### Known CGO Stubs
When CGO disabled, these use CPU fallbacks:
- `consensus/quasar/gpu_ntt_nocgo.go`
- `vms/thresholdvm/fhe/gpu_fhe_nocgo.go`
- `vms/zkvm/accel/accel_mlx.go`

---

*Last Updated*: 2025-01-13
