// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/consensus/validators"
	consensusset "github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/utils/window"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
)

const (
	validatorSetsCacheSize        = 64
	maxRecentlyAcceptedWindowSize = 64
	minRecentlyAcceptedWindowSize = 16
	recentlyAcceptedWindowTTL     = 2 * time.Minute
)

var (
	_ validators.State = (*manager)(nil)

	errUnfinalizedHeight = errors.New("failed to fetch validator set at unfinalized height")
)

// Manager adds the ability to introduce newly accepted blocks IDs to the State
// interface.
type Manager interface {
	validators.State

	// OnAcceptedBlockID registers the ID of the latest accepted block.
	// It is used to update the [recentlyAccepted] sliding window.
	OnAcceptedBlockID(blkID ids.ID)
}

type State interface {
	GetTx(txID ids.ID) (*txs.Tx, status.Status, error)

	GetLastAccepted() ids.ID
	GetStatelessBlock(blockID ids.ID) (block.Block, error)

	// ApplyValidatorWeightDiffs iterates from [startHeight] towards the genesis
	// block until it has applied all of the diffs up to and including
	// [endHeight]. Applying the diffs modifies [validators].
	//
	// Invariant: If attempting to generate the validator set for
	// [endHeight - 1], [validators] must initially contain the validator
	// weights for [startHeight].
	//
	// Note: Because this function iterates towards the genesis, [startHeight]
	// should normally be greater than or equal to [endHeight].
	ApplyValidatorWeightDiffs(
		ctx context.Context,
		validators map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
		netID ids.ID,
	) error

	// ApplyValidatorPublicKeyDiffs iterates from [startHeight] towards the
	// genesis block until it has applied all of the diffs up to and including
	// [endHeight]. Applying the diffs modifies [validators].
	//
	// Invariant: If attempting to generate the validator set for
	// [endHeight - 1], [validators] must initially contain the validator
	// weights for [startHeight].
	//
	// Note: Because this function iterates towards the genesis, [startHeight]
	// should normally be greater than or equal to [endHeight].
	ApplyValidatorPublicKeyDiffs(
		ctx context.Context,
		validators map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
	) error
}

func NewManager(
	log log.Logger,
	cfg config.Config,
	state State,
	metrics metrics.Metrics,
	clk *mockable.Clock,
) Manager {
	return &manager{
		log:     log,
		cfg:     cfg,
		state:   state,
		metrics: metrics,
		clk:     clk,
		caches:  make(map[ids.ID]cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput]),
		recentlyAccepted: window.New[ids.ID](
			window.Config{
				Clock:   clk,
				MaxSize: maxRecentlyAcceptedWindowSize,
				MinSize: minRecentlyAcceptedWindowSize,
				TTL:     recentlyAcceptedWindowTTL,
			},
		),
	}
}

// calling exported functions.
type manager struct {
	log     log.Logger
	cfg     config.Config
	state   State
	metrics metrics.Metrics
	clk     *mockable.Clock

	// Maps caches for each net that is currently tracked.
	// Key: Net ID
	// Value: cache mapping height -> validator set map
	caches map[ids.ID]cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput]

	// sliding window of blocks that were recently accepted
	recentlyAccepted window.Window[ids.ID]
}

// GetMinimumHeight returns the height of the most recent block beyond the
// horizon of our recentlyAccepted window.
//
// Because the time between blocks is arbitrary, we're only guaranteed that
// the window's configured TTL amount of time has passed once an element
// expires from the window.
//
// To try to always return a block older than the window's TTL, we return the
// parent of the oldest element in the window (as an expired element is always
// guaranteed to be sufficiently stale). If we haven't expired an element yet
// in the case of a process restart, we default to the lastAccepted block's
// height which is likely (but not guaranteed) to also be older than the
// window's configured TTL.
//
// If [UseCurrentHeight] is true, we override the block selection policy
// described above and we will always return the last accepted block height
// as the minimum.
func (m *manager) GetMinimumHeight(ctx context.Context) (uint64, error) {
	if m.cfg.UseCurrentHeight {
		return m.getCurrentHeight(ctx)
	}

	oldest, ok := m.recentlyAccepted.Oldest()
	if !ok {
		return m.getCurrentHeight(ctx)
	}

	blk, err := m.state.GetStatelessBlock(oldest)
	if err != nil {
		return 0, err
	}

	// We subtract 1 from the height of [oldest] because we want the height of
	// the last block accepted before the [recentlyAccepted] window.
	//
	// There is guaranteed to be a block accepted before this window because the
	// first block added to [recentlyAccepted] window is >= height 1.
	return blk.Height() - 1, nil
}

