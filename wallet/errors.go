// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wallet

import "errors"

var (
	ErrInvalidTransaction = errors.New("invalid transaction type")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrInvalidAddress     = errors.New("invalid address")
	ErrUnsupportedChain   = errors.New("unsupported chain type")
	ErrNilKeyManager      = errors.New("nil key manager")
)