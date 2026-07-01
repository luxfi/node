// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/metric"

	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/math"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/cache/metercacher"
	pqfinality "github.com/luxfi/node/consensus/quasar"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/runtime"
	"github.com/luxfi/timer/mockable"
	validators "github.com/luxfi/validators"
	vmcore "github.com/luxfi/vm"
	vmchain "github.com/luxfi/vm/chain"

	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/node/vms/proposervm/state"
	"github.com/luxfi/node/vms/proposervm/tree"

	statelessblock "github.com/luxfi/node/vms/proposervm/block"
)

const (
	// DefaultMinBlockDelay should be kept as whole seconds because block
	// timestamps are only specific to the second.
	DefaultMinBlockDelay = time.Second
	// DefaultNumHistoricalBlocks as 0 results in never deleting any historical
	// blocks.
	DefaultNumHistoricalBlocks uint64 = 0

	innerBlkCacheSize = 64 * constants.MiB
)

var (
	_ vmchain.ChainVM         = (*VM)(nil)
	_ vmchain.BatchedChainVM  = (*VM)(nil)
	_ vmchain.StateSyncableVM = (*VM)(nil)

	dbPrefix = []byte("proposervm")
)

func cachedBlockSize(_ ids.ID, blk vmchain.Block) int {
	return ids.IDLen + len(blk.Bytes()) + constants.PointerOverhead
}

type VM struct {
	vmchain.ChainVM
	Config
	blockBuilderVM vmchain.BuildBlockWithRuntimeChainVM
	batchedVM      vmchain.BatchedChainVM
	ssVM           vmchain.StateSyncableVM

	state.State

	proposer.Windower
	tree.Tree
	mockable.Clock

	lock           sync.Mutex
	rt             *runtime.Runtime
	db             *versiondb.Database
	logger         log.Logger
	validatorState validators.State
	netIDsCache    cache.Cacher[ids.ID, ids.ID] // chainID -> netID cache for GetNetworkID lookups

	// verifiedBlocksLock guards [verifiedBlocks]. The consensus engine drives
	// the proposervm from multiple goroutines concurrently (e.g. a
	// PullQuery/Put handler verifying a block — which writes the map — while a
	// Qbit handler reads the same map via GetBlock), so every access to the map
	// below must hold this lock. It is a leaf lock: it is never held across a
	// callout, so it can never participate in a deadlock with [lock], the
	// [Tree], or the inner VM.
	verifiedBlocksLock sync.RWMutex
	// Block ID --> Block
	// Each element is a block that passed verification but
	// hasn't yet been accepted/rejected
	verifiedBlocks map[ids.ID]PostForkBlock
	// Stateless block ID --> inner block.
	// Only contains post-fork blocks near the tip so that the cache doesn't get
	// filled with random blocks every time this node parses blocks while
	// processing a GetAncestors message from a bootstrapping node.
	innerBlkCache  cache.Cacher[ids.ID, vmchain.Block]
	preferred      ids.ID
	consensusState uint32 // Consensus state: Syncing, Bootstrapping, Ready

	// lastAcceptedTime is set to the last accepted PostForkBlock's timestamp
	// if the last accepted block has been a PostForkOption block since having
	// initialized the VM.
	lastAcceptedTime time.Time

	// lastAcceptedHeight is set to the last accepted PostForkBlock's height.
	lastAcceptedHeight uint64

	// proposerBuildSlotGauge reports the slot index when this node may attempt
	// to build a block.
	proposerBuildSlotGauge metric.Gauge

	// acceptedBlocksSlotHistogram reports the slots that accepted blocks were
	// proposed in.
	acceptedBlocksSlotHistogram metric.Histogram

	// lastAcceptedTimestampGaugeVec reports timestamps for the last-accepted
	// [postForkBlock] and its inner block.
	lastAcceptedTimestampGaugeVec metric.GaugeVec

	// quasarGate is the OPTIONAL post-quantum finality-cert gate. nil (the
	// default) means PQ-finality verification is OFF — the accept path is
	// unchanged classical Snow. When set AND forward-dated activation is reached,
	// it requires a valid QuasarCert at every checkpoint and fails closed. See
	// consensus/quasar.
	quasarGate *pqfinality.Gate
}

// SetQuasarGate installs the post-quantum finality gate. Called once at chain
// wiring time when PQ-finality config is present; left unset (nil) otherwise so
// the accept path stays classical. Idempotent, set before consensus starts.
func (vm *VM) SetQuasarGate(g *pqfinality.Gate) { vm.quasarGate = g }

// verifyQuasarFinality is the accept-path hook for one finalized post-fork
// block. It is nil-safe and dormant-by-default: with no gate, or pre-activation,
// or off a checkpoint height, it returns nil and the block finalizes on the
// classical path unchanged. Post-activation at a checkpoint it requires a valid
// QuasarCert bound to this block and returns the verification error otherwise
// (fail closed — the caller surfaces it from Accept).
//
// BlockID binds the proposervm block id (the accepted block at this layer);
// StateRoot is left zero here (committed transitively through the block id), so
// the cert's StateRoot binding is not cross-checked at this layer.
func (vm *VM) verifyQuasarFinality(b *postForkBlock) error {
	// Fast path: no gate (the default) => zero cost, no block-accessor calls, no
	// checkpoint build. The classical accept path is untouched.
	if vm.quasarGate == nil {
		return nil
	}
	return vm.quasarGate.VerifyAccepted(pqfinality.Checkpoint{
		Epoch:   b.PChainEpoch().Number,
		Height:  b.Height(),
		BlockID: [32]byte(b.ID()),
	})
}

