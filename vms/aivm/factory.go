// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aivm

import (
	"errors"

	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
)

var (
	_ vms.Factory = (*Factory)(nil)

	errWrongVMType = errors.New("wrong vm type")
)

// Factory creates new instances of the AI VM
type Factory struct{}

// New returns a new instance of the AI VM
func (f *Factory) New(log.Logger) (interface{}, error) {
	return &VM{}, nil
}