// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcchainvm provides the RPC infrastructure for Chain VMs.
// This file provides backward-compatible type aliases to the shared implementation
// in github.com/luxfi/vm/rpc/chain.
package rpcchainvm

import (
	"github.com/luxfi/log"
	"github.com/luxfi/vm/api/metrics"
	"github.com/luxfi/vm/rpc/chain"
	"github.com/luxfi/vm/rpc/runtime"
	"github.com/luxfi/resource"
	"google.golang.org/grpc"
)

// VMClient is a type alias for backward compatibility.
// The actual implementation is in github.com/luxfi/vm/rpc/chain.Client.
type VMClient = chain.Client

// NewClient returns a VM connected to a remote VM.
// This delegates to the shared implementation in github.com/luxfi/vm/rpc/chain.
func NewClient(
	clientConn *grpc.ClientConn,
	runtime runtime.Stopper,
	pid int,
	processTracker resource.ProcessTracker,
	metricsGatherer metrics.MultiGatherer,
	logger log.Logger,
) *VMClient {
	return chain.NewClient(clientConn, runtime, pid, processTracker, metricsGatherer, logger)
}
