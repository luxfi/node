// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	consensuscontext "github.com/luxfi/consensus/context"
	validators "github.com/luxfi/consensus/validator"
	consensusuptime "github.com/luxfi/consensus/validator/uptime"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/config"

	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/reward"

	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/state/statetest"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/testcontext"

	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/txstest"
	"github.com/luxfi/node/vms/platformvm/utxo"

	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/chain/p/wallet"
)

const (
	defaultMinValidatorStake = 5 * units.MilliLux

	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour

	defaultTxFee = 100 * units.NanoLux
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
	ctx            *testcontext.Context
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
	luxAssetID := ids.GenerateTestID()

	ctx := testcontext.New(context.Background())
	ctx.ChainID = constants.PlatformChainID
	ctx.XAssetID = luxAssetID
	ctx.NetworkID = constants.UnitTestID
	m := atomic.NewMemory(baseDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	fx := defaultFx(clk, ctx.Log, isBootstrapped.Get())

	// Convert testcontext.Context to consensus.Context for statetest
	consensusCtx := &consensuscontext.Context{
		NetworkID:      ctx.NetworkID,
		NetID:          ctx.NetID,
		ChainID:        ctx.ChainID,
		NodeID:         ctx.NodeID,
		PublicKey:      []byte{}, // Use empty bytes for test
		XChainID:       ctx.XChainID,
		CChainID:       ctx.CChainID,
		LUXAssetID:     ctx.LUXAssetID,
		ValidatorState: ctx.ValidatorState,
		SharedMemory:   ctx.SharedMemory,
		ChainDataDir:   ctx.ChainDataDir,
		Log:            ctx.Log,
		Lock:           sync.RWMutex{}, // Create new RWMutex
		Keystore:       nil,             // No keystore needed for test
		WarpSigner:     ctx.WarpSigner,
	}

	rewards := reward.NewCalculator(config.RewardConfig)
	baseState := statetest.New(t, statetest.Config{
		DB:         baseDB,
		Genesis:    genesistest.NewBytes(t, genesistest.Config{}),
		Validators: config.Validators,
		Upgrades:   config.UpgradeConfig,
		Context:    consensusCtx,
		Rewards:    rewards,
	})
	lastAcceptedID = baseState.GetLastAccepted()

	uptimes := consensusuptime.NoOpCalculator{}
	utxosHandler := utxo.NewHandler(ctx.Context, &mockable.Clock{}, fx)

	backend := Backend{
		Config:       config,
		Ctx:          consensusCtx,
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
		ctx:            ctx,
		msm:            msm,
		state:          baseState,
		states:         make(map[ids.ID]state.Chain),
		uptimes:        uptimes,
		backend:        backend,
	}

	addNet(t, env)

	t.Cleanup(func() {
		env.ctx.Lock.Lock()
		defer env.ctx.Lock.Unlock()

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

type walletConfig struct {
	config    *config.Internal
	keys      []*secp256k1.PrivateKey
	subnetIDs []ids.ID
	chainIDs  []ids.ID
}

func newWallet(t testing.TB, e *environment, c walletConfig) wallet.Wallet {
	if c.config == nil {
		c.config = e.config
	}
	if len(c.keys) == 0 {
		c.keys = genesistest.DefaultFundedKeys
	}
	// Convert testcontext.Context to consensus.Context
	consensusCtx := &consensuscontext.Context{
		NetworkID:      e.ctx.NetworkID,
		NetID:          e.ctx.NetID,
		ChainID:        e.ctx.ChainID,
		NodeID:         e.ctx.NodeID,
		PublicKey:      []byte{}, // Use empty bytes for test
		XChainID:       e.ctx.XChainID,
		CChainID:       e.ctx.CChainID,
		LUXAssetID:     e.ctx.LUXAssetID,
		ValidatorState: e.ctx.ValidatorState,
		SharedMemory:   e.ctx.SharedMemory,
		ChainDataDir:   e.ctx.ChainDataDir,
		Log:            e.ctx.Log,
		Lock:           sync.RWMutex{}, // Create new RWMutex
		Keystore:       nil,             // No keystore needed for test
		WarpSigner:     e.ctx.WarpSigner,
	}
	// Create a basic Config for wallet
	walletConfig := &config.Config{
		TxFee: units.MilliLux,
		CreateAssetTxFee: units.MilliLux,
		CreateNetTxFee: units.Lux,
		CreateBlockchainTxFee: units.Lux,
	}
	return txstest.NewWallet(
		t,
		consensusCtx,
		walletConfig,
		e.state,
		secp256k1fx.NewKeychain(c.keys...),
		c.subnetIDs,
		nil, // validationIDs
		c.chainIDs,
	)
}

func addNet(t *testing.T, env *environment) {
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: genesistest.DefaultFundedKeys[:1],
	})

	var err error
	testNet1, err = wallet.IssueCreateNetTx(
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
		MinValidatorStake:      5 * units.MilliLux,
		MaxValidatorStake:      500 * units.MilliLux,
		MinDelegatorStake:      1 * units.MilliLux,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig: reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .10 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaLux,
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
