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
	ErrMismatchedNetIDs = errors.New("mismatched netIDs")
)

// SameNet verifies that the provided [ctx] was provided to a chain in the
// same net as [peerChainID], but not the same chain. If this verification
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
	netID, err := vs.GetNetID(peerChainID)
	if err != nil {
		return fmt.Errorf("failed to get net of %q: %w", peerChainID, err)
	}
	myNetID := consensus.GetNetID(chainCtx)
	if myNetID != netID {
		return fmt.Errorf("%w; expected %q got %q", ErrMismatchedNetIDs, myNetID, netID)
	}
	return nil
}
