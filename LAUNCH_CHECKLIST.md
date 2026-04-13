# Lux Mainnet Launch Checklist

Node: luxfi/node v1.24.11
Consensus: Quasar (BLS+Corona, slashing, stake-weighted sampling)
EVM: GPU ecrecover, 18 precompiles
Genesis: networkID=1, startTime=2025-12-12T21:06:51Z (mainnet), networkID=2, startTime=2026-02-10T16:00:00Z (testnet)
Precompile constraint: all activations MUST be after 2025-12-25

---

## Infrastructure

| Environment | Cluster | DOKS ID | K8s Version | Namespace | Validators |
|-------------|---------|---------|-------------|-----------|------------|
| Testnet | do-sfo3-lux-test-k8s | `005ec3c4` | 1.35.1-do.0 | lux-testnet | 11 (target) |
| Rehearsal | do-sfo3-lux-dev-k8s | `0ff340e1` | 1.35.1-do.0 | lux-devnet | 21 (target) |
| Mainnet | do-sfo3-lux-k8s | `04c46df5` | 1.34.1-do.4 | lux-mainnet | 21 (target) |

Current state: lux-k8s runs 5 validators (v1.23.31) via LuxNetwork CRD. Testnet cluster (lux-test-k8s) has a 3-replica StatefulSet (v1.23.40).

Image: `ghcr.io/luxfi/node:v1.24.11` (built via CI/CD, linux/amd64+arm64)
Staking keys: KMS at `kms.lux.network`, project `lux-infra`, synced via KMSSecret CRD
Secrets: never in manifests, never in env files, never committed
Genesis configs: `/Users/z/work/lux/genesis/configs/{testnet,mainnet,devnet}/`
K8s manifests: `/Users/z/work/lux/universe/k8s/`
Profiles: `standard.json` (~100MB/node), `max.json` (~512MB/node)

Bootstrappers:
- Mainnet: 5 seeds (ports 9631) -- `209.38.118.46`, `209.38.174.69`, `24.144.69.101`, `134.199.187.56`, `143.198.246.173`
- Testnet: 2 seeds (ports 9641) -- `134.199.187.16`, `209.38.174.84`

Consensus parameters (mainnet): K=20, AlphaPreference=15, AlphaConfidence=15, Beta=20, ConcurrentPolls=4

Tokenomics: 10B total supply (9 decimals), min validator stake 1M LUX, min delegator stake 25K LUX, combined staking allowed (NFT+delegation), 80% uptime threshold

C-Chain: chainId=96369 (mainnet), 96368 (testnet), gasLimit=12M, targetBlockRate=2s, minBaseFee=25gwei

---

## Phase 1: Testnet (lux-test-k8s, networkID=2)

Target: validate all consensus, EVM, and staking behavior with K=11 validators.

### 1.1 Deployment

- [ ] Update LuxNetwork CRD in `universe/k8s/lux-k8s/validators/statefulset.yaml` (testnet section): `validators: 11`, `image.tag: v1.24.11`
- [ ] Update lux-test-k8s StatefulSet in `universe/k8s/lux-test-k8s/testnet/statefulset.yaml`: replicas=11, image=v1.24.11
- [ ] Generate 11 staking key pairs via `lux cli` and store in KMS (`lux-infra/testnet/staking/`)
- [ ] Add 9 new bootstrapper entries to `genesis/configs/testnet/bootstrappers.json` (currently 2)
- [ ] Apply `max.json` profile for testnet validators (512MB/node for stress testing headroom)
- [ ] Regenerate testnet genesis with 11 initial validators via `genesis` tool
- [ ] Verify all precompile activation timestamps are after 2025-12-25
- [ ] Deploy via PaaS (platform.hanzo.ai), not manual kubectl
- [ ] Verify all 11 pods reach Running state
- [ ] Verify all 11 nodes report healthy via `/ext/health/liveness`

### 1.2 Bootstrap and Connectivity

- [ ] Verify all 11 validators discover each other via P2P (check `info.peers` RPC, expect 10 peers per node)
- [ ] Verify staking port 9641 reachable between all pods (`luxd-{0..10}.luxd-headless.testnet.svc.cluster.local:9641`)
- [ ] Verify P-chain bootstraps and all validators appear in `platform.getCurrentValidators`
- [ ] Verify C-chain bootstraps and produces blocks
- [ ] Verify X-chain bootstraps and processes UTXO transactions

