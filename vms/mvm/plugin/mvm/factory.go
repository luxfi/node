// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mvm

import (
	"fmt"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms"
)

var (
	// MVMID is the ID of the MVM
	MVMID = ids.ID{'b', 'v', 'm'}

	_ vms.Factory = &Factory{}
)

// Factory implements vms.Factory
type Factory struct{}

// New creates a new MVM instance
func (f *Factory) New(vms.Parameters) (interface{}, error) {
	return NewVM()
}

// NewVM creates a new MVM instance
func NewVM() (VM, error) {
	return &VM{
		mvmID: MVMID,
	}, nil
}

func init() {
	// Register the MVM ID
	if err := constants.RegisterVMID("mvm", MVMID); err != nil {
		panic(fmt.Errorf("failed to register MVM ID: %w", err))
	}
}