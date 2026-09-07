// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"

	"github.com/luxfi/node/version"
	chain "github.com/luxfi/vm/chain"
)

// HealthCheck reports the VM's health to the consensus engine. It returns a
// chain.HealthResult (= block.HealthCheckResult) so *VM satisfies the linear
// chain.ChainVM interface used by the certificate path.
func (vm *VM) HealthCheck(context.Context) (chain.HealthResult, error) {
	return chain.HealthResult{
		Healthy: vm.onShutdownCtx == nil || vm.onShutdownCtx.Err() == nil,
		Details: map[string]string{
			"version": version.Current.String(),
		},
	}, nil
}
