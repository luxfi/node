# AI Development Guide

This file provides guidance for AI assistants working with the Lux node codebase.

## Repository Overview

Lux blockchain node implementation - a high-performance, multi-chain blockchain platform written in Go. Features multiple consensus engines (Chain, DAG, PQ), EVM compatibility, and a multi-chain architecture with specialized capabilities.

**Key Context:**
- Original Lux Network node — NOT a fork
- Latest Tag: v1.26.31
- Network ID: 96369 (Lux Mainnet), 96368 (Testnet), 96370 (Devnet)
- Go Version: 1.26.1+
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

## v1.36.12 fleet rollout — durable rejoin fix + RewardManager→DAO Safe (IN PROGRESS 2026-07-15)

Rolling the durable rejoin fix (node `63f61429d1`) across all Lux nets, gated
devnet→testnet→mainnet, + activating RewardManager fees. Two hard facts were found
on the devnet canary that change the naive "swap image" plan:

**BLOCKER (fixed in v1.36.12): published `node:v1.36.11` (digest `c3cf92a6`) cannot
run ANY EVM chain.** Its baked VM plugins were built against a stale `luxfi/api`:
C-Chain EVM from `luxfi/evm@v1.104.8` and D-Chain dexvm from `luxfi/dex@v1.5.15`
both resolve `api v1.0.15`; the node pins `api v1.0.16`, which APPENDED
`InitializeResponse.Capabilities` (Quasar-export handshake, api `1f2dc5a`). Node
decodes the field, stale plugins never encode it → `vms/rpcchainvm/zap/client.go`
fails every EVM `Initialize` with `zap decode initialize response: unexpected EOF`.
Native VMs (P/X/Q) unaffected. **Fix = v1.36.12** (this repo, tag pushed, ARC docker
build run 29381442539): `EVM_VERSION v1.104.8→v1.104.9`, force `api@v1.0.16` in the
dexvm build stage; `CHAINS_REF=v1.7.6` was already v1.0.16. Node binary unchanged
(still the durable fix). api bump is code-free for plugins (`chains v1.7.4→v1.7.5`
adopted it go.mod-only). **Roll v1.36.12, NOT v1.36.11.** (Also: `api 1f2dc5a` added
`Capabilities` WITHOUT bumping `version.RPCChainVMProtocol` (42) → skew is invisible
at handshake, only fails at Initialize decode. Consider bumping the protocol next
api-wire change so skews fail fast.)

