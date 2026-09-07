// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// envelope_wire_test.go — THE ENVELOPE AS BYTES.
//
// ParseBlock is where every envelope this node ever sees enters: gossip,
// catch-up, ancestors, a summary's embedded block. Whatever it hands back is
// what consensus then votes on, so the properties that matter are the ones that
// tie the bytes to the block: an envelope round-trips to itself, nothing else
// round-trips to it, and the proposer signature it carries is only good on the
// chain it was made for.
//
// The wire path is also the only surface here that a remote peer chooses the
// bytes for, so the mutation and truncation sweeps below are the point of the
// file — not the round trip.
package proposervm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	vmchain "github.com/luxfi/vm/chain"

	"github.com/luxfi/node/vms/proposervm/block"
)

// TestEnvelopeRoundTripsThroughItsBytes pins the base case both directions:
// what we build parses back to the same envelope with every committed field
// intact, and the bytes it reports are the bytes it came from — a re-encode
// would be a second, divergent authority on the block's id.
func TestEnvelopeRoundTripsThroughItsBytes(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	innerBlk := inner.mint(ids.GenerateTestID(), 11, envT0)
	parentOuter := ids.GenerateTestID()
	epoch := block.Epoch{PChainHeight: 3, Number: 4, StartTime: envT0.Unix()}
	sb := envUnsigned(t, parentOuter, envT0, envPChainHeight, epoch, innerBlk)

	parsed, err := vm.ParseBlock(ctx, sb.Bytes())
	require.NoError(err)
	pfb, ok := parsed.(*postForkBlock)
	require.True(ok, "signed-form bytes must come back as a post-fork envelope, not a bare inner block")

	require.Equal(sb.ID(), pfb.ID())
	require.Equal(parentOuter, pfb.Parent())
	require.Equal(envPChainHeight, pfb.PChainHeight())
	require.Equal(epoch, pfb.PChainEpoch())
	require.True(envT0.Equal(pfb.Timestamp()))
	require.Equal(innerBlk.Height(), pfb.Height(), "an envelope reports the height of the block it wraps")
	require.Equal(innerBlk.ID(), pfb.getInnerBlk().ID())
	require.Equal(innerBlk.ID(), pfb.CanonicalID())
	require.Equal(sb.Bytes(), pfb.Bytes(), "the bytes are authoritative and must never be re-encoded")

	// An option round-trips to an option, not to a signed envelope: the kind is
	// carried in the bytes, so the two can never be confused for one another.
	opt, err := block.BuildOption(sb.ID(), innerBlk.Bytes())
	require.NoError(err)
	parsedOpt, err := vm.ParseBlock(ctx, opt.Bytes())
	require.NoError(err)
	pfo, ok := parsedOpt.(*postForkOption)
	require.True(ok)
	require.Equal(opt.ID(), pfo.ID())
	require.Equal(sb.ID(), pfo.Parent())

	// Bytes the inner VM knows and the envelope parser does not are a pre-fork
	// block — the fallback that makes the fork boundary work at all.
	pre, err := vm.ParseBlock(ctx, innerBlk.Bytes())
	require.NoError(err)
	require.IsType(&preForkBlock{}, pre)
	require.Equal(innerBlk.ID(), pre.ID())

	// And bytes nobody claims are refused, rather than becoming an empty block.
	_, err = vm.ParseBlock(ctx, []byte("not a block"))
	require.Error(err)
	_, err = vm.ParseBlock(ctx, nil)
	require.Error(err)
}

// TestNoTruncatedEnvelopeParsesAsTheWholeOne sweeps every proper prefix of a
// real envelope. A framing that accepts a short read is how a peer gets to
// choose which fields a block appears to carry: a prefix that decoded would
// present the same id with a different tail. Nothing shorter than the whole
// thing may come back as this block, and nothing may panic on the way to
// saying so.
func TestNoTruncatedEnvelopeParsesAsTheWholeOne(t *testing.T) {
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	innerBlk := inner.mint(ids.GenerateTestID(), 11, envT0)
	sb := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, innerBlk)
	full := sb.Bytes()

	for cut := 0; cut < len(full); cut++ {
		blk, err := vm.ParseBlock(ctx, full[:cut])
		if err != nil {
			continue
		}
		if blk.ID() == sb.ID() {
			t.Fatalf("a %d-byte prefix of a %d-byte envelope parsed back as the whole block",
				cut, len(full))
		}
	}
}

