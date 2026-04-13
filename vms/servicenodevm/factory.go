// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
)

var _ vms.Factory = (*Factory)(nil)

// VMID is the unique identifier for ServiceNodeVM (S-Chain)
var VMID = ids.ID{'s', 'e', 'r', 'v', 'i', 'c', 'e', 'n', 'o', 'd', 'e', 'v', 'm'}

// Factory creates new ServiceNodeVM instances
type Factory struct{}

// New returns a new instance of the ServiceNodeVM
func (f *Factory) New(logger log.Logger) (interface{}, error) {
	return &VM{}, nil
}
