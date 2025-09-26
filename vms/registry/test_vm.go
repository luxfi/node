// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"context"
	"net/http"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/version"
)

// Ensure testVM implements common.VM
var _ common.VM = (*testVM)(nil)

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
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	return nil
}

func (vm *testVM) SetState(ctx context.Context, state snow.State) error {
	return nil
}

func (vm *testVM) Shutdown(ctx context.Context) error {
	if vm.shutdownF != nil {
		return vm.shutdownF(ctx)
	}
	return nil
}

func (vm *testVM) Version(ctx context.Context) (string, error) {
	return "test-1.0.0", nil
}

func (vm *testVM) HealthCheck(ctx context.Context) (interface{}, error) {
	return nil, nil
}

func (vm *testVM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	if vm.createHandlersF != nil {
		return vm.createHandlersF(ctx)
	}
	return nil, nil
}

func (vm *testVM) CreateStaticHandlers(ctx context.Context) (map[string]http.Handler, error) {
	return nil, nil
}

func (vm *testVM) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	return nil, nil
}

func (vm *testVM) WaitForEvent(ctx context.Context) (common.Message, error) {
	return common.PendingTxs, nil
}

func (vm *testVM) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	// No-op implementation for test VM
	return nil
}

func (vm *testVM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	// No-op implementation for test VM
	return nil
}

// AppHandler interface methods

func (vm *testVM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	// No-op implementation for test VM
	return nil
}

func (vm *testVM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	// No-op implementation for test VM
	return nil
}

func (vm *testVM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	// No-op implementation for test VM
	return nil
}

func (vm *testVM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	// No-op implementation for test VM
	return nil
}