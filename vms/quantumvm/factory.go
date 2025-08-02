// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quantumvm

import (
	"github.com/luxfi/log"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/vms"
)

var (
	_ vms.Factory = (*Factory)(nil)

	// ID is the VM ID for the Quantum VM
	ID = constants.QuantumVMID
)

// Factory implements vms.Factory
type Factory struct{}

// New creates a new QuantumVM instance
func (f *Factory) New(log.Logger) (interface{}, error) {
	return &VM{}, nil
}