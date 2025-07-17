// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/validators"
)

var TestManager Manager = testManager{}

type testManager struct{}

func (testManager) GetMinimumHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (testManager) GetCurrentHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (testManager) GetSubnetID(context.Context, ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (testManager) GetValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return nil, nil
}

func (testManager) OnAcceptedBlockID(ids.ID) {}
