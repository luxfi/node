# LLM.md - AI Development Guide

This file provides guidance for AI assistants working with the Lux node codebase.

## Repository Overview

Lux blockchain node implementation - a high-performance, multi-chain blockchain platform written in Go. Features multiple consensus engines (Chain, DAG, PQ), EVM compatibility, and a multi-chain architecture with specialized capabilities.

**Key Context:**
- Original Lux Network node — NOT a fork
- Network ID: 96369 (Lux Mainnet), 96368 (Testnet), 96370 (Devnet)
- Go Version: 1.26.1+
- Database: ZapDB (primary, default)

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

Primary network (P/X/C) uses Quasar consensus via `luxfi/consensus`.
All new native chains use Quasar (BLS + Ringtail + ML-DSA). No snow/snowball.

| Chain | Purpose | VM | Consensus |
|-------|---------|-----|-----------|
| **P-Chain** | Staking, validators, L1 validators | PlatformVM | Quasar |
| **X-Chain** | UTXO-based asset exchange | XVM | Quasar |
| **C-Chain** | EVM smart contracts | EVM | Quasar |
| **A-Chain** | AI inference, model registry | AIVM | Quasar |
| **B-Chain** | Cross-chain bridge operations | BridgeVM | Quasar |
| **D-Chain** | DEX (order book, perpetuals) | DexVM | Quasar |
| **G-Chain** | On-chain graph database | GraphVM | Quasar |
| **I-Chain** | Decentralized identity (DID/VC) | IdentityVM | Quasar |
| **K-Chain** | Post-quantum key management | KeyVM | Quasar |
| **M-Chain** | Threshold signing (MPC) | ThresholdVM | Quasar |
| **O-Chain** | Oracle price feeds | OracleVM | Quasar |
| **Q-Chain** | Post-quantum consensus coordination | QuantumVM | Quasar |
| **R-Chain** | Cross-chain message relay | RelayVM | Quasar |
| **S-Chain** | Service node coordination | ServiceNodeVM | Quasar |
| **T-Chain** | Cross-chain teleport (bridge+relay+oracle) | TeleportVM | Quasar |
| **Z-Chain** | Zero-knowledge proofs (FHE) | ZKVM | Quasar |

### Consensus Layer
Located in `/consensus/` (separate package `github.com/luxfi/consensus`):
- **Quasar**: Production consensus -- BLS12-381 + Ringtail (lattice) + ML-DSA-65 (FIPS 204)
- **Chain Engine**: Linear blockchain consensus (Nova sub-protocol)
- **DAG Engine**: Directed acyclic graph for parallel processing (Nebula sub-protocol)
- **PQ Engine**: Post-quantum finality layer

Sub-protocols: Photon (sampling) -> Wave (voting) -> Focus (confidence) -> Ray/Field (finality)

### Virtual Machines
Located in `/vms/`:
- **platformvm**: Staking, validation, network management
- **xvm**: Asset transfers, UTXO model
- **dexvm**: DEX with order book, perpetuals, AMM
- **thresholdvm**: Threshold MPC and FHE for confidential computing
- **quantumvm**: PQ consensus coordination (ML-DSA, Ringtail)
- **identityvm**: Decentralized identity (DID, verifiable credentials)
- **keyvm**: Post-quantum key management (ML-KEM, ML-DSA)
- **bridgevm**: Cross-chain bridge with MPC attestation
- **oraclevm**: Decentralized oracle network
- **aivm**: AI inference verification
- **graphvm**: On-chain graph database
- **relayvm**: Cross-chain message relay
- **servicenodevm**: Service node epoch management
- **teleportvm**: Unified bridge+relay+oracle
- **zkvm**: Zero-knowledge proof verification
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
- ❌ `github.com/luxfi/*` legacy upstream forks
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

## RNS Transport (Reticulum Network Stack)

The node supports RNS as an alternative transport layer alongside TCP/IP, enabling mesh networking, LoRa connectivity, and offline-first validator operation.

**Specification**: [LP-9701](../lps/LPs/lp-9701-reticulum-network-stack.md)

### Endpoint Types

The `net/endpoints` package supports three addressing modes:

```go
// IP address
endpoint := endpoints.NewIPEndpoint(netip.MustParseAddrPort("203.0.113.50:9631"))

// Hostname (DNS resolved)
endpoint, _ := endpoints.NewHostnameEndpoint("validator.example.com", 9631)

// RNS destination (mesh/LoRa)
endpoint, _ := endpoints.NewRNSEndpointFromHex("rns://a5f72c3d4e5f60718293a4b5c6d7e8f9")
```

### Key Files

| File | Purpose |
|------|---------|
| `net/endpoints/endpoint.go` | Unified endpoint abstraction (IP, hostname, RNS) |
| `network/dialer/rns_transport.go` | RNS transport implementation |
| `network/dialer/rns_identity.go` | Classical identity (Ed25519 + X25519) |
| `network/dialer/rns_identity_pq.go` | Hybrid PQ identity (+ ML-DSA + ML-KEM) |
| `network/dialer/rns_link.go` | Encrypted link protocol with PQ support |
| `network/dialer/rns_announce.go` | Destination discovery and announcements |

