// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subprocess

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/rpcchainvm/runtime"
)

var _ runtime.Initializer = (*initializer)(nil)

// Subprocess VM Runtime initializer.
type initializer struct {
	path string

	once sync.Once
	// Address of the RPC Chain VM server
	vmAddr string
	// Error, if one occurred, during Initialization
	err error
	// Initialized is closed once Initialize is called
	initialized chan struct{}
}

func newInitializer(path string) *initializer {
	return &initializer{
		path:        path,
		initialized: make(chan struct{}),
	}
}

func (i *initializer) Initialize(_ context.Context, protocolVersion uint, vmAddr string) error {
	// File-based debug logging
	debugFile := func(msg string) {
		if f, err := os.OpenFile("/tmp/node_initializer.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			fmt.Fprintf(f, "[node] %s\n", msg)
			f.Close()
		}
	}
	debugFile(fmt.Sprintf("Initialize called: protocolVersion=%d, vmAddr=%s, path=%s", protocolVersion, vmAddr, i.path))

	i.once.Do(func() {
		debugFile(fmt.Sprintf("once.Do executing: nodeProtocol=%d, vmProtocol=%d", version.RPCChainVMProtocol, protocolVersion))
		if version.RPCChainVMProtocol != protocolVersion {
			i.err = fmt.Errorf("%w. Lux Node version %s implements RPCChainVM protocol version %d. The VM located at %s implements RPCChainVM protocol version %d. Please make sure that there is an exact match of the protocol versions. This can be achieved by updating your VM or running an older/newer version of Lux Node. Please be advised that some virtual machines may not yet support the latest RPCChainVM protocol version",
				runtime.ErrProtocolVersionMismatch,
				version.Current,
				version.RPCChainVMProtocol,
				i.path,
				protocolVersion,
			)
			debugFile("Protocol mismatch error: " + i.err.Error())
		}
		i.vmAddr = vmAddr
		debugFile("Closing initialized channel")
		close(i.initialized)
		debugFile("Initialized channel closed")
	})
	debugFile(fmt.Sprintf("Returning from Initialize, err=%v", i.err))
	return i.err
}
