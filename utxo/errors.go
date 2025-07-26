// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import "errors"

var (
	ErrUTXONotFound      = errors.New("UTXO not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrInvalidChainType  = errors.New("invalid chain type")
	ErrInvalidAddress    = errors.New("invalid address format")
)
