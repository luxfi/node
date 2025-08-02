// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quantumvm

import (
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
)

var _ vms.Factory = (*Factory)(nil)

// Factory implements vms.Factory
type Factory struct{}

// New creates a new QuantumVM instance
func (f *Factory) New(log.Logger) (interface{}, error) {
	return &VM{}, nil
}