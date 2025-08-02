// Copyright (C) 2024-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adapter

import (
	"context"
	"errors"
	"time"

	qparams "github.com/luxfi/node/quasar/params"
	qtopological "github.com/luxfi/node/quasar/chain"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/quasar/choices"
	"github.com/luxfi/node/quasar/engine/chain"
	"github.com/luxfi/node/quasar/engine/core"
	"github.com/luxfi/node/quasar/networking/sender"
	"github.com/luxfi/node/quasar/validators"
)

var (
	errNotImplemented = errors.New("not implemented")
	_ chain.Engine    = (*ChainAdapter)(nil)
)

// ChainAdapter adapts the external consensus protocol (nova) to the node's chain.Engine interface
type ChainAdapter struct {
	ctx         *core.Context
	vm          chain.VM
	sender      sender.Sender
	validators  validators.State
	params      qparams.Parameters
	consensus   *qtopological.Topological
	blockMap    map[ids.ID]chain.Block
	preferredID ids.ID
	initialized bool
}

// NewChainAdapter creates a new adapter for linear chain consensus
func NewChainAdapter() *ChainAdapter {
	return &ChainAdapter{
		blockMap: make(map[ids.ID]chain.Block),
	}
}

// Initialize initializes the chain consensus engine
func (ca *ChainAdapter) Initialize(ctx context.Context, params chain.Parameters) error {
	engineCtx, ok := ctx.Value("engineContext").(*core.Context)
	if !ok {
		return errors.New("missing engine context")
	}
	
	ca.ctx = engineCtx
	
	// Extract VM, sender, and validators from context
	// This is a simplified initialization - in practice, these would come from the chain manager
	ca.vm, _ = params.ConsensusParams.(chain.VM)
	
	// Convert chain parameters to consensus parameters
	consensusParams := qparams.Parameters{
		K:                     21, // Default for mainnet
		AlphaPreference:       13,
		AlphaConfidence:       18,
		Beta:                  8,
		ConcurrentRepolls:     4,
		OptimalProcessing:     50,
		MaxOutstandingItems:   256,
		MaxItemProcessingTime: 10 * time.Second,
	}
	
	ca.params = consensusParams
	
	// Create nova consensus instance
	ca.consensus = &qtopological.Topological{}
	
	// Get last accepted block
	lastAcceptedID, err := ca.vm.LastAccepted(ctx)
	if err != nil {
		return err
	}
	
	lastAcceptedBlk, err := ca.vm.GetBlock(ctx, lastAcceptedID)
	if err != nil {
		return err
	}
	
	// Initialize nova consensus
	consensusCtx := context.WithValue(ctx, "consensus", ca.ctx)
	
	// Convert params.Parameters to chain.Parameters
	chainParams := qtopological.Parameters{
		K:                     consensusParams.K,
		AlphaPreference:       consensusParams.AlphaPreference,
		AlphaConfidence:       consensusParams.AlphaConfidence,
		Beta:                  consensusParams.Beta,
		ConcurrentRepolls:     consensusParams.ConcurrentRepolls,
		OptimalProcessing:     consensusParams.OptimalProcessing,
		MaxOutstandingItems:   consensusParams.MaxOutstandingItems,
		MaxItemProcessingTime: int64(consensusParams.MaxItemProcessingTime),
	}
	
	err = ca.consensus.Initialize(
		consensusCtx,
		chainParams,
		lastAcceptedID.String(),
		lastAcceptedBlk.Height(),
		lastAcceptedBlk.Time(),
	)
	if err != nil {
		return err
	}
	
	ca.initialized = true
	return nil
}

// GetBlock retrieves a block by its ID
func (ca *ChainAdapter) GetBlock(blkID ids.ID) (chain.Block, error) {
	if blk, ok := ca.blockMap[blkID]; ok {
		return blk, nil
	}
	
	blk, err := ca.vm.GetBlock(context.Background(), blkID)
	if err != nil {
		return nil, err
	}
	
	ca.blockMap[blkID] = blk
	return blk, nil
}

// GetAncestor retrieves an ancestor block at the given height
func (ca *ChainAdapter) GetAncestor(blkID ids.ID, height uint64) (ids.ID, error) {
	return ca.vm.GetAncestor(context.Background(), blkID, height)
}

