// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bridgevm

import (
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
)

var _ vms.Factory = (*Factory)(nil)

// Factory creates new instances of the Bridge VM
type Factory struct{}

// New returns a new instance of the Bridge VM
func (f *Factory) New(log.Logger) (interface{}, error) {
	return &VM{}, nil
}