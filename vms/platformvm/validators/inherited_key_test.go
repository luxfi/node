// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/timer/mockable"
	validatorsp "github.com/luxfi/validators"
)

// keyedStakerState is the State the manager reads through. GetCurrentValidators
// returns the chain staker exactly as the state layer hands it over — carrying
// the key inherited from its primary-network validator.
//
// publicKeyDiffs models the height-indexed public key diffs: heights the
// validator changed at. A validator that did not change between the target and
// current heights has no entry, which is the ordinary case and the one where
// the fast path and the replay path must not disagree.
type keyedStakerState struct {
	chainID       ids.ID
	staker        *state.Staker
	currentHeight uint64
	lastAccepted  ids.ID
	block         block.Block

	publicKeyDiffs map[uint64][]byte
}

func (s *keyedStakerState) GetTx(ids.ID) (*txs.Tx, status.Status, error) {
	return nil, status.Unknown, nil
}
func (s *keyedStakerState) GetLastAccepted() ids.ID { return s.lastAccepted }
func (s *keyedStakerState) GetStatelessBlock(ids.ID) (block.Block, error) {
	return s.block, nil
}

func (s *keyedStakerState) ApplyValidatorWeightDiffs(
	context.Context,
	map[ids.NodeID]*validatorsp.GetValidatorOutput,
	uint64, uint64, ids.ID,
) error {
	return nil
}

// ApplyValidatorPublicKeyDiffs mirrors the real implementation
// (state_diffs.go): for each height walked, a validator present in the set is
// assigned the diff's recorded key, and an empty diff value clears it. A
// validator with no entry at that height is left untouched.
func (s *keyedStakerState) ApplyValidatorPublicKeyDiffs(
	_ context.Context,
	vdrs map[ids.NodeID]*validatorsp.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	_ ids.ID,
) error {
	for height := startHeight; height >= endHeight; height-- {
		pkBytes, ok := s.publicKeyDiffs[height]
		if !ok {
			if height == 0 {
				break
			}
			continue
		}
		vdr, ok := vdrs[s.staker.NodeID]
		if !ok {
			continue
		}
		if len(pkBytes) == 0 {
			vdr.PublicKey = nil
		} else {
			vdr.PublicKey = pkBytes
		}
		if height == 0 {
			break
		}
	}
	return nil
}

func (s *keyedStakerState) GetCurrentValidators(
	context.Context, ids.ID,
) ([]*state.Staker, []state.L1Validator, uint64, error) {
	return []*state.Staker{s.staker}, nil, s.currentHeight, nil
}

func newManagerFor(t *testing.T, st State, chainID ids.ID) *manager {
	t.Helper()
	m, err := metrics.New(metric.NewRegistry())
	require.NoError(t, err)
	return &manager{
		state:         st,
		metrics:       m,
		clk:           &mockable.Clock{},
		trackedChains: set.NewSet[ids.ID](0),
		caches:        make(map[ids.ID]cache.Cacher[uint64, map[ids.NodeID]*validatorsp.GetValidatorOutput]),
	}
}

// TestLegacyChainValidatorKeyedAtBothHeights is the BUG-2 surfacing gate.
//
// The set handed to quorum and warp must carry the validator's inherited key at
// the tip and at any replayed height alike. If the two paths disagree, honest
// nodes at different heights verify votes against different keys — and the one
// that reads keyless drops the vote while still counting the weight.
func TestLegacyChainValidatorKeyedAtBothHeights(t *testing.T) {
	require := require.New(t)

	sk, err := localsigner.New()
	require.NoError(err)
	inherited := bls.PublicKeyToUncompressedBytes(sk.PublicKey())

	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()
	const currentHeight = 100

	st := &keyedStakerState{
		chainID: chainID,
		staker: &state.Staker{
			TxID:      ids.GenerateTestID(),
			NodeID:    nodeID,
			PublicKey: sk.PublicKey(), // what the fixed state layer surfaces
			ChainID:   chainID,
			Weight:    1000,
		},
		currentHeight:  currentHeight,
		lastAccepted:   ids.GenerateTestID(),
		block:          blockAtHeight(t, currentHeight),
		publicKeyDiffs: map[uint64][]byte{},
	}
	m := newManagerFor(t, st, chainID)

	// currentHeight == H: the fast path.
	atTip, err := m.GetValidatorSet(context.Background(), currentHeight, chainID)
	require.NoError(err)
	require.Contains(atTip, nodeID)
	require.NotEmpty(atTip[nodeID].PublicKey, "legacy chain validator surfaced KEYLESS at the tip")
	require.Equal(inherited, atTip[nodeID].PublicKey)

	// currentHeight > H: the replay path.
	atPast, err := m.GetValidatorSet(context.Background(), currentHeight-10, chainID)
	require.NoError(err)
	require.Contains(atPast, nodeID)
	require.NotEmpty(atPast[nodeID].PublicKey, "legacy chain validator surfaced KEYLESS on replay")
	require.Equal(inherited, atPast[nodeID].PublicKey)

	// And the two paths must name the same key.
	require.Equal(atTip[nodeID].PublicKey, atPast[nodeID].PublicKey,
		"fast path and replay disagree on the validator's key")
}

