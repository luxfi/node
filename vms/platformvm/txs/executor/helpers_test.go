// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"
	
	"github.com/luxfi/metrics"

	"github.com/stretchr/testify/require"


	"github.com/luxfi/node/chains"

	"github.com/luxfi/node/chains/atomic"

	"github.com/luxfi/node/codec"

	"github.com/luxfi/node/codec/linearcodec"

	"github.com/luxfi/node/consensus"

	"github.com/luxfi/node/consensus/consensustest"

	"github.com/luxfi/node/consensus/uptime"

	"github.com/luxfi/node/consensus/validators"

	"github.com/luxfi/database"

	"github.com/luxfi/database/memdb"

	"github.com/luxfi/database/versiondb"

	"github.com/luxfi/ids"

	"github.com/luxfi/node/utils"

	"github.com/luxfi/node/utils/constants"

	"github.com/luxfi/crypto/secp256k1"

	"github.com/luxfi/node/utils/formatting"

	"github.com/luxfi/node/utils/formatting/address"

	"github.com/luxfi/node/utils/json"

	"github.com/luxfi/log"

	"github.com/luxfi/node/utils/timer/mockable"

	"github.com/luxfi/node/utils/units"

	"github.com/luxfi/node/vms/platformvm/api"

	"github.com/luxfi/node/vms/platformvm/config"

	"github.com/luxfi/node/vms/platformvm/fx"

	"github.com/luxfi/node/vms/platformvm/metrics"

	"github.com/luxfi/node/vms/platformvm/reward"

	"github.com/luxfi/node/vms/platformvm/state"

	"github.com/luxfi/node/vms/platformvm/status"

	"github.com/luxfi/node/vms/platformvm/txs"

	"github.com/luxfi/node/vms/platformvm/txs/fee"

	"github.com/luxfi/node/vms/platformvm/txs/txstest"

	"github.com/luxfi/node/vms/platformvm/upgrade"

	"github.com/luxfi/node/vms/platformvm/utxo"

	"github.com/luxfi/node/vms/secp256k1fx"

	"github.com/luxfi/node/wallet/subnet/primary/common"

	walletsigner "github.com/luxfi/node/wallet/chain/p/signer"
)

const (
	defaultWeight = 5 * units.MilliLux
	trackChecksum = false

	apricotPhase3 fork = iota
	apricotPhase5
	banff
	cortina
	durango
	eUpgrade
)

var (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour
	defaultGenesisTime        = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultValidateStartTime  = defaultGenesisTime
	defaultValidateEndTime    = defaultValidateStartTime.Add(20 * defaultMinStakingDuration)
	defaultMinValidatorStake  = 5 * units.MilliLux
	defaultBalance            = 100 * defaultMinValidatorStake
	preFundedKeys             = secp256k1.TestKeys()
	defaultTxFee              = uint64(100)
	lastAcceptedID            = ids.GenerateTestID()

	testSubnet1            *txs.Tx
	testSubnet1ControlKeys = preFundedKeys[0:3]

	// Node IDs of genesis validators. Initialized in init function
	genesisNodeIDs []ids.NodeID
)

func init() {
	genesisNodeIDs = make([]ids.NodeID, len(preFundedKeys))
	for i := range preFundedKeys {
		genesisNodeIDs[i] = ids.GenerateTestNodeID()
	}
}

type fork uint8

type mutableSharedMemory struct {
	atomic.SharedMemory
}

type environment struct {
	isBootstrapped *utils.Atomic[bool]
	config         *config.Config
	clk            *mockable.Clock
	baseDB         *versiondb.Database
	ctx            *consensus.Context
	msm            *mutableSharedMemory
	fx             fx.Fx
	state          state.State
	states         map[ids.ID]state.Chain
	uptimes        uptime.Manager
	utxosHandler   utxo.Verifier
	factory        *txstest.WalletFactory
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

func newEnvironment(t *testing.T, f fork) *environment {
	var isBootstrapped utils.Atomic[bool]
	isBootstrapped.Set(true)

	config := defaultConfig(t, f)
	clk := defaultClock(f)

	baseDB := versiondb.New(memdb.New())
	ctx := consensustest.Context(t, consensustest.PChainID)
	m := atomic.NewMemory(baseDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	fx := defaultFx(clk, ctx.Log, isBootstrapped.Get())

	rewards := reward.NewCalculator(config.RewardConfig)
	baseState := defaultState(config, ctx, baseDB, rewards)

	uptimes := uptime.NewManager(baseState, clk)
	utxosHandler := utxo.NewHandler(ctx, clk, fx)

	factory := txstest.NewWalletFactory(ctx, config, baseState)

	backend := Backend{
		Config:       config,
		Ctx:          ctx,
		Clk:          clk,
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
		fx:             fx,
		state:          baseState,
		states:         make(map[ids.ID]state.Chain),
		uptimes:        uptimes,
		utxosHandler:   utxosHandler,
		factory:        factory,
		backend:        backend,
	}

	addSubnet(t, env)

	t.Cleanup(func() {
		env.ctx.Lock.Lock()
		defer env.ctx.Lock.Unlock()

		require := require.New(t)

		if env.isBootstrapped.Get() {
			validatorIDs := env.config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)

			// Only stop tracking if it was started
			_ = env.uptimes.StopTracking(validatorIDs)

			for subnetID := range env.config.TrackedSubnets {
				validatorIDs := env.config.Validators.GetValidatorIDs(subnetID)

				_ = env.uptimes.StopTracking(validatorIDs)
			}
			env.state.SetHeight(math.MaxUint64)
			require.NoError(env.state.Commit())
		}

		require.NoError(env.state.Close())
		require.NoError(env.baseDB.Close())
	})

	return env
}

func addSubnet(t *testing.T, env *environment) {
	require := require.New(t)

	builder, signer := env.factory.NewWallet(preFundedKeys[0])
	utx, err := builder.NewCreateSubnetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 2,
			Addrs: []ids.ShortID{
				preFundedKeys[0].PublicKey().Address(),
				preFundedKeys[1].PublicKey().Address(),
				preFundedKeys[2].PublicKey().Address(),
			},
		},
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
		}),
	)
	require.NoError(err)
	testSubnet1, err = walletsigner.SignUnsigned(context.Background(), signer, utx)
	require.NoError(err)

	stateDiff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)

	executor := StandardTxExecutor{
		Backend: &env.backend,
		State:   stateDiff,
		Tx:      testSubnet1,
	}
	require.NoError(testSubnet1.Unsigned.Visit(&executor))

	stateDiff.AddTx(testSubnet1, status.Committed)
	require.NoError(stateDiff.Apply(env.state))
	require.NoError(env.state.Commit())
}

