//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"
	"os"

	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/rpcchainvm/runtime"
	"github.com/luxfi/node/vms/rpcchainvm/runtime/subprocess"
	"github.com/luxfi/vm/rpc/grpcutils"
)

// newGRPC creates a VM client using gRPC transport
func (f *factory) newGRPC(logger log.Logger) (interface{}, error) {
	config := &subprocess.Config{
		Stderr:           os.Stderr,
		Stdout:           os.Stdout,
		HandshakeTimeout: runtime.DefaultHandshakeTimeout,
		Log:              logger,
	}

	listener, err := grpcutils.NewListener()
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC listener: %w", err)
	}

	status, stopper, err := subprocess.Bootstrap(
		context.TODO(),
		listener,
		subprocess.NewCmd(f.path),
		config,
	)
	if err != nil {
		return nil, err
	}

	clientConn, err := grpcutils.Dial(status.Addr)
	if err != nil {
		logger.Error("failed to dial VM gRPC service", "error", err)
		return nil, err
	}

	f.processTracker.TrackProcess(status.Pid)
	f.runtimeTracker.TrackRuntime(stopper)
	return NewClient(clientConn, stopper, status.Pid, f.processTracker, f.metricsGatherer, logger), nil
}
