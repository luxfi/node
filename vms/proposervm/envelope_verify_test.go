// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// envelope_verify_test.go — WHAT AN ENVELOPE COMMITS TO.
//
// postForkCommonComponents.Verify is the whole of the outer layer's admission
// rule, and until this file nothing executed it. That matters more here than in
// most packages: the identity of an envelope versus the identity of the inner
// block it wraps is exactly the confusion that froze mainnet C-Chain 1082879,
// where hundreds of distinct envelopes over ONE inner block were each treated as
// a distinct chain and no cert could form.
//
// Every test below states a property of that admission rule and drives the real
// path — child.Verify → vm.getBlock(parent) → parent.verifyPostForkChild →
// postForkCommonComponents.Verify — over a real State, with a real envelope
// built by the real block builders. Nothing inside the proposervm is stubbed;
// the inner VM and the proposer schedule are, because they are the two things a
// block cannot carry with it.
package proposervm

import (
	"context"
	"crypto"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/runtime"
	validatorstest "github.com/luxfi/validators/validatorstest"
	vmcore "github.com/luxfi/vm"
	vmchain "github.com/luxfi/vm/chain"
	"github.com/luxfi/vm/chain/blocktest"

	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/vms/proposervm/block"
	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/node/vms/proposervm/state"
	"github.com/luxfi/node/vms/proposervm/tree"
)

// envPChainHeight is what the validator state reports as the current P-chain
// height in these tests. Envelopes carry a height at or below it unless a test
// is specifically about the ceiling.
const envPChainHeight = uint64(7)

// envT0 anchors every clock in this file. Whole seconds, because block
// timestamps are encoded to second resolution.
var envT0 = time.Unix(1_700_000_000, 0)

// envInner is the inner execution VM as the envelope layer sees it: a bag of
// blocks answerable by id and by bytes. Blocks are minted freely — siblings,
// forks, orphans — because what is under test is which of them an envelope is
// permitted to commit to, not how the inner VM chose them.
type envInner struct {
	byID     map[ids.ID]vmchain.Block
	byBytes  map[string]vmchain.Block
	byHeight map[uint64]vmchain.Block
	last     ids.ID
}

func newEnvInner() *envInner {
	return &envInner{
		byID:     map[ids.ID]vmchain.Block{},
		byBytes:  map[string]vmchain.Block{},
		byHeight: map[uint64]vmchain.Block{},
	}
}

// add records a block. byHeight keeps the most recently minted block at a
// height, which is the inner VM's own canonical answer — siblings exist in
// byID but only one of them is at the height.
func (e *envInner) add(b vmchain.Block) {
	e.byID[b.ID()] = b
	e.byBytes[string(b.Bytes())] = b
	e.byHeight[b.Height()] = b
	e.last = b.ID()
}

// mint returns an inner block extending [parent] at [height]. Bytes are unique
// per block so the inner ParseBlock is a total function on what we minted.
func (e *envInner) mint(parent ids.ID, height uint64, ts time.Time) *blocktest.Block {
	id := ids.GenerateTestID()
	b := &blocktest.Block{
		IDV:        id,
		ParentV:    parent,
		HeightV:    height,
		BytesV:     append([]byte("inner"), id[:]...),
		TimestampV: ts,
	}
	e.add(b)
	return b
}

func (e *envInner) vm() *blocktest.VM {
	return &blocktest.VM{
		GetBlockF: func(_ context.Context, id ids.ID) (vmchain.Block, error) {
			if b, ok := e.byID[id]; ok {
				return b, nil
			}
			return nil, database.ErrNotFound
		},
		ParseBlockF: func(_ context.Context, b []byte) (vmchain.Block, error) {
			if blk, ok := e.byBytes[string(b)]; ok {
				return blk, nil
			}
			return nil, errors.New("inner: unknown bytes")
		},
		LastAcceptedF: func(context.Context) (ids.ID, error) {
			return e.last, nil
		},
		GetBlockIDAtHeightF: func(_ context.Context, h uint64) (ids.ID, error) {
			if b, ok := e.byHeight[h]; ok {
				return b.ID(), nil
			}
			return ids.Empty, database.ErrNotFound
		},
	}
}

