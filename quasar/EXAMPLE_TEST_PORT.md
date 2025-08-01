# Example Test Port: Consensus Test

This document shows a concrete example of porting a test from Avalanche to Lux.

## Original Avalanche Test Structure

### File: `snow/consensus/snowman/consensus_test.go`

```go
package snowman

import (
    "context"
    "testing"
    
    "github.com/stretchr/testify/require"
    
    "github.com/ava-labs/avalanchego/ids"
    "github.com/ava-labs/avalanchego/snow/consensus/snowball"
    "github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
    "github.com/ava-labs/avalanchego/snow/snowtest"
    "github.com/ava-labs/avalanchego/utils/bag"
)

func TestConsensus(t *testing.T) {
    require := require.New(t)
    
    // Setup parameters
    params := snowball.Parameters{
        K:               1,
        AlphaPreference: 1,
        AlphaConfidence: 1,
        Beta:            2,
    }
    
    // Create consensus instance
    sm := &Topological{}
    
    // Create context
    ctx := snowtest.Context(t, snowtest.PChainID)
    
    // Initialize
    require.NoError(sm.Initialize(ctx, params, GenesisID, GenesisHeight, GenesisTimestamp))
    
    // Create and add blocks
    block := snowmantest.BuildChild(snowmantest.Genesis)
    require.NoError(sm.Add(block))
    
    // Record poll
    votes := bag.Of(block.ID())
    require.NoError(sm.RecordPoll(context.Background(), votes))
    
    // Verify state
    require.Equal(block.ID(), sm.Preference())
}
```

## Ported Lux Test Structure

### File: `consensus/chain/consensus_test.go`

```go
package chain

import (
    "context"
    "testing"
    
    "github.com/stretchr/testify/require"
    
    "github.com/lux/node/consensus/chain/chaintest"
    "github.com/lux/node/consensus/consensustest"
    "github.com/lux/node/consensus/params"
    "github.com/lux/node/ids"
    "github.com/lux/node/utils/bag"
)

// Test with mainnet parameters (21 nodes)
func TestConsensusMainnet(t *testing.T) {
    require := require.New(t)
    
    // Use Lux mainnet parameters
    parameters := params.DefaultParameters
    
    // Create consensus instance
    consensus := &Topological{}
    
    // Create Lux context
    ctx := consensustest.Context(t, consensustest.TestChainID)
    
    // Initialize with Lux parameters
    require.NoError(consensus.Initialize(
        ctx,
        parameters,
        chaintest.GenesisID,
        chaintest.GenesisHeight,
        chaintest.GenesisTimestamp,
    ))
    
    // Create and add blocks using Lux test helpers
    block := chaintest.BuildChild(chaintest.Genesis)
    require.NoError(consensus.Add(block))
    
    // Record poll
    votes := bag.Of(block.ID())
    require.NoError(consensus.RecordPoll(context.Background(), votes))
    
    // Verify state
    require.Equal(block.ID(), consensus.Preference())
}

// Test with testnet parameters (11 nodes)
func TestConsensusTestnet(t *testing.T) {
    require := require.New(t)
    
    // Use Lux testnet parameters
    parameters := params.TestnetParameters
    
    // Create consensus instance
    consensus := &Topological{}
    
    // Create Lux context
    ctx := consensustest.Context(t, consensustest.TestChainID)
    
    // Initialize with testnet parameters
    require.NoError(consensus.Initialize(
        ctx,
        parameters,
        chaintest.GenesisID,
        chaintest.GenesisHeight,
        chaintest.GenesisTimestamp,
    ))
    
    // Test with multiple blocks
    blocks := chaintest.BuildChain(5)
    for _, block := range blocks[1:] { // Skip genesis
        require.NoError(consensus.Add(block))
    }
    
    // Simulate voting rounds
    for i := 0; i < parameters.Beta; i++ {
        votes := bag.Of(blocks[len(blocks)-1].ID())
        require.NoError(consensus.RecordPoll(context.Background(), votes))
    }
    
    // Verify finalization
    require.Equal(blocks[len(blocks)-1].ID(), consensus.Preference())
}

// Test with local parameters (5 nodes)
func TestConsensusLocal(t *testing.T) {
    require := require.New(t)
    
    // Use Lux local parameters
    parameters := params.LocalParameters
    
    // Similar test structure...
}

// Test edge cases specific to Lux
func TestConsensusQuorumThresholds(t *testing.T) {
    testCases := []struct {
        name   string
        params params.Parameters
    }{
        {"Mainnet", params.DefaultParameters},
        {"Testnet", params.TestnetParameters},
        {"Local", params.LocalParameters},
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            require := require.New(t)
            
            consensus := &Topological{}
            ctx := consensustest.Context(t, consensustest.TestChainID)
            
            require.NoError(consensus.Initialize(
                ctx,
                tc.params,
                chaintest.GenesisID,
                chaintest.GenesisHeight,
                chaintest.GenesisTimestamp,
            ))
            
            // Test quorum calculations
            require.Equal(tc.params.AlphaPreference, consensus.QuorumThreshold())
            require.Equal(tc.params.AlphaConfidence, consensus.ConfidenceThreshold())
        })
    }
}
```

