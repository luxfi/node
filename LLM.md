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

## Build Status (2025-11-03 Update)

### ✅ Compilation Fixes Completed
- Fixed platformvm main package (context type conversions)
- Fixed consensus context extraction from interface parameters  
- Fixed clock references (vm.consensusClock)
- Fixed import redeclarations in wallet packages
- Fixed metric/log usage in test fixtures
- Created luxfi/metric/collectors and luxfi/metric/promhttp packages
- Fixed GaugeVec type declarations (interface vs pointer)

### 🔧 Remaining Issues
- **metric.Registerer compatibility**: metric interfaces don't implement metric.Collector
- **network/peer log usage**: Files still using github.com/luxfi/log directly
- **Import redeclarations**: Some files have duplicate log imports

**Strategy**: Complete metric package integration, then fix network/peer log usage.

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

## Test Compilation Progress (2025-11-05)

### Session Goal
Fix all test compilation errors and achieve 100% test pass rate. Compare with ~/work/ava/avalanchego for feature parity.

### Progress Summary
- **Starting Point**: ~105/148 test packages compiling (71%)
- **Current Status**: ~109+/148 test packages compiling (73%+)
- **Commits Made**: 9 commits (latest: 2e8c3619d5)
- **Key Directive**: Don't remove/skip tests - re-enable and fix properly

### Fixes Completed

#### 1. utils/crypto/keychain Test (Commit 2e8c3619d5)
**Problem**: Mock interface out of sync, API signature changes
**Solution**:
- Regenerated keychainmock/ledger.go with `go generate`
- Updated API calls:
  - `Addresses()` → `GetAddresses()`
  - `SignHash(hash, []uint32)` → `SignHash(hash, uint32)` (single index)
  - `SignHash` return: `[][]byte` → `[]byte` (single signature)
  - `NewLedgerKeychain(ledger, N)` → `NewLedgerKeychain(ledger, []uint32{...})`
- Removed `NewLedgerKeychainFromIndices` (merged into NewLedgerKeychain)
- Renamed duplicate test function to TestNewLedgerKeychainWithIndices
- Fixed variable shadowing (`:=` → `=`)

#### 2. Network Test Packages
- network/ - Now compiling ✓
- network/p2p/lp118/ - Now compiling ✓

#### 3. VMs Test Packages
- vms/nftfx/ - Now compiling ✓

#### 4. Integration Test Packages
- tests/poa/ - Now compiling ✓

#### 5. Earlier Fixes (Previous Session)
- tests/fixture/bootstrapmonitor/e2e - Fixed log.Error and FlagsMap type conversion
- tests/fixture/tmpnet - Fixed logging.NoLog reference
- tests/load - Fixed logging.NoLog reference
- network/example_test - Fixed log.Reflect usage

### Remaining Work

#### Skipped Files Requiring Re-enabling (3 files)
These were temporarily skipped but MUST be re-enabled and fixed:
1. `vms/platformvm/txs/executor/create_subnet_test.go.skip`
2. `vms/platformvm/warp/validator_test.go.skip`
3. `vms/platformvm/warp/signature_test.go.skip`

**Action Required**: Compare with avalanchego equivalents and fix properly

