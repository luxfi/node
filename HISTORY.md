# Lux Network -- Development Timeline

> Comprehensive history of development across Hanzo AI, Lux Network, and Zoo Labs Foundation.
> All dates sourced from `git log` across 445 repositories. Fork provenance noted where applicable.

**Generated**: 2026-04-07 from live git history

---

## Summary

| Metric | Count |
|--------|-------|
| Total repositories | 445 (Hanzo 209, Lux 179, Zoo 57) |
| Total commits | 1,103,364 (Hanzo 852,218 / Lux 175,459 / Zoo 75,687) |
| Research papers (LaTeX) | 329 (Hanzo 152, Lux 136, Zoo 41) |
| Formal proofs (Lean4) | 13,160 files (Lux 6,851 / Hanzo 6,309) |
| TLA+ specifications | 4 |
| Tamarin protocol proofs | 2 |
| Halmos symbolic tests | 10 |
| Security audits | 23 reports |
| Governance proposals | 1,735 (LIPs 848, HIPs 784, ZIPs 103) |
| Patent applications | 2 portfolios (Hanzo, Zoo) |
| Years of continuous development | 12 (2014--2026) |

### Key Technologies (Original Work)

- **Quasar Consensus** -- Multi-metric BFT with FPC, Wave protocol, pipelined block production
- **Ringtail** -- Post-quantum signature scheme (ML-DSA + FROST hybrid)
- **LuxFHE** -- Fully homomorphic encryption engine with Go bindings, NTT SIMD acceleration
- **Lattice Cryptography** -- ML-KEM (FIPS 203), constant-time CBD sampler, CKKS/BFV schemes
- **MPC Engine** -- CGGMP21 + FROST threshold signing, WebAuthn integration
- **Jin Architecture** -- Multimodal AI (saccade JEPA, vision-language-audio)
- **Zen Model Family** -- Qwen3+ fine-tuning, refusal removal, agentic datasets
- **Hanzo Candle** -- Rust ML inference framework
- **GPU EVM** -- CUDA-accelerated opcode dispatch, GPU ecrecover, GPU state hashing
- **FHE Coprocessor** -- Encrypted smart contract execution

---

## Founder

Zach Kelling (zeekay) -- computer scientist, cryptographer, AI/ML researcher, musician, composer, architect, engineer, mathematician.

- **1983**: Born
- **1998**: Enrolled in university for Computer Science at age 15
- **Early 2000s**: Digidesign (Pro Tools) -- audio engineering, DSP, signal processing. Music composition and production.
- **2000s--2010s**: Software engineering across distributed systems, infrastructure, and early machine learning. Artist, writer, composer, architect, mathematician.
- **2008**: First open source contributions
- **2011**: GitHub activity begins (github.com/zeekay) -- Python, Vim, shell frameworks, distributed systems
- **2014**: Open-source AI/ML and commerce tooling -- the precursor work to Hanzo AI

Today: **1,239+ public repositories** across github.com/zeekay (547), github.com/hanzoai (366), github.com/luxfi (305), and additional orgs. 15+ years of continuous open source contribution.

Everything built has been open source, permissively licensed, and given to the public for free. This is not a commercial play -- it is a contribution to humanity's infrastructure.

---

## 2014--2016: Foundations

Early open-source work in commerce, automation, infrastructure, and AI/ML tooling. These repositories represent the precursor work to Hanzo AI.

### Original Hanzo Repositories

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `hanzo/autogui` | 2014-07-17 | GUI automation framework |
| `hanzo/classic` | 2014-09-29 | E-commerce platform (original, 7,395 commits) |
| `hanzo/commerce` | 2014-09-29 | Commerce engine (original, 7,636 commits) |
| `hanzo/s3-cli` | 2015-01-14 | S3-compatible object storage CLI |
| `hanzo/openapi` | 2016-01-14 | API specification and documentation |
| `hanzo/tasks` | 2016-10-24 | Distributed task execution engine |

### Forked Infrastructure (upstream dates precede Hanzo)

These repositories were forked from established open-source projects. The earliest commit dates reflect upstream history, not Hanzo origination.

| Repository | Upstream | Upstream First Commit |
|------------|----------|----------------------|
| `hanzo/postgres` / `hanzo/sql` | postgres/postgres | 1996-07-09 |
| `hanzo/datastore` | ClickHouse/ClickHouse | 2008-12-01 |
| `hanzo/kv` | valkey-io/valkey | 2009-03-22 |
| `hanzo/redis` | redis/redis | 2009-03-22 |
| `hanzo/kv-go` | redis/go-redis | 2012-07-25 |
| `hanzo/pubsub-go` | nats-io/nats.go | 2012-08-15 |
| `hanzo/pubsub` | nats-io/nats-server | 2012-10-29 |
| `hanzo/storage` | minio/minio | 2014-10-30 |
| `hanzo/ingress` | (custom proxy, original) | 2015-08-28 |
| `hanzo/dns` | coredns/coredns | 2016-03-18 |
| `hanzo/golang-migrate` | golang-migrate/migrate | 2014-08-11 |
| `hanzo/dbx` | pocketbase/dbx | 2015-12-10 |

