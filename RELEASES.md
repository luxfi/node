# Release Notes

## Historical Timeline

### Private Testing Phase (January 2021)
- Initial private alpha testing and benchmarking
- Based on early Avalanche node implementation
- Used for internal evaluation and performance testing
- Testing consensus mechanisms and network topology

### Mainnet Beta Launch (January 1, 2022)
- Lux mainnet beta launched with network ID 96369
- Based on avalanchego v1.7.x series
- Initial production deployment with 21 validators
- Established primary network infrastructure

### Second Major Update (January 2023)
- Network upgrade to avalanchego v1.9.x compatibility
- Enhanced stability and performance improvements
- Added support for dynamic fees
- Improved subnet management capabilities

### Public Mainnet (November 2024)
- Upgraded to avalanchego v1.11.x compatibility
- Public mainnet launch
- Enhanced Warp messaging support
- Improved cross-chain communication
- Running stable through July 2025

### Current Sync (July 26, 2025)
- Full synchronization with latest upstream versions
- Now at parity with avalanchego v1.13.3
- All tests passing with upstream compatibility
- Final compatibility build for Lux network

---

## Current Releases

## [v1.13.3](https://github.com/luxfi/node/releases/tag/v1.13.3)

**First Official Tagged Release of Lux Node**

This is the first officially tagged release of Lux Node, fully synchronized with avalanchego v1.13.3.

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