// New performs best when [minBlkDelay] is whole seconds. This is because block
// timestamps are only specific to the second.
func New(
	vm vmchain.ChainVM,
	config Config,
) *VM {
	blockBuilderVM, _ := vm.(vmchain.BuildBlockWithRuntimeChainVM)
	batchedVM, _ := vm.(vmchain.BatchedChainVM)
	ssVM, _ := vm.(vmchain.StateSyncableVM)
	return &VM{
		ChainVM:        vm,
		Config:         config,
		blockBuilderVM: blockBuilderVM,
		batchedVM:      batchedVM,
		ssVM:           ssVM,
	}
}

func (vm *VM) Initialize(
	ctx context.Context,
	init vmcore.Init,
) error {
	vm.rt = init.Runtime
	vm.logger = init.Log
	vm.db = versiondb.New(prefixdb.New(dbPrefix, init.DB))

	if vm.rt.ValidatorState == nil {
		return errors.New("validator state is required")
	}
	// Type assert validator state - we know it must be validators.State in full node
	if vs, ok := vm.rt.ValidatorState.(validators.State); ok {
		vm.validatorState = vs
	} else {
		return errors.New("invalid validator state type")
	}

	baseState, err := state.NewMetered(vm.db, "state", vm.Config.Registerer)
	if err != nil {
		return err
	}
	vm.State = baseState
	vm.Windower = vm.newWindower()
	vm.Tree = tree.New()
	registry, ok := vm.Config.Registerer.(metric.Registry)
	if !ok {
		return errors.New("registerer must be a Registry")
	}
	metrics := metric.NewWithRegistry("", registry)
	innerBlkCache, err := metercacher.New(
		"inner_block_cache",
		registry,
		lru.NewSizedCache(innerBlkCacheSize, cachedBlockSize),
	)
	if err != nil {
		return err
	}
	vm.innerBlkCache = innerBlkCache

	// Initialize ChainID cache for validator state lookups
	vm.netIDsCache = lru.NewCache[ids.ID, ids.ID](4096)

	vm.verifiedBlocks = make(map[ids.ID]PostForkBlock)

	err = vm.ChainVM.Initialize(ctx, init)
	if err != nil {
		return err
	}

	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		return fmt.Errorf("failed to repair accepted chain by height: %w", err)
	}

	if err := vm.setLastAcceptedMetadata(ctx); err != nil {
		return fmt.Errorf("failed to set last accepted metadata: %w", err)
	}

	if err := vm.pruneOldBlocks(); err != nil {
		return fmt.Errorf("failed to prune old blocks: %w", err)
	}

	forkHeight, err := vm.GetForkHeight()
	switch err {
	case nil:
		vm.logger.Info("initialized proposervm",
			log.String("state", "after fork"),
			log.Uint64("forkHeight", forkHeight),
			log.Uint64("lastAcceptedHeight", vm.lastAcceptedHeight),
		)
	case database.ErrNotFound:
		vm.logger.Info("initialized proposervm",
			log.String("state", "before fork"),
		)
	default:
		return fmt.Errorf("failed to get fork height: %w", err)
	}

	vm.proposerBuildSlotGauge = metrics.NewGauge(
		"block_building_slot",
		"the slot that this node may attempt to build a block",
	)
	vm.acceptedBlocksSlotHistogram = metrics.NewHistogram(
		"accepted_blocks_slot",
		"the slot accepted blocks were proposed in",
		// define the following ranges:
		// (-inf, 0]
		// (0, 1]
		// (1, 2]
		// (2, inf)
		// the usage of ".5" before was to ensure we work around the limitation
		// of comparing floating point of the same numerical value.
		[]float64{0.5, 1.5, 2.5},
	)
	vm.lastAcceptedTimestampGaugeVec = metrics.NewGaugeVec(
		"last_accepted_timestamp",
		"timestamp of the last block accepted",
		[]string{"block_type"},
	)

	// Metrics are automatically registered by the metrics instance
	return nil
}

// newWindower builds the proposer-schedule windower bound to the validator-set
// ID the consensus cert side resolves under: vm.Config.NetworkID, set once by
// chains/manager.go (constants.PrimaryNetworkID for native chains, else the
// L1's own chainID). CRITICAL-3: hardcoding PrimaryNetworkID here made a
// sovereign L1's windower call GetValidatorSet under the wrong ID, get an empty
// set, degrade to ErrAnyoneCanPropose, and equivocate exactly like the unfixed
// C-Chain — while diverging from the cert's set. The zero value (ids.Empty) IS
// constants.PrimaryNetworkID, so a native chain matches the original
// proposer.New(..., PrimaryNetworkID, ...) byte-for-byte.
func (vm *VM) newWindower() proposer.Windower {
	netID := vm.Config.NetworkID
	if netID == ids.Empty {
		netID = constants.PrimaryNetworkID
	}
	return proposer.New(vm.validatorState, netID, vm.rt.ChainID)
}

// Shutdown ops then propagate shutdown to innerVM
func (vm *VM) Shutdown(ctx context.Context) error {
	if err := vm.db.Commit(); err != nil {
		return err
	}
	// ChainVM doesn't have Shutdown in new consensus
	// return vm.ChainVM.Shutdown(ctx)
	return nil
}

