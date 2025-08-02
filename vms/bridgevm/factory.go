// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bridgevm

import (
	"github.com/luxfi/log"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/vms"
)

var (
	_ vms.Factory = (*Factory)(nil)

	// ID is the VM ID for the Bridge VM
	ID = constants.BridgeVMID
)

// Factory creates new instances of the Bridge VM
type Factory struct{}

// New returns a new instance of the Bridge VM
func (f *Factory) New(log.Logger) (interface{}, error) {
	return &VM{}, nil
}