// envOracle is an inner block that answers Options — the shape the envelope
// layer must refuse as the parent of a plain post-fork child and require as the
// parent of an option.
type envOracle struct {
	*blocktest.Block
	opts [2]vmchain.Block
	err  error
}

func (o *envOracle) Options(context.Context) ([2]vmchain.Block, error) {
	return o.opts, o.err
}

// envWindower answers the proposer election from a closure so a test can pin
// who owns a slot. The zero value elects nobody (ErrAnyoneCanPropose), which is
// the shape of a chain whose validator set the windower cannot resolve.
type envWindower struct {
	expected func(slot uint64) (ids.NodeID, error)
	delay    func() (time.Duration, error)
}

func (w envWindower) Proposers(context.Context, uint64, uint64, int) ([]ids.NodeID, error) {
	return nil, proposer.ErrAnyoneCanPropose
}

func (w envWindower) Delay(context.Context, uint64, uint64, ids.NodeID, int) (time.Duration, error) {
	return 0, proposer.ErrAnyoneCanPropose
}

func (w envWindower) ExpectedProposer(_ context.Context, _, _, slot uint64) (ids.NodeID, error) {
	if w.expected == nil {
		return ids.EmptyNodeID, proposer.ErrAnyoneCanPropose
	}
	return w.expected(slot)
}

func (w envWindower) MinDelayForProposer(context.Context, uint64, uint64, ids.NodeID, uint64) (time.Duration, error) {
	if w.delay == nil {
		return 0, proposer.ErrAnyoneCanPropose
	}
	return w.delay()
}

// newEnvVM stands a proposervm up over [inner] with every field the envelope
// verify path reads. It goes through New so the interface probing there is
// exercised, then fills the parts Initialize would have. Defaults are the
// permissive ones — open schedule, consensus not yet Ready — so each test moves
// exactly the one thing it is about.
func newEnvVM(t *testing.T, inner *envInner) *VM {
	t.Helper()
	vm := New(inner.vm(), Config{})
	vm.db = versiondb.New(memdb.New())
	vm.State = state.New(vm.db)
	vm.Tree = tree.New()
	vm.Windower = envWindower{}
	vm.verifiedBlocks = map[ids.ID]PostForkBlock{}
	vm.innerBlkCache = lru.NewSizedCache(innerBlkCacheSize, cachedBlockSize)
	vm.logger = log.Noop()
	vm.rt = &runtime.Runtime{
		ChainID: ids.GenerateTestID(),
		NodeID:  ids.GenerateTestNodeID(),
	}
	vm.validatorState = &validatorstest.State{
		GetCurrentHeightF: func(context.Context) (uint64, error) { return envPChainHeight, nil },
	}
	metrics := metric.New("")
	vm.lastAcceptedTimestampGaugeVec = metrics.NewGaugeVec(
		"last_accepted_timestamp", "timestamp of the last block accepted", []string{"block_type"})
	vm.acceptedBlocksSlotHistogram = metrics.NewHistogram(
		"accepted_blocks_slot", "the slots accepted blocks were proposed in", []float64{0.5, 1.5, 2.5})
	vm.proposerBuildSlotGauge = metrics.NewGauge(
		"block_building_slot", "the slot that this node may attempt to build a block")
	// The clock sits well ahead of every envelope timestamp used below, so the
	// future-skew bound is never the reason a test fails unless it says so.
	vm.Clock.Set(envT0.Add(time.Hour))
	return vm
}

// envUnsigned builds an unsigned envelope — the K=1 and transition shape, and
// the one every structural test uses because a signature is orthogonal to
// structure.
func envUnsigned(t *testing.T, parentOuter ids.ID, ts time.Time, pChainHeight uint64, epoch block.Epoch, inner vmchain.Block) block.SignedBlock {
	t.Helper()
	sb, err := block.BuildUnsigned(parentOuter, ts, pChainHeight, epoch, inner.Bytes())
	require.NoError(t, err)
	return sb
}

