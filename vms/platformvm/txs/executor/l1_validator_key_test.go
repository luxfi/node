// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/util"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"

	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/validators/fee"
)

// ownSetState is the live diff both own-set fixtures execute against: a state
// holding no prior validators, so every registration under test is the tx's own.
func ownSetState(t *testing.T) state.Diff {
	t.Helper()

	ctrl := gomock.NewController(t)
	parent := state.NewMockState(ctrl)
	parent.EXPECT().GetTimestamp().Return(time.Unix(1000, 0)).AnyTimes()
	parent.EXPECT().GetFeeState().Return(gas.State{}).AnyTimes()
	parent.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).AnyTimes()
	parent.EXPECT().GetAccruedFees().Return(uint64(0)).AnyTimes()
	parent.EXPECT().NumActiveL1Validators().Return(0).AnyTimes()
	parent.EXPECT().GetL1Validator(gomock.Any()).Return(state.L1Validator{}, database.ErrNotFound).AnyTimes()
	parent.EXPECT().GetCurrentValidator(gomock.Any(), gomock.Any()).Return(nil, database.ErrNotFound).AnyTimes()
	parent.EXPECT().HasL1Validator(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
	parent.EXPECT().WeightOfL1Validators(gomock.Any()).Return(uint64(0), nil).AnyTimes()
	// Reached only if a caller's SyntacticVerify gate is gone and execution
	// runs on. Answering it keeps that a legible assertion failure rather than
	// an unexpected-call report about the mock.
	parent.EXPECT().GetNetOwner(gomock.Any()).Return(nil, database.ErrNotFound).AnyTimes()

	diff, err := state.NewDiffOn(parent)
	require.NoError(t, err)
	return diff
}

// ownSetFixture builds a real ConvertNetworkTx carrying [n] genesis validators,
// each with a real BLS key + proof of possession, and returns an executor whose
// state is a live diff.
//
// Nothing here stands in for the production path: the tx is built by the
// exported constructor, SyntacticVerify runs exactly as the executor runs it,
// and registerOwnSet is handed tx.Validators() — the same second decode the
// ConvertNetworkTx and CreateNetworkTx executors hand it.
func ownSetFixture(t *testing.T, n int, balance uint64) (*standardTxExecutor, *txs.ConvertNetworkTx) {
	t.Helper()
	require := require.New(t)

	rt := consensustest.Runtime(t, ids.GenerateTestID())

	vdrs := make([]*txs.NetworkValidator, n)
	for i := range vdrs {
		sk, err := localsigner.New()
		require.NoError(err)
		pop, err := signer.NewProofOfPossession(sk)
		require.NoError(err)

		nodeID := ids.GenerateTestNodeID()
		vdrs[i] = &txs.NetworkValidator{
			NodeID:  nodeID[:],
			Weight:  100,
			Balance: balance,
			Signer:  *pop,
		}
	}
	// The tx wire requires validators sorted and unique by NodeID.
	utils.Sort(vdrs)

	tx, err := txs.NewConvertNetworkTx(
		&lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID},
		ids.GenerateTestID(), // the network being promoted
		ids.GenerateTestID(), // its new parent
		ids.GenerateTestID(), // the chain hosting the manager
		security.Mode{Admission: security.Gated, Manager: security.PChain},
		nil,
		vdrs,
		&secp256k1fx.Input{SigIndices: []uint32{0}},
	)
	require.NoError(err)

	// The executor's first act. It verifies its OWN decode of the validators,
	// which is exactly why the executor's later decode is unverified.
	require.NoError(tx.SyntacticVerify(rt))

	e := &standardTxExecutor{
		state: ownSetState(t),
		backend: &Backend{
			Config: &config.Internal{
				ValidatorFeeConfig: fee.Config{Capacity: 1000},
			},
		},
	}
	return e, tx
}

