# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

Lux blockchain node implementation - a high-performance, multi-chain blockchain platform written in Go. Features multiple consensus engines (Chain, DAG, PQ), EVM compatibility, and a 6-chain architecture (A/B/C/D/Y/Z chains) with specialized capabilities.

**Key Context:**
- Fork of Avalanche with Lux-specific enhancements
- Network ID: 96369 (Lux Mainnet)
- Go Version: 1.23.9+
- Database: BadgerDB (primary), PebbleDB support

## Essential Build Commands

### Building
```bash
# Build node binary
./scripts/run_task.sh build

# Output: ./build/node

# Build specific components
go build -o luxd ./app
```

### Running Tests
```bash
# Run all tests (disable test caching with -count=1)
go test ./... -count=1

# Run specific package tests
go test ./vms/platformvm/state -count=1

# Run with race detection
go test -race ./...

# Test coverage
go test ./... -cover

# Run specific test
go test -run TestSpecificFunction ./path/to/package
```

### Code Generation
```bash
# Generate mocks (in package directory)
go generate ./...

# Regenerate protobuf (requires buf v1.31.0)
./scripts/run_task.sh generate-protobuf
```

### Running the Node
```bash
# Mainnet
./build/node

# Testnet
./build/node --network-id=testnet

# Local network (use lux-cli)
lux network start
```

## High-Level Architecture

### Multi-Chain Design
Lux implements a 6-chain architecture, each with specialized roles:

- **C-Chain**: EVM-compatible smart contracts (Hamiltonian market settlement)
- **A-Chain**: Attestation VM for supply/price oracles and compute attestations
- **B-Chain**: Bridge VM for MPC-based cross-chain interoperability
- **Z-Chain**: Zero-Knowledge VM for private clearing with ZK proofs + FHE
- **X-Chain**: UTXO-based asset exchange
- **Q-Chain**: Post-quantum cryptography layer (Ringtail signatures)

### Consensus Layer
The `/consensus/` package (separate from node) provides multiple consensus families:
- **Chain Engine**: Linear blockchain consensus
- **DAG Engine**: Directed acyclic graph for parallel processing
- **PQ Engine**: Post-quantum consensus

**Important:** The consensus package is integrated via go.mod replace directive pointing to `../consensus`

### Virtual Machines (VMs)
Located in `/vms/`:
- **platformvm**: Staking, validation, L1 validators (formerly P-Chain, now D-Chain)
- **avm**: Asset transfers, UTXO model
- **evm**: EVM execution (currently references disabled C-Chain in some code)
- **Custom VMs**: Pluggable architecture via Factory pattern

### Database Abstraction
Defined in `/database/`:
- Interface: `KeyValueReaderWriterDeleter + Batcher + Iteratee + Compacter`
- Backends: BadgerDB (default), PebbleDB, LevelDB
- Features: Prefixed namespaces, versioning, metering, batch operations

### Network Layer (`/network/`)
P2P communication with:
- TLS-based node authentication
- Gossip protocol for message propagation
- Throttling and rate limiting
- Dynamic IP resolution and NAT traversal

## Lux Proposals (LPs)

Lux uses "LP" (Lux Proposal) instead of "ACP" for branding. Key implemented LPs:

### LP-181 (Epoching)
- **Location**: `vms/proposervm/lp181/`
- **Purpose**: Validator set optimization with fixed P-Chain height windows
- **Status**: ✅ Integrated (Granite upgrade)

### LP-176 (Dynamic Gas Pricing)
- **Location**: `vms/evm/lp176/`
- **Purpose**: Congestion-aware gas pricing for C-Chain
- **Status**: ✅ Implemented

### LP-226 (Dynamic Block Timing)
- **Location**: `vms/evm/lp226/`
- **Purpose**: Sub-second blocks with adaptive timing
- **Status**: ✅ Implemented

### LP-204 (secp256r1 Precompile)
- **Location**: `geth/core/vm/contracts.go` (in geth submodule)
- **Purpose**: P-256 signatures for biometric wallets
- **Gas Cost**: 6,900 (2x RIP-7212 spec)
- **Status**: ✅ Implemented

## Critical Development Patterns

### Context Usage
The codebase has migrated from struct-based contexts to standard `context.Context`:
```go
// Modern pattern
ctx := context.Background()
ctx = consensus.WithIDs(ctx, consensus.IDs{
    NetworkID:  1,
    ChainID:    constants.PlatformChainID,
})
```

### Import Aliasing
To avoid package conflicts, especially with consensus:
```go
import (
    platformblock "github.com/luxfi/node/vms/platformvm/block"
    consensusblock "github.com/luxfi/consensus/engine/chain/block"
)
```

### Mock Generation
Use `//go:generate` directives:
```go
//go:generate go run go.uber.org/mock/mockgen -package=state -destination=mock_state.go . State
```

## Known Constraints

### Package Dependencies
- **DO NOT** use `github.com/ava-labs` packages - use `github.com/luxfi` equivalents
- **DO NOT** use `go-ethereum` - use `github.com/luxfi/geth`
- **DO NOT** use `github.com/metric/client_golang` - use `github.com/luxfi/metric`
- **DO NOT** use `luxfi/log` - use `github.com/luxfi/log`
- **ALWAYS** use Lux consensus packages, not snow/avalanche consensus

### Naming Conventions
- Go: Standard Go style (lowercase packages, CamelCase exports)
- Commit prefixes: `node:`, `vms:`, `consensus:`, etc.
- LP packages: `lp176`, `lp226` (lowercase, no hyphen)

### Testing Requirements
- **ALWAYS** test your changes - show passing tests
- Table-driven tests preferred
- Use `_test.go` suffix
- Mock external dependencies

## Recent Migration Work

### Database Migration (Oct 2025)
- **Tool**: `/cmd/migrate-subnet-to-cchain/`
- **Purpose**: Migrate EVM PebbleDB → C-Chain BadgerDB
- **Status**: ✅ Complete (34.1M keys, 1.08M canonical hashes)
- **Output**: `/Users/z/.luxd/db/C-migrated/`

### Granite Upgrade Integration (Oct 2025)
All Granite features implemented:
- LP-181 (Epoching)
- LP-176 (Dynamic Gas)
- LP-226 (Dynamic Timing)
- LP-204 (secp256r1)

**Test Suite**: `/tests/granite_integration_test.go`
- 9 test functions, 30 test cases
- 6 benchmark functions
- All passing

## Build Issues (Current)

Some packages have remaining compilation errors (NOT related to recent work):
- Missing interface methods (PrimaryAlias, Lock, NewHTTPHandler)
- Protobuf definitions need regeneration
- Type mismatches in VM integration points

**Strategy**: Fix critical paths first (proposervm, platformvm), iterate on remaining packages.

## Special Files

- **LLM.md**: Extensive project history and session notes (2000+ lines)
- **CLAUDE.md**: This file - guidance for Claude Code
- **README.md**: Public-facing project documentation
- **`~/.claude/CLAUDE.md`**: Global user instructions (Lux-specific rules)
- **`~/CLAUDE.md`**: Project-wide instructions for Hanzo/Lux/Zoo ecosystems

## References

- **LP Specifications**: `~/work/lux/lps/` repository
- **Consensus Package**: `~/work/lux/consensus/`
- **Related Repos**:
  - `~/work/lux/geth` - C-Chain EVM implementation
  - `~/work/lux/cli` - Management CLI
  - `~/work/lux/netrunner` - Network testing

---

*Last Updated*: 2025-11-03
*Maintainer*: AI-assisted development following test-driven principles
