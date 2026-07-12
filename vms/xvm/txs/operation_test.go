// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
)

// testOperable is a minimal fxs.FxOperation: it verifies (via the embedded
// TestState), costs zero (via TestTransferable), yields no outputs, and is
// wire-serializable via a constant fx-operation envelope so the byte-keyed
// operation sort has a stable key.
type testOperable struct {
	lux.TestTransferable

	Outputs []verify.State
}

func (o *testOperable) Outs() []verify.State {
	return o.Outputs
}

func (*testOperable) Bytes() []byte {
	// A deterministic, non-empty fx-operation envelope stand-in. The operation
	// sort key is Asset + UTXOIDs + this blob; distinct UTXOIDs make distinct
	// keys.
	return []byte{0x01, 0x05}
}

func TestOperationVerifyNil(t *testing.T) {
	op := (*Operation)(nil)
	err := op.Verify()
	require.ErrorIs(t, err, ErrNilOperation)
}

func TestOperationVerifyEmpty(t *testing.T) {
	op := &Operation{
		Asset: lux.Asset{ID: ids.Empty},
	}
	err := op.Verify()
	require.ErrorIs(t, err, ErrNilFxOperation)
}

func TestOperationVerifyUTXOIDsNotSorted(t *testing.T) {
	op := &Operation{
		Asset: lux.Asset{ID: ids.Empty},
		UTXOIDs: []*lux.UTXOID{
			{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
			{
				TxID:        ids.Empty,
				OutputIndex: 0,
			},
		},
		Op: &testOperable{},
	}
	err := op.Verify()
	require.ErrorIs(t, err, ErrNotSortedAndUniqueUTXOIDs)
}

func TestOperationVerify(t *testing.T) {
	assetID := ids.GenerateTestID()
	op := &Operation{
		Asset: lux.Asset{ID: assetID},
		UTXOIDs: []*lux.UTXOID{
			{
				TxID:        assetID,
				OutputIndex: 1,
			},
		},
		Op: &testOperable{},
	}
	require.NoError(t, op.Verify())
}

func TestOperationSorting(t *testing.T) {
	require := require.New(t)

	ops := []*Operation{
		{
			Asset:   lux.Asset{ID: ids.Empty},
			UTXOIDs: []*lux.UTXOID{{TxID: ids.Empty, OutputIndex: 1}},
			Op:      &testOperable{},
		},
		{
			Asset:   lux.Asset{ID: ids.Empty},
			UTXOIDs: []*lux.UTXOID{{TxID: ids.Empty, OutputIndex: 0}},
			Op:      &testOperable{},
		},
	}
	require.False(IsSortedAndUniqueOperations(ops))
	SortOperations(ops)
	require.True(IsSortedAndUniqueOperations(ops))
	ops = append(ops, &Operation{
		Asset:   lux.Asset{ID: ids.Empty},
		UTXOIDs: []*lux.UTXOID{{TxID: ids.Empty, OutputIndex: 1}},
		Op:      &testOperable{},
	})
	require.False(IsSortedAndUniqueOperations(ops))
}

func TestOperationTxNotState(t *testing.T) {
	intf := interface{}(&OperationTx{})
	_, ok := intf.(verify.State)
	require.False(t, ok)
}
