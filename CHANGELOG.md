# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.36.29]

### Fixed
- **`/v1/health` reported a chain the node denies exists.** The `bootstrapped` check publishes `chains.Nets.Bootstrapping()` verbatim as its message, but `Nets.chains` is keyed by **net** ID and the aggregate appended the map **key**, not the chain. Every primary-network chain that failed to converge therefore surfaced as the single ID `11111111111111111111111111111111LpoYY` — `constants.PrimaryNetworkID` (`ids.Empty`) — so an operator resolving it got "there is no chain with alias/ID", and N stuck chains collapsed into one indistinguishable entry (measured on devnet/testnet: `"message":["11111111111111111111111111111111LpoYY"],"contiguousFailures":3213`). The net owns the bootstrapping set, so the net now names its own chains: new `nets.Net.Bootstrapping() []ids.ID` (`nets/net.go`), and `chains/chains.go` aggregates those instead of the keys. `TestNetsBootstrappingReportsChainsNotNets` asserts `ids.Empty` never appears and that two stuck chains are individually named; `TestNetsBootstrapping` no longer asserts the bug (it demanded the net ID).
- Ships the GET `/v1/health` encoder fix from `dada5a31` (`{"healthy":…,"error":"health reply encode failed"}` on every node — jsonv2 has no representation for `apihealth.Result.Duration`), which was on `main` but had never been tagged.

## [1.36.13]

