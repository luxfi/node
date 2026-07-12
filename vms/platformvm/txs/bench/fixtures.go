// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package bench holds the reflection codec-vs-native-ZAP benchmark harness
// for the platformvm txs.
//
// FIXTURE PHILOSOPHY: every tx the harness measures is built with field
// counts representative of mainnet — a typical AddPermissionlessValidator
// in production carries 2 inputs + 2 outputs + 1 stake-out + signer.PoP
// (the most common case as of 2026-06). Empty BaseTx{} fixtures are
// excluded from the headline numbers; they appear only as a "minimal
// payload" floor in the report to bound how much of the speedup is
// fixed-cost.
package bench

import (
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/platformvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/types"
)

// MainnetAssetID is the canonical LUX asset ID on the mainnet P-Chain,
// used in every fixture so that benchmark inputs match production tx
// payloads byte-for-byte in the "asset ID" 32-byte slot.
var MainnetAssetID = ids.ID{
	0x51, 0xc2, 0x4f, 0xe7, 0xee, 0x02, 0x01, 0xff,
	0x0f, 0x33, 0x5f, 0x51, 0x99, 0x28, 0xdb, 0x6e,
	0xef, 0x62, 0x24, 0x25, 0x45, 0x52, 0xc9, 0x6b,
	0x6b, 0x42, 0x5f, 0xbc, 0x18, 0xfa, 0x24, 0x3b,
}

// pChainID is the canonical primary network chain ID embedded in all
// platformvm fixtures. constants.PlatformChainID would do the same, but
// pinning the value here makes the fixtures independent of runtime.
var pChainID = ids.Empty

// fixedAddr is a deterministic short-address used throughout fixtures.
var fixedAddr = ids.ShortID{
	0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
	0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
	0x44, 0x55, 0x66, 0x77,
}

// fixedNodeID is the deterministic node ID used by every validator
// fixture so the bench numbers are reproducible.
var fixedNodeID = ids.NodeID{
	0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
	0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
	0x11, 0x22, 0x33, 0x44,
}

// fixedTxID is the parent TxID for input UTXOs.
var fixedTxID = ids.ID{
	0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
}

// fixedSig is a fixed 96-byte BLS signature placeholder used in the
// proof-of-possession fixtures. The signer.ProofOfPossession structure
// is what mainnet validators carry; embedding it makes the fixture
// realistic in size (192 bytes for the PoP alone).
var fixedSig = [bls.SignatureLen]byte{}

// fixedPK is the fixed 48-byte BLS public key placeholder.
var fixedPK = [bls.PublicKeyLen]byte{}

// buildBaseTxFields builds the embedded lux.BaseTx with the requested
// number of inputs and outputs. Each output carries a single
// OutputOwners with 1 threshold + 1 address, matching the
// overwhelmingly-common mainnet shape.
func buildBaseTxFields(numIns, numOuts int) lux.BaseTx {
	outs := make([]*lux.TransferableOutput, numOuts)
	for i := 0; i < numOuts; i++ {
		outs[i] = &lux.TransferableOutput{
			Asset: lux.Asset{ID: MainnetAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1_000_000 + uint64(i),
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{fixedAddr},
				},
			},
		}
	}
	ins := make([]*lux.TransferableInput, numIns)
	for i := 0; i < numIns; i++ {
		ins[i] = &lux.TransferableInput{
			UTXOID: lux.UTXOID{
				TxID:        fixedTxID,
				OutputIndex: uint32(i),
			},
			Asset: lux.Asset{ID: MainnetAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 2_000_000 + uint64(i),
				Input: secp256k1fx.Input{
					SigIndices: []uint32{0},
				},
			},
		}
	}
	return lux.BaseTx{
		NetworkID:    constants.MainnetID,
		BlockchainID: pChainID,
		Outs:         outs,
		Ins:          ins,
		Memo:         types.JSONByteSlice{},
	}
}

// NewBaseTxFixture returns the canonical "1 input / 1 output" BaseTx
// the way it appears in real mainnet bytes — the smallest non-trivial
// payload and the unit by which all other fixtures are bounded from
// below.
func NewBaseTxFixture() *txs.BaseTx {
	base := buildBaseTxFields(1, 1)
	utx, err := txs.NewBaseTx(&base)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewAddValidatorTxFixture returns a legacy primary-network
// AddValidator with a realistic 2-in / 2-out base, 1 stake-out, and a
// rewards owner of size 1.
func NewAddValidatorTxFixture() *txs.AddValidatorTx {
	stake := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: MainnetAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 2_000 * constants.Lux,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{fixedAddr},
			},
		},
	}}
	base := buildBaseTxFields(2, 2)
	utx, err := txs.NewAddValidatorTx(
		&base,
		txs.Validator{
			NodeID: fixedNodeID,
			Start:  uint64(1_700_000_000),
			End:    uint64(1_700_000_000 + 21*24*60*60),
			Wght:   2_000 * constants.Lux,
		},
		stake,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{fixedAddr},
		},
		200_000, // 20%
	)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewAddDelegatorTxFixture is the delegator counterpart — same shape,
