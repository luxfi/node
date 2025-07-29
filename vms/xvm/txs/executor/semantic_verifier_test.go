// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/validators/validatorsmock"
	"github.com/luxfi/db"
	"github.com/luxfi/db/memdb"
	"github.com/luxfi/db/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
)

func TestSemanticVerifierBaseTx(t *testing.T) {
	ctx := consensustest.Context(t, consensustest.XChainID)

	typeToFxIndex := make(map[reflect.Type]int)
	secpFx := &secp256k1fx.Fx{}
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			secpFx,
		},
	)
	require.NoError(t, err)

	codec := parser.Codec()
	txID := ids.GenerateTestID()
	utxoID := lux.UTXOID{
		TxID:        txID,
		OutputIndex: 2,
	}
	asset := lux.Asset{
		ID: ids.GenerateTestID(),
	}
	inputSigner := secp256k1fx.Input{
		SigIndices: []uint32{
			0,
		},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   12345,
		Input: inputSigner,
	}
	input := lux.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}
	baseTx := txs.BaseTx{
		BaseTx: lux.BaseTx{
			Ins: []*lux.TransferableInput{
				&input,
			},
		},
	}

	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: secpFx,
			},
		},
		TypeToFxIndex: typeToFxIndex,
		Codec:         codec,
		FeeAssetID:    ids.GenerateTestID(),
		Bootstrapped:  true,
	}
	require.NoError(t, secpFx.Bootstrapped())

	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].Address(),
		},
	}
	output := secp256k1fx.TransferOutput{
		Amt:          12345,
		OutputOwners: outputOwners,
	}
	utxo := lux.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &output,
	}
	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}

	tests := []struct {
		name      string
		stateFunc func(*gomock.Controller) state.Chain
		txFunc    func(*require.Assertions) *txs.Tx
		err       error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: nil,
		},
		{
			name: "assetID mismatch",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				utxo := utxo
				utxo.Asset.ID = ids.GenerateTestID()

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errAssetIDMismatch,
		},
		{
			name: "not allowed input feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "invalid signature",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[1]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrWrongSig,
		},
		{
			name: "missing UTXO",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "invalid UTXO amount",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				output := output
				output.Amt--

				utxo := utxo
				utxo.Out = &output

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrMismatchedAmounts,
		},
		{
			name: "not allowed output feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				baseTx := baseTx
				baseTx.Ins = nil
				baseTx.Outs = []*lux.TransferableOutput{
					{
						Asset: asset,
						Out:   &output,
					},
				}
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{},
				))
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "unknown asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "not an asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				tx := txs.Tx{
					Unsigned: &baseTx,
				}

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&tx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &baseTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errNotAnAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			state := test.stateFunc(ctrl)
			tx := test.txFunc(require)

			err = tx.Unsigned.Visit(&SemanticVerifier{
				Backend: backend,
				State:   state,
				Tx:      tx,
			})
			require.ErrorIs(err, test.err)
		})
	}
}

func TestSemanticVerifierExportTx(t *testing.T) {
	ctx := consensustest.Context(t, consensustest.XChainID)

	typeToFxIndex := make(map[reflect.Type]int)
	secpFx := &secp256k1fx.Fx{}
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			secpFx,
		},
	)
	require.NoError(t, err)

	codec := parser.Codec()
	txID := ids.GenerateTestID()
	utxoID := lux.UTXOID{
		TxID:        txID,
		OutputIndex: 2,
	}
	asset := lux.Asset{
		ID: ids.GenerateTestID(),
	}
	inputSigner := secp256k1fx.Input{
		SigIndices: []uint32{
			0,
		},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   12345,
		Input: inputSigner,
	}
	input := lux.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}
	baseTx := txs.BaseTx{
		BaseTx: lux.BaseTx{
			Ins: []*lux.TransferableInput{
				&input,
			},
		},
	}
	exportTx := txs.ExportTx{
		BaseTx:           baseTx,
		DestinationChain: ctx.CChainID,
	}

	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: secpFx,
			},
		},
		TypeToFxIndex: typeToFxIndex,
		Codec:         codec,
		FeeAssetID:    ids.GenerateTestID(),
		Bootstrapped:  true,
	}
	require.NoError(t, secpFx.Bootstrapped())

	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].Address(),
		},
	}
	output := secp256k1fx.TransferOutput{
		Amt:          12345,
		OutputOwners: outputOwners,
	}
	utxo := lux.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &output,
	}
	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}

	tests := []struct {
		name      string
		stateFunc func(*gomock.Controller) state.Chain
		txFunc    func(*require.Assertions) *txs.Tx
		err       error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: nil,
		},
		{
			name: "assetID mismatch",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				utxo := utxo
				utxo.Asset.ID = ids.GenerateTestID()

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errAssetIDMismatch,
		},
		{
			name: "not allowed input feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "invalid signature",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[1]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrWrongSig,
		},
		{
			name: "missing UTXO",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "invalid UTXO amount",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				output := output
				output.Amt--

				utxo := utxo
				utxo.Out = &output

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: secp256k1fx.ErrMismatchedAmounts,
		},
		{
			name: "not allowed output feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil

				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}

				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				exportTx := exportTx
				exportTx.Ins = nil
				exportTx.ExportedOuts = []*lux.TransferableOutput{
					{
						Asset: asset,
						Out:   &output,
					},
				}
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{},
				))
				return tx
			},
			err: errIncompatibleFx,
		},
		{
			name: "unknown asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(nil, database.ErrNotFound)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: database.ErrNotFound,
		},
		{
			name: "not an asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)

				tx := txs.Tx{
					Unsigned: &baseTx,
				}

				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
				state.EXPECT().GetTx(asset.ID).Return(&tx, nil)

				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &exportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[0]},
					},
				))
				return tx
			},
			err: errNotAnAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			state := test.stateFunc(ctrl)
			tx := test.txFunc(require)

			err = tx.Unsigned.Visit(&SemanticVerifier{
				Backend: backend,
				State:   state,
				Tx:      tx,
			})
			require.ErrorIs(err, test.err)
		})
	}
}