// envCert mints a classical TLS leaf plus its signer and the NodeID the
// windower would have to elect for a block signed with it to be admitted.
func envCert(t *testing.T) (*staking.Certificate, crypto.Signer, ids.NodeID) {
	t.Helper()
	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	parsed, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	cert := &ids.Certificate{Raw: parsed.Raw, PublicKey: parsed.PublicKey}
	return cert, tlsCert.PrivateKey.(crypto.Signer), ids.NodeIDFromCert(cert)
}

// envPersist writes [sb] into the block store and returns the envelope the VM
// will hand back for its id — a freshly reconstructed one, exactly as a verify
// of a gossiped child resolves its parent.
func envPersist(t *testing.T, vm *VM, sb block.SignedBlock) *postForkBlock {
	t.Helper()
	require.NoError(t, vm.State.PutBlock(sb))
	blk, err := vm.getPostForkBlock(context.Background(), sb.ID())
	require.NoError(t, err)
	pfb, ok := blk.(*postForkBlock)
	require.True(t, ok, "a signed stateless block must resolve to a *postForkBlock")
	return pfb
}

// envChild builds the child envelope in the shape the tests keep reusing: one
// second after its parent, at the parent's P-chain height, no epoch.
func envChild(t *testing.T, parent block.SignedBlock, inner vmchain.Block) block.SignedBlock {
	t.Helper()
	return envUnsigned(t, parent.ID(), parent.Timestamp().Add(time.Second), parent.PChainHeight(), block.Epoch{}, inner)
}

// envVerify runs the production admission path on a child envelope: resolve the
// parent through the VM, then let the parent judge the child.
func envVerify(t *testing.T, vm *VM, sb block.SignedBlock, inner vmchain.Block) error {
	t.Helper()
	child := &postForkBlock{
		SignedBlock:              sb,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: inner},
	}
	return child.Verify(context.Background())
}

// TestEnvelopeCommitsToItsParentsInnerBlock is the property the mainnet halt
// turned on, stated both ways.
//
// FORWARD: naming an outer parent buys nothing. Two children name the SAME
// parent envelope; only the one whose inner block descends from that parent's
// inner block is admitted. An envelope's outer parentage is a claim; its inner
// parentage is the thing the chain is made of.
//
// REVERSE: the parent's outer identity is not what the child is judged against.
// Two DISTINCT envelopes over the SAME inner block — the alias shape that
// produced 758 wrappers of one EVM block — both admit the same child. That is
// precisely why collapsing aliases on CanonicalID is safe: admission never
// depended on which wrapper you asked.
func TestEnvelopeCommitsToItsParentsInnerBlock(t *testing.T) {
	require := require.New(t)
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	parentInner := inner.mint(ids.Empty, 10, envT0)
	descendant := inner.mint(parentInner.ID(), 11, envT0.Add(time.Second))
	orphan := inner.mint(ids.GenerateTestID(), 11, envT0.Add(time.Second))

	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, parentInner)
	envPersist(t, vm, parentSB)

	require.NoError(envVerify(t, vm, envChild(t, parentSB, descendant), descendant),
		"a child whose inner block extends the parent's inner block is the only valid child")

	require.ErrorIs(envVerify(t, vm, envChild(t, parentSB, orphan), orphan), errInnerParentMismatch,
		"naming the right outer parent must not admit an inner block that extends something else")

	// The alias: a second envelope over the SAME inner block, differing only in
	// its own outer parent and timestamp, so it has a different id.
	aliasSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, parentInner)
	envPersist(t, vm, aliasSB)
	require.NotEqual(parentSB.ID(), aliasSB.ID(), "the two wrappers must be distinct envelopes for this to mean anything")

	require.NoError(envVerify(t, vm, envChild(t, aliasSB, descendant), descendant),
		"admission depends on the parent's INNER block, not on which wrapper of it was named — the premise of the canonical collapse")
}

