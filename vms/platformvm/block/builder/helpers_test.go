// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"testing"
	"time"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/core/coremock"
	"github.com/luxfi/runtime"
	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/validators/uptime"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/p2p"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/constants"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/upgrade/upgradetest"
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
	"github.com/luxfi/node/wallet/chain/p/wallet"
	"github.com/luxfi/timer/mockable"
	"github.com/luxfi/utils"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/chains/atomic"

	blockexecutor "github.com/luxfi/node/vms/platformvm/block/executor"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	"github.com/luxfi/node/vms/platformvm/warp"
	txmempool "github.com/luxfi/node/vms/txs/mempool"

	validators "github.com/luxfi/validators"
)

const (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour
)

var testNet1 *txs.Tx

// mockValidatorState implements validators.State for testing
type mockValidatorState struct{}

func (m *mockValidatorState) GetChainID(netID ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (m *mockValidatorState) GetNetworkID(chainID ids.ID) (ids.ID, error) {
	return constants.PrimaryNetworkID, nil
}

func (m *mockValidatorState) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return nil, nil
}

func (m *mockValidatorState) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return nil, nil
}

func (m *mockValidatorState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return 100, nil
}

func (m *mockValidatorState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockValidatorState) GetWarpValidatorSets(ctx context.Context, heights []uint64, netIDs []ids.ID) (map[ids.ID]map[uint64]*validators.WarpSet, error) {
	return nil, nil
}

func (m *mockValidatorState) GetWarpValidatorSet(ctx context.Context, height uint64, netID ids.ID) (*validators.WarpSet, error) {
	return nil, nil
}

type mutableSharedMemory struct {
	atomic.SharedMemory
}

type environment struct {
	Builder
	blkManager blockexecutor.Manager
	mempool    txmempool.Mempool[*txs.Tx]
	network    *network.Network
	sender     *coremock.MockAppSender

	isBootstrapped *utils.Atomic[bool]
	config         *config.Internal
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	rt             *runtime.Runtime
	msm            *mutableSharedMemory
	fx             fx.Fx
	state          state.State
	uptimes        uptime.Calculator
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

	// Use PlatformChainID to match genesis transactions
	res.rt = consensustest.Runtime(t, constants.PlatformChainID)
	logger := log.NoLog{}
	res.rt.Log = logger
	res.msm = &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(res.rt.ChainID),
	}
	res.rt.SharedMemory = res.msm

	// Create a mock ValidatorState that implements runtime.ValidatorState
	res.rt.ValidatorState = &mockValidatorState{}

	res.rt.Lock.Lock()
	defer res.rt.Lock.Unlock()

	res.fx = defaultFx(t, res.clk, logger, res.isBootstrapped.Get())

	rewardsCalc := reward.NewCalculator(res.config.RewardConfig)
	// Convert test runtime to runtime.Runtime for state.
	stateConsensusCtx := &runtime.Runtime{
		NetworkID: res.rt.NetworkID,
		ChainID:   res.rt.ChainID,
		NodeID:    res.rt.NodeID,
		XAssetID:  res.rt.XAssetID,
		Log:       logger,
	}
	res.state = statetest.New(t, statetest.Config{
		DB:         res.baseDB,
		Genesis:    genesistest.NewBytes(t, genesistest.Config{}),
		Validators: res.config.Validators,
		Runtime:    stateConsensusCtx,
		Rewards:    rewardsCalc,
	})

	// Uptime calculator is set to NoOp in backend
	res.utxosVerifier = utxo.NewVerifier(res.clk, res.fx)

	genesisID := res.state.GetLastAccepted()
	// Convert test runtime to runtime.Runtime.
	backendConsensusCtx := &runtime.Runtime{
		NetworkID:      res.rt.NetworkID,
		ChainID:        res.rt.ChainID,
		NodeID:         res.rt.NodeID,
		XAssetID:       res.rt.XAssetID,
		Log:            logger,
		ValidatorState: res.rt.ValidatorState,
		SharedMemory:   res.rt.SharedMemory,
	}

	res.backend = txexecutor.Backend{
		Config:       res.config,
		Runtime:      backendConsensusCtx,
		Clk:          res.clk,
		Bootstrapped: res.isBootstrapped,
		Fx:           res.fx,
		FlowChecker:  res.utxosVerifier,
		Uptimes:      &uptime.NoOpCalculator{},
		Rewards:      rewardsCalc,
	}

	registerer := metric.NewRegistry()
	res.sender = &coremock.MockAppSender{
		SendGossipF: func(context.Context, p2p.SendConfig, []byte) error {
			return nil
		},
	}

	platformMetrics, err := metrics.New(registerer)
	require.NoError(err)

	res.mempool, err = mempool.New("mempool", registerer)
	require.NoError(err)

	res.blkManager = blockexecutor.NewManager(
		res.mempool,
		platformMetrics,
		res.state,
		&res.backend,
		validatorstest.Manager,
	)

	// Use validatorstest.Manager for validator state
	txVerifier := network.NewLockedTxVerifier(&res.rt.Lock, res.blkManager)

	// Tests don't need warp signing, so we pass nil
	var warpSigner warp.Signer

	res.network, err = network.New(
		logger,
		res.rt.NodeID,
		res.rt.ChainID,
		validatorstest.Manager,
		txVerifier,
		res.mempool,
		res.backend.Config.PartialSyncPrimaryNetwork,
		res.sender,
		&res.rt.Lock,
		res.state,
		warpSigner,
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
	addNet(t, res)

	t.Cleanup(func() {
		// Note: We need to be careful about the cleanup order.
		// The lock should already be released before cleanup runs.
		// State and DB should be closed only after all operations complete.
		if res.state != nil {
			_ = res.state.Close()
		}
		if res.baseDB != nil {
			_ = res.baseDB.Close()
		}
	})

	return res
}

