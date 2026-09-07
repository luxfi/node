// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	_ "embed"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/proto/node/zap/schema"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/zap"
)

//go:embed block.zap
var schemaSrc []byte

// The block layout is stated twice: as constants in blockwire.go and as a
// schema in block.zap. This file is what keeps the two honest — first that
// they name the same offsets, then that a reader holding only the schema
// reads a block this node built.

func loadSchema(t *testing.T) *schema.File {
	t.Helper()
	f, err := schema.Parse(schemaSrc)
	require.NoError(t, err)
	return f
}

// TestSchemaMatchesWire: every offset and every object size in block.zap is
// the one blockwire.go writes at.
func TestSchemaMatchesWire(t *testing.T) {
	require := require.New(t)
	file := loadSchema(t)

	for _, c := range []struct {
		strct, field string
		off          int
	}{
		{"DecidedBlock", "Kind", offBlkKind},
		{"DecidedBlock", "ParentID", offBlkParent},
		{"DecidedBlock", "Height", offBlkHeight},
		{"DecidedBlock", "Time", offBlkTime},
		{"StandardBlock", "TxLengths", offBlkTxLengths},
		{"StandardBlock", "TxBlob", offBlkTxBlob},
		{"ProposalBlock", "ProposalTx", offBlkProposalTx},
	} {
		s, ok := file.Struct(c.strct)
		require.True(ok, "block.zap declares no %s", c.strct)
		f, ok := s.Field(c.field)
		require.True(ok, "%s declares no %s", c.strct, c.field)
		require.Equal(c.off, f.Off, "%s.%s offset", c.strct, c.field)
	}

	for _, c := range []struct {
		strct string
		size  int
	}{
		{"DecidedBlock", sizeDecided},
		{"StandardBlock", sizeStandard},
		{"ProposalBlock", sizeProposal},
	} {
		s, _ := file.Struct(c.strct)
		require.Equal(c.size, s.Size(), "%s object size", c.strct)
	}

	// An id is thirty-two bytes inline here, not a length-prefixed blob.
	s, _ := file.Struct("DecidedBlock")
	f, _ := s.Field("ParentID")
	require.Equal(schema.Kind("bytes_fixed"), f.Kind)
	require.Equal(32, f.Fixed)
}

// TestSchemaReadsABlock: a reader holding the schema and nothing else gets
// the same values out of a block's bytes that the block's own accessors do.
func TestSchemaReadsABlock(t *testing.T) {
	require := require.New(t)
	file := loadSchema(t)

	var (
		parent = ids.GenerateTestID()
		ts     = time.Unix(1_700_000_000, 0)
		tx     = makeTx(t, 1)
	)
	abort, err := NewAbortBlock(ts, parent, 41)
	require.NoError(err)
	standard, err := NewStandardBlock(ts, parent, 42, []*txs.Tx{tx})
	require.NoError(err)
	proposal, err := NewProposalBlock(ts, parent, 43, tx, nil)
	require.NoError(err)

	for _, c := range []struct {
		strct string
		blk   Block
	}{
		{"DecidedBlock", abort},
		{"StandardBlock", standard},
		{"ProposalBlock", proposal},
	} {
		s, ok := file.Struct(c.strct)
		require.True(ok)

		msg, err := zap.Parse(c.blk.Bytes())
		require.NoError(err)
		obj := msg.Root()

		read := func(name string) schema.Field {
			f, ok := s.Field(name)
			require.True(ok, "%s.%s", c.strct, name)
			return f
		}
		require.NotZero(obj.Uint8(read("Kind").Off), "%s: kind byte", c.strct)
		require.Equal(parent[:], obj.BytesFixedSlice(read("ParentID").Off, 32), "%s: ParentID", c.strct)
		require.Equal(c.blk.Height(), obj.Uint64(read("Height").Off), "%s: Height", c.strct)
		require.Equal(uint64(ts.Unix()), obj.Uint64(read("Time").Off), "%s: Time", c.strct)
	}

	// The tx slots too: split the blob by the length list, both found
	// through the schema, and the txs come back whole.
	s, _ := file.Struct("StandardBlock")
	msg, err := zap.Parse(standard.Bytes())
	require.NoError(err)
	obj := msg.Root()
	lenOff, _ := s.Field("TxLengths")
	blobOff, _ := s.Field("TxBlob")
	lengths := obj.ListStride(lenOff.Off, 4)
	require.Equal(1, lengths.Len())
	blob := obj.Bytes(blobOff.Off)
	require.Equal(int(lengths.Uint32(0)), len(blob))
	require.Equal(tx.Bytes(), blob)

	s, _ = file.Struct("ProposalBlock")
	msg, err = zap.Parse(proposal.Bytes())
	require.NoError(err)
	ptxOff, _ := s.Field("ProposalTx")
	require.Equal(tx.Bytes(), msg.Root().Bytes(ptxOff.Off))
}