### Lux Precursor Forks

| Repository | Upstream | Upstream First Commit | Notes |
|------------|----------|----------------------|-------|
| `lux/coreth` / `lux/geth` | go-ethereum | 2013-12-26 | EVM fork, Lux-specific work begins ~2022 |
| `lux/libp2p` | libp2p/go-libp2p | 2014-08-28 | P2P networking library (archived -- replaced by ZAP) |
| `lux/czmq` | (ZeroMQ C bindings) | 2014-09-05 | Messaging infrastructure |

### Commit Activity

| Year | Hanzo | Lux | Zoo |
|------|-------|-----|-----|
| 2014 | 14,547 | 5,405 | -- |
| 2015 | 23,098 | 9,028 | -- |
| 2016 | 17,989 | 2,681 | -- |

---

## 2017--2018: Hanzo AI Founded (Techstars '17)

Hanzo AI is accepted into Techstars 2017. Focus on AI-powered commerce, analytics, and infrastructure services.

### New Hanzo Repositories

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `hanzo/datastore-go` | 2017-01-11 | Go client for analytics datastore |
| `hanzo/documentdb-go` | 2017-01-25 | Document database Go driver |
| `hanzo/docker` | 2017-07-18 | Container orchestration configs |
| `hanzo/krakend` | 2017-12-03 | API gateway (KrakenD-based) |
| `hanzo/search` | 2018-04-22 | Search engine (13,728 commits) |
| `hanzo/telemetry` | 2018-06-05 | Observability platform (8,002 commits) |
| `hanzo/rrweb` | 2018-09-30 | Session recording/replay |

### Lux Precursor Work

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `lux/zapdb` | 2017-01-26 | Key-value store (fork of Badger) |
| `lux/hid` | 2017-02-17 | Hardware device interface |
| `lux/onnx` | 2017-09-06 | Open Neural Network Exchange |
| `lux/safe` | 2017-09-27 | Multisig wallet (fork of Gnosis Safe) |
| `lux/explorer` | 2018-01-16 | Block explorer (replaced by luxfi/explorer) |
| `lux/zmq` | 2018-04-13 | ZeroMQ Go bindings |

### Zoo Precursor

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `zoo/explorer` | 2018-01-16 | Block explorer (shared with Lux) |

### Commit Activity

| Year | Hanzo | Lux | Zoo |
|------|-------|-----|-----|
| 2017 | 20,219 | 3,854 | -- |
| 2018 | 24,508 | 7,427 | 3,009 |

---

## 2019--2020: Lux Network Founded

Lux Network development begins in late 2019. Core blockchain node (`luxd`) launches March 2020. JavaScript SDK, wallet, and DeFi primitives follow.

### Lux Core Chain

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `lux/assets` | 2019-08-09 | Token asset registry |
| `lux/lattice` | 2019-08-12 | Lattice-based cryptography (CKKS, BFV, BGV schemes) |
| `lux/cex` | 2019-08-16 | Exchange frontend |
| `lux/exchange-sdk` | 2019-11-08 | Exchange SDK |
| `lux/js` | 2020-01-21 | JavaScript SDK (initial pre-release) |
| `lux/node` | 2020-03-10 | Core blockchain node -- 11,623 commits |
| `lux/trace` | 2020-03-10 | Transaction tracing |
| `lux/wwallet` | 2020-07-21 | Web wallet |
| `lux/build` | 2020-11-04 | Build and release tooling |

### Hanzo Infrastructure Expansion

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `hanzo/telemetry-go` | 2019-05-16 | Go telemetry client |
| `hanzo/search-go` | 2019-12-08 | Go search client |
| `hanzo/insights` | 2020-01-23 | Product analytics (35,710 commits) |
| `hanzo/posthog-python` | 2020-02-09 | Python analytics SDK |
| `hanzo/insights-node` | 2020-02-19 | Node.js analytics SDK |
| `hanzo/insights-go` | 2020-02-27 | Go analytics SDK |
| `hanzo/storage-console` | 2020-04-01 | Object storage management UI |
| `hanzo/vector` | 2020-05-30 | Log aggregation pipeline |
| `hanzo/analytics` | 2020-07-17 | Analytics engine (5,662 commits) |
| `hanzo/ingress-parser` | 2020-08-15 | Ingress log parser |
| `hanzo/livekit` | 2020-09-29 | Real-time audio/video |
| `hanzo/iam` | 2020-10-20 | Identity and access management (3,746 commits) |