### 1.3 Quasar Consensus Verification

- [ ] Submit transactions, verify Quasar finalization with K=11
- [ ] Verify BLS aggregate signatures in block headers
- [ ] Verify Corona optimistic fast path activates when all 11 validators are online
- [ ] Measure finality latency (target: sub-second with Corona)
- [ ] Verify stake-weighted sampling: validators with more stake get polled proportionally

### 1.4 EVM Execution

- [ ] Deploy a test contract, call all standard opcodes
- [ ] Submit 100 sequential transactions, verify correct nonce ordering
- [ ] Verify `eth_call` and `eth_estimateGas` return correct results
- [ ] Verify block gas limit is 12M (from cchain.json config)
- [ ] Verify minBaseFee=25gwei is enforced

### 1.5 GPU ecrecover

- [ ] Verify GPU backend auto-detection: CUDA on Linux DOKS nodes, Metal on macOS
- [ ] Run ecrecover-heavy workload (1000 signature verifications per block)
- [ ] Compare ecrecover throughput: GPU vs CPU fallback
- [ ] Verify graceful fallback to CPU when GPU unavailable (set `--gpu-backend=cpu`)

### 1.6 Precompiles (all 18)

- [ ] Test each precompile individually via contract calls
- [ ] Verify DEX precompile (LP-9010 PoolManager): pool creation, swaps, flash loans
- [ ] Verify DEX router precompile (LP-9012): multi-hop routing
- [ ] Verify all precompile addresses are deterministic and match spec
- [ ] Verify precompile gas metering is correct (no underpriced or overpriced ops)
- [ ] Verify precompiles revert correctly on invalid input

### 1.7 Slashing

- [ ] Craft equivocation evidence: have a validator sign two different blocks at same height
- [ ] Submit equivocation proof to P-chain slashing precompile
- [ ] Verify slashed validator's stake is burned
- [ ] Verify slashed validator is removed from active set
- [ ] Verify honest validators are unaffected

### 1.8 Uptime and Rewards

- [ ] Stop 1 validator (scale pod to 0)
- [ ] Wait for reward period to elapse
- [ ] Verify stopped validator's uptime drops below 80%
- [ ] Verify rewards are withheld for the stopped validator
- [ ] Restart the validator, verify it re-bootstraps and resumes
- [ ] Verify validators with >80% uptime receive expected rewards

### 1.9 Stress Test

- [ ] Run stress test: maximum TPS with 1B gas blocks (increase gas limit temporarily)
- [ ] Measure sustained TPS over 1 hour (target: verify consensus is the bottleneck, not EVM)
- [ ] Monitor memory usage per node (should stay within `max.json` profile ~512MB)
- [ ] Monitor disk I/O and database growth rate
- [ ] Verify no consensus stalls under load
- [ ] Verify block production rate stays at targetBlockRate=2s

### 1.10 Validator Join/Leave

- [ ] Add a 12th validator via `platform.addPermissionlessValidator` (permissionless staking)
- [ ] Verify new validator bootstraps from existing state
- [ ] Verify new validator begins participating in consensus
- [ ] Remove a validator via unstaking (wait for stake period to end or use testnet short periods)
- [ ] Verify removed validator exits gracefully
- [ ] Verify remaining validators continue producing blocks

### 1.11 Formal Verification

- [ ] Run Lean proofs for Quasar consensus safety and liveness
- [ ] Run TLA+ model checker for consensus state machine
- [ ] Run Tamarin prover for BLS+Corona security properties
- [ ] Run Halmos for EVM precompile correctness (symbolic execution)
- [ ] All proofs pass with zero counterexamples

---

## Phase 2: Mainnet Rehearsal (lux-dev-k8s, networkID=3)

Target: full mainnet simulation with real parameters for 72 hours.

### 2.1 Deployment

- [ ] Update LuxNetwork CRD (devnet section): `validators: 21`, `image.tag: v1.24.11`
- [ ] Generate 21 staking key pairs, store in KMS (`lux-infra/devnet/staking/`)
- [ ] Use mainnet genesis parameters (networkID=3, but same tokenomics, same stake amounts)
- [ ] Apply `max.json` profile
- [ ] Deploy via PaaS
- [ ] Verify all 21 pods healthy

