// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
	lux "github.com/luxfi/utxo"
)

// makeTx builds a real signed-shaped tx (an in-process BaseTx with no creds)
// whose Bytes()/ID() are bound, suitable for embedding in a block.
func makeTx(t *testing.T, networkID uint32) *txs.Tx {
	t.Helper()
	utx, err := txs.NewBaseTx(&lux.BaseTx{
		NetworkID:    networkID,
		BlockchainID: ids.GenerateTestID(),
	})
	require.NoError(t, err)
	tx := &txs.Tx{Unsigned: utx}
	require.NoError(t, tx.Initialize())
	return tx
}

// requireSameTxs asserts two tx slices are byte- and ID-equal in order.
func requireSameTxs(t *testing.T, want, got []*txs.Tx) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].ID(), got[i].ID(), "tx %d ID", i)
		require.Equal(t, want[i].Bytes(), got[i].Bytes(), "tx %d bytes", i)
	}
}

func TestAbortBlockRoundTrip(t *testing.T) {
	require := require.New(t)
	ts := time.Unix(1_700_000_000, 0)
	parentID := ids.GenerateTestID()

	blk, err := NewAbortBlock(ts, parentID, 42)
	require.NoError(err)

	parsed, err := Parse(blk.Bytes())
	require.NoError(err)

	require.IsType(&AbortBlock{}, parsed)
	require.Equal(blk.ID(), parsed.ID())
	require.Equal(parentID, parsed.Parent())
	require.Equal(uint64(42), parsed.Height())
	require.Equal(ts.Unix(), parsed.(*AbortBlock).Timestamp().Unix())
	require.Nil(parsed.Txs())

	// Idempotent Visit dispatch.
	require.NoError(parsed.Visit(dispatchVisitor{}))
}

func TestCommitBlockRoundTrip(t *testing.T) {
	require := require.New(t)
	ts := time.Unix(1_700_000_123, 0)
	parentID := ids.GenerateTestID()

	blk, err := NewCommitBlock(ts, parentID, 7)
	require.NoError(err)

	parsed, err := Parse(blk.Bytes())
	require.NoError(err)

	require.IsType(&CommitBlock{}, parsed)
	require.Equal(blk.ID(), parsed.ID())
	require.Equal(parentID, parsed.Parent())
	require.Equal(uint64(7), parsed.Height())
	require.Equal(ts.Unix(), parsed.(*CommitBlock).Timestamp().Unix())
	require.Nil(parsed.Txs())
}

func TestStandardBlockRoundTrip(t *testing.T) {
	require := require.New(t)
	ts := time.Unix(1_700_001_000, 0)
	parentID := ids.GenerateTestID()
	decision := []*txs.Tx{makeTx(t, 11), makeTx(t, 22)}

	blk, err := NewStandardBlock(ts, parentID, 100, decision)
	require.NoError(err)

	parsed, err := Parse(blk.Bytes())
	require.NoError(err)

	require.IsType(&StandardBlock{}, parsed)
	require.Equal(blk.ID(), parsed.ID())
	require.Equal(parentID, parsed.Parent())
	require.Equal(uint64(100), parsed.Height())
	require.Equal(ts.Unix(), parsed.(*StandardBlock).Timestamp().Unix())
	requireSameTxs(t, decision, parsed.Txs())
}

func TestStandardBlockEmptyTxsRoundTrip(t *testing.T) {
	require := require.New(t)
	ts := time.Unix(1_700_002_000, 0)
	parentID := ids.GenerateTestID()

	blk, err := NewStandardBlock(ts, parentID, 5, nil)
	require.NoError(err)

	parsed, err := Parse(blk.Bytes())
	require.NoError(err)
	require.Empty(parsed.Txs())
}

func TestProposalBlockRoundTrip(t *testing.T) {
	require := require.New(t)
	ts := time.Unix(1_700_003_000, 0)
	parentID := ids.GenerateTestID()
	proposalTx := makeTx(t, 99)
	decision := []*txs.Tx{makeTx(t, 33)}

	blk, err := NewProposalBlock(ts, parentID, 250, proposalTx, decision)
	require.NoError(err)

	parsed, err := Parse(blk.Bytes())
	require.NoError(err)

	pb, ok := parsed.(*ProposalBlock)
	require.True(ok)
	require.Equal(blk.ID(), pb.ID())
	require.Equal(parentID, pb.Parent())
	require.Equal(uint64(250), pb.Height())
	require.Equal(ts.Unix(), pb.Timestamp().Unix())

	// Tx() returns the single proposal tx.
	require.Equal(proposalTx.ID(), pb.Tx().ID())
	require.Equal(proposalTx.Bytes(), pb.Tx().Bytes())

	// Txs() returns decision txs followed by the proposal tx (last).
	want := append(append([]*txs.Tx{}, decision...), proposalTx)
	requireSameTxs(t, want, pb.Txs())
}

// dispatchVisitor is a no-op Visitor used to exercise Visit dispatch.
type dispatchVisitor struct{}

func (dispatchVisitor) AbortBlock(*AbortBlock) error       { return nil }
func (dispatchVisitor) CommitBlock(*CommitBlock) error     { return nil }
func (dispatchVisitor) ProposalBlock(*ProposalBlock) error { return nil }
func (dispatchVisitor) StandardBlock(*StandardBlock) error { return nil }