func (vm *VM) SetState(ctx context.Context, newState uint32) error {
	if err := vm.ChainVM.SetState(ctx, newState); err != nil {
		return err
	}

	oldState := vm.consensusState
	vm.consensusState = newState
	if oldState != uint32(vmcore.Syncing) {
		return nil
	}

	// When finishing StateSyncing, if state sync has failed or was skipped,
	// repairAcceptedChainByHeight rolls back the chain to the previously last
	// accepted block. If state sync has completed successfully, this call is a
	// no-op.
	if err := vm.repairAcceptedChainByHeight(ctx); err != nil {
		return fmt.Errorf("failed to repair accepted chain height: %w", err)
	}
	return vm.setLastAcceptedMetadata(ctx)
}

func (vm *VM) BuildBlock(ctx context.Context) (vmchain.Block, error) {
	preferredBlock, err := vm.getBlock(ctx, vm.preferred)
	if err != nil {
		vm.logger.Error("unexpected build block failure",
			log.String("reason", "failed to fetch preferred block"),
			log.Stringer("parentID", vm.preferred),
			log.Err(err),
		)
		return nil, err
	}

	return preferredBlock.buildChild(ctx)
}

func (vm *VM) ParseBlock(ctx context.Context, b []byte) (vmchain.Block, error) {
	if blk, err := vm.parsePostForkBlock(ctx, b, true); err == nil {
		return blk, nil
	}
	return vm.parsePreForkBlock(ctx, b)
}

func (vm *VM) ParseLocalBlock(ctx context.Context, b []byte) (vmchain.Block, error) {
	if blk, err := vm.parsePostForkBlock(ctx, b, false); err == nil {
		return blk, nil
	}
	return vm.parsePreForkBlock(ctx, b)
}

func (vm *VM) GetBlock(ctx context.Context, id ids.ID) (vmchain.Block, error) {
	return vm.getBlock(ctx, id)
}

func (vm *VM) SetPreference(ctx context.Context, preferred ids.ID) error {
	// Short-circuit if already preferred - no context check needed
	if vm.preferred == preferred {
		return nil
	}

	// Check for context cancellation before any state changes
	if err := ctx.Err(); err != nil {
		return err
	}

	vm.preferred = preferred

	// Check for context cancellation before expensive operations
	if err := ctx.Err(); err != nil {
		return err
	}

	blk, err := vm.getBlock(ctx, preferred)
	if err != nil {
		vm.logger.Error("preferred block not found",
			log.Stringer("blkID", preferred),
			log.Err(err),
		)
		return fmt.Errorf("preferred block %s not found: %w", preferred, err)
	}

	// Check for context cancellation before delegating to inner VM
	if err := ctx.Err(); err != nil {
		return err
	}

	// For post-fork blocks, getInnerBlk() returns the inner (unwrapped) block
	// with a different ID than the proposer wrapper.
	// For pre-fork blocks, getInnerBlk() returns the block itself (same ID).
	// Always use the inner block ID to avoid passing wrapper IDs to the inner VM.
	innerBlkID := blk.getInnerBlk().ID()
	if err := vm.ChainVM.SetPreference(ctx, innerBlkID); err != nil {
		return err
	}

	vm.logger.Debug("set preference",
		log.Stringer("blkID", preferred),
		log.Stringer("innerBlkID", innerBlkID),
	)
	return nil
}

func (vm *VM) WaitForEvent(ctx context.Context) (vmcore.Message, error) {
	for {
		if err := ctx.Err(); err != nil {
			vm.logger.Debug("Aborting WaitForEvent, context is done", log.Err(err))
			return vmcore.Message{}, err
		}

		timeToBuild, shouldWait, err := vm.timeToBuild(ctx)
		if err != nil {
			vm.logger.Debug("Aborting WaitForEvent", log.Err(err))
			return vmcore.Message{}, err
		}

		// If we are pre-fork or haven't finished bootstrapping yet, we should
		// directly forward the inner VM's events.
		if !shouldWait {
			vm.logger.Debug("Waiting for inner VM event (pre-fork or before normal operation)")
			return vm.ChainVM.WaitForEvent(ctx)
		}

		duration := time.Until(timeToBuild)
		if duration <= 0 {
			vm.logger.Debug("Can build a block without waiting")
			return vm.ChainVM.WaitForEvent(ctx)
		}

		vm.logger.Debug("Waiting until we should build a block", log.Duration("duration", duration))

		// Wait until it is our turn to build a block.
		select {
		case <-ctx.Done():
		case <-time.After(duration):
			// We should not call ChainVM.WaitForEvent here as it is possible
			// that timeToBuild was capped less than the actual time for us to
			// build a block. If it is actually our turn to build, timeToBuild
			// will be <= 0 in the next iteration.
		}
	}
}