### 2.2 Real Staking Parameters

- [ ] Configure minimum validator stake: 1M LUX
- [ ] Configure minimum delegator stake: 25K LUX
- [ ] Configure max delegation ratio: 10x
- [ ] Configure NFT staking tiers (Genesis 500K/2x, Pioneer 750K/1.5x, Standard 1M/1x)
- [ ] Verify combined staking logic: NFT value + delegation + staked >= 1M
- [ ] Verify B-chain validators require 100M LUX + KYC

### 2.3 72-Hour Soak Test

- [ ] Start clock. Record block height and timestamp.
- [ ] Continuous transaction load: 50 TPS sustained
- [ ] Monitor: CPU, memory, disk, network per node (Prometheus + Grafana via PaaS)
- [ ] Monitor: consensus latency p50/p95/p99
- [ ] Monitor: block production rate (target: 1 block per 2s)
- [ ] Monitor: peer count stability (all 21 connected)
- [ ] Monitor: no OOMKills, no pod restarts, no crashloops
- [ ] At hour 24: rolling restart of 5 validators (verify zero downtime)
- [ ] At hour 48: simulate network partition (isolate 7 nodes), verify chain halts (< 2/3 online)
- [ ] Restore partition, verify chain resumes within 30s
- [ ] At hour 72: record final block height, calculate actual vs expected blocks
- [ ] Pass criteria: zero consensus faults, zero data loss, <1% block time variance

### 2.4 Security Audit

- [ ] External security audit firm engaged (Red team)
- [ ] Audit scope: consensus, EVM, precompiles, staking, slashing, P2P networking
- [ ] Audit result: 0 critical findings, 0 high findings
- [ ] All medium findings remediated or accepted with documented risk
- [ ] Audit report signed and archived

### 2.5 Bridge / Teleport (B-Chain + T-Chain)

- [ ] Deploy MPC threshold signing (5 nodes, threshold 3) in `lux-mpc` namespace
- [ ] Deploy bridge UI and API in `lux-bridge` namespace
- [ ] Verify CGGMP21 keygen: 5 parties generate shared key
- [ ] Verify threshold signing: 3-of-5 produces valid signature
- [ ] Test cross-chain transfer: lock on source chain, mint on Lux
- [ ] Test reverse: burn on Lux, unlock on source chain
- [ ] Verify MPC API at `mpc-api.lux.network` responds
- [ ] Verify bridge handles partial MPC node failure (2 down, 3 still sign)

### 2.6 DEX (D-Chain + Precompiles)

- [ ] Deploy DEX precompile PoolManager (LP-9010) -- already active from genesis
- [ ] Deploy DEX Router precompile (LP-9012) -- already active from genesis
- [ ] Deploy off-chain CLOB matching engine
- [ ] Create liquidity pool via precompile
- [ ] Execute swap via router precompile
- [ ] Verify AMM pricing matches expected curve
- [ ] Verify flash loan execution and repayment
- [ ] Test CLOB: place limit order, verify fill
- [ ] Verify DEX on lux.exchange frontend connects to devnet

---

## Phase 3: Mainnet Launch (lux-k8s, networkID=1)

Target: production network with real value.

### 3.1 Pre-launch

- [ ] All Phase 1 items passed
- [ ] All Phase 2 items passed
- [ ] Security audit sign-off received
- [ ] Formal verification suite green
- [ ] Legal review complete (terms of service, validator agreements)
- [ ] Incident response runbook written and tested

### 3.2 Genesis Ceremony

- [ ] Final genesis config reviewed: `genesis/configs/mainnet/genesis.json` (networkID=1)
- [ ] Genesis startTime confirmed: 2025-12-12T21:06:51Z
- [ ] Initial allocations verified (500M initial + unlock schedule)
- [ ] All 5 bootstrapper IPs confirmed reachable on port 9631
- [ ] Genesis hash computed and published to lux.network
- [ ] Genesis block signed by founding validators

### 3.3 Validator Onboarding

