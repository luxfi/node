// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

import (
	"context"
	"errors"
	"fmt"

	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/ids"
)

var (
	ErrSameChainID      = errors.New("same chainID")
	ErrMismatchedNetIDs = errors.New("mismatched netIDs")
)

// ChainContext provides context for chain operations
type ChainContext struct {
	ChainID        ids.ID
	NetID       ids.ID
	ValidatorState ValidatorState
}

// ValidatorState provides validator state lookups
type ValidatorState interface {
	GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error)
}

// ConsensusValidatorState wraps the consensus context ValidatorState interface
type ConsensusValidatorState interface {
	GetNetID(chainID ids.ID) (ids.ID, error)
}

// SameNet verifies that the provided [ctx] was provided to a chain in the
// same subnet as [peerChainID], but not the same chain. If this verification
// fails, a non-nil error will be returned.
func SameNet(ctx context.Context, chainCtx *ChainContext, peerChainID ids.ID) error {
	if peerChainID == chainCtx.ChainID {
		return ErrSameChainID
	}

	peerNetID, err := chainCtx.ValidatorState.GetNetID(ctx, peerChainID)
	if err != nil {
		return fmt.Errorf("failed to get subnet of %q: %w", peerChainID, err)
	}
	if chainCtx.NetID != peerNetID {
		return fmt.Errorf("%w; expected %q got %q", ErrMismatchedNetIDs, chainCtx.NetID, peerNetID)
	}
	return nil
}

// SameSubnet verifies that the peerChainID is in the same subnet as the chain
// represented by consensusCtx, but not the same chain. This is a convenience
// wrapper for coreth compatibility that accepts *consensusctx.Context directly.
func SameSubnet(ctx context.Context, consensusCtx *consensusctx.Context, peerChainID ids.ID) error {
	if peerChainID == consensusCtx.ChainID {
		return ErrSameChainID
	}

	// Get the validator state from consensus context
	vs, ok := consensusCtx.ValidatorState.(consensusctx.ValidatorState)
	if !ok {
		return fmt.Errorf("validator state does not implement required interface")
	}

	peerNetID, err := vs.GetSubnetID(peerChainID)
	if err != nil {
		return fmt.Errorf("failed to get subnet of %q: %w", peerChainID, err)
	}
	if consensusCtx.NetID != peerNetID {
		return fmt.Errorf("%w; expected %q got %q", ErrMismatchedNetIDs, consensusCtx.NetID, peerNetID)
	}
	return nil
}
