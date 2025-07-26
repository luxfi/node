// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	db "github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/cache"
	"github.com/luxfi/node/cache/lru"
	"github.com/luxfi/node/cache/metercacher"
	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/metric"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/utils/wrappers"
	"github.com/luxfi/node/vms/proposervm/block"
)

const blockCacheSize = 64 * units.MiB

var (
	errBlockWrongVersion = errors.New("wrong version")

	_ BlockState = (*blockState)(nil)
)

type BlockState interface {
	GetBlock(blkID ids.ID) (block.Block, error)
	PutBlock(blk block.Block) error
	DeleteBlock(blkID ids.ID) error
}

type blockState struct {
	// Caches BlockID -> Block. If the Block is nil, that means the block is not
	// in storage.
	blkCache cache.Cacher[ids.ID, *blockWrapper]

	db db.Database
}

type blockWrapper struct {
	Block  []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`

	block block.Block
}

func cachedBlockSize(_ ids.ID, bw *blockWrapper) int {
	if bw == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(bw.Block) + wrappers.IntLen + 2*constants.PointerOverhead
}

func NewBlockState(db db.Database) BlockState {
	return &blockState{
		blkCache: lru.NewSizedCache(blockCacheSize, cachedBlockSize),
		db:       db,
	}
}

func NewMeteredBlockState(db db.Database, namespace string, metrics prometheus.Registerer) (BlockState, error) {
	blkCache, err := metercacher.New[ids.ID, *blockWrapper](
		metric.AppendNamespace(namespace, "block_cache"),
		metrics,
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
			return nil, db.ErrNotFound
		}
		return blk.block, nil
	}

	blkWrapperBytes, err := s.db.Get(blkID[:])
	if err == db.ErrNotFound {
		s.blkCache.Put(blkID, nil)
		return nil, db.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	blkWrapper := blockWrapper{}
	parsedVersion, err := Codec.Unmarshal(blkWrapperBytes, &blkWrapper)
	if err != nil {
		return nil, err
	}
	if parsedVersion != CodecVersion {
		return nil, errBlockWrongVersion
	}

	// The key was in the database
	blk, err := block.ParseWithoutVerification(blkWrapper.Block)
	if err != nil {
		return nil, err
	}
	blkWrapper.block = blk

	s.blkCache.Put(blkID, &blkWrapper)
	return blk, nil
}

func (s *blockState) PutBlock(blk block.Block) error {
	blkWrapper := blockWrapper{
		Block:  blk.Bytes(),
		Status: choices.Accepted,
		block:  blk,
	}

	bytes, err := Codec.Marshal(CodecVersion, &blkWrapper)
	if err != nil {
		return err
	}

	blkID := blk.ID()
	s.blkCache.Put(blkID, &blkWrapper)
	return s.db.Put(blkID[:], bytes)
}

func (s *blockState) DeleteBlock(blkID ids.ID) error {
	s.blkCache.Evict(blkID)
	return s.db.Delete(blkID[:])
}