- [ ] Update LuxNetwork CRD (mainnet section): `validators: 21`, `image.tag: v1.24.11`
- [ ] Scale from 5 current validators to 21
- [ ] Generate 16 new staking key pairs in KMS (`lux-infra/mainnet/staking/`)
- [ ] Update bootstrappers.json with all 21 validator endpoints
- [ ] Deploy via PaaS with rolling update strategy
- [ ] Verify all 21 validators healthy and in consensus
- [ ] Publish validator onboarding guide for external operators
- [ ] Open permissionless staking after initial stabilization period

### 3.4 Public RPC Endpoints

- [ ] Deploy KrakenD API gateway in `lux-gateway` namespace
- [ ] Configure rate limiting per IP and per API key
- [ ] Configure Cloudflare DNS (proxied, full SSL):
  - `api.lux.network` -> gateway (C-chain + P-chain + X-chain RPC)
  - `ws.lux.network` -> gateway (WebSocket subscriptions)
- [ ] Verify `eth_chainId` returns `0x17871` (96369)
- [ ] Verify `net_version` returns `96369`
- [ ] Verify RPC endpoints handle 10K req/s without degradation
- [ ] Verify WebSocket subscriptions for `newHeads`, `logs`, `pendingTransactions`

### 3.5 Explorer Deployment

- [ ] Deploy explorer (luxfi/explorer) in `lux-explorer` namespace (already has manifests for 5 chains)
- [ ] Configure for C-chain (chainId 96369)
- [ ] Configure indexers for all active chains
- [ ] Configure Cloudflare DNS: `explore.lux.network`
- [ ] Verify block display, transaction search, contract verification
- [ ] Deploy exchange frontend: `lux.exchange`

### 3.6 Bridge Activation

- [ ] Deploy MPC production cluster (5 nodes, threshold 3)
- [ ] Generate production MPC keys (CGGMP21 keygen ceremony)
- [ ] Store MPC key shares in KMS (`lux-infra/mainnet/mpc/`)
- [ ] Deploy bridge contracts on supported chains (ETH, BNB, Polygon, Arbitrum, Base, Optimism)
- [ ] Deploy bridge UI at bridge domain
- [ ] Configure Cloudflare DNS
- [ ] Enable deposits (one chain at a time, small limits first)
- [ ] Monitor for 24h, then raise limits

### 3.7 Post-Launch Monitoring

- [ ] Prometheus + Grafana dashboards live (via PaaS o11y stack)
- [ ] Alerts configured:
  - Validator down (any pod not Ready for >5min)
  - Consensus stall (no new block for >30s)
  - Peer count drop (any node <15 peers)
  - Memory usage >80% of limit
  - Disk usage >70%
  - Error rate >1% on RPC endpoints
- [ ] On-call rotation established
- [ ] Runbook covers: validator restart, chain halt recovery, emergency upgrade, key rotation

---

## Port Reference

| Network | HTTP | Staking | Metrics |
|---------|------|---------|---------|
| Mainnet | 9630 | 9631 | 9090 |
| Testnet | 9640 | 9641 | 9090 |
| Devnet | 9650 | 9651 | 9090 |

## Chain IDs

| Chain | Mainnet | Testnet | Devnet |
|-------|---------|---------|--------|
| C-Chain | 96369 | 96368 | 96370 |
| Zoo EVM | 200200 | 200201 | 200202 |
| Hanzo EVM | 36963 | 36964 | 36964 |
| SPC EVM | 36911 | 36910 | 36912 |
| Pars EVM | 494949 | 7071 | 494951 |

## File References

| What | Path |
|------|------|
| Node source | `~/work/lux/node/` |
| Genesis configs | `~/work/lux/genesis/configs/{mainnet,testnet,devnet}/` |
| Chain configs | `~/work/lux/genesis/configs/chain-configs/` |
| K8s manifests | `~/work/lux/universe/k8s/` |
| Validator CRD | `~/work/lux/universe/k8s/lux-k8s/validators/statefulset.yaml` |
| Testnet StatefulSet | `~/work/lux/universe/k8s/lux-test-k8s/testnet/statefulset.yaml` |
| Node profiles | `~/work/lux/node/config/profiles/{standard,max}.json` |
| Tokenomics config | `~/work/lux/node/config/tokenomics.go` |
| GPU config | `~/work/lux/node/config/gpu.go` |
| Health/consensus params | `~/work/lux/node/config/health.go` |
| Network registry | `~/work/lux/universe/NETWORKS.yaml` |
