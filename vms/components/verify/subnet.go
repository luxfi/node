// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/ids"
)

var (
	ErrSameChainID      = errors.New("same chainID")
	ErrMismatchedNetIDs = errors.New("mismatched netIDs")
)

// ChainContext provides context for chain operations
type ChainContext struct {
	ChainID        ids.ID
	SubnetID       ids.ID
	ValidatorState ValidatorState
}

// ValidatorState provides validator state lookups
type ValidatorState interface {
	GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error)
}

// SameSubnet verifies that the provided [ctx] was provided to a chain in the
// same subnet as [peerChainID], but not the same chain. If this verification
// fails, a non-nil error will be returned.
func SameSubnet(ctx context.Context, chainCtx *ChainContext, peerChainID ids.ID) error {
	if peerChainID == chainCtx.ChainID {
		return ErrSameChainID
	}

	vs := consensus.GetValidatorState(chainCtx)
	if vs == nil {
		return fmt.Errorf("no validator state found in context")
	}
	netID, err := vs.GetNetID(peerChainIDLux)
	if err != nil {
		return fmt.Errorf("failed to get net of %q: %w", peerChainID, err)
	}
	myNetID := consensus.GetNetID(chainCtx)
	if myNetID != netID {
		return fmt.Errorf("%w; expected %q got %q", ErrMismatchedNetIDs, myNetID, netID)
	}
	return nil
}
