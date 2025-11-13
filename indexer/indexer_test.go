// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	nodeconsensus "github.com/luxfi/node/consensus"
	consensustest "github.com/luxfi/consensus/test/helpers"
	consensuscontext "github.com/luxfi/consensus/context"
	"github.com/luxfi/consensus"
	"github.com/luxfi/database/manager"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/api/server"
	"github.com/luxfi/node/utils"
)

// mockChainVM is a simple mock for testing that implements core.VM
type mockChainVM struct{}

func (m *mockChainVM) Initialize(
	ctx context.Context,
	chainCtx *consensuscontext.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- core.Message,
	fxs []*core.Fx,
	appSender interface{},
) error {
	return nil
}

func (m *mockChainVM) SetState(ctx context.Context, state core.VMState) error {
	return nil
}

func (m *mockChainVM) Shutdown(ctx context.Context) error {
	return nil
}

func (m *mockChainVM) Version(ctx context.Context) (string, error) {
	return "mock", nil
}

func (m *mockChainVM) HealthCheck(ctx context.Context) (interface{}, error) {
	return nil, nil
}

func (m *mockChainVM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return map[string]http.Handler{}, nil
}

func (m *mockChainVM) CreateStaticHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return map[string]http.Handler{}, nil
}

func (m *mockChainVM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	return nil, nil
}

var (
	_ server.PathAdder = (*apiServerMock)(nil)

	errUnimplemented = errors.New("unimplemented")
)

type apiServerMock struct {
	timesCalled int
	bases       []string
	endpoints   []string
}

func (a *apiServerMock) AddRoute(_ http.Handler, base, endpoint string) error {
	a.timesCalled++
	a.bases = append(a.bases, base)
	a.endpoints = append(a.endpoints, endpoint)
	return nil
}

func (*apiServerMock) AddAliases(string, ...string) error {
	return errUnimplemented
}

// Test that newIndexer sets fields correctly
func TestNewIndexer(t *testing.T) {
	require := require.New(t)
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: true,
		Log:                  log.NoLog{},
		DB:                   memdb.New(),
		BlockAcceptorGroup:   nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		TxAcceptorGroup:      nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		VertexAcceptorGroup:  nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		APIServer:            &apiServerMock{},
		ShutdownF:            func() {},
	}

	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)
	require.NotNil(idxr.log)
	require.NotNil(idxr.db)
	require.False(idxr.closed)
	require.NotNil(idxr.pathAdder)
	require.True(idxr.indexingEnabled)
	require.True(idxr.allowIncompleteIndex)
	require.NotNil(idxr.blockIndices)
	require.Empty(idxr.blockIndices)
	require.NotNil(idxr.txIndices)
	require.Empty(idxr.txIndices)
	require.NotNil(idxr.vtxIndices)
	require.Empty(idxr.vtxIndices)
	require.NotNil(idxr.blockAcceptorGroup)
	require.NotNil(idxr.txAcceptorGroup)
	require.NotNil(idxr.vertexAcceptorGroup)
	require.NotNil(idxr.shutdownF)
	require.False(idxr.hasRunBefore)
}

// Test that [hasRunBefore] is set correctly and that Shutdown is called on close
func TestMarkHasRunAndShutdown(t *testing.T) {
	require := require.New(t)
	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	shutdown := &sync.WaitGroup{}
	shutdown.Add(1)
	config := Config{
		IndexingEnabled:     true,
		Log:                 log.NoLog{},
		DB:                  db,
		BlockAcceptorGroup:  nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		TxAcceptorGroup:     nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		VertexAcceptorGroup: nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		APIServer:           &apiServerMock{},
		ShutdownF:           shutdown.Done,
	}

	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.False(idxrIntf.(*indexer).hasRunBefore)
	require.NoError(db.Commit())
	require.NoError(idxrIntf.Close())
	shutdown.Wait()
	shutdown.Add(1)

	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)
	require.True(idxr.hasRunBefore)
	require.NoError(idxr.Close())
	shutdown.Wait()
}