// TestAnEnvelopeIdCommitsToItsBytes is the integrity sweep, and it FAILS.
//
// THE PROPERTY. The envelope id is the hash of its unsigned prefix and the
// proposer signs a header binding that id, so every byte on the wire ought to be
// under one of the two: change any of them and the block must stop being this
// block. Everything downstream reads the map as a bijection — the block store is
// keyed by id and holds Bytes(), GetAncestors re-serves Bytes(), and a fleet is
// assumed to agree byte-for-byte on a block they agree the id of.
//
// WHAT ACTUALLY HAPPENS. For a SIGNED envelope the map is many-to-one. A peer
// may hand this node bytes that are not the proposer's bytes and have them
// accepted as the proposer's block:
//
//   - two bytes inside the signature frame can be flipped, and
//   - ARBITRARY trailing bytes can be appended, without bound.
//
// Both come back from ParseBlock with the true id, the true proposer, and a
// valid signature — because the signed form is unsigned_prefix ‖ sig_message,
// both self-delimiting, and nothing checks that the buffer was fully consumed.
// (An UNSIGNED envelope is accidentally immune: the trailing bytes land where
// the parser tries to read a signature message and fail to decode.)
//
// WHY IT MATTERS, in the order it bites. The block this node then holds reports
// Bytes() = the peer's bytes. acceptPostForkBlock persists exactly those under
// the true id, and GetAncestors serves them onward. So a peer picks the on-disk
// and re-gossiped encoding of somebody else's legitimate block, inflates it to
// the transport's message limit, and every node that took its copy from that
// peer disagrees byte-for-byte with the rest of the fleet about a block they all
// agree the id of. The last subtest measures the inflation so the failure output
// names the cost.
//
// The sweep doubles as proof the parser survives arbitrary peer bytes without
// panicking, which is the other failure mode a length-prefixed framing invites.
func TestAnEnvelopeIdCommitsToItsBytes(t *testing.T) {
	ctx := context.Background()
	inner := newEnvInner()
	vm, _ := newEnvBatchedVM(t, inner)

	innerBlk := inner.mint(ids.GenerateTestID(), 11, envT0)
	cert, signer, _ := envCert(t)
	sb, err := block.Build(
		ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{},
		cert, innerBlk.Bytes(), vm.rt.ChainID, signer,
	)
	require.NoError(t, err)
	original := sb.Bytes()
	require.NotEmpty(t, original)

	t.Run("no_single_bit_change_survives", func(t *testing.T) {
		var malleable []int
		corrupt := make([]byte, len(original))
		for i := range original {
			copy(corrupt, original)
			corrupt[i] ^= 0x01

			blk, err := vm.ParseBlock(ctx, corrupt)
			if err != nil {
				continue
			}
			if blk.ID() == sb.ID() {
				malleable = append(malleable, i)
				continue
			}
			t.Fatalf("byte %d: a corrupted envelope parsed as a different post-fork block %s", i, blk.ID())
		}
		if len(malleable) > 0 {
			t.Fatalf("%d byte positions of %d can be changed while the envelope keeps id %s and a valid signature: %v",
				len(malleable), len(original), sb.ID(), malleable)
		}
	})

	t.Run("no_trailing_bytes_are_absorbed", func(t *testing.T) {
		for _, pad := range []int{1, 64, 1 << 20} {
			padded := append(append([]byte{}, original...), make([]byte, pad)...)
			blk, err := vm.ParseBlock(ctx, padded)
			if err != nil {
				continue
			}
			if blk.ID() == sb.ID() {
				t.Errorf("%d bytes of trailing padding were absorbed: the envelope still reports id %s, "+
					"but Bytes() is now %d bytes instead of %d — this is what gets persisted and re-served",
					pad, blk.ID(), len(blk.Bytes()), len(original))
			}
		}
	})

	t.Run("what_we_store_and_re_serve_is_the_proposers_block", func(t *testing.T) {
		padded := append(append([]byte{}, original...), make([]byte, 1<<20)...)
		blk, err := vm.ParseBlock(ctx, padded)
		if err != nil {
			return // the padding was refused; nothing to store
		}
		pfb, ok := blk.(*postForkBlock)
		if !ok {
			return
		}
		require.NoError(t, vm.State.PutBlock(pfb.getStatelessBlk()))

		served, err := vm.GetAncestors(ctx, sb.ID(), 1, 1<<30, time.Minute)
		require.NoError(t, err)
		require.Len(t, served, 1)
		if len(served[0]) != len(original) {
			t.Fatalf("this node now serves %d bytes for block %s; the proposer's block is %d bytes (%.0fx)",
				len(served[0]), sb.ID(), len(original), float64(len(served[0]))/float64(len(original)))
		}
	})
}

