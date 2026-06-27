// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// pchain_height_vm.go — the node-layer boundary that delivers the REAL P-CHAIN
// EPOCH HEIGHT to the consensus chain engine (the b2 wiring; the LAST
// QUORUM_FINALITY blocker).
//
// THE BUG IT FIXES. The chain engine pins a block's weighted validator set to a
// P-CHAIN height (engine/chain: pChainHeightOf → epochHeightLocked → the SINGLE
// height the set-root commitment, the ⅔-by-stake tally, AND the per-voter pubkey
// resolution are read at). It reads that height by asserting the VM block to a
// `PChainHeight() uint64` (consensus block.SignedBlock). A bare VM block — every
// block the node's plugin VMs (C-Chain EVM, dexvm, …) produce — does NOT expose
// one, so pChainHeightOf returns 0 and the engine resolves the set at P-chain
// height 0: the GENESIS set. That is SAFE (non-empty, identical on every node,
// ≤ current) and UNBRICKS finality, but it FREEZES the epoch at genesis: a
// validator that JOINED after genesis is absent from set@0, so its vote is
// dropped and its stake is not counted — finality cannot track a DYNAMIC
// validator set (a departed genesis majority could even collude). This is the
// frozen-set caveat Gate D must sign away.
//
// THE FIX. pChainHeightVM wraps the chain's BlockBuilder (the VM the engine
// builds/parses blocks through) and makes the block the engine sees carry the
// proposer's P-chain epoch height — WITHOUT changing the inner VM's block format,
// IDs, or ledger state:
//
//   - BuildBlock (proposer): build the inner block, then stamp the proposer's
//     live P-chain epoch height H = max(GetCurrentHeight, parentH) (the
//     proposervm selectChildPChainHeight rule — monotone, ≤ current). Return a
//     pChainHeightBlock whose Bytes() are [magic|H|innerBytes] and whose
//     PChainHeight() is H. Every other method (ID, ParentID, Height, Verify,
//     Accept, …) delegates to the inner block — so the block's IDENTITY and the
//     VM's state are the inner VM's, unchanged.
//   - ParseBlock (follower): split [magic|H|innerBytes], parse the inner bytes
//     through the inner VM, and re-attach H. Bytes WITHOUT the magic (a raw inner
//     block from a pre-wrapper peer, GetAncestors, or genesis) parse with H=0 →
//     the SAFE genesis-set fallback — never worse than today.
//
// DETERMINISM is the whole point and is why H must travel IN the gossiped bytes.
// The engine gossips only blk.Bytes(); a follower recovers the block solely via
// ParseBlock(those bytes). The proposer STAMPS H; every follower ADOPTS the
// identical H from the envelope — it never recomputes H from its own (skewing)
// current P-chain view. So every honest node derives the SAME epoch height from
// the SAME signed block, which is the invariant the engine's cert verifier
// requires (engine/chain HandleIncomingCert cross-checks the cert's set-root
// against the set-root WE recompute at OUR epoch height for the block: equal H ⟹
// equal root ⟹ the cert verifies; a post-genesis validator's vote+stake now
// count). A build-time-only stamp would give the proposer a real H but leave
// followers computing a DIFFERENT root from their own height → the cert is
// dropped → finality STALLS on a dynamic set (strictly worse than the genesis
// fallback). That is why the height is carried, not recomputed.
//
// This is a consensus-TRANSPORT framing (an 8-byte height + 4-byte magic prefix
// on the gossiped bytes), NOT a chain/ledger fork: the inner VM's bytes, block
// IDs, and execution state are byte-identical, so there is no re-genesis — only a
// coordinated node upgrade (the whole validator set ships together).
package chains

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	consensuschain "github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/ids"
	validators "github.com/luxfi/validators"
)

// pChainHeightMagic tags a transport-wrapped block so ParseBlock can tell a
// wrapped block ([magic|H:8|innerBytes]) from a raw inner block (no magic →
// H=0, the genesis-set fallback). Distinct, fixed, version-pinned: a future
// envelope change bumps the last byte and is non-malleable. "LXP" = Lux P-chain.
var pChainHeightMagic = [4]byte{'L', 'X', 'P', 0x01}

// pChainHeightHeaderLen is the fixed transport-header width: magic(4) + height(8).
const pChainHeightHeaderLen = 4 + 8