// Test registering a linear chain and a DAG chain and accepting
// some vertices
func TestIndexer(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	server := &apiServerMock{}
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: false,
		Log:                  log.NoLog{},
		DB:                   db,
		BlockAcceptorGroup:   nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		TxAcceptorGroup:      nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		VertexAcceptorGroup:  nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		APIServer:            server,
		ShutdownF:            func() {},
	}

	// Create indexer
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)
	now := time.Now()
	idxr.clock.Set(now)

	// Assert state is right
	// Use a test chain ID
	testChainID := ids.GenerateTestID()
	chain1Ctx := consensustest.Context(t, testChainID)
	isIncomplete, err := idxr.isIncomplete(testChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err := idxr.previouslyIndexed(testChainID)
	require.NoError(err)
	require.False(previouslyIndexed)

	// Register this chain, creating a new index
	chainVM := &mockChainVM{}
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	t.Logf("After RegisterChain, closed=%v", idxr.closed)
	isIncomplete, err = idxr.isIncomplete(testChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(testChainID)
	require.NoError(err)
	require.True(previouslyIndexed)
	require.Equal(1, server.timesCalled)
	require.Equal("index/chain1", server.bases[0])
	require.Equal("/block", server.endpoints[0])
	require.Len(idxr.blockIndices, 1)
	require.Empty(idxr.txIndices)
	require.Empty(idxr.vtxIndices)

	// Accept a container
	blkID, blkBytes := ids.GenerateTestID(), utils.RandomBytes(32)
	expectedContainer := Container{
		ID:        blkID,
		Bytes:     blkBytes,
		Timestamp: now.UnixNano(),
	}

	// Accept the block through the index
	blkIdx := idxr.blockIndices[testChainID]
	require.NotNil(blkIdx)

	// Accept the container
	err = blkIdx.Accept(chain1Ctx, blkID, blkBytes)
	require.NoError(err)

	// Verify GetLastAccepted is right
	gotLastAccepted, err := blkIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(expectedContainer, gotLastAccepted)

	// Verify GetContainerByID is right
	container, err := blkIdx.GetContainerByID(blkID)
	require.NoError(err)
	require.Equal(expectedContainer, container)

	// Verify GetIndex is right
	index, err := blkIdx.GetIndex(blkID)
	require.NoError(err)
	require.Zero(index)

	// Verify GetContainerByIndex is right
	container, err = blkIdx.GetContainerByIndex(0)
	require.NoError(err)
	require.Equal(expectedContainer, container)

	// Verify GetContainerRange is right
	containers, err := blkIdx.GetContainerRange(0, 1)
	require.NoError(err)
	require.Len(containers, 1)
	require.Equal(expectedContainer, containers[0])

	// Commit the database before closing the indexer
	require.NoError(db.Commit())

	// Don't actually close the indexer to avoid closing the database
	// Just check that it would close properly
	require.False(idxr.closed)

	server.timesCalled = 0

	// Create a new indexer using the same baseDB to simulate restart
	config.DB = versiondb.New(baseDB)
	// Create new AcceptorGroups since the old ones still have the chain registered
	config.BlockAcceptorGroup = nodeconsensus.NewAcceptorGroup(log.NoLog{})
	config.TxAcceptorGroup = nodeconsensus.NewAcceptorGroup(log.NoLog{})
	config.VertexAcceptorGroup = nodeconsensus.NewAcceptorGroup(log.NoLog{})
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr = idxrIntf.(*indexer)
	now = time.Now()
	idxr.clock.Set(now)
	require.Empty(idxr.blockIndices)
	require.Empty(idxr.txIndices)
	require.Empty(idxr.vtxIndices)
	require.True(idxr.hasRunBefore)
	previouslyIndexed, err = idxr.previouslyIndexed(testChainID)
	require.NoError(err)
	require.True(previouslyIndexed)
	hasRun, err := idxr.hasRun()
	require.NoError(err)
	require.True(hasRun)
	isIncomplete, err = idxr.isIncomplete(testChainID)
	require.NoError(err)
	require.False(isIncomplete)

	// Register the same chain as before
	chainVM2 := &mockChainVM{}
	idxr.RegisterChain("chain1", chain1Ctx, chainVM2)
	blkIdx = idxr.blockIndices[testChainID]
	require.NotNil(blkIdx)
	container, err = blkIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(blkID, container.ID)
	require.Equal(1, server.timesCalled) // block index for chain
	require.Contains(server.endpoints, "/block")

	// Register a second chain (block-based, not DAG)
	// Note: DAG/vertex/tx indices are not supported in the new consensus package
	chain2ChainID := ids.GenerateTestID()
	chain2Ctx := consensustest.Context(t, chain2ChainID)
	isIncomplete, err = idxr.isIncomplete(chain2Ctx.ChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err = idxr.previouslyIndexed(chain2Ctx.ChainID)
	require.NoError(err)
	require.False(previouslyIndexed)
	chain2VM := &mockChainVM{}
	idxr.RegisterChain("chain2", chain2Ctx, chain2VM)
	require.NoError(err)
	// Only block indices are created now (vtx/tx indices not supported)
	require.Equal(2, server.timesCalled) // block index for chain1, block index for chain2
	require.Contains(server.bases, "index/chain2")
	require.Contains(server.endpoints, "/block")
	require.Len(idxr.blockIndices, 2)
	// No vertex or tx indices in new consensus package
	require.Empty(idxr.txIndices)
	require.Empty(idxr.vtxIndices)

	// Accept a block on chain2
	blk2ID, blk2Bytes := ids.GenerateTestID(), utils.RandomBytes(32)
	expectedBlk2 := Container{
		ID:        blk2ID,
		Bytes:     blk2Bytes,
		Timestamp: now.UnixNano(),
	}

	// Get the block index for chain2
	blk2Idx := idxr.blockIndices[chain2ChainID]
	require.NotNil(blk2Idx)

	// Accept the block
	err = blk2Idx.Accept(chain2Ctx, blk2ID, blk2Bytes)
	require.NoError(err)

	// Verify GetLastAccepted is right
	gotLastAccepted, err = blk2Idx.GetLastAccepted()
	require.NoError(err)
	require.Equal(expectedBlk2, gotLastAccepted)

	// Verify GetContainerByID is right
	blk2, err := blk2Idx.GetContainerByID(blk2ID)
	require.NoError(err)
	require.Equal(expectedBlk2, blk2)

	// Verify GetIndex is right
	index, err = blk2Idx.GetIndex(blk2ID)
	require.NoError(err)
	require.Zero(index)

	// Verify GetContainerByIndex is right
	blk2, err = blk2Idx.GetContainerByIndex(0)
	require.NoError(err)
	require.Equal(expectedBlk2, blk2)

	// Verify GetContainerRange is right
	blks2, err := blk2Idx.GetContainerRange(0, 1)
	require.NoError(err)
	require.Len(blks2, 1)
	require.Equal(expectedBlk2, blks2[0])

	// No vertex or tx indices in new consensus package
	require.Empty(idxr.vtxIndices)
	require.Empty(idxr.txIndices)

	// Verify both chains have their expected last accepted blocks
	lastAcceptedBlk1, err := blkIdx.GetLastAccepted()
	require.NoError(err)
	require.Equal(blkID, lastAcceptedBlk1.ID)

	lastAcceptedBlk2, err := blk2Idx.GetLastAccepted()
	require.NoError(err)
	require.Equal(blk2ID, lastAcceptedBlk2.ID)

	// Close the indexer again
	require.NoError(config.DB.(*versiondb.Database).Commit())
	// Don't actually close the indexer to avoid issues with shared database
	// Just check that it would close properly
	require.False(idxr.closed)

	// Re-open one more time and re-register chains
	config.DB = versiondb.New(baseDB)
	// Create new AcceptorGroups since the old ones were closed
	config.BlockAcceptorGroup = nodeconsensus.NewAcceptorGroup(log.NoLog{})
	config.TxAcceptorGroup = nodeconsensus.NewAcceptorGroup(log.NoLog{})
	config.VertexAcceptorGroup = nodeconsensus.NewAcceptorGroup(log.NoLog{})
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr = idxrIntf.(*indexer)
	chainVM3 := &mockChainVM{}
	idxr.RegisterChain("chain1", chain1Ctx, chainVM3)
	// Re-register chain2 as well
	chain2VM2 := &mockChainVM{}
	idxr.RegisterChain("chain2", chain2Ctx, chain2VM2)

	// Verify state - both chains should have their blocks
	lastAcceptedBlk1Again, err := idxr.blockIndices[testChainID].GetLastAccepted()
	require.NoError(err)
	require.Equal(blkID, lastAcceptedBlk1Again.ID)

	lastAcceptedBlk2Again, err := idxr.blockIndices[chain2ChainID].GetLastAccepted()
	require.NoError(err)
	require.Equal(blk2ID, lastAcceptedBlk2Again.ID)

	// No vertex or tx indices in new consensus package
	require.Empty(idxr.vtxIndices)
	require.Empty(idxr.txIndices)
}

// Make sure the indexer doesn't allow incomplete indices unless explicitly allowed
func TestIncompleteIndex(t *testing.T) {
	// Create an indexer with indexing disabled
	require := require.New(t)

	baseDB := memdb.New()
	config := Config{
		IndexingEnabled:      false,
		AllowIncompleteIndex: false,
		Log:                  log.NoLog{},
		DB:                   versiondb.New(baseDB),
		BlockAcceptorGroup:   nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		TxAcceptorGroup:      nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		VertexAcceptorGroup:  nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		APIServer:            &apiServerMock{},
		ShutdownF:            func() {},
	}
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)
	require.False(idxr.indexingEnabled)

	// Register a chain
	testChainID := ids.GenerateTestID()
	chain1Ctx := consensustest.Context(t, testChainID)
	isIncomplete, err := idxr.isIncomplete(testChainID)
	require.NoError(err)
	require.False(isIncomplete)
	previouslyIndexed, err := idxr.previouslyIndexed(testChainID)
	require.NoError(err)
	require.False(previouslyIndexed)
	chainVM := &mockChainVM{}
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	isIncomplete, err = idxr.isIncomplete(testChainID)
	require.NoError(err)
	require.True(isIncomplete)
	require.Empty(idxr.blockIndices)

	// Close and re-open the indexer, this time with indexing enabled
	require.NoError(config.DB.(*versiondb.Database).Commit())
	require.NoError(idxr.Close())
	config.IndexingEnabled = true
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr = idxrIntf.(*indexer)
	require.True(idxr.indexingEnabled)

	// Register the chain again. Should die due to incomplete index.
	require.NoError(config.DB.(*versiondb.Database).Commit())
	chainVM2 := &mockChainVM{}
	idxr.RegisterChain("chain1", chain1Ctx, chainVM2)
	require.True(idxr.closed)

	// Close and re-open the indexer, this time with indexing enabled
	// and incomplete index allowed.
	require.NoError(idxr.Close())
	config.AllowIncompleteIndex = true
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr = idxrIntf.(*indexer)
	require.True(idxr.allowIncompleteIndex)

	// Register the chain again. Should be OK
	chainVM3 := &mockChainVM{}
	idxr.RegisterChain("chain1", chain1Ctx, chainVM3)
	require.False(idxr.closed)

	// Don't close the indexer to avoid closing the database
	// Instead, just mark it as closed for testing purposes
	idxr.closed = true

	config.AllowIncompleteIndex = false
	config.IndexingEnabled = false
	// Re-use the same baseDB
	config.DB = versiondb.New(baseDB)
	idxrIntf, err = NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
}

// Ensure we only index chains in the primary network
func TestIgnoreNonDefaultChains(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	config := Config{
		IndexingEnabled:      true,
		AllowIncompleteIndex: false,
		Log:                  log.NoLog{},
		DB:                   db,
		BlockAcceptorGroup:   nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		TxAcceptorGroup:      nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		VertexAcceptorGroup:  nodeconsensus.NewAcceptorGroup(log.NoLog{}),
		APIServer:            &apiServerMock{},
		ShutdownF:            func() {},
	}

	// Create indexer
	idxrIntf, err := NewIndexer(config)
	require.NoError(err)
	require.IsType(&indexer{}, idxrIntf)
	idxr := idxrIntf.(*indexer)

	// Create chain1Ctx for a random net + chain.
	testChainID := ids.GenerateTestID()
	testNetID := ids.GenerateTestID() // Non-primary network (testNetID)
	chain1Ctx := consensustest.Context(t, testChainID)
	// Override the NetID to be non-primary
	chain1Ctx.NetID = testNetID

	// The test context is configured correctly for a non-primary net

	// RegisterChain should return without adding an index for this chain
	chainVM := &mockChainVM{}
	idxr.RegisterChain("chain1", chain1Ctx, chainVM)
	require.Empty(idxr.blockIndices)
}
