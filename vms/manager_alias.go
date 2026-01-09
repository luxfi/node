// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	vmvms "github.com/luxfi/vm/vms"
)

type Factory = vmvms.Factory
type Manager = vmvms.Manager

var ErrNotFound = vmvms.ErrNotFound

func NewManager(log log.Logger, aliaser ids.Aliaser) Manager {
	return vmvms.NewManager(log, aliaser)
}