// TestAProposerSignatureIsOnlyGoodOnItsOwnChain pins the chain binding and the
// one place it is deliberately not enforced.
//
// The signed header commits to the chainID, so an envelope lifted off one chain
// cannot be replayed onto another — without that, a proposer's signature from a
// test net would verify on mainnet at the same height. ParseBlock enforces it
// because those bytes came from a peer. ParseLocalBlock skips verification by
// design: it reads blocks this node already committed, and re-verifying every
// signature on every boot buys nothing. The distinction is only safe while it
// stays exactly that way round, so both halves are asserted here.
func TestAProposerSignatureIsOnlyGoodOnItsOwnChain(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	innerBlk := inner.mint(ids.GenerateTestID(), 11, envT0)
	cert, signer, _ := envCert(t)

	foreign, err := block.Build(
		ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{},
		cert, innerBlk.Bytes(), ids.GenerateTestID(), signer,
	)
	require.NoError(err)

	_, err = vm.ParseBlock(ctx, foreign.Bytes())
	require.Error(err, "an envelope signed for another chain must not enter through the wire path")

	local, err := vm.ParseLocalBlock(ctx, foreign.Bytes())
	require.NoError(err, "the local path reads already-committed bytes and does not re-verify")
	require.Equal(foreign.ID(), local.ID())

	// Signed for THIS chain: both paths take it, and they agree on the block.
	own, err := block.Build(
		ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{},
		cert, innerBlk.Bytes(), vm.rt.ChainID, signer,
	)
	require.NoError(err)
	fromWire, err := vm.ParseBlock(ctx, own.Bytes())
	require.NoError(err)
	fromDisk, err := vm.ParseLocalBlock(ctx, own.Bytes())
	require.NoError(err)
	require.Equal(fromWire.ID(), fromDisk.ID())
	require.Equal(own.ID(), fromWire.ID())
}

// TestParsingAnUnreadableInnerBlockLeavesNothingBehind pins the failure atom. An
// envelope whose inner bytes the inner VM refuses is not half a block: the parse
// fails, and no association between that envelope id and any inner block is
// recorded. A cache entry written before the inner parse succeeded would let a
// later lookup of the same id hand back an inner block that was never accepted.
func TestParsingAnUnreadableInnerBlockLeavesNothingBehind(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm := newEnvVM(t, inner)

	// An envelope over inner bytes the inner VM has never seen.
	sb := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{},
		&blocktestBytes{b: []byte("inner-nobody-knows")})

	_, err := vm.ParseBlock(ctx, sb.Bytes())
	require.Error(err)

	_, cached := vm.innerBlkCache.Get(sb.ID())
	require.False(cached, "a failed parse must not leave an inner block bound to the envelope id")
}

// blocktestBytes is just a byte carrier: the envelope builders take an inner
// block only to read its bytes.
type blocktestBytes struct {
	vmchain.Block
	b []byte
}

