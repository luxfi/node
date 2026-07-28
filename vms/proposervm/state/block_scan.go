// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/database/versiondb"

	"github.com/luxfi/node/vms/proposervm/block"
)

// ScanBlocks visits every OUTER proposervm block persisted in the block store of
// [db], in key order.
//
// It is a REPAIR-PATH reader, deliberately kept OUT of the State interface: the
// hot path addresses blocks by id, and only the boot-time index rebuild
// (proposervm.rebuildOuterIndexFromStore) needs to enumerate them. Keeping it a
// free function over the same [blockStatePrefix] the store writes under means the
// prefix stays defined exactly once, and no mock/interface churn is imposed on
// callers that will never scan.
//
// Records that fail to decode are SKIPPED, not fatal: a rebuild must extract every
// usable wrapper from a store that may be partially damaged — that is the whole
// point of running it. Only an iterator-level error aborts the scan.
func ScanBlocks(db *versiondb.Database, visit func(block.Block) error) error {
	blockDB := prefixdb.New(blockStatePrefix, db)
	it := blockDB.NewIterator()
	defer it.Release()

	for it.Next() {
		wrapper := blockWrapper{}
		if err := parseBlockWrapper(it.Value(), &wrapper); err != nil {
			continue
		}
		blk, err := block.ParseWithoutVerification(wrapper.Block)
		if err != nil {
			continue
		}
		if err := visit(blk); err != nil {
			return err
		}
	}
	return it.Error()
}