### Configuration

```yaml
# ~/.lux/config.yaml
rns:
  enabled: true
  configPath: ~/.lux/reticulum
  announceInterval: 5m
  interfaces:
    - AutoInterface
    - TCPClientInterface
  linkTimeout: 30s
  postQuantum: true        # Enable hybrid PQ mode
  requirePostQuantum: false # Allow classical-only peers
```

## Post-Quantum Cryptography (Hybrid Mode)

RNS transport supports hybrid post-quantum cryptography combining classical algorithms with NIST-standardized post-quantum primitives (TLS 1.3-like approach).

### Cryptographic Suite

| Purpose | Classical | Post-Quantum | Security |
|---------|-----------|--------------|----------|
| Identity Signing | Ed25519 | ML-DSA-65 | NIST Level 3 |
| Key Exchange | X25519 | ML-KEM-768 | NIST Level 3 |
| Session Encryption | AES-256-GCM | - | 256-bit |
| Key Derivation | HKDF-SHA256 | - | - |

### Forward Secrecy

- **Ephemeral Keys**: Fresh X25519 + ML-KEM keypairs generated per session
- **Key Destruction**: Ephemeral private keys zeroed after handshake
- **Hybrid Derivation**: `combined_secret = X25519_shared || ML_KEM_shared`
- **Defense-in-Depth**: Secure if either algorithm remains unbroken

### Wire Format Sizes

| Component | Classical | Hybrid | Delta |
|-----------|-----------|--------|-------|
| Public Identity | 64 bytes | ~3.2 KB | +3.1 KB |
| Signature | 64 bytes | ~2.5 KB | +2.4 KB |
| Key Exchange | 64 bytes | ~1.2 KB | +1.1 KB |
| Handshake Total | ~256 bytes | ~7.5 KB | +7.2 KB |

### Backward Compatibility

- **Capability Exchange**: Handshake advertises PQ support
- **Graceful Fallback**: Falls back to classical if peer lacks PQ
- **Mixed Networks**: PQ and classical validators coexist
- **Policy Enforcement**: `requirePostQuantum: true` rejects classical peers

### Testing PQ Forward Secrecy

```bash
# Run hybrid PQ tests
go test -v -run "TestHybrid" ./node/network/dialer/... -count=1

# Key tests:
# - TestHybridIdentity_SignVerify (ML-DSA-65 signatures)
# - TestHybridIdentity_Encapsulate_Decapsulate (ML-KEM-768)
# - TestHybridRNSLink_Handshake (full hybrid handshake)
# - TestHybridRNSLink_ForwardSecrecy (ephemeral key destruction)
# - TestHybridToClassical_Fallback (backward compatibility)
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

### 8. `vms/components/lux` vs `luxfi/utxo` (parallel UTXO types)
The `github.com/luxfi/node/vms/components/lux` package contains a parallel
`lux.UTXO`/`lux.TransferableInput` type tree alongside `github.com/luxfi/utxo`.
External consumers (e.g. `~/work/liquidity/network-bootstrap/fund.go`) need
to import the `vms/components/lux` variant to interop with PlatformVM/AVM
tx builders — `luxfi/utxo` types alone are not accepted by the X→P export
path. This is a known anomaly pending #58 follow-up consolidation; do NOT
collapse the two packages without that migration.

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

### 11. P-Chain Block Sync (isMissingContextError "not found")
**Problem**: New validator node stays at P-chain height 0 even after connecting to testnet peers. Blocks received via Put/PushQuery are silently discarded.

**Root Cause**: `HandleIncomingBlock` returns `"not found"` when the block's parent isn't in the local state. `isMissingContextError` didn't recognize `"not found"` as a missing-context condition, so `requestContext` (GetAncestors) was never called.

**Fix** in `chains/manager.go`, `isMissingContextError`:
```go
// Added "not found" pattern:
strings.Contains(errStr, "not found") // parent block not in local state
```

**Effect**: Now when a block arrives whose parent is unknown, the handler sends `GetAncestors` to the peer, receives the full ancestor chain, and processes blocks in order, advancing the P-chain height.

**Note**: The network layer (`network.go:sequencerID`) already correctly maps native chain IDs (P, C, X, etc.) to `PrimaryNetworkID` for validator set lookups — no separate gossip fix needed.

### Known CGO Stubs
When CGO disabled, these use CPU fallbacks:
- `consensus/quasar/gpu_ntt_nocgo.go`
- `vms/thresholdvm/fhe/gpu_fhe_nocgo.go`
- `vms/zkvm/accel/accel_mlx.go`

### 8. ZAP CreateHandlers for VM HTTP Endpoints
**Problem**: C-chain and D-chain RPC endpoints returning 404 despite VMs running.

**Cause**: The `zap.Client` in `vms/rpcchainvm/zap/client.go` did not implement the `CreateHandlers` interface. The node checks for this interface to register HTTP handlers (like `/rpc`, `/ws`) with the HTTP server.

**Solution**: Added `CreateHandlers` method to `zap.Client` that:
1. Sends `MsgCreateHandlers` via ZAP wire protocol to the VM
2. Receives `CreateHandlersResponse` with list of handlers (prefix + server address)
3. Creates `httputil.NewSingleHostReverseProxy` for each handler
4. Returns `map[string]http.Handler` for registration

**File Modified**: `vms/rpcchainvm/zap/client.go`

**Verification**:
```bash
curl -s -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
  http://localhost:9640/ext/bc/C/rpc
