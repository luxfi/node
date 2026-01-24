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
    consensusblock "github.com/luxfi/consensus/engine/chain"
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

### ZAP Transport (Zero-Copy App Proto)
ZAP is the default high-performance binary wire protocol for VM<->Node communication.
gRPC support is available via build tag for testing/compatibility.

**Build Tags:**
```bash
go build                  # ZAP only (default, production)
go build -tags=grpc       # gRPC support (for testing/compatibility)
```

**Key Packages:**
- `github.com/luxfi/api/zap` - Core wire protocol and message types
- `github.com/luxfi/vm/rpc/sender` - p2p.Sender over ZAP/gRPC
- `vms/rpcchainvm/sender/` - Node-side sender implementation
- `vms/platformvm/warp/zwarp/` - ZAP-based warp signing client/server

**Wire Protocol Format:**
```
[4 bytes: length][1 byte: message type][payload...]
```

**Performance Benefits:**
- Zero-copy serialization (buffer pooling via sync.Pool)
- ~5-10x faster serialization than protobuf
- ~2-3x lower latency (no HTTP/2 overhead)
- ~30-50% CPU reduction on hot paths

**Sender Usage:**
```go
// ZAP transport (default)
s := sender.ZAP(zapConn)

// gRPC transport (requires -tags=grpc build)
s := sender.GRPC(senderpb.NewSenderClient(grpcConn))
```

**Warp over ZAP:**
The `zwarp` package implements warp signing via ZAP:
```go
// Client implements warp.Signer over ZAP
client := zwarp.NewClient(zapConn)
sig, err := client.Sign(unsignedMsg)

// BatchSign for HFT optimization
sigs, errs := client.BatchSign(messages)
```

## Common Gotchas

### 1. P2P Sender Interface
Node's rpcchainvm implements `p2p.Sender` (from `github.com/luxfi/p2p`) for cross-chain messaging.
The `sender` package is a gRPC implementation of `p2p.Sender`.

### 2. Chain Tracking
Nodes don't automatically track chains. Use:
```bash
--track-chains=<ChainID>
```
Or create config: `~/.lux/runs/.../node*/chainConfigs/<ChainID>.json`

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

### 6. Genesis Hash Mismatch on Restart
**Problem**: "db contains invalid genesis hash" error when restarting nodes.

**Cause**: Genesis bytes are rebuilt from JSON config on each start. Due to non-deterministic JSON serialization (map iteration order), the rebuilt bytes differ from the original, causing hash mismatch.

**Solution**: Genesis bytes are now cached to `genesis.bytes` file in the node's data directory. On subsequent restarts, the cached bytes are used directly. This happens automatically when using `--genesis-file`.

### 7. VM Config Format Mismatch
**Problem**: "failed to parse config: unknown codec version" for T-Chain (ThresholdVM) or Z-Chain (ZKVM) in dev mode.

**Cause**: Two issues:
1. Genesis builder passes JSON config (`{"version":1,"message":"..."}`) to VMs that expect binary codec format
2. Dev mode's automining config injection converts all chain configs to JSON, breaking binary-codec VMs

**Solution**:
- `genesis/builder/builder.go`: T-Chain and Z-Chain use `[]byte(config.TChainGenesis)` (empty bytes for defaults) instead of `getGenesis()` which returns JSON
- `chains/manager.go`: `injectAutominingConfig` only injects for `EVMID`, skipping binary-codec VMs

**Alternative**: Use `--genesis-raw-bytes` flag to pass base64-encoded pre-built genesis bytes directly.

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

*Last Updated*: 2026-01-19
