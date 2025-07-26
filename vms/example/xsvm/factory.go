// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	luxlog "github.com/luxfi/log"
	"github.com/luxfi/node/vms"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct{}

func (*Factory) New(luxlog.Logger) (interface{}, error) {
	return &VM{}, nil
}