// pChainHeightRecencySlack is the receive-side UPPER bound on how far a gossiped
// block's stamped P-chain epoch height H may exceed THIS node's live P-chain
// height before the block is rejected as absurd-future (HIGH-1, predicate b). The
// proposer stamps H = its own GetCurrentHeight (the proposervm selectChildPChainHeight
// rule, ≤ its current); a follower lagging the P-chain sees a smaller current, so
// some forward skew is LEGITIMATE during a staking change or a gossip burst. 256
// P-chain heights swamps any honest inter-node skew (P-chain blocks are seconds
// apart; gossip+verify is sub-second) while still rejecting a wildly-future H. The
// bound is a LIVENESS/DoS sanity gate, NOT the safety bound (that is the monotone
// gate in the engine): a future H always FAILS set resolution at the verifier
// (errUnfinalizedHeight) and never finalizes against a bogus set regardless — this
// just fails it fast and keeps the heights map from accreting unresolvable entries.
const pChainHeightRecencySlack = uint64(256)

// pChainHeightMapCap is the hard backstop on the heights map size — the cap that
// bounds the gossip-parse DoS (MEDIUM-3): ParseBlock runs before the engine's
// dedup/Verify, so an attacker peer could stream distinct unverified blocks, each
// adding a permanent entry. The map plateaus far below this in steady state (the
// watermark prune evicts every finalized block's entry as finality advances); the
// cap fires only under a sustained flood, and then evicts HIGHEST-chain-height
// first — spam claims wild heights, real pending blocks cluster just above the
// finalized watermark, so the near-tip pending band is preserved (NOT a blind LRU
// that could drop a live block). Chosen >> any realistic pending working set.
const pChainHeightMapCap = 4096

// errPChainHeightNotRecent is returned by ParseBlock when a wrapped block's stamped
// P-chain epoch exceeds this node's live P-chain height by more than the recency
// slack. Returning an error fails CLOSED: HandleIncomingBlock drops the block (it
// is never tracked or voted), which is the correct response to an absurd-future
// epoch a follower cannot resolve a set at.
var errPChainHeightNotRecent = errors.New("chains: gossiped block P-chain epoch height exceeds local P-chain height + recency slack (absurd-future epoch, rejected fail-closed)")

// pChainHeightVM wraps a chain BlockBuilder so the block the consensus engine
// sees carries the proposer's P-chain epoch height. It is the ONLY thing that
// changes between the engine and the inner VM; everything else is the inner VM,
// verbatim.
//
// It is installed ONLY on K>1 (quorum) chains: a K==1 chain finalizes on its
// sole validator's self-vote with no cert and no validator-set epoch, so the
// stamp is inert there and the wrapper is not installed (the inner VM is used
// directly — one obvious path per mode).
type pChainHeightVM struct {
	inner     consensuschain.BlockBuilder
	state     validators.State
	networkID ids.ID

	// heights memoises blockID → (stamped P-chain epoch, value-chain height) for
	// blocks this node has built or parsed, so GetBlock can re-attach the epoch a
	// block was stamped with (the inner VM stores only the inner bytes, which carry
	// no epoch). Best-effort: a cache miss yields H=0 (the genesis-set fallback).
	// GetBlock is NOT on the finality-capture path — the engine records a block's
	// epoch the first time it BUILDS or PARSES the block (where the epoch is always
	// present) and reads it back from its own pending-block record, never by
	// re-fetching via GetBlock — so a miss here cannot drop a LIVE block's epoch
	// (the consensus PendingBlock holds it, not this map).
	//
	// BOUNDED (MEDIUM-3): the value-chain height tagged on each entry lets onAccepted
	// PRUNE every entry at or below the finalized-height watermark (a finalized block
	// never needs a GetBlock epoch re-attach), so under steady finality the map
	// plateaus at the watermark. finalizedHeight is that watermark, advanced as
	// blocks Accept. A hard cap (pChainHeightMapCap) backstops a sustained
	// unverified-parse flood, evicting highest-height-first to preserve the near-tip
	// pending band. A still-PENDING block (height above the watermark) is never
	// evicted by the watermark prune.
	mu              sync.Mutex
	heights         map[ids.ID]heightEntry
	finalizedHeight uint64
}