func defaultState(
	cfg *config.Config,
	ctx *consensus.Context,
	db database.Database,
	rewards reward.Calculator,
) state.State {
	genesisBytes := buildGenesisTest(ctx)
	execCfg, _ := config.GetExecutionConfig(nil)
	state, err := state.New(
		db,
		genesisBytes,
		metrics.NewNoOpMetrics("test").Registry(),
		cfg,
		execCfg,
		ctx,
		metrics.Noop,
		rewards,
	)
	if err != nil {
		panic(err)
	}

	// persist and reload to init a bunch of in-memory stuff
	state.SetHeight(0)
	if err := state.Commit(); err != nil {
		panic(err)
	}
	lastAcceptedID = state.GetLastAccepted()
	return state
}

func defaultConfig(t *testing.T, f fork) *config.Config {
	c := &config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		Validators:             validators.NewManager(),
		StaticFeeConfig: fee.StaticConfig{
			TxFee:                 defaultTxFee,
			CreateSubnetTxFee:     100 * defaultTxFee,
			CreateBlockchainTxFee: 100 * defaultTxFee,
		},
		MinValidatorStake: 5 * units.MilliLux,
		MaxValidatorStake: 500 * units.MilliLux,
		MinDelegatorStake: 1 * units.MilliLux,
		MinStakeDuration:  defaultMinStakingDuration,
		MaxStakeDuration:  defaultMaxStakingDuration,
		RewardConfig: reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .10 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaLux,
		},
		UpgradeConfig: upgrade.Config{
			ApricotPhase3Time: mockable.MaxTime,
			ApricotPhase5Time: mockable.MaxTime,
			BanffTime:         mockable.MaxTime,
			CortinaTime:       mockable.MaxTime,
			DurangoTime:       mockable.MaxTime,
			EUpgradeTime:      mockable.MaxTime,
		},
	}

	switch f {
	case eUpgrade:
		c.UpgradeConfig.EUpgradeTime = defaultValidateStartTime.Add(-2 * time.Second)
		fallthrough
	case durango:
		c.UpgradeConfig.DurangoTime = defaultValidateStartTime.Add(-2 * time.Second)
		fallthrough
	case cortina:
		c.UpgradeConfig.CortinaTime = defaultValidateStartTime.Add(-2 * time.Second)
		fallthrough
	case banff:
		c.UpgradeConfig.BanffTime = defaultValidateStartTime.Add(-2 * time.Second)
		fallthrough
	case apricotPhase5:
		c.UpgradeConfig.ApricotPhase5Time = defaultValidateEndTime
		fallthrough
	case apricotPhase3:
		c.UpgradeConfig.ApricotPhase3Time = defaultValidateEndTime
	default:
		require.FailNow(t, "unhandled fork", f)
	}

	return c
}

func defaultClock(f fork) *mockable.Clock {
	now := defaultGenesisTime
	if f >= banff {
		// 1 second after active fork
		now = defaultValidateEndTime.Add(-2 * time.Second)
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

func buildGenesisTest(ctx *consensus.Context) []byte {
	genesisUTXOs := make([]api.UTXO, len(preFundedKeys))
	for i, key := range preFundedKeys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
		if err != nil {
			panic(err)
		}
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.GenesisPermissionlessValidator, len(genesisNodeIDs))
	for i, nodeID := range genesisNodeIDs {
		addr, err := address.FormatBech32(constants.UnitTestHRP, nodeID.Bytes())
		if err != nil {
			panic(err)
		}
		genesisValidators[i] = api.GenesisPermissionlessValidator{
			GenesisValidator: api.GenesisValidator{
				StartTime: json.Uint64(defaultValidateStartTime.Unix()),
				EndTime:   json.Uint64(defaultValidateEndTime.Unix()),
				NodeID:    nodeID,
			},
			RewardOwner: &api.Owner{
				Threshold: 1,
				Addresses: []string{addr},
			},
			Staked: []api.UTXO{{
				Amount:  json.Uint64(defaultWeight),
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		NetworkID:     json.Uint32(constants.UnitTestID),
		LuxAssetID:    ctx.LUXAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaLux),
		Encoding:      formatting.Hex,
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		panic(fmt.Errorf("problem while building platform chain's genesis state: %w", err))
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		panic(err)
	}

	return genesisBytes
}