**MIGRATION: v1.36.2→v1.36.x is a P-Chain codec change (linearcodec→ZAP-native),
one-time DB wipe + re-bootstrap.** v1.36.11/12 cannot read a v1.36.2 P-Chain zapdb
(`loadMetadata: feeState: zap: invalid magic bytes`; `state_commit.go:116` "database
must be wiped"). Recovery = wipe `/data/db`+`/data/chainData`, re-bootstrap from
peers. **Cross-version bootstrap (v1.36.11 node ← v1.36.2 peers) is PROVEN working**
(devnet luxd-1: P/X re-bootstrapped from the 4 v1.36.2 peers). Devnet startup got a
marker-gated one-time wipe (`/data/.zap-native-migrated`): absent→wipe+set marker,
present→NO wipe (the durable-fix no-wipe restart path). mainnet/testnet/zoo use
`startup.sh` which already has `.wipe-cchain` (C-Chain only) + `.allow-bootstrap`
(flips skip-bootstrap=false + EVM state-sync); for the codec migration the P-Chain
zapdb (`/data/db`) must also be cleared. **Mainnet C-Chain is 1.08M blocks → MUST
enable EVM state-sync for the re-sync (full replay is too slow); native VMs are tiny.**

**Durable fix (`63f61429d1`):** discriminator for keeping the staked beacon set is
SYBIL-PROTECTION, not `--skip-bootstrap`. So a behind validator on a sybil-protected
net catches up from peers even with `--skip-bootstrap=true` (which prod hardcodes),
no wipe. Devnet added `--skip-bootstrap=true` to the inline cmd to exercise this.

**RewardManager (C-Chain precompile, per-net `cchain-upgrade.json` → append one
`precompileUpgrades` entry, dated `blockTimestamp`):** proven testnet shape is
`{"rewardManagerConfig":{"blockTimestamp":<ts>,"adminAddresses":["<admin>"],
"initialRewardConfig":{"rewardAddress":"<reward>"}}}`. Reward addr = coinbase; 100%
fees land there, blackhole `0x0100…00` goes flat. Addresses: **testnet ALREADY LIVE**
(reward `0xEAbCC110fAcBfebabC66Ad6f9E7B67288e720B59`, admin `0x9011…94714`); **mainnet**
reward+admin = DAO Gov Safe `0x8E29b816c6C35b13cE1ff68D33E245C2bda8ac3D`; **zoo**
reward `0x229599f227231d8C90fcF1a78589F5DC4b7A6962`; **devnet** reward
`0x8d5081153aE1cfb41f5c932fe0b6Beb7E159cF84` (idx2), admin `0x9011…94714` (idx0).
Source ConfigMaps: devnet `luxd-chain-upgrades/cchain-upgrade.json`; mainnet+testnet
`luxd-startup/cchain-upgrade.json`; zoo `zood-mv-genesis/upgrade.json` (`--upgrade-file`).

**Rollout levers (lux-operator is scaled 0/0 — sts/cm are the live source of truth;
CRs are STALE, do not scale operator up mid-roll):** ports devnet 9650 / testnet 9640
/ mainnet 9630 / zoo 9630; RPC path `/v1/bc/C/rpc`; container `luxd` (`zood` on zoo).
Devnet uses an inline generated cmd; testnet/mainnet/zoo use `/scripts/startup.sh`.
**Zoo `zood-mv` trap: RollingUpdate + hardcoded `--skip-bootstrap=true` with NO
`.allow-bootstrap` gate → switch to OnDelete BEFORE rolling.** Lux sts are OnDelete.
Master funded key = LUX_MNEMONIC (secret `lux-deployer`) idx0 `0x9011…94714`;
`genesis/cmd/derivekey -mnemonic "$M"` (path m/44'/9000'/0'/0/i, `CGO_ENABLED=0`).

**Per-node roll protocol (ALL nets, one at a time, NEVER 2 mainnet down — quorum
4/5):** set sts image v1.36.12 (+ rewardManager cm edit) → delete ONE pod → WAIT
until it is back at **TIP HEIGHT matching the others** (NOT pod-Ready; a wedged node
false-reports Ready) AND C-Chain serves RPC → only then the next. If any node fails
to return to tip, STOP that net and report.

**State at pause:** v1.36.12 tag pushed + ARC build dispatched (run 29381442539).
Devnet sts = v1.36.11 + skip-bootstrap + wipe-marker; luxd-1 migrated (P/X up on
v1.36.11, C-Chain down = the plugin bug → will heal on v1.36.12); luxd-0/2/3/4 still
v1.36.2 healthy (devnet C-Chain 4/5). Nothing rolled on testnet/mainnet/zoo. NEXT:
when v1.36.12 image is ready → set devnet sts image v1.36.12, delete luxd-1, confirm
C-Chain inits + reaches tip; then finish devnet (durable-fix proof + RewardManager),
then gated testnet→mainnet→zoo.

## v1.36.33 — the build→self-verify-fail→drop loop (devnet 96367 / testnet 96368, 2026-07-28)

**Symptom.** Every proposer built a block and then rejected the block it had just
built, forever: `built block … height=1047` immediately followed by
`built block failed verification — dropping error="inner parentID didn't match
expected parent"`, 83–456 drops/min per node, accepted tip frozen two heights BELOW
what the builder kept proposing (devnet tip 1045, builds 1047).

**Root cause (one line).** `buildChild` asked the inner VM for a block without first
pointing the inner VM at the parent's inner block. The inner VM builds on ITS OWN
head (`luxfi/evm` miner reads `bc.CurrentBlock()`); the verify path requires
`child.innerBlk.Parent() == parent.innerBlk.ID()`. Two different pointers, one
required equal to the other, nothing asserting it. The head drifts on its own:
verifying a GOSSIPED block whose parent is the current head optimistically sets the
head (`core/blockchain.go writeBlockAndSetHead → newTip → writeCanonicalBlockWithLogs`),
and `VM.SetPreference` short-circuits on an unchanged outer preference so it never
re-pushes the inner preference to drag the head back. Non-self-correcting by
construction.

**Fix.** `VM.anchorInnerBuildParent` (vms/proposervm/vm.go) — one inner
`SetPreference` at the point of use, called from BOTH build delegations
(`postForkCommonComponents.buildChild`, `preForkBlock.buildChild`). On a healthy node
the inner `setPreference` early-returns on `current.Hash() == block.Hash()`, so it is
a lookup and no state change; when it fails, the head is provably not the parent's
inner block, so refusing to build beats emitting a block we would drop.
Regression proof: `vms/proposervm/build_inner_parent_test.go` (models the three evm
head semantics — build-on-head, verify-advances-head, SetPreference-reorgs-head).

**NOT the cause, measured:** the P-Chain. `info.isBootstrapped{"chain":"P"}` = true on
15/15 nodes and `platform.getHeight` = 0 on 15/15 **including mainnet**, whose built
blocks also carry `pChainHeight=0`. `bootstrapped.message:["111…LpoYY"]` is the
primary-network NET id (`chains/chains.go Nets.Bootstrapping` keys by net), not the
P-Chain — the P-Chain's chain id prints `111…P`. Nothing in the verify path reads
pChainHeight before the parent check.

**Sibling failure modes on the same fleets (already fixed in 1.36.32, needs the roll):**
outer index BEHIND the inner tip ⇒ `refusing to build`/boot repair
(`height_backfill.go`, 7d2f01eb), and preferred-absent-locally ⇒ last-accepted
fallback (`vm.go BuildBlock`). All three are the same invariant seen from three sides.

**Roll surface — measured 2026-07-28T07:5xZ, do not use the LuxNetwork CR.**
`lux-operator` and `lux-operator-devnet` are **0/0** in `lux-system`, so nothing
reconciles `luxnetworks.lux.cloud/luxd`; its tags are stale garbage (mainnet
`v1.34.0`, testnet `v1.32.12` — v1.34.0 exists in no registry). The live image is on
the **StatefulSet**, hand-maintained: mainnet `ghcr.io/luxfi/node:v1.36.2`
(`kubectl-patch` 07-25T00:27:04Z), devnet `v1.36.25@sha256:ca497eff…`, testnet
`v1.36.24@sha256:91e2542b…`, all three `updateStrategy: OnDelete`. Change the image in
the universe manifest and apply, then delete one pod. Editing the CR and deleting a pod
reboots it on the OLD image.

**Mainnet 96369 is NOT a clean control — 3 of 5 nodes have Mode B, invisibly.**
luxd-0/3/4 `eth_blockNumber` = `0x10c1cf` (1098191, block ts 2026-07-24T15:46:19Z) and
have not moved in 3+ days while luxd-1 mines (1098341, tx status 0x1, 07:58Z); luxd-2
serves 404 (no C-Chain). Not a fork — 1098191 hashes identically on luxd-0 and luxd-1.
3998 of luxd-0's last 4000 log lines are the same `built block … height=1098196`. The
drop line is absent because the **binary** lacks it: `grep -c "built block failed
verification" /luxd/build/luxd` = **0** on mainnet v1.36.2, **1** on devnet v1.36.25.
`/v1/health` says `{"healthy":true,"error":"health reply encode failed"}` on the frozen
node and the mining node alike — never gate on it, use tip parity.
Two hazards for the roll: the 4 broken mainnet pods are exactly the ones on
ControllerRevision `luxd-596857c9d6` (rev 147, kubectl-patch 07-25: GOMEMLIMIT=6GiB,
mem limit 16Gi→12Gi, request 4Gi→7Gi) and the one mining node, luxd-1, is the only pod
still on rev 144 — **deleting luxd-1 recreates it on the revision every other node
broke on**. And `ghcr.io/luxfi/node:v1.36.2` has no git tag at all
(`git ls-remote origin refs/tags/v1.36.2` → empty; v1.36.24/25 → present), so what
mainnet runs is not reproducible from this repo.

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

### 1. P2P Sender Interface
Node's rpcchainvm implements `p2p.Sender` (from `github.com/luxfi/p2p`) for cross-chain messaging.
The `sender` package is the ZAP-native implementation of `p2p.Sender`.

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
External consumers (e.g. a white-label tenant's network-bootstrap tooling) need
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
- `vms/mpcvm/fhe/gpu_fhe_nocgo.go`
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
  http://localhost:9640/v1/bc/C/rpc
# Returns: {"jsonrpc":"2.0","id":1,"result":"0x17870"}
```

### 9. Root "/" Endpoint Handler
**Feature**: The node's root endpoint ("/") provides EVM compatibility and node information.

**Behavior**:
- **GET /**: Returns JSON node information (nodeId, networkId, version, chains, endpoints)
- **POST /**: Proxies JSON-RPC requests directly to C-chain `/v1/bc/C/rpc`
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
curl -s http://localhost:9650/v1/health | jq '.checks.bls'
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