// TestReplayedKeyMatchesRecordedDiff pins the replay path to the key the diff
// actually records. calculateValidatorDiffs writes the inherited key in
// uncompressed form; replaying a height that carries that diff must reproduce
// exactly what the fast path surfaces.
func TestReplayedKeyMatchesRecordedDiff(t *testing.T) {
	require := require.New(t)

	sk, err := localsigner.New()
	require.NoError(err)
	inherited := bls.PublicKeyToUncompressedBytes(sk.PublicKey())

	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()
	const currentHeight = 50

	st := &keyedStakerState{
		chainID: chainID,
		staker: &state.Staker{
			TxID:      ids.GenerateTestID(),
			NodeID:    nodeID,
			PublicKey: sk.PublicKey(),
			ChainID:   chainID,
			Weight:    1000,
		},
		currentHeight: currentHeight,
		lastAccepted:  ids.GenerateTestID(),
		block:         blockAtHeight(t, currentHeight),
		// The diff the write path records for a legacy chain validator: the
		// inherited primary key, uncompressed.
		publicKeyDiffs: map[uint64][]byte{45: inherited},
	}
	m := newManagerFor(t, st, chainID)

	atPast, err := m.GetValidatorSet(context.Background(), 40, chainID)
	require.NoError(err)
	require.Contains(atPast, nodeID)
	require.Equal(inherited, atPast[nodeID].PublicKey,
		"replayed key differs from the key the diff recorded")
}

func blockAtHeight(t *testing.T, height uint64) block.Block {
	t.Helper()
	blk, err := block.NewStandardBlock(time.Unix(1000, 0), ids.GenerateTestID(), height, nil)
	require.NoError(t, err)
	return blk
}

// TestKeylessStakerSurfacesKeyless is the negative control for the two tests
// above. The manager is a faithful conduit: it surfaces exactly the key the
// state layer hands it. That is why BUG-2 had to be fixed in the state layer —
// patching the manager would have put a second copy of the inheritance rule
// next to the one in getInheritedPublicKey.
//
// It also shows the shape of the defect: a validator with full weight and no
// key, which VerifyVote rejects while the weight stays in the denominator.
func TestKeylessStakerSurfacesKeyless(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()
	const currentHeight = 20

	st := &keyedStakerState{
		chainID: chainID,
		staker: &state.Staker{
			TxID:      ids.GenerateTestID(),
			NodeID:    nodeID,
			PublicKey: nil, // the pre-fix shape of a legacy chain staker
			ChainID:   chainID,
			Weight:    1000,
		},
		currentHeight:  currentHeight,
		lastAccepted:   ids.GenerateTestID(),
		block:          blockAtHeight(t, currentHeight),
		publicKeyDiffs: map[uint64][]byte{},
	}
	m := newManagerFor(t, st, chainID)

	got, err := m.GetValidatorSet(context.Background(), currentHeight, chainID)
	require.NoError(err)
	require.Contains(got, nodeID)
	require.Empty(got[nodeID].PublicKey)
	require.Equal(uint64(1000), got[nodeID].Weight,
		"the weight counts toward the quorum denominator even with no key to vote with")
}