func (vm *VM) timeToBuild(ctx context.Context) (time.Time, bool, error) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	// Block building is only supported if the consensus state is Ready
	// and the vm is not state syncing.
	//
	// When the innerVM is dynamically state syncing, consensusState will
	// not be Ready, so we correctly return early in that case as well.
	if vm.consensusState != uint32(vmcore.Ready) {
		return time.Time{}, false, nil
	}

	// Because the VM is marked as being in the Ready state, we know
	// that [VM.SetPreference] must have already been called.
	blk, err := vm.getPostForkBlock(ctx, vm.preferred)
	// If the preferred block is pre-fork, the next block is the pre-fork →
	// post-fork TRANSITION. CRITICAL-1: window WHEN this node builds it (mirroring
	// the post-fork path) so non-leaders WAIT their slot and adopt the elected
	// leader's gossiped transition block instead of every validator forwarding to
	// the inner VM and building its own (the old behavior, which forked the chain
	// at its start). On no-schedule / unresolvable, this falls back to the legacy
	// immediate forward.
	if err != nil {
		return vm.timeToBuildPreForkTransitionLocked(ctx)
	}

	pChainHeight, err := blk.pChainHeight(ctx)
	if err != nil {
		return time.Time{}, false, err
	}

	var (
		childBlockHeight = blk.Height() + 1
		parentTimestamp  = blk.Timestamp()
		nextStartTime    time.Time
	)
	currentTime := vm.Clock.Time().Truncate(time.Second)
	if nextStartTime, err = vm.getPostDurangoSlotTime(
		ctx,
		childBlockHeight,
		pChainHeight,
		proposer.TimeToSlot(parentTimestamp, currentTime),
		parentTimestamp,
	); err == nil {
		vm.proposerBuildSlotGauge.Set(float64(proposer.TimeToSlot(parentTimestamp, nextStartTime)))
	}
	if err != nil {
		vm.logger.Debug("failed to fetch the expected delay",
			log.Err(err),
		)

		// A nil error is returned here because it is possible that
		// bootstrapping caused the last accepted block to move past the latest
		// P-chain height. This will cause building blocks to return an error
		// until the P-chain's height has advanced.
		return time.Time{}, false, nil
	}

	return nextStartTime, true, nil
}

// timeToBuildPreForkTransitionLocked computes the build window for the pre-fork →
// post-fork TRANSITION block (the first post-fork block) when the preferred block
// is still pre-fork. It is the timing half of CRITICAL-1 and mirrors
// getPostDurangoSlotTime: when this node has a real proposer slot in the schedule
// it returns that slot's start time (shouldWait=true), so a non-leader waits its
// slot and adopts the elected leader's gossiped transition block — and a down
// leader does not stall the chain because the eligible set widens as wall-clock
// (and therefore the slot) advances. When there is NO schedule
// (proposer.ErrAnyoneCanPropose — empty/degenerate validator set) or the window
// cannot be resolved, it preserves the legacy behavior (shouldWait=false → forward
// to the inner VM and build an unsigned block immediately). Caller holds vm.lock;
// this only reads (validatorState / windower) and never re-acquires vm.lock.
func (vm *VM) timeToBuildPreForkTransitionLocked(ctx context.Context) (time.Time, bool, error) {
	pre, err := vm.getPreForkBlock(ctx, vm.preferred)
	if err != nil {
		// Preferred is neither a post-fork nor a resolvable pre-fork block — keep
		// the legacy immediate-forward behavior.
		return time.Time{}, false, nil
	}
	pChainHeight, err := vm.selectChildPChainHeight(ctx, 0)
	if err != nil {
		return time.Time{}, false, nil
	}
	var (
		parentTimestamp = pre.Timestamp()
		childHeight     = pre.Height() + 1
		currentTime     = vm.Clock.Time().Truncate(time.Second)
		slot            = proposer.TimeToSlot(parentTimestamp, currentTime)
	)
	// MinDelayForProposer returns the delay until THIS node's earliest slot in the
	// schedule for (childHeight, pChainHeight). The elected leader's delay is ~0;
	// a non-leader's delay is its slot offset, so it waits then builds only if the
	// leader has not already produced the transition block by then.
	delay, err := vm.Windower.MinDelayForProposer(ctx, childHeight, pChainHeight, vm.rt.NodeID, slot)
	switch {
	case err == nil:
		delay = max(delay, vm.MinBlkDelay)
		return parentTimestamp.Add(delay), true, nil
	case errors.Is(err, proposer.ErrAnyoneCanPropose):
		// No schedule — preserve the legacy immediate forward (unsigned build).
		return time.Time{}, false, nil
	default:
		return time.Time{}, false, nil
	}
}

func (vm *VM) getPostDurangoSlotTime(
	ctx context.Context,
	blkHeight,
	pChainHeight,
	slot uint64,
	parentTimestamp time.Time,
) (time.Time, error) {
	delay, err := vm.Windower.MinDelayForProposer(
		ctx,
		blkHeight,
		pChainHeight,
		vm.rt.NodeID,
		slot,
	)
	// Note: The P-chain does not currently try to target any block time. It
	// notifies the consensus engine as soon as a new block may be built. To
	// avoid fast runs of blocks there is an additional minimum delay that
	// validators can specify. This delay may be an issue for high performance,
	// custom VMs. Until the P-chain is modified to target a specific block
	// time, ProposerMinBlockDelay can be configured in the net config.
	switch {
	case err == nil:
		delay = max(delay, vm.MinBlkDelay)
		return parentTimestamp.Add(delay), nil
	case errors.Is(err, proposer.ErrAnyoneCanPropose):
		return parentTimestamp.Add(vm.MinBlkDelay), nil
	default:
		return time.Time{}, err
	}
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	lastAccepted, err := vm.State.GetLastAccepted()
	if err == database.ErrNotFound {
		return vm.ChainVM.LastAccepted(ctx)
	}
	return lastAccepted, err
}

