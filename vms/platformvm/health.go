// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/node/utils/constants"
)

func (vm *VM) HealthCheck(context.Context) (interface{}, error) {
	localPrimaryValidator, err := vm.state.GetCurrentValidator(
		constants.PrimaryNetworkID,
		vm.nodeID,
	)
	switch err {
	case nil:
		vm.metrics.SetTimeUntilUnstake(time.Until(localPrimaryValidator.EndTime))
	case database.ErrNotFound:
		vm.metrics.SetTimeUntilUnstake(0)
	default:
		return nil, fmt.Errorf("couldn't get current local validator: %w", err)
	}

	for netID := range vm.TrackedNets {
		localNetValidator, err := vm.state.GetCurrentValidator(
			netID,
			vm.nodeID,
		)
		switch err {
		case nil:
			vm.metrics.SetTimeUntilNetUnstake(netID, time.Until(localNetValidator.EndTime))
		case database.ErrNotFound:
			vm.metrics.SetTimeUntilNetUnstake(netID, 0)
		default:
			return nil, fmt.Errorf("couldn't get current net validator of %q: %w", netID, err)
		}
	}
	return nil, nil
}
