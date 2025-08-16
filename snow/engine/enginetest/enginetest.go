// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/choices"
)

// MockEngine is a mock implementation of the Engine interface
type MockEngine struct {
	StartF             func(context.Context, uint32) error
	StopF              func(context.Context) error
	AcceptF            func(context.Context, ids.ID) error
	RejectF            func(context.Context, ids.ID) error
	PreferenceF        func() ids.ID
	IsBootstrappedF    func() bool
	HealthCheckF       func(context.Context) (interface{}, error)
}

func (e *MockEngine) Start(ctx context.Context, requestID uint32) error {
	if e.StartF != nil {
		return e.StartF(ctx, requestID)
	}
	return nil
}

func (e *MockEngine) Stop(ctx context.Context) error {
	if e.StopF != nil {
		return e.StopF(ctx)
	}
	return nil
}

func (e *MockEngine) Accept(ctx context.Context, id ids.ID) error {
	if e.AcceptF != nil {
		return e.AcceptF(ctx, id)
	}
	return nil
}

func (e *MockEngine) Reject(ctx context.Context, id ids.ID) error {
	if e.RejectF != nil {
		return e.RejectF(ctx, id)
	}
	return nil
}

func (e *MockEngine) Preference() ids.ID {
	if e.PreferenceF != nil {
		return e.PreferenceF()
	}
	return ids.Empty
}

func (e *MockEngine) IsBootstrapped() bool {
	if e.IsBootstrappedF != nil {
		return e.IsBootstrappedF()
	}
	return true
}

func (e *MockEngine) HealthCheck(ctx context.Context) (interface{}, error) {
	if e.HealthCheckF != nil {
		return e.HealthCheckF(ctx)
	}
	return nil, nil
}

// MockVM is a mock implementation of VM operations
type MockVM struct {
	LastAcceptedF func(context.Context) (ids.ID, error)
	GetBlockF     func(context.Context, ids.ID) (interface{}, error)
	ParseBlockF   func(context.Context, []byte) (interface{}, error)
	BuildBlockF   func(context.Context) (interface{}, error)
}

func (vm *MockVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	if vm.LastAcceptedF != nil {
		return vm.LastAcceptedF(ctx)
	}
	return ids.Empty, errors.New("not implemented")
}

func (vm *MockVM) GetBlock(ctx context.Context, id ids.ID) (interface{}, error) {
	if vm.GetBlockF != nil {
		return vm.GetBlockF(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (vm *MockVM) ParseBlock(ctx context.Context, b []byte) (interface{}, error) {
	if vm.ParseBlockF != nil {
		return vm.ParseBlockF(ctx, b)
	}
	return nil, errors.New("not implemented")
}

func (vm *MockVM) BuildBlock(ctx context.Context) (interface{}, error) {
	if vm.BuildBlockF != nil {
		return vm.BuildBlockF(ctx)
	}
	return nil, errors.New("not implemented")
}
