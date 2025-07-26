// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	luxlog "github.com/luxfi/log"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/xvm/config"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct {
	config.Config
}

func (f *Factory) New(luxlog.Logger) (interface{}, error) {
	return &VM{Config: f.Config}, nil
}