### Commit Activity

| Year | Hanzo | Lux | Zoo |
|------|-------|-----|-----|
| 2019 | 30,747 | 10,229 | 4,467 |
| 2020 | 46,845 | 18,333 | 1,151 |

---

## 2021--2022: Expanding the Stack

Wallet, CLI, EVM, DeFi, threshold cryptography, and the first key management systems.

### Lux Ecosystem Growth

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `lux/threshold` | 2021-02-16 | Threshold ECDSA (tECDSA) library |
| `lux/erc20-go` | 2021-05-21 | ERC-20 Go bindings |
| `lux/netrunner` | 2021-10-22 | Network testing framework (2,384 commits) |
| `lux/evm` | 2021-12-15 | Subnet EVM (1,632 commits) |
| `lux/devops` / `lux/lux-ops` | 2022-01-28 | Infrastructure automation |
| `lux/ledger` | 2022-03-14 | Ledger hardware wallet integration |
| `lux/lpm` | 2022-03-28 | Lux Plugin Manager |
| `lux/plugins-core` | 2022-03-29 | Core VM plugins |
| `lux/standard` | 2022-04-19 | Token standards |
| `lux/cli` | 2022-04-23 | Command-line interface (2,153 commits) |
| `lux/faucet` | 2022-05-12 | Testnet faucet |
| `lux/netrunner-sdk` | 2022-05-13 | Network runner SDK |
| `lux/explorer-rs` | 2022-05-20 | Rust block explorer |
| `lux/market` / `lux/marketplace` | 2022-05-31 | NFT marketplace |
| `lux/explore` | 2022-05-31 | Block explorer frontend |
| `lux/monitoring` | 2022-06-02 | Network monitoring |
| `lux/finance` | 2022-08-04 | DeFi protocols |
| `lux/teleport` | 2022-09-13 | Cross-chain teleport bridge |
| `lux/kms` | 2022-11-17 | Key management system (14,395 commits) |

### Hanzo Platform Build-Out

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `hanzo/o11y` | 2021-01-03 | Observability stack |
| `hanzo/sql-vector` | 2021-04-20 | Vector search in PostgreSQL |
| `hanzo/insights-rs` | 2021-04-27 | Rust analytics SDK |
| `hanzo/treasury` | 2021-06-11 | Treasury management |
| `hanzo/team` | 2021-08-02 | Team management |
| `hanzo/docdb` | 2021-10-31 | Document database (FerretDB-based) |
| `hanzo/cloud` | 2022-03-31 | Cloud platform |
| `hanzo/faucet` | 2022-05-12 | Token faucet |
| `hanzo/mds` | 2022-05-17 | Metadata service |
| `hanzo/otel-collector` | 2022-06-11 | OpenTelemetry collector |
| `hanzo/vector-go` | 2022-06-24 | Go vector client |
| `hanzo/base` | 2022-07-07 | Application backend framework (2,287 commits) |
| `hanzo/evm` | 2022-09-19 | EVM utilities |
| `hanzo/chat` | 2022-10-20 | Real-time chat |
| `hanzo/sign` | 2022-11-14 | Document e-signing |
| `hanzo/payments` | 2022-11-16 | Payment processing |
| `hanzo/kms` | 2022-11-17 | Secret management (19,820 commits) |

### Zoo Ecosystem Begins

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `zoo/solidity` | 2021-01-09 | Smart contract library |
| `zoo/hardhat` | 2021-02-15 | Development framework |
| `zoo/node` | 2021-06-15 | Zoo blockchain node |
| `zoo/zoo-test` / `zoo/zoo-v4` / `zoo/zoo2` / `zoo/zoo3` | 2021-07-10 | Iterative protocol versions |
| `zoo/zoogov-app` | 2022-03-03 | Governance application |
| `zoo/zdk` | 2022-03-22 | Zoo Development Kit |
| `zoo/explorer-app` | 2022-05-31 | Explorer frontend |
| `zoo/CGI_Animation` | 2022-12-15 | AI-generated media |

### Commit Activity

| Year | Hanzo | Lux | Zoo |
|------|-------|-----|-----|
| 2021 | 58,199 | 27,811 | 8,923 |
| 2022 | 59,535 | 20,333 | 10,029 |

---

## 2023: Post-Quantum + MPC + AI Agents

Major cryptographic research: threshold signing, MPC engines, lattice crypto. AI work accelerates with Jin architecture, ML frameworks, and computer-use agents.