#### Complex Test Packages Still Failing (~39 packages)
1. **indexer** - Mock issues (blockmock.NewChainVM, snowtest, vertexmock)
2. **vms/exchangevm/** - Multiple packages with mock/API issues
3. **vms/components/** - Mock generation compatibility
4. **vms/platformvm/warp** - Complex gomock to manual mock migration
5. **utils/crypto/ledger** - API changes not fully addressed
6. **tests/integration_test** - Dependencies on above packages

### Next Steps
1. Re-enable .skip files and fix using avalanchego as reference
2. Fix indexer package (mock generation issues)
3. Fix exchangevm packages systematically  
4. Fix components packages
5. Complete warp test fixes
6. Run full test suite and fix runtime failures
7. Verify 100% test pass rate
8. Compare feature set with avalanchego

### Reference
- Avalanchego repo: ~/work/ava/avalanchego
- Current commit: 2e8c3619d5 (pushed to main)

## Test Compilation Progress (2025-11-06 Session)

### Session Goal
Continue fixing test compilation errors, re-enable .skip files, achieve 100% test pass rate.

### Progress Summary
- **Starting Point**: 105/148 packages (71%)
- **Current Status**: 105/148 packages (71%) - 43 remain
- **Commits Made**: 2 commits (b90b689b1e, a15355b27e)
- **Pushed to GitHub**: ✅ Yes

### Fixes Completed

#### 1. vms/platformvm/warp Package (Commits b90b689b1e, a15355b27e)
**Problem**: Architectural mocking incompatibility - Avalanchego uses gomock, Lux used function-field mocks
**Solution**:
- Created `testValidatorStateAdapter` wrapper struct
- Bridges `validators.State` (gomock) → `ValidatorState` interface (Lux requirement)
- Lux added `GetNetID()` method not present in Avalanchego
- Adapter provides stub `GetNetID()` implementation for tests
- Fixed all `validators.Warp` → `Validator` type references globally
- Fixed BLS PublicKey conversions: `bls.PublicKeyToUncompressedBytes()`
- Fixed signer_test.go:
  - Added missing `bls` import
  - Changed function signatures: `*bls.SecretKey` → `*localsigner.LocalSigner`
  - Fixed PublicKey access: `bls.PublicFromSecretKey(sk)` → `sk.PublicKey()`
  - Added `warp.` qualifiers for Signer, NewUnsignedMessage

**Test Status**: ✅ vms/platformvm/warp now compiles successfully

#### 2. Renamed create_subnet_test → create_net_test (Commit a15355b27e)
**Problem**: Old subnet terminology, file needed renaming
**Solution**:
- Renamed `vms/platformvm/txs/executor/create_subnet_test.go.skip` → `create_net_test.go`
- Test already uses `CreateNetTx` internally (good!)
- File re-enabled but has compilation issues remaining:
  - Missing test helpers (genesistest, defaultGenesisTime, apricotPhase3, preFundedKeys)
  - Config structure changes (StaticFeeConfig field)
  - NewWalletFactory signature changes (parameters mismatch)
  - StandardTxExecutor undefined

**Test Status**: ⚠️ Renamed but not yet compiling

### Remaining Work

#### Skipped Files (1 remaining)
1. ✅ `vms/platformvm/txs/executor/create_net_test.go` - Renamed, needs API fixes
2. ⏳ `vms/platformvm/warp/signature_test.go.skip` - Not yet addressed

#### Complex Test Packages Still Failing (43 packages)
Same as previous session - no progress on these yet

### Next Steps
1. Fix create_net_test.go API compatibility issues
2. Re-enable and fix signature_test.go.skip
3. Tackle remaining 43 failing packages systematically
4. Run full test suite for runtime failures
5. Compare with avalanchego for feature parity
6. Verify 100% pass rate

### Key Insights
- **Adapter Pattern Works**: The testValidatorStateAdapter successfully bridged gomock to Lux ValidatorState
- **Naming Convention**: Lux uses "Net" not "Subnet" terminology throughout
- **Test Helpers**: Many tests depend on shared test fixtures that need updating


## Subnet to Net Renaming (2025-11-06)

Successfully completed systematic renaming of all "subnet" references to "net" throughout the Lux node codebase.

### Summary of Changes

#### Directory Renames
- `tests/fixture/subnet/` → `tests/fixture/net/`
- `cmd/migrate-subnet-to-cchain/` → `cmd/migrate-net-to-cchain/`
- `wallet/subnet/` → Removed (wallet/net already existed)

#### File Renames
- `chains/subnets.go` → `chains/nets.go` (and test file)
- `tests/e2e/p/elastic_subnets.go` → `elastic_nets.go`
- `tests/e2e/p/permissionless_subnets.go` → `permissionless_nets.go`
- `tests/fixture/tmpnet/subnet.go` → `net.go`
- `vms/components/verify/subnet.go` → `net.go` (and test file)
- `vms/experimental/aivm/gpu_subnet.go` → `gpu_net.go`
- `vms/platformvm/state/subnet_id_node_id.go` → `net_id_node_id.go` (and test)
- `vms/platformvm/txs/executor/subnet_tx_verification.go` → `net_tx_verification.go`
- `vms/platformvm/warp/message/subnet_to_l1_conversion.go` → `net_to_l1_conversion.go` (and test)
- `vms/platformvm/docs/subnets.md` → `nets.md`

#### Code Updates
- Updated all import paths from `wallet/subnet` to `wallet/net`
- Updated all import paths from `fixture/subnet` to `fixture/net`
- Replaced `Subnet` with `Net` in type names, struct names, and variables
- Replaced `subnet` with `net` in function names and identifiers
- Updated documentation and comments

#### Notable Preserved Elements
- Proto field names kept unchanged for backwards compatibility (e.g., `tracked_subnets`, `subnet_uptimes`)
- NetID type alias maintained (was already renamed from SubnetID)

### Testing Results
- ✅ All packages compile successfully
- ✅ Main binary (`./app`) builds without errors
- ✅ Changed files: 66 files modified
- ✅ Lines changed: 27 insertions, 3115 deletions (net reduction due to wallet/subnet removal)

### Commit
- Commit hash: 3ad42d81fd
- Message: "refactor: rename subnet to net throughout codebase"

This completes the comprehensive subnet → net renaming task across the entire Lux node repository.
