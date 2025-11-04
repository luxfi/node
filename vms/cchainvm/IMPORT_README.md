# EVM to C-Chain Import Tool

## Overview

This tool provides a comprehensive solution for importing EVM blockchain data into the C-Chain VM format. It handles the critical namespace prefix issue where EVM uses a 32-byte prefix (337fb73f...) that C-Chain doesn't expect.

## Features

- **Namespace Prefix Stripping**: Automatically removes the 32-byte EVM namespace prefix
- **Key Format Translation**: Converts EVM key formats to C-Chain compatible formats
- **Batch Processing**: Efficiently processes blocks in configurable batches
- **State Rebuilding**: Reconstructs state trie during import
- **Progress Tracking**: Real-time progress reporting and ETA calculation
- **Wallet Monitoring**: Tracks specific wallet addresses during import
- **VM Integration**: Direct integration with C-Chain VM for seamless operation

## Components

### 1. Core Import Module (`import.go`)
- Main import logic with namespace handling
- Database abstraction layers for Pebble and LevelDB
- State reconstruction and verification
- Transaction processing and receipt generation

### 2. VM Integration (`import_integration.go`)
- Hooks for C-Chain VM integration
- Configuration management
- VM lifecycle coordination
- Status tracking

### 3. CLI Tool (`run_import.go`)
- Standalone command-line interface
- Interactive confirmation
- Progress visualization

### 4. Configuration (`import_config.json`)
- JSON-based configuration
- Customizable parameters

## Usage

### Quick Start

```bash
# Build the import tool
cd /home/z/work/lux/node/vms/cchainvm
go build -o import_tool run_import.go import.go import_integration.go

# Run with default settings (imports 1,082,780 blocks)
./import_tool

# Run with custom parameters
./import_tool \
  -source /home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb \
  -target /home/z/work/lux/node/chaindata/cchain \
  -end 1082780 \
  -batch 5000 \
  -workers 8
```

### Using Configuration File

```bash
# Edit configuration
vi import_config.json

# Run with config file
./import_tool -config import_config.json
```

### Integration with C-Chain VM

```go
// In your VM initialization code
vm := &VM{...}

// Import EVM database
err := vm.ImportEVMDatabase(
    "/path/to/subnet/db",
    1082780, // target height
)
```

## Configuration Options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `source_path` | EVM database path | Required |
| `target_path` | C-Chain database path | Required |
| `start_block` | Starting block number | 0 |
| `end_block` | Ending block number | 1082780 |
| `batch_size` | Blocks per batch | 5000 |
| `workers` | Parallel workers | 8 |
| `target_wallet` | Wallet to monitor | Optional |
| `verify_state` | Verify state roots | false |
| `rebuild_state` | Rebuild state trie | true |
| `strip_namespace` | Strip 32-byte prefix | true |

## Technical Details

### Namespace Prefix Issue

EVM databases use a 32-byte namespace prefix (337fb73f...) on all keys. This tool:

1. **Detects** namespace prefixes by checking for the 337fb73f pattern
2. **Strips** the 32-byte prefix during key translation
3. **Translates** remaining key format to C-Chain compatible format
4. **Preserves** data integrity during conversion

### Database Compatibility

- **Source**: Pebble DB (EVM format)
- **Target**: LevelDB (C-Chain format)
- **Key Translation**: Automatic format conversion
- **State Migration**: Full state trie reconstruction

### Performance Optimization

- **Batch Processing**: Reduces write operations
- **Parallel Workers**: Concurrent block processing
- **Memory Management**: Configurable cache sizes
- **Progress Tracking**: Real-time ETA calculation

## Import Process

1. **Initialization**
   - Open source database (EVM/Pebble)
   - Create target database (C-Chain/LevelDB)
   - Initialize blockchain instance

2. **Block Processing**
   - Read blocks from source
   - Strip namespace prefixes
   - Translate key formats
   - Process transactions
   - Rebuild state

3. **State Reconstruction**
   - Process state nodes
   - Update account balances
   - Track contract storage
   - Verify state roots (optional)

4. **Finalization**
   - Commit final batch
   - Update chain metadata
   - Generate import report

## Monitoring

During import, the tool provides:

```
[2025-01-01 12:00:00] Import progress
  current=500000
  target=1082780
  processed=500000
  txs=1234567
  state=98765
  blocks/sec=1234.56
  eta=8m30s

[2025-01-01 12:00:00] Wallet status
  address=0x9011E888251AB053B7bD1cdB598Db4f9DEd94714
  balance=1000000000000000000
  nonce=42
```

## Error Handling

The tool handles various error scenarios:

- **Missing Blocks**: Logs warning and continues
- **Invalid State**: Optional verification and retry
- **Database Errors**: Graceful shutdown with state preservation
- **Memory Issues**: Automatic cache adjustment

## Troubleshooting

### Common Issues

1. **"Missing canonical hash"**
   - Some blocks may not have canonical hashes
   - Tool continues processing other blocks

2. **"State root mismatch"**
   - Enable state verification with `-verify`
   - Check source database integrity

3. **"Database locked"**
   - Ensure no other process is using the database
   - Check file permissions

### Performance Tuning

```bash
# For faster import (less verification)
./import_tool -batch 10000 -workers 16 -verify=false

# For safer import (more verification)
./import_tool -batch 1000 -workers 4 -verify=true
```

## Integration Examples

### Direct VM Integration

```go
// In C-Chain VM initialization
func (vm *VM) Initialize(...) error {
    // ... existing initialization ...

    // Check if import is needed
    if vm.needsImport() {
        log.Info("Importing EVM database")

        err := vm.ImportEVMDatabase(
            vm.config.ImportSource,
            vm.config.ImportHeight,
        )
        if err != nil {
            return fmt.Errorf("import failed: %w", err)
        }
    }

    // Continue with normal initialization
    return vm.initBlockchain()
}
```

### Standalone Import Script

```bash
#!/bin/bash
# import_subnet.sh

SOURCE="/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
TARGET="/home/z/work/lux/node/chaindata/cchain"
HEIGHT=1082780
WALLET="0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"

echo "Starting EVM import..."
./import_tool \
    -source "$SOURCE" \
    -target "$TARGET" \
    -end "$HEIGHT" \
    -wallet "$WALLET" \
    -batch 5000 \
    -workers 8 \
    -rebuild=true \
    -strip-namespace=true

echo "Import complete!"
```

## Success Criteria

The import is successful when:

1. ✅ All 1,082,780 blocks are processed
2. ✅ Target wallet balance is preserved
3. ✅ State can be queried correctly
4. ✅ Transactions can be replayed
5. ✅ C-Chain VM starts with imported data

## Notes

- The import process is idempotent - it can be safely rerun
- Partial imports are supported (use start/end parameters)
- The tool preserves all transaction receipts and logs
- State verification is optional but recommended for critical imports
- The namespace stripping is essential for EVM compatibility

## Support

For issues or questions:
1. Check the logs in the target directory
2. Verify source database integrity
3. Ensure sufficient disk space (2x source size recommended)
4. Review configuration parameters