### Lux Cryptography and Protocol

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `lux/sdk` | 2023-02-19 | Unified SDK (225 commits) |
| `lux/markets` | 2023-06-24 | DeFi market infrastructure |
| `lux/web` | 2023-10-13 | Lux Network website |
| `lux/wallet` | 2023-10-16 | Production wallet (1,203 commits) |
| `lux/mpc` | 2023-11-03 | MPC engine -- CGGMP21 + FROST (388 commits) |
| `lux/audits` | 2023-12-28 | Security audit reports (23 reports) |
| `lux/bridge` | 2023-12-30 | Cross-chain bridge (1,919 commits) |

### Hanzo AI Systems

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `hanzo/ui` | 2023-01-24 | Shared UI component library (1,114 commits) |
| `hanzo/flow` | 2023-02-08 | AI workflow orchestration (17,390 commits) |
| `hanzo/cli` | 2023-04-03 | Developer CLI (10,596 commits) |
| `hanzo/jin` | 2023-05-15 | Multimodal AI -- saccade JEPA architecture |
| `hanzo/console` | 2023-05-18 | Admin console |
| `hanzo/dataroom` | 2023-05-27 | Secure document sharing |
| `hanzo/ml` | 2023-06-19 | Rust ML framework -- Candle (2,619 commits) |
| `hanzo/node` | 2023-06-25 | Distributed compute node (11,711 commits) |
| `hanzo/docs` | 2023-07-03 | Documentation platform |
| `hanzo/visor` / `hanzo/vm` | 2023-07-30 | Virtual machine runtime |
| `hanzo/desktop` | 2023-08-30 | Desktop application |
| `hanzo/cua` | 2023-11-03 | Computer-Use Agent (649 commits) |

### Zoo DeSci / DeAI

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `zoo/ui` | 2023-01-24 | Shared UI library |
| `zoo/zones` | 2023-02-18 | Zone management |
| `zoo/gym-v1` | 2023-04-13 | AI training gym v1 |
| `zoo/foundation` | 2023-05-08 | Zoo Labs Foundation website |
| `zoo/zooai` | 2023-05-19 | Zoo AI platform |
| `zoo/gym` | 2023-05-28 | AI training gym |
| `zoo/agent` | 2023-06-25 | AI agent framework |
| `zoo/app` | 2023-08-30 | Zoo application |

### Commit Activity

| Year | Hanzo | Lux | Zoo |
|------|-------|-----|-----|
| 2023 | 99,672 | 24,417 | 13,855 |

---

## 2024: BFT Consensus + Hardware Wallets + Compute

Byzantine fault tolerance research, hardware signing, Ringtail post-quantum signatures, and AI model refinement.

### Lux Advanced Protocol

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `lux/chat` | 2024-04-06 | Network communication |
| `lux/kms-go` | 2024-06-05 | KMS Go SDK |
| `lux/liquid` | 2024-06-18 | Liquid staking |
| `lux/ringtail` | 2024-07-08 | Post-quantum signature scheme (30 commits) |
| `lux/xwallet` | 2024-07-09 | Extended wallet |
| `lux/bank` | 2024-07-09 | Banking integration |
| `lux/tokens` | 2024-07-15 | Token management |
| `lux/uni-v4-subgraph` | 2024-07-23 | Uniswap V4 subgraph |
| `lux/dwallet` | 2024-07-31 | Decentralized wallet |
| `lux/kit` | 2024-08-07 | Development toolkit |
| `lux/bft` | 2024-08-28 | BFT consensus research (140 commits) |

### Hanzo AI Platform

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `hanzo/captable` | 2024-01-08 | Cap table management |
| `hanzo/runtime` | 2024-02-06 | ML inference runtime |
| `hanzo/sentry` | 2024-02-15 | Error monitoring |
| `hanzo/engine` | 2024-02-26 | Standalone AI inference engine (3,258 commits) |
| `hanzo/enso` | 2024-03-28 | Code generation |
| `hanzo/paas` / `hanzo/platform` | 2024-04-19 | Platform-as-a-Service |
| `hanzo/web` | 2024-04-29 | Web framework |
| `hanzo/remove-refusals` | 2024-05-16 | Model uncensoring -- permanent weight modification |
| `hanzo/kms-go-sdk` | 2024-06-05 | KMS Go SDK |
| `hanzo/studio-desktop` | 2024-08-12 | AI Studio desktop app |
| `hanzo/capnp-es` | 2024-08-16 | Cap'n Proto TypeScript bindings |
| `hanzo/kms-python-sdk` | 2024-08-19 | KMS Python SDK |
| `hanzo/kms-node-sdk` | 2024-08-29 | KMS Node.js SDK |