### Fixed
- **The durable rejoin fix (#66/#74) shipped INERT for the C-Chain — the exact chain whose 40h mainnet freeze motivated it.** `chains/manager.go` gated `expectsStakedBeacons` on `ids.IsNativeChain(chainParams.ID)` — the *blockchain* ID. But `ids.IsNativeChain` only matches the symbolic `111…C` alias form, and every deployed C/X/Q carries a **hash** blockchain ID (devnet C `21HieZng…`, mainnet C `2wRdZG…`); only the P-chain has a symbolic blockchain ID (`111…P`), and it is excluded as the platform chain. So `isNativeChain` was **always false** for the real C-Chain → `expectsStakedBeacons=false` → under the production `--skip-bootstrap=true` the beacon set was emptied → a behind C-Chain named its STALE local tip the frontier and never caught up (verified on devnet v1.36.12: C-Chain wedged at height 0 with "using empty beacons for single-node mode"). Fix: discriminate on the **validating Net** (`chainParams.ChainID == constants.PrimaryNetworkID`) via new `chainValidatesOnPrimaryNetwork`, which is `PrimaryNetworkID` for C/X/Q and each sovereign L1's own net ID for an L2 — so C/X/Q now keep their staked beacons under `--skip-bootstrap` (peer-sync a behind validator) while L2s keep the empty-beacon single-node path. Regression test `TestChainValidatesOnPrimaryNetwork_RealHashChainID` exercises the real discriminator with hash chain IDs (the prior hardcoded-bool tests could not catch this); `TestRED_EmptyStakedSetFailsSafe` still passes (forged-frontier gate intact).

## [1.36.12]

### Fixed
- **EVM chains (C/D/L2) failed to initialize on v1.36.11 — bundled VM plugins were built against a stale `luxfi/api`.** The node pins `luxfi/api v1.0.16`, whose `InitializeResponse` carries the appended `Capabilities uint64` field (Quasar-export handshake, api commit `1f2dc5a`). The image baked the C-Chain EVM plugin from `luxfi/evm@v1.104.8` and the D-Chain dexvm plugin from `luxfi/dex@v1.5.15`, both of which resolve `luxfi/api v1.0.15` (no `Capabilities`). Their `InitializeResponse.Encode` writes a shorter payload than the node's strict `InitializeResponse.Decode` expects, so `vms/rpcchainvm/zap/client.go` fails every EVM VM handshake with `zap decode initialize response: unexpected EOF`. Native VMs (P/X/Q) were unaffected. Fix: bump `EVM_VERSION` `v1.104.8 → v1.104.9` (api v1.0.16) and force `luxfi/api@v1.0.16` in the dexvm build stage. The api bump is code-free for plugins (`luxfi/chains v1.7.4 → v1.7.5` adopted it with a go.mod-only diff; `CHAINS_REF=v1.7.6` already carries v1.0.16). No node source change beyond the version bump — this is v1.36.11's node binary (durable rejoin fix, commit `63f61429d1`) rebuilt with plugin↔node ZAP-wire alignment.

## [1.28.0]

### Added
- Multi-version P-Chain block + tx codec. v1.23.x ("Apricot/Banff") wire layout is now registered as `CodecVersionV0` on the platformvm tx and block `codec.Manager`s; the current canonical layout is `CodecVersionV1`. Bytes carry their wire version in the standard 2-byte codec prefix and `txs.Parse` / `block.Parse` dispatch by it.
- Byte-preserving tx Initialization: `tx.InitializeFromBytes(c, version, signedBytes)` and `tx.InitializeFromBytesAtVersion(c, version)` bind a tx to its original `signedBytes` without re-marshalling. `TxID = hash(signedBytes)` is therefore stable across the v0->v1 migration — mainnet and testnet chain commitments hash back to byte-identical inputs.
- `vms/platformvm/block/v0/` subpackage holding the pure-data v0 block kinds (`ApricotProposalBlock`, `BanffProposalBlock`, ... — 9 types, slots 0-4 + 29-32). The package is a one-way decoder only; the `liftedV0Block` adapter in the parent package translates each v0 block kind into the corresponding canonical v1 `Visitor` arm without ever re-marshalling.
- Cross-version tx + block tests: `TestCodecVersionV0V1Coexist`, `TestParseDispatchesByVersion`, `TestTxIDStabilityRoundTrip`, `TestCrossVersionRefuses`, `TestParseV0ApricotProposalBlock`, `TestParseV0BanffStandardBlock`, `TestParseV1RoundTrip`, `TestVersionPrefixDispatch`.

### Changed
- `tx.Initialize(c)` is retained for the fresh-build path only (wallet, builder). The from-DB / from-wire paths in `genesis/genesis.go` and `block/{standard,proposal}_block.go` now go through `InitializeFromBytesAtVersion` against the codec version the surrounding container was decoded under.
- `genesis.Parse` is wire-version-aware: pre-codec-v1 P-Chain genesis blobs on mainnet / testnet decode at `CodecVersionV0`; new blobs are written at `CodecVersionV1`.
- The L1-tx slot map advances by one to accommodate `CreateSovereignL1Tx` at slot 36: `RegisterL1ValidatorTx`=37, `SetL1ValidatorWeightTx`=38, `IncreaseL1ValidatorBalanceTx`=39, `DisableL1ValidatorTx`=40 (was 36-39). Both pre-rip mainnet/testnet bytes (no slot conflict; L1 txs did not exist) and current code paths line up with the new slot map.
- Test fixtures in `vms/platformvm/txs/fee/calculator_test.go` and the serialization tests under `vms/platformvm/txs/*_test.go` are regenerated to use the v1 wire prefix (`0x0001`) and the post-`CreateSovereignL1Tx` slot map.

### Migration notes
- Mainnet + testnet P-Chain DBs do NOT need to be rebuilt: pre-codec-v1 blocks continue to decode via the v0 path with their original `BlockID = hash(v0 bytes)` preserved. Newly accepted blocks are written at v1.
- Devnet, which was bootstrapped post-rip at wire-version 0 but with the post-rip slot map, must be rebuilt before rolling to `v1.28.0`: its existing blobs would now decode against the v0 slot map (the pre-rip layout) and produce wrong types. A fresh bootstrap at `v1.28.0` writes v1 bytes from height 0 and is internally consistent thereafter.

## [1.13.5-alpha] - 2025-01-23

### Added
- L1 (Layer 1) validator support with complete transaction types:
  - `ConvertNetToL1Tx` - Convert existing chains to L1
  - `RegisterL1ValidatorTx` - Register new L1 validators  
  - `SetL1ValidatorWeightTx` - Adjust validator weights
  - `IncreaseL1ValidatorBalanceTx` - Increase validator balance
  - `DisableL1ValidatorTx` - Disable validators
- LP-118 protocol implementation for warp message handling:
  - Signature aggregation support
  - BLS signature verification
  - Cached handler for performance optimization
  - Handler adapter for P2P integration
- Complete wallet support for L1 validator operations
- Extended AppSender interface for cross-chain messaging

### Fixed
- P2P test package compatibility issues
- Set package import conflicts (math/set vs utils/set vs consensus/utils/set)
- Interface compatibility between consensus and local packages
- Handler function signatures for proper interface implementation
- Mock testing with gomock package updates
- BLS signature handling in tests
- AppError type conversions between packages
- All wallet examples now compile and run correctly

### Changed
- Updated import paths to use luxfi packages consistently
- Improved error handling in P2P message handlers
- Enhanced test coverage for LP-118 protocol
- Standardized AppError usage across packages

### Technical Details
- 100% of internal packages (351 packages) now build successfully
- All tests pass in modified packages
- Full CI/CD pipeline configured with GitHub Actions
- Compatible with Go 1.21.12+

## [1.13.4] - Previous Release

[Previous release notes...]