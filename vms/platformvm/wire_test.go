// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/vm/types"

	avajson "github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/vms/components/gas"
	api "github.com/luxfi/node/vms/platformvm/api"
)

// The replies that used to hold a map or an `any` now hold a list or a sum, so
// that they have a layout and can cross the plane. That is only allowed if the
// JSON is the same afterwards, which is what these prove — twice over.
//
//   - TestWireGolden reads bytes RECORDED FROM MAINNET (golden/) and requires that
//     decoding them into the reply and encoding it again gives the same bytes
//     back. It anchors the shapes to what a live node actually answers.
//   - TestWireUnchanged builds the value BOTH WAYS — once in the shape the
//     reply held before and once in the shape it holds now — and requires the
//     two encode identically. That covers the data mainnet did not happen to
//     have: a permissioned validator, an L1 validator, a non-empty balance
//     map, a nil map against an empty one.
//
// The encoder here is encoding/json because that is the one the recorded
// responses were written with.

// golden/ holds bodies recorded from api.lux.network, byte for byte as the node
// sent them. It is not called testdata because this repository does not track a
// directory by that name.
func golden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("golden", name))
	require.NoError(t, err)
	return b
}

func TestWireGolden(t *testing.T) {
	tests := []struct {
		file  string
		reply func() any
	}{
		{"getCurrentValidators.json", func() any { return &GetCurrentValidatorsReply{} }},
		{"getCurrentValidators_nodeIDs.json", func() any { return &GetCurrentValidatorsReply{} }},
		{"getValidatorsAt.json", func() any { return &GetValidatorsAtReply{} }},
		{"getAllValidatorsAt.json", func() any { return &GetAllValidatorsAtReply{} }},
		{"getBalance.json", func() any { return &GetBalanceResponse{} }},
		{"getStake.json", func() any { return &GetStakeReply{} }},
		{"getFeeConfig.json", func() any { return &GetFeeConfigReply{} }},
		{"getTimestamp.json", func() any { return &GetTimestampReply{} }},
		{"getFeeState.json", func() any { return &GetFeeStateReply{} }},
		{"getValidatorFeeState.json", func() any { return &GetValidatorFeeStateReply{} }},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			require := require.New(t)

			recorded := golden(t, test.file)
			reply := test.reply()
			require.NoError(json.Unmarshal(recorded, reply))

			again, err := json.Marshal(reply)
			require.NoError(err)
			require.JSONEq(string(recorded), string(again))
			require.Equal(string(recorded), string(again), "the bytes a live node answers with must survive the round trip")
		})
	}
}

