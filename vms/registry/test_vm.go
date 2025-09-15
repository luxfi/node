// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"context"
	"net/http"
)

// testVM is a test VM implementation for testing the registry
type testVM struct {
	createHandlersF func(context.Context) (map[string]http.Handler, error)
	shutdownF       func(context.Context) error
}

func newTestVM() *testVM {
	return &testVM{}
}

func (vm *testVM) Initialize(
	ctx context.Context,
	chainCtx interface{},
	db interface{},
	genesisBytes []byte,
	upgradeBytes interface{},
	configBytes interface{},
	msgChan interface{},
	fxs interface{},
	appSender interface{},
) error {
	return nil
}

func (vm *testVM) Shutdown(ctx context.Context) error {
	if vm.shutdownF != nil {
		return vm.shutdownF(ctx)
	}
	return nil
}

func (vm *testVM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	if vm.createHandlersF != nil {
		return vm.createHandlersF(ctx)
	}
	return nil, nil
}

func (vm *testVM) BuildBlock(context.Context) (interface{}, error) {
	return nil, nil
}

func (vm *testVM) ParseBlock(context.Context, []byte) (interface{}, error) {
	return nil, nil
}

func (vm *testVM) GetBlock(context.Context, interface{}) (interface{}, error) {
	return nil, nil
}

func (vm *testVM) SetPreference(context.Context, interface{}) error {
	return nil
}

func (vm *testVM) LastAccepted(context.Context) (interface{}, error) {
	return nil, nil
}

func (vm *testVM) VerifyHeightIndex(context.Context) error {
	return nil
}

func (vm *testVM) GetBlockIDAtHeight(context.Context, uint64) (interface{}, error) {
	return nil, nil
}