// heightEntry records, for a remembered block, both the P-chain EPOCH it was
// stamped with (re-attached by GetBlock) and its VALUE-CHAIN height (the prune key:
// an entry at/below the finalized watermark is evictable).
type heightEntry struct {
	epoch       uint64
	chainHeight uint64
}

// newPChainHeightVM wraps inner with P-chain-epoch-height stamping backed by the
// height-indexed validators.State. networkID is the set the height is resolved
// against (PrimaryNetworkID for native chains; the L1's set ID otherwise) — it is
// recorded for symmetry with the engine's epoch reads, though GetCurrentHeight is
// a P-chain-global height. A nil state disables stamping (BuildBlock falls back to
// H=0): callers install this ONLY for K>1 chains, which the manager already
// guards to have a live height-indexed state, so the nil path is a defensive
// fail-soft, not a runtime mode.
func newPChainHeightVM(inner consensuschain.BlockBuilder, state validators.State, networkID ids.ID) *pChainHeightVM {
	return &pChainHeightVM{
		inner:     inner,
		state:     state,
		networkID: networkID,
		heights:   make(map[ids.ID]heightEntry),
	}
}

var _ consensuschain.BlockBuilder = (*pChainHeightVM)(nil)

// remember records the epoch a block (at value-chain height chainHeight) was
// stamped with so GetBlock can re-attach it. BOUNDED (MEDIUM-3): the map is pruned
// against the finalized-height watermark by onAccepted, so it plateaus under steady
// finality; remember additionally enforces the hard cap as a flood backstop. The
// chainHeight is the prune key.
func (vm *pChainHeightVM) remember(id ids.ID, epoch, chainHeight uint64) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.heights[id] = heightEntry{epoch: epoch, chainHeight: chainHeight}
	vm.enforceCapLocked()
}

func (vm *pChainHeightVM) recall(id ids.ID) (uint64, bool) {
	vm.mu.Lock()
	e, ok := vm.heights[id]
	vm.mu.Unlock()
	return e.epoch, ok
}

// onAccepted advances the finalized-height watermark to acceptedHeight and PRUNES
// every remembered entry at or below it (MEDIUM-3). A finalized block's epoch is
// already captured in the engine's records and will never be re-attached via
// GetBlock, so dropping its entry is loss-free — and a still-PENDING block (height
// strictly above the watermark) is NEVER evicted by this prune, so its GetBlock
// epoch survives. Called from the wrapped block's Accept (the block tells its VM
// the height at which it finalized). Idempotent and monotone: a re-accept or an
// out-of-order lower height never lowers the watermark.
func (vm *pChainHeightVM) onAccepted(acceptedHeight uint64) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if acceptedHeight > vm.finalizedHeight {
		vm.finalizedHeight = acceptedHeight
	}
	for id, e := range vm.heights {
		if e.chainHeight <= vm.finalizedHeight {
			delete(vm.heights, id)
		}
	}
}

// enforceCapLocked is the flood backstop: if the map exceeds the hard cap, evict
// the entry with the HIGHEST value-chain height. Real pending blocks cluster just
// above the finalized watermark (the next few heights); an unverified-parse flood
// claims arbitrary, typically far-future heights — so evicting highest-first sheds
// spam while preserving the near-tip pending band (NOT a blind LRU that could drop
// a live block's epoch). The cap is reached only under attack: the watermark prune
// keeps the steady-state map far below it. Caller holds vm.mu.
func (vm *pChainHeightVM) enforceCapLocked() {
	for len(vm.heights) > pChainHeightMapCap {
		var victim ids.ID
		var maxHeight uint64
		first := true
		for id, e := range vm.heights {
			if first || e.chainHeight > maxHeight {
				victim, maxHeight, first = id, e.chainHeight, false
			}
		}
		delete(vm.heights, victim)
	}
}

// currentPChainHeight returns the proposer's live P-chain height, the upper end
// of the proposervm selectChildPChainHeight rule. A nil state or a read error
// yields 0 (the genesis-set fallback): a height the proposer cannot resolve must
// not be stamped onto a block, since a follower could not resolve the set there
// either. A read error is symmetric across nodes for a committed P-chain, so the
// fallback is uniform.
func (vm *pChainHeightVM) currentPChainHeight(ctx context.Context) uint64 {
	if vm.state == nil {
		return 0
	}
	h, err := vm.state.GetCurrentHeight(ctx)
	if err != nil {
		return 0
	}
	return h
}

