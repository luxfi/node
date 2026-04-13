// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package vm provides VM types for node-VM communication (ZAP implementation).
package vm

// State represents the VM state
type State int32

const (
	State_STATE_UNSPECIFIED   State = 0
	State_STATE_STATE_SYNCING State = 1
	State_STATE_BOOTSTRAPPING State = 2
	State_STATE_NORMAL_OP     State = 3
)

func (s State) String() string {
	switch s {
	case State_STATE_STATE_SYNCING:
		return "STATE_SYNCING"
	case State_STATE_BOOTSTRAPPING:
		return "BOOTSTRAPPING"
	case State_STATE_NORMAL_OP:
		return "NORMAL_OP"
	default:
		return "UNSPECIFIED"
	}
}

// Error represents VM error codes
type Error int32

const (
	Error_ERROR_UNSPECIFIED                Error = 0
	Error_ERROR_CLOSED                     Error = 1
	Error_ERROR_NOT_FOUND                  Error = 2
	Error_ERROR_HEIGHT_INDEX_INCOMPLETE    Error = 3
	Error_ERROR_STATE_SYNC_NOT_IMPLEMENTED Error = 4
)

func (e Error) String() string {
	switch e {
	case Error_ERROR_CLOSED:
		return "CLOSED"
	case Error_ERROR_NOT_FOUND:
		return "NOT_FOUND"
	case Error_ERROR_HEIGHT_INDEX_INCOMPLETE:
		return "HEIGHT_INDEX_INCOMPLETE"
	case Error_ERROR_STATE_SYNC_NOT_IMPLEMENTED:
		return "STATE_SYNC_NOT_IMPLEMENTED"
	default:
		return "UNSPECIFIED"
	}
}
