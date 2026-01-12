// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcchainvm provides the RPC infrastructure for Chain VMs (linear blockchains).
// This package is a thin wrapper that re-exports the shared implementation from
// github.com/luxfi/vm/rpc/chain for backward compatibility.
package rpcchainvm

import (
	"context"

	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/log"
	"github.com/luxfi/vm/rpc/chain"
	"github.com/luxfi/vm/rpc/grpcutils"
)

// Serve starts the RPC Chain VM server and performs a handshake with the VM runtime service.
// The address of the Runtime server is expected to be passed via ENV `runtime.EngineAddressKey`.
// This address is used by the Runtime client to send Initialize RPC to server.
//
// This function delegates to the shared implementation in github.com/luxfi/vm/rpc/chain.
func Serve(ctx context.Context, log log.Logger, vm block.ChainVM, opts ...grpcutils.ServerOption) error {
	return chain.Serve(ctx, log, vm, opts...)
}
