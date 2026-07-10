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

	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// The P-Chain block codec is single-version ZAP-native. BlockIDs are
// hash(canonical block bytes) and inner TxIDs ride the same encoding, so
// the block wire MUST be canonical + non-malleable. These are the block-
// level determinism assertions RED verifies before the Nova re-genesis.

// blockDeterminismTx returns a signed decision tx for the tx-bearing block
// fixtures. Signed with nil signers (no credentials) so it is fully
// deterministic and CGO-independent.
func blockDeterminismTx(t *testing.T) *txs.Tx {
	t.Helper()
	unsigned := &txs.BaseTx{BaseTx: lux.BaseTx{
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
	}}
	tx, err := txs.NewSigned(unsigned, txs.Codec, nil)
	require.NoError(t, err)
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
// canonical bytes survive decode -> re-encode unchanged and that the
// BlockID is stable. This is the invariant that keeps the fleet from
// forking on block hashes.
func TestBlockRoundTripByteStability(t *testing.T) {
	for _, f := range blockDeterminismFixtures(t) {
		f := f
		t.Run(f.name, func(t *testing.T) {
			require := require.New(t)

			wire := f.blk.Bytes()
			require.NotEmpty(wire)

			parsed, err := Parse(Codec, wire)
			require.NoError(err)

			// Byte-preserving read: Parse stashes the input verbatim.
			require.Equal(wire, parsed.Bytes(), "Parse must preserve block bytes")
			require.Equal(f.blk.ID(), parsed.ID(), "BlockID must be stable across parse")

			// Decode -> re-encode reproduces byte-identical wire (canonical).
			reMarshaled, err := Codec.Marshal(CodecVersion, &parsed)
			require.NoError(err)
			require.Equal(wire, reMarshaled,
				"decode->re-encode is not byte-stable; block encoding is non-canonical")
		})
	}
}

// TestGoldenAbortBlock pins the exact wire bytes and BlockID of a fixed
// AbortBlock. Any drift in block-codec layout (version prefix, type-id
// width/position, field order/endianness) breaks this — which is the
// point: these bytes are the chain commitment.
//
// Layout, interface-marshaled at CodecVersion:
//
//	01 00                     version 1, uint16 LE
//	2d 00 00 00               interface type-id 45 (AbortBlock), uint32 LE
//	08 07 06 05 04 03 02 01   Time uint64 LE (0x0102030405060708)
//	00 01 .. 1f               PrntID ids.ID, 32 bytes verbatim
//	88 77 66 55 44 33 22 11   Hght uint64 LE (0x1122334455667788)
func TestGoldenAbortBlock(t *testing.T) {
	require := require.New(t)

	var parent ids.ID
	for i := range parent {
		parent[i] = byte(i)
	}
	blk, err := NewAbortBlock(time.Unix(0x0102030405060708, 0), parent, 0x1122334455667788)
	require.NoError(err)

	golden := []byte{
		0x01, 0x00,
		0x2d, 0x00, 0x00, 0x00,
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11,
	}
	require.Equal(golden, blk.Bytes(), "AbortBlock golden wire bytes drifted")
	require.Equal(ids.ID(hash.ComputeHash256Array(golden)), blk.ID(),
		"BlockID = hash(wire) is not stable")
}

// TestBlockTrailingBytesRejected is the block-level malleability guard.
func TestBlockTrailingBytesRejected(t *testing.T) {
	require := require.New(t)

	blk, err := NewCommitBlock(time.Unix(1_700_000_000, 0), ids.ID{0x01}, 7)
	require.NoError(err)

	malleable := append(append([]byte{}, blk.Bytes()...), 0x00)
	_, err = Parse(Codec, malleable)
	require.ErrorIs(err, pcodecs.ErrExtraSpace,
		"trailing bytes on a block MUST be rejected")
}
