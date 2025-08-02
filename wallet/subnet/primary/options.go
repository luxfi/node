// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"github.com/luxfi/node/wallet"
)

// Re-export types and functions from the wallet package for backward compatibility
type Option = wallet.Option
type Options = wallet.Options
type IssuanceReceipt = wallet.IssuanceReceipt
type ConfirmationReceipt = wallet.ConfirmationReceipt

var (
	NewOptions              = wallet.NewOptions
	UnionOptions            = wallet.UnionOptions
	WithContext             = wallet.WithContext
	WithCustomAddresses     = wallet.WithCustomAddresses
	WithCustomEthAddresses  = wallet.WithCustomEthAddresses
	WithBaseFee             = wallet.WithBaseFee
	WithMinIssuanceTime     = wallet.WithMinIssuanceTime
	WithStakeableLocked     = wallet.WithStakeableLocked
	WithChangeOwner         = wallet.WithChangeOwner
	WithMemo                = wallet.WithMemo
	WithAssumeDecided       = wallet.WithAssumeDecided
	WithPollFrequency       = wallet.WithPollFrequency
	WithIssuanceHandler     = wallet.WithIssuanceHandler
	WithConfirmationHandler = wallet.WithConfirmationHandler
)

// Deprecated: Options struct exported for compatibility
type deprecatedOptions struct {
	// Empty struct for backward compatibility
}
