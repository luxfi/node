// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// oneOut is a single spendable secp256k1fx output owned by one address.
func oneOut(asset ids.ID, amt uint64) *lux.TransferableOutput {
	return &lux.TransferableOutput{
		Asset: lux.Asset{ID: asset},
		Out: &secp256k1fx.TransferOutput{
			Amt: amt,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
		},
	}
}

// oneOwner is a single-address rewards/ownership owner.
func oneOwner() *secp256k1fx.OutputOwners {
	return &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
}

// FuzzTransactionParsing feeds arbitrary bytes at Parse: it must never panic,
// and any tx it does decode must survive a re-Parse of its own bytes with a
// stable ID (the struct-is-wire round-trip: Parse wraps bytes zero-copy, ID =
// hash(bytes)).
func FuzzTransactionParsing(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x00, 0x00})
	f.Add([]byte{0x03, 0x00, 0x22, 0x00})
	testID := ids.GenerateTestID()
	f.Add(testID[:])

	f.Fuzz(func(t *testing.T, data []byte) {
		tx, err := Parse(data)
		if err != nil {
			return // expected for arbitrary bytes
		}

		// Methods must not panic on a decoded tx.
		require.NotNil(t, tx.Unsigned)
		_ = tx.Unsigned.Visit(&visitor{})

		// Re-parse the exact bytes: the ID is hash(signedBytes) and must be stable.
		reparsed, err := Parse(tx.Bytes())
		require.NoError(t, err)
		require.Equal(t, tx.ID(), reparsed.ID())
	})
}

// FuzzBaseTx builds a BaseTx through the constructor and proves the envelope
// round-trips through the signed-bytes prefix.
func FuzzBaseTx(f *testing.F) {
	f.Add(uint64(1), []byte{})
	f.Add(uint64(1000000), bytes.Repeat([]byte{0xff}, 32))
	testID := ids.GenerateTestID()
	f.Add(uint64(0), testID[:])

	f.Fuzz(func(t *testing.T, networkID uint64, assetData []byte) {
		require := require.New(t)

		var assetID ids.ID
		if len(assetData) >= 32 {
			copy(assetID[:], assetData[:32])
		}

		base := &lux.BaseTx{
			NetworkID:    uint32(networkID),
			BlockchainID: ids.GenerateTestID(),
			Outs:         []*lux.TransferableOutput{oneOut(assetID, 1000)},
		}
		utx, err := NewBaseTx(base)
		require.NoError(err)

		got := roundTrip(t, utx).(*BaseTx)
		require.Equal(base.NetworkID, got.NetworkID())
		require.Equal(base.BlockchainID, got.BlockchainID())
		require.Len(got.Outputs(), 1)
	})
}

// FuzzCreateChainTx builds a CreateChainTx through the constructor and proves
// its delta fields round-trip.
func FuzzCreateChainTx(f *testing.F) {
	f.Add([]byte("chainName"), []byte{}, []byte("vmID"))
	f.Add([]byte("test"), bytes.Repeat([]byte{0x01}, 100), []byte("customvm"))
	f.Add([]byte{}, []byte{}, []byte{})

	f.Fuzz(func(t *testing.T, chainName []byte, genesisData []byte, vmIDData []byte) {
		require := require.New(t)

		if len(chainName) > MaxNameLen {
			chainName = chainName[:MaxNameLen]
		}
		if len(genesisData) > 10000 {
			genesisData = genesisData[:10000]
		}

		var vmID ids.ID
		if len(vmIDData) >= 32 {
			copy(vmID[:], vmIDData[:32])
		} else {
			vmID = ids.GenerateTestID()
		}

		base := &lux.BaseTx{NetworkID: 1, BlockchainID: ids.GenerateTestID()}
		utx, err := NewCreateChainTx(
			base,
			ids.GenerateTestID(),
			string(chainName),
			vmID,
			nil,
			genesisData,
			&secp256k1fx.Input{},
		)
		require.NoError(err)

		got := roundTrip(t, utx).(*CreateChainTx)
		require.Equal(string(chainName), got.BlockchainName())
		require.Equal(vmID, got.VMID())
		if len(genesisData) == 0 {
			require.Empty(got.GenesisData())
		} else {
			require.Equal(genesisData, got.GenesisData())
		}
	})
}

// FuzzAddValidatorTx builds an AddValidatorTx through the constructor and
// proves its inline Validator + shares round-trip (no SyntacticVerify: any
// numeric values must survive the wire).
func FuzzAddValidatorTx(f *testing.F) {
	f.Add(uint64(1), uint64(100), uint64(1000), uint32(100000))
	f.Add(uint64(0), uint64(0), uint64(0), uint32(0))
	f.Add(uint64(time.Now().Unix()), uint64(time.Now().Add(time.Hour).Unix()), uint64(1000000), uint32(20000))

	f.Fuzz(func(t *testing.T, startTime, endTime, weight uint64, shares uint32) {
		require := require.New(t)

		base := &lux.BaseTx{NetworkID: 1, BlockchainID: ids.GenerateTestID()}
		validator := Validator{
			NodeID: ids.GenerateTestNodeID(),
			Start:  startTime,
			End:    endTime,
			Wght:   weight,
		}
		utx, err := NewAddValidatorTx(
			base,
			validator,
			[]*lux.TransferableOutput{oneOut(ids.GenerateTestID(), weight)},
			oneOwner(),
			shares,
		)
		require.NoError(err)

		got := roundTrip(t, utx).(*AddValidatorTx)
		require.Equal(validator, got.Validator())
		require.Equal(shares, got.DelegationShares())
	})
}