// TestWireUnchanged marshals the same answer through the shape the reply used
// to hold and the shape it holds now, and requires one set of bytes.
func TestWireUnchanged(t *testing.T) {
	var (
		assetA = ids.ID{0xa}
		assetB = ids.ID{0xb}
		nodeA  = ids.NodeID{0x1}
		nodeB  = ids.NodeID{0x2}
		txID   = ids.ID{0xc}
		key    = []byte{0xde, 0xad, 0xbe, 0xef}
	)

	t.Run("amounts", func(t *testing.T) {
		tests := []struct {
			name string
			m    map[ids.ID]avajson.Uint64
			a    Amounts
		}{
			{"nil", nil, nil},
			{"empty", map[ids.ID]avajson.Uint64{}, Amounts{}},
			{
				"two",
				map[ids.ID]avajson.Uint64{assetA: 1, assetB: 2},
				Amounts{{AssetID: assetA, Value: 1}, {AssetID: assetB, Value: 2}},
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				require := require.New(t)
				was, err := json.Marshal(test.m)
				require.NoError(err)
				is, err := json.Marshal(test.a)
				require.NoError(err)
				require.Equal(string(was), string(is))

				var back Amounts
				require.NoError(json.Unmarshal(was, &back))
				require.Equal(test.a, back)
			})
		}
	})

	// newAmounts is what the balance walk hands the reply. The map it is given
	// is the accumulator; the order the list comes out in is the asset id's own,
	// so the same balances always encode to the same bytes.
	t.Run("amounts are ordered by asset", func(t *testing.T) {
		require := require.New(t)
		first := newAmounts(map[ids.ID]uint64{assetB: 2, assetA: 1})
		second := newAmounts(map[ids.ID]uint64{assetA: 1, assetB: 2})
		require.Equal(first, second)
		require.Equal(Amounts{{AssetID: assetA, Value: 1}, {AssetID: assetB, Value: 2}}, first)
	})

	t.Run("getValidatorsAt", func(t *testing.T) {
		require := require.New(t)

		// What the reply held before: the read's own output, keyed by node.
		was, err := json.Marshal(&legacyValidatorsAtReply{Validators: map[ids.NodeID]*validators.GetValidatorOutput{
			nodeA: {NodeID: nodeA, PublicKey: key, Weight: 7, TxID: txID, Light: 7},
			nodeB: {NodeID: nodeB, Weight: 9},
		}})
		require.NoError(err)

		is, err := json.Marshal(&GetValidatorsAtReply{Validators: ValidatorSet{
			{NodeID: nodeA, PublicKey: key, Weight: 7},
			{NodeID: nodeB, Weight: 9},
		}})
		require.NoError(err)
		require.Equal(string(was), string(is))

		// A validator with no key keeps its explicit null.
		require.Contains(string(is), `"publicKey":null`)
	})

	t.Run("getValidatorsAt nil", func(t *testing.T) {
		require := require.New(t)
		was, err := json.Marshal(&legacyValidatorsAtReply{})
		require.NoError(err)
		is, err := json.Marshal(&GetValidatorsAtReply{})
		require.NoError(err)
		require.Equal(string(was), string(is))
	})

	t.Run("getAllValidatorsAt", func(t *testing.T) {
		require := require.New(t)
		vdrA := &validators.GetValidatorOutput{NodeID: nodeA, PublicKey: key, Light: 7, Weight: 7, TxID: txID}
		vdrB := &validators.GetValidatorOutput{NodeID: nodeB, Weight: 9}

		was, err := json.Marshal(&legacyAllValidatorsAtReply{
			ValidatorSets: map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput{
				assetA: {nodeA: vdrA, nodeB: vdrB},
				assetB: {},
			},
		})
		require.NoError(err)

		is, err := json.Marshal(&GetAllValidatorsAtReply{ValidatorSets: []ChainValidatorSet{
			{ChainID: assetA, Validators: []*validators.GetValidatorOutput{vdrA, vdrB}},
			{ChainID: assetB, Validators: []*validators.GetValidatorOutput{}},
		}})
		require.NoError(err)
		require.Equal(string(was), string(is))
	})

	t.Run("getBalance", func(t *testing.T) {
		require := require.New(t)
		m := map[ids.ID]avajson.Uint64{assetA: 3}
		a := Amounts{{AssetID: assetA, Value: 3}}

		was, err := json.Marshal(&legacyBalanceResponse{
			Balance: 3, Balances: m, Unlockeds: m,
			LockedStakeables: map[ids.ID]avajson.Uint64{}, LockedNotStakeables: nil,
		})
		require.NoError(err)
		is, err := json.Marshal(&GetBalanceResponse{
			Balance: 3, Balances: a, Unlockeds: a,
			LockedStakeables: Amounts{}, LockedNotStakeables: nil,
		})
		require.NoError(err)
		require.Equal(string(was), string(is))
	})

	// getCurrentValidators answers with three different objects depending on the
	// chain asked about. Each arm has to marshal as the object it always was —
	// which is why they are three arms and not one struct: a staker has a txID
	// and an endTime an L1 validator does not, and a permissionless validator
	// carries a delegationFee neither of the others does.
	t.Run("getCurrentValidators", func(t *testing.T) {
		require := require.New(t)

		reward := avajson.Uint64(11)
		staker := api.Staker{TxID: txID, StartTime: 1, EndTime: 2, Weight: 3, NodeID: nodeA}
		permissionless := api.PermissionlessValidator{
			Staker:          staker,
			PotentialReward: &reward,
			DelegationFee:   2,
		}
		pubKey := types.JSONByteSlice(key)
		l1 := api.APIL1Validator{
			NodeID: nodeB, Weight: 5, StartTime: 6,
			BaseL1Validator: api.BaseL1Validator{ValidationID: &txID, PublicKey: &pubKey},
		}

		was, err := json.Marshal(&legacyCurrentValidatorsReply{
			Validators: []any{permissionless, staker, l1},
		})
		require.NoError(err)

		is, err := json.Marshal(&GetCurrentValidatorsReply{Validators: []CurrentValidator{
			{Permissionless: &permissionless},
			{Permissioned: &staker},
			{L1: &l1},
		}})
		require.NoError(err)
		require.Equal(string(was), string(is))

		// And the arms come back from the wire as the arms they went out as.
		var back GetCurrentValidatorsReply
		require.NoError(json.Unmarshal(is, &back))
		require.Len(back.Validators, 3)
		require.NotNil(back.Validators[0].Permissionless)
		require.NotNil(back.Validators[1].Permissioned)
		require.NotNil(back.Validators[2].L1)
		require.Equal(nodeB, back.Validators[2].L1.NodeID)
	})

	// Asking about specific nodes adds `delegators` to each entry. That is one
	// omitempty pointer being set, not a second element shape: the same type
	// answers both calls, and the plane sees the same layout either way.
	t.Run("getCurrentValidators delegators", func(t *testing.T) {
		require := require.New(t)
		delegators := []api.PrimaryDelegator{{Staker: api.Staker{NodeID: nodeA}}}
		vdr := api.PermissionlessValidator{Staker: api.Staker{NodeID: nodeA}}

		without, err := json.Marshal(CurrentValidator{Permissionless: &vdr})
		require.NoError(err)
		require.NotContains(string(without), `"delegators"`)

		vdr.Delegators = &delegators
		with, err := json.Marshal(CurrentValidator{Permissionless: &vdr})
		require.NoError(err)
		require.Contains(string(with), `"delegators"`)
	})

	t.Run("timestamps", func(t *testing.T) {
		// time.Time lays out with no slots, so a reply holding one crosses
		// carrying nothing and says nothing about it. avajson.Time is the same
		// instant as numbers — and has to render as the same bytes, in whatever
		// zone the instant was read in.
		for _, when := range []time.Time{
			time.Unix(1765573611, 0).UTC(),
			time.Unix(1765573611, 123456789).UTC(),
			time.Unix(1765573611, 0).In(time.FixedZone("east", 7200)),
			time.Unix(1765573611, 0).In(time.FixedZone("west", -18000)),
			{},
		} {
			require := require.New(t)
			was, err := json.Marshal(struct {
				T time.Time `json:"timestamp"`
			}{when})
			require.NoError(err)
			is, err := json.Marshal(struct {
				T avajson.Time `json:"timestamp"`
			}{avajson.NewTime(when)})
			require.NoError(err)
			require.Equal(string(was), string(is), "instant %s", when)

			var back struct {
				T avajson.Time `json:"timestamp"`
			}
			require.NoError(json.Unmarshal(is, &back))
			require.True(back.T.Time().Equal(when), "instant %s did not survive", when)
		}
	})

	t.Run("getFeeConfig", func(t *testing.T) {
		require := require.New(t)
		config := gas.Config{
			Weights:                  gas.Dimensions{1, 2, 3, 4},
			MaxCapacity:              5,
			MaxPerSecond:             6,
			TargetPerSecond:          7,
			MinPrice:                 8,
			ExcessConversionConstant: 9,
		}
		was, err := json.Marshal(config)
		require.NoError(err)
		is, err := json.Marshal(newFeeConfig(config))
		require.NoError(err)
		require.Equal(string(was), string(is))
	})
}

