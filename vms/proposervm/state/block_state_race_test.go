// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"

	"github.com/luxfi/node/vms/proposervm/block"
)

// newTestBlock builds a real signed proposervm block, so these tests exercise
// the same parse/serialise path production uses.
func newTestBlock(require *require.Assertions) block.Block {
	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)

	b, err := block.Build(
		ids.ID{1},         // parentID
		time.Unix(123, 0), // timestamp
		2,                 // pChainHeight
		block.Epoch{},
		cert,
		[]byte{3}, // innerBlockBytes
		ids.ID{4}, // chainID
		tlsCert.PrivateKey.(crypto.Signer),
	)
	require.NoError(err)
	return b
}

// pausingDB stalls the FIRST Get of a chosen key so a writer can be interleaved
// exactly between the read path's database lookup and its cache fill. That is
// the one window the poisoning race lives in, and it is far too narrow to hit
// reliably by racing goroutines — so it is made deterministic here rather than
// left to chance.
type pausingDB struct {
	database.Database

	target  []byte
	entered chan struct{} // closed once the read is inside the stall
	release chan struct{} // closed by the test to let the read finish
	armed   bool
}

func (d *pausingDB) Get(key []byte) ([]byte, error) {
	if d.armed && string(key) == string(d.target) {
		d.armed = false
		// Answer from the database as it was BEFORE the writer ran: that is what
		// a real read in flight would have seen.
		v, err := d.Database.Get(key)
		close(d.entered)
		<-d.release
		return v, err
	}
	return d.Database.Get(key)
}

// A block that becomes durable while a read for it is in flight must stay
// readable. Before the fix the read's cached miss overwrote the writer's entry,
// so every later GetBlock answered ErrNotFound for a block sitting on disk —
// the "preferred block IS last-accepted and is unfetchable" wedge in
// proposervm BuildBlock, which only an operator restart cleared because a
// restart drops the cache and the next read comes off disk.
func TestGetBlockDoesNotCacheAMissOverAConcurrentWrite(t *testing.T) {
	require := require.New(t)

	blk := newTestBlock(require)
	blkID := blk.ID()

	pdb := &pausingDB{
		Database: memdb.New(),
		target:   blkID[:],
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		armed:    true,
	}
	s := NewBlockState(pdb)

	// Reader: misses the cache, then stalls inside the database lookup.
	readErr := make(chan error, 1)
	go func() {
		_, err := s.GetBlock(blkID)
		readErr <- err
	}()

	select {
	case <-pdb.entered:
	case <-time.After(5 * time.Second):
		require.FailNow("reader never reached the stalled database lookup")
	}

	// Writer: completes fully while the reader is stalled. After this the block
	// is durable and published to the cache.
	require.NoError(s.PutBlock(blk))

	close(pdb.release)
	<-readErr // the stalled read may report the miss it genuinely saw

	// The assertion that matters: a LATER read must find the block. Before the
	// fix the stalled reader's cached miss shadowed the writer and this returned
	// ErrNotFound forever.
	got, err := s.GetBlock(blkID)
	require.NoError(err, "block is on disk but the cache answers not-found — the read path poisoned itself")
	require.Equal(blk.Bytes(), got.Bytes())
}

// The mirror case: a read already holding the database bytes must not install
// them after a delete, resurrecting a block the writer removed.
func TestGetBlockDoesNotResurrectADeletedBlock(t *testing.T) {
	require := require.New(t)

	blk := newTestBlock(require)
	blkID := blk.ID()

	base := memdb.New()
	seed := NewBlockState(base)
	require.NoError(seed.PutBlock(blk)) // durable, so the stalled read finds bytes

	pdb := &pausingDB{
		Database: base,
		target:   blkID[:],
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
		armed:    true,
	}
	s := NewBlockState(pdb) // fresh cache: the read must go to the database

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		_, _ = s.GetBlock(blkID)
	}()

	select {
	case <-pdb.entered:
	case <-time.After(5 * time.Second):
		require.FailNow("reader never reached the stalled database lookup")
	}

	require.NoError(s.DeleteBlock(blkID))
	close(pdb.release)

	// Wait for the reader to finish its cache fill. Asserting before it lands
	// would pass whether or not the fill is guarded — the bug is what the reader
	// leaves BEHIND, so the assertion has to come after it.
	<-readDone

	_, err := s.GetBlock(blkID)
	require.ErrorIs(err, database.ErrNotFound,
		"deleted block came back — a read in flight installed its stale bytes over the delete")
}
