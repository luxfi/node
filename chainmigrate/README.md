# Chain Migration Framework

A generic blockchain import/export framework for migrating data between different blockchain implementations, enabling live mainnet "regenesis" and cross-chain state transfers.

## Overview

This framework provides a flexible, extensible solution for migrating blockchain data between different chain implementations, including:

- **SubnetEVM** → **C-Chain** (with CorethVM compatibility)
- **Zoo L2** → **C-Chain** or any other chain
- **C-Chain** → **SubnetEVM**
- Any custom blockchain that implements the interfaces

## Features

- ✅ **Generic Interfaces**: Clean separation between exporters and importers
- ✅ **Multiple Database Backends**: Support for PebbleDB, BadgerDB, LevelDB
- ✅ **Streaming Architecture**: Efficient memory usage for large chains
- ✅ **Parallel Processing**: Configurable worker pools for performance
- ✅ **Progress Tracking**: Real-time progress reporting with ETA
- ✅ **Error Recovery**: Continue-on-error mode for resilient migrations
- ✅ **Verification**: Optional block and state verification
- ✅ **CorethVM Compatibility**: Special handling for C-Chain differences
- ✅ **L2 Support**: Built-in support for optimistic rollups

## Architecture

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│  Source Chain   │────▶│   Exporter   │────▶│    Migrator     │
│  (SubnetEVM)    │     │              │     │  (Orchestrator) │
└─────────────────┘     └──────────────┘     └────────┬────────┘
                                                       │
                                                       ▼
┌─────────────────┐     ┌──────────────┐     ┌─────────────────┐
│   Dest Chain    │◀────│   Importer   │◀────│   Block Data    │
│   (C-Chain)     │     │              │     │   State Data    │
└─────────────────┘     └──────────────┘     └─────────────────┘
```

## Components

### Core Interfaces

#### ChainExporter
```go
type ChainExporter interface {
    Init(config ExporterConfig) error
    GetChainInfo() (*ChainInfo, error)
    ExportBlocks(ctx context.Context, start, end uint64) (<-chan *BlockData, <-chan error)
    ExportState(ctx context.Context, blockNumber uint64) (<-chan *StateAccount, <-chan error)
    ExportConfig() (*ChainConfig, error)
    VerifyExport(blockNumber uint64) error
    Close() error
}
```

#### ChainImporter
```go
type ChainImporter interface {
    Init(config ImporterConfig) error
    ImportConfig(config *ChainConfig) error
    ImportBlock(block *BlockData) error
    ImportBlocks(blocks []*BlockData) error
    ImportState(accounts []*StateAccount, blockNumber uint64) error
    FinalizeImport(blockNumber uint64) error
    VerifyImport(blockNumber uint64) error
    ExecuteBlock(block *BlockData) error
    Close() error
}
```

### Implementations

- **SubnetEVMExporter**: Exports from SubnetEVM databases
- **CChainImporter**: Imports into C-Chain with optional CorethVM compatibility
- **ZooL2Exporter**: Exports from Zoo L2 via RPC
- **ChainMigrator**: Orchestrates the entire migration process

## Usage

### Command Line Tool

```bash
# Build the tool
cd cmd/chainmigrate
go build -o chainmigrate

# Migrate SubnetEVM to C-Chain
./chainmigrate \
  --source-type=subnet-evm \
  --source-db=/path/to/subnet/db \
  --source-db-type=pebble \
  --dest-type=c-chain \
  --dest-db=/path/to/cchain/db \
  --dest-db-type=leveldb \
  --start-block=0 \
  --end-block=1074616 \
  --verify-blocks \
  --verify-state \
  --coreth-compat

# Migrate Zoo L2 to C-Chain
./chainmigrate \
  --source-type=zoo-l2 \
  --source-rpc=http://zoo-l2-rpc:8545 \
  --l1-contract=0x1234... \
  --dest-type=c-chain \
  --dest-db=/path/to/cchain/db \
  --start-block=1000000 \
  --end-block=2000000
```

### Programmatic Usage

```go
import "github.com/luxfi/node/chainmigrate"

// Create exporter
exporterConfig := chainmigrate.SubnetEVMExporterConfig{
    DatabasePath: "/path/to/subnet/db",
    DatabaseType: "pebble",
    Namespace:    []byte{0x33, 0x7f, ...}, // SubnetEVM namespace
}
exporter := chainmigrate.NewSubnetEVMExporter(exporterConfig)