func (b *blocktestBytes) Bytes() []byte { return b.b }

// envBatchedInner is an inner VM that also answers the batched calls, which is
// how the proposervm decides at construction whether it can serve them at all.
type envBatchedInner struct {
	*envInner
	vmchain.ChainVM

	// short, when set, makes BatchedParseBlock return fewer blocks than it was
	// asked about — the shape a truncated response from an out-of-process inner
	// VM has.
	short bool
}

func newEnvBatchedInner(e *envInner) *envBatchedInner {
	return &envBatchedInner{envInner: e, ChainVM: e.vm()}
}

func (b *envBatchedInner) GetAncestors(_ context.Context, blkID ids.ID, maxBlocksNum, _ int, _ time.Duration) ([][]byte, error) {
	res := make([][]byte, 0)
	for len(res) < maxBlocksNum {
		blk, ok := b.byID[blkID]
		if !ok {
			break
		}
		res = append(res, blk.Bytes())
		blkID = blk.Parent()
	}
	if len(res) == 0 {
		return nil, errors.New("inner: no ancestors")
	}
	return res, nil
}

func (b *envBatchedInner) BatchedParseBlock(_ context.Context, blks [][]byte) ([]vmchain.Block, error) {
	out := make([]vmchain.Block, 0, len(blks))
	for _, raw := range blks {
		blk, ok := b.byBytes[string(raw)]
		if !ok {
			return nil, errors.New("inner: unknown bytes in batch")
		}
		out = append(out, blk)
	}
	if b.short && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out, nil
}

// newEnvBatchedVM is newEnvVM over an inner VM that serves the batched calls,
// so New's probe binds them.
func newEnvBatchedVM(t *testing.T, inner *envInner) (*VM, *envBatchedInner) {
	t.Helper()
	batched := newEnvBatchedInner(inner)
	vm := newEnvVM(t, inner)
	vm.ChainVM = batched
	vm.batchedVM = batched
	return vm, batched
}

// TestBatchedParseReturnsOneBlockPerInputInOrder pins the arity and the
// ordering, which is the whole contract of a batched parse: the caller matches
// results to requests by index, so a batch that drops, reorders or merges an
// entry hands consensus a block under another block's name.
//
// The mixed batch is the interesting one — envelopes, an option and bare
// pre-fork bytes in the same call — because the implementation splits the batch
// at the first entry that is not an envelope and rejoins the two halves by
// index arithmetic.
func TestBatchedParseReturnsOneBlockPerInputInOrder(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm, _ := newEnvBatchedVM(t, inner)

	innerA := inner.mint(ids.Empty, 10, envT0)
	innerB := inner.mint(innerA.ID(), 11, envT0.Add(time.Second))
	innerC := inner.mint(innerB.ID(), 12, envT0.Add(2*time.Second))

	envA := envUnsigned(t, ids.GenerateTestID(), envT0, envPChainHeight, block.Epoch{}, innerA)
	envB := envUnsigned(t, envA.ID(), envT0.Add(time.Second), envPChainHeight, block.Epoch{}, innerB)
	optC, err := block.BuildOption(envB.ID(), innerC.Bytes())
	require.NoError(err)

	batch := [][]byte{envA.Bytes(), envB.Bytes(), optC.Bytes(), innerC.Bytes()}
	got, err := vm.BatchedParseBlock(ctx, batch)
	require.NoError(err)
	require.Len(got, len(batch), "one result per input, always")

	require.IsType(&postForkBlock{}, got[0])
	require.Equal(envA.ID(), got[0].ID())
	require.IsType(&postForkBlock{}, got[1])
	require.Equal(envB.ID(), got[1].ID())
	require.IsType(&postForkOption{}, got[2])
	require.Equal(optC.ID(), got[2].ID())
	require.IsType(&preForkBlock{}, got[3], "bytes the envelope parser cannot read fall through to the inner VM")
	require.Equal(innerC.ID(), got[3].ID())

	// A batch is not a place to smuggle a block past verification: a block
	// already held as verified is returned as that same object, not re-wrapped.
	held := &postForkBlock{
		SignedBlock:              envA,
		postForkCommonComponents: postForkCommonComponents{vm: vm, innerBlk: innerA},
	}
	vm.recordVerifiedBlock(held)
	got, err = vm.BatchedParseBlock(ctx, [][]byte{envA.Bytes(), envB.Bytes()})
	require.NoError(err)
	require.Same(held, got[0], "the verified copy is the authority while it is held")
	require.Equal(envB.ID(), got[1].ID())
	vm.forgetVerifiedBlock(held.ID())

	// Degenerate inputs.
	empty, err := vm.BatchedParseBlock(ctx, nil)
	require.NoError(err)
	require.Empty(empty)

	// And a node whose inner VM does not serve batches says so rather than
	// silently answering for it.
	plain := newEnvVM(t, inner)
	_, err = plain.BatchedParseBlock(ctx, batch)
	require.ErrorIs(err, vmchain.ErrRemoteVMNotImplemented)
	_, err = plain.GetAncestors(ctx, envA.ID(), 10, 1<<20, time.Second)
	require.ErrorIs(err, vmchain.ErrRemoteVMNotImplemented)
}