### Zoo Growth

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `zoo/game` | 2024-08-06 | AI gaming platform |
| `zoo/tools` | 2024-12-17 | Developer tooling |

### Commit Activity

| Year | Hanzo | Lux | Zoo |
|------|-------|-----|-----|
| 2024 | 141,053 | 20,987 | 21,139 |

---

## 2025: FHE + Formal Verification + Production Hardening

Fully homomorphic encryption, NTT SIMD acceleration, formal proofs in Lean4/TLA+/Tamarin, 23 security audits, and the full agent SDK stack.

### Lux Cryptography and Consensus

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `lux/safe-frost` | 2025-04-11 | On-chain FROST signature verification (37 commits) |
| `lux/consensus` | 2025-07-28 | Quasar consensus -- multi-metric BFT (504 commits) |
| `lux/crypto` | 2025-07-25 | Unified crypto library -- BLS, ML-DSA, ML-KEM, secp256k1 |
| `lux/database` | 2025-07-25 | Database abstraction layer |
| `lux/ids` | 2025-07-25 | Identity and addressing |
| `lux/warp` | 2025-07-24 | Warp cross-chain messaging |
| `lux/go-bip32` / `lux/go-bip39` | 2025-07-25 | HD wallet key derivation |
| `lux/p2p` | 2025-12-04 | Peer-to-peer networking |
| `lux/cache` | 2025-12-04 | Caching layer |
| `lux/vm` | 2025-12-19 | Virtual machine framework |
| `lux/fhe` | 2025-12-28 | Fully homomorphic encryption engine (94 commits) |
| `lux/proofs` / `lux/formal` | 2025-12-25 | Formal verification: 6,851 Lean4 files, 4 TLA+ specs, 2 Tamarin proofs |
| `lux/papers` | 2025-10-28 | 136 research papers (LaTeX) |
| `lux/lips` / `lux/lps` | 2025-07-22 | Lux Improvement Proposals (848 proposals) |

### Lux Infrastructure Modules (extracted from monolith)

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `lux/timer` | 2025-12-04 | Timing utilities |
| `lux/constants` | 2025-12-04 | Network constants |
| `lux/codec` | 2025-12-04 | Serialization codec |
| `lux/upgrade` | 2025-12-04 | Network upgrade coordination |
| `lux/metric` | 2025-07-26 | Metrics collection |
| `lux/math` / `lux/mock` | 2025-08-18 | Math utilities, test mocking |
| `lux/sampler` | 2025-12-24 | Validator sampling |
| `lux/staking` | 2025-12-24 | Staking mechanics |
| `lux/keychain` | 2025-12-24 | Key management |
| `lux/config` / `lux/keys` | 2025-12-21 | Configuration, key formats |
| `lux/lamport` | 2025-12-25 | Lamport one-time signatures |

### Hanzo Agent and AI Stack

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `hanzo/hanzo.ai` / `hanzo/hanzo.industries` | 2025-02-12 | Corporate websites |
| `hanzo/rules` | 2025-02-17 | AI behavior rules |
| `hanzo/operate` | 2025-03-06 | Computer operation framework |
| `hanzo/agent` | 2025-03-11 | Agent SDK |
| `hanzo/agency` | 2025-03-12 | Multi-agent orchestration |
| `hanzo/python-sdk` | 2025-03-15 | Python SDK |
| `hanzo/operative` | 2025-03-18 | Operative agent runtime |
| `hanzo/js-sdk` / `hanzo/go-sdk` | 2025-03-26 | JavaScript and Go SDKs |
| `hanzo/extension` | 2025-04-04 | Browser extension |
| `hanzo/stream` | 2024-12-16 | Real-time streaming |
| `hanzo/tools` | 2024-12-17 | Agent tool library |
| `hanzo/hanzo.sh` | 2024-11-11 | CLI installer |
| `hanzo/mcp` | 2025-07-24 | Model Context Protocol server |
| `hanzo/agents` | 2025-07-24 | Agent definitions and configs |
| `hanzo/engine` | (continued) | AI inference -- 3,258 commits |
| `hanzo/node` | (continued) | Distributed compute -- 11,711 commits |
| `hanzo/skills` | 2025-10-18 | Agent skill library |
| `hanzo/computer` | 2025-10-29 | Computer-use tools |
| `hanzo/gateway` | 2025-10-28 | API gateway |
| `hanzo/rust-sdk` | 2025-10-28 | Rust SDK |
| `hanzo/zen-agentic-dataset` | 2025-12-30 | Agentic training data |
| `hanzo/patents` | 2025-12-28 | Patent portfolio |
| `hanzo/proofs` | (2026-03-31 active) | Formal proofs: 6,309 Lean4 files |
| `hanzo/papers` | (2026-03-31 active) | 152 research papers |
| `hanzo/hips` | 2025-09-07 | Hanzo Improvement Proposals (784 proposals) |

