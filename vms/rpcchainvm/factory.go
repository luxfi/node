// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"
	"os"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/rpcchainvm/runtime"
	"github.com/luxfi/node/vms/rpcchainvm/runtime/subprocess"
	rpcchainvmzap "github.com/luxfi/node/vms/rpcchainvm/zap"
	"github.com/luxfi/resource"
)

var _ vms.Factory = (*factory)(nil)

type factory struct {
	path            string
	processTracker  resource.ProcessTracker
	runtimeTracker  runtime.Tracker
	metricsGatherer metric.MultiGatherer
	transportConfig TransportConfig
}

// NewFactory creates a new factory with default transport configuration (ZAP)
func NewFactory(
	path string,
	processTracker resource.ProcessTracker,
	runtimeTracker runtime.Tracker,
	metricsGatherer metric.MultiGatherer,
) vms.Factory {
	return NewFactoryWithTransport(
		path,
		processTracker,
		runtimeTracker,
		metricsGatherer,
		DefaultTransportConfig(),
	)
}

// NewFactoryWithTransport creates a new factory with specified transport configuration
func NewFactoryWithTransport(
	path string,
	processTracker resource.ProcessTracker,
	runtimeTracker runtime.Tracker,
	metricsGatherer metric.MultiGatherer,
	transportConfig TransportConfig,
) vms.Factory {
	return &factory{
		path:            path,
		processTracker:  processTracker,
		runtimeTracker:  runtimeTracker,
		metricsGatherer: metricsGatherer,
		transportConfig: transportConfig,
	}
}

func (f *factory) New(logger log.Logger) (interface{}, error) {
	logger.Info("creating VM client",
		"path", f.path,
		"transport", f.transportConfig.Transport,
	)

	// Use gRPC transport if explicitly configured (requires grpc build tag)
	if f.transportConfig.Transport.UsesGRPC() {
		return f.newGRPC(logger)
	}

	// Default to ZAP
	return f.newZAP(logger)
}

// newZAP creates a VM client using ZAP transport
func (f *factory) newZAP(logger log.Logger) (interface{}, error) {
	config := &subprocess.Config{
		Stderr:           os.Stderr,
		Stdout:           os.Stdout,
		HandshakeTimeout: runtime.DefaultHandshakeTimeout,
		Log:              logger,
	}

	// Create TCP listener for ZAP handshake
	listener, err := rpcchainvmzap.NewListener()
	if err != nil {
		return nil, fmt.Errorf("failed to create ZAP listener: %w", err)
	}

	// Tell subprocess to use ZAP transport.
	// IMPORTANT: Start with os.Environ() so the subprocess inherits PATH, HOME, etc.
	// Setting cmd.Env to a non-nil value prevents exec.Cmd from inheriting the parent env.
	cmd := subprocess.NewCmd(f.path)
	cmd.Env = append(os.Environ(), "LUX_VM_TRANSPORT=zap")

	status, stopper, err := subprocess.Bootstrap(
		context.TODO(),
		listener,
		cmd,
		config,
	)
	if err != nil {
		return nil, err
	}

	// Connect via ZAP with no read timeout (WaitForEvent blocks indefinitely)
	zapCfg := zapwire.DefaultConfig()
	zapCfg.ReadTimeout = 0

	ctx, cancel := context.WithTimeout(context.Background(), runtime.DefaultHandshakeTimeout)
	defer cancel()

	zapConn, err := rpcchainvmzap.Dial(ctx, status.Addr, zapCfg)
	if err != nil {
		stopper.Stop(context.Background())
		logger.Error("failed to dial VM ZAP service", "error", err)
		return nil, err
	}

	f.processTracker.TrackProcess(status.Pid)
	f.runtimeTracker.TrackRuntime(stopper)

	logger.Info("VM client connected via ZAP",
		"addr", status.Addr,
		"pid", status.Pid,
	)

	return rpcchainvmzap.NewClient(zapConn, logger), nil
}
