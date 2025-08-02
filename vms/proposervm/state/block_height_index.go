// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	db "github.com/luxfi/database"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/cache"
	"github.com/luxfi/node/v2/cache/lru"
)

const cacheSize = 8192 // max cache entries

var (
	_ HeightIndex = (*heightIndex)(nil)

	heightPrefix   = []byte("height")
	metadataPrefix = []byte("metadata")

	forkKey = []byte("fork")
)

type HeightIndexGetter interface {
	// GetMinimumHeight return the smallest height of an indexed blockID. If
	// there are no indexed blockIDs, ErrNotFound will be returned.
	GetMinimumHeight() (uint64, error)
	GetBlockIDAtHeight(height uint64) (ids.ID, error)

	// Fork height is stored when the first post-fork block/option is accepted.
	// Before that, fork height won't be found.
	GetForkHeight() (uint64, error)
}

type HeightIndexWriter interface {
	SetForkHeight(height uint64) error
	SetBlockIDAtHeight(height uint64, blkID ids.ID) error
	DeleteBlockIDAtHeight(height uint64) error
}

// HeightIndex contains mapping of blockHeights to accepted proposer block IDs
// along with some metadata (fork height and checkpoint).
type HeightIndex interface {
	HeightIndexWriter
	HeightIndexGetter
}

type heightIndex struct {
	// Caches block height -> proposerVMBlockID.
	heightsCache cache.Cacher[uint64, ids.ID]

	heightDB   db.Database
	metadataDB db.Database
}

func NewHeightIndex(database db.Database, _ *versiondb.Database) HeightIndex {
	return &heightIndex{

		heightsCache: lru.NewCache[uint64, ids.ID](cacheSize),
		heightDB:     prefixdb.New(heightPrefix, database),
		metadataDB:   prefixdb.New(metadataPrefix, database),
	}
}

func (hi *heightIndex) GetMinimumHeight() (uint64, error) {
	it := hi.heightDB.NewIterator()
	defer it.Release()

	if !it.Next() {
		return 0, db.ErrNotFound
	}

	height, err := db.ParseUInt64(it.Key())
	if err != nil {
		return 0, err
	}
	return height, it.Error()
}

func (hi *heightIndex) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if blkID, found := hi.heightsCache.Get(height); found {
		return blkID, nil
	}

	key := db.PackUInt64(height)
	idBytes, err := hi.heightDB.Get(key)
	if err != nil {
		return ids.Empty, err
	}
	blkID, err := ids.ToID(idBytes)
	if err != nil {
		return ids.Empty, err
	}
	hi.heightsCache.Put(height, blkID)
	return blkID, err
}

func (hi *heightIndex) SetBlockIDAtHeight(height uint64, blkID ids.ID) error {
	hi.heightsCache.Put(height, blkID)
	key := db.PackUInt64(height)
	return hi.heightDB.Put(key, blkID[:])
}

func (hi *heightIndex) DeleteBlockIDAtHeight(height uint64) error {
	hi.heightsCache.Evict(height)
	key := db.PackUInt64(height)
	return hi.heightDB.Delete(key)
}

func (hi *heightIndex) GetForkHeight() (uint64, error) {
	return db.GetUInt64(hi.metadataDB, forkKey)
}

func (hi *heightIndex) SetForkHeight(height uint64) error {
	return db.PutUInt64(hi.metadataDB, forkKey, height)
}