// LastAccepted returns the ID of the last accepted block
func (ca *ChainAdapter) LastAccepted() (ids.ID, uint64) {
	// Return the preferredID and a placeholder height
	// In a real implementation, we'd track the last accepted height
	return ca.preferredID, 0
}

// VerifyHeightIndex returns whether height index is enabled
func (ca *ChainAdapter) VerifyHeightIndex() error {
	return ca.vm.VerifyHeightIndex(context.Background())
}

// Start starts the consensus engine
func (ca *ChainAdapter) Start(ctx context.Context, startReqID uint32) error {
	if !ca.initialized {
		return errors.New("engine not initialized")
	}
	// Use no-op logger for now
	return nil
}

// Stop stops the consensus engine
func (ca *ChainAdapter) Stop(ctx context.Context) error {
	// Use no-op logger for now
	return nil
}

// Notify notifies the engine of an event
func (ca *ChainAdapter) Notify(ctx context.Context, msg core.Message) error {
	return nil
}

// Context returns the engine's context
func (ca *ChainAdapter) Context() *core.Context {
	return ca.ctx
}

// HealthCheck returns the engine's health status
func (ca *ChainAdapter) HealthCheck(ctx context.Context) (interface{}, error) {
	return ca.consensus.HealthCheck(ctx)
}

// GetVM returns the VM associated with this engine
func (ca *ChainAdapter) GetVM() interface{} {
	return ca.vm
}

// Put is called when a container is received
func (ca *ChainAdapter) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	blk, err := ca.vm.ParseBlock(ctx, container)
	if err != nil {
		return err
	}
	
	blkIDStr := blk.ID()
	blkID, _ := ids.FromString(blkIDStr)
	ca.blockMap[blkID] = blk
	
	// For now, just store the block
	// TODO: Integrate with nova consensus when proper interface is available
	return nil
}

// Get retrieves a container and its ancestors
func (ca *ChainAdapter) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	blk, err := ca.GetBlock(containerID)
	if err != nil {
		// We don't have the block, request it
		ca.sender.SendGetAccepted(ctx, nodeID, requestID, []ids.ID{containerID})
		return nil
	}
	// We have the block, send it
	ca.sender.SendPut(ctx, nodeID, requestID, blk.Bytes())
	return nil
}

// PushQuery sends a query for a block
func (ca *ChainAdapter) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkBytes []byte, requestedHeight uint64) error {
	blk, err := ca.vm.ParseBlock(ctx, blkBytes)
	if err != nil {
		return err
	}
	
	blkIDStr := blk.ID()
	blkID, _ := ids.FromString(blkIDStr)
	ca.blockMap[blkID] = blk
	
	// Record poll with the block
	votes := []ids.ID{blkID}
	return ca.recordPoll(ctx, votes)
}

// PullQuery sends a query for a block ID
func (ca *ChainAdapter) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkID ids.ID, requestedHeight uint64) error {
	// If we have the block, vote for it
	if _, err := ca.GetBlock(blkID); err == nil {
		votes := []ids.ID{blkID}
		return ca.recordPoll(ctx, votes)
	}
	
	// Otherwise, we need to fetch it
	ca.sender.SendGet(context.Background(), nodeID, requestID, blkID)
	return nil
}

// Chits handles chits (votes) from a node
func (ca *ChainAdapter) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID) error {
	votes := []ids.ID{preferredID}
	return ca.recordPoll(ctx, votes)
}

// QueryFailed handles a failed query
func (ca *ChainAdapter) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// Record an empty poll
	return ca.recordPoll(ctx, nil)
}

// recordPoll records a poll with the consensus engine
func (ca *ChainAdapter) recordPoll(ctx context.Context, votes []ids.ID) error {
	return ca.consensus.RecordPoll(ctx, votes)
}

// GetAcceptedFrontier returns the accepted frontier
func (ca *ChainAdapter) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	lastAcceptedID, _ := ca.LastAccepted()
	ca.sender.SendAcceptedFrontier(context.Background(), nodeID, requestID, lastAcceptedID)
	return nil
}

// AcceptedFrontier handles an accepted frontier message
func (ca *ChainAdapter) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	return nil
}

// GetAccepted returns accepted blocks
func (ca *ChainAdapter) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	acceptedIDs := make([]ids.ID, 0, len(containerIDs))
	for _, blkID := range containerIDs {
		if blk, err := ca.GetBlock(blkID); err == nil {
			if blk.Status() == choices.Accepted {
				acceptedIDs = append(acceptedIDs, blkID)
			}
		}
	}
	ca.sender.SendAccepted(ctx, nodeID, requestID, acceptedIDs)
	return nil
}

