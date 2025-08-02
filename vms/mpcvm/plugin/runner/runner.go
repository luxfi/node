// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/ulimit"
	"github.com/luxfi/node/vms/rpcchainvm"

	"github.com/luxfi/node/vms/mpcvm/plugin/mpcvm"
)

// Run starts the MPCVM VM
func Run(
	versionEnabledFunc func() (bool, error),
	createVMFunc func(mpcvm.VM) rpcchainvm.VMWithShutdownProtocol,
) {
	versionEnabled, err := versionEnabledFunc()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to check if version is enabled: %s\n", err)
		os.Exit(1)
	}
	if !versionEnabled {
		fmt.Fprintf(os.Stderr, "MPCVM version is not enabled, exiting\n")
		os.Exit(1)
	}

	if err := ulimit.Set(ulimit.DefaultFDLimit, logging.NoLog{}); err != nil {
		fmt.Fprintf(os.Stderr, "failed to set fd limit correctly, continuing\n")
	}

	vm, err := mpcvm.NewVM()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create vm: %s\n", err)
		os.Exit(1)
	}

	rpcchainvm.Serve(context.Background(), createVMFunc(vm))
}