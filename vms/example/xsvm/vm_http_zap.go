//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"net/http"
)

func (vm *VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	// ZAP mode: no connect/gRPC handlers
	return http.NewServeMux(), nil
}