// Accepted handles accepted blocks notification
func (ca *ChainAdapter) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	return nil
}

// Halt halts the engine
func (ca *ChainAdapter) Halt() {
	// Use no-op logger for now
}

// Timeout handles a timeout event
func (ca *ChainAdapter) Timeout() error {
	return nil
}

// Gossip handles a gossip message
func (ca *ChainAdapter) Gossip() error {
	return nil
}

// Connected handles a node connection event
func (ca *ChainAdapter) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	return nil
}

// Disconnected handles a node disconnection event
func (ca *ChainAdapter) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	return nil
}

// GetStateSummaryFrontier returns the state summary frontier
func (ca *ChainAdapter) GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// For now, return empty summary
	ca.sender.SendStateSummaryFrontier(ctx, nodeID, requestID, []byte{})
	return nil
}

// StateSummaryFrontier is called when the state summary frontier is received
func (ca *ChainAdapter) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	return nil
}

// GetAcceptedStateSummary retrieves the state summary for the given block heights
func (ca *ChainAdapter) GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, heights []uint64) error {
	// For now, return empty summaries
	ca.sender.SendAcceptedStateSummary(ctx, nodeID, requestID, []ids.ID{})
	return nil
}

// AcceptedStateSummary is called when the requested state summary is received
func (ca *ChainAdapter) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) error {
	return nil
}

// AppRequest is called when an application request is received
func (ca *ChainAdapter) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}

// AppResponse is called when an application response is received
func (ca *ChainAdapter) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return nil
}

// AppGossip is called when an application gossip message is received
func (ca *ChainAdapter) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return nil
}

// CrossChainAppRequest is called when a cross-chain application request is received
func (ca *ChainAdapter) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error {
	return nil
}

// CrossChainAppResponse is called when a cross-chain application response is received
func (ca *ChainAdapter) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	return nil
}

// Ancestors is called when a container and its ancestors are received
func (ca *ChainAdapter) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error {
	for _, container := range containers {
		blk, err := ca.vm.ParseBlock(ctx, container)
		if err != nil {
			continue
		}
		blkIDStr := blk.ID()
		blkID, _ := ids.FromString(blkIDStr)
		ca.blockMap[blkID] = blk
	}
	return nil
}

// GetAncestors retrieves a container and its ancestors
func (ca *ChainAdapter) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	// For now, just send the requested container if we have it
	if blk, err := ca.GetBlock(containerID); err == nil {
		ca.sender.SendAncestors(ctx, nodeID, requestID, [][]byte{blk.Bytes()})
	}
	return nil
}

// Shutdown the engine
func (ca *ChainAdapter) Shutdown(ctx context.Context) error {
	return nil
}

// chainBlock wraps a chain.Block for nova consensus
type chainBlock struct {
	block chain.Block
	ca    *ChainAdapter
}

func (cb *chainBlock) ID() string {
	return cb.block.ID()
}

func (cb *chainBlock) Parent() ids.ID {
	return cb.block.Parent()
}

func (cb *chainBlock) Height() uint64 {
	return cb.block.Height()
}

func (cb *chainBlock) Timestamp() time.Time {
	return time.Unix(int64(cb.block.Time()), 0)
}

func (cb *chainBlock) Verify() error {
	return cb.block.Verify(context.Background())
}

func (cb *chainBlock) Bytes() []byte {
	return cb.block.Bytes()
}

func (cb *chainBlock) Accept(ctx context.Context) error {
	return cb.block.Accept()
}

func (cb *chainBlock) Reject(ctx context.Context) error {
	return cb.block.Reject()
}

func (cb *chainBlock) Status() choices.Status {
	return cb.block.Status()
}

// blockAcceptor handles block acceptance for nova consensus
type blockAcceptor struct {
	ca *ChainAdapter
}

func (ba *blockAcceptor) Accept(ctx context.Context, blkID ids.ID, bytes []byte) error {
	// Log block acceptance - use no-op logger for now
	return nil
}

// registererAdapter adapts core.Registerer to interfaces.Registerer
type registererAdapter struct {
	reg core.Registerer
}

func (ra *registererAdapter) Register(collector interface{}) error {
	// For now, just return nil as we don't have direct access to prometheus.Collector
	return nil
}