// declaredByNodeID indexes the validators the tx registered.
func declaredByNodeID(t *testing.T, tx *txs.ConvertNetworkTx) map[ids.NodeID]*txs.NetworkValidator {
	t.Helper()
	out := map[ids.NodeID]*txs.NetworkValidator{}
	for _, vdr := range tx.Validators() {
		nodeID, err := ids.ToNodeID(vdr.NodeID)
		require.NoError(t, err)
		out[nodeID] = vdr
	}
	return out
}

// TestConvertNetworkStoresKeyedL1Validators is the BUG-1 gate.
//
// A converted L1's genesis validators must be stored WITH the BLS key they sign
// with. A keyless validator still counts toward the quorum denominator but can
// never contribute a vote, so an L1 born keyless cannot reach finality — it
// halts at birth.
func TestConvertNetworkStoresKeyedL1Validators(t *testing.T) {
	require := require.New(t)

	const n = 3
	e, tx := ownSetFixture(t, n, 0) // inactive: Balance 0
	netID := tx.Network()

	require.NoError(registerOwnSet(e, netID, tx.Validators(), tx.Security(), tx.ManagerChainID(), tx.ManagerAddress()))

	declared := declaredByNodeID(t, tx)
	for i := 0; i < n; i++ {
		stored, err := e.state.GetL1Validator(netID.Append(uint32(i)))
		require.NoError(err)

		// 1. The key is populated at all. l1_validator.go declares PublicKey
		//    "guaranteed to be populated"; a keyless validator breaks that
		//    invariant and is unreachable as a signer.
		require.NotEmpty(stored.PublicKey, "L1 validator %d stored KEYLESS", i)

		// 2. In the encoding the field is declared to hold — uncompressed G1
		//    (96 bytes). The 48-byte compressed form would satisfy a
		//    non-emptiness check while still reading back nil at every consumer.
		require.Len(stored.PublicKey, 2*bls.PublicKeyLen, "L1 validator %d stored in the wrong G1 encoding", i)

		// 3. It survives the reader every consumer actually uses
		//    (warp/validator.go, peer.go, service.go, l1_validator.go).
		got := bls.PublicKeyFromValidUncompressedBytes(stored.PublicKey)
		require.NotNil(got, "L1 validator %d key does not parse as uncompressed G1", i)

		// 4. It is the key the tx registered, not some other key.
		want := declared[stored.NodeID]
		require.NotNil(want, "stored a validator the tx never declared")
		require.Equal(want.Signer.PublicKey[:], bls.PublicKeyToCompressedBytes(got),
			"L1 validator %d stored a DIFFERENT key than the tx registered", i)
	}
}

// TestConvertedL1ValidatorIsSurfacedAsSigner closes the loop for an ACTIVE
// validator: the stored key must come back out of state as a usable signer key
// on the path quorum and warp read.
func TestConvertedL1ValidatorIsSurfacedAsSigner(t *testing.T) {
	require := require.New(t)

	e, tx := ownSetFixture(t, 1, 1_000_000) // active: non-zero Balance
	netID := tx.Network()
	require.NoError(registerOwnSet(e, netID, tx.Validators(), tx.Security(), tx.ManagerChainID(), tx.ManagerAddress()))

	stored, err := e.state.GetL1Validator(netID.Append(0))
	require.NoError(err)
	require.True(stored.IsActive(), "fixture should have produced an active validator")

	pk := bls.PublicKeyFromValidUncompressedBytes(stored.PublicKey)
	require.NotNil(pk, "converted L1 validator is surfaced KEYLESS to quorum/warp")

	declared := tx.Validators()[0]
	require.Equal(declared.Signer.PublicKey[:], bls.PublicKeyToCompressedBytes(pk))
}