// FuzzImportExportTx builds ImportTx and ExportTx through their constructors
// and proves the cross-chain id fields round-trip.
func FuzzImportExportTx(f *testing.F) {
	f.Add([]byte{}, []byte{})
	testID1 := ids.GenerateTestID()
	testID2 := ids.GenerateTestID()
	f.Add(testID1[:], testID2[:])

	f.Fuzz(func(t *testing.T, sourceChainData, destChainData []byte) {
		require := require.New(t)

		sourceChain := ids.GenerateTestID()
		if len(sourceChainData) >= 32 {
			copy(sourceChain[:], sourceChainData[:32])
		}
		destChain := ids.GenerateTestID()
		if len(destChainData) >= 32 {
			copy(destChain[:], destChainData[:32])
		}

		importedIn := &lux.TransferableInput{
			UTXOID: lux.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
			Asset:  lux.Asset{ID: ids.GenerateTestID()},
			In:     &secp256k1fx.TransferInput{Amt: 1000, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
		}
		importTx, err := NewImportTx(
			&lux.BaseTx{NetworkID: 1, BlockchainID: destChain},
			sourceChain,
			[]*lux.TransferableInput{importedIn},
		)
		require.NoError(err)
		gotImport := roundTrip(t, importTx).(*ImportTx)
		require.Equal(sourceChain, gotImport.SourceChain())

		exportTx, err := NewExportTx(
			&lux.BaseTx{NetworkID: 1, BlockchainID: sourceChain},
			destChain,
			[]*lux.TransferableOutput{oneOut(ids.GenerateTestID(), 1000)},
		)
		require.NoError(err)
		gotExport := roundTrip(t, exportTx).(*ExportTx)
		require.Equal(destChain, gotExport.DestinationChain())
	})
}

// FuzzTransactionSignatures attaches credentials to a signed Tx and proves the
// signed bytes (unsigned‖creds) round-trip with a stable ID.
func FuzzTransactionSignatures(f *testing.F) {
	f.Add([]byte{})
	f.Add(bytes.Repeat([]byte{0x01}, 65))

	f.Fuzz(func(t *testing.T, sigData []byte) {
		require := require.New(t)

		utx, err := NewBaseTx(&lux.BaseTx{NetworkID: 1, BlockchainID: ids.GenerateTestID()})
		require.NoError(err)

		signed := &Tx{Unsigned: utx}
		if len(sigData) >= secp256k1.SignatureLen {
			cred := &secp256k1fx.Credential{}
			for i := 0; i+secp256k1.SignatureLen <= len(sigData) && len(cred.Sigs) < 10; i += secp256k1.SignatureLen {
				var sig [secp256k1.SignatureLen]byte
				copy(sig[:], sigData[i:i+secp256k1.SignatureLen])
				cred.Sigs = append(cred.Sigs, sig)
			}
			signed.Creds = []verify.Verifiable{cred}
		}
		require.NoError(signed.Initialize())

		parsed, err := Parse(signed.Bytes())
		require.NoError(err)
		require.Equal(signed.ID(), parsed.ID())
		require.Equal(signed.Creds, parsed.Creds)
	})
}

// visitor is a no-op Visitor used to prove decoded txs can be visited without
// panicking. It implements the current txs.Visitor surface (post-LP-018: no
// ConvertNetworkToL1Tx / CreateSovereignL1Tx / SlashValidatorTx).
type visitor struct{}

func (*visitor) AddValidatorTx(*AddValidatorTx) error                             { return nil }
func (*visitor) AddChainValidatorTx(*AddChainValidatorTx) error                   { return nil }
func (*visitor) AddDelegatorTx(*AddDelegatorTx) error                             { return nil }
func (*visitor) CreateNetworkTx(*CreateNetworkTx) error                           { return nil }
func (*visitor) ConvertNetworkTx(*ConvertNetworkTx) error                         { return nil }
func (*visitor) CreateChainTx(*CreateChainTx) error                               { return nil }
func (*visitor) ImportTx(*ImportTx) error                                         { return nil }
func (*visitor) ExportTx(*ExportTx) error                                         { return nil }
func (*visitor) AdvanceTimeTx(*AdvanceTimeTx) error                               { return nil }
func (*visitor) RewardValidatorTx(*RewardValidatorTx) error                       { return nil }
func (*visitor) RemoveChainValidatorTx(*RemoveChainValidatorTx) error             { return nil }
func (*visitor) TransformChainTx(*TransformChainTx) error                         { return nil }
func (*visitor) AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error { return nil }
func (*visitor) AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error { return nil }
func (*visitor) TransferChainOwnershipTx(*TransferChainOwnershipTx) error         { return nil }
func (*visitor) BaseTx(*BaseTx) error                                             { return nil }
func (*visitor) RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error               { return nil }
func (*visitor) SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error             { return nil }
func (*visitor) IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error { return nil }
func (*visitor) DisableL1ValidatorTx(*DisableL1ValidatorTx) error                 { return nil }
