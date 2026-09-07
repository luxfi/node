// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"sync"

	"github.com/luxfi/consensus/core/choices"
	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/cache/metercacher"
	"github.com/luxfi/node/vms/proposervm/block"
)

const blockCacheSize = 64 * constants.MiB

// statusIntLen is the wire width of the serialized status uint32; used only for
// cache-size accounting.
const statusIntLen = 4

var _ BlockState = (*blockState)(nil)

type BlockState interface {
	GetBlock(blkID ids.ID) (block.Block, error)
	PutBlock(blk block.Block) error
	DeleteBlock(blkID ids.ID) error
}

type blockState struct {
	// Caches BlockID -> Block. If the Block is nil, that means the block is not
	// in storage.
	blkCache cache.Cacher[ids.ID, *blockWrapper]

	// Serialises cache MUTATION only. Never held across database IO, so reads
	// stay concurrent; it exists so that "is this key already cached?" and the
	// write that follows cannot be split by another goroutine.
	//
	// Without it the read path poisons itself. GetBlock caches a miss, so:
	//
	//   A: db.Get(X) -> ErrNotFound          (X not written yet)
	//   B: PutBlock(X): cache.Put(X, blk); db.Put(X)   -- X is now durable
	//   A: cache.Put(X, nil)                 -- overwrites B with a tombstone
	//
	// and every later GetBlock(X) answers ErrNotFound from that tombstone while
	// X sits on disk. That is the "preferred block IS last-accepted and is
	// unfetchable" wedge in proposervm BuildBlock: the builder cannot fetch its
	// own committed tip, goes mute, and only an operator restart clears it —
	// because a restart drops the cache and the next read comes off disk. The
	// tombstone lives as long as the process, so this presents as a solid stream
	// of build failures rather than an intermittent one.
	mu sync.Mutex
	db database.Database
}

type blockWrapper struct {
	Block     []byte
	StatusInt uint32 // Store status as uint32 for serialization

	block  block.Block
	status choices.Status // Keep the actual status here
}

func cachedBlockSize(_ ids.ID, bw *blockWrapper) int {
	if bw == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(bw.Block) + statusIntLen + 2*constants.PointerOverhead
}

func NewBlockState(db database.Database) BlockState {
	return &blockState{
		blkCache: lru.NewSizedCache(blockCacheSize, cachedBlockSize),
		db:       db,
	}
}

func NewMeteredBlockState(db database.Database, namespace string, metrics metric.Registerer) (BlockState, error) {
	registry, ok := metrics.(metric.Registry)
	if !ok {
		return nil, errors.New("metrics must be a Registry")
	}
	blkCache, err := metercacher.New[ids.ID, *blockWrapper](
		namespace,
		registry,
		lru.NewSizedCache(blockCacheSize, cachedBlockSize),
	)

	return &blockState{
		blkCache: blkCache,
		db:       db,
	}, err
}

func (s *blockState) GetBlock(blkID ids.ID) (block.Block, error) {
	if blk, found := s.blkCache.Get(blkID); found {
		if blk == nil {
			return nil, database.ErrNotFound
		}
		return blk.block, nil
	}

	blkWrapperBytes, err := s.db.Get(blkID[:])
	if err == database.ErrNotFound {
		// Cache the miss, unless a writer landed while the read above was in
		// flight — in which case serve what it wrote rather than a stale miss.
		if w := s.cacheFill(blkID, nil); w != nil {
			return w.block, nil
		}
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	blkWrapper := blockWrapper{}
	if err := parseBlockWrapper(blkWrapperBytes, &blkWrapper); err != nil {
		return nil, err
	}

	// The key was in the database
	blk, err := block.ParseWithoutVerification(blkWrapper.Block)
	if err != nil {
		return nil, err
	}
	blkWrapper.block = blk
	blkWrapper.status = choices.Status(blkWrapper.StatusInt) // Convert back from uint32

	if w := s.cacheFill(blkID, &blkWrapper); w != nil {
		return w.block, nil
	}
	return blk, nil
}

// cacheFill installs [bw] for [blkID] on behalf of a database read that has
// already completed, and returns the entry that is authoritative afterwards
// (nil when that entry says "not in storage").
//
// The rule is one line: a fill derived from a read NEVER overwrites an entry a
// writer installed. PutBlock and DeleteBlock publish to the cache under [mu]
// before touching the database, so an entry present here is always newer than
// the read that produced [bw] — keeping it is what stops the read path from
// clobbering a durable block with a tombstone.
func (s *blockState) cacheFill(blkID ids.ID, bw *blockWrapper) *blockWrapper {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, found := s.blkCache.Get(blkID); found {
		return existing
	}
	s.blkCache.Put(blkID, bw)
	return bw
}

func (s *blockState) PutBlock(blk block.Block) error {
	blkWrapper := blockWrapper{
		Block:  blk.Bytes(),
		status: choices.Accepted,
		block:  blk,
	}

	bytes := marshalBlockWrapper(&blkWrapper)

	blkID := blk.ID()
	s.mu.Lock()
	s.blkCache.Put(blkID, &blkWrapper)
	s.mu.Unlock()
	return s.db.Put(blkID[:], bytes)
}

func (s *blockState) DeleteBlock(blkID ids.ID) error {
	// A tombstone, not an eviction. Evicting leaves the key absent, so a read
	// that fetched the block from the database just before this delete would
	// find nothing cached and install its now-stale bytes — resurrecting a
	// deleted block. nil already means "not in storage" (see blkCache above),
	// so the tombstone is the same statement the read path makes on a miss.
	s.mu.Lock()
	s.blkCache.Put(blkID, nil)
	s.mu.Unlock()
	return s.db.Delete(blkID[:])
}
