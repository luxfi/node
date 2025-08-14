# Release Notes

## Historical Timeline

### Private Testing Phase (January 2021)
- Initial private alpha testing and benchmarking
- Based on early Lux node implementation
- Used for internal evaluation and performance testing
- Testing consensus mechanisms and network topology

### Mainnet Beta Launch (January 1, 2022)
- Lux mainnet beta launched with network ID 96369
- Based on luxd v1.7.x series
- Initial production deployment with 21 validators
- Established primary network infrastructure

### First Public Network - Chain ID 7777 (December 2023)
- **Version**: v1.10.20
- First public deployment with Chain ID 7777
- Based on luxd v1.10.20 compatibility
- Beginning of public testing phase
- Network upgrade to luxd v1.9.x/v1.10.x compatibility
- Enhanced stability and performance improvements
- Added support for dynamic fees
- Improved subnet management capabilities

### Public Mainnet - Chain ID 96369 (November 2024)
- **Version**: v1.11.13
- Official mainnet launch with Chain ID 96369
- Upgraded to luxd v1.11.x compatibility
- Public mainnet launch with original genesis
- Enhanced Warp messaging support
- Improved cross-chain communication
- Running stable through July 2025

### Current Sync (July 26, 2025)
- Full synchronization with latest upstream versions
- Now at parity with luxd v1.13.3
- All tests passing with upstream compatibility
- Final compatibility build for Lux network

---

## Historical Version Tags

### Preserved for Historical Reference

- **v1.10.20** - Chain ID 7777 genesis version (December 2023)
- **v1.11.13** - Chain ID 96369 original genesis version (November 2024)
- **v1.13.13** - Current development version (August 2025)

---

## Current Releases

## [v1.13.13](https://github.com/luxfi/node/releases/tag/v1.13.13)

**Current Development Version**

This version represents the current state of development, fully synchronized with upstream luxd.

## [v1.11.13](https://github.com/luxfi/node/releases/tag/v1.11.13)

**Chain ID 96369 Genesis Version**

This was the version used for the official Lux Network mainnet launch in November 2024 with Chain ID 96369.

## [v1.10.20](https://github.com/luxfi/node/releases/tag/v1.10.20)

**Chain ID 7777 Genesis Version**

This was the version used for the first public Lux Network deployment in December 2023 with Chain ID 7777.

### Key Features

- Full node implementation for Lux network
- Support for primary network and subnets/L2s
- Consensus mechanism optimized for Lux network
- Support for POA mode with modified consensus parameters
- Built with Go 1.24.5
- All upstream tests passing

### Major Components

- **Consensus Engine**: Our own consensus implementation
- **Network Layer**: P2P networking with custom modifications
- **Chain Management**: Support for multiple blockchain VMs
- **API Server**: Full RPC/REST API support
- **Database**: Efficient state management

### Configuration

- Network ID 96369 for mainnet
- Support for single-node development mode
- Modified consensus parameters (snow-sample-size=1, snow-quorum-size=1)
- APIs: admin, auth, health, info, keystore, metrics, platform, X-Chain

### Network Support

- **Primary Network**: LUX mainnet and testnet
- **Subnet Support**: Full L2/subnet capabilities
- **Cross-Chain**: Warp messaging and atomic swaps
- **Validator Management**: Staking and delegation

### Development

- Extensive test suite with full upstream compatibility
- Docker support
- Comprehensive documentation
- Active development and community support