// parentHeight returns the P-chain epoch height stamped on parentID, or 0 if it
// is unknown — used to keep a child's stamped height monotone ≥ its parent's
// (selectChildPChainHeight). An unknown parent (0) cannot LOWER the child below
// the current height (the max below), so monotonicity is preserved fail-soft.
func (vm *pChainHeightVM) parentHeight(parentID ids.ID) uint64 {
	h, _ := vm.recall(parentID)
	return h
}

// BuildBlock builds the inner block and stamps it with the proposer's P-chain
// epoch height H = max(currentPChainHeight, parentH). This is the proposer side
// of the epoch: the one node building this block chooses H from its own live
// P-chain view, and that H rides the gossiped bytes to every follower (which
// adopt it, never recompute it). Monotone ≥ parent so a chain's epoch never goes
// backwards across blocks (a cert at a lower epoch than its parent would bind a
// stale set).
func (vm *pChainHeightVM) BuildBlock(ctx context.Context) (block.Block, error) {
	inner, err := vm.inner.BuildBlock(ctx)
	if err != nil {
		return nil, err
	}
	h := vm.currentPChainHeight(ctx)
	if ph := vm.parentHeight(inner.ParentID()); ph > h {
		h = ph
	}
	vm.remember(inner.ID(), h, inner.Height())
	return newPChainHeightBlock(vm, inner, h), nil
}

// ParseBlock recovers a block from transport bytes. Wrapped bytes
// ([magic|H|innerBytes]) yield a block whose PChainHeight() is the EXACT H the
// proposer stamped — the determinism guarantee: every node parsing the same
// gossiped bytes recovers the identical epoch height. Bytes without the magic are
// a raw inner block (a pre-wrapper peer, GetAncestors, genesis): parsed straight
// through with H=0, the SAFE genesis-set fallback.
//
// FRAME DISCRIMINATOR (LOW-2). The 4-byte magic collides at 2^-32 with the raw
// hash prefix some VMs use as their Bytes() (dexvm/quantumvm/dchain serialize
// parentID[0:32]||…). The magic match alone is NOT sufficient: a frame is real
// only if the inner re-parse of the framed payload ALSO succeeds. So when the
// prefix matches, attempt to parse b[header:] as an inner block; if that FAILS,
// fall back to parsing the WHOLE b as a raw inner block (the colliding raw block,
// whose bytes minus the 12-byte prefix are not a valid inner block). This removes
// the bootstrap stall a magic collision used to cause (mis-framed → inner decode
// fails closed → stall on that chain).
//
// RECENCY GATE (HIGH-1, predicate b). A real frame's stamped epoch H must be
// recent relative to THIS node's live P-chain height: H ≤ localCurrentH + slack.
// An absurd-future H (a Byzantine proposer claiming an epoch the network has not
// reached) is rejected fail-closed (the block is dropped, never tracked). This is
// the upper half of the epoch bound; the engine's monotone gate is the lower half
// (≥ parent's recorded epoch), so the epoch is pinned to [parentEpoch, localH+slack].
func (vm *pChainHeightVM) ParseBlock(ctx context.Context, b []byte) (block.Block, error) {
	candidateInner, h, magicMatched := unwrapPChainHeight(b)
	if magicMatched {
		// The prefix looks like a frame. It IS a frame only if the framed payload
		// parses as an inner block AND the stamped epoch is recent.
		if inner, err := vm.inner.ParseBlock(ctx, candidateInner); err == nil {
			if !vm.epochRecent(ctx, h) {
				return nil, errPChainHeightNotRecent
			}
			vm.remember(inner.ID(), h, inner.Height())
			return newPChainHeightBlock(vm, inner, h), nil
		}
		// Magic matched but the framed payload did NOT parse → this is a RAW block
		// whose first bytes coincidentally equal the magic. Parse the whole b raw.
	}
	// Raw inner block: no proposer epoch available → genesis-set fallback. Still
	// wrap (uniform block type to the engine) but with H=0 and a passthrough
	// Bytes() so re-gossip of a raw block stays raw.
	inner, err := vm.inner.ParseBlock(ctx, b)
	if err != nil {
		return nil, err
	}
	return newRawPChainHeightBlock(vm, inner), nil
}

