// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/vm/chain"
)

func (vm *VM) HealthCheck(context.Context) (chain.HealthResult, error) {
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
		return chain.HealthResult{}, fmt.Errorf("couldn't get current local validator: %w", err)
	}

	for chainID := range vm.TrackedChains {
		localChainValidator, err := vm.state.GetCurrentValidator(
			chainID,
			vm.nodeID,
		)
		switch err {
		case nil:
			vm.metrics.SetTimeUntilNetUnstake(chainID, time.Until(localChainValidator.EndTime))
		case database.ErrNotFound:
			vm.metrics.SetTimeUntilNetUnstake(chainID, 0)
		default:
			return chain.HealthResult{}, fmt.Errorf("couldn't get current network validator on %q: %w", chainID, err)
		}
	}
	return chain.HealthResult{Healthy: true, Details: nil}, nil
}
