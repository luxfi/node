// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "errors"

var (
	// ErrNilTx is returned when a transaction is nil
	ErrNilTx = errors.New("nil tx is not valid")

	// ErrInvalidAssetID is returned when an asset ID is invalid
	ErrInvalidAssetID = errors.New("invalid asset ID")

	// ErrInvalidAmount is returned when an amount is invalid
	ErrInvalidAmount = errors.New("invalid amount")

	// ErrInvalidMintAmount is returned when a mint amount is invalid
	ErrInvalidMintAmount = errors.New("invalid mint amount")

	// ErrInvalidDestChain is returned when destination chain is invalid
	ErrInvalidDestChain = errors.New("invalid destination chain")

	// ErrInvalidDestAddress is returned when destination address is invalid
	ErrInvalidDestAddress = errors.New("invalid destination address")

	// ErrInvalidFeeAssetID is returned when fee asset ID is invalid
	ErrInvalidFeeAssetID = errors.New("fee asset ID must be valid")
)