// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	vmtypes "github.com/luxfi/node/v2/vms"
)

var (
	// MPCVMID is the ID of the MPCVM
	MPCVMID = ids.ID{'m', 'p', 'c', 'v', 'm'}

	_ vmtypes.Factory = &Factory{}
)

// Factory implements vms.Factory
type Factory struct{}

// New creates a new MPCVM instance
func (f *Factory) New(log.Logger) (interface{}, error) {
	return NewVM()
}

// NewVM creates a new MPCVM instance
func NewVM() (*VM, error) {
	return &VM{
		mpcvmID: MPCVMID,
	}, nil
}

func init() {
	// Register the MPCVM ID
	// TODO: fix RegisterVMID
	// if err := constants.RegisterVMID("mpcvm", MPCVMID); err != nil {
	// 	panic(fmt.Errorf("failed to register MPCVM ID: %w", err))
	// }
}