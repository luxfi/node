// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"slices"

	"github.com/go-json-experiment/json"
	jsonv1 "github.com/go-json-experiment/json/v1"

	"github.com/luxfi/ids"

	avajson "github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/vms/components/gas"
	api "github.com/luxfi/node/vms/platformvm/api"
	validators "github.com/luxfi/validators"
)

// The shapes the P-Chain's replies carry, written so they can also cross the
// ZAP plane.
//
// A JSON object keyed by an id and a plane field are different animals. The
// object's key set is data; a field is an offset and a width. So a map has no
// layout and cannot cross, and neither can an `any`. Every type here is the
// same answer said differently: the list the map always was, with the key it
// was keyed by living inside each entry, or the sum the `any` always was, with
// one arm per shape the wire carries. The JSON is untouched — each of these is
// spelled back as the object or the element it has always been.
//
// Entries are ordered by the id they were keyed by, so one answer encodes to
// one sequence of bytes. A map has no order to inherit and a list has to have
// one.
//
// Everything that writes an object here writes it under
// jsonv1.DefaultOptionsV1, because v1 is the spelling mainnet answers in: v1
// orders map entries and writes a nil []byte as null, v2 does neither, and both
// differences are visible in what mainnet answers. Reading does NOT pass the option — under it a map key whose
// type has UnmarshalText is handed to UnmarshalJSON unquoted instead and every
// id-keyed object fails to decode.

// Amount is a quantity of one asset.
type Amount struct {
	AssetID ids.ID         `json:"assetID"`
	Value   avajson.Uint64 `json:"value"`
}

// Amounts is a quantity per asset. The wire spells it as an object keyed by
// asset id, which is what MarshalJSON writes and UnmarshalJSON reads; the list
// is what has a layout.
type Amounts []Amount

// newAmounts is the crossing point between the accumulator a balance walk wants
// — a map, keyed by asset, added into — and the value a reply carries. The map
// stays inside the walk.
func newAmounts(m map[ids.ID]uint64) Amounts {
	a := make(Amounts, 0, len(m))
	for assetID, amount := range m {
		a = append(a, Amount{AssetID: assetID, Value: avajson.Uint64(amount)})
	}
	slices.SortFunc(a, func(x, y Amount) int { return x.AssetID.Compare(y.AssetID) })
	return a
}

func (a Amounts) MarshalJSON() ([]byte, error) {
	if a == nil {
		return []byte(avajson.Null), nil
	}
	m := make(map[ids.ID]avajson.Uint64, len(a))
	for _, amount := range a {
		m[amount.AssetID] = amount.Value
	}
	return json.Marshal(m, jsonv1.DefaultOptionsV1())
}

func (a *Amounts) UnmarshalJSON(b []byte) error {
	var m map[ids.ID]avajson.Uint64
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if m == nil {
		*a = nil
		return nil
	}
	out := make(Amounts, 0, len(m))
	for assetID, value := range m {
		out = append(out, Amount{AssetID: assetID, Value: value})
	}
	slices.SortFunc(out, func(x, y Amount) int { return x.AssetID.Compare(y.AssetID) })
	*a = out
	return nil
}

// Validator is one entry of the set getValidatorsAt answers with: the node, the
// BLS public key it signs with when it has one, and its weight. That is all
// that reply has ever carried — the read behind it knows more about each
// validator and the wire has never said it, so neither does this.
//
// The node id is the object's KEY on the wire, not one of its fields, which is
// what `json:"-"` says here; ValidatorSet supplies it.
type Validator struct {
	NodeID    ids.NodeID     `json:"-"`
	PublicKey []byte         `json:"publicKey"`
	Weight    avajson.Uint64 `json:"weight"`
}

// ValidatorSet is a validator set read at a height. The wire spells it as an
// object keyed by node id; GetValidatorsAtReply writes and reads that spelling.
type ValidatorSet []Validator

func newValidatorSet(m map[ids.NodeID]*validators.GetValidatorOutput) ValidatorSet {
	s := make(ValidatorSet, 0, len(m))
	for nodeID, vdr := range m {
		s = append(s, Validator{
			NodeID:    nodeID,
			PublicKey: vdr.PublicKey,
			Weight:    avajson.Uint64(vdr.Weight),
		})
	}
	slices.SortFunc(s, func(x, y Validator) int { return x.NodeID.Compare(y.NodeID) })
	return s
}

// ChainValidatorSet is one chain's validator set, in the full shape
// getAllValidatorsAt answers with. That method reports the state read verbatim,
// under the field names Go gave it, and this keeps that exactly.
type ChainValidatorSet struct {
	ChainID    ids.ID
	Validators []*validators.GetValidatorOutput
}

