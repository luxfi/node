// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
	"github.com/luxfi/node/database/memdb"
	"github.com/luxfi/node/database/prefixdb"
	"github.com/luxfi/node/database/versiondb"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/engine"
	"github.com/luxfi/node/consensus/engine/enginetest"
	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/uptime"
	"github.com/luxfi/node/consensus/validators"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/network"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/state/statetest"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/mempool"
	"github.com/luxfi/node/vms/platformvm/txs/txstest"
	"github.com/luxfi/node/vms/platformvm/utxo"
	"github.com/luxfi/node/vms/platformvm/validators/validatorstest"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/chain/p/wallet"

	blockexecutor "github.com/luxfi/node/vms/platformvm/block/executor"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	txmempool "github.com/luxfi/node/vms/txs/mempool"
)

const (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour
)

var testSubnet1 *txs.Tx

type mutableSharedMemory struct {
	atomic.SharedMemory
}

type environment struct {
	Builder
	blkManager blockexecutor.Manager
	mempool    txmempool.Mempool[*txs.Tx]
	network    *network.Network
	sender     *enginetest.Sender

	isBootstrapped *utils.Atomic[bool]
	config         *config.Internal
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *consensus.Context
	msm            *mutableSharedMemory
	fx             fx.Fx
	state          state.State
	uptimes        uptime.Manager
	utxosVerifier  utxo.Verifier
	backend        txexecutor.Backend
}

func newEnvironment(t *testing.T, f upgradetest.Fork) *environment { //nolint:unparam
	require := require.New(t)

	res := &environment{
		isBootstrapped: &utils.Atomic[bool]{},
		config:         defaultConfig(f),
		clk:            defaultClock(),
	}
	res.isBootstrapped.Set(true)

	res.baseDB = versiondb.New(memdb.New())
	atomicDB := prefixdb.New([]byte{1}, res.baseDB)
	m := atomic.NewMemory(atomicDB)

	res.ctx = snowtest.Context(t, snowtest.PChainID)
	res.msm = &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(res.ctx.ChainID),
	}
	res.ctx.SharedMemory = res.msm

	res.ctx.Lock.Lock()
	defer res.ctx.Lock.Unlock()

	res.fx = defaultFx(t, res.clk, res.ctx.Log, res.isBootstrapped.Get())

	rewardsCalc := reward.NewCalculator(res.config.RewardConfig)
	res.state = statetest.New(t, statetest.Config{
		DB:         res.baseDB,
		Genesis:    genesistest.NewBytes(t, genesistest.Config{}),
		Validators: res.config.Validators,
		Context:    res.ctx,
		Rewards:    rewardsCalc,
	})

	res.uptimes = uptime.NewManager(res.state, res.clk)
	res.utxosVerifier = utxo.NewVerifier(res.ctx, res.clk, res.fx)

	genesisID := res.state.GetLastAccepted()
	res.backend = txexecutor.Backend{
		Config:       res.config,
		Ctx:          res.ctx,
		Clk:          res.clk,
		Bootstrapped: res.isBootstrapped,
		Fx:           res.fx,
		FlowChecker:  res.utxosVerifier,
		Uptimes:      res.uptimes,
		Rewards:      rewardsCalc,
	}

	registerer := prometheus.NewRegistry()
	res.sender = &enginetest.Sender{T: t}
	res.sender.SendAppGossipF = func(context.Context, engine.SendConfig, []byte) error {
		return nil
	}

	metrics, err := metrics.New(registerer)
	require.NoError(err)

	res.mempool, err = mempool.New("mempool", registerer)
	require.NoError(err)

	res.blkManager = blockexecutor.NewManager(
		res.mempool,
		metrics,
		res.state,
		&res.backend,
		validatorstest.Manager,
	)

	txVerifier := network.NewLockedTxVerifier(&res.ctx.Lock, res.blkManager)
	res.network, err = network.New(
		res.backend.Ctx.Log,
		res.backend.Ctx.NodeID,
		res.backend.Ctx.SubnetID,
		res.backend.Ctx.ValidatorState,
		txVerifier,
		res.mempool,
		res.backend.Config.PartialSyncPrimaryNetwork,
		res.sender,
		&res.ctx.Lock,
		res.state,
		res.ctx.WarpSigner,
		registerer,
		config.DefaultNetwork,
	)
	require.NoError(err)

	res.Builder = New(
		res.mempool,
		&res.backend,
		res.blkManager,
	)

	res.blkManager.SetPreference(genesisID)
	addSubnet(t, res)

	t.Cleanup(func() {
		res.ctx.Lock.Lock()
		defer res.ctx.Lock.Unlock()

		if res.uptimes.StartedTracking() {
			validatorIDs := res.config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)

			require.NoError(res.uptimes.StopTracking(validatorIDs))

			require.NoError(res.state.Commit())
		}

		require.NoError(res.state.Close())
		require.NoError(res.baseDB.Close())
	})

	return res
}