// CreateHandlers returns HTTP handlers for both the proposervm API and the inner ChainVM
func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	// Create the proposervm-specific handler
	proposerHandler, err := NewHTTPHandler(vm)
	if err != nil {
		return nil, err
	}

	// Get the inner ChainVM handlers
	handlers, err := vms.DelegateHandlers(ctx, vm.ChainVM)
	if err != nil {
		return nil, err
	}

	// Initialize handlers map if it's nil
	if handlers == nil {
		handlers = make(map[string]http.Handler)
	}

	// Add the proposervm handler to the map
	handlers["/proposervm"] = proposerHandler
	return handlers, nil
}

// repairAction is the reconciliation the proposervm applies at init when its
// finality index and the inner VM's accepted tip disagree in height.
type repairAction int

const (
	// repairNone: the proposervm and inner heights match; nothing to do.
	repairNone repairAction = iota
	// repairRollBackToInner: the proposervm is AHEAD of the inner (and the
	// rollback target is at/above the fork); roll the proposervm index back to
	// the inner's height.
	repairRollBackToInner
	// repairForgetPastFork: the proposervm is ahead but the rollback target is
	// BELOW the fork; drop all proposervm indices (pre-fork territory).
	repairForgetPastFork
	// repairResetBehindIndex: the proposervm index is BEHIND the inner tip — a
	// truncated/inconsistent on-disk state (e.g. a partially-restored snapshot).
	// The proposervm cannot fabricate the missing wrapper blocks, so drop the
	// stale finality pointer and re-bootstrap rather than fatally bricking init.
	repairResetBehindIndex
)

// planHeightRepair is the PURE reconciliation decision for
// repairAcceptedChainByHeight: given the proposervm's last-accepted height, the
// inner VM's last-accepted height, and the proposervm fork height, it returns
// the action to apply. Separating this decision from the DB effects keeps it
// deterministically testable and makes the behind-index policy (which replaced a
// node-bricking fatal) explicit and regression-locked.
func planHeightRepair(proHeight, innerHeight, forkHeight uint64) repairAction {
	switch {
	case proHeight == innerHeight:
		return repairNone
	case proHeight < innerHeight:
		return repairResetBehindIndex
	case forkHeight > innerHeight:
		return repairForgetPastFork
	default: // proHeight > innerHeight and forkHeight <= innerHeight
		return repairRollBackToInner
	}
}

