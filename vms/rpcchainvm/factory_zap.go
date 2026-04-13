//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"errors"

	"github.com/luxfi/log"
)

var errGRPCNotAvailable = errors.New("gRPC transport requires -tags=grpc build flag")

// newGRPC is not available in ZAP-only builds
func (f *factory) newGRPC(logger log.Logger) (interface{}, error) {
	return nil, errGRPCNotAvailable
}