// TestEnvelopeRefusesNonMonotonicPChainHeight pins the one-way ratchet on the
// P-chain view an envelope carries. A child may hold the parent's height or
// advance past it; it may never walk it back, because the validator set the
// child's proposer was elected from must not be older than its parent's.
func TestEnvelopeRefusesNonMonotonicPChainHeight(t *testing.T) {
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	parentInner := inner.mint(ids.Empty, 10, envT0)
	childInner := inner.mint(parentInner.ID(), 11, envT0.Add(time.Second))
	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, parentInner)
	envPersist(t, vm, parentSB)

	for _, tt := range []struct {
		name         string
		pChainHeight uint64
		wantErr      error
	}{
		{name: "below_parent", pChainHeight: envPChainHeight - 1, wantErr: errPChainHeightNotMonotonic},
		{name: "equal_to_parent", pChainHeight: envPChainHeight},
		{name: "above_parent", pChainHeight: envPChainHeight + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sb := envUnsigned(t, parentSB.ID(), envT0.Add(time.Second), tt.pChainHeight, block.Epoch{}, childInner)
			err := envVerify(t, vm, sb, childInner)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestEnvelopeTimeIsMonotonicAndBounded pins both ends of the window an
// envelope's timestamp must land in: never behind its parent, and never further
// ahead of this node's clock than maxSkew. The boundary is included on purpose
// — exactly maxSkew is legal, one granularity beyond it is not — because an
// off-by-one there either rejects honest blocks fleet-wide or admits a
// timestamp that lets a proposer claim a slot it does not own yet.
func TestEnvelopeTimeIsMonotonicAndBounded(t *testing.T) {
	inner := newEnvInner()
	vm := newEnvVM(t, inner)
	now := vm.Time()

	parentInner := inner.mint(ids.Empty, 10, envT0)
	childInner := inner.mint(parentInner.ID(), 11, envT0)
	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, parentInner)
	envPersist(t, vm, parentSB)

	for _, tt := range []struct {
		name    string
		ts      time.Time
		wantErr error
	}{
		{name: "before_parent", ts: envT0.Add(-time.Second), wantErr: errTimeNotMonotonic},
		{name: "equal_to_parent", ts: envT0},
		{name: "at_the_skew_bound", ts: now.Add(maxSkew)},
		{name: "one_second_past_the_skew_bound", ts: now.Add(maxSkew + time.Second), wantErr: errTimeTooAdvanced},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sb := envUnsigned(t, parentSB.ID(), tt.ts, envPChainHeight, block.Epoch{}, childInner)
			err := envVerify(t, vm, sb, childInner)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestStrictPQRefusesAClassicalProposerBeforeAnythingElse pins the profile
// refusal as UNCONDITIONAL, which is the whole of its value.
//
// On a chain whose own proposer signs with ML-DSA-65 the validator set is
// ML-DSA-keyed, so a secp256k1 proposer identity can never be a legitimate
// proposer. The refusal therefore may not sit behind the Ready check: a node
// still bootstrapping or state-syncing skips the proposer-window check
// entirely, and that is exactly the window in which a classical envelope would
// otherwise be admitted and land in the accepted chain. The subtests fix the
// consensus state BELOW Ready so a gate that moved inside the Ready branch
// would let the block through.
func TestStrictPQRefusesAClassicalProposerBeforeAnythingElse(t *testing.T) {
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	parentInner := inner.mint(ids.Empty, 10, envT0)
	childInner := inner.mint(parentInner.ID(), 11, envT0.Add(time.Second))
	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, parentInner)
	envPersist(t, vm, parentSB)

	cert, signer, _ := envCert(t)
	classical, err := block.Build(
		parentSB.ID(), envT0.Add(time.Second), envPChainHeight, block.Epoch{},
		cert, childInner.Bytes(), vm.rt.ChainID, signer,
	)
	require.NoError(t, err)
	require.True(t, classical.HasClassicalProposer(), "the fixture must actually carry a classical identity")

	mldsaKey, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	require.NoError(t, err)
	mldsaPub := mldsaKey.Public().(*mldsa.PublicKey).Bytes()
	pq, err := block.BuildMLDSA(
		parentSB.ID(), envT0.Add(time.Second), envPChainHeight, block.Epoch{},
		mldsaPub, childInner.Bytes(), vm.rt.ChainID, mldsaKey,
	)
	require.NoError(t, err)
	require.False(t, pq.HasClassicalProposer())

	for _, cs := range []vmcore.State{vmcore.Unknown, vmcore.Syncing, vmcore.Bootstrapping} {
		t.Run("classical_refused_at_state_"+cs.String(), func(t *testing.T) {
			vm.StakingMLDSASigner = mldsaKey
			vm.consensusState = uint32(cs)
			require.ErrorIs(t, envVerify(t, vm, classical, childInner), errClassicalProposerUnderStrictPQ,
				"a strict-PQ chain must refuse a classical proposer even where the proposer-window check does not run")
		})
	}

	t.Run("strict_pq_admits_its_own_scheme", func(t *testing.T) {
		vm.StakingMLDSASigner = mldsaKey
		vm.consensusState = uint32(vmcore.Bootstrapping)
		require.NoError(t, envVerify(t, vm, pq, childInner),
			"the refusal is of the classical SCHEME, not of signed blocks")
	})

	t.Run("classical_chain_is_untouched", func(t *testing.T) {
		vm.StakingMLDSASigner = nil
		vm.consensusState = uint32(vmcore.Bootstrapping)
		require.NoError(t, envVerify(t, vm, classical, childInner),
			"a chain that is not strict-PQ must keep accepting classical proposers")
	})
}

// TestEnvelopeBindsItsProposerToTheElectedSlot covers the proposer half of
// admission, which only runs once consensus is Ready.
//
// The envelope carries a proposer identity and a timestamp; the timestamp fixes
// the slot, the slot fixes who was elected, and the two must agree in both
// directions. Signed-when-nobody-is-elected and unsigned-when-someone-is are
// both refused, because either one lets a node put a block at a height it does
// not own. The slot is also recorded on the block here — it is the only place
// it is computed, and Accept reports it.
func TestEnvelopeBindsItsProposerToTheElectedSlot(t *testing.T) {
	inner := newEnvInner()
	vm := newEnvVM(t, inner)
	vm.consensusState = uint32(vmcore.Ready)

	parentInner := inner.mint(ids.Empty, 10, envT0)
	childInner := inner.mint(parentInner.ID(), 11, envT0)
	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, parentInner)
	envPersist(t, vm, parentSB)

	// Ready checks the epoch too; derive the one this parent implies so the
	// proposer verdict is the only thing that can fail.
	childTS := envT0.Add(2 * proposer.WindowDuration)
	wantSlot := proposer.TimeToSlot(envT0, childTS)
	require.Equal(t, uint64(2), wantSlot, "the fixture must land in a non-zero slot")
	epoch := block.Epoch{PChainHeight: envPChainHeight, Number: 1, StartTime: envT0.Unix()}

	cert, signer, certNodeID := envCert(t)
	signed, err := block.Build(parentSB.ID(), childTS, envPChainHeight, epoch, cert, childInner.Bytes(), vm.rt.ChainID, signer)
	require.NoError(t, err)
	unsigned := envUnsigned(t, parentSB.ID(), childTS, envPChainHeight, epoch, childInner)

	t.Run("elected_proposer_signs", func(t *testing.T) {
		var sawSlot uint64
		vm.Windower = envWindower{expected: func(slot uint64) (ids.NodeID, error) {
			sawSlot = slot
			return certNodeID, nil
		}}
		child := &postForkBlock{
			SignedBlock:              signed,
			postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: childInner},
		}
		require.NoError(t, child.Verify(context.Background()))
		require.Equal(t, wantSlot, sawSlot, "the slot the election is asked about comes from the block's own timestamp")
		require.NotNil(t, child.slot, "the verified slot must be recorded — Accept reports it")
		require.Equal(t, wantSlot, *child.slot)
	})

	t.Run("someone_else_was_elected", func(t *testing.T) {
		other := ids.GenerateTestNodeID()
		vm.Windower = envWindower{expected: func(uint64) (ids.NodeID, error) { return other, nil }}
		require.ErrorIs(t, envVerify(t, vm, signed, childInner), errUnexpectedProposer)
	})

	t.Run("unsigned_where_a_proposer_was_elected", func(t *testing.T) {
		// The empty proposer of an unsigned block is compared against the
		// election like any other identity, so this comes out as "not the
		// expected proposer" rather than a signed/unsigned mismatch. Either
		// way the slot's owner is the only node that can fill it.
		vm.Windower = envWindower{expected: func(uint64) (ids.NodeID, error) { return certNodeID, nil }}
		require.ErrorIs(t, envVerify(t, vm, unsigned, childInner), errUnexpectedProposer,
			"an elected slot may not be filled by an unsigned block — nothing would bind it to its proposer")
	})

	t.Run("open_schedule_takes_unsigned_only", func(t *testing.T) {
		vm.Windower = envWindower{} // ErrAnyoneCanPropose
		require.NoError(t, envVerify(t, vm, unsigned, childInner))
		require.ErrorIs(t, envVerify(t, vm, signed, childInner), errProposerMismatch,
			"with no schedule there is no election to point at, so a signature claims a slot that does not exist")
	})

	t.Run("election_failure_is_surfaced", func(t *testing.T) {
		boom := errors.New("validator set unavailable")
		vm.Windower = envWindower{expected: func(uint64) (ids.NodeID, error) { return ids.EmptyNodeID, boom }}
		require.ErrorIs(t, envVerify(t, vm, signed, childInner), boom,
			"an unresolvable schedule must fail the verify, never default to admitting the block")
	})
}