// no DelegationShares (delegators don't carry that field).
func NewAddDelegatorTxFixture() *txs.AddDelegatorTx {
	stake := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: MainnetAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 25 * constants.Lux,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{fixedAddr},
			},
		},
	}}
	base := buildBaseTxFields(2, 2)
	utx, err := txs.NewAddDelegatorTx(
		&base,
		txs.Validator{
			NodeID: fixedNodeID,
			Start:  uint64(1_700_000_000),
			End:    uint64(1_700_000_000 + 14*24*60*60),
			Wght:   25 * constants.Lux,
		},
		stake,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{fixedAddr},
		},
	)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewAddPermissionlessValidatorTxFixture is the Banff-era primary-net
// validator with a full ProofOfPossession (a real mainnet validator tx
// has this signer). Field count: BaseTx(2/2) + Validator + ChainID +
// Signer(PoP) + StakeOuts(1) + ValidatorRewardsOwner + DelegatorRewardsOwner +
// DelegationShares = 8 top-level fields.
func NewAddPermissionlessValidatorTxFixture() *txs.AddPermissionlessValidatorTx {
	stake := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: MainnetAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 2_000 * constants.Lux,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{fixedAddr},
			},
		},
	}}
	base := buildBaseTxFields(2, 2)
	utx, err := txs.NewAddPermissionlessValidatorTx(
		&base,
		txs.Validator{
			NodeID: fixedNodeID,
			Start:  uint64(1_700_000_000),
			End:    uint64(1_700_000_000 + 21*24*60*60),
			Wght:   2_000 * constants.Lux,
		},
		constants.PrimaryNetworkID,
		&signer.ProofOfPossession{
			PublicKey:         fixedPK,
			ProofOfPossession: fixedSig,
		},
		stake,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{fixedAddr},
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{fixedAddr},
		},
		reward.PercentDenominator/5, // 20%
	)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewAddPermissionlessDelegatorTxFixture is the delegator companion to
// AddPermissionlessValidator. No Signer; no RewardsOwner split; just
// the single DelegationRewardsOwner. Smaller wire footprint than the
// validator fixture by ~200B (no PoP).
func NewAddPermissionlessDelegatorTxFixture() *txs.AddPermissionlessDelegatorTx {
	stake := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: MainnetAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 25 * constants.Lux,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{fixedAddr},
			},
		},
	}}
	base := buildBaseTxFields(2, 2)
	utx, err := txs.NewAddPermissionlessDelegatorTx(
		&base,
		txs.Validator{
			NodeID: fixedNodeID,
			Start:  uint64(1_700_000_000),
			End:    uint64(1_700_000_000 + 14*24*60*60),
			Wght:   25 * constants.Lux,
		},
		constants.PrimaryNetworkID,
		stake,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{fixedAddr},
		},
	)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewImportTxFixture covers the cross-chain import path; the
// ImportedInputs slice is what real X→P moves carry. Two imported
// inputs is the modal case (one for amount, one for fee).
func NewImportTxFixture() *txs.ImportTx {
	importedIns := []*lux.TransferableInput{{
		UTXOID: lux.UTXOID{TxID: fixedTxID, OutputIndex: 0},
		Asset:  lux.Asset{ID: MainnetAssetID},
		In: &secp256k1fx.TransferInput{
			Amt:   500_000,
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}, {
		UTXOID: lux.UTXOID{TxID: fixedTxID, OutputIndex: 1},
		Asset:  lux.Asset{ID: MainnetAssetID},
		In: &secp256k1fx.TransferInput{
			Amt:   1_500_000,
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}}
	base := buildBaseTxFields(0, 1)
	utx, err := txs.NewImportTx(&base, ids.GenerateTestID(), importedIns)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewExportTxFixture covers the P→X (or P→C) export.
func NewExportTxFixture() *txs.ExportTx {
	exportedOuts := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: MainnetAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 750_000,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{fixedAddr},
			},
		},
	}}
	base := buildBaseTxFields(1, 1)
	utx, err := txs.NewExportTx(&base, ids.GenerateTestID(), exportedOuts)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewAdvanceTimeTxFixture is the minimal scheduled-time-advance tx.