### Zoo Labs Foundation

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `zoo/wander` | 2025-03-30 | Exploration agent |
| `zoo/nano-1` | 2025-05-23 | Nano model experiments |
| `zoo/ZIPs` | 2025-09-07 | Zoo Improvement Proposals (103 proposals) |
| `zoo/zoo-ai` | 2025-10-05 | Zoo AI platform |
| `zoo/zoo-papers-site` | 2025-10-29 | Papers website |
| `zoo/zoo.ngo` / `zoo.exchange` / `zoo.lab` / `zoo.vote` | 2025-11-02 | Foundation web properties |
| `zoo/universe` | 2025-11-02 | CI/CD and infrastructure |
| `zoo/docs` | 2025-12-14 | Documentation |
| `zoo/patents` | 2025-12-28 | Patent portfolio |
| `zoo/papers` | (2026-03-31 active) | 41 research papers |

### Security Audits (lux/audits)

| Date | Scope |
|------|-------|
| 2025-12-11 | DexVM, Oracle, Perpetuals |
| 2025-12-30 | Architecture, Consensus, Contracts, Crypto, Database, Network, Oracle, PlatformVM, ProposerVM+EVM, ThresholdVM, Warp, ZKVM, DexVM, Other VMs |
| 2026-01-30 | Standard audit (dedicated directory) |
| 2026-03-25 | Comprehensive security audit |

### Commit Activity

| Year | Hanzo | Lux | Zoo |
|------|-------|-----|-----|
| 2025 | 157,036 | 19,312 | 12,520 |

---

## 2026: Launch

Production launch. Mainnet, exchanges, compliance engine, GPU-accelerated EVM, FHE coprocessor.

### Lux Production Launch

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `lux/fhe-coprocessor` | 2026-01-07 | Encrypted smart contract coprocessor |
| `lux/fpga` | 2026-01-07 | FPGA acceleration |
| `lux/tui` | 2026-01-07 | Terminal UI for node management |
| `lux/accel` | 2026-01-09 | Hardware acceleration layer |
| `lux/mlx` | 2026-01-09 | Apple MLX integration |
| `lux/benchmarks` | 2026-02-04 | Performance benchmarks |
| `lux/operator` | 2026-02-19 | Kubernetes operator |
| `lux/treasury` | 2026-03-02 | Treasury management |
| `lux/exchange-api` / `lux/exchange-proxy` | 2026-03-06 | Exchange infrastructure |
| `lux/exchange` | 2026-04-02 | DEX frontend |
| `lux/amm` | 2026-03-25 | Automated market maker |
| `lux/evmgpu` | 2026-03-29 | GPU-accelerated EVM (CUDA opcode dispatch) |
| `lux/futures` / `lux/forex` | 2026-03-30 | Derivatives and forex |
| `lux/bank-v2` | 2026-03-31 | Banking v2 |
| `lux/genesis` | 2026-04-04 | Genesis configuration and validator management |
| `lux/cevm` | 2026-04-05 | C-Chain EVM |
| `lux/gpu` | 2026-04-06 | GPU compute framework |
| `lux/sdk-rs` | 2026-04-04 | Rust SDK |
| `lux/evm-bench` | 2026-04-04 | EVM benchmarking suite |

### Hanzo Production Infrastructure

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `hanzo/embeddings` | 2026-01-14 | Vector embeddings service |
| `hanzo/store-api` | 2026-01-14 | Store API |
| `hanzo/mpc` | 2026-01-24 | MPC threshold signing (36 commits) |
| `hanzo/mq` | 2026-01-19 | Message queue |
| `hanzo/contracts` | 2026-01-31 | Smart contracts |
| `hanzo/charts` | 2026-01-31 | Helm charts |
| `hanzo/hsm` | 2026-02-14 | Hardware security module integration |
| `hanzo/zen-gateway` | 2026-02-14 | AI model gateway |
| `hanzo/database` | 2026-02-14 | Database service |
| `hanzo/kms-operator` | 2026-02-15 | KMS Kubernetes operator |
| `hanzo/iam-sdk` | 2026-02-17 | IAM SDK |
| `hanzo/vault` | 2026-02-23 | Secret vault |
| `hanzo/billing` | 2026-02-23 | Billing system |
| `hanzo/orm` | 2026-02-23 | Object-relational mapping |
| `hanzo/operator` / `hanzo/hanzo-operator` | 2026-02-24 | Kubernetes operators |
| `hanzo/models` | 2026-02-26 | Model registry |
| `hanzo/ANE` | 2026-02-28 | Apple Neural Engine integration |
| `hanzo/ast` | 2026-03-02 | Abstract syntax tree tools |
| `hanzo/ledger` | 2026-03-05 | Financial ledger |
| `hanzo/tunnel` | 2026-03-11 | Secure tunneling |
| `hanzo/audit` | 2026-03-25 | Audit trail |
| `hanzo/onnxgo` | 2026-03-31 | ONNX Go runtime |
| `hanzo/proofs` | 2026-03-31 | Formal proofs |
| `hanzo/papers` | 2026-03-31 | Research papers |