// TestReadyEnvelopeIsBoundToThePChainViewAndEpoch pins the two remaining
// Ready-only commitments. The P-chain height an envelope names must be one this
// node has actually reached — a block referring to a validator set from the
// future is unverifiable, not merely early — and the epoch must be the one
// LP-181 derives from the parent, so an envelope cannot pick its own epoch and
// with it the validator view its children inherit.
func TestReadyEnvelopeIsBoundToThePChainViewAndEpoch(t *testing.T) {
	inner := newEnvInner()
	vm := newEnvVM(t, inner)
	vm.consensusState = uint32(vmcore.Ready)

	parentInner := inner.mint(ids.Empty, 10, envT0)
	childInner := inner.mint(parentInner.ID(), 11, envT0)
	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, parentInner)
	envPersist(t, vm, parentSB)

	derived := block.Epoch{PChainHeight: envPChainHeight, Number: 1, StartTime: envT0.Unix()}

	t.Run("epoch_must_be_the_derived_one", func(t *testing.T) {
		sb := envUnsigned(t, parentSB.ID(), envT0.Add(time.Second), envPChainHeight, block.Epoch{}, childInner)
		require.ErrorIs(t, envVerify(t, vm, sb, childInner), errEpochMismatch)

		wrongNumber := derived
		wrongNumber.Number = 2
		sb = envUnsigned(t, parentSB.ID(), envT0.Add(time.Second), envPChainHeight, wrongNumber, childInner)
		require.ErrorIs(t, envVerify(t, vm, sb, childInner), errEpochMismatch)

		sb = envUnsigned(t, parentSB.ID(), envT0.Add(time.Second), envPChainHeight, derived, childInner)
		require.NoError(t, envVerify(t, vm, sb, childInner))
	})

	t.Run("pchain_height_may_not_exceed_ours", func(t *testing.T) {
		sb := envUnsigned(t, parentSB.ID(), envT0.Add(time.Second), envPChainHeight+1, derived, childInner)
		require.ErrorIs(t, envVerify(t, vm, sb, childInner), errPChainHeightNotReached)
	})

	t.Run("unreadable_pchain_height_fails_the_verify", func(t *testing.T) {
		boom := errors.New("P-chain unreachable")
		vm.validatorState = &validatorstest.State{
			GetCurrentHeightF: func(context.Context) (uint64, error) { return 0, boom },
		}
		defer func() {
			vm.validatorState = &validatorstest.State{
				GetCurrentHeightF: func(context.Context) (uint64, error) { return envPChainHeight, nil },
			}
		}()
		sb := envUnsigned(t, parentSB.ID(), envT0.Add(time.Second), envPChainHeight, derived, childInner)
		require.ErrorIs(t, envVerify(t, vm, sb, childInner), boom)
	})
}