func (vm *VM) repairAcceptedChainByHeight(ctx context.Context) error {
	innerLastAcceptedID, err := vm.ChainVM.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get inner last accepted: %w", err)
	}
	// A fresh inner chain that has accepted no block reports the empty ID (some
	// Lux VMs return ids.Empty before their first acceptance / before genesis is
	// committed at the moment proposervm initializes). GetBlock(ids.Empty) would
	// fail, and there is no accepted chain to roll the proposervm index back
	// against — there is nothing to repair. Mirror the other "nothing to repair"
	// early returns below. Without this guard, wrapping a fresh chain in
	// proposervm fails VM initialization and crashes the whole node.
	if innerLastAcceptedID == ids.Empty {
		return nil
	}
	innerLastAccepted, err := vm.ChainVM.GetBlock(ctx, innerLastAcceptedID)
	if err != nil {
		// A fresh / not-yet-committed inner chain can report a last-accepted ID
		// whose block is not retrievable — e.g. the brand/feature VMs (Q/A/G/K...)
		// whose genesis references an empty parent, so GetBlock returns
		// "block 111...LpoYY: not found" even though innerLastAcceptedID is not
		// itself ids.Empty (so the guard above does not catch it). There is no
		// accepted chain to roll the proposervm height index back against, so
		// there is nothing to repair. Without this, wrapping such a chain crashes
		// the WHOLE node at init ("error creating required chain" → exit 1).
		vm.logger.Warn("proposervm: inner last-accepted block not retrievable at init; nothing to repair",
			log.Stringer("innerLastAcceptedID", innerLastAcceptedID),
			log.Err(err),
		)
		return nil
	}
	proLastAcceptedID, err := vm.State.GetLastAccepted()
	if err == database.ErrNotFound {
		// If the last accepted block isn't indexed yet, then the underlying
		// chain is the only chain and there is nothing to repair.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get last accepted: %w", err)
	}
	proLastAccepted, err := vm.getPostForkBlock(ctx, proLastAcceptedID)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}

	proLastAcceptedHeight := proLastAccepted.Height()
	innerLastAcceptedHeight := innerLastAccepted.Height()

	// The fork height only matters when the proposervm is AHEAD (rolling back),
	// but the state is already initialized here (we read a last-accepted above),
	// so reading it up front is safe and keeps the reconciliation decision pure.
	forkHeight, err := vm.State.GetForkHeight()
	if err != nil {
		return fmt.Errorf("failed to get fork height: %w", err)
	}

	switch planHeightRepair(proLastAcceptedHeight, innerLastAcceptedHeight, forkHeight) {
	case repairNone:
		// Heights match — nothing to repair.
		return nil

	case repairResetBehindIndex:
		// INVARIANT VIOLATION, but a RECOVERABLE one: the proposervm's finality
		// index sits BELOW the inner VM's accepted tip. In a correct system this
		// never happens (the proposervm's last-accepted and height index commit in
		// the SAME versiondb batch as every inner accept, so they cannot lag), so
		// reaching here means the on-disk proposervm state was truncated relative
		// to the inner — e.g. a crash-inconsistent or partially-restored snapshot
		// (the devnet-C "index 7 < inner 8" brick). The proposervm cannot fabricate
		// the missing outer wrapper blocks for heights (pro, inner], so it CANNOT
		// heal forward locally.
		//
		// Previously this was a FATAL init error that bricked the WHOLE chain
		// ("error creating required chain" -> exit 1), turning a recoverable
		// index/snapshot inconsistency on ONE node into a hard outage that needed
		// manual surgery. Instead, drop the stale proposervm finality pointer and
		// let the node re-bootstrap: consensus re-fetches the canonical blocks (with
		// their finality certs) from healthy peers through the catch-up transport
		// and re-accepts them, rebuilding the proposervm index consistently on top
		// of the inner tip. This is the SAME recovery repairForgetPastFork performs;
		// here we reach it because the index is behind, not ahead. Loud + actionable
		// so operators see exactly what healed and why.
		vm.logger.Warn("proposervm finality index is behind the inner VM tip; resetting the proposervm index to re-bootstrap from peers (recoverable snapshot/index inconsistency, NOT a crash)",
			log.Uint64("proposervmHeight", proLastAcceptedHeight),
			log.Uint64("innerHeight", innerLastAcceptedHeight),
			log.Stringer("innerLastAcceptedID", innerLastAcceptedID),
		)
		if err := vm.State.DeleteLastAccepted(); err != nil {
			return fmt.Errorf("failed to reset proposervm last accepted while healing behind-index: %w", err)
		}
		return vm.db.Commit()

	case repairForgetPastFork:
		// We are rolling back past the fork, so we should just forget about all
		// of our proposervm indices.
		vm.logger.Info("repairing accepted chain by height: rolling back past the proposervm fork",
			log.Uint64("outerHeight", proLastAcceptedHeight),
			log.Uint64("innerHeight", innerLastAcceptedHeight),
			log.Uint64("forkHeight", forkHeight),
		)
		if err := vm.State.DeleteLastAccepted(); err != nil {
			return fmt.Errorf("failed to delete last accepted: %w", err)
		}
		return vm.db.Commit()
	}

	// repairRollBackToInner: the inner vm is behind the proposer vm, so roll the
	// proposervm back to the inner's height.
	vm.logger.Info("repairing accepted chain by height",
		log.Uint64("outerHeight", proLastAcceptedHeight),
		log.Uint64("innerHeight", innerLastAcceptedHeight),
	)

	newProLastAcceptedID, err := vm.State.GetBlockIDAtHeight(innerLastAcceptedHeight)
	if err != nil {
		// This fatal error can happen if NumHistoricalBlocks is set too
		// aggressively and the inner vm rolled back before the oldest
		// proposervm block.
		return fmt.Errorf("proposervm failed to rollback last accepted block to height (%d): %w", innerLastAcceptedHeight, err)
	}

	if err := vm.State.SetLastAccepted(newProLastAcceptedID); err != nil {
		return fmt.Errorf("failed to set last accepted: %w", err)
	}

	if err := vm.db.Commit(); err != nil {
		return fmt.Errorf("failed to commit db: %w", err)
	}

	return nil
}

func (vm *VM) setLastAcceptedMetadata(ctx context.Context) error {
	lastAcceptedID, err := vm.LastAccepted(ctx)
	if err == database.ErrNotFound {
		// If the last accepted block wasn't a PostFork block, then we don't
		// initialize the metadata.
		vm.lastAcceptedHeight = 0
		vm.lastAcceptedTime = time.Time{}
		return nil
	}
	if err != nil {
		return err
	}

	lastAccepted, err := vm.getPostForkBlock(ctx, lastAcceptedID)
	if err == database.ErrNotFound {
		// The last accepted block exists but is not a post-fork block
		// (e.g., it's the genesis block or a pre-fork block)
		// We treat this the same as if LastAccepted returned ErrNotFound
		vm.lastAcceptedHeight = 0
		vm.lastAcceptedTime = time.Time{}
		return nil
	}
	if err != nil {
		return err
	}

	// Set the last accepted height
	vm.lastAcceptedHeight = lastAccepted.Height()

	if _, ok := lastAccepted.getStatelessBlk().(statelessblock.SignedBlock); ok {
		// If the last accepted block wasn't a PostForkOption, then we don't
		// initialize the time.
		return nil
	}

	acceptedParent, err := vm.getPostForkBlock(ctx, lastAccepted.Parent())
	if err != nil {
		return err
	}
	vm.lastAcceptedTime = acceptedParent.Timestamp()
	return nil
}

func (vm *VM) parsePostForkBlock(ctx context.Context, b []byte, verifySignature bool) (PostForkBlock, error) {
	var (
		statelessBlock statelessblock.Block
		err            error
	)

	if verifySignature {
		statelessBlock, err = statelessblock.Parse(b, vm.rt.ChainID)
	} else {
		statelessBlock, err = statelessblock.ParseWithoutVerification(b)
	}
	if err != nil {
		return nil, err
	}

	blkID := statelessBlock.ID()
	innerBlkBytes := statelessBlock.Block()
	innerBlk, err := vm.parseInnerBlock(ctx, blkID, innerBlkBytes)
	if err != nil {
		return nil, err
	}

	if statelessSignedBlock, ok := statelessBlock.(statelessblock.SignedBlock); ok {
		return &postForkBlock{
			SignedBlock: statelessSignedBlock,
			postForkCommonComponents: postForkCommonComponents{
				vm:       vm,
				innerBlk: innerBlk,
			},
		}, nil
	}

	return &postForkOption{
		Block: statelessBlock,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
		},
	}, nil
}