## Key Changes in the Port

### 1. Import Updates
- `github.com/ava-labs/avalanchego/` → `github.com/lux/node/`
- `snow/consensus/snowman` → `consensus/chain`
- `snowtest` → `consensustest`
- `snowmantest` → `chaintest`

### 2. Parameter Updates
- Generic snowball parameters → Specific Lux network parameters
- Added tests for all three network configurations (mainnet/testnet/local)

### 3. Test Structure
- Added separate test functions for each network configuration
- Added table-driven tests for parameter validation
- More comprehensive coverage of Lux-specific features

### 4. Helper Usage
- `snowtest.Context` → `consensustest.Context`
- `snowmantest.BuildChild` → `chaintest.BuildChild`
- Network-specific test scenarios

## Creating Test Helpers

### consensustest/context.go
```go
package consensustest

import (
    "testing"
    
    "github.com/stretchr/testify/require"
    
    "github.com/lux/node/consensus"
    "github.com/lux/node/database/badgerdb"
    "github.com/lux/node/ids"
)

var (
    TestChainID = ids.GenerateTestID()
    TestSubnetID = ids.GenerateTestID()
)

func Context(t *testing.T, chainID ids.ID) *consensus.Context {
    require := require.New(t)
    
    // Use BadgerDB for tests
    db := badgerdb.New(t.TempDir())
    
    return &consensus.Context{
        NetworkID: 1,
        SubnetID:  TestSubnetID,
        ChainID:   chainID,
        NodeID:    ids.GenerateTestNodeID(),
        
        // Lux-specific configurations
        Database: db,
        // ... other fields
    }
}
```

### chaintest/builders.go
```go
package chaintest

import (
    "time"
    
    "github.com/lux/node/consensus/chain"
    "github.com/lux/node/ids"
)

var (
    GenesisID        = ids.GenerateTestID()
    GenesisHeight    = uint64(0)
    GenesisTimestamp = time.Unix(1, 0)
    
    Genesis = &Block{
        IDV:        GenesisID,
        HeightV:    GenesisHeight,
        TimestampV: GenesisTimestamp,
        StatusV:    Accepted,
    }
)

func BuildChild(parent *Block) *Block {
    return &Block{
        IDV:        ids.GenerateTestID(),
        ParentV:    parent.ID(),
        HeightV:    parent.Height() + 1,
        TimestampV: parent.Timestamp().Add(time.Second),
        StatusV:    Processing,
    }
}

func BuildChain(length int) []*Block {
    chain := make([]*Block, length)
    chain[0] = Genesis
    
    for i := 1; i < length; i++ {
        chain[i] = BuildChild(chain[i-1])
    }
    
    return chain
}
```

## Testing the Port

### 1. Run Individual Tests
```bash
go test -v -run TestConsensusMainnet ./consensus/chain/
go test -v -run TestConsensusTestnet ./consensus/chain/
go test -v -run TestConsensusLocal ./consensus/chain/
```

### 2. Run with Race Detector
```bash
go test -race ./consensus/chain/
```

### 3. Run Benchmarks
```bash
go test -bench=. ./consensus/chain/
```

### 4. Check Coverage
```bash
go test -cover ./consensus/chain/
```

## Common Pitfalls and Solutions

### 1. Timing Issues
**Problem**: Tests fail due to different consensus timing
**Solution**: Use network-specific parameters and timeouts

### 2. Database Differences
**Problem**: Tests expect LevelDB behavior
**Solution**: Always use BadgerDB in tests

### 3. Mock Incompatibility
**Problem**: Mocks don't match new interfaces
**Solution**: Regenerate mocks with mockgen

### 4. Parameter Mismatches
**Problem**: Tests use wrong consensus parameters
**Solution**: Always use params package constants

This example demonstrates the complete process of porting a test from Avalanche to Lux, including all necessary adaptations and best practices.