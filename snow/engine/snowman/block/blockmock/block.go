// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockmock

import (
	"context"
	"errors"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/choices"
)

// MockBlock is a mock implementation of the Block interface
type MockBlock struct {
	ID_       ids.ID
	ParentID_ ids.ID
	Height_   uint64
	Status_   choices.Status
	Time_     time.Time
	Bytes_    []byte
	Err       error
}

func (b *MockBlock) ID() ids.ID                      { return b.ID_ }
func (b *MockBlock) Parent() ids.ID                  { return b.ParentID_ }
func (b *MockBlock) Height() uint64                  { return b.Height_ }
func (b *MockBlock) Timestamp() time.Time            { return b.Time_ }
func (b *MockBlock) Accept(context.Context) error    { b.Status_ = choices.Accepted; return b.Err }
func (b *MockBlock) Reject(context.Context) error    { b.Status_ = choices.Rejected; return b.Err }
func (b *MockBlock) Status() choices.Status          { return b.Status_ }
func (b *MockBlock) Bytes() []byte                   { return b.Bytes_ }
func (b *MockBlock) Verify(context.Context) error    { return b.Err }

// MockVM is a mock implementation of the VM interface
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
