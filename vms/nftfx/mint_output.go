// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"encoding/json"

	"github.com/luxfi/node/v2/vms/components/verify"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
)

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	verify.IsState `json:"-"`

	GroupID                  uint32 `serialize:"true" json:"groupID"`
	secp256k1fx.OutputOwners `serialize:"true"`
}

// MarshalJSON marshals Amt and the embedded OutputOwners struct
// into a JSON readable format
// If OutputOwners cannot be serialized then this will return error
func (out *MintOutput) MarshalJSON() ([]byte, error) {
	result, err := out.OutputOwners.Fields()
	if err != nil {
		return nil, err
	}

	result["groupID"] = out.GroupID
	return json.Marshal(result)
}

// InitCtx implements the verify.State interface
func (out *MintOutput) InitCtx(ctx interface{}) {
	// No initialization needed
}

// Initialize implements the verify.State interface
func (out *MintOutput) Initialize(ctx interface{}) error {
	// The MintOutput initialization is handled by the embedded OutputOwners
	// and the GroupID field is set during unmarshaling
	return nil
}
