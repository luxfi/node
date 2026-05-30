// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"strings"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/runtime"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func newRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	return &runtime.Runtime{
		NetworkID: 1337,
		ChainID:   ids.GenerateTestID(),
		XAssetID:  ids.GenerateTestID(),
	}
}

func validBaseTxForAsset(rt *runtime.Runtime) BaseTx {
	return BaseTx{
		BaseTx: lux.BaseTx{
			NetworkID:    rt.NetworkID,
			BlockchainID: rt.ChainID,
			Ins:          []*lux.TransferableInput{},
			Outs:         []*lux.TransferableOutput{},
		},
	}
}

func TestCreateAssetTx_SyntacticVerify_NameTooShort(t *testing.T) {
	rt := newRuntime(t)
	tx := &CreateAssetTx{
		BaseTx: validBaseTxForAsset(rt),
		Name:   "",
		Symbol: "LUX",
	}
	require.ErrorIs(t, tx.SyntacticVerify(rt), ErrNameTooShort)
}

func TestCreateAssetTx_SyntacticVerify_NameTooLong(t *testing.T) {
	rt := newRuntime(t)
	tx := &CreateAssetTx{
		BaseTx: validBaseTxForAsset(rt),
		Name:   strings.Repeat("a", maxNameLen+1),
		Symbol: "LUX",
	}
	require.ErrorIs(t, tx.SyntacticVerify(rt), ErrNameTooLong)
}

func TestCreateAssetTx_SyntacticVerify_SymbolBadChar(t *testing.T) {
	rt := newRuntime(t)
	tx := &CreateAssetTx{
		BaseTx: validBaseTxForAsset(rt),
		Name:   "ValidAsset",
		Symbol: "lux", // must be upper-case
		States: []*InitialState{{FxIndex: 0, Outs: []verify.State{}}},
	}
	require.ErrorIs(t, tx.SyntacticVerify(rt), ErrIllegalSymbolChar)
}

func TestCreateAssetTx_SyntacticVerify_DenominationTooLarge(t *testing.T) {
	rt := newRuntime(t)
	tx := &CreateAssetTx{
		BaseTx:       validBaseTxForAsset(rt),
		Name:         "LUX",
		Symbol:       "LUX",
		Denomination: maxDenomination + 1,
		States:       []*InitialState{{FxIndex: 0, Outs: []verify.State{}}},
	}
	require.ErrorIs(t, tx.SyntacticVerify(rt), ErrDenominationTooLarge)
}

func TestCreateAssetTx_SyntacticVerify_NoInitialStates(t *testing.T) {
	rt := newRuntime(t)
	tx := &CreateAssetTx{
		BaseTx: validBaseTxForAsset(rt),
		Name:   "LUX",
		Symbol: "LUX",
		States: nil,
	}
	require.ErrorIs(t, tx.SyntacticVerify(rt), ErrNoInitialStates)
}

func TestCreateAssetTx_SyntacticVerify_NonZeroFxIndexRejected(t *testing.T) {
	rt := newRuntime(t)
	tx := &CreateAssetTx{
		BaseTx:       validBaseTxForAsset(rt),
		Name:         "LUX",
		Symbol:       "LUX",
		Denomination: 9,
		States:       []*InitialState{{FxIndex: 1, Outs: []verify.State{}}},
	}
	require.ErrorIs(t, tx.SyntacticVerify(rt), ErrInitialStateNonZero)
}

func TestCreateAssetTx_CodecRoundtrip(t *testing.T) {
	require := require.New(t)
	tx := &CreateAssetTx{
		BaseTx: BaseTx{
			BaseTx: lux.BaseTx{
				NetworkID:    1337,
				BlockchainID: ids.GenerateTestID(),
			},
		},
		Name:         "LUX",
		Symbol:       "LUX",
		Denomination: 9,
		States: []*InitialState{
			{FxIndex: 0, Outs: []verify.State{
				&secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			}},
		},
	}

	var unsigned UnsignedTx = tx
	bytes, err := Codec.Marshal(CodecVersion, &unsigned)
	require.NoError(err)

	var roundtripped UnsignedTx
	_, err = Codec.Unmarshal(bytes, &roundtripped)
	require.NoError(err)

	got, ok := roundtripped.(*CreateAssetTx)
	require.True(ok)
	require.Equal(tx.Name, got.Name)
	require.Equal(tx.Symbol, got.Symbol)
	require.Equal(tx.Denomination, got.Denomination)
	require.Equal(len(tx.States), len(got.States))
}

