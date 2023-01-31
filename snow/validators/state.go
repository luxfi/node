// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
)

var _ State = (*lockedState)(nil)

// State allows the lookup of validator sets on specified subnets at the
// requested P-chain height.
type State interface {
	// GetMinimumHeight returns the minimum height of the block still in the
	// proposal window.
	GetMinimumHeight(context.Context) (uint64, error)
	// GetCurrentHeight returns the current height of the P-chain.
	GetCurrentHeight(context.Context) (uint64, error)

<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 85ab999a4 (Improve subnetID lookup to support non-whitelisted subnets (#2354))
	// GetSubnetID returns the subnetID of the provided chain.
	GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error)

	// GetValidatorSet returns the validators of the provided subnet at the
	// requested P-chain height.
	// The returned map should not be modified.
<<<<<<< HEAD
=======
	// GetValidatorSet returns the validators of the provided subnet at the
	// requested P-chain height.
	// The returned map should not be modified.
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
	GetValidatorSet(
		ctx context.Context,
		height uint64,
		subnetID ids.ID,
<<<<<<< HEAD
<<<<<<< HEAD
	) (map[ids.NodeID]*GetValidatorOutput, error)
=======
	GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error)
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
=======
	) (map[ids.NodeID]*Validator, error)
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
=======
	) (map[ids.NodeID]*GetValidatorOutput, error)
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
}

type lockedState struct {
	lock sync.Locker
	s    State
}

func NewLockedState(lock sync.Locker, s State) State {
	return &lockedState{
		lock: lock,
		s:    s,
	}
}

func (s *lockedState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetMinimumHeight(ctx)
}

func (s *lockedState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetCurrentHeight(ctx)
}

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 85ab999a4 (Improve subnetID lookup to support non-whitelisted subnets (#2354))
func (s *lockedState) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.s.GetSubnetID(ctx, chainID)
}

<<<<<<< HEAD
=======
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
=======
>>>>>>> 85ab999a4 (Improve subnetID lookup to support non-whitelisted subnets (#2354))
func (s *lockedState) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
) (map[ids.NodeID]*GetValidatorOutput, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

=======
func (s *lockedState) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
=======
) (map[ids.NodeID]*Validator, error) {
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
	s.lock.Lock()
	defer s.lock.Unlock()

>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
	return s.s.GetValidatorSet(ctx, height, subnetID)
}

type noValidators struct {
	State
}

func NewNoValidatorsState(state State) State {
	return &noValidators{
		State: state,
	}
}

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
func (*noValidators) GetValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]*GetValidatorOutput, error) {
=======
func (*noValidators) GetValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]uint64, error) {
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
=======
func (*noValidators) GetValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]*Validator, error) {
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
=======
func (*noValidators) GetValidatorSet(context.Context, uint64, ids.ID) (map[ids.NodeID]*GetValidatorOutput, error) {
>>>>>>> 62b728221 (Add txID to `validators.Set#Add` (#2312))
	return nil, nil
}
