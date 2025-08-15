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
	c := consensus.GetChainContext(chainCtx)
	if c == nil {
		return fmt.Errorf("no chain context found")
	}
	if peerChainID == c.ChainID {
		return ErrSameChainID
	}

	subnetID, err := c.ValidatorState.GetSubnetID(peerChainID)
	if err != nil {
		return fmt.Errorf("failed to get subnet of %q: %w", peerChainID, err)
	}
	if c.SubnetID != subnetID {
		return fmt.Errorf("%w; expected %q got %q", ErrMismatchedSubnetIDs, c.SubnetID, subnetID)
	}
	return nil
}