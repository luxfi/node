// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package photon

import (
	"context"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/set"
)

// Sampler represents photonic sampling of validators
// (Previously known as Poll in Avalanche consensus)
//
// The photon sampler emits "photons" (queries) to a sample
// of K validators and collects their "reflections" (responses).
type Sampler interface {
	// Sample returns a sample of K validators to query
	// This is like emitting photons to specific targets
	Sample(ctx context.Context, validatorSet set.Set[ids.NodeID], k int) (set.Set[ids.NodeID], error)

	// GetK returns the sample size
	GetK() int

	// Reset clears any internal state
	Reset()
}

// Response represents a validator's response to a photon query
// (Previously known as Vote in Avalanche consensus)
type Response struct {
	NodeID     ids.NodeID
	Preference ids.ID // The validator's preference
	Confidence int    // Confidence level (0-100)
}

// PhotonQuery represents a query sent to validators
// (Previously known as Poll message)
type PhotonQuery struct {
	RequestID  uint32
	ChainID    ids.ID
	Height     uint64
	Containers []ids.ID // Options being queried
}

// PhotonReflection represents a response from a validator
// (Previously known as Vote message)
type PhotonReflection struct {
	RequestID  uint32
	ChainID    ids.ID
	Height     uint64
	Container  ids.ID // The validator's choice
	Confidence int    // Confidence level
}