type walletConfig struct {
	keys   []*secp256k1.PrivateKey
	netIDs []ids.ID
}

func newWallet(t testing.TB, e *environment, c walletConfig) wallet.Wallet {
	if len(c.keys) == 0 {
		c.keys = genesistest.DefaultFundedKeys
	}
	// Convert test runtime to runtime.Runtime for wallet.
	walletCtx := &runtime.Runtime{
		NetworkID:    e.rt.NetworkID,
		ChainID:      e.rt.ChainID,
		NodeID:       e.rt.NodeID,
		XAssetID:     e.rt.XAssetID,
		SharedMemory: e.rt.SharedMemory,
	}
	// Create a minimal Config for the wallet
	walletCfg := &config.Config{
		TxFee:                         constants.MilliLux,
		CreateAssetTxFee:              constants.MilliLux,
		CreateNetworkTxFee:                constants.Lux,
		CreateChainTxFee:              constants.Lux,
		AddNetworkValidatorFee: 0,
		AddNetworkDelegatorFee: 0,
	}
	return txstest.NewWallet(
		t,
		walletCtx,
		walletCfg,
		e.state,
		secp256k1fx.NewKeychain(c.keys...),
		c.netIDs,
		nil, // validationIDs
		[]ids.ID{e.rt.CChainID, e.rt.XChainID},
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

	genesisID := env.state.GetLastAccepted()
	stateDiff, err := state.NewDiff(genesisID, env.blkManager)
	require.NoError(err)

	feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
	_, _, _, err = txexecutor.StandardTx(
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

func defaultClock() *mockable.Clock {
	// set time after Banff fork (and before default nextStakerTime)
	clk := &mockable.Clock{}
	clk.Set(genesistest.DefaultValidatorStartTime)
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

func defaultFx(t *testing.T, clk *mockable.Clock, log log.Logger, isBootstrapped bool) fx.Fx {
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
