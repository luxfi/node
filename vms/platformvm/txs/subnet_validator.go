// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/constants"
)

// SubnetValidator validates a subnet on the Lux network.
type SubnetValidator struct {
	Validator `serialize:"true"`

	// ID of the subnet this validator is validating
	Subnet ids.ID `serialize:"true" json:"subnetID"`
}

// SubnetID is the ID of the subnet this validator is validating
func (v *SubnetValidator) SubnetID() ids.ID {
	return v.Subnet
}

// Verify this validator is valid
func (v *SubnetValidator) Verify() error {
	switch v.Subnet {
	case constants.PrimaryNetworkID:
		return errBadSubnetID
	default:
		return v.Validator.Verify()
	}
}
