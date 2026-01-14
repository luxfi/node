// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/runtime"
	validators "github.com/luxfi/validators"
	consensusuptime "github.com/luxfi/validators/uptime"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/utils"
	"github.com/luxfi/vm/chains/atomic"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/timer/mockable"

	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/reward"

	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/state/statetest"
	"github.com/luxfi/node/vms/platformvm/status"

	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/txstest"
	"github.com/luxfi/node/vms/platformvm/utxo"

	"github.com/luxfi/node/wallet/chain/p/wallet"
	"github.com/luxfi/utxo/secp256k1fx"
)

const (
	defaultMinValidatorStake = 5 * constants.MilliLux

	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour

	defaultTxFee = 100 * constants.NanoLux
)

var (
	lastAcceptedID = ids.GenerateTestID()

	testNet1 *txs.Tx
)

type mutableSharedMemory struct {
	atomic.SharedMemory
}

type environment struct {
	isBootstrapped *utils.Atomic[bool]
	config         *config.Internal
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	rt             *runtime.Runtime
	msm            *mutableSharedMemory
	state          state.State
	states         map[ids.ID]state.Chain
	uptimes        consensusuptime.Calculator
	backend        Backend
}

func (e *environment) GetState(blkID ids.ID) (state.Chain, bool) {
	if blkID == lastAcceptedID {
		return e.state, true
	}
	chainState, ok := e.states[blkID]
	return chainState, ok
}

func (e *environment) SetState(blkID ids.ID, chainState state.Chain) {
	e.states[blkID] = chainState
}

func newEnvironment(t *testing.T, f upgradetest.Fork) *environment {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	config := defaultConfig(f)
	clk := defaultClock(f)

	baseDB := versiondb.New(memdb.New())
	// Use the same fixed X-chain asset ID as genesis for consistency
	xAssetID := genesistest.XAssetID

	logger := log.NoLog{}
	rt := &runtime.Runtime{
		NetworkID: constants.UnitTestID,
		ChainID:   constants.PlatformChainID,
		XChainID:  ids.GenerateTestID(), // Set a test X-Chain ID
		CChainID:  ids.GenerateTestID(), // Set a test C-Chain ID
		XAssetID:  xAssetID,
		Log:       logger,
	}
	m := atomic.NewMemory(baseDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(rt.ChainID),
	}
	rt.SharedMemory = msm

	fx := defaultFx(clk, logger, isBootstrapped.Get())

	// Initialize utxo.XAssetID from runtime
	utxo.XAssetID = rt.XAssetID

	rewards := reward.NewCalculator(config.RewardConfig)
	baseState := statetest.New(t, statetest.Config{
		DB:         baseDB,
		Genesis:    genesistest.NewBytes(t, genesistest.Config{}),
		Validators: config.Validators,
		Upgrades:   config.UpgradeConfig,
		Runtime:    rt,
		Rewards:    rewards,
	})
	lastAcceptedID = baseState.GetLastAccepted()

	uptimes := consensusuptime.NoOpCalculator{}
	utxosHandler := utxo.NewHandler(context.Background(), &mockable.Clock{}, fx)

	backend := Backend{
		Config:       config,
		Runtime:      rt,
		Clk:          &mockable.Clock{},
		Bootstrapped: &isBootstrapped,
		Fx:           fx,
		FlowChecker:  utxosHandler,
		Uptimes:      uptimes,
		Rewards:      rewards,
	}

	env := &environment{
		isBootstrapped: &isBootstrapped,
		config:         config,
		clk:            clk,
		baseDB:         baseDB,
		rt:             rt,
		msm:            msm,
		state:          baseState,
		states:         make(map[ids.ID]state.Chain),
		uptimes:        uptimes,
		backend:        backend,
	}

	addNet(t, env)

	t.Cleanup(func() {
		env.rt.Lock.Lock()
		defer env.rt.Lock.Unlock()

		require := require.New(t)

		if env.isBootstrapped.Get() {
			// NoOpCalculator doesn't track validators, so nothing to stop
			// if env.uptimes.StartedTracking() {
			// 	validatorIDs := env.config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
			// 	require.NoError(env.uptimes.StopTracking(validatorIDs))
			// }

			env.state.SetHeight(math.MaxUint64)
			require.NoError(env.state.Commit())
		}

		require.NoError(env.state.Close())
		require.NoError(env.baseDB.Close())
	})

	return env
}

// walletConfig configures test wallet creation.
//
// Field naming follows txstest.NewWallet parameters:
//   - ownedNetworkIDs: Network IDs (L1 validator sets) the keychain owns (for GetNetOwner lookups)
//   - importSourceChainIDs: Chain IDs to check for importable atomic UTXOs
type walletConfig struct {
	config               *config.Internal
	keys                 []*secp256k1.PrivateKey
	ownedNetworkIDs      []ids.ID // networks (L1s) we own (for GetNetOwner)
	importSourceChainIDs []ids.ID // chains to import atomic UTXOs from
}