type walletConfig struct {
	keys      []*secp256k1.PrivateKey
	subnetIDs []ids.ID
}

func newWallet(t testing.TB, e *environment, c walletConfig) wallet.Wallet {
	if len(c.keys) == 0 {
		c.keys = genesistest.DefaultFundedKeys
	}
	return txstest.NewWallet(
		t,
		e.ctx,
		e.config,
		e.state,
		secp256k1fx.NewKeychain(c.keys...),
		c.subnetIDs,
		nil, // validationIDs
		[]ids.ID{e.ctx.CChainID, e.ctx.XChainID},
	)
}

func addSubnet(t *testing.T, env *environment) {
	require := require.New(t)

	wallet := newWallet(t, env, walletConfig{
		keys: genesistest.DefaultFundedKeys[:1],
	})

	var err error
	testSubnet1, err = wallet.IssueCreateSubnetTx(
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

	genesisID := env.state.GetLastAccepted()
	stateDiff, err := state.NewDiff(genesisID, env.blkManager)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
	_, _, _, err = txexecutor.StandardTx(
		&env.backend,
		feeCalculator,
		testSubnet1,
		stateDiff,
	)
	require.NoError(err)

	stateDiff.AddTx(testSubnet1, status.Committed)
	require.NoError(stateDiff.Apply(env.state))
	require.NoError(env.state.Commit())
}

func defaultConfig(f upgradetest.Fork) *config.Internal {
	upgrades := upgradetest.GetConfigWithUpgradeTime(f, time.Time{})
	// This package neglects fork ordering
	upgradetest.SetTimesTo(
		&upgrades,
		min(f, upgradetest.ApricotPhase5),
		genesistest.DefaultValidatorEndTime,
	)

	return &config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
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

func defaultClock() *mockable.Clock {
	// set time after Banff fork (and before default nextStakerTime)
	clk := &mockable.Clock{}
	clk.Set(genesistest.DefaultValidatorStartTime)
	return clk
}

type fxVMInt struct {
	registry codec.Registry
	clk      *mockable.Clock
	log      logging.Logger
}

func (fvi *fxVMInt) CodecRegistry() codec.Registry {
	return fvi.registry
}

func (fvi *fxVMInt) Clock() *mockable.Clock {
	return fvi.clk
}

func (fvi *fxVMInt) Logger() logging.Logger {
	return fvi.log
}

func defaultFx(t *testing.T, clk *mockable.Clock, log logging.Logger, isBootstrapped bool) fx.Fx {
	require := require.New(t)

	fxVMInt := &fxVMInt{
		registry: linearcodec.NewDefault(),
		clk:      clk,
		log:      log,
	}
	res := &secp256k1fx.Fx{}
	require.NoError(res.Initialize(fxVMInt))
	if isBootstrapped {
		require.NoError(res.Bootstrapped())
	}
	return res
}
