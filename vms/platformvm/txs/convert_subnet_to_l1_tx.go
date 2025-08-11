// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
)

// ConvertSubnetToL1Validator represents a validator for subnet-to-L1 conversion
// This is a placeholder implementation for LP99 support
type ConvertSubnetToL1Validator struct {
	// Validator's node ID
	NodeID ids.NodeID `serialize:"true" json:"nodeID"`
	
	// Validator's weight
	Weight uint64 `serialize:"true" json:"weight"`
	
	// Validator's BLS public key
	BLSPublicKey *bls.PublicKey `serialize:"true" json:"blsPublicKey"`
	
	// Initial balance for the validator
	Balance uint64 `serialize:"true" json:"balance"`
	
	// P-Chain address that will receive rewards  
	RewardAddress ids.ShortID `serialize:"true" json:"rewardAddress"`
	
	// P-Chain address that will receive delegation fees
	DelegationRewardAddress ids.ShortID `serialize:"true" json:"delegationRewardAddress"`
	
	// Percentage of delegation rewards given to delegators (out of 1,000,000)
	DelegationShares uint32 `serialize:"true" json:"delegationShares"`
}