// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/platformvm/config"
)

var _ vms.Factory = (*Factory)(nil)

// Factory can create new instances of the Platform Chain
type Factory struct {
	config.Config
}

// New returns a new instance of the Platform Chain
func (f *Factory) New(log.Logger) (interface{}, error) {
	return &VM{Config: f.Config}, nil
}
