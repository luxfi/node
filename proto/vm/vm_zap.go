// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import "github.com/luxfi/proto/node/zap/vm"

type (
	State = vm.State
	Error = vm.Error
)

const (
	State_STATE_UNSPECIFIED   = vm.State_STATE_UNSPECIFIED
	State_STATE_STATE_SYNCING = vm.State_STATE_STATE_SYNCING
	State_STATE_BOOTSTRAPPING = vm.State_STATE_BOOTSTRAPPING
	State_STATE_NORMAL_OP     = vm.State_STATE_NORMAL_OP

	Error_ERROR_UNSPECIFIED                = vm.Error_ERROR_UNSPECIFIED
	Error_ERROR_CLOSED                     = vm.Error_ERROR_CLOSED
	Error_ERROR_NOT_FOUND                  = vm.Error_ERROR_NOT_FOUND
	Error_ERROR_HEIGHT_INDEX_INCOMPLETE    = vm.Error_ERROR_HEIGHT_INDEX_INCOMPLETE
	Error_ERROR_STATE_SYNC_NOT_IMPLEMENTED = vm.Error_ERROR_STATE_SYNC_NOT_IMPLEMENTED
)