// Create importer
importerConfig := chainmigrate.ImporterConfig{
    DatabasePath:       "/path/to/cchain/db",
    DatabaseType:       "leveldb",
    EnableCorethCompat: true,
}
importer := chainmigrate.NewCChainImporter(importerConfig)

// Create migrator
migratorConfig := chainmigrate.MigratorConfig{
    StartBlock:       0,
    EndBlock:         1074616,
    BlockBatchSize:   100,
    ParallelWorkers:  4,
    VerifyBlocks:     true,
    VerifyState:      true,
    ProgressInterval: 10 * time.Second,
}
migrator := chainmigrate.NewChainMigrator(exporter, importer, migratorConfig)

// Run migration
ctx := context.Background()
if err := migrator.Migrate(ctx); err != nil {
    log.Fatalf("Migration failed: %v", err)
}
```

## Real-World Example: Lux Mainnet Regenesis

This framework was built to enable the migration of 1,074,617 blocks from the old Lux mainnet (network ID 96369) SubnetEVM to the new C-Chain implementation:

```bash
# Extract all blocks from SubnetEVM
./extract-all-blocks \
  --source=/path/to/lux-mainnet-96369/db/pebbledb \
  --dest=/tmp/cchain-blocks \
  --max=1074616 \
  --verify

# Migrate to C-Chain
./chainmigrate \
  --source-type=subnet-evm \
  --source-db=/tmp/cchain-blocks \
  --dest-type=c-chain \
  --dest-db=/tmp/cchain-migrated \
  --coreth-compat \
  --verify-blocks \
  --verify-state

# Start node with migrated data
LUX_IMPORTED_HEIGHT=1074616 \
CHAIN_DATA_DIR=/tmp/cchain-migrated \
luxd --network-id=96369 \
     --genesis-file=/tmp/lux-runtime-replay/genesis.json
```

### Results

- **Blocks Migrated**: 1,074,617
- **Treasury Balance**: 61.5 billion LUX preserved
- **Migration Time**: ~2 hours on M1 Mac
- **Database Size**: ~15GB compressed
- **Verification**: 100% block integrity maintained

## Adding New Chain Support

To add support for a new blockchain:

1. **Create an Exporter** implementing `ChainExporter`:
```go
type MyChainExporter struct {
    // Your fields
}

func (e *MyChainExporter) ExportBlocks(ctx context.Context, start, end uint64) (<-chan *BlockData, <-chan error) {
    // Implementation
}
// ... implement other methods
```

2. **Create an Importer** implementing `ChainImporter`:
```go
type MyChainImporter struct {
    // Your fields
}

func (i *MyChainImporter) ImportBlock(block *BlockData) error {
    // Implementation
}
// ... implement other methods
```

3. **Register in CLI tool** (cmd/chainmigrate/main.go):
```go
case "my-chain":
    return NewMyChainExporter(config), nil
```

## Performance Considerations

- **Batch Size**: Default 100 blocks, adjust based on block size
- **Parallel Workers**: Default 4, increase for faster migrations
- **Memory Usage**: Streaming design keeps memory usage constant
- **Database Type**: PebbleDB fastest for reads, LevelDB for writes
- **Network Latency**: For RPC-based exports (L2), use local nodes

## Testing

```bash
# Run unit tests
go test ./...

# Run integration test with test data
go test -tags=integration ./...

# Benchmark performance
go test -bench=. ./...
```

## Limitations

- **State Migration**: Only migrates state at the specified end block
- **Pruned Databases**: Cannot migrate from pruned databases (need full archive)
- **Chain-Specific Features**: Some features may require custom handling
- **Genesis Compatibility**: Genesis must be compatible between chains

## Future Enhancements

- [ ] Incremental migration support
- [ ] Multi-chain parallel migrations
- [ ] State diff migration (not just end state)
- [ ] Merkle proof verification
- [ ] Resume from checkpoint
- [ ] Cloud storage backend support
- [ ] Web UI for monitoring

## License

Same as Lux Node - see LICENSE file in repository root.

## Support

For issues or questions, please open an issue in the Lux Node repository.