### Zoo Launch

| Repository | First Commit | Description |
|------------|-------------|-------------|
| `zoo/contracts` | 2026-01-31 | Smart contracts |
| `zoo/genesis` | 2026-02-13 | Genesis configuration |
| `zoo/evm` | 2026-03-30 | Zoo EVM |
| `zoo/cli` | 2026-03-30 | Zoo CLI |
| `zoo/operator` | 2026-03-30 | Kubernetes operator |
| `zoo/kms` | 2026-03-30 | Key management |
| `zoo/mpc` | 2026-03-30 | MPC engine |
| `zoo/bridge` | 2026-03-30 | Cross-chain bridge |
| `zoo/computer` | 2026-03-31 | Compute platform |
| `zoo/proofs` | 2026-03-31 | Formal proofs |
| `zoo/papers` | 2026-03-31 | Research papers |
| `zoo/formal` | 2026-04-03 | Formal verification |
| `zoo/exchange` | 2026-04-02 | DEX |

### Commit Activity (YTD through 2026-04-07)

| Year | Hanzo | Lux | Zoo |
|------|-------|-----|-----|
| 2026 | 68,379 | 4,707 | 550 |

---

## Cumulative Commit History

```
Year    Hanzo       Lux         Zoo         Total
----    -----       ---         ---         -----
2014    14,547      5,405       --          19,952
2015    23,098      9,028       --          32,126
2016    17,989      2,681       --          20,670
2017    20,219      3,854       --          24,073
2018    24,508      7,427       3,009       34,944
2019    30,747      10,229      4,467       45,443
2020    46,845      18,333      1,151       66,329
2021    58,199      27,811      8,923       94,933
2022    59,535      20,333      10,029      89,897
2023    99,672      24,417      13,855      137,944
2024    141,053     20,987      21,139      183,179
2025    157,036     19,312      12,520      188,868
2026    68,379      4,707       550         73,636

TOTAL   761,827     174,524     75,643      1,011,994
```

Note: Annual totals sum to ~1,012,000. The `git rev-list --count HEAD` grand total of 1,103,364 is higher because it counts all reachable commits including merge bases and upstream fork history counted once per repo.

---

## Fork Provenance

The following repositories contain upstream history from established open-source projects. Lux/Hanzo contributions are layered on top.

| Repository | Upstream Project | Upstream Origin Date | Fork Purpose |
|------------|-----------------|---------------------|-------------|
| `hanzo/sql` | PostgreSQL | 1996 | Managed PostgreSQL service |
| `hanzo/datastore` | ClickHouse | 2008 | Analytics datastore |
| `hanzo/kv` | Valkey | 2009 | Key-value cache |
| `hanzo/redis` | Redis | 2009 | Redis compatibility |
| `hanzo/kv-go` | go-redis | 2012 | Go client library |
| `hanzo/pubsub-go` | nats.go | 2012 | Go pub/sub client |
| `hanzo/pubsub` | NATS Server | 2012 | Message broker |
| `hanzo/storage` | MinIO | 2014 | S3-compatible storage |
| `hanzo/dns` | CoreDNS | 2016 | DNS service |
| `hanzo/golang-migrate` | golang-migrate | 2014 | Database migrations |
| `hanzo/dbx` | PocketBase dbx | 2015 | Database abstraction |
| `hanzo/tasks` | Temporal | 2016 | Distributed task engine |
| `lux/coreth` / `lux/geth` | go-ethereum | 2013 | EVM implementation |
| `lux/libp2p` | go-libp2p | 2014 | P2P networking (archived -- replaced by ZAP) |
| `lux/zapdb` | Badger (Dgraph) | 2017 | Embedded KV store |
| `lux/safe` | Gnosis Safe | 2017 | Multisig contracts |
| `lux/explorer` | Blockscout | 2018 | Block explorer (replaced by luxfi/explorer) |
| `lux/lattice` | Lattigo (EPFL) | 2019 | Lattice cryptography |

All other repositories are original work.

---

## Research Papers by Domain

### Lux Network (136 papers)