// GetCurrentHeight without context to implement validators.State
func (m *manager) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return m.getCurrentHeight(ctx)
}

// GetCurrentHeightWithContext with context for internal use
func (m *manager) GetCurrentHeightWithContext(ctx context.Context) (uint64, error) {
	return m.getCurrentHeight(ctx)
}

func (m *manager) getCurrentHeight(context.Context) (uint64, error) {
	lastAcceptedID := m.state.GetLastAccepted()
	lastAccepted, err := m.state.GetStatelessBlock(lastAcceptedID)
	if err != nil {
		return 0, err
	}
	return lastAccepted.Height(), nil
}

// GetValidatorSet implements validators.State
func (m *manager) GetValidatorSet(
	ctx context.Context,
	targetHeight uint64,
	netID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return m.GetValidatorSetWithContext(ctx, targetHeight, netID)
}

// GetValidatorSetWithContext returns detailed validator information
func (m *manager) GetValidatorSetWithContext(
	ctx context.Context,
	targetHeight uint64,
	netID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	validatorSetsCache := m.getValidatorSetCache(netID)

	if validatorSet, ok := validatorSetsCache.Get(targetHeight); ok {
		m.metrics.IncValidatorSetsCached()
		return validatorSet, nil
	}

	// get the start time to track metrics
	startTime := m.clk.Time()

	var (
		validatorSet  map[ids.NodeID]*validators.GetValidatorOutput
		currentHeight uint64
		err           error
	)
	if netID == constants.PrimaryNetworkID {
		validatorSet, currentHeight, err = m.makePrimaryNetworkValidatorSet(ctx, targetHeight)
	} else {
		validatorSet, currentHeight, err = m.makeNetValidatorSet(ctx, targetHeight, netID)
	}
	if err != nil {
		return nil, err
	}

	// cache the validator set
	validatorSetsCache.Put(targetHeight, validatorSet)

	duration := m.clk.Time().Sub(startTime)
	m.metrics.IncValidatorSetsCreated()
	m.metrics.AddValidatorSetsDuration(duration)
	m.metrics.AddValidatorSetsHeightDiff(currentHeight - targetHeight)
	return validatorSet, nil
}

func (m *manager) getValidatorSetCache(netID ids.ID) cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput] {
	// Only cache tracked subnets
	if netID != constants.PrimaryNetworkID && !m.cfg.TrackedSubnets.Contains(netID) {
		return &cache.Empty[uint64, map[ids.NodeID]*validators.GetValidatorOutput]{}
	}

	validatorSetsCache, exists := m.caches[netID]
	if exists {
		return validatorSetsCache
	}

	validatorSetsCache = &cache.LRU[uint64, map[ids.NodeID]*validators.GetValidatorOutput]{
		Size: validatorSetsCacheSize,
	}
	m.caches[netID] = validatorSetsCache
	return validatorSetsCache
}

func (m *manager) makePrimaryNetworkValidatorSet(
	ctx context.Context,
	targetHeight uint64,
) (map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	validatorSet, currentHeight, err := m.getCurrentPrimaryValidatorSet(ctx)
	if err != nil {
		return nil, 0, err
	}
	if currentHeight < targetHeight {
		return nil, 0, fmt.Errorf("%w with NetID = %s: current P-chain height (%d) < requested P-Chain height (%d)",
			errUnfinalizedHeight,
			constants.PrimaryNetworkID,
			currentHeight,
			targetHeight,
		)
	}

	// Rebuild primary network validators at [targetHeight]
	//
	// Note: Since we are attempting to generate the validator set at
	// [targetHeight], we want to apply the diffs from
	// (targetHeight, currentHeight]. Because the state interface is implemented
	// to be inclusive, we apply diffs in [targetHeight + 1, currentHeight].
	lastDiffHeight := targetHeight + 1
	err = m.state.ApplyValidatorWeightDiffs(
		ctx,
		validatorSet,
		currentHeight,
		lastDiffHeight,
		constants.PlatformChainID,
	)
	if err != nil {
		return nil, 0, err
	}

	err = m.state.ApplyValidatorPublicKeyDiffs(
		ctx,
		validatorSet,
		currentHeight,
		lastDiffHeight,
	)
	return validatorSet, currentHeight, err
}