func TestSemanticVerifierExportTxDifferentSubnet(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	ctx := consensustest.Context(t, consensustest.XChainID)

	validatorState := validatorsmock.NewState(ctrl)
	validatorState.EXPECT().GetSubnetID(gomock.Any(), ctx.CChainID).AnyTimes().Return(ids.GenerateTestID(), nil)
	ctx.ValidatorState = validatorState

	typeToFxIndex := make(map[reflect.Type]int)
	secpFx := &secp256k1fx.Fx{}
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			secpFx,
		},
	)
	require.NoError(err)

	codec := parser.Codec()
	txID := ids.GenerateTestID()
	utxoID := lux.UTXOID{
		TxID:        txID,
		OutputIndex: 2,
	}
	asset := lux.Asset{
		ID: ids.GenerateTestID(),
	}
	inputSigner := secp256k1fx.Input{
		SigIndices: []uint32{
			0,
		},
	}
	fxInput := secp256k1fx.TransferInput{
		Amt:   12345,
		Input: inputSigner,
	}
	input := lux.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In:     &fxInput,
	}
	baseTx := txs.BaseTx{
		BaseTx: lux.BaseTx{
			Ins: []*lux.TransferableInput{
				&input,
			},
		},
	}
	exportTx := txs.ExportTx{
		BaseTx:           baseTx,
		DestinationChain: ctx.CChainID,
	}

	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: secpFx,
			},
		},
		TypeToFxIndex: typeToFxIndex,
		Codec:         codec,
		FeeAssetID:    ids.GenerateTestID(),
		Bootstrapped:  true,
	}
	require.NoError(secpFx.Bootstrapped())

	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].Address(),
		},
	}
	output := secp256k1fx.TransferOutput{
		Amt:          12345,
		OutputOwners: outputOwners,
	}
	utxo := lux.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &output,
	}
	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}

	state := state.NewMockChain(ctrl)

	state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil)
	state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil)

	tx := &txs.Tx{
		Unsigned: &exportTx,
	}
	require.NoError(tx.SignSECP256K1Fx(
		codec,
		[][]*secp256k1.PrivateKey{
			{keys[0]},
		},
	))

	err = tx.Unsigned.Visit(&SemanticVerifier{
		Backend: backend,
		State:   state,
		Tx:      tx,
	})
	require.ErrorIs(err, verify.ErrMismatchedSubnetIDs)
}

