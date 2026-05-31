# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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