// TestConvertNetworkKeyMatchesConversionRecord pins the two writes in
// registerOwnSet to the SAME key. The stored validator and the conversion
// record hold different ENCODINGS of one key (uncompressed for state,
// compressed for the warp conversion record); they must never hold different
// KEYS.
func TestConvertNetworkKeyMatchesConversionRecord(t *testing.T) {
	require := require.New(t)

	e, tx := ownSetFixture(t, 2, 0)
	netID := tx.Network()
	require.NoError(registerOwnSet(e, netID, tx.Validators(), tx.Security(), tx.ManagerChainID(), tx.ManagerAddress()))

	declared := declaredByNodeID(t, tx)
	for i := 0; i < 2; i++ {
		stored, err := e.state.GetL1Validator(netID.Append(uint32(i)))
		require.NoError(err)

		pk := bls.PublicKeyFromValidUncompressedBytes(stored.PublicKey)
		require.NotNil(pk)

		// The conversion record stores the compressed form of the same key.
		record := declared[stored.NodeID].Signer.PublicKey
		require.Equal(record[:], bls.PublicKeyToCompressedBytes(pk),
			"stored key and conversion-record key disagree for validator %d", i)
	}
}

// TestCreateNetworkSovereignStoresKeyedL1Validators covers the OTHER caller of
// registerOwnSet. A sovereign network born from CreateNetworkTx seeds its own
// set through the same primitive and from the same kind of second decode, so it
// carried the identical keyless defect.
func TestCreateNetworkSovereignStoresKeyedL1Validators(t *testing.T) {
	require := require.New(t)

	rt := consensustest.Runtime(t, ids.GenerateTestID())

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)
	nodeID := ids.GenerateTestNodeID()

	tx, err := txs.NewCreateNetworkTx(
		&lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID},
		ids.GenerateTestID(),
		&secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
		security.Mode{Admission: security.Gated, Manager: security.PChain},
		[]*txs.NetworkValidator{{
			NodeID: nodeID[:],
			Weight: 100,
			Signer: *pop,
		}},
		ids.GenerateTestID(),
		nil,
	)
	require.NoError(err)
	require.NoError(tx.SyntacticVerify(rt))
	require.True(tx.Security().Sovereign())

	ctrl := gomock.NewController(t)
	parent := state.NewMockState(ctrl)
	parent.EXPECT().GetTimestamp().Return(time.Unix(1000, 0)).AnyTimes()
	parent.EXPECT().GetFeeState().Return(gas.State{}).AnyTimes()
	parent.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).AnyTimes()
	parent.EXPECT().GetAccruedFees().Return(uint64(0)).AnyTimes()
	parent.EXPECT().NumActiveL1Validators().Return(0).AnyTimes()
	parent.EXPECT().GetL1Validator(gomock.Any()).Return(state.L1Validator{}, database.ErrNotFound).AnyTimes()
	parent.EXPECT().GetCurrentValidator(gomock.Any(), gomock.Any()).Return(nil, database.ErrNotFound).AnyTimes()
	parent.EXPECT().HasL1Validator(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
	parent.EXPECT().WeightOfL1Validators(gomock.Any()).Return(uint64(0), nil).AnyTimes()

	diff, err := state.NewDiffOn(parent)
	require.NoError(err)
	e := &standardTxExecutor{
		state:   diff,
		backend: &Backend{Config: &config.Internal{ValidatorFeeConfig: fee.Config{Capacity: 1000}}},
	}

	// The CreateNetworkTx executor keys the set under the tx's own id.
	netID := ids.GenerateTestID()
	require.NoError(registerOwnSet(e, netID, tx.Validators(), tx.Security(), tx.ManagerChainID(), tx.ManagerAddress()))

	stored, err := e.state.GetL1Validator(netID.Append(0))
	require.NoError(err)
	require.Len(stored.PublicKey, 2*bls.PublicKeyLen, "sovereign CreateNetworkTx stored the wrong G1 encoding")

	pk := bls.PublicKeyFromValidUncompressedBytes(stored.PublicKey)
	require.NotNil(pk, "sovereign CreateNetworkTx stored a KEYLESS validator")
	require.Equal(pop.PublicKey[:], bls.PublicKeyToCompressedBytes(pk))
}
