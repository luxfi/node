// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runtime

import vmruntime "github.com/luxfi/vm/vms/rpcchainvm/runtime"

const (
	EngineAddressKey        = vmruntime.EngineAddressKey
	DefaultHandshakeTimeout = vmruntime.DefaultHandshakeTimeout
	DefaultGracefulTimeout  = vmruntime.DefaultGracefulTimeout
)

var (
	ErrProtocolVersionMismatch = vmruntime.ErrProtocolVersionMismatch
	ErrHandshakeFailed         = vmruntime.ErrHandshakeFailed
	ErrInvalidConfig           = vmruntime.ErrInvalidConfig
	ErrProcessNotFound         = vmruntime.ErrProcessNotFound
)

type Initializer = vmruntime.Initializer
type Stopper = vmruntime.Stopper
type Tracker = vmruntime.Tracker
type Manager = vmruntime.Manager

func NewManager() Manager {
	return vmruntime.NewManager()
}