// epochRecent reports whether a stamped P-chain epoch height is within the recency
// slack of this node's live P-chain height (HIGH-1, predicate b). A node with no
// resolvable current height (state nil / read error → 0) cannot bound recency, so
// it admits the block (the verifier still fails closed on an unresolvable future
// epoch at set-resolution time — recency here is a fast-fail/DoS sanity gate, not
// the safety bound). Heights at or below local are always recent.
func (vm *pChainHeightVM) epochRecent(ctx context.Context, h uint64) bool {
	localH := vm.currentPChainHeight(ctx)
	if localH == 0 {
		return true
	}
	return h <= localH+pChainHeightRecencySlack
}

// GetBlock fetches a block from the inner VM and re-attaches the P-chain height
// it was stamped with (recalled from build/parse). A miss yields H=0 — acceptable
// because GetBlock is not on the engine's finality-capture path (see the heights
// field doc). The returned block's Bytes() is the inner bytes (the inner VM's
// canonical at-rest form); a stamped height is re-attached for the engine's
// in-memory use only.
func (vm *pChainHeightVM) GetBlock(ctx context.Context, id ids.ID) (block.Block, error) {
	inner, err := vm.inner.GetBlock(ctx, id)
	if err != nil {
		return nil, err
	}
	if h, ok := vm.recall(id); ok {
		return newRawPChainHeightBlockWithHeight(vm, inner, h), nil
	}
	return newRawPChainHeightBlock(vm, inner), nil
}

// LastAccepted delegates to the inner VM.
func (vm *pChainHeightVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return vm.inner.LastAccepted(ctx)
}

// NOTE: this wrapper deliberately does NOT forward GetBlockIDAtHeight. The bootstrap acceptance
// oracle's fork-sibling check reads the IN-PROCESS consensus finalized ledger
// (blockHandler.finalizedBlockAtHeight → engine.FinalizedBlockAtHeight), NOT a VM height index —
// because the VM index is dead over ZAP (the zap server has no MsgGetBlockIDAtHeight handler, so
// the real C-Chain returns nothing). A forwarder here would only re-expose that dead path.

// SetPreference delegates to the inner VM.
func (vm *pChainHeightVM) SetPreference(ctx context.Context, id ids.ID) error {
	return vm.inner.SetPreference(ctx, id)
}

// wrapPChainHeight frames inner bytes with the proposer's P-chain height:
// magic(4) || height(8,BE) || innerBytes. The header is fixed-width so unwrap is
// a constant-time prefix read.
func wrapPChainHeight(innerBytes []byte, height uint64) []byte {
	out := make([]byte, pChainHeightHeaderLen+len(innerBytes))
	copy(out[0:4], pChainHeightMagic[:])
	binary.BigEndian.PutUint64(out[4:12], height)
	copy(out[pChainHeightHeaderLen:], innerBytes)
	return out
}

// unwrapPChainHeight is a PURE prefix splitter: it reports whether b carries the
// frame magic at the header width and, if so, returns the candidate payload and
// the encoded height. magicMatched==true means ONLY that the prefix looks like a
// frame — NOT that b is definitely framed. ParseBlock makes the real decision by
// re-parsing the candidate payload (LOW-2): a raw block whose first bytes collide
// with the magic (2^-32) yields magicMatched==true here but its payload fails the
// inner re-parse, so ParseBlock falls back to a raw whole-b parse. Keeping this a
// pure split (no VM dependency) localizes the collision handling to ParseBlock.
func unwrapPChainHeight(b []byte) (payload []byte, height uint64, magicMatched bool) {
	if len(b) < pChainHeightHeaderLen ||
		b[0] != pChainHeightMagic[0] || b[1] != pChainHeightMagic[1] ||
		b[2] != pChainHeightMagic[2] || b[3] != pChainHeightMagic[3] {
		return b, 0, false
	}
	height = binary.BigEndian.Uint64(b[4:12])
	return b[pChainHeightHeaderLen:], height, true
}

// --- the wrapped block -------------------------------------------------------

