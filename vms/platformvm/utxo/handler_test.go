// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"math"
	"testing"
	"time"

	"github.com/luxdefi/luxd/database/memdb"
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow"
	"github.com/luxdefi/luxd/utils/crypto"
	"github.com/luxdefi/luxd/utils/timer/mockable"
	"github.com/luxdefi/luxd/vms/components/lux"
	"github.com/luxdefi/luxd/vms/components/verify"
	"github.com/luxdefi/luxd/vms/platformvm/stakeable"
	"github.com/luxdefi/luxd/vms/platformvm/txs"
	"github.com/luxdefi/luxd/vms/secp256k1fx"
)

var _ txs.UnsignedTx = (*dummyUnsignedTx)(nil)

type dummyUnsignedTx struct {
	txs.BaseTx
}

func (du *dummyUnsignedTx) Visit(txs.Visitor) error {
	return nil
}

func TestVerifySpendUTXOs(t *testing.T) {
	fx := &secp256k1fx.Fx{}

	if err := fx.InitializeVM(&secp256k1fx.TestVM{}); err != nil {
		t.Fatal(err)
	}
	if err := fx.Bootstrapped(); err != nil {
		t.Fatal(err)
	}

	h := &handler{
		ctx: snow.DefaultContextTest(),
		clk: &mockable.Clock{},
		utxosReader: lux.NewUTXOState(
			memdb.New(),
			txs.Codec,
		),
		fx: fx,
	}

	// The handler time during a test, unless [chainTimestamp] is set
	now := time.Unix(1607133207, 0)

	unsignedTx := dummyUnsignedTx{
		BaseTx: txs.BaseTx{},
	}
	unsignedTx.Initialize([]byte{0})

	customAssetID := ids.GenerateTestID()

	// Note that setting [chainTimestamp] also set's the handler's clock.
	// Adjust input/output locktimes accordingly.
	tests := []struct {
		description     string
		utxos           []*lux.UTXO
		ins             []*lux.TransferableInput
		outs            []*lux.TransferableOutput
		creds           []verify.Verifiable
		producedAmounts map[ids.ID]uint64
		shouldErr       bool
	}{
		{
			description:     "no inputs, no outputs, no fee",
			utxos:           []*lux.UTXO{},
			ins:             []*lux.TransferableInput{},
			outs:            []*lux.TransferableOutput{},
			creds:           []verify.Verifiable{},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       false,
		},
		{
			description: "no inputs, no outputs, positive fee",
			utxos:       []*lux.UTXO{},
			ins:         []*lux.TransferableInput{},
			outs:        []*lux.TransferableOutput{},
			creds:       []verify.Verifiable{},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "wrong utxo assetID, one input, no outputs, no fee",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: customAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "one wrong assetID input, no outputs, no fee",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: customAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "one input, one wrong assetID output, no fee",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "attempt to consume locked output as unlocked",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(now.Add(time.Second).Unix()),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "attempt to modify locktime",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(now.Add(time.Second).Unix()),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &stakeable.LockIn{
					Locktime: uint64(now.Unix()),
					TransferableIn: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "one input, no outputs, positive fee",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: false,
		},
		{
			description: "wrong number of credentials",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs:  []*lux.TransferableOutput{},
			creds: []verify.Verifiable{},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "wrong number of UTXOs",
			utxos:       []*lux.UTXO{},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "invalid credential",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				(*secp256k1fx.Credential)(nil),
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "invalid signature",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.GenerateTestShortID(),
						},
					},
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{
					Sigs: [][crypto.SECP256K1RSigLen]byte{
						{},
					},
				},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "one input, no outputs, positive fee",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1,
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: 1,
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: false,
		},
		{
			description: "locked one input, no outputs, no fee",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(now.Unix()) + 1,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &stakeable.LockIn{
					Locktime: uint64(now.Unix()) + 1,
					TransferableIn: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       false,
		},
		{
			description: "locked one input, no outputs, positive fee",
			utxos: []*lux.UTXO{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(now.Unix()) + 1,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			}},
			ins: []*lux.TransferableInput{{
				Asset: lux.Asset{ID: h.ctx.LUXAssetID},
				In: &stakeable.LockIn{
					Locktime: uint64(now.Unix()) + 1,
					TransferableIn: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			}},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "one locked and one unlocked input, one locked output, positive fee",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) + 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &stakeable.LockIn{
						Locktime: uint64(now.Unix()) + 1,
						TransferableIn: &secp256k1fx.TransferInput{
							Amt: 1,
						},
					},
				},
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) + 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: false,
		},
		{
			description: "one locked and one unlocked input, one locked output, positive fee, partially locked",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) + 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 2,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &stakeable.LockIn{
						Locktime: uint64(now.Unix()) + 1,
						TransferableIn: &secp256k1fx.TransferInput{
							Amt: 1,
						},
					},
				},
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 2,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) + 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 2,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: false,
		},
		{
			description: "one unlocked input, one locked output, zero fee",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) - 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       false,
		},
		{
			description: "attempted overflow",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 2,
					},
				},
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "attempted mint",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 2,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "attempted mint through locking",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 2,
						},
					},
				},
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: math.MaxUint64,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "attempted mint through mixed locking (low then high)",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 2,
					},
				},
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: math.MaxUint64,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "attempted mint through mixed locking (high then low)",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: math.MaxUint64,
					},
				},
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &stakeable.LockOut{
						Locktime: 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 2,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "transfer non-lux asset",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       false,
		},
		{
			description: "lock non-lux asset",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Add(time.Second).Unix()),
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       false,
		},
		{
			description: "attempted asset conversion",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{},
			shouldErr:       true,
		},
		{
			description: "attempted asset conversion with burn",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "two inputs, one output with custom asset, with fee",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
				{
					Asset: lux.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: false,
		},
		{
			description: "one input, fee, custom asset",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "one input, custom fee",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				customAssetID: 1,
			},
			shouldErr: false,
		},
		{
			description: "one input, custom fee, wrong burn",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				customAssetID: 1,
			},
			shouldErr: true,
		},
		{
			description: "two inputs, multiple fee",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: h.ctx.LUXAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
				{
					Asset: lux.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
				&secp256k1fx.Credential{},
			},
			producedAmounts: map[ids.ID]uint64{
				h.ctx.LUXAssetID: 1,
				customAssetID:     1,
			},
			shouldErr: false,
		},
		{
			description: "one unlock input, one locked output, zero fee, unlocked, custom asset",
			utxos: []*lux.UTXO{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &stakeable.LockOut{
						Locktime: uint64(now.Unix()) - 1,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt: 1,
						},
					},
				},
			},
			ins: []*lux.TransferableInput{
				{
					Asset: lux.Asset{ID: customAssetID},
					In: &secp256k1fx.TransferInput{
						Amt: 1,
					},
				},
			},
			outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: customAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 1,
					},
				},
			},
			creds: []verify.Verifiable{
				&secp256k1fx.Credential{},
			},
			producedAmounts: make(map[ids.ID]uint64),
			shouldErr:       false,
		},
	}

	for _, test := range tests {
		h.clk.Set(now)

		t.Run(test.description, func(t *testing.T) {
			err := h.VerifySpendUTXOs(
				&unsignedTx,
				test.utxos,
				test.ins,
				test.outs,
				test.creds,
				test.producedAmounts,
			)

			if err == nil && test.shouldErr {
				t.Fatalf("expected error but got none")
			} else if err != nil && !test.shouldErr {
				t.Fatalf("unexpected error: %s", err)
			}
		})
	}
}
