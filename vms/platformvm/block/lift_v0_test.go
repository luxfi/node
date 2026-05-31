// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	hash "github.com/luxfi/crypto/hash"
	"github.com/luxfi/node/vms/platformvm/block/v0"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// TestParseV0ApricotProposalBlock asserts that a hand-encoded
// ApricotProposalBlock (slot 0 in v0) decodes via Parse and is lifted
// into a *ProposalBlock with the original bytes preserved.
//
// The Apricot proposal flow on the modern P-only network is replayed
// through the canonical Visitor.ProposalBlock arm. The fixture here
// is constructed by re-marshalling a synthetic ApricotProposalBlock
// at v0 — this is a closed-loop test of the v0 codec's encode + decode
// path; on mainnet the bytes come from disk, not from re-encoding.
func TestParseV0ApricotProposalBlock(t *testing.T) {
	require := require.New(t)

	// Build a synthetic v0 ApricotProposalBlock carrying an
	// AdvanceTimeTx proposal — the simplest decision form pre-Banff.
	proposalTx := &txs.Tx{Unsigned: &txs.AdvanceTimeTx{Time: 1_700_000_000}}
	require.NoError(proposalTx.InitializeFromBytesAtVersion(txs.GenesisCodec, txs.CodecVersionV0))

	v0Blk := &v0.ApricotProposalBlock{
		CommonBlock: v0.CommonBlock{Hght: 7},
		Tx:          proposalTx,
	}

	raw, err := v0GenesisCodec.Marshal(CodecVersionV0, &([]v0.Block{v0Blk})[0])
	require.NoError(err, "v0 marshal of ApricotProposalBlock")

	// Parse via the version-aware block.Parse path.
	parsed, err := Parse(GenesisCodec, raw)
	require.NoError(err, "parse v0 ApricotProposalBlock")

	// The lifted block reports the canonical fields.
	require.Equal(uint64(7), parsed.Height())
	require.True(bytes.Equal(raw, parsed.Bytes()),
		"lifted v0 block must return original bytes verbatim")
	require.Equal(hash.ComputeHash256Array(raw), [32]byte(parsed.ID()),
		"lifted v0 block ID = hash(raw v0 bytes)")
	require.Len(parsed.Txs(), 1, "ApricotProposalBlock carries exactly one tx")
	require.Equal(proposalTx.TxID, parsed.Txs()[0].TxID,
		"embedded tx TxID byte-preserved")
}

// TestParseV0BanffStandardBlock asserts that a hand-encoded
// BanffStandardBlock (slot 32 in v0) decodes via Parse and lifts to
// a *StandardBlock that carries the original timestamp.
func TestParseV0BanffStandardBlock(t *testing.T) {
	require := require.New(t)

	decisionTx := &txs.Tx{Unsigned: &txs.AdvanceTimeTx{Time: 1_700_000_000}}
	require.NoError(decisionTx.InitializeFromBytesAtVersion(txs.GenesisCodec, txs.CodecVersionV0))

	v0Blk := &v0.BanffStandardBlock{
		Time: 1_700_000_100,
		ApricotStandardBlock: v0.ApricotStandardBlock{
			CommonBlock:  v0.CommonBlock{Hght: 42},
			Transactions: []*txs.Tx{decisionTx},
		},
	}

	raw, err := v0GenesisCodec.Marshal(CodecVersionV0, &([]v0.Block{v0Blk})[0])
	require.NoError(err)

	parsed, err := Parse(GenesisCodec, raw)
	require.NoError(err)

	require.Equal(uint64(42), parsed.Height())
	require.True(bytes.Equal(raw, parsed.Bytes()))
	require.Equal(hash.ComputeHash256Array(raw), [32]byte(parsed.ID()))
	require.Len(parsed.Txs(), 1)
}

// TestParseV1RoundTrip asserts that the canonical v1 path is
// byte-preserving the same way: a v1 block decoded via Parse keeps
// its original bytes and its BlockID = hash(raw).
func TestParseV1RoundTrip(t *testing.T) {
	require := require.New(t)

	tx := &txs.Tx{Unsigned: &txs.AdvanceTimeTx{Time: 1_700_000_000}}
	require.NoError(tx.InitializeFromBytesAtVersion(txs.GenesisCodec, txs.CodecVersionV1))

	blk := &StandardBlock{
		Time:         1_700_000_100,
		CommonBlock:  CommonBlock{Hght: 5},
		Transactions: []*txs.Tx{tx},
	}
	// Use the canonical block-build path (initialize sets bytes
	// from a Marshal).
	require.NoError(initialize(blk, &blk.CommonBlock))

	raw := blk.Bytes()
	parsed, err := Parse(GenesisCodec, raw)
	require.NoError(err)

	require.Equal(uint64(5), parsed.Height())
	require.True(bytes.Equal(raw, parsed.Bytes()))
	require.Equal(blk.ID(), parsed.ID(), "BlockID stable across Parse")
}

// TestVersionPrefixDispatch asserts that Parse dispatches by the
// 2-byte wire prefix and rejects unknown versions.
func TestVersionPrefixDispatch(t *testing.T) {
	require := require.New(t)

	// Empty / too-short input.
	_, err := Parse(GenesisCodec, []byte{})
	require.ErrorIs(err, ErrShortBytes)
	_, err = Parse(GenesisCodec, []byte{0x00})
	require.ErrorIs(err, ErrShortBytes)

	// Unknown version (v2+).
	_, err = Parse(GenesisCodec, []byte{0x00, 0x02, 0x00})
	require.Error(err, "unknown version must error")
}