// TestOracleParentAdmitsOptionsAndRefusesPlainChildren pins the two-sided rule
// on oracle inner blocks. An oracle block's successors are its own options and
// nothing else, so a plain post-fork child of one is refused; conversely an
// option may only hang off a parent that really is an oracle. The middle case
// matters most: an inner block that reports errNotOracle is a normal block, and
// treating that sentinel as a failure would refuse every ordinary child on the
// chain.
func TestOracleParentAdmitsOptionsAndRefusesPlainChildren(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	// An oracle parent and the two options it offers.
	base := inner.mint(ids.Empty, 10, envT0)
	oracleInner := &envOracle{Block: base}
	optA := inner.mint(base.ID(), 11, envT0.Add(time.Second))
	optB := inner.mint(base.ID(), 11, envT0.Add(time.Second))
	oracleInner.opts = [2]vmchain.Block{optA, optB}
	inner.add(oracleInner)

	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, oracleInner)
	require.NoError(vm.State.PutBlock(parentSB))
	oracleParent := &postForkBlock{
		SignedBlock:              parentSB,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: oracleInner},
	}
	vm.recordVerifiedBlock(oracleParent)

	// A plain child of an oracle is refused outright.
	plain := envChild(t, parentSB, optA)
	require.ErrorIs(envVerify(t, vm, plain, optA), errUnexpectedBlockType,
		"an oracle block's successor is one of its options, never a plain child")

	// The options this envelope offers are parented at the ENVELOPE, wrap the
	// inner options, and each reports the inner option as its canonical id.
	outerOptions, err := oracleParent.Options(ctx)
	require.NoError(err)
	for i, want := range []vmchain.Block{optA, optB} {
		opt, ok := outerOptions[i].(*postForkOption)
		require.True(ok)
		require.Equal(parentSB.ID(), opt.Parent(), "an option hangs off the envelope that produced it")
		require.Equal(want.ID(), opt.innerBlk.ID())
		require.Equal(want.ID(), opt.CanonicalID())
		require.NoError(opt.Verify(ctx), "an option of this oracle must verify against it")
		require.Equal(parentSB.Timestamp(), opt.Timestamp(),
			"an option carries no time of its own — it inherits the envelope's")
	}

	// An option whose inner block is not one of the oracle's descendants is
	// refused for the same reason a plain child would be.
	stray := inner.mint(ids.GenerateTestID(), 11, envT0.Add(time.Second))
	strayOpt, err := block.BuildOption(parentSB.ID(), stray.Bytes())
	require.NoError(err)
	strayBlk := &postForkOption{
		Block:                    strayOpt,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: stray},
	}
	require.ErrorIs(strayBlk.Verify(ctx), errInnerParentMismatch)

	// And an option may not hang off a non-oracle envelope.
	plainInner := inner.mint(ids.Empty, 10, envT0)
	plainSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, plainInner)
	envPersist(t, vm, plainSB)
	childOfPlain := inner.mint(plainInner.ID(), 11, envT0.Add(time.Second))
	optOfPlain, err := block.BuildOption(plainSB.ID(), childOfPlain.Bytes())
	require.NoError(err)
	optBlk := &postForkOption{
		Block:                    optOfPlain,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: childOfPlain},
	}
	require.ErrorIs(optBlk.Verify(ctx), errUnexpectedBlockType)

	// A normal inner block reports errNotOracle, and that sentinel must read as
	// "not an oracle", not as a verification failure.
	normal := &envOracle{Block: inner.mint(ids.Empty, 10, envT0), err: errNotOracle}
	inner.add(normal)
	normalSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, normal)
	envPersist(t, vm, normalSB)
	normalChild := inner.mint(normal.ID(), 11, envT0.Add(time.Second))
	require.NoError(envVerify(t, vm, envChild(t, normalSB, normalChild), normalChild),
		"errNotOracle means ordinary block; reading it as a failure would refuse every child on the chain")

	// Any OTHER error from Options is a real failure and must surface.
	boom := errors.New("oracle exploded")
	broken := &envOracle{Block: inner.mint(ids.Empty, 10, envT0), err: boom}
	inner.add(broken)
	brokenSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, broken)
	envPersist(t, vm, brokenSB)
	brokenChild := inner.mint(broken.ID(), 11, envT0.Add(time.Second))
	require.ErrorIs(envVerify(t, vm, envChild(t, brokenSB, brokenChild), brokenChild), boom)
}