func (vm *VM) parsePreForkBlock(ctx context.Context, b []byte) (*preForkBlock, error) {
	blk, err := vm.ChainVM.ParseBlock(ctx, b)
	if err != nil {
		return nil, err
	}
	return &preForkBlock{
		Block: blk,
		vm:    vm,
	}, nil
}

func (vm *VM) getBlock(ctx context.Context, id ids.ID) (Block, error) {
	if blk, err := vm.getPostForkBlock(ctx, id); err == nil {
		return blk, nil
	}
	return vm.getPreForkBlock(ctx, id)
}

// cachedVerifiedBlock returns the verified-but-not-yet-decided block for
// [blkID] if it is currently held in the verified set. Concurrency-safe.
func (vm *VM) cachedVerifiedBlock(blkID ids.ID) (PostForkBlock, bool) {
	vm.verifiedBlocksLock.RLock()
	defer vm.verifiedBlocksLock.RUnlock()
	blk, exists := vm.verifiedBlocks[blkID]
	return blk, exists
}

// recordVerifiedBlock adds [blk] to the verified set after it passes
// verification. Concurrency-safe.
func (vm *VM) recordVerifiedBlock(blk PostForkBlock) {
	vm.verifiedBlocksLock.Lock()
	defer vm.verifiedBlocksLock.Unlock()
	vm.verifiedBlocks[blk.ID()] = blk
}

// forgetVerifiedBlock drops [blkID] from the verified set once it has been
// accepted or rejected. Concurrency-safe and idempotent.
func (vm *VM) forgetVerifiedBlock(blkID ids.ID) {
	vm.verifiedBlocksLock.Lock()
	defer vm.verifiedBlocksLock.Unlock()
	delete(vm.verifiedBlocks, blkID)
}

func (vm *VM) getPostForkBlock(ctx context.Context, blkID ids.ID) (PostForkBlock, error) {
	if block, exists := vm.cachedVerifiedBlock(blkID); exists {
		return block, nil
	}

	statelessBlock, err := vm.State.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	innerBlkBytes := statelessBlock.Block()
	innerBlk, err := vm.parseInnerBlock(ctx, blkID, innerBlkBytes)
	if err != nil {
		return nil, err
	}

	if statelessSignedBlock, ok := statelessBlock.(statelessblock.SignedBlock); ok {
		return &postForkBlock{
			SignedBlock: statelessSignedBlock,
			postForkCommonComponents: postForkCommonComponents{
				vm:       vm,
				innerBlk: innerBlk,
			},
		}, nil
	}
	return &postForkOption{
		Block: statelessBlock,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vm,
			innerBlk: innerBlk,
		},
	}, nil
}

func (vm *VM) getPreForkBlock(ctx context.Context, blkID ids.ID) (*preForkBlock, error) {
	engineBlk, err := vm.ChainVM.GetBlock(ctx, blkID)
	if err != nil {
		return nil, err
	}
	return &preForkBlock{
		Block: engineBlk,
		vm:    vm,
	}, nil
}

func (vm *VM) acceptPostForkBlock(blk PostForkBlock) error {
	height := blk.Height()
	blkID := blk.ID()

	vm.lastAcceptedHeight = height
	vm.forgetVerifiedBlock(blkID)

	// Persist this block, its height index, and its status
	if err := vm.State.SetLastAccepted(blkID); err != nil {
		return err
	}
	if err := vm.State.PutBlock(blk.getStatelessBlk()); err != nil {
		return err
	}
	if err := vm.updateHeightIndex(height, blkID); err != nil {
		return err
	}
	return vm.db.Commit()
}

func (vm *VM) verifyAndRecordInnerBlk(ctx context.Context, blockRuntime *runtime.Runtime, postFork PostForkBlock) error {
	innerBlk := postFork.getInnerBlk()
	postForkID := postFork.ID()
	originalInnerBlock, previouslyVerified := vm.Tree.Get(innerBlk)
	if previouslyVerified {
		innerBlk = originalInnerBlock
		// We must update all of the mappings from postFork -> innerBlock to
		// now point to originalInnerBlock.
		postFork.setInnerBlk(originalInnerBlock)
		vm.innerBlkCache.Put(postForkID, originalInnerBlock)
	}

	var (
		shouldVerifyWithCtx = blockRuntime != nil
		blkWithCtx          vmchain.WithVerifyRuntime
		err                 error
	)
	if shouldVerifyWithCtx {
		blkWithCtx, shouldVerifyWithCtx = innerBlk.(vmchain.WithVerifyRuntime)
		if shouldVerifyWithCtx {
			shouldVerifyWithCtx, err = blkWithCtx.ShouldVerifyWithRuntime(ctx)
			if err != nil {
				return err
			}
		}
	}

	// Invariant: If either [Verify] or [VerifyWithRuntime] returns nil, this
	//            function must return nil. This maintains the inner block's
	//            invariant that successful verification will eventually result
	//            in accepted or rejected being called.
	if shouldVerifyWithCtx {
		// This block needs to know the P-Chain height during verification.
		// Note that [VerifyWithRuntime] with context may be called multiple
		// times with multiple contexts.
		err = blkWithCtx.VerifyWithRuntime(ctx, blockRuntime)
	} else if !previouslyVerified {
		// This isn't a [vmchain.WithVerifyRuntime] so we only call [Verify] once.
		err = innerBlk.Verify(ctx)
	}
	if err != nil {
		return err
	}

	// Since verification passed, we should ensure the inner block tree is
	// populated.
	if !previouslyVerified {
		vm.Tree.Add(innerBlk)
	}
	vm.recordVerifiedBlock(postFork)
	return nil
}

