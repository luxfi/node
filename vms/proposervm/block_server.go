// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"

	chain "github.com/luxfi/vm/chain"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/proposervm/indexer"
)

var _ indexer.BlockServer = (*VM)(nil)

// Note: this is a contention heavy call that should be avoided
// for frequent/repeated indexer ops
func (vm *VM) GetFullPostForkBlock(ctx context.Context, blkID ids.ID) (chain.Block, error) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	return vm.getPostForkBlock(ctx, blkID)
}

func (vm *VM) Commit() error {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	return vm.db.Commit()
}