// TestGetAncestorsWalksTheEnvelopesThenTheInnerChain pins the shape of an
// ancestors response: envelopes newest-first from the requested block down,
// stopping at the fork, where the remainder of the caller's budget is handed to
// the inner VM. Getting the hand-off wrong is how a bootstrapping peer receives
// a chain with a hole in it and believes it is contiguous.
func TestGetAncestorsWalksTheEnvelopesThenTheInnerChain(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	inner := newEnvInner()
	vm, _ := newEnvBatchedVM(t, inner)

	// Three inner blocks; the lower one is pre-fork (no envelope), the upper two
	// are wrapped and stored.
	preFork := inner.mint(ids.Empty, 9, envT0)
	innerA := inner.mint(preFork.ID(), 10, envT0.Add(time.Second))
	innerB := inner.mint(innerA.ID(), 11, envT0.Add(2*time.Second))

	envA := envUnsigned(t, preFork.ID(), envT0.Add(time.Second), envPChainHeight, block.Epoch{}, innerA)
	envB := envUnsigned(t, envA.ID(), envT0.Add(2*time.Second), envPChainHeight, block.Epoch{}, innerB)
	require.NoError(vm.State.PutBlock(envA))
	require.NoError(vm.State.PutBlock(envB))

	t.Run("crosses_the_fork_in_one_response", func(t *testing.T) {
		got, err := vm.GetAncestors(ctx, envB.ID(), 10, 1<<20, time.Minute)
		require.NoError(err)
		require.Equal([][]byte{envB.Bytes(), envA.Bytes(), preFork.Bytes()}, got,
			"envelopes down to the fork, then the inner VM's own bytes — in one contiguous walk")
	})

	t.Run("stops_at_the_requested_count", func(t *testing.T) {
		got, err := vm.GetAncestors(ctx, envB.ID(), 1, 1<<20, time.Minute)
		require.NoError(err)
		require.Equal([][]byte{envB.Bytes()}, got)
	})

	t.Run("stops_at_the_size_budget_but_never_returns_nothing", func(t *testing.T) {
		// A budget smaller than a single envelope still yields the first block —
		// a peer that asked for a block must get one, or it can never make
		// progress — and nothing beyond it.
		got, err := vm.GetAncestors(ctx, envB.ID(), 10, 1, time.Minute)
		require.NoError(err)
		require.Equal([][]byte{envB.Bytes()}, got)
	})

	t.Run("unknown_block_is_the_inner_vms_answer", func(t *testing.T) {
		_, err := vm.GetAncestors(ctx, ids.GenerateTestID(), 10, 1<<20, time.Minute)
		require.Error(err, "an id neither layer holds must not come back as an empty ancestry")
	})
}
