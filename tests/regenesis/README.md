# Regenesis Test Suite

Comprehensive CI test suite for network regenesis verification.

## Test Categories

### Unit Tests (`regenesis_test.go`)

Fast, isolated tests that don't require external dependencies:

- **Genesis Block Creation**: Validates genesis block creation and configuration
- **Multi-Chain Genesis Consistency**: Verifies P, X, C chain genesis alignment
- **State Migration**: Tests database key migration and namespace stripping
- **Block Import with Hash Verification**: Validates block import integrity
- **RLP Encoding/Decoding**: Tests RLP round-trip for headers and blocks
- **Database Key Format**: Verifies key format correctness
- **Progress Tracking**: Tests migration progress and checkpointing

### Integration Tests (`integration_test.go`)

Tests requiring network bootstrap but no external data:

- **Network Bootstrap**: Single and multi-validator network startup
- **eth_blockNumber Verification**: Block height after migration
- **Cross-Chain State Consistency**: Balance and state root verification
- **Full Migration Flow**: Complete migration pipeline test
- **Genesis Validation**: Genesis configuration verification

### E2E Tests (`e2e_test.go`)

Full end-to-end tests requiring complete environment:

- **Full Network Regenesis**: Complete migration and bootstrap flow
- **Multi-Chain Consistency**: All chains operational verification
- **Validator Operations**: Post-regenesis validator set verification
- **Transaction Processing**: C-Chain transaction capability
- **Network Restart Resilience**: State persistence across restarts
- **Bootstrap New Node**: New node can join and sync

## Running Tests

### Unit Tests (Fast, No Dependencies)

```bash
# Run all unit tests
go test -v ./tests/regenesis/...

# Run with race detection
go test -race -v ./tests/regenesis/...

# Run specific test
go test -v -run TestGenesisBlockCreation ./tests/regenesis/...

# Run benchmarks
go test -bench=. ./tests/regenesis/...
```

### Integration Tests

```bash
# Build luxd first
go build -o ./build/luxd ./main

# Set environment
export LUXD_PATH=$(pwd)/build/luxd

# Run integration tests
go test -v -tags=integration ./tests/regenesis/...
```

### E2E Tests

```bash
# Set required environment variables
export LUXD_PATH=$(pwd)/build/luxd
export E2E_REGENESIS=1
export SOURCE_DATA_DIR=/path/to/source/chaindata  # For migration tests

# Run E2E tests
go test -v -tags=e2e -timeout=30m ./tests/regenesis/...
```

## Test Utilities

### `testutil/genesis.go`
- `DefaultGenesisConfig()` - Returns default test genesis configuration
- `CreateGenesisBlock()` - Creates a genesis block from config
- `CreateMultiChainGenesis()` - Creates full P/X/C chain genesis

### `testutil/migrator.go`
- `NewStateMigrator()` - Creates state migrator between databases
- `HeaderKey()`, `CanonicalKey()`, `BodyKey()` - Key format helpers
- `ParseHeaderKey()` - Parses header key components

### `testutil/block.go`
- `NewBlockImporter()` - Creates block importer
- `CreateTestBlock()` - Creates test block with computed hash
- `WriteTestBlocks()` - Writes test blocks to database

### `testutil/database.go`
- `CreateTestDatabase()` - Creates in-memory test database
- `OpenTestDatabase()` - Opens existing test database

## CI Configuration

### GitHub Actions Workflow

```yaml
name: Regenesis Tests

on:
  push:
    paths:
      - 'tests/regenesis/**'
      - 'chainmigrate/**'
      - 'cmd/regenesis/**'
  pull_request:
    paths:
      - 'tests/regenesis/**'
      - 'chainmigrate/**'
      - 'cmd/regenesis/**'

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Run unit tests
        run: go test -v -race ./tests/regenesis/...

  integration-tests:
    runs-on: ubuntu-latest
    needs: unit-tests
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Build luxd
        run: go build -o ./build/luxd ./main
      - name: Run integration tests
        env:
          LUXD_PATH: ./build/luxd
        run: go test -v -tags=integration -timeout=10m ./tests/regenesis/...
```

## Key Assertions

### Genesis Tests
- Network ID is valid (non-zero)
- Genesis block number is 0
- Parent hash is zero hash
- State root reflects allocations
- Genesis hash is deterministic

### Migration Tests
- All keys are migrated
- Namespace is properly stripped
- Block count matches source
- State root is preserved
- Progress callbacks are invoked

### Block Import Tests
- Hash verification passes
- Parent hash chain is valid
- Canonical mapping is correct
- Block sequence is continuous

### Network Tests
- All nodes report healthy
- Network ID matches genesis
- All chains are accessible
- Validators match genesis config
- eth_blockNumber reflects migrated height

## Critical Test Scenarios

1. **Genesis Creation with Invalid Network ID**
   - Should fail with clear error

2. **Migration with Corrupted Source**
   - Should detect and report corruption

3. **Block Import with Invalid Hash**
   - Should reject and not persist

4. **Network Bootstrap Failure Recovery**
   - Should clean up partial state

5. **Cross-Chain State Mismatch**
   - Should detect and report inconsistency

## Adding New Tests

1. Add test function to appropriate file based on category
2. Follow existing naming conventions: `TestCategory_Scenario`
3. Use `require` for assertions
4. Add appropriate build tags for integration/e2e tests
5. Document test purpose in function comment
6. Update this README if adding new category