// 4 fields in the wire layout: codec version, type ID, then a single
// uint64. Smallest of the smalls.
func NewAdvanceTimeTxFixture() *txs.AdvanceTimeTx {
	return txs.NewAdvanceTimeTx(1_700_000_000)
}

// NewRewardValidatorTxFixture is the "reward this validator" tx.
// Carries a single ids.ID. Tiny.
func NewRewardValidatorTxFixture() *txs.RewardValidatorTx {
	return txs.NewRewardValidatorTx(fixedTxID)
}

// NewCreateChainTxFixture is the legacy create-blockchain tx.
func NewCreateChainTxFixture() *txs.CreateChainTx {
	base := buildBaseTxFields(1, 1)
	utx, err := txs.NewCreateChainTx(
		&base,
		ids.GenerateTestID(),
		"lqd bench chain",
		ids.GenerateTestID(),
		[]ids.ID{ids.GenerateTestID()},
		make([]byte, 256),
		&secp256k1fx.Input{SigIndices: []uint32{0}},
	)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewCreateNetworkTxFixture is the legacy create-network tx.
func NewCreateNetworkTxFixture() *txs.CreateNetworkTx {
	base := buildBaseTxFields(1, 1)
	utx, err := txs.NewCreateNetworkTx(
		&base,
		ids.Empty, // parent = primary network
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{fixedAddr},
		},
		security.Mode{RestakeParent: true}, // restaked L2, no own set
		nil,       // validators
		ids.Empty, // managerChainID (P-Chain-governed)
		nil,       // managerAddress
	)
	if err != nil {
		panic(err)
	}
	return utx
}

// NewBaseTxWithStakeableFixture stresses the stakeable.LockOut path
// (slot 22). Real validator deposit txs use this; ignoring it would
// understate the codec cost.
func NewBaseTxWithStakeableFixture() *txs.BaseTx {
	base := buildBaseTxFields(1, 0)
	base.Outs = []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: MainnetAssetID},
		Out: &stakeable.LockOut{
			Locktime: 1_700_000_000 + 30*24*60*60,
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: 1_000_000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{fixedAddr},
				},
			},
		},
	}}
	utx, err := txs.NewBaseTx(&base)
	if err != nil {
		panic(err)
	}
	return utx
}

// FixtureMap returns the named fixtures the bench harness iterates
// over. Add new tx types here as Blue lands accessors for them; the
// per-type bench is reflective over this map.
//
// Insertion order matters for table output: this is the order the
// columns appear in RESULTS.md.
func FixtureMap() map[string]txs.UnsignedTx {
	return map[string]txs.UnsignedTx{
		"BaseTx":                       NewBaseTxFixture(),
		"BaseTxStakeable":              NewBaseTxWithStakeableFixture(),
		"AddValidatorTx":               NewAddValidatorTxFixture(),
		"AddDelegatorTx":               NewAddDelegatorTxFixture(),
		"AddPermissionlessValidatorTx": NewAddPermissionlessValidatorTxFixture(),
		"AddPermissionlessDelegatorTx": NewAddPermissionlessDelegatorTxFixture(),
		"ImportTx":                     NewImportTxFixture(),
		"ExportTx":                     NewExportTxFixture(),
		"AdvanceTimeTx":                NewAdvanceTimeTxFixture(),
		"RewardValidatorTx":            NewRewardValidatorTxFixture(),
		"CreateChainTx":                NewCreateChainTxFixture(),
		"CreateNetworkTx":              NewCreateNetworkTxFixture(),
	}
}

// MustMarshal returns the unsigned tx's canonical bytes. The struct IS the
// wire (native ZAP), so this is just the tx buffer — no codec step.
func MustMarshal(unsigned txs.UnsignedTx) []byte {
	return unsigned.Bytes()
}

// MustMarshalSignedTx wraps an unsigned in a *txs.Tx with empty creds
// and returns the canonical signed-byte form. This matches the
// mempool/block path: txs on the wire are *txs.Tx, not bare unsigned.
// With no creds, signed bytes == unsigned bytes ‖ (empty) — Initialize
// binds them.
func MustMarshalSignedTx(unsigned txs.UnsignedTx) []byte {
	tx := &txs.Tx{Unsigned: unsigned}
	if err := tx.Initialize(); err != nil {
		panic(err)
	}
	return tx.Bytes()
}