# Returns: {"jsonrpc":"2.0","id":1,"result":"0x17870"}
```

### 9. Root "/" Endpoint Handler
**Feature**: The node's root endpoint ("/") provides EVM compatibility and node information.

**Behavior**:
- **GET /**: Returns JSON node information (nodeId, networkId, version, chains, endpoints)
- **POST /**: Proxies JSON-RPC requests directly to C-chain `/ext/bc/C/rpc`
- **OPTIONS /**: Returns CORS preflight headers

**Files Modified**: `server/http/router.go`, `server/http/server.go`

**Types**:
```go
type RootInfo struct {
    NodeID    string `json:"nodeId,omitempty"`
    NetworkID uint32 `json:"networkId,omitempty"`
    Version   string `json:"version,omitempty"`
    Ready     bool   `json:"ready"`
    Chains    struct { C, P, X string } `json:"chains"`
    Endpoints struct { RPC, Websocket, Info, Health string } `json:"endpoints"`
}

type RootInfoProvider interface {
    GetRootInfo() RootInfo
}
```

**Usage**:
```bash
# Get node info
curl http://localhost:9650/

# Send EVM JSON-RPC directly to root (proxied to C-chain)
curl -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
  http://localhost:9650/
```

**Implementation Notes**:
- The Server interface exposes `SetRootInfoProvider(provider)` to configure node info
- When no provider is set, returns default endpoint paths
- POST errors return proper JSON-RPC error format if C-chain unavailable

### 10. BLS Key Not Loaded into Validators Manager
**Problem**: Health check shows "validator doesn't have a BLS key" despite BLS keys being correctly configured in genesis.

**Cause**: The `initValidatorSets()` function in `/vms/platformvm/state/state.go` was skipping validator population when `NumNets() != 0`. This happened because:
1. Network layer might pre-populate validators (without BLS keys) before state initialization
2. When `initValidatorSets()` runs, it sees validators exist and skips adding them with proper BLS keys
3. The health check queries `n.vdrs.GetValidator()` which returns validator with nil PublicKey

**Solution**: Modified `initValidatorSets()` to always add validators (not skip when `NumNets() != 0`). The `AddStaker` method replaces existing entries, so validators get updated with proper BLS keys.

**File Modified**: `vms/platformvm/state/state.go` (line ~2144)

**Before**:
```go
if s.validators.NumNets() != 0 {
    // skip re-adding them here
    return nil
}
```

**After**:
```go
if s.validators.NumNets() != 0 {
    log.Info("initValidatorSets: validator manager not empty, will update with BLS keys")
}
// Continue to add validators with proper BLS keys
```

**Verification**:
```bash
curl -s http://localhost:9650/ext/health | jq '.checks.bls'
# Should show: "message": "node has the correct BLS key"
```

## Benchmark Results (Single Node)

Testing conducted on a single Lux validator node (testnet mode, macOS):

| Metric | Result |
|--------|--------|
| Sustained TPS | 1,091 TPS (60s benchmark) |
| Peak TPS | 1,094 TPS (5 workers) |
| Query Performance | 840 queries/sec |
| Query Latency | 17.67ms avg |
| Optimal Concurrency | 5 workers |
| Total Transactions | 65,497 txs/min |

**Concurrency Scaling:**
| Workers | TPS |
|---------|-----|
| 1 | 438 |
| 5 | 1,094 (optimal) |
| 10 | 684 |
| 20 | 521 |

**Key Findings:**
- Single node achieves ~1,100 TPS sustained with optimal concurrency
- Higher concurrency (>5 workers) decreases TPS due to nonce contention
- Query latency is consistent at ~18ms
- Testnet mode uses K=20 Lux consensus (vs K=1 dev mode)

**Benchmark Command:**
```bash
cd ~/work/lux/benchmarks
LUX_ENDPOINT="http://localhost:9640/ext/bc/C/rpc" \
PRIVATE_KEY="<funded_key>" \
./bin/bench tps --chains=lux --duration=60s --concurrency=5
```

---

*Last Updated*: 2026-02-04
