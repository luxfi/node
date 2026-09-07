// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"github.com/go-json-experiment/json"
	jsonv1 "github.com/go-json-experiment/json/v1"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/constants"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/txs/auth"
	xvmfx "github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/runtime"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm"
	"github.com/luxfi/vm/chains/atomic"
)

// TestXVMInitialize_WiresSecurityProfileIntoMempool proves the F102
// close-out wiring on the X-chain: the SecurityProfile carried on
// xvm.Factory propagates through vm.Initialize → mempool.SetAuthPolicy.
//
// Strict-PQ chains refuse classical secp256k1 credentials at gossip
// time; the X-chain test exercises the unwrap path because X-chain
// credentials are wrapped in fxs.FxCredential before being checked by
// the auth policy gate.
//
// The test issues the tx via IssueTxFromRPCWithoutVerification, which
// skips block-state verification but still runs the mempool's
// SetAuthPolicy gate — that gate is the F102 close-out being verified
// here.
func TestXVMInitialize_WiresSecurityProfileIntoMempool(t *testing.T) {
	require := require.New(t)

	strictPQ := consensusconfig.StrictPQ()
	vmImpl := &VM{
		securityProfile:         strictPQ,
		classicalCompatRegistry: nil,
	}

	chainID := ids.GenerateTestID()
	rt := &runtime.Runtime{
		NetworkID:      constants.UnitTestID,
		ChainID:        chainID,
		XChainID:       ids.GenerateTestID(),
		CChainID:       ids.GenerateTestID(),
		NodeID:         ids.GenerateTestNodeID(),
		ValidatorState: &mockValidatorState{chainID: chainID},
	}

	baseDB := memdb.New()
	sharedMemory := atomic.NewMemory(memdb.New())
	vmImpl.SharedMemory = &testSharedMemory{mem: sharedMemory.NewSharedMemory(rt.ChainID)}

	testLock := &sync.Mutex{}
	testLock.Lock()

	genesisBytes := newGenesisBytesTest(t)
	fxs := []interface{}{
		&vm.Fx{ID: secp256k1fx.ID, Fx: &secp256k1fx.Fx{}},
		&vm.Fx{ID: nftfx.ID, Fx: &nftfx.Fx{}},
		&vm.Fx{ID: propertyfx.ID, Fx: &propertyfx.Fx{}},
	}

	configBytes, err := json.Marshal(DefaultConfig, jsonv1.FormatDurationAsNano(true))
	require.NoError(err)

	require.NoError(vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime:  rt,
			DB:       baseDB,
			Genesis:  genesisBytes,
			Upgrade:  nil,
			Config:   configBytes,
			ToEngine: make(chan vm.Message, 1),
			Fx:       fxs,
			Sender:   &noOpSender{},
		},
	))
	t.Cleanup(func() { _ = vmImpl.Shutdown(context.Background()) })

	// Linearize so the mempool builder is constructed and SetAuthPolicy
	// has fired with the strict-PQ profile.
	stopVertexID := getCreateTxFromGenesisTest(t, genesisBytes, "LUX").ID()
	require.NoError(vmImpl.Linearize(
		context.Background(),
		stopVertexID,
		make(chan vm.Message, 1),
	))

	// Construct an X-chain tx whose Creds carry one FxCredential
	// wrapping a classical *secp256k1fx.Credential. The mempool gate
	// unwraps the FxCredential before consulting the auth policy.
	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{},
		Creds: []*xvmfx.FxCredential{
			{Credential: &secp256k1fx.Credential{}},
		},
	}

	// IssueTxFromRPCWithoutVerification reaches the mempool's
	// AuthPolicy gate without going through block-state verification.
	// The network is unexported on *VM; tests in-package access it
	// directly to assert end-to-end wiring.
	err = vmImpl.network.IssueTxFromRPCWithoutVerification(tx)
	require.True(
		errors.Is(err, auth.ErrLegacyCredentialUnderStrictPQ),
		"IssueTxFromRPCWithoutVerification: got %v, want wrap of ErrLegacyCredentialUnderStrictPQ", err,
	)
}