func newChainValidatorSet(chainID ids.ID, m map[ids.NodeID]*validators.GetValidatorOutput) ChainValidatorSet {
	s := ChainValidatorSet{ChainID: chainID, Validators: make([]*validators.GetValidatorOutput, 0, len(m))}
	for _, vdr := range m {
		s.Validators = append(s.Validators, vdr)
	}
	slices.SortFunc(s.Validators, func(x, y *validators.GetValidatorOutput) int {
		return x.NodeID.Compare(y.NodeID)
	})
	return s
}

// CurrentValidator is one entry of the list getCurrentValidators answers with.
//
// That list is heterogeneous on the wire because what a validator IS depends on
// the chain asked about: the primary network and a permissionless net answer
// with a permissionless validator, a permissioned net with a bare staker, and
// an L1 with an L1 validator. Those are three different objects — a staker has
// a txID and an endTime that an L1 validator does not, and a permissionless
// validator always carries a delegationFee that neither of the others does — so
// folding them into one struct would add keys to two of the three answers.
//
// It is therefore a sum: exactly one arm is set, and it marshals as itself.
//
// The list also changes width with the request — asking about specific nodes
// adds `delegators` to each entry — but that is one omitempty pointer being set
// rather than nil. It is data, not a second shape, and the layout is the same
// either way.
type CurrentValidator struct {
	Permissionless *api.PermissionlessValidator
	Permissioned   *api.Staker
	L1             *api.APIL1Validator
}

func (v CurrentValidator) MarshalJSON() ([]byte, error) {
	switch {
	case v.Permissionless != nil:
		return json.Marshal(v.Permissionless, jsonv1.DefaultOptionsV1())
	case v.Permissioned != nil:
		return json.Marshal(v.Permissioned, jsonv1.DefaultOptionsV1())
	case v.L1 != nil:
		return json.Marshal(v.L1, jsonv1.DefaultOptionsV1())
	default:
		return []byte(avajson.Null), nil
	}
}

// UnmarshalJSON picks the arm from what the object carries. delegationFee is on
// every permissionless validator and on neither of the others; txID is on every
// staker and not on an L1 validator. Two keys tell the three apart.
func (v *CurrentValidator) UnmarshalJSON(b []byte) error {
	var probe struct {
		DelegationFee *avajson.Float32 `json:"delegationFee"`
		TxID          *ids.ID          `json:"txID"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	switch {
	case probe.DelegationFee != nil:
		v.Permissionless = &api.PermissionlessValidator{}
		return json.Unmarshal(b, v.Permissionless)
	case probe.TxID != nil:
		v.Permissioned = &api.Staker{}
		return json.Unmarshal(b, v.Permissioned)
	default:
		v.L1 = &api.APIL1Validator{}
		return json.Unmarshal(b, v.L1)
	}
}

// staker is the one arm read as a staker, whichever arm is set. Every entry has
// a node and a weight; the client's view is built from that.
func (v CurrentValidator) staker() api.PermissionlessValidator {
	switch {
	case v.Permissionless != nil:
		return *v.Permissionless
	case v.Permissioned != nil:
		return api.PermissionlessValidator{Staker: *v.Permissioned}
	case v.L1 != nil:
		return api.PermissionlessValidator{
			Staker: api.Staker{
				StartTime: v.L1.StartTime,
				Weight:    v.L1.Weight,
				NodeID:    v.L1.NodeID,
			},
			BaseL1Validator: v.L1.BaseL1Validator,
		}
	default:
		return api.PermissionlessValidator{}
	}
}

// GetFeeConfigReply is the chain's dynamic-fee configuration.
//
// It restates gas.Config rather than answering with it because gas.Dimensions
// is a fixed array and an array of anything but bytes has no wire form on the
// plane. The same four weights cross as a list, in the same order, and the JSON
// is the array it has always been.
type GetFeeConfigReply struct {
	Weights                  []uint64  `json:"weights"`
	MaxCapacity              gas.Gas   `json:"maxCapacity"`
	MaxPerSecond             gas.Gas   `json:"maxPerSecond"`
	TargetPerSecond          gas.Gas   `json:"targetPerSecond"`
	MinPrice                 gas.Price `json:"minPrice"`
	ExcessConversionConstant gas.Gas   `json:"excessConversionConstant"`
}

func newFeeConfig(c gas.Config) GetFeeConfigReply {
	return GetFeeConfigReply{
		Weights:                  c.Weights[:],
		MaxCapacity:              c.MaxCapacity,
		MaxPerSecond:             c.MaxPerSecond,
		TargetPerSecond:          c.TargetPerSecond,
		MinPrice:                 c.MinPrice,
		ExcessConversionConstant: c.ExcessConversionConstant,
	}
}
