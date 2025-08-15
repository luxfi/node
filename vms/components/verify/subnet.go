// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/consensus"
	"github.com/luxfi/ids"
)

var (
	ErrSameChainID         = errors.New("same chainID")
	ErrMismatchedSubnetIDs = errors.New("mismatched subnetIDs")
)

// SameSubnet verifies that the provided [ctx] was provided to a chain in the
// same subnet as [peerChainID], but not the same chain. If this verification
// fails, a non-nil error will be returned.
func SameSubnet(ctx context.Context, chainCtx context.Context, peerChainID ids.ID) error {
	chainID := consensus.GetChainID(chainCtx)
	if chainID == ids.Empty {
		return fmt.Errorf("no chain ID found in context")
	}
	if peerChainID == chainID {
		return ErrSameChainID
	}

	vs := consensus.GetValidatorState(chainCtx)
	if vs == nil {
		return fmt.Errorf("no validator state found in context")
	}
	subnetID, err := vs.GetSubnetID(ctx, peerChainID)
	if err != nil {
		return fmt.Errorf("failed to get subnet of %q: %w", peerChainID, err)
	}
	mySubnetID := consensus.GetSubnetID(chainCtx)
	if mySubnetID != subnetID {
		return fmt.Errorf("%w; expected %q got %q", ErrMismatchedSubnetIDs, mySubnetID, subnetID)
	}
	return nil
}