func TestOperationTx_SyntacticVerify_NoOperations(t *testing.T) {
	rt := newRuntime(t)
	tx := &OperationTx{
		BaseTx: validBaseTxForAsset(rt),
		Ops:    nil,
	}
	require.ErrorIs(t, tx.SyntacticVerify(rt), ErrNoOperations)
}

func TestOperationTx_VisitorDispatches(t *testing.T) {
	require := require.New(t)
	tx := &OperationTx{}

	called := false
	v := &mockVisitor{operationFn: func(*OperationTx) error {
		called = true
		return nil
	}}
	require.NoError(tx.Visit(v))
	require.True(called)
}

func TestCreateAssetTx_VisitorDispatches(t *testing.T) {
	require := require.New(t)
	tx := &CreateAssetTx{}

	called := false
	v := &mockVisitor{createAssetFn: func(*CreateAssetTx) error {
		called = true
		return nil
	}}
	require.NoError(tx.Visit(v))
	require.True(called)
}

// mockVisitor stubs every Visitor method but the two we exercise here.
type mockVisitor struct {
	createAssetFn func(*CreateAssetTx) error
	operationFn   func(*OperationTx) error
}

func (*mockVisitor) AddValidatorTx(*AddValidatorTx) error                              { return nil }
func (*mockVisitor) AddChainValidatorTx(*AddChainValidatorTx) error                    { return nil }
func (*mockVisitor) AddDelegatorTx(*AddDelegatorTx) error                              { return nil }
func (*mockVisitor) CreateNetworkTx(*CreateNetworkTx) error                            { return nil }
func (*mockVisitor) CreateChainTx(*CreateChainTx) error                                { return nil }
func (*mockVisitor) ImportTx(*ImportTx) error                                          { return nil }
func (*mockVisitor) ExportTx(*ExportTx) error                                          { return nil }
func (*mockVisitor) AdvanceTimeTx(*AdvanceTimeTx) error                                { return nil }
func (*mockVisitor) RewardValidatorTx(*RewardValidatorTx) error                        { return nil }
func (*mockVisitor) RemoveChainValidatorTx(*RemoveChainValidatorTx) error              { return nil }
func (*mockVisitor) TransformChainTx(*TransformChainTx) error                          { return nil }
func (*mockVisitor) AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error  { return nil }
func (*mockVisitor) AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error  { return nil }
func (*mockVisitor) TransferChainOwnershipTx(*TransferChainOwnershipTx) error          { return nil }
func (*mockVisitor) BaseTx(*BaseTx) error                                              { return nil }
func (*mockVisitor) ConvertNetworkToL1Tx(*ConvertNetworkToL1Tx) error                  { return nil }
func (*mockVisitor) CreateSovereignL1Tx(*CreateSovereignL1Tx) error                    { return nil }
func (*mockVisitor) RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error                { return nil }
func (*mockVisitor) SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error              { return nil }
func (*mockVisitor) IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error  { return nil }
func (*mockVisitor) DisableL1ValidatorTx(*DisableL1ValidatorTx) error                  { return nil }
func (*mockVisitor) SlashValidatorTx(*SlashValidatorTx) error                          { return nil }
func (m *mockVisitor) CreateAssetTx(tx *CreateAssetTx) error {
	if m.createAssetFn != nil {
		return m.createAssetFn(tx)
	}
	return nil
}
func (m *mockVisitor) OperationTx(tx *OperationTx) error {
	if m.operationFn != nil {
		return m.operationFn(tx)
	}
	return nil
}