// The shapes these replies held before they were given a layout. They exist only
// so a test can encode the same answer both ways and compare.
type (
	legacyValidatorsAtReply struct {
		Validators map[ids.NodeID]*validators.GetValidatorOutput
	}
	legacyAllValidatorsAtReply struct {
		ValidatorSets map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput `json:"validatorSets"`
	}
	legacyCurrentValidatorsReply struct {
		Validators []any `json:"validators"`
	}
	legacyBalanceResponse struct {
		Balance             avajson.Uint64            `json:"balance"`
		Unlocked            avajson.Uint64            `json:"unlocked"`
		LockedStakeable     avajson.Uint64            `json:"lockedStakeable"`
		LockedNotStakeable  avajson.Uint64            `json:"lockedNotStakeable"`
		Balances            map[ids.ID]avajson.Uint64 `json:"balances"`
		Unlockeds           map[ids.ID]avajson.Uint64 `json:"unlockeds"`
		LockedStakeables    map[ids.ID]avajson.Uint64 `json:"lockedStakeables"`
		LockedNotStakeables map[ids.ID]avajson.Uint64 `json:"lockedNotStakeables"`
		UTXOIDs             []*lux.UTXOID             `json:"utxoIDs"`
	}
)

// legacyValidatorsAtReply spells itself the way GetValidatorsAtReply used to:
// an object keyed by node id whose values are a public key and a weight, with
// the same encoder the live one uses, so the comparison isolates the change to
// the field's type.
func (v *legacyValidatorsAtReply) MarshalJSON() ([]byte, error) {
	m := make(map[ids.NodeID]*jsonGetValidatorOutput, len(v.Validators))
	for _, vdr := range v.Validators {
		out := &jsonGetValidatorOutput{Weight: avajson.Uint64(vdr.Weight)}
		if vdr.PublicKey != nil {
			pk, err := formatting.Encode(formatting.HexNC, vdr.PublicKey)
			if err != nil {
				return nil, err
			}
			out.PublicKey = &pk
		}
		m[vdr.NodeID] = out
	}
	return json.Marshal(m)
}