func (m *manager) getCurrentPrimaryValidatorSet(
	ctx context.Context,
) (map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	// primaryMap := m.cfg.Validators.GetMap(constants.PrimaryNetworkID)
	primaryMap := make(map[ids.NodeID]*validators.GetValidatorOutput)
	currentHeight, err := m.getCurrentHeight(ctx)
	return primaryMap, currentHeight, err
}

func (m *manager) makeNetValidatorSet(
	ctx context.Context,
	targetHeight uint64,
	netID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	subnetValidatorSet, primaryValidatorSet, currentHeight, err := m.getCurrentValidatorSets(ctx, netID)
	if err != nil {
		return nil, 0, err
	}
	if currentHeight < targetHeight {
		return nil, 0, fmt.Errorf("%w with NetID = %s: current P-chain height (%d) < requested P-Chain height (%d)",
			errUnfinalizedHeight,
			netID,
			currentHeight,
			targetHeight,
		)
	}

	// Rebuild net validators at [targetHeight]
	//
	// Note: Since we are attempting to generate the validator set at
	// [targetHeight], we want to apply the diffs from
	// (targetHeight, currentHeight]. Because the state interface is implemented
	// to be inclusive, we apply diffs in [targetHeight + 1, currentHeight].
	lastDiffHeight := targetHeight + 1
	err = m.state.ApplyValidatorWeightDiffs(
		ctx,
		subnetValidatorSet,
		currentHeight,
		lastDiffHeight,
		netID,
	)
	if err != nil {
		return nil, 0, err
	}

	// Update the net validator set to include the public keys at
	// [currentHeight]. When we apply the public key diffs, we will convert
	// these keys to represent the public keys at [targetHeight]. If the subnet
	// validator is not currently a primary network validator, it doesn't have a
	// key at [currentHeight].
	for nodeID, vdr := range subnetValidatorSet {
		if primaryVdr, ok := primaryValidatorSet[nodeID]; ok {
			vdr.PublicKey = primaryVdr.PublicKey
		} else {
			vdr.PublicKey = nil
		}
	}

	err = m.state.ApplyValidatorPublicKeyDiffs(
		ctx,
		subnetValidatorSet,
		currentHeight,
		lastDiffHeight,
	)
	return subnetValidatorSet, currentHeight, err
}

func (m *manager) getCurrentValidatorSets(
	ctx context.Context,
	netID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, map[ids.NodeID]*validators.GetValidatorOutput, uint64, error) {
	// GetMap doesn't exist, so we need to build the map from validators
	subnetMap := make(map[ids.NodeID]*validators.GetValidatorOutput)
	primaryMap := make(map[ids.NodeID]*validators.GetValidatorOutput)
	
	// For now, return empty maps
	currentHeight, err := m.getCurrentHeight(ctx)
	return subnetMap, primaryMap, currentHeight, err
}

func (m *manager) GetNetID(_ context.Context, chainID ids.ID) (ids.ID, error) {
	if chainID == constants.PlatformChainID {
		return constants.PrimaryNetworkID, nil
	}

	chainTx, _, err := m.state.GetTx(chainID)
	if err != nil {
		return ids.Empty, fmt.Errorf(
			"problem retrieving blockchain %q: %w",
			chainID,
			err,
		)
	}
	chain, ok := chainTx.Unsigned.(*txs.CreateChainTx)
	if !ok {
		return ids.Empty, fmt.Errorf("%q is not a blockchain", chainID)
	}
	return chain.NetID, nil
}

func (m *manager) OnAcceptedBlockID(blkID ids.ID) {
	m.recentlyAccepted.Add(blkID)
}

func (m *manager) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return m.GetValidatorSet(ctx, height, netID)
}

func (m *manager) GetCurrentValidatorSet(
	ctx context.Context,
	netID ids.ID,
) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	// For now, return an empty map with current height
	// This is a stub implementation that needs to be properly implemented
	currentHeight, err := m.getCurrentHeight(ctx)
	if err != nil {
		return nil, 0, err
	}

	result := make(map[ids.ID]*validators.GetCurrentValidatorOutput)
	return result, currentHeight, nil
}

// AddStaker implements validators.Manager interface
// This is required for consensus compatibility but not used in platformvm
func (m *manager) AddStaker(subnetID ids.ID, nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	// This method is not used by platformvm as it manages validators through state changes
	// Return nil for interface compatibility
	return nil
}

// RemoveWeight implements validators.Manager interface
func (m *manager) RemoveWeight(subnetID ids.ID, nodeID ids.NodeID, weight uint64) error {
	// This method is not used by platformvm as it manages validators through state changes
	// Return nil for interface compatibility
	return nil
}

