# LLM Context for Lux Network Node

## Project Overview

This is the core node implementation for the Lux Network. The node enables
validation of multiple L1,L2,L3 blockchains in parallel using a single node
instance.

## Key Features and Changes

### 1. Multi-Consensus Architecture
- **Purpose**: Enable a single node to validate multiple L1 blockchains simultaneously
- **Implementation Status**: Architecture designed, implementation pending
- **Key Components**:
  - ConsensusModule interface (to be designed)
  - Multi-consensus manager (to be implemented)
  - Network isolation and routing (to be implemented)

### 2. Database Improvements
- **Default Backend**: Changed from LevelDB to BadgerDB for better performance
- **Implementation**:
  - BadgerDB set as default in EVM module
  - All database backends (LevelDB, PebbleDB, BadgerDB) pass test suite
  - Database factory in `/luxfi/database` package

### 3. Network Upgrades Simplification
- **GenesisRules**: Always active, no upgrade logic needed
- **Benefits**: Simplified configuration and reduced complexity
- **Changes Made**:
  - Removed upgrade checks from EVM config
  - Updated test files to work with simplified rules
  - Created network upgrades guide documentation

### 4. Logger Interface Adaptation
- **Issue**: Database factory requires `log.Logger` but node provides `logging.Logger`
- **Solution**: Created adapter at `/node/utils/logging/logadapter/adapter.go`
- **Implementation**: Handles method signature differences including the Crit method

## Recent Development Work

### Fixed Issues
1. **Database Module**: Fixed subpackage structure and imports
2. **Node Module**: Fixed RPC and remaining compilation issues
3. **EVM Module**: Fixed remaining issues and set BadgerDB as default
4. **Certificate Migration**: Converted from `staking.Certificate` to `ids.Certificate`
5. **Protobuf Imports**: Fixed imports to use correct node module paths
6. **Genesis Divide by Zero**: Added guard clause for zero splits case
7. **Database Health Check**: Created wrapper for interface compatibility

### Current Status
- CLI tools (luxd, tmpnetctl) build successfully
- Node starts but encounters database initialization errors
- Multiple "closed" messages suggest database lifecycle management issues

## Key Files and Locations

### Core Components
- `/node/node.go`: Main node implementation with database initialization
- `/utils/logging/logadapter/adapter.go`: Logger interface adapter
- `/chains/manager.go`: Chain management and initialization
- `/genesis/genesis.go`: Genesis configuration and allocation

### Configuration
- `/dev-genesis.json`: Simple genesis configuration for development
- `/simple-genesis.json`: Minimal genesis for testing

### Test Infrastructure
- `/tests/e2e/`: End-to-end test suite
- `/tests/fixture/tmpnet/`: Temporary network test fixtures

## Development Commands

### Building
```bash
make                    # Build luxd binary
make tmpnetctl         # Build tmpnetctl for test networks
```

### Running a Node
```bash
# Development mode (single node)
./build/luxd --dev

# With custom genesis
./build/luxd --data-dir=/tmp/luxd-data --genesis-file=genesis.json

# Test network with tmpnetctl
./build/tmpnetctl start-network --node-count 1 --luxd-path ./build/luxd
```

## Known Issues

### Database Initialization Error
- **Symptom**: Node crashes with "not found" and multiple "closed" errors
- **Cause**: Database lifecycle management or initialization sequence issue
- **Workaround**: Under investigation

### Network Deployment
- tmpnetctl starts nodes but they crash immediately
- Direct luxd execution with --dev flag encounters same database error

## Architecture Decisions

### Why BadgerDB?
- Better performance for blockchain workloads
- Native Go implementation (no CGO dependencies)
- Efficient memory usage and compression

### Why Remove Network Upgrades?
- Simplifies configuration for new networks
- GenesisRules always active removes upgrade complexity
- Cleaner codebase with fewer conditional paths

## Next Steps

### Immediate Priority
1. Fix database initialization error preventing node startup
2. Deploy local development network
3. Setup 5-node validator network with staking

### Multi-Consensus Implementation
1. Design ConsensusModule interface
2. Implement multi-consensus manager
4. Implement network isolation and routing
5. Add monitoring endpoints for multi-consensus operation

### Testing
1. Verify single node operation
2. Test 5-node network with staking
3. Deploy and test L2 subnet
4. Validate multi-consensus architecture

## Integration Points

### With Other Lux Components
- **Bridge**: Will use node RPC endpoints for cross-chain operations
- **Wallet**: Connects via JSON-RPC API
- **Explorer**: Indexes blockchain data from node
- **SDK**: Uses node APIs for blockchain interactions

### External Dependencies
- `github.com/luxfi/database`: Database abstraction layer
- `github.com/luxfi/crypto`: Cryptographic primitives
- `github.com/ethereum/go-ethereum`: EVM implementation
- `google.golang.org/protobuf`: Protocol buffer serialization

## Security Considerations

1. **Staking Keys**: Generated and stored in data directory
2. **API Access**: Admin APIs disabled by default in production
3. **Network Security**: P2P communication uses TLS
4. **Database Security**: Local file access only

## Debugging Tips

1. **Logs**: Check `~/.luxd/logs/main.log` for detailed output
2. **Database**: Ensure clean data directory for fresh start
3. **Ports**: Default HTTP port 9650, staking port 9651
4. **Genesis**: Verify genesis hash matches expected value
