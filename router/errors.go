// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import "errors"

var (
	// ErrChainAlreadyRegistered is returned when trying to register a chain that's already registered
	ErrChainAlreadyRegistered = errors.New("chain already registered")

	// ErrChainNotRegistered is returned when trying to unregister a chain that's not registered
	ErrChainNotRegistered = errors.New("chain not registered")

	// ErrNoChainID is returned when a message doesn't contain a chain ID
	ErrNoChainID = errors.New("message does not contain chain ID")

	// ErrNilHandler is returned when trying to register a nil handler
	ErrNilHandler = errors.New("handler cannot be nil")

	// ErrRouterNotInitialized is returned when router is used before initialization
	ErrRouterNotInitialized = errors.New("router not initialized")
)
