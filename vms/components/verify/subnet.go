// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/quasar/engine/core"
)

var (
	ErrSameChainID         = errors.New("same chainID")
	ErrMismatchedSubnetIDs = errors.New("mismatched subnetIDs")
)

// SameSubnet verifies that the provided [ctx] was provided to a chain in the
// same subnet as [peerChainID], but not the same chain. If this verification
// fails, a non-nil error will be returned.
func SameSubnet(ctx context.Context, chainCtx interface{}, peerChainID ids.ID) error {
	// Handle both core.Context and quasar.Context
	var chainID ids.ID
	var subnetID ids.ID
	var validatorState interface {
		GetSubnetID(context.Context, ids.ID) (ids.ID, error)
	}

	switch c := chainCtx.(type) {
	case *core.Context:
		chainID = c.ChainID
		subnetID = c.SubnetID
		validatorState = c.ValidatorState
	case *quasar.Context:
		chainID = c.ChainID
		subnetID = c.SubnetID
		validatorState = c.ValidatorState
	default:
		return fmt.Errorf("unsupported context type: %T", chainCtx)
	}

	if peerChainID == chainID {
		return ErrSameChainID
	}

	peerSubnetID, err := validatorState.GetSubnetID(ctx, peerChainID)
	if err != nil {
		return fmt.Errorf("failed to get subnet of %q: %w", peerChainID, err)
	}
	if subnetID != peerSubnetID {
		return fmt.Errorf("%w; expected %q got %q", ErrMismatchedSubnetIDs, subnetID, peerSubnetID)
	}
	return nil
}
