# AI Development Guide

This file provides guidance for AI assistants working with the Lux node codebase.

## Repository Overview

Lux blockchain node implementation - a high-performance, multi-chain blockchain platform written in Go. Features multiple consensus engines (Chain, DAG, PQ), EVM compatibility, and a multi-chain architecture with specialized capabilities.

**Key Context:**
- Original Lux Network node — NOT a fork
- Latest Tag: v1.26.31
- Network ID: 96369 (Lux Mainnet), 96368 (Testnet), 96370 (Devnet)
- Go Version: go.mod floor `1.26.4`; builder images pin `1.26.5` (see [Go toolchain policy](#go-toolchain-policy))
- Database: ZapDB (primary, default)

## Post-E2E-PQ State (current)

Node now consumes the locked `ChainSecurityProfile` end-to-end and enforces
strict-PQ at four boundaries: peer handshake, mempool, validator scheme
selection, and EVM contract auth.

- `node/node.go:initSecurityProfile` (F102 closure) loads the chain-wide
  profile from genesis at boot, hashes it, and pins it into every chain's
  bootstrap. Resolved profile is what every downstream verifier consults.
- `network/peer/scheme_gate.go` — `SchemeGate.Classify(presented, site)`
  funnels every inbound NodeID through the profile's
  `AcceptsValidatorScheme`. Wire-typed NodeID (`luxfi/ids` TypedNodeID +
  scheme byte) is the canonical handshake form.
- `vms/txs/auth/policy.go` — `ClassicalCompatRegistry` + strict-PQ
  mempool gate. Both `platformvm` (P-Chain) and `avm` (X-Chain) mempools
  refuse classical credentials when the resolved profile has
  `ForbidECDSAContractAuth=true`.
- `vms/mldsafx/` — re-exports the consensus `mldsafx` UTXO feature
  extension as the node-owned UTXO surface (ML-DSA-65 verify).
- `network/peer` PQ handshake — ML-KEM-768 / ML-KEM-1024 KEM +
  ML-DSA-65 identity (`dc906d281b`).

### Recent significant commits

| SHA | Tag | Impact |
|-----|-----|--------|
| (pending) | next | LP-023 Phase 1 batch 2: 5 more native-ZAP tx types — BaseTx v1, RegisterL1ValidatorTx v1, SlashValidatorTx v1, TransferChainOwnershipTx v1, RemoveChainValidatorTx. Cross-type Parse mean speedup 8.5× over linearcodec. Variable-length nested-object schemas (Outs/Ins/full OutputOwners/Warp Message/Evidence) deferred to batch 3. |
| `e77a7ef78e` | (pre) | LP-023 Phase 1 batch 1: 4 simple tx types + bench harness. 37× Parse, 5.6× cross-type mean. |
| `9df72a6f55` | v1.26.10 | Wire ChainSecurityProfile into bootstrap (closes F102) |
| `c4af52411e` | v1.26.10 | X-Chain (avm) mempool refuses classical creds under strict-PQ |
| `a14a1601f4` | v1.26.10 | P-Chain (platformvm) mempool refuses classical creds under strict-PQ |
| `1cf0aa80ca` | v1.26.10 | ClassicalCompatRegistry + strict-PQ mempool gate |
| `a0f4f4b21c` | v1.26.10 | vms/mldsafx: re-export ML-DSA feature extension |
| `448fdeb7a1` | v1.26.10 | ML-DSA-65 promoted to canonical NodeID under strict-PQ |
| `dc906d281b` | v1.26.10 | PQ peer handshake — ML-KEM-768/1024 + ML-DSA-65 identity |

### Active versions
- Repo: `v1.26.12` (next bump: `v1.26.13`).
- Pinned: `consensus v1.23.4+` (needed `ValidatorSchemeID`),
  `crypto v1.18.5`, `ids v1.2.9` (will move to v1.2.10 in next bump for
  `TypedNodeID`), `genesis v1.9.6`.

### Cross-repo dependencies
- `luxfi/consensus` → profile + auth + zchain types
- `luxfi/crypto` → ML-DSA / ML-KEM / SLH-DSA primitives
- `luxfi/genesis` → genesis-pinned profile (`Resolve` at load)
- `luxfi/ids` → `TypedNodeID` wire form (consumed at handshake)
- `luxfi/geth` → EVM (for `vm.SetActiveSecurityProfile` install point)

### Where to look for X
- Profile resolve at boot: `node/node.go:initSecurityProfile`
- Profile RPC + REST + metrics: `service/security/`
  - JSON-RPC namespace: `security` at `POST /v1/security`
    (methods `securityProfile`, `blockSecurity`)
  - REST sidecars: `GET /v1/security/profile`, `GET /v1/security/block/{n}`
  - Prometheus gauges: `/v1/metrics` under the `security_*` family
- Peer scheme gate: `network/peer/scheme_gate.go`
- Classical-compat registry: `vms/txs/auth/policy.go`
- Mempool gate (P-Chain): `vms/platformvm/mempool/*.go`
- Mempool gate (X-Chain): `vms/avm/mempool/*.go`
- ML-DSA feature extension: `vms/mldsafx/`

### Open follow-ups
- `vms/zkvm/accel/` still soft-falls-back when CGO is disabled; Z-Chain
  proof verification path needs CGO-required mode for production strict-PQ.
- `vm.SetActiveSecurityProfile` install point exists in `luxfi/geth/core/vm`
  but EVM-side contract-auth refusal still needs a chain-bootstrap call
  (F102 wiring closes the consensus side; geth-side hookup is the
  remaining tail).

## FeePolicy — canonical user-tx fee gate

> **Topology + UTXO ownership + cross-chain fee model** are normatively
> specified by [**LP-0130** (Chain Topology, UTXO Ownership, and Fee
> Model)](https://github.com/luxfi/lps/blob/main/LPs/lp-0130-chain-topology-utxo-ownership-and-fee-model.md).
> Read that LP before touching any VM's fee/settlement path or any
> cross-chain import/export flow. In particular:
>
> - Only **P** and **X** are canonical UTXO state machines (LP-0130 §2).
> - **X** is the money rail; **P** is the staking/reward rail; **LUX**
>   is the fee currency everywhere (LP-0130 §3, §5).
> - **Q-Chain has no user-payable blockspace** — finality is a
>   validator obligation paid via P (LP-0130 §6). `quantumvm` MUST
>   use `NoUserTxPolicy{}` — enforced in chains/quantumvm/feegate.go as of 2026-07-03.
> - **M-Chain fees are service fees** deducted from the originating
>   chain's fee pool, not a user M-balance (LP-0130 §7). `mpcvm`
>   already runs `NoUserTxPolicy{}` — correct.
> - **B-Chain fees** are deducted from the bridged amount (LP-0130 §8).
> - Every non-P/X chain settles worker rewards to X (asset payouts) or
>   P (staker rewards) via epoch fee roots reconciled at Q finality
>   (LP-0130 §4, §11).
> - **Σ-escrow invariant** (LP-0130 I-8): `Σ non-P/X fee balances ==
>   Σ X-side fee escrow` at every Q checkpoint. Drift is a
>   finality-blocking fault.

Every Lux VM that accepts user-submitted txs declares a `fee.Policy`
(package `vms/types/fee`). There is one interface and one validator —
no per-VM bespoke fee structs.

### Policy choice per VM

| VM | Chain | Posture | Policy |
|----|-------|---------|--------|
| dexvm | D-Chain | user-tx | `FlatPolicy{Fee: MinTxFeeFloor, AssetID: UTXOAssetIDFor(networkID)}` |
| zkvm | Z-Chain | user-tx | `FlatPolicy{Fee: MinTxFeeFloor, ...}` |
| aivm | A-Chain | user-tx | `FlatPolicy{Fee: MinTxFeeFloor, ...}` |
| keyvm | K-Chain | user-tx | `FlatPolicy{Fee: MinTxFeeFloor, ...}` |
| bridgevm | B-Chain | user-tx | `FlatPolicy{Fee: MinTxFeeFloor, ...}` (deducted from bridged amount, LP-0130 §8) |
| quantumvm | Q-Chain | **service-only** (LP-0130 §6) | `NoUserTxPolicy{}` — validator obligation, no user-payable blockspace |
| identityvm | I-Chain | user-tx | `FlatPolicy{Fee: MinTxFeeFloor, ...}` |
| mpcvm | M-Chain | service-only (LP-0130 §7) | `NoUserTxPolicy{}` — fees pulled from originating chain's fee pool |
| oraclevm | O-Chain | service-only | `NoUserTxPolicy{}` |
| relayvm | R-Chain | service-only | `NoUserTxPolicy{}` |
| graphvm | G-Chain | read-only | `NoUserTxPolicy{}` (GraphQL refuses `mutation`) |
| evm | C-Chain | user-tx | native EVM gas (gas * gasPrice >= 0 enforced upstream); balance is X-imported LUX (LP-0130 §10) |
| platformvm | P-Chain | user-tx | native `TxFee` field on Config |
| avm | X-Chain | user-tx | native `TxFee` field on Config |

`MinTxFeeFloor = 1 mLUX = 1_000_000 nLUX` (the same minimum the P-Chain
base fee enforces). User-facing chains MAY charge more; they MUST NOT
charge less.

### Wiring contract

1. VM struct holds `feePolicy fee.Policy` (and `networkID uint32`).
2. `Initialize` sets `feePolicy = fee.FlatPolicy{...}` (or
   `fee.NoUserTxPolicy{}` for service-only) from `init.Runtime.NetworkID`
   and calls `fee.Validate(vm.feePolicy)` — refuses zero-fee user-facing
   chains at boot, before any block is accepted.
3. The canonical user-tx admission entry (e.g. `SubmitTx`, `IssueTx`,
   `InitiateBridgeTransfer`, mutating service RPCs) calls
   `policy.ValidateFee(paid, asset)` BEFORE mempool insert.
4. Consensus-internal paths (engine→VM block delivery, replay, internal
   tx emission) bypass the fee gate — the policy gates only the
   *user-submitted* entrypoint.

### Where the gates live

- `vms/types/fee/policy.go` — interface + FlatPolicy + NoUserTxPolicy + Validate
- `~/work/lux/chains/<vm>/feegate.go` — per-VM helper + gate method
- `~/work/lux/chains/<vm>/feegate_test.go` — RejectsZeroFee + AcceptsMinFee
- Oracle (O-Chain): `~/work/lux/oracle/vm/feegate.go` (re-exported by `~/work/lux/chains/oraclevm/`)
- Relay (R-Chain): `~/work/lux/relay/vm/feegate.go` (re-exported by `~/work/lux/chains/relayvm/`)
- Graph (G-Chain): `~/work/lux/chains/graphvm/feegate.go` (read-only; NoUserTxPolicy)

## C-Chain tx-fee routing — RewardManager to DAO Safe (P-Chain: NO CHANGE)

Owner tokenomics pivoted: **100% of C-Chain tx fees accrue to the chain's DAO Gov
Safe** via the existing `rewardmanager` precompile (C-Chain only). This needs **no
P-Chain change** — routing is `GetCoinbaseAt` → reward address on the C-Chain. See
`~/work/lux/evm/LLM.md` → "C-Chain Tx-Fee Routing — RewardManager". The P-Chain
`feeRewardPool` fold-in below is **NOT built** (design-only, superseded); no
`vms/platformvm/state` change was made.

<details><summary>Superseded design — 50/50 burn + P-Chain staking-reward fold (dormant option)</summary>

If the DAO ever chooses an in-protocol 50/50 split, the C-Chain half exists (dormant,
`FeeSplitTimestamp` gated off) and the P-Chain fold-in would be: system-triggered epoch
export of the C-Chain vault C→P → persisted `feeRewardPool` in `vms/platformvm/state`
(mirror the `accruedFees` singleton, upgrade-safe) → pro-rata payout at
`vms/platformvm/txs/executor/proposal_tx_executor.go` `rewardValidatorTx` (~line 285),
unified into `PotentialReward`, NO second mint → decrement `currentSupply` by the epoch
burn. Model R1 (move-not-mint), conservation-exact; R2 (burn+re-mint) rejected.
</details>

## Crash-boot recovery armor + upstream delta verdict (vms/proposervm, test-only, 2026-07-28)

**Upstream review (avalanchego master `bcc851822d..c5d3c8aafe`, 6 commits): ZERO code
ports.** grpc bump (no named bug; would cascade through luxfi/vm), README typo, typed
sync-client scaffolding (client-side only, no serving handlers even upstream; our sync
direction is ZAP-native — reference for task #66 only), a Firewood `debug_intermediateRoots`
enabler (our tracers API is `.disabled`, twice N/A), and two streaming-VM-only mempool
guards whose failure modes we already bracket (`miner/worker.go:62 targetTxsSize=1800KiB`
build cap + txpool `ErrGasLimit`/128KB caps under the 2MiB wire cap). **Nothing in the
delta touches proposervm — the open `vm.go:380` preferred-fetch failure gets no upstream
help.** One upstream BRANCH does touch proposervm — `containerman17/proposervm-dedup-capability`
(+72 vm.go): inner-bytes dedup in the block store plus an extra boot-repair arm for the
unclean-shutdown window where the outer index lands above the inner survivor (the very
window `OuterCommittedInnerNot` below pins) — storage-layer work, not the build-path
preferred fetch, so it changes nothing about `vm.go:380` either. What WAS worth taking is
their recovery-test discipline, ported as:

**`vm_crashboot_test.go`** — copy the COMMITTED bytes out from under a RUNNING proposervm
(no Shutdown), boot a SECOND, cold VM over the copy (repair + metadata, Initialize order),
assert source-equality (finality pointer, fork height, every height's envelope openable
cold) and then BUILD on the recovered tip (outer parent == recovered envelope, inner
parent == its inner block — the errInnerParentMismatch invariant from a cold boot).
Matrix: nothing-accepted / first-accept / longer run / copy-while-source-runs-on, plus
`OuterCommittedInnerNot`: the one crash window the accept path leaves open
(`acceptPostForkBlock` commits BEFORE the inner accept) must boot via the roll-back arm
and re-propose. Harness seams added in height_lag_repro_test.go (`testVMOnBase`,
`acceptRangeThroughProposervm`). **Negative control executed**: deleting the
`vm.db.Commit()` from `acceptPostForkBlock` fails every persistence-bearing scenario
(4 of 6) — the armor bites on flush-order regressions, not just on green paths.

### C-Chain ancient store

`--cchain-ancient` moves finalized C-Chain history into an append-only store on
a path of its own, so the node's key-value database carries only the blocks
above `--cchain-freeze-threshold`. `--cchain-ancient-shared` reads a store
another node on the same machine owns and writes, which is how many nodes share
one copy of the chain instead of keeping one each.

These are node flags, but the C-Chain reads its settings from its own config, so
they are merged into it — that is the one way anything reaches the plugin. The
store itself and what a shared reader is held to are documented in luxfi/geth.

## Upgrade compatibility

**P-Chain codec: v1.36.2 → v1.36.x is a flag day.** The P-Chain database moved
from linearcodec to ZAP-native encoding, so a v1.36.x node cannot read a database
a v1.36.2 node wrote. Upgrading across that boundary means clearing the node's
database and chain data and re-bootstrapping from peers; a node that is only
restarted on the new image refuses the database it finds rather than misreading
it. Plan the fleet so enough peers stay up to serve the ones being re-bootstrapped.

**Build order matters across the module bump.** `luxfi/evm` has to be built after
its `luxfi/vm` bump, or the C-Chain plugin it produces is linked against the
older module — the binary is not a function of the repository alone. See
"Build Order" below.

## Essential Commands

### Release & build (canonical) — via platform.hanzo.ai, NOT GitHub Actions
The ONE way to build + publish releases is **[`RELEASE.md`](./RELEASE.md)**:
platform.hanzo.ai reads [`hanzo.yml`](./hanzo.yml) on a `v*` tag push and
schedules the image build onto self-hosted **arcd** pools (`lux-build-linux-*`)
over the native long-poll fabric — no GitHub-Actions hop. ONE `Dockerfile`
build yields BOTH artifacts: the node image (`ghcr.io/luxfi/node:vX.Y.Z`, luxd
+ 12 baked VM plugins) and, via [`scripts/publish_plugin_set.sh`](./scripts/publish_plugin_set.sh),
the plugin set to `s3://lux-plugins-<env>/<pluginset>/` (operator `pluginSource`).
The `.github/workflows/*` build/release workflows are retired (RELEASE.md §Retire).

### Go toolchain policy
Two rules, both required, for every Dockerfile stage that compiles Go:

1. **Pin the base to the latest stable patch** (currently `1.26.5`) — never
   below the go.mod `go` directive.
2. **Set `ENV GOTOOLCHAIN=auto`** in that stage.

The official `golang` images ship `GOTOOLCHAIN=local`, which turns "base older
than the governing `go` directive" into a hard failure —
`go: go.mod requires go >= X (running go Y; GOTOOLCHAIN=local)` — rather than
fetching the needed toolchain. Rule 2 is what stops that class recurring; rule 1
is what keeps current compiler/stdlib security fixes in the image. A dependency
can floor above this module, so rule 2 applies even where the base tracks
go.mod exactly (`vms/example/xsvm`, fed by `go list -m -f {{.GoVersion}}`).

**The `go` directive stays at 1.26.4 — do not raise it casually.** node is a
dependency hub for much of the luxfi fleet plus external OSS consumers; raising
it pushes a hard patch-level floor onto all of them, and any consumer on
`GOTOOLCHAIN=local` then fails exactly as above. Building an older directive
with a newer toolchain is always valid, so pinning the image already gets the
newer compiler. Raise it only for a genuinely required language/stdlib feature.

`Dockerfile.bootnode` and `docker/Dockerfile.prebuilt` are exempt: they copy
prebuilt binaries and compile no Go.

### Building (local dev only)
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
All new native chains use Quasar (BLS + Corona + ML-DSA).

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
- **Quasar**: Production consensus -- BLS12-381 + Corona (lattice) + ML-DSA-65 (FIPS 204)
- **Chain Engine**: Linear blockchain consensus (Nova sub-protocol)
- **DAG Engine**: Directed acyclic graph for parallel processing (Nebula sub-protocol)
- **PQ Engine**: Post-quantum finality layer

Sub-protocols: Photon (sampling) -> Wave (voting) -> Focus (confidence) -> Ray/Field (finality)

### Virtual Machines
Located in `/vms/`:
- **platformvm**: Staking, validation, network management
- **xvm**: Asset transfers, UTXO model
- **dexvm**: DEX with order book, perpetuals, AMM
- **mpcvm**: Threshold MPC and FHE for confidential computing
- **quantumvm**: PQ consensus coordination (ML-DSA, Corona)
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
- `vms/mpcvm/fhe` - GPU FHE operations
- `x/blockdb` - zstd compression

### FHE (Fully Homomorphic Encryption)
Located in `vms/mpcvm/fhe/`:
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
ZAP is the only wire protocol for VM<->Node communication. The gRPC
fallback (and its `-tags=grpc` opt-in) was retired in v1.26.31 along
with every `//go:build grpc` file under `node/`. There is one and only
one way to talk to a Chain VM: ZAP.

**Build:**
```bash
go build                  # ZAP only — there are no build tags
```

**Key Packages:**
- `github.com/luxfi/api/zap` — Core wire protocol and message types (Layer A)
- `github.com/luxfi/protocol/rpcdb` — rpcdb service spec / data carriers (Layer B)
- `github.com/luxfi/node/db/rpcdb` — rpcdb Service + ZAP transport adapter (Layer C)
- `vms/rpcchainvm/sender/` — Node-side `p2p.Sender` over ZAP
- `vms/rpcchainvm/zap/` — ChainVM client/server over ZAP
- `vms/platformvm/warp/zwarp/` — Warp signing over ZAP

**rpcdb Layered Topology:**
- Layer A — wire framing: `github.com/luxfi/api/zap`
- Layer B — rpcdb service spec: `github.com/luxfi/protocol/rpcdb`
- Layer C — rpcdb impl: `node/db/rpcdb/{service.go, zap_server.go}`
  - `service.go` — transport-neutral `Service` wrapping `database.Database`
  - `zap_server.go` — ZAP transport adapter (only adapter)
- One Service, one transport. The dual-adapter pattern stays available
  for future transports (each is a new file wrapping `*Service`), but
  ZAP is the only one shipping.

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
// ZAP transport — the only transport
s := sender.ZAP(zapConn)
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

### SchemeGate — cross-axis NodeIDScheme enforcement

`network/peer/scheme_gate.go` (v1.26.10) is the single primitive that
turns a wire NodeID into a `(scheme, NodeID)` pair and runs the
cross-axis check against the chain's `ChainSecurityProfile`.

- `SchemeGate{Profile, ClassicalCompatUnsafe, ActivationHeight}` is the
  chain-scoped policy object. One gate per chain, pinned at bootstrap.
- `Classify(nodeID, scheme, height, site) (TypedNodeID, error)` is the
  single entry point. Callers pass a site tag (`"handshake"`,
  `"proposer"`, `"validator"`, `"mempool-sender"`) that appears in the
  refused-by error.
- Migration path: `ActivationHeight` is the block at which a strict-PQ
  chain refuses any non-PQ `NodeIDScheme` byte at every height under
  the forward-only PQ policy. The classical `secp256k1` (0x90) scheme is
  refused at the gate; there is no transition window and no operator
  classical-compat escape hatch (strict-PQ chains refuse classical at
  every boundary, period).
- Typed errors: `ErrSchemeGateConfig`, `ErrSchemeGateMismatch`,
  `ErrSchemeGateUnknownScheme`.

Wire form: `TypedNodeID = (NodeIDScheme byte, 20-byte NodeID)`. The
20-byte storage/map-key form stays byte-identical; the scheme byte
travels on the wire so a receiver knows which verifier to dispatch
without trusting the chain profile alone.

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
- `vms/mpcvm/fhe/gpu_fhe_nocgo.go`
- `vms/zkvm/accel/accel_mlx.go`

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
NODE_ENDPOINT="http://localhost:9640/v1/bc/C/rpc" \
PRIVATE_KEY="<funded_key>" \
./bin/bench tps --chains=lux --duration=60s --concurrency=5
```

## JSON rule — json/v2 at HTTP boundary only; ZAP for all internal data

Encoding boundaries are one-way and explicit:

- **External (HTTP / JSON-RPC API)** — `github.com/go-json-experiment/json` (v2).
  Never `encoding/json`. This covers: `service/*`, `server/*`, `pubsub/`,
  `vms/platformvm/service.go`, `vms/xvm/service.go`, JSON-RPC clients
  (`vms/platformvm/client_*`), CLI tools (`cmd/*`), wallet examples,
  on-disk config files (read once at boot), genesis/upgrade blobs.
- **Internal (state, P2P, consensus, MPC, logs, metrics)** — ZAP wire only.
  No JSON in: `network/`, `consensus/`, `snow/`, `chains/` (data-plane),
  `vms/*/state/`, `vms/*/block/`, `vms/*/txs/` (struct codec), threshold
  payloads, P2P message bodies, internal databases.

Migration helpers (v2 API delta vs v1):

| v1 (encoding/json)                | v2 (go-json-experiment/json)                 |
|-----------------------------------|----------------------------------------------|
| `json.Marshal(v)`                 | `json.Marshal(v)` (variadic opts; signature compat) |
| `json.MarshalIndent(v, "", "  ")` | `json.Marshal(v, jsontext.WithIndent("  "))` |
| `json.Unmarshal(b, &v)`           | `json.Unmarshal(b, &v)`                      |
| `json.NewEncoder(w).Encode(v)`    | `json.MarshalWrite(w, v)` (no trailing `\n`) |
| `json.NewDecoder(r).Decode(&v)`   | `json.UnmarshalRead(r, &v)`                  |
| `json.RawMessage`                 | `jsontext.Value`                             |
| `*json.SyntaxError`               | `*jsontext.SyntacticError`                   |

v2 semantic differences worth knowing (these change wire shape):

- `[N]byte` field with no `MarshalJSON` ⇒ v2 marshals as base64 string,
  v1 marshalled as JSON array of byte numbers. Add `MarshalJSON` on the
  type if the array form is wanted on the wire.
- `time.Duration` ⇒ v2 default is the standard string form ("30m");
  v1 marshalled as int nanoseconds. v1 sub-package
  (`github.com/go-json-experiment/json/v1`) exposes `FormatDurationAsNano(true)`;
  v2 root does not. Prefer the string form on new APIs.
- v2 enforces strict UTF-8; raw arbitrary bytes in JSON strings fail.
  This matters for legacy P2P/internal blobs that happen to be stored
  through JSON — those should already be on ZAP.
- `json.MarshalWrite` does NOT append a trailing `\n` (v1 `NewEncoder.Encode` did).
  Adjust HTTP-handler test fixtures accordingly.

---

## Housekeeping

Removed 6 generated write-ups / stale root artifacts (`LAUNCH_CHECKLIST.md`,
`rename_app.sh`, `replace_imports.sh`, `gen_zoo_addr` binary, `.ci-status-check.md`,
`.ci-trigger`) plus the 73MB `.claude/worktrees/` agent scratch tree. Release and
launch state live in this file, `CHANGELOG.md`, `RELEASE.md`; chain IDs/ports in
`~/work/lux/universe/NETWORKS.yaml` and `~/work/lux/genesis/configs/`.

---

*Last Updated*: 2026-06-06
