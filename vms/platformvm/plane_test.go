// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	avajson "github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/vms/components/gas"
	platformapi "github.com/luxfi/node/vms/platformvm/api"
)

// A message crosses the plane, or it does not — and the dangerous case is
// neither. A struct with NO EXPORTED FIELD has an empty layout rather than no
// layout, so zip.LayoutOf accepts it, the Gaps ledger stays silent, and the
// value crosses as nothing at all: measured elsewhere in this node,
// netip.AddrPort went out as 203.0.113.9:9651 and arrived as "invalid
// AddrPort", with no error anywhere.
//
// So the check is a VALUE round trip through the codec the plane encodes
// against, not a question put to the ledger. Every P-Chain reply carrying a
// composite is filled with something recognisable and read back.

type crosser interface {
	MarshalZAP() ([]byte, error)
	UnmarshalZAP([]byte) error
}

// crosses encodes want, decodes it into a fresh value of the same type, and
// hands both back for comparison.
func crosses[T any](t *testing.T, want *T) *T {
	t.Helper()
	out, ok := any(want).(crosser)
	require.True(t, ok, "%T states no ZAP wire", want)

	raw, err := out.MarshalZAP()
	require.NoError(t, err)

	got := new(T)
	back, ok := any(got).(crosser)
	require.True(t, ok)
	require.NoError(t, back.UnmarshalZAP(raw))
	return got
}

// TestATimeCrossesWithItsValue. avajson.Time is the P-Chain's timestamp, and it
// is the shape that hides an empty layout: a wrapper over time.Time, whose own
// fields are unexported. Three replies carry one.
func TestATimeCrossesWithItsValue(t *testing.T) {
	at := time.Date(2026, 8, 29, 22, 34, 56, 789, time.UTC)

	stamp := crosses(t, &GetTimestampReply{Timestamp: avajson.NewTime(at)})
	require.True(t, stamp.Timestamp.Time().Equal(at), "getTimestamp answered %v, want %v", stamp.Timestamp.Time(), at)

	fee := crosses(t, &GetFeeStateReply{
		State: gas.State{Capacity: 11, Excess: 22},
		Price: 33,
		Time:  avajson.NewTime(at),
	})
	require.Equal(t, gas.Gas(11), fee.State.Capacity)
	require.Equal(t, gas.Gas(22), fee.State.Excess)
	require.Equal(t, gas.Price(33), fee.Price)
	require.True(t, fee.Time.Time().Equal(at), "getFeeState answered %v, want %v", fee.Time.Time(), at)

	vdr := crosses(t, &GetValidatorFeeStateReply{Excess: 44, Price: 55, Time: avajson.NewTime(at)})
	require.Equal(t, gas.Gas(44), vdr.Excess)
	require.Equal(t, gas.Price(55), vdr.Price)
	require.True(t, vdr.Time.Time().Equal(at), "getValidatorFeeState answered %v, want %v", vdr.Time.Time(), at)
}

// TestTheAnswersCrossWithTheirValues walks the rest of the P-Chain's replies —
// the ids, the lists, the sum, and the two that were maps until they were
// restated — and reads each one back.
func TestTheAnswersCrossWithTheirValues(t *testing.T) {
	one, two := ids.GenerateTestID(), ids.GenerateTestID()
	node := ids.GenerateTestNodeID()

	height := crosses(t, &GetCurrentSupplyReply{Supply: 7, Height: 9})
	require.Equal(t, avajson.Uint64(7), height.Supply)
	require.Equal(t, avajson.Uint64(9), height.Height)

	// An id is 32 bytes and needs a stated codec; a list of them needs one too.
	blockchains := crosses(t, &ValidatesResponse{BlockchainIDs: []ids.ID{one, two}})
	require.Equal(t, []ids.ID{one, two}, blockchains.BlockchainIDs)

	// Amounts was a map keyed by asset id, restated as the list it always was.
	balance := crosses(t, &GetBalanceResponse{
		Balance:  5,
		Balances: Amounts{{AssetID: one, Value: 5}},
	})
	require.Equal(t, avajson.Uint64(5), balance.Balance)
	require.Equal(t, Amounts{{AssetID: one, Value: 5}}, balance.Balances)

	// ValidatorSet was a map keyed by node id.
	set := crosses(t, &GetValidatorsAtReply{
		Validators: ValidatorSet{{NodeID: node, PublicKey: []byte{1, 2, 3}, Weight: 8}},
	})
	require.Equal(t, ValidatorSet{{NodeID: node, PublicKey: []byte{1, 2, 3}, Weight: 8}}, set.Validators)

	// CurrentValidator was []any, restated as a sum with one arm per shape.
	validators := crosses(t, &GetCurrentValidatorsReply{
		Validators: []CurrentValidator{{
			Permissionless: &platformapi.PermissionlessValidator{
				Staker: platformapi.Staker{TxID: one, NodeID: node, Weight: 12},
			},
		}},
	})
	require.Len(t, validators.Validators, 1)
	arm := validators.Validators[0].Permissionless
	require.NotNil(t, arm, "the sum lost which arm was set")
	require.Equal(t, one, arm.TxID)
	require.Equal(t, node, arm.NodeID)
	require.Equal(t, avajson.Uint64(12), arm.Weight)

	// gas.Dimensions is a fixed array, restated as the list it is on the wire.
	config := crosses(t, &GetFeeConfigReply{
		Weights:     []uint64{1, 2, 3, 4},
		MaxCapacity: 10,
		MinPrice:    2,
	})
	require.Equal(t, []uint64{1, 2, 3, 4}, config.Weights)
	require.Equal(t, gas.Gas(10), config.MaxCapacity)

	// An encoding is a u8 whose written form is a word, and it rides both ways.
	utxos := crosses(t, &GetRewardUTXOsReply{
		NumFetched: 1,
		UTXOs:      []string{"0x00"},
		Encoding:   formatting.JSON,
	})
	require.Equal(t, formatting.JSON, utxos.Encoding)
	require.Equal(t, []string{"0x00"}, utxos.UTXOs)
}

// TestNoAnswerIsHollow is the structural half of the same question, and it is
// what makes the round trips above a rule rather than a list. A composite field
// with no exported member of its own cannot carry anything; if one appears in a
// reply, the value it holds will be dropped in silence.
//
// A type that states its own ZAP wire is exempt: it answers for itself, and its
// fields being unexported is how it keeps them.
func TestNoAnswerIsHollow(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(at reflect.Type, where string) {
		for at.Kind() == reflect.Pointer || at.Kind() == reflect.Slice || at.Kind() == reflect.Array {
			at = at.Elem()
		}
		if at.Kind() != reflect.Struct || seen[at] {
			return
		}
		seen[at] = true

		if reflect.PointerTo(at).Implements(reflect.TypeOf((*crosser)(nil)).Elem()) {
			return // states its own wire, so its own fields are its business
		}
		if at.NumField() == 0 {
			return // an op that takes nothing says so with struct{}; that is honest
		}
		exported := 0
		for i := range at.NumField() {
			if at.Field(i).IsExported() {
				exported++
				walk(at.Field(i).Type, where+"."+at.Field(i).Name)
			}
		}
		require.NotZero(t, exported,
			"%s is %s: it HAS fields and exports none, so it crosses the plane carrying nothing and the Gaps ledger cannot see it",
			where, at)
	}

	for _, op := range (&Service{}).ops(nil).Registry() {
		at := zip.ID(op.Method, op.Path)
		walk(op.InType, at+" in")
		walk(op.OutType, at+" out")
	}
}
