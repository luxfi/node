# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.13.5-alpha] - 2025-01-23

### Added
- L1 (Layer 1) validator support with complete transaction types:
  - `ConvertSubnetToL1Tx` - Convert existing subnets to L1
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