// GetWeight implements validators.Manager interface
func (m *manager) GetWeight(subnetID ids.ID, nodeID ids.NodeID) uint64 {
	// Delegate to GetValidatorSet for actual weight retrieval
	ctx := context.Background()
	currentHeight, err := m.getCurrentHeight(ctx)
	if err != nil {
		return 0
	}
	
	validators, err := m.GetValidatorSet(ctx, currentHeight, subnetID)
	if err != nil {
		return 0
	}
	
	if validator, ok := validators[nodeID]; ok {
		return validator.Weight
	}
	return 0
}

// GetSubnetID implements validators.Manager interface
func (m *manager) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	return m.GetNetID(ctx, chainID)
}

// SubsetWeight implements validators.Manager interface
func (m *manager) SubsetWeight(subnetID ids.ID, nodeIDs consensusset.Set[ids.NodeID]) (uint64, error) {
	ctx := context.Background()
	currentHeight, err := m.getCurrentHeight(ctx)
	if err != nil {
		return 0, err
	}
	
	validators, err := m.GetValidatorSet(ctx, currentHeight, subnetID)
	if err != nil {
		return 0, err
	}
	
	var totalWeight uint64
	for nodeID := range nodeIDs {
		if validator, ok := validators[nodeID]; ok {
			totalWeight += validator.Weight
		}
	}
	return totalWeight, nil
}

// TotalWeight implements validators.Manager interface
func (m *manager) TotalWeight(subnetID ids.ID) (uint64, error) {
	ctx := context.Background()
	currentHeight, err := m.getCurrentHeight(ctx)
	if err != nil {
		return 0, err
	}
	
	validators, err := m.GetValidatorSet(ctx, currentHeight, subnetID)
	if err != nil {
		return 0, err
	}
	
	var totalWeight uint64
	for _, validator := range validators {
		totalWeight += validator.Weight
	}
	return totalWeight, nil
}

// GetValidator implements validators.Manager interface
func (m *manager) GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*validators.GetValidatorOutput, bool) {
	ctx := context.Background()
	currentHeight, err := m.getCurrentHeight(ctx)
	if err != nil {
		return nil, false
	}
	
	validatorSet, err := m.GetValidatorSet(ctx, currentHeight, subnetID)
	if err != nil {
		return nil, false
	}
	
	if validatorOutput, ok := validatorSet[nodeID]; ok {
		// Return the validator output
		return validatorOutput, true
	}
	return nil, false
}

// GetValidatorIDs implements validators.Manager interface
func (m *manager) GetValidatorIDs(subnetID ids.ID) []ids.NodeID {
	ctx := context.Background()
	currentHeight, err := m.getCurrentHeight(ctx)
	if err != nil {
		return nil
	}
	
	validators, err := m.GetValidatorSet(ctx, currentHeight, subnetID)
	if err != nil {
		return nil
	}
	
	nodeIDs := make([]ids.NodeID, 0, len(validators))
	for nodeID := range validators {
		nodeIDs = append(nodeIDs, nodeID)
	}
	return nodeIDs
}

// Count implements validators.Manager interface
func (m *manager) Count(subnetID ids.ID) int {
	ctx := context.Background()
	currentHeight, err := m.getCurrentHeight(ctx)
	if err != nil {
		return 0
	}
	
	validators, err := m.GetValidatorSet(ctx, currentHeight, subnetID)
	if err != nil {
		return 0
	}
	
	return len(validators)
}

// RegisterSetCallbackListener implements validators.Manager interface
func (m *manager) RegisterSetCallbackListener(subnetID ids.ID, listener validators.SetCallbackListener) {
	// This is typically used for logging changes, but not critical for basic operation
	// For now, we'll just ignore it
}

// RegisterWeightCallbackListener is not part of the consensus interface
// It was removed as WeightCallbackListener doesn't exist in consensus package

// GetValidators implements validators.Manager interface
func (m *manager) GetValidators(netID ids.ID) (validators.Set, error) {
	// For now, return nil
	// This may need to be properly implemented based on actual requirements
	return nil, nil
}

// TotalLight implements validators.Manager interface
func (m *manager) TotalLight(netID ids.ID) (uint64, error) {
	// TotalLight is the same as TotalWeight
	return m.TotalWeight(netID)
}

// String implements validators.Manager interface
func (m *manager) String() string {
	return "platformvm.validators.Manager"
}
