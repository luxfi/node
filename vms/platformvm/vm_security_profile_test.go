// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	consensusconfig "github.com/luxfi/consensus/config"
	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/txs/auth"
	"github.com/luxfi/utxo/secp256k1fx"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
	"github.com/luxfi/vm"
	"github.com/luxfi/vm/chains/atomic"
)

// TestVMInitialize_WiresSecurityProfileIntoMempool proves the end-to-end
// F102 close-out wiring: the SecurityProfile pinned on
// platformvm.config.Internal propagates through vm.Initialize and lands
// on the mempool's SetAuthPolicy gate, where it refuses a tx whose
// credentials carry a classical secp256k1 entry.
//
// This is the integration counterpart to the pure-mempool unit test in
// vms/platformvm/txs/mempool/auth_test.go: that test pokes
// SetAuthPolicy directly; this one proves the chain construction path
// (genesis → node.SecurityProfile → Internal.SecurityProfile →
// vm.Initialize → mempool.SetAuthPolicy) is end-to-end correct.
func TestVMInitialize_WiresSecurityProfileIntoMempool(t *testing.T) {
	require := require.New(t)

	// StrictPQ pins RequireTypedTxAuth=true; the mempool gate refuses
	// any unwrapped classical secp256k1.Credential without an
	// allow-listed originator.
	strictPQ := consensusconfig.StrictPQ() // *ChainSecurityProfile

	vmImpl := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		DynamicFeeConfig:       defaultDynamicFeeConfig,
		ValidatorFeeConfig:     defaultValidatorFeeConfig,
		MinValidatorStake:      defaultMinValidatorStake,
		MaxValidatorStake:      defaultMaxValidatorStake,
		MinDelegatorStake:      defaultMinDelegatorStake,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(
			upgradetest.Latest,
			genesistest.DefaultValidatorStartTime,
		),
		// F102 close-out: pin the strict-PQ profile. The mempool builder
		// installs this via SetAuthPolicy; classical credentials are
		// refused at gossip time.
		SecurityProfile:         strictPQ,
		ClassicalCompatRegistry: nil,
	}}

	db := memdb.New()
	chainDB := prefixdb.New([]byte{0}, db)
	atomicDB := prefixdb.New([]byte{1}, db)

	vmImpl.Clock().Set(genesistest.DefaultValidatorStartTime)
	rt := consensustest.Runtime(t, consensustest.PChainID)

	m := atomic.NewMemory(atomicDB)
	rt.SharedMemory = m.NewSharedMemory(rt.ChainID)
	rt.ValidatorState = &mockValidatorState{}

	rt.Lock.Lock()
	defer rt.Lock.Unlock()

	require.NoError(vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime: rt,
			DB:      chainDB,
			Genesis: genesistest.NewBytes(t, genesistest.Config{
				InitialBalance: 200*1_000_000_000 + 20_000,
			}),
			Upgrade:  nil,
			Config:   []byte(`{"network":{"max-validator-set-staleness":0}}`),
			ToEngine: make(chan vm.Message, 1),
			Fx:       nil,
			Sender:   &TestSender{},
		},
	))
	t.Cleanup(func() {
		_ = vmImpl.Shutdown(context.Background())
	})

	// Construct a P-chain BaseTx with one classical credential. The
	// mempool gate refuses unwrapped *secp256k1fx.Credential under
	// RequireTypedTxAuth=true, ids.ShortEmpty originator, nil registry.
	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{},
		Creds:    []verify.Verifiable{&secp256k1fx.Credential{}},
	}

	err := vmImpl.Builder.Add(tx)
	require.True(
		errors.Is(err, auth.ErrLegacyCredentialUnderStrictPQ),
		"vm.Builder.Add: got %v, want wrap of ErrLegacyCredentialUnderStrictPQ", err,
	)
}

// TestVMInitialize_NoSecurityProfile_AdmitsClassicalCredentials proves
// the wiring is backwards-compatible: a node booted without a
// SecurityProfile pin (legacy/classical-compat networks) keeps admitting
// classical credentials. The chain-builder MUST NOT regress on the
// existing behaviour for pre-locked-profile networks.
func TestVMInitialize_NoSecurityProfile_AdmitsClassicalCredentials(t *testing.T) {
	require := require.New(t)

	vmImpl := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		DynamicFeeConfig:       defaultDynamicFeeConfig,
		ValidatorFeeConfig:     defaultValidatorFeeConfig,
		MinValidatorStake:      defaultMinValidatorStake,
		MaxValidatorStake:      defaultMaxValidatorStake,
		MinDelegatorStake:      defaultMinDelegatorStake,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig: upgradetest.GetConfigWithUpgradeTime(
			upgradetest.Latest,
			genesistest.DefaultValidatorStartTime,
		),
		// SecurityProfile intentionally left nil — legacy path.
		SecurityProfile: nil,
	}}

	db := memdb.New()
	chainDB := prefixdb.New([]byte{0}, db)
	atomicDB := prefixdb.New([]byte{1}, db)

	vmImpl.Clock().Set(genesistest.DefaultValidatorStartTime)
	rt := consensustest.Runtime(t, consensustest.PChainID)
	rt.SharedMemory = atomic.NewMemory(atomicDB).NewSharedMemory(rt.ChainID)
	rt.ValidatorState = &mockValidatorState{}

	rt.Lock.Lock()
	defer rt.Lock.Unlock()

	require.NoError(vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime: rt,
			DB:      chainDB,
			Genesis: genesistest.NewBytes(t, genesistest.Config{
				InitialBalance: 200*1_000_000_000 + 20_000,
			}),
			Config:   []byte(`{"network":{"max-validator-set-staleness":0}}`),
			ToEngine: make(chan vm.Message, 1),
			Sender:   &TestSender{},
		},
	))
	t.Cleanup(func() {
		_ = vmImpl.Shutdown(context.Background())
	})

	// Same shape as the strict-PQ test; under a nil profile the gate is
	// a no-op and the BaseTx (which has no inputs to fail on) is
	// admitted by the underlying mempool.
	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{},
		Creds:    []verify.Verifiable{&secp256k1fx.Credential{}},
	}
	require.NoError(vmImpl.Builder.Add(tx))
}
