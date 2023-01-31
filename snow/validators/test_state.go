// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errMinimumHeight   = errors.New("unexpectedly called GetMinimumHeight")
	errCurrentHeight   = errors.New("unexpectedly called GetCurrentHeight")
	errSubnetID        = errors.New("unexpectedly called GetSubnetID")
	errGetValidatorSet = errors.New("unexpectedly called GetValidatorSet")
)

var _ State = (*TestState)(nil)

type TestState struct {
	T *testing.T

	CantGetMinimumHeight,
	CantGetCurrentHeight,
	CantGetSubnetID,
	CantGetValidatorSet bool

<<<<<<< HEAD
<<<<<<< HEAD
	GetMinimumHeightF func(ctx context.Context) (uint64, error)
	GetCurrentHeightF func(ctx context.Context) (uint64, error)
	GetSubnetIDF      func(ctx context.Context, chainID ids.ID) (ids.ID, error)
	GetValidatorSetF  func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*GetValidatorOutput, error)
=======
	GetMinimumHeightF func(context.Context) (uint64, error)
	GetCurrentHeightF func(context.Context) (uint64, error)
	GetValidatorSetF  func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error)
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
=======
	GetMinimumHeightF func(ctx context.Context) (uint64, error)
	GetCurrentHeightF func(ctx context.Context) (uint64, error)
	GetValidatorSetF  func(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*Validator, error)
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
}

func (vm *TestState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	if vm.GetMinimumHeightF != nil {
		return vm.GetMinimumHeightF(ctx)
	}
	if vm.CantGetMinimumHeight && vm.T != nil {
		vm.T.Fatal(errMinimumHeight)
	}
	return 0, errMinimumHeight
}

func (vm *TestState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	if vm.GetCurrentHeightF != nil {
		return vm.GetCurrentHeightF(ctx)
	}
	if vm.CantGetCurrentHeight && vm.T != nil {
		vm.T.Fatal(errCurrentHeight)
	}
	return 0, errCurrentHeight
}

<<<<<<< HEAD
<<<<<<< HEAD
func (vm *TestState) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	if vm.GetSubnetIDF != nil {
		return vm.GetSubnetIDF(ctx, chainID)
	}
	if vm.CantGetSubnetID && vm.T != nil {
		vm.T.Fatal(errSubnetID)
	}
	return ids.Empty, errSubnetID
}

=======
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
func (vm *TestState) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
<<<<<<< HEAD
) (map[ids.NodeID]*GetValidatorOutput, error) {
=======
func (vm *TestState) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
>>>>>>> f94b52cf8 ( Pass message context through the validators.State interface (#2242))
=======
) (map[ids.NodeID]*Validator, error) {
>>>>>>> 117ff9a78 (Add BLS keys to `GetValidatorSet` (#2111))
	if vm.GetValidatorSetF != nil {
		return vm.GetValidatorSetF(ctx, height, subnetID)
	}
	if vm.CantGetValidatorSet && vm.T != nil {
		vm.T.Fatal(errGetValidatorSet)
	}
	return nil, errGetValidatorSet
}