// pChainHeightBlock is a chain block that carries a P-chain epoch height for the
// engine while delegating identity, verification, and acceptance to the inner VM
// block. It satisfies consensus block.SignedBlock's PChainHeight() (the subset
// the engine reads via pChainHeightOf), so the engine records the real epoch
// height — every other behaviour is the inner VM's.
//
// `bytes` is what the engine gossips. For a proposer-built / wrapped-parsed block
// it is the framed [magic|H|innerBytes] so a follower recovers H; for a raw block
// (genesis-set fallback) it is the inner bytes verbatim (passthrough) so re-gossip
// stays raw.
type pChainHeightBlock struct {
	vm           *pChainHeightVM
	inner        block.Block
	pChainHeight uint64
	bytes        []byte
}

// newPChainHeightBlock wraps inner with height h and a FRAMED Bytes()
// ([magic|h|inner]) — the proposer/parse path, where H must travel to followers.
func newPChainHeightBlock(vm *pChainHeightVM, inner block.Block, h uint64) *pChainHeightBlock {
	return &pChainHeightBlock{
		vm:           vm,
		inner:        inner,
		pChainHeight: h,
		bytes:        wrapPChainHeight(inner.Bytes(), h),
	}
}

// newRawPChainHeightBlock wraps inner with H=0 and a PASSTHROUGH Bytes() (the
// inner bytes verbatim) — the genesis-set fallback for a raw inner block.
func newRawPChainHeightBlock(vm *pChainHeightVM, inner block.Block) *pChainHeightBlock {
	return &pChainHeightBlock{vm: vm, inner: inner, pChainHeight: 0, bytes: inner.Bytes()}
}

// newRawPChainHeightBlockWithHeight re-attaches a recalled height to an inner
// block fetched via GetBlock while keeping a PASSTHROUGH Bytes() (the inner VM's
// at-rest bytes). Used off the finality-capture path; the height is for the
// engine's in-memory epoch read, not for re-gossip framing.
func newRawPChainHeightBlockWithHeight(vm *pChainHeightVM, inner block.Block, h uint64) *pChainHeightBlock {
	return &pChainHeightBlock{vm: vm, inner: inner, pChainHeight: h, bytes: inner.Bytes()}
}

func (b *pChainHeightBlock) ID() ids.ID           { return b.inner.ID() }
func (b *pChainHeightBlock) Parent() ids.ID       { return b.inner.Parent() }
func (b *pChainHeightBlock) ParentID() ids.ID     { return b.inner.ParentID() }
func (b *pChainHeightBlock) Height() uint64       { return b.inner.Height() }
func (b *pChainHeightBlock) Timestamp() time.Time { return b.inner.Timestamp() }
func (b *pChainHeightBlock) Status() uint8        { return b.inner.Status() }
func (b *pChainHeightBlock) Bytes() []byte        { return b.bytes }

// PChainHeight is the method the engine reads via pChainHeightOf — the SOLE
// reason this wrapper exists. The inner block does not expose it; this does.
func (b *pChainHeightBlock) PChainHeight() uint64 { return b.pChainHeight }

// Verify/Reject delegate to the inner VM block: the wrapper changes the epoch the
// engine SEES, never the block's validity or state transition.
func (b *pChainHeightBlock) Verify(ctx context.Context) error { return b.inner.Verify(ctx) }
func (b *pChainHeightBlock) Reject(ctx context.Context) error { return b.inner.Reject(ctx) }

// Accept finalizes the inner block, then advances the VM's finalized-height
// watermark and prunes the heights map at/below it (MEDIUM-3). The inner Accept
// runs FIRST: finalization must not depend on the bookkeeping prune, and the prune
// is a pure memory-management side effect that can never fail. The block carries
// its own height, so the VM learns the exact finalized height with no extra read.
func (b *pChainHeightBlock) Accept(ctx context.Context) error {
	if err := b.inner.Accept(ctx); err != nil {
		return err
	}
	if b.vm != nil {
		b.vm.onAccepted(b.inner.Height())
	}
	return nil
}

// Unwrap exposes the inner block for code that must reach the underlying VM block
// (e.g. a missing-context reparse). Kept narrow on purpose.
func (b *pChainHeightBlock) Unwrap() block.Block { return b.inner }
