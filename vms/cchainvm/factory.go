// (c) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/vms"
)

var _ vms.Factory = (*Factory)(nil)

// Factory creates new instances of the C-Chain VM
type Factory struct{}

// New creates a new C-Chain VM instance
func (f *Factory) New(log logging.Logger) (interface{}, error) {
	return &VM{}, nil
}