func (vm *VM) selectChildPChainHeight(ctx context.Context, minPChainHeight uint64) (uint64, error) {
	// Use GetCurrentHeight to get the recommended P-Chain height
	recommendedHeight, err := vm.validatorState.GetCurrentHeight(ctx)
	if err != nil {
		return 0, err
	}
	return max(recommendedHeight, minPChainHeight), nil
}

// parseInnerBlock attempts to parse the provided bytes as an inner block. If
// the inner block happens to be cached, then the inner block will not be
// parsed.
func (vm *VM) parseInnerBlock(ctx context.Context, outerBlkID ids.ID, innerBlkBytes []byte) (vmchain.Block, error) {
	if innerBlk, ok := vm.innerBlkCache.Get(outerBlkID); ok {
		return innerBlk, nil
	}

	engineBlk, err := vm.ChainVM.ParseBlock(ctx, innerBlkBytes)
	if err != nil {
		return nil, err
	}
	innerBlk := engineBlk
	vm.cacheInnerBlock(outerBlkID, innerBlk)
	return innerBlk, nil
}

// Caches proposervm block ID --> inner block if the inner block's height
// is within [innerBlkCacheSize] of the last accepted block's height.
func (vm *VM) cacheInnerBlock(outerBlkID ids.ID, innerBlk vmchain.Block) {
	diff := math.AbsDiff(innerBlk.Height(), vm.lastAcceptedHeight)
	if diff < innerBlkCacheSize {
		vm.innerBlkCache.Put(outerBlkID, innerBlk)
	}
}

// validatorStateWrapper wraps runtime.ValidatorState to match validators.State
type validatorStateWrapper struct {
	ctx         context.Context
	vs          runtime.ValidatorState
	netIDsCache cache.Cacher[ids.ID, ids.ID] // chainID -> netID cache
}

func (v *validatorStateWrapper) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return v.vs.GetCurrentHeight(ctx)
}

func (v *validatorStateWrapper) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Pass context to the underlying validator state
	return v.vs.GetValidatorSet(ctx, height, netID)
}

func (v *validatorStateWrapper) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return v.vs.GetMinimumHeight(ctx)
}

func (v *validatorStateWrapper) GetNetworkID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	// Check cache first
	if netID, ok := v.netIDsCache.Get(chainID); ok {
		return netID, nil
	}

	// Cache miss - fetch from underlying validator state
	netID, err := v.vs.GetNetworkID(chainID)
	if err != nil {
		return ids.Empty, err
	}

	// Cache the result
	v.netIDsCache.Put(chainID, netID)
	return netID, nil
}

func (v *validatorStateWrapper) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// For now, return empty set - need proper implementation
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

func (v *validatorStateWrapper) GetCurrentValidatorSet(ctx context.Context, netID ids.ID) (map[ids.ID]*validators.GetValidatorOutput, uint64, error) {
	// For now, return empty set with current height - need proper implementation
	height, err := v.vs.GetCurrentHeight(ctx)
	if err != nil {
		return nil, 0, err
	}
	return make(map[ids.ID]*validators.GetValidatorOutput), height, nil
}

// interfacesToConsensusValidatorStateAdapter adapts ValidatorState from chainRuntime
type interfacesToConsensusValidatorStateAdapter struct {
	ctx         context.Context
	vs          runtime.ValidatorState
	netIDsCache cache.Cacher[ids.ID, ids.ID] // chainID -> netID cache
}

func (a *interfacesToConsensusValidatorStateAdapter) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return a.vs.GetMinimumHeight(ctx)
}

func (a *interfacesToConsensusValidatorStateAdapter) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return a.vs.GetCurrentHeight(ctx)
}

func (a *interfacesToConsensusValidatorStateAdapter) GetChainID(chainID ids.ID) (ids.ID, error) {
	return a.vs.GetChainID(chainID)
}

func (a *interfacesToConsensusValidatorStateAdapter) GetNetworkID(chainID ids.ID) (ids.ID, error) {
	// Check cache first
	if netID, ok := a.netIDsCache.Get(chainID); ok {
		return netID, nil
	}

	// Cache miss - fetch from underlying validator state
	netID, err := a.vs.GetNetworkID(chainID)
	if err != nil {
		return ids.Empty, err
	}

	// Cache the result
	a.netIDsCache.Put(chainID, netID)
	return netID, nil
}

func (a *interfacesToConsensusValidatorStateAdapter) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Pass context to the underlying validator state
	return a.vs.GetValidatorSet(ctx, height, netID)
}

func (a *interfacesToConsensusValidatorStateAdapter) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Pass context to the underlying validator state
	return a.vs.GetValidatorSet(ctx, height, netID)
}