// TestPostForkParentRefusesAPreForkChild pins the one-way door at the fork. Once
// a height is wrapped, its successors are wrapped too; a bare inner block
// claiming to descend from an envelope has no envelope of its own and could
// never be indexed, which is the shape that leaves the finality index behind the
// inner tip.
func TestPostForkParentRefusesAPreForkChild(t *testing.T) {
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	parentInner := inner.mint(ids.Empty, 10, envT0)
	parentSB := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, parentInner)
	parent := envPersist(t, vm, parentSB)

	childInner := inner.mint(parentInner.ID(), 11, envT0.Add(time.Second))
	pre := &preForkBlock{Block: childInner, vm: vm}
	require.ErrorIs(t, parent.verifyPreForkChild(context.Background(), pre), errUnsignedChild)

	// An option cannot father a pre-fork child either, nor another option.
	opt := &postForkOption{postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: parentInner}}
	require.ErrorIs(t, opt.verifyPreForkChild(context.Background(), pre), errUnsignedChild)
	require.ErrorIs(t, opt.verifyPostForkOption(context.Background(), nil), errUnexpectedBlockType)
}

// TestTransitionBlockMustBeUnsignedAndExtendItsPreForkParent pins the fork
// boundary itself. The first post-fork block is the only block whose parent is a
// bare inner block, and it is the one block on the chain that cannot be bound to
// a proposer: verifyPostForkChild refuses a signature on it, so every honest
// node must produce it unsigned. Its inner parent is the pre-fork block itself,
// and its epoch is derived from an empty parent epoch.
func TestTransitionBlockMustBeUnsignedAndExtendItsPreForkParent(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	last := inner.mint(ids.Empty, 10, envT0)
	pre := &preForkBlock{Block: last, vm: vm}
	transitionInner := inner.mint(last.ID(), 11, envT0.Add(time.Second))
	childTS := envT0.Add(time.Second)
	epoch := block.Epoch{PChainHeight: 0, Number: 1, StartTime: envT0.Unix()}

	good := envUnsigned(t, ids.GenerateTestID(), childTS, envPChainHeight, epoch, transitionInner)
	child := &postForkBlock{
		SignedBlock:              good,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: transitionInner},
	}
	require.NoError(pre.verifyPostForkChild(ctx, child))

	// Signed: refused, whatever else is right about it.
	cert, signer, _ := envCert(t)
	signed, err := block.Build(ids.GenerateTestID(), childTS, envPChainHeight, epoch, cert, transitionInner.Bytes(), vm.rt.ChainID, signer)
	require.NoError(err)
	require.ErrorIs(pre.verifyPostForkChild(ctx, &postForkBlock{
		SignedBlock:              signed,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: transitionInner},
	}), errChildOfPreForkBlockHasProposer)

	// Inner parentage still has to hold across the fork.
	orphan := inner.mint(ids.GenerateTestID(), 11, childTS)
	orphanSB := envUnsigned(t, ids.GenerateTestID(), childTS, envPChainHeight, epoch, orphan)
	require.ErrorIs(pre.verifyPostForkChild(ctx, &postForkBlock{
		SignedBlock:              orphanSB,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: orphan},
	}), errInnerParentMismatch)

	// And the P-chain ceiling and epoch derivation apply here as they do after
	// the fork — the transition block is not a hole in the rules.
	tooHigh := envUnsigned(t, ids.GenerateTestID(), childTS, envPChainHeight+1, epoch, transitionInner)
	require.ErrorIs(pre.verifyPostForkChild(ctx, &postForkBlock{
		SignedBlock:              tooHigh,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: transitionInner},
	}), errPChainHeightNotReached)

	wrongEpoch := envUnsigned(t, ids.GenerateTestID(), childTS, envPChainHeight, block.Epoch{}, transitionInner)
	require.ErrorIs(pre.verifyPostForkChild(ctx, &postForkBlock{
		SignedBlock:              wrongEpoch,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: transitionInner},
	}), errEpochMismatch)

	behind := envUnsigned(t, ids.GenerateTestID(), envT0.Add(-time.Second), envPChainHeight, block.Epoch{PChainHeight: 0, Number: 1, StartTime: envT0.Unix()}, transitionInner)
	require.ErrorIs(pre.verifyPostForkChild(ctx, &postForkBlock{
		SignedBlock:              behind,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: transitionInner},
	}), errTimeNotMonotonic)
}
