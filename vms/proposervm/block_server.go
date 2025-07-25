// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"

	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/vms/proposervm/indexer"
)

var _ indexer.BlockServer = (*VM)(nil)

// Note: this is a contention heavy call that should be avoided
// for frequent/repeated indexer ops
func (vm *VM) GetFullPostForkBlock(ctx context.Context, blkID ids.ID) (linear.Block, error) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	return vm.getPostForkBlock(ctx, blkID)
}

func (vm *VM) Commit() error {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	return vm.db.Commit()
}
