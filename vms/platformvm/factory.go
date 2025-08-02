// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	log "github.com/luxfi/log"
	"github.com/luxfi/node/v2/vms"
	"github.com/luxfi/node/v2/vms/platformvm/config"
)

var _ vms.Factory = (*Factory)(nil)

// Factory can create new instances of the Platform Chain
type Factory struct {
	config.Internal
}

// New returns a new instance of the Platform Chain
func (f *Factory) New(log.Logger) (interface{}, error) {
	return &VM{Internal: f.Internal}, nil
}
