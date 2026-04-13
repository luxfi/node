//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"net/http"

	"connectrpc.com/grpcreflect"
	"github.com/luxfi/log"

	"github.com/luxfi/node/connectproto/pb/xsvm/xsvmconnect"
	"github.com/luxfi/node/vms/example/xsvm/api"
)

func (vm *VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	mux := http.NewServeMux()

	reflectionPattern, reflectionHandler := grpcreflect.NewHandlerV1(
		grpcreflect.NewStaticReflector(xsvmconnect.PingName),
	)
	mux.Handle(reflectionPattern, reflectionHandler)

	pingService := &api.PingService{Log: vm.rt.Log.(log.Logger)}
	pingPath, pingHandler := xsvmconnect.NewPingHandler(pingService)
	mux.Handle(pingPath, pingHandler)

	return mux, nil
}