func TestSemanticVerifierImportTx(t *testing.T) {
	ctx := consensustest.Context(t, consensustest.XChainID)

	m := atomic.NewMemory(prefixdb.New([]byte{0}, memdb.New()))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	typeToFxIndex := make(map[reflect.Type]int)
	fx := &secp256k1fx.Fx{}
	parser, err := txs.NewCustomParser(
		typeToFxIndex,
		new(mockable.Clock),
		logging.NoWarn{},
		[]fxs.Fx{
			fx,
		},
	)
	require.NoError(t, err)

	codec := parser.Codec()
	utxoID := lux.UTXOID{
		TxID:        ids.GenerateTestID(),
		OutputIndex: 2,
	}

	asset := lux.Asset{
		ID: ids.GenerateTestID(),
	}
	outputOwners := secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].Address(),
		},
	}
	baseTx := txs.BaseTx{
		BaseTx: lux.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: ctx.ChainID,
			Outs: []*lux.TransferableOutput{{
				Asset: asset,
				Out: &secp256k1fx.TransferOutput{
					Amt:          1000,
					OutputOwners: outputOwners,
				},
			}},
		},
	}
	input := lux.TransferableInput{
		UTXOID: utxoID,
		Asset:  asset,
		In: &secp256k1fx.TransferInput{
			Amt: 12345,
			Input: secp256k1fx.Input{
				SigIndices: []uint32{0},
			},
		},
	}
	unsignedImportTx := txs.ImportTx{
		BaseTx:      baseTx,
		SourceChain: ctx.CChainID,
		ImportedIns: []*lux.TransferableInput{
			&input,
		},
	}
	importTx := &txs.Tx{
		Unsigned: &unsignedImportTx,
	}
	require.NoError(t, importTx.SignSECP256K1Fx(
		codec,
		[][]*secp256k1.PrivateKey{
			{keys[0]},
		},
	))

	backend := &Backend{
		Ctx:    ctx,
		Config: &feeConfig,
		Fxs: []*fxs.ParsedFx{
			{
				ID: secp256k1fx.ID,
				Fx: fx,
			},
		},
		TypeToFxIndex: typeToFxIndex,
		Codec:         codec,
		FeeAssetID:    ids.GenerateTestID(),
		Bootstrapped:  true,
	}
	require.NoError(t, fx.Bootstrapped())

	output := secp256k1fx.TransferOutput{
		Amt:          12345,
		OutputOwners: outputOwners,
	}
	utxo := lux.UTXO{
		UTXOID: utxoID,
		Asset:  asset,
		Out:    &output,
	}
	utxoBytes, err := codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(t, err)

	peerSharedMemory := m.NewSharedMemory(ctx.CChainID)
	inputID := utxo.InputID()
	require.NoError(t, peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			keys[0].PublicKey().Address().Bytes(),
		},
	}}}}))

	unsignedCreateAssetTx := txs.CreateAssetTx{
		States: []*txs.InitialState{{
			FxIndex: 0,
		}},
	}
	createAssetTx := txs.Tx{
		Unsigned: &unsignedCreateAssetTx,
	}
	tests := []struct {
		name        string
		stateFunc   func(*gomock.Controller) state.Chain
		txFunc      func(*require.Assertions) *txs.Tx
		expectedErr error
	}{
		{
			name: "valid",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return importTx
			},
			expectedErr: nil,
		},
		{
			name: "not allowed input feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil
				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return importTx
			},
			expectedErr: errIncompatibleFx,
		},
		{
			name: "invalid signature",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				tx := &txs.Tx{
					Unsigned: &unsignedImportTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					[][]*secp256k1.PrivateKey{
						{keys[1]},
					},
				))
				return tx
			},
			expectedErr: secp256k1fx.ErrWrongSig,
		},
		{
			name: "not allowed output feature extension",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				unsignedCreateAssetTx := unsignedCreateAssetTx
				unsignedCreateAssetTx.States = nil
				createAssetTx := txs.Tx{
					Unsigned: &unsignedCreateAssetTx,
				}
				state.EXPECT().GetTx(asset.ID).Return(&createAssetTx, nil).AnyTimes()
				return state
			},
			txFunc: func(require *require.Assertions) *txs.Tx {
				importTx := unsignedImportTx
				importTx.Ins = nil
				importTx.ImportedIns = []*lux.TransferableInput{
					&input,
				}
				tx := &txs.Tx{
					Unsigned: &importTx,
				}
				require.NoError(tx.SignSECP256K1Fx(
					codec,
					nil,
				))
				return tx
			},
			expectedErr: errIncompatibleFx,
		},
		{
			name: "unknown asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(nil, database.ErrNotFound)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return importTx
			},
			expectedErr: database.ErrNotFound,
		},
		{
			name: "not an asset",
			stateFunc: func(ctrl *gomock.Controller) state.Chain {
				state := state.NewMockChain(ctrl)
				tx := txs.Tx{
					Unsigned: &baseTx,
				}
				state.EXPECT().GetUTXO(utxoID.InputID()).Return(&utxo, nil).AnyTimes()
				state.EXPECT().GetTx(asset.ID).Return(&tx, nil)
				return state
			},
			txFunc: func(*require.Assertions) *txs.Tx {
				return importTx
			},
			expectedErr: errNotAnAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)

			state := test.stateFunc(ctrl)
			tx := test.txFunc(require)
			err := tx.Unsigned.Visit(&SemanticVerifier{
				Backend: backend,
				State:   state,
				Tx:      tx,
			})
			require.ErrorIs(err, test.expectedErr)
		})
	}
}
