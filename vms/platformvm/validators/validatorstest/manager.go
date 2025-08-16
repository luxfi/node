// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validatorstest

import (
	"context"

	"github.com/luxfi/ids"

	snowvalidators "github.com/luxfi/consensus/validators"
	"github.com/luxfi/consensus/validator"
	vmvalidators "github.com/luxfi/node/vms/platformvm/validators"
)

var Manager vmvalidators.Manager = manager{}

type manager struct{}

func (manager) GetMinimumHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (manager) GetCurrentHeight() (uint64, error) {
	return 0, nil
}

func (manager) GetSubnetID(context.Context, ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (manager) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	return nil, nil
}

func (manager) OnAcceptedBlockID(ids.ID) {}

func (manager) GetCurrentValidatorSet(context.Context, ids.ID) (map[ids.ID]*validator.GetCurrentValidatorOutput, uint64, error) {
	return nil, 0, nil
}