func newWallet(t testing.TB, e *environment, c walletConfig) wallet.Wallet {
	if c.config == nil {
		c.config = e.config
	}
	if len(c.keys) == 0 {
		c.keys = genesistest.DefaultFundedKeys
	}
	// Convert test runtime to runtime.Runtime.
	rt := &runtime.Runtime{
		NetworkID: e.rt.NetworkID,

		ChainID:        e.rt.ChainID,
		NodeID:         e.rt.NodeID,
		PublicKey:      []byte{}, // Use empty bytes for test
		XChainID:       e.rt.XChainID,
		CChainID:       e.rt.CChainID,
		XAssetID:       e.rt.XAssetID,
		ValidatorState: e.rt.ValidatorState,
		SharedMemory:   e.rt.SharedMemory,
		ChainDataDir:   e.rt.ChainDataDir,
		Log:            e.rt.Log,
		Lock:           sync.RWMutex{}, // Create new RWMutex
		Keystore:       nil,            // No keystore needed for test
		WarpSigner:     e.rt.WarpSigner,
	}
	// Create a basic Config for wallet
	walletConfig := &config.Config{
		TxFee:            constants.MilliLux,
		CreateAssetTxFee: constants.MilliLux,
		CreateNetTxFee:   constants.Lux,
		CreateChainTxFee: constants.Lux,
	}
	return txstest.NewWallet(
		t,
		rt,
		walletConfig,
		e.state,
		secp256k1fx.NewKeychain(c.keys...),
		c.ownedNetworkIDs,      // networks (L1s) we own for GetNetOwner lookups
		nil,                    // validationIDs
		c.importSourceChainIDs, // chains to import atomic UTXOs from
	)
}

func addNet(t *testing.T, env *environment) {
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: genesistest.DefaultFundedKeys[:1],
	})

	var err error
	testNet1, err = wallet.IssueCreateNetworkTx(
		&secp256k1fx.OutputOwners{
			Threshold: 2,
			Addrs: []ids.ShortID{
				genesistest.DefaultFundedKeys[0].Address(),
				genesistest.DefaultFundedKeys[1].Address(),
				genesistest.DefaultFundedKeys[2].Address(),
			},
		},
	)
	require.NoError(err)

	stateDiff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(
		&env.backend,
		feeCalculator,
		testNet1,
		stateDiff,
	)
	require.NoError(err)

	stateDiff.AddTx(testNet1, status.Committed)
	require.NoError(stateDiff.Apply(env.state))
	require.NoError(env.state.Commit())
}

func defaultConfig(f upgradetest.Fork) *config.Internal {
	upgrades := upgradetest.GetConfigWithUpgradeTime(
		f,
		genesistest.DefaultValidatorStartTime.Add(-2*time.Second),
	)
	upgradetest.SetTimesTo(
		&upgrades,
		min(f, upgradetest.ApricotPhase5),
		genesistest.DefaultValidatorEndTime,
	)

	return &config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: consensusuptime.NewLockedCalculator(),
		Validators:             validators.NewManager(),
		TrackedChains:          set.Of(constants.PrimaryNetworkID),
		MinValidatorStake:      5 * constants.MilliLux,
		MaxValidatorStake:      500 * constants.MilliLux,
		MinDelegatorStake:      1 * constants.MilliLux,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig: reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .10 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * constants.MegaLux,
		},
		UpgradeConfig: upgrades,
	}
}

func defaultClock(f upgradetest.Fork) *mockable.Clock {
	now := genesistest.DefaultValidatorStartTime
	if f >= upgradetest.Banff {
		// 1 second after active fork
		now = genesistest.DefaultValidatorEndTime.Add(-2 * time.Second)
	}
	clk := &mockable.Clock{}
	clk.Set(now)
	return clk
}

type fxVMInt struct {
	registry codec.Registry
	clk      *mockable.Clock
	log      log.Logger
}

func (fvi *fxVMInt) CodecRegistry() codec.Registry {
	return fvi.registry
}

func (fvi *fxVMInt) Clock() *mockable.Clock {
	return fvi.clk
}

func (fvi *fxVMInt) Logger() log.Logger {
	return fvi.log
}

func defaultFx(clk *mockable.Clock, log log.Logger, isBootstrapped bool) fx.Fx {
	fxVMInt := &fxVMInt{
		registry: linearcodec.NewDefault(),
		clk:      clk,
		log:      log,
	}
	res := &secp256k1fx.Fx{}
	if err := res.Initialize(fxVMInt); err != nil {
		panic(err)
	}
	if isBootstrapped {
		if err := res.Bootstrapped(); err != nil {
			panic(err)
		}
	}
	return res
}
