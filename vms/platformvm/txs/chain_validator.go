// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
)

// ChainValidator is the legacy per-chain validator descriptor used by
// AddChainValidatorTx. The Chain field is the network ID this validator
// registers under (legacy: subnet ID).
//
// Deprecated: Use Validator with AddValidatorTx. Under LP-018
// sovereign-L1, validators validate networks — not chains. Chains live
// on networks (created via CreateChainTx); validators no longer
// register per-chain. Retained for one release cycle for wire/codec
// compat with pre-LP-018 binaries.
type ChainValidator struct {
	Validator `serialize:"true"`

	// ID of the chain this validator is validating
	Chain ids.ID `serialize:"true" json:"chainID"`
}

// ChainID is the ID of the chain this validator is validating
func (v *ChainValidator) ChainID() ids.ID {
	return v.Chain
}

// Verify this validator is valid
func (v *ChainValidator) Verify() error {
	switch v.Chain {
	case constants.PrimaryNetworkID:
		return errBadChainID
	default:
		return v.Validator.Verify()
	}
}
