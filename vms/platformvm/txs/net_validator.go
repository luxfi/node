// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
)

// NetValidator validates a net on the Lux network.
type NetValidator struct {
	Validator `serialize:"true"`

	// ID of the net this validator is validating
	Net ids.ID `serialize:"true" json:"netID"`
}

// NetID is the ID of the net this validator is validating
func (v *NetValidator) NetID() ids.ID {
	return v.Net
}

// Verify this validator is valid
func (v *NetValidator) Verify() error {
	switch v.Net {
	case constants.PrimaryNetworkID:
		return errBadNetID
	default:
		return v.Validator.Verify()
	}
}
