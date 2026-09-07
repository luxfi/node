// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"

	"github.com/luxfi/node/vms/platformvm/txs"
)

// The P-Chain block wire is native ZAP struct-is-wire (single format, no
// codec). BlockIDs are hash(canonical block bytes) and inner TxIDs ride the
// same encoding, so the block wire MUST be canonical + non-malleable. These
// are the block-level determinism assertions RED verifies before re-genesis.

// blockDeterminismTx returns a signed decision tx for the tx-bearing block
// fixtures. Signed with nil signers (no credentials) so it is fully
// deterministic and CGO-independent.
func blockDeterminismTx(t *testing.T) *txs.Tx {
	t.Helper()
	base := &lux.BaseTx{
		NetworkID:    constants.MainnetID,
		BlockchainID: constants.PlatformChainID,
		Outs: []*lux.TransferableOutput{
			{
				Asset: lux.Asset{ID: ids.ID{0x09, 0x09, 0x09}},
				Out: &secp256k1fx.TransferOutput{
					Amt: constants.MilliLux,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{{0x01, 0x02, 0x03}},
					},
				},
			},
		},
	}
	unsigned, err := txs.NewBaseTx(base)
	require.NoError(t, err)
	tx := &txs.Tx{Unsigned: unsigned}
	require.NoError(t, tx.Initialize())
	return tx
}

func blockDeterminismFixtures(t *testing.T) []struct {
	name string
	blk  Block
} {
	t.Helper()
	var (
		ts     = time.Unix(1_700_000_000, 0)
		parent = ids.ID{0x0a, 0x0b, 0x0c, 0x0d}
		height = uint64(42)
		tx     = blockDeterminismTx(t)
	)

	abort, err := NewAbortBlock(ts, parent, height)
	require.NoError(t, err)
	commit, err := NewCommitBlock(ts, parent, height)
	require.NoError(t, err)
	standard, err := NewStandardBlock(ts, parent, height, []*txs.Tx{tx})
	require.NoError(t, err)
	proposal, err := NewProposalBlock(ts, parent, height, tx, []*txs.Tx{tx})
	require.NoError(t, err)

	return []struct {
		name string
		blk  Block
	}{
		{"AbortBlock", abort},
		{"CommitBlock", commit},
		{"StandardBlock", standard},
		{"ProposalBlock", proposal},
	}
}

// TestBlockRoundTripByteStability proves, for every block type, that the
// canonical bytes survive Parse unchanged and that the BlockID is stable.
// This is the invariant that keeps the fleet from forking on block hashes.
func TestBlockRoundTripByteStability(t *testing.T) {
	for _, f := range blockDeterminismFixtures(t) {
		f := f
		t.Run(f.name, func(t *testing.T) {
			require := require.New(t)

			wire := f.blk.Bytes()
			require.NotEmpty(wire)

			parsed, err := Parse(wire)
			require.NoError(err)

			// Byte-preserving read: Parse wraps the input verbatim.
			require.Equal(wire, parsed.Bytes(), "Parse must preserve block bytes")
			require.Equal(f.blk.ID(), parsed.ID(), "BlockID must be stable across parse")

			// Re-parsing the parsed block's bytes reproduces the same ID —
			// the encoding is canonical (struct IS the wire; nothing to
			// re-marshal).
			reparsed, err := Parse(parsed.Bytes())
			require.NoError(err)
			require.Equal(f.blk.ID(), reparsed.ID(),
				"decode->decode is not stable; block encoding is non-canonical")
		})
	}
}

// TestGoldenAbortBlock pins the exact wire bytes and BlockID of a fixed
// AbortBlock. Any drift in block-wire layout (kind byte width/position, field
// order/endianness, zap header) breaks this — which is the point: these bytes
// are the chain commitment.
func TestGoldenAbortBlock(t *testing.T) {
	require := require.New(t)

	var parent ids.ID
	for i := range parent {
		parent[i] = byte(i)
	}
	blk, err := NewAbortBlock(time.Unix(0x0102030405060708, 0), parent, 0x1122334455667788)
	require.NoError(err)

	// Native-ZAP golden captured at the re-genesis cutover:
	// zap header (magic ZAP\0, version 2, root@16, size 65) + object
	// {kind=1 @16, ParentID @17, Height LE @49, Time LE @57}.
	golden := []byte{
		0x5a, 0x41, 0x50, 0x00, 0x02, 0x00, 0x00, 0x00,
		0x10, 0x00, 0x00, 0x00, 0x41, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06,
		0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e,
		0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16,
		0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e,
		0x1f, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22,
		0x11, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02,
		0x01,
	}
	require.Equal(golden, blk.Bytes(), "AbortBlock golden wire bytes drifted")
	require.Equal(ids.ID(hash.ComputeHash256Array(golden)), blk.ID(),
		"BlockID = hash(wire) is not stable")
}

// TestBlockTrailingBytesRejected is the block-level malleability guard: a
// buffer with extra tail bytes wraps the same zap message but hashes to a
// different ID, so Parse MUST reject it.
func TestBlockTrailingBytesRejected(t *testing.T) {
	require := require.New(t)

	blk, err := NewCommitBlock(time.Unix(1_700_000_000, 0), ids.ID{0x01}, 7)
	require.NoError(err)

	malleable := append(append([]byte{}, blk.Bytes()...), 0x00)
	_, err = Parse(malleable)
	require.ErrorIs(err, ErrExtraSpace,
		"trailing bytes on a block MUST be rejected")
}
