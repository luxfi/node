# Database Abstraction in Lux Node

## Overview

The Lux node now uses a fully abstracted database layer that allows switching between different database backends at runtime without requiring specific imports in the node code. This provides flexibility and makes it easier to test and deploy with different storage engines.

## Database Strategy

- **PebbleDB** (default) - Used for initial replay of existing subnet EVM blocks during genesis
- **BadgerDB** - New backend for all blockchains after replay is complete
- **LevelDB** - Legacy support for existing deployments
- **MemDB** - In-memory database for testing

## Migration Process

1. Start with existing subnet EVM data in PebbleDB format
2. Use `--genesis-db` to replay all blocks during node initialization
3. After replay completes, the node continues with BadgerDB for all new blocks
4. This provides a clean migration path from subnet to C-Chain with optimized storage

## Configuration

### Default Database Type

Use the `--db-type` flag to set the default database for all chains:

```bash
./build/luxd --db-type=pebbledb  # Default for all chains
```

### Per-Chain Database Configuration

You can override the database type for specific chains:

```bash
./build/luxd \
  --db-type=pebbledb \              # Default for all chains
  --p-chain-db-type=badgerdb \      # P-Chain uses BadgerDB
  --x-chain-db-type=badgerdb \      # X-Chain uses BadgerDB
  --c-chain-db-type=badgerdb        # C-Chain uses BadgerDB
```

This allows fine-grained control over storage backends:
- Use PebbleDB for genesis replay from existing subnet data
- Use BadgerDB for production blockchains after replay
- Mix and match based on chain requirements

### Database Paths

Each database type uses a specific subdirectory:
- BadgerDB: `<data-dir>/db/badger/`
- PebbleDB: `<data-dir>/db/pebble/`
- LevelDB: `<data-dir>/db/<version>/`
- MemDB: No persistent storage

## Genesis Database Replay

The node supports replaying blocks from an existing database (of any supported type) during genesis initialization. This is useful for:
- Migrating from subnet EVM to C-Chain
- Switching database backends while preserving state
- Testing with existing blockchain data

### Usage

```bash
./build/luxd \
  --network-id=96369 \
  --db-type=badgerdb \
  --genesis-db=/path/to/existing/database \
  --genesis-db-type=pebbledb \
  --data-dir=/path/to/new/data
```

Parameters:
- `--genesis-db`: Path to the existing database to replay from
- `--genesis-db-type`: Type of the genesis database (leveldb, pebbledb, or badgerdb)
- `--db-type`: Type of database to use for the new node

## Building with Database Support

### Build Tags

The build system automatically includes support for all database types:

```bash
# Using make (recommended)
make build

# Direct go build
go build -tags "pebbledb badgerdb" -o luxd ./main
```

### Makefile Targets

```bash
make build          # Build with all database support
make test           # Run tests
make clean          # Clean build artifacts
```

## Architecture

### Database Factory

The `database/factory` package provides a unified interface for creating databases:

```go
db, err := factory.New(
    dbType,      // "leveldb", "pebbledb", "badgerdb", "memdb"
    dbPath,      // Path to database directory
    readOnly,    // Read-only mode
    config,      // Optional configuration
    gatherer,    // Metrics gatherer
    logger,      // Logger instance
    prefix,      // Metrics prefix
    regName,     // Registry name
)
```

### Node Integration

The node code no longer imports specific database implementations. Instead, it uses the abstract `database.Database` interface:

```go
// No specific database imports needed
import "github.com/luxfi/database"
import "github.com/luxfi/database/factory"

// Create database using factory
n.DB, err = factory.New(
    n.Config.DatabaseConfig.Name,
    dbPath,
    n.Config.ReadOnly,
    nil,
    n.MeterDBMetricsGatherer,
    n.Log,
    "db",
    "all",
)
```

## Testing

### Unit Tests

Run database-specific tests:

```bash
cd database
go test -v -tags "pebbledb badgerdb" ./...
```

### Integration Tests

Test database replay functionality:

```bash
cd genesis
./bin/genesis test replay --source-db /path/to/db --source-type pebbledb
```

## Migration Guide

### From Hardcoded Database

If you have existing code that imports specific databases:

```go
// Old approach
import "github.com/luxfi/database/leveldb"
db, err := leveldb.New(path, 512, 512, 1024)

// New approach
import "github.com/luxfi/database/factory"
db, err := factory.New("leveldb", path, false, nil, gatherer, logger, "db", "all")
```

### Switching Database Types

To switch an existing node to a different database type:

1. Stop the node
2. Use the genesis tool to export state:
   ```bash
   ./bin/genesis export state /old/db/path /export/path --db-type=leveldb
   ```
3. Start node with new database type and genesis-db:
   ```bash
   ./build/luxd --db-type=badgerdb --genesis-db=/export/path --genesis-db-type=leveldb
   ```

## Performance Considerations

- **BadgerDB**: Best for write-heavy workloads, built-in compression
- **PebbleDB**: Optimized for SSDs, good balance of read/write performance
- **LevelDB**: Mature and stable, good general-purpose performance
- **MemDB**: Testing only, no persistence

## Troubleshooting

### Database Type Not Recognized

Ensure the binary was built with the correct tags:
```bash
make clean
make build
```

### Genesis Replay Fails

Check that:
1. The genesis-db path exists and is readable
2. The genesis-db-type matches the actual database type
3. The database is not corrupted

### Performance Issues

1. Check disk I/O metrics
2. Consider switching database types based on workload
3. Adjust database-specific configuration if needed