**Consensus**: lux-consensus, lux-quasar-consensus, lux-fpc-consensus, lux-wave-protocol
**Cryptography**: lux-crypto-agility, lux-ringtail-pq, lux-pq-crypto-suite, lux-pq-migration, lux-ntt-transform
**FHE**: lux-fhe-smart-contracts, lux-fhe-mpc-hybrid, fhe/fhevm, fhe/fhecrdt, fhe/ml-privacy, fhe/voting
**MPC**: lux-lss-mpc, lux-mchain-mpc
**DeFi**: lux-lightspeed-dex, lux-economics, lux-tokenomics, lux-credit-lending, lux-omnichain-yield
**Infrastructure**: lux-bridge, lux-teleport-protocol, lux-teleport-architecture, lux-photon-protocol, lux-nova-protocol
**Scaling**: gpu-evm-whitepaper, evmgpu-benchmark, lux-data-availability
**Identity**: lux-achain-attestation, lux-secure-messaging, lux-zap-wire-protocol
**Governance**: lux-dao-governance-framework, lux-adoption-roadmap
**Markets**: lux-market-nft, lux-credit-protocol-spec

### Hanzo AI (152 papers)

**AI/ML**: hanzo-jin-architecture, hanzo-engine-ml, hanzo-candle, hanzo-analytics-ml, hanzo-hmm, hanzo-agent-grpo, hanzo-agent-sdk
**Infrastructure**: hanzo-aci, hanzo-base, hanzo-api-gateway, hanzo-ingress-proxy, hanzo-pubsub-events, hanzo-search
**Commerce**: crowdstart-commerce, hanzo-commerce-payments, hanzo-checkout, hanzo-ai-commerce
**Security**: hanzo-pq-crypto, hanzo-formal-verification, hanzo-harness-hacking
**Platform**: hanzo-iam-platform, hanzo-sdk-ecosystem, hanzo-mcp-server, hanzo-network-whitepaper, hanzo-tokenomics
**Communication**: hanzo-chat, hanzo-flow
**Algorithms**: algorithms/ subdirectory, defense/ subdirectory
**Models**: zen/ subdirectory (Zen model family)
**Computer Use**: hanzo-operate-computer, hanzo-operative

### Zoo Labs (41 papers)

**DeSci**: zoo-conservation-ai, zoo-habitat-modeling, zoo-satellite-ecology, zoo-wildlife-tracking, zoo-citizen-science, zoo-carbon-credits, zoo-educational-ai
**DeAI**: zoo-fhe-ai, zoo-mobile-inference, zoo-agent-nft, embedding-7680, hllm-training-free-grpo, experience-ledger-dso, beluga-l3-whitepaper
**Blockchain**: zoo-consensus, zoo-poai-consensus, zoo-quasar-benchmarks, zoo-bridge, zoo-dex, zoo-evm-l2-architecture, zoo-evm-benchmarks, zoo-gpu-evm
**Governance**: zoo-dao-governance, zoo-tokenomics, zip-002-zen-reranker
**Security**: zoo-pq-crypto, zoo-mpc-custody, zoo-key-management, zoo-fhe
**Identity**: zoo-identity-chain, zoo-experience-ledger
**Launch**: zoo-mainnet-launch-checklist

---

## Formal Verification

### Lean4 Proofs (13,160 files total)

- **Lux** (`lux/formal/lean/`, `lux/proofs/`): BFT consensus safety, bridge security, DeFi invariants, cross-chain compute, post-quantum hybrid crypto, Verkle tree, warp security, GPU scaling laws, FHE, sharia compliance
- **Hanzo** (`hanzo/proofs/lean/`): Complementary formal proofs

### TLA+ Specifications (4 specs)

- Teleport cross-chain protocol
- MPC bridge protocol state machine

### Tamarin Protocol Proofs (2 proofs)

- MPC bridge cryptographic protocol security

### Halmos Symbolic Tests (10 contracts)

- Bridge and yield vault Solidity verification

---

## Active Development (as of 2026-04-07)

Most recently committed repositories across all three organizations:

| Repository | Last Commit |
|------------|-------------|
| `lux/node` | 2026-04-07 |
| `lux/papers` | 2026-04-07 |
| `lux/netrunner` | 2026-04-07 |
| `lux/threshold` | 2026-04-07 |
| `lux/dex` | 2026-04-07 |
| `lux/formal` | 2026-04-07 |
| `lux/mpc` | 2026-04-07 |
| `hanzo/blog` | 2026-04-07 |
| `hanzo/iam` | 2026-04-07 |
| `hanzo/kms` | 2026-04-07 |
| `hanzo/papers` | 2026-04-07 |
| `hanzo/cloud` | 2026-04-07 |
| `zoo/blog` | 2026-04-07 |
| `zoo/papers` | 2026-04-07 |
| `zoo/universe` | 2026-04-07 |
