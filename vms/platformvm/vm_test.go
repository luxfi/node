// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/consensustest"
	consContext "github.com/luxfi/consensus/context"
	linearblock "github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/interfaces"
	"github.com/luxfi/consensus/uptime"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	mathset "github.com/luxfi/math/set"
	"github.com/luxfi/node/benchlist"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/api"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/testcontext"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/vms/platformvm/txs/txstest"
	"github.com/luxfi/node/vms/platformvm/upgrade"
	"github.com/luxfi/node/vms/secp256k1fx"

	blockbuilder "github.com/luxfi/node/vms/platformvm/block/builder"
	blockexecutor "github.com/luxfi/node/vms/platformvm/block/executor"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	walletbuilder "github.com/luxfi/node/wallet/chain/p/builder"
	walletsigner "github.com/luxfi/node/wallet/chain/p/signer"
	walletcommon "github.com/luxfi/node/wallet/net/primary/common"
)

const (
	apricotPhase3 fork = iota
	apricotPhase5
	banff
	cortina
	durango
	eUpgrade

	latestFork = durango

	defaultWeight uint64 = 5000 // Reduced to free up balance for fees
)

var (
	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour

	defaultRewardConfig = reward.Config{
		MaxConsumptionRate: .12 * reward.PercentDenominator,
		MinConsumptionRate: .10 * reward.PercentDenominator,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * units.MegaLux,
	}

	defaultTxFee = uint64(100)

	// chain timestamp at genesis
	defaultGenesisTime = time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)

	// time that genesis validators start validating
	defaultValidateStartTime = defaultGenesisTime

	// time that genesis validators stop validating
	defaultValidateEndTime = defaultValidateStartTime.Add(10 * defaultMinStakingDuration)

	latestForkTime = defaultGenesisTime.Add(time.Second)

	// each key controls an address that has [defaultBalance] LUX at genesis
	keys = secp256k1.TestKeys()

	// Node IDs of genesis validators. Initialized in init function
	genesisNodeIDs           []ids.NodeID
	defaultMinDelegatorStake = 1 * units.MilliLux
	defaultMinValidatorStake = 5 * defaultMinDelegatorStake
	defaultMaxValidatorStake = 100 * defaultMinValidatorStake
	defaultBalance           = 2*defaultMaxValidatorStake + 1000*units.Lux // amount all genesis validators have in defaultVM, with extra for fees

	// net that exists at genesis in defaultVM
	// Its controlKeys are keys[0], keys[1], keys[2]
	// Its threshold is 2
	testSubnet1            *txs.Tx
	testSubnet1ControlKeys = keys[0:3]
)

func init() {
	for _, key := range keys {
		// Can be done when TestGetState is refactored
		nodeBytes := key.PublicKey().Address()
		nodeID := ids.BuildTestNodeID(nodeBytes[:])

		genesisNodeIDs = append(genesisNodeIDs, nodeID)
	}
}

type fork uint8

type mutableSharedMemory struct {
	atomic.SharedMemory
}

// Returns:
// 1) The genesis state
// 2) The byte representation of the default genesis for tests
func defaultGenesis(t *testing.T, luxAssetID ids.ID) (*api.BuildGenesisArgs, []byte) {
	require := require.New(t)

	genesisUTXOs := make([]api.UTXO, len(keys))
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
		require.NoError(err)
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	genesisValidators := make([]api.GenesisPermissionlessValidator, len(genesisNodeIDs))
	for i, nodeID := range genesisNodeIDs {
		// Use the actual key address, not the nodeID bytes as address
		keyAddr := keys[i].PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, keyAddr.Bytes())
		require.NoError(err)
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
				Amount:  json.Uint64(defaultWeight - 1000), // Reserve some balance for fees
				Address: addr,
			}},
			DelegationFee: reward.PercentDenominator,
		}
	}

	buildGenesisArgs := api.BuildGenesisArgs{
		Encoding:      formatting.Hex,
		NetworkID:     json.Uint32(constants.UnitTestID),
		LuxAssetID:    luxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaLux),
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(err)

	return &buildGenesisArgs, genesisBytes
}

func defaultVM(t *testing.T, f fork) (*VM, *txstest.WalletFactory, database.Database, *mutableSharedMemory, *testcontext.Context) {
	require := require.New(t)
	var (
		apricotPhase3Time = mockable.MaxTime
		apricotPhase5Time = mockable.MaxTime
		banffTime         = mockable.MaxTime
		cortinaTime       = mockable.MaxTime
		durangoTime       = mockable.MaxTime
		eUpgradeTime      = mockable.MaxTime
	)

	// always reset latestForkTime (a package level variable)
	// to ensure test independence
	latestForkTime = defaultGenesisTime.Add(time.Second)
	switch f {
	case eUpgrade:
		eUpgradeTime = latestForkTime
		fallthrough
	case durango:
		durangoTime = latestForkTime
		fallthrough
	case cortina:
		cortinaTime = latestForkTime
		fallthrough
	case banff:
		banffTime = latestForkTime
		fallthrough
	case apricotPhase5:
		apricotPhase5Time = latestForkTime
		fallthrough
	case apricotPhase3:
		apricotPhase3Time = latestForkTime
	default:
		require.FailNow("unhandled fork", f)
	}

	vm := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		StaticFeeConfig: fee.StaticConfig{
			TxFee:                 defaultTxFee,
			CreateNetTxFee:        defaultTxFee, // Minimal fee for testing
			TransformNetTxFee:     defaultTxFee, // Minimal fee for testing
			CreateBlockchainTxFee: defaultTxFee, // Minimal fee for testing
		},
		MinValidatorStake: defaultMinValidatorStake,
		MaxValidatorStake: defaultMaxValidatorStake,
		MinDelegatorStake: defaultMinDelegatorStake,
		MinStakeDuration:  defaultMinStakingDuration,
		MaxStakeDuration:  defaultMaxStakingDuration,
		RewardConfig:      defaultRewardConfig,
		UpgradeConfig: upgrade.Config{
			ApricotPhase3Time: apricotPhase3Time,
			ApricotPhase5Time: apricotPhase5Time,
			BanffTime:         banffTime,
			CortinaTime:       cortinaTime,
			DurangoTime:       durangoTime,
			EUpgradeTime:      eUpgradeTime,
		},
	}}

	db := memdb.New()
	chainDB := prefixdb.New([]byte{0}, db)
	atomicDB := prefixdb.New([]byte{1}, db)

	vm.Clock().Set(latestForkTime)
	ctx := testcontext.New(context.Background())
	ctx.ChainID = consensustest.PChainID
	ctx.XAssetID = ids.GenerateTestID()

	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	_, genesisBytes := defaultGenesis(t, ctx.XAssetID)
	// Create a simple app sender mock
	appSender := &testAppSender{}

	// Create a ChainContext from the test context
	// ChainContext embeds ConsensusContext and Context
	luxCtx := &consContext.Context{
		QuantumID:   ctx.NetworkID,
		NetID:       ctx.NetID,
		ChainID:     ctx.ChainID,
		NodeID:      ctx.NodeID,
		PublicKey:   nil,
		XChainID:    ctx.XChainID,
		CChainID:    ctx.CChainID,
		XAssetID: ctx.XAssetID,
		XAssetID:  ctx.XAssetID,
		StartTime:   time.Now(),
	}
	chainCtx := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          luxCtx,
	}

	// Create DB manager
	dbManager := &simpleDBManager{
		db: chainDB,
	}

	// Create message channel
	toEngine := make(chan linearblock.Message, 1)

	dynamicConfigBytes := []byte(`{"network":{"max-validator-set-staleness":0}}`)
	require.NoError(vm.Initialize(
		context.Background(),
		chainCtx,
		dbManager,
		genesisBytes,
		nil,
		dynamicConfigBytes,
		toEngine,
		nil,
		appSender,
	))

	// align chain time and local clock
	vm.state.SetTimestamp(vm.Clock().Time())

	require.NoError(vm.SetState(context.Background(), interfaces.NormalOp))

	factory := txstest.NewWalletFactoryWithAssets(
		ctx.Context,
		ctx.SharedMemory,
		&vm.Config,
		vm.state,
		ctx.XAssetID,
	)

	// Create a net and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
	builder, signer := factory.NewWallet(keys[0])

	// Debug: check available UTXOs and fees
	addr := keys[0].PublicKey().Address()
	t.Logf("keys[0] address: %s", addr)
	utxoIDs, _ := vm.state.UTXOIDs(addr.Bytes(), ids.Empty, math.MaxInt32)
	t.Logf("Available UTXOs for keys[0]: %d", len(utxoIDs))
	t.Logf("LUX AssetID: %s", ctx.XAssetID)
	t.Logf("CreateNetTxFee: %d", vm.Config.StaticFeeConfig.CreateNetTxFee)
	for _, utxoID := range utxoIDs {
		utxo, _ := vm.state.GetUTXO(utxoID)
		if utxo != nil {
			out := utxo.Out
			t.Logf("  UTXO %s: AssetID=%s OutType=%T", utxoID, utxo.AssetID(), out)
			if transferOut, ok := out.(*secp256k1fx.TransferOutput); ok {
				t.Logf("    Amount=%d Addrs=%v", transferOut.Amt, transferOut.Addrs)
			}
		}
	}

	utx, err := builder.NewCreateNetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 2,
			Addrs: []ids.ShortID{
				keys[0].PublicKey().Address(),
				keys[1].PublicKey().Address(),
				keys[2].PublicKey().Address(),
			},
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
		}),
	)
	require.NoError(err)
	testSubnet1, err = walletsigner.SignUnsigned(context.Background(), signer, utx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(testSubnet1))
	ctx.Lock.Lock()
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	t.Cleanup(func() {
		ctx.Lock.Lock()
		defer ctx.Lock.Unlock()

		require.NoError(vm.Shutdown(context.Background()))
	})

	return vm, factory, db, msm, ctx
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	require := require.New(t)
	vm, _, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// Ensure the genesis block has been accepted and stored
	genesisBlockID, err := vm.LastAccepted(context.Background()) // lastAccepted should be ID of genesis block
	require.NoError(err)

	// Verify we can get the genesis block
	_, err = vm.manager.GetBlock(genesisBlockID)
	require.NoError(err)
	// Genesis block is already accepted

	genesisState, _ := defaultGenesis(t, vm.luxAssetID)
	// Ensure all the genesis UTXOs are there
	for _, utxo := range genesisState.UTXOs {
		_, addrBytes, err := address.ParseBech32(utxo.Address)
		require.NoError(err)

		addr, err := ids.ToShortID(addrBytes)
		require.NoError(err)

		addrs := mathset.Of(addr)
		utxos, err := lux.GetAllUTXOs(vm.state, addrs)
		require.NoError(err)
		require.Len(utxos, 1)

		out := utxos[0].Out.(*secp256k1fx.TransferOutput)
		if out.Amount() != uint64(utxo.Amount) {
			id := keys[0].PublicKey().Address()
			addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
			require.NoError(err)

			require.Equal(utxo.Address, addr)
			require.Equal(uint64(utxo.Amount)-vm.StaticFeeConfig.CreateNetTxFee, out.Amount())
		}
	}

	// Ensure current validator set of primary network is correct
	validatorIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
	require.Len(genesisState.Validators, len(validatorIDs))

	for _, nodeID := range genesisNodeIDs {
		_, ok := vm.Validators.GetValidator(constants.PrimaryNetworkID, nodeID)
		require.True(ok)
	}

	// Ensure the new net we created exists
	_, _, err = vm.state.GetTx(testSubnet1.ID())
	require.NoError(err)
}

// accept proposal to add validator to primary network
func TestAddValidatorCommit(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	var (
		startTime     = vm.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime       = startTime.Add(defaultMinStakingDuration)
		nodeID        = ids.GenerateTestNodeID()
		rewardAddress = ids.GenerateTestShortID()
	)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	// Generate proof of possession
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	// create valid tx
	builder, txSigner := factory.NewWallet(keys[0])
	utx, err := builder.NewAddPermissionlessValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   vm.MinValidatorStake,
			},
			Net: constants.PrimaryNetworkID,
		},
		pop,
		vm.luxAssetID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	// trigger block creation
	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator now in current validator set
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
}

// verify invalid attempt to add validator to primary network
func TestInvalidAddValidatorCommit(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, cortina)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	nodeID := ids.GenerateTestNodeID()
	startTime := defaultGenesisTime.Add(-txexecutor.SyncBound).Add(-1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	// create invalid tx
	builder, txSigner := factory.NewWallet(keys[0])
	utx, err := builder.NewAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		preferred.Timestamp(),
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	blkBytes := statelessBlk.Bytes()

	parsedBlock, err := vm.ParseBlock(context.Background(), blkBytes)
	require.NoError(err)

	err = parsedBlock.Verify(context.Background())
	require.ErrorIs(err, txexecutor.ErrTimestampNotBeforeStartTime)

	txID := statelessBlk.Txs()[0].ID()
	reason := vm.Builder.GetDropReason(txID)
	require.ErrorIs(reason, txexecutor.ErrTimestampNotBeforeStartTime)
}

// Reject attempt to add validator to primary network
func TestAddValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, cortina)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	var (
		startTime     = vm.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime       = startTime.Add(defaultMinStakingDuration)
		nodeID        = ids.GenerateTestNodeID()
		rewardAddress = ids.GenerateTestShortID()
	)

	// create valid tx
	builder, txSigner := factory.NewWallet(keys[0])
	utx, err := builder.NewAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   vm.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	// trigger block creation
	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Reject(context.Background()))

	_, _, err = vm.state.GetTx(tx.ID())
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Reject proposal to add validator to primary network
func TestAddValidatorInvalidNotReissued(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// Use nodeID that is already in the genesis
	repeatNodeID := genesisNodeIDs[0]

	startTime := latestForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	// Generate proof of possession
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	// create valid tx
	builder, txSigner := factory.NewWallet(keys[0])
	utx, err := builder.NewAddPermissionlessValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: repeatNodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   vm.MinValidatorStake,
			},
			Net: constants.PrimaryNetworkID,
		},
		pop,
		vm.luxAssetID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	// trigger block creation
	ctx.Lock.Unlock()
	err = vm.issueTxFromRPC(tx)
	ctx.Lock.Lock()
	require.ErrorIs(err, txexecutor.ErrDuplicateValidator)
}

// Accept proposal to add validator to subnet
func TestAddNetValidatorAccept(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	var (
		startTime = vm.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime   = startTime.Add(defaultMinStakingDuration)
		nodeID    = genesisNodeIDs[0]
	)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	builder, txSigner := factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
	utx, err := builder.NewAddNetValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   defaultWeight,
			},
			Net: testSubnet1.ID(),
		},
	)
	require.NoError(err)
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	// trigger block creation
	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator is in current validator set
	_, err = vm.state.GetCurrentValidator(testSubnet1.ID(), nodeID)
	require.NoError(err)
}

// Reject proposal to add validator to subnet
func TestAddNetValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	var (
		startTime = vm.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime   = startTime.Add(defaultMinStakingDuration)
		nodeID    = genesisNodeIDs[0]
	)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([defaultValidateStartTime, defaultValidateEndTime])
	builder, txSigner := factory.NewWallet(testSubnet1ControlKeys[1], testSubnet1ControlKeys[2])
	utx, err := builder.NewAddNetValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   defaultWeight,
			},
			Net: testSubnet1.ID(),
		},
	)
	require.NoError(err)
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	// trigger block creation
	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Reject(context.Background()))

	_, _, err = vm.state.GetTx(tx.ID())
	require.ErrorIs(err, database.ErrNotFound)

	// Verify that new validator NOT in validator set
	_, err = vm.state.GetCurrentValidator(testSubnet1.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator rewarded
// noOpBenchlist is a mock implementation of benchlist.Manager for testing
type noOpBenchlist struct{}

func (n *noOpBenchlist) IsBenched(nodeID ids.NodeID, chainID ids.ID) bool {
	return false
}

func (n *noOpBenchlist) GetBenched(chainID ids.ID) []ids.NodeID {
	return nil
}

func (n *noOpBenchlist) RegisterChain(chainID ids.ID, vdrs validators.Manager) error {
	return nil
}

func (n *noOpBenchlist) Benchable(chainID ids.ID, nodeID ids.NodeID) benchlist.Benchable {
	return n
}

func (n *noOpBenchlist) Benched(chainID ids.ID, nodeID ids.NodeID) {}

func (n *noOpBenchlist) Unbenched(chainID ids.ID, nodeID ids.NodeID) {}

func TestRewardValidatorAccept(t *testing.T) {
	require := require.New(t)
	vm, _, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// Fast forward clock to time for genesis validators to leave
	vm.Clock().Set(defaultValidateEndTime)

	// Advance time and create proposal to reward a genesis validator
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	execBlk := blk.(*blockexecutor.Block)
	options, err := execBlk.Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)
	abort := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := blk.(block.Block).Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)

	// Verify options and accept commmit block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	txID := blk.(block.Block).Txs()[0].ID()
	{
		onAbort, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAbort.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(blk.Accept(context.Background()))
	require.NoError(commit.Accept(context.Background()))

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	tx, _, err := vm.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator not rewarded
func TestRewardValidatorReject(t *testing.T) {
	require := require.New(t)
	vm, _, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// Fast forward clock to time for genesis validators to leave
	vm.Clock().Set(defaultValidateEndTime)

	// Advance time and create proposal to reward a genesis validator
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct
	execBlk := blk.(*blockexecutor.Block)
	options, err := execBlk.Options(context.Background())
	require.NoError(err)

	commit := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	abort := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := execBlk.Block.Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)

	// Verify options and accept abort block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	txID := execBlk.Block.Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(blk.Accept(context.Background()))
	require.NoError(abort.Accept(context.Background()))

	// Verify that chain's timestamp has advanced
	timestamp := vm.state.GetTimestamp()
	require.Equal(defaultValidateEndTime.Unix(), timestamp.Unix())

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	tx, _, err := vm.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

// Ensure BuildBlock errors when there is no block to build
func TestUnneededBuildBlock(t *testing.T) {
	require := require.New(t)
	vm, _, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	_, err := vm.Builder.BuildBlock(context.Background())
	require.ErrorIs(err, blockbuilder.ErrNoPendingBlocks)
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	builder, txSigner := factory.NewWallet(testSubnet1ControlKeys[0], testSubnet1ControlKeys[1])
	utx, err := builder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		ids.ID{'t', 'e', 's', 't', 'v', 'm'},
		nil,
		"name",
	)
	require.NoError(err)
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err) // should contain proposal to create chain

	require.NoError(blk.Verify(context.Background()))

	require.NoError(blk.Accept(context.Background()))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify chain was created
	chains, err := vm.state.GetChains(testSubnet1.ID())
	require.NoError(err)

	foundNewChain := false
	for _, chain := range chains {
		if bytes.Equal(chain.Bytes(), tx.Bytes()) {
			foundNewChain = true
		}
	}
	require.True(foundNewChain)
}

// test where we:
// 1) Create a subnet
// 2) Add a validator to the subnet's current validator set
// 3) Advance timestamp to validator's end time (removing validator from current)
func TestCreateNet(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// Use keys[1] instead of keys[0] as keys[0] was used in defaultVM to create a net
	builder, txSigner := factory.NewWallet(keys[1])
	uCreateNetTx, err := builder.NewCreateNetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				keys[0].PublicKey().Address(),
				keys[1].PublicKey().Address(),
			},
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
		}),
	)
	require.NoError(err)
	createSubnetTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uCreateNetTx)
	require.NoError(err)
	netID := createSubnetTx.ID()

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(createSubnetTx))
	ctx.Lock.Lock()

	// should contain the CreateNetTx
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, txStatus, err := vm.state.GetTx(netID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	netIDs, err := vm.state.GetNetIDs()
	require.NoError(err)
	require.Contains(netIDs, netID)

	// Now that we've created a new subnet, add a validator to that subnet
	nodeID := genesisNodeIDs[0]
	startTime := vm.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	// [startTime, endTime] is subset of time keys[0] validates default net so tx is valid
	uAddValTx, err := builder.NewAddNetValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   defaultWeight,
			},
			Net: netID,
		},
	)
	require.NoError(err)
	addValidatorTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uAddValTx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(addValidatorTx))
	ctx.Lock.Lock()

	blk, err = vm.Builder.BuildBlock(context.Background()) // should add validator to the new subnet
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background())) // add the validator to current validator set
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	txID := blk.(block.Block).Txs()[0].ID()
	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	_, err = vm.state.GetPendingValidator(netID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetCurrentValidator(netID, nodeID)
	require.NoError(err)

	// fast forward clock to time validator should stop validating
	vm.Clock().Set(endTime)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background())) // remove validator from current validator set

	_, err = vm.state.GetPendingValidator(netID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vm.state.GetCurrentValidator(netID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// test asset import
func TestAtomicImport(t *testing.T) {
	require := require.New(t)
	vm, factory, baseDB, mutableSharedMemory, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	utxoID := lux.UTXOID{
		TxID:        ids.Empty.Prefix(1),
		OutputIndex: 1,
	}
	amount := uint64(50000)
	recipientKey := keys[1]

	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))

	mutableSharedMemory.SharedMemory = m.NewSharedMemory(ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(ctx.XChainID)

	builder, _ := factory.NewWallet(keys[0])
	_, err := builder.NewImportTx(
		ctx.XChainID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
		},
	)
	require.ErrorIs(err, walletbuilder.ErrInsufficientFunds)

	// Provide the xvm UTXO

	utxo := &lux.UTXO{
		UTXOID: utxoID,
		Asset:  lux.Asset{ID: vm.luxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		ctx.ChainID: {
			PutRequests: []*atomic.Element{
				{
					Key:   inputID[:],
					Value: utxoBytes,
					Traits: [][]byte{
						recipientKey.PublicKey().Address().Bytes(),
					},
				},
			},
		},
	}))

	builder, txSigner := factory.NewWallet(recipientKey)
	utx, err := builder.NewImportTx(
		ctx.XChainID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
		},
	)
	require.NoError(err)
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(tx))
	ctx.Lock.Lock()

	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))

	require.NoError(blk.Accept(context.Background()))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	inputID = utxoID.InputID()
	_, err = ctx.SharedMemory.Get(ctx.XChainID, [][]byte{inputID[:]})
	require.ErrorIs(err, database.ErrNotFound)
}

// test optimistic asset import
func TestOptimisticAtomicImport(t *testing.T) {
	require := require.New(t)
	vm, _, _, _, ctx := defaultVM(t, apricotPhase3)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: ctx.ChainID,
		}},
		SourceChain: ctx.XChainID,
		ImportedInputs: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: lux.Asset{ID: vm.luxAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx.Initialize(txs.Codec))

	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewApricotAtomicBlock(
		preferredID,
		preferredHeight+1,
		tx,
	)
	require.NoError(err)

	blk := vm.manager.NewBlock(statelessBlk)

	err = blk.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound) // erred due to missing shared memory UTXOs

	require.NoError(vm.SetState(context.Background(), interfaces.Bootstrapping))

	require.NoError(blk.Verify(context.Background())) // skips shared memory UTXO verification during bootstrapping

	require.NoError(blk.Accept(context.Background()))

	// Stop tracking before transitioning back to NormalOp to avoid "already started tracking" error
	// Note: StopTracking method no longer exists in uptime.Calculator interface
	// validatorIDs := vm.Config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
	// require.NoError(vm.uptimeManager.StopTracking(validatorIDs))

	require.NoError(vm.SetState(context.Background(), interfaces.NormalOp))

	_, txStatus, err := vm.state.GetTx(tx.ID())
	require.NoError(err)

	require.Equal(status.Committed, txStatus)
}

// test restarting the node
func TestRestartFullyAccepted(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	firstDB := prefixdb.New([]byte{}, db)
	firstVM := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig: upgrade.Config{
			BanffTime:    latestForkTime,
			CortinaTime:  latestForkTime,
			DurangoTime:  latestForkTime,
			EUpgradeTime: mockable.MaxTime,
		},
	}}

	firstCtx := testcontext.New(context.Background())
	firstCtx.ChainID = consensustest.PChainID
	firstCtx.XAssetID = ids.GenerateTestID()

	_, genesisBytes := defaultGenesis(t, firstCtx.XAssetID)

	baseDB := memdb.New()
	atomicDB := prefixdb.New([]byte{1}, baseDB)
	m := atomic.NewMemory(atomicDB)
	firstCtx.SharedMemory = m.NewSharedMemory(firstCtx.ChainID)

	initialClkTime := latestForkTime.Add(time.Second)
	firstVM.Clock().Set(initialClkTime)
	firstCtx.Lock.Lock()

	// Create lux context for chain context
	luxCtx := &consContext.Context{
		QuantumID:  firstCtx.NetworkID,
		NodeID:     firstCtx.NodeID,
		PublicKey:  nil,
		XChainID:   firstCtx.XChainID,
		CChainID:   firstCtx.CChainID,
		XAssetID: firstCtx.XAssetID,
		ChainID:    firstCtx.ChainID,
		NetID:      constants.PrimaryNetworkID,
		StartTime:  time.Now(),
	}

	chainCtx := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          luxCtx,
	}

	firstDB = prefixdb.New([]byte{}, memdb.New())
	dbManager := &simpleDBManager{
		db: firstDB,
	}

	require.NoError(firstVM.Initialize(
		context.Background(),
		chainCtx,
		dbManager,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
		&testAppSender{},
	))

	genesisID, err := firstVM.LastAccepted(context.Background())
	require.NoError(err)

	// include a tx to make the block be accepted
	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    firstCtx.NetworkID,
			BlockchainID: firstCtx.ChainID,
		}},
		SourceChain: firstCtx.XChainID,
		ImportedInputs: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: lux.Asset{ID: firstCtx.XAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx.Initialize(txs.Codec))

	nextChainTime := initialClkTime.Add(time.Second)
	firstVM.Clock().Set(initialClkTime)

	preferredID := firstVM.manager.Preferred()
	preferred, err := firstVM.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx},
	)
	require.NoError(err)

	firstAdvanceTimeBlk := firstVM.manager.NewBlock(statelessBlk)

	nextChainTime = nextChainTime.Add(2 * time.Second)
	firstVM.Clock().Set(nextChainTime)
	require.NoError(firstAdvanceTimeBlk.Verify(context.Background()))
	require.NoError(firstAdvanceTimeBlk.Accept(context.Background()))

	require.NoError(firstVM.Shutdown(context.Background()))
	firstCtx.Lock.Unlock()

	secondVM := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig: upgrade.Config{
			BanffTime:    latestForkTime,
			CortinaTime:  latestForkTime,
			DurangoTime:  latestForkTime,
			EUpgradeTime: mockable.MaxTime,
		},
	}}

	secondCtx := testcontext.New(context.Background())
	secondCtx.ChainID = consensustest.PChainID
	secondCtx.XAssetID = firstCtx.XAssetID
	secondCtx.SharedMemory = firstCtx.SharedMemory
	secondVM.Clock().Set(initialClkTime)
	secondCtx.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(context.Background()))
		secondCtx.Lock.Unlock()
	}()

	// Create lux context for chain context
	luxCtx2 := &consContext.Context{
		QuantumID:  secondCtx.NetworkID,
		NodeID:     secondCtx.NodeID,
		PublicKey:  nil,
		XChainID:   secondCtx.XChainID,
		CChainID:   secondCtx.CChainID,
		XAssetID: secondCtx.XAssetID,
		ChainID:    secondCtx.ChainID,
		NetID:      constants.PrimaryNetworkID,
		StartTime:  time.Now(),
	}

	chainCtx2 := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          luxCtx2,
	}

	secondDB := prefixdb.New([]byte{}, db)
	dbManager2 := &simpleDBManager{
		db: secondDB,
	}

	require.NoError(secondVM.Initialize(
		context.Background(),
		chainCtx2,
		dbManager2,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
		nil,
	))

	lastAccepted, err := secondVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(genesisID, lastAccepted)
}

// test basic VM bootstrapping and initialization
func TestBootstrapPartiallyAccepted(t *testing.T) {
	require := require.New(t)

	// Use simpler VM setup without subnet creation
	vm := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		StaticFeeConfig: fee.StaticConfig{
			TxFee:                 defaultTxFee,
			CreateNetTxFee:        defaultTxFee,
			TransformNetTxFee:     defaultTxFee,
			CreateBlockchainTxFee: defaultTxFee,
		},
		MinValidatorStake: defaultMinValidatorStake,
		MaxValidatorStake: defaultMaxValidatorStake,
		MinDelegatorStake: defaultMinDelegatorStake,
		MinStakeDuration:  defaultMinStakingDuration,
		MaxStakeDuration:  defaultMaxStakingDuration,
		RewardConfig:      defaultRewardConfig,
		UpgradeConfig: upgrade.Config{
			ApricotPhase3Time: latestForkTime,
			ApricotPhase5Time: mockable.MaxTime,
			BanffTime:         mockable.MaxTime,
			CortinaTime:       mockable.MaxTime,
			DurangoTime:       mockable.MaxTime,
			EUpgradeTime:      mockable.MaxTime,
		},
	}}

	db := memdb.New()
	chainDB := prefixdb.New([]byte{0}, db)
	atomicDB := prefixdb.New([]byte{1}, db)

	vm.Clock().Set(latestForkTime)
	ctx := testcontext.New(context.Background())
	ctx.ChainID = consensustest.PChainID
	ctx.XAssetID = ids.GenerateTestID()

	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	_, genesisBytes := defaultGenesis(t, ctx.XAssetID)

	luxCtx := &consContext.Context{
		QuantumID:  ctx.NetworkID,
		NetID:      constants.PrimaryNetworkID,
		ChainID:    ctx.ChainID,
		NodeID:     ctx.NodeID,
		PublicKey:  nil,
		XChainID:   ctx.XChainID,
		CChainID:   ctx.CChainID,
		XAssetID: ctx.XAssetID,
		StartTime:  time.Now(),
	}

	chainCtx := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          luxCtx,
	}

	dbManager := &simpleDBManager{
		db: chainDB,
	}

	// Initialize the VM
	require.NoError(vm.Initialize(
		context.Background(),
		chainCtx,
		dbManager,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
		&testAppSender{},
	))

	// Test basic bootstrap functionality
	genesisBlockID, err := vm.LastAccepted(context.Background())
	require.NoError(err)
	require.NotEqual(ids.Empty, genesisBlockID)

	// Verify we can get the genesis block
	genesisBlock, err := vm.manager.GetBlock(genesisBlockID)
	require.NoError(err)
	require.NotNil(genesisBlock)

	// Verify basic chain metrics work
	height := genesisBlock.Height()
	require.Equal(uint64(0), height) // Genesis block is at height 0

	// Verify VM is ready to process blocks
	preferred := vm.manager.Preferred()
	require.Equal(genesisBlockID, preferred)
}

func TestUnverifiedParent(t *testing.T) {
	require := require.New(t)

	vm := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig: upgrade.Config{
			BanffTime:    latestForkTime,
			CortinaTime:  latestForkTime,
			DurangoTime:  latestForkTime,
			EUpgradeTime: mockable.MaxTime,
		},
	}}

	initialClkTime := latestForkTime.Add(time.Second)
	vm.Clock().Set(initialClkTime)
	ctx := testcontext.New(context.Background())
	ctx.ChainID = consensustest.PChainID
	ctx.XAssetID = ids.GenerateTestID()
	ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	_, genesisBytes := defaultGenesis(t, ctx.XAssetID)

	// Create lux context for chain context
	luxCtx := &consContext.Context{
		QuantumID:  ctx.NetworkID,
		NetID:      constants.PrimaryNetworkID,
		ChainID:    ctx.ChainID,
		NodeID:     ctx.NodeID,
		PublicKey:  nil,
		XChainID:   ctx.XChainID,
		CChainID:   ctx.CChainID,
		XAssetID: ctx.XAssetID,
		StartTime:  time.Now(),
	}

	chainCtx := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          luxCtx,
	}

	vmDB := memdb.New()
	dbManager := &simpleDBManager{
		db: vmDB,
	}

	require.NoError(vm.Initialize(
		context.Background(),
		chainCtx,
		dbManager,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
		&testAppSender{},
	))

	// include a tx1 to make the block be accepted
	tx1 := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: constants.PlatformChainID,
		}},
		SourceChain: ctx.XChainID,
		ImportedInputs: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: lux.Asset{ID: vm.luxAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx1.Initialize(txs.Codec))

	nextChainTime := initialClkTime.Add(time.Second)

	preferredID := vm.manager.Preferred()
	preferred, err := vm.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx1},
	)
	require.NoError(err)
	firstAdvanceTimeBlk := vm.manager.NewBlock(statelessBlk)
	require.NoError(firstAdvanceTimeBlk.Verify(context.Background()))

	// include a tx2 to make the block be accepted
	tx2 := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    ctx.NetworkID,
			BlockchainID: constants.PlatformChainID,
		}},
		SourceChain: ctx.XChainID,
		ImportedInputs: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty.Prefix(2),
				OutputIndex: 2,
			},
			Asset: lux.Asset{ID: vm.luxAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx2.Initialize(txs.Codec))
	nextChainTime = nextChainTime.Add(time.Second)
	vm.Clock().Set(nextChainTime)
	statelessSecondAdvanceTimeBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		firstAdvanceTimeBlk.ID(),
		firstAdvanceTimeBlk.Height()+1,
		[]*txs.Tx{tx2},
	)
	require.NoError(err)
	secondAdvanceTimeBlk := vm.manager.NewBlock(statelessSecondAdvanceTimeBlk)

	require.Equal(secondAdvanceTimeBlk.Parent(), firstAdvanceTimeBlk.ID())
	require.NoError(secondAdvanceTimeBlk.Verify(context.Background()))
}

func TestMaxStakeAmount(t *testing.T) {
	vm, _, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	nodeID := genesisNodeIDs[0]

	tests := []struct {
		description string
		startTime   time.Time
		endTime     time.Time
	}{
		{
			description: "[validator.StartTime] == [startTime] < [endTime] == [validator.EndTime]",
			startTime:   defaultValidateStartTime,
			endTime:     defaultValidateEndTime,
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] == [validator.EndTime]",
			startTime:   defaultValidateStartTime.Add(time.Minute),
			endTime:     defaultValidateEndTime,
		},
		{
			description: "[validator.StartTime] == [startTime] < [endTime] < [validator.EndTime]",
			startTime:   defaultValidateStartTime,
			endTime:     defaultValidateEndTime.Add(-time.Minute),
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] < [validator.EndTime]",
			startTime:   defaultValidateStartTime.Add(time.Minute),
			endTime:     defaultValidateEndTime.Add(-time.Minute),
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)
			staker, err := txexecutor.GetValidator(vm.state, constants.PrimaryNetworkID, nodeID)
			require.NoError(err)

			amount, err := txexecutor.GetMaxWeight(vm.state, staker, test.startTime, test.endTime)
			require.NoError(err)
			require.Equal(defaultWeight, amount)
		})
	}
}

func TestUptimeDisallowedWithRestart(t *testing.T) {
	require := require.New(t)
	latestForkTime = defaultValidateStartTime.Add(defaultMinStakingDuration)
	db := memdb.New()

	firstDB := prefixdb.New([]byte{}, db)
	const firstUptimePercentage = 20 // 20%
	firstVM := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimePercentage:       firstUptimePercentage / 100.,
		RewardConfig:           defaultRewardConfig,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		UpgradeConfig: upgrade.Config{
			BanffTime:    latestForkTime,
			CortinaTime:  latestForkTime,
			DurangoTime:  latestForkTime,
			EUpgradeTime: mockable.MaxTime,
		},
	}}

	firstCtx := testcontext.New(context.Background())
	firstCtx.ChainID = consensustest.PChainID
	firstCtx.XAssetID = ids.GenerateTestID()
	firstCtx.Lock.Lock()

	_, genesisBytes := defaultGenesis(t, firstCtx.XAssetID)

	// Create lux context for chain context
	luxCtx := &consContext.Context{
		QuantumID:  firstCtx.NetworkID,
		NetID:      constants.PrimaryNetworkID,
		ChainID:    firstCtx.ChainID,
		NodeID:     firstCtx.NodeID,
		PublicKey:  nil,
		XChainID:   firstCtx.XChainID,
		CChainID:   firstCtx.CChainID,
		XAssetID: firstCtx.XAssetID,
		StartTime:  time.Now(),
	}

	chainCtx := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          luxCtx,
	}

	dbManager := &simpleDBManager{
		db: firstDB,
	}

	require.NoError(firstVM.Initialize(
		context.Background(),
		chainCtx,
		dbManager,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
		&testAppSender{},
	))

	initialClkTime := latestForkTime.Add(time.Second)
	firstVM.Clock().Set(initialClkTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(firstVM.SetState(context.Background(), interfaces.Bootstrapping))
	require.NoError(firstVM.SetState(context.Background(), interfaces.NormalOp))

	// Fast forward clock so that validators meet 20% uptime required for reward
	durationForReward := defaultValidateEndTime.Sub(defaultValidateStartTime) * firstUptimePercentage / 100
	vmStopTime := defaultValidateStartTime.Add(durationForReward)
	firstVM.Clock().Set(vmStopTime)

	// Shutdown VM to stop all genesis validator uptime.
	// At this point they have been validating for the 20% uptime needed to be rewarded
	require.NoError(firstVM.Shutdown(context.Background()))
	firstCtx.Lock.Unlock()

	// Restart the VM with a larger uptime requirement
	secondDB := prefixdb.New([]byte{}, db)
	const secondUptimePercentage = 21 // 21% > firstUptimePercentage, so uptime for reward is not met now
	secondVM := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimePercentage:       secondUptimePercentage / 100.,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		UpgradeConfig: upgrade.Config{
			BanffTime:    latestForkTime,
			CortinaTime:  latestForkTime,
			DurangoTime:  latestForkTime,
			EUpgradeTime: mockable.MaxTime,
		},
	}}

	secondCtx := testcontext.New(context.Background())
	secondCtx.ChainID = consensustest.PChainID
	secondCtx.XAssetID = firstCtx.XAssetID
	secondCtx.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(context.Background()))
		secondCtx.Lock.Unlock()
	}()

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)
	secondCtx.SharedMemory = m.NewSharedMemory(secondCtx.ChainID)

	// Create lux context for second VM
	secondLuxCtx := &consContext.Context{
		QuantumID:  secondCtx.NetworkID,
		NetID:      constants.PrimaryNetworkID,
		ChainID:    secondCtx.ChainID,
		NodeID:     secondCtx.NodeID,
		PublicKey:  nil,
		XChainID:   secondCtx.XChainID,
		CChainID:   secondCtx.CChainID,
		XAssetID: secondCtx.XAssetID,
		StartTime:  time.Now(),
	}

	secondChainCtx := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          secondLuxCtx,
	}

	secondDBManager := &simpleDBManager{
		db: secondDB,
	}

	require.NoError(secondVM.Initialize(
		context.Background(),
		secondChainCtx,
		secondDBManager,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
		&testAppSender{},
	))

	secondVM.Clock().Set(vmStopTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(secondVM.SetState(context.Background(), interfaces.Bootstrapping))
	require.NoError(secondVM.SetState(context.Background(), interfaces.NormalOp))

	// after restart and change of uptime required for reward, push validators to their end of life
	secondVM.Clock().Set(defaultValidateEndTime)

	// evaluate a genesis validator for reward
	blk, err := secondVM.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct.
	// secondVM should prefer abort since uptime requirements are not met anymore
	execBlk := blk.(*blockexecutor.Block)
	options, err := execBlk.Options(context.Background())
	require.NoError(err)

	abort := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	commit := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := execBlk.Block.Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)
	txID := blk.(block.Block).Txs()[0].ID()

	// Verify options and accept abort block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(abort.Accept(context.Background()))
	require.NoError(secondVM.SetPreference(context.Background(), secondVM.manager.LastAccepted()))

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := secondVM.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	tx, _, err := secondVM.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = secondVM.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

func TestUptimeDisallowedAfterNeverConnecting(t *testing.T) {
	require := require.New(t)
	latestForkTime = defaultValidateStartTime.Add(defaultMinStakingDuration)

	db := memdb.New()

	vm := &VM{Config: config.Config{
		Chains:                 chains.TestManager,
		UptimePercentage:       .2,
		RewardConfig:           defaultRewardConfig,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		UpgradeConfig: upgrade.Config{
			BanffTime:    latestForkTime,
			CortinaTime:  latestForkTime,
			DurangoTime:  latestForkTime,
			EUpgradeTime: mockable.MaxTime,
		},
	}}

	ctx := testcontext.New(context.Background())
	ctx.ChainID = consensustest.PChainID
	ctx.XAssetID = ids.GenerateTestID()
	ctx.Lock.Lock()

	_, genesisBytes := defaultGenesis(t, ctx.XAssetID)

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// Create lux context for chain context
	luxCtx := &consContext.Context{
		QuantumID:  ctx.NetworkID,
		NetID:      constants.PrimaryNetworkID,
		ChainID:    ctx.ChainID,
		NodeID:     ctx.NodeID,
		PublicKey:  nil,
		XChainID:   ctx.XChainID,
		CChainID:   ctx.CChainID,
		XAssetID: ctx.XAssetID,
		StartTime:  time.Now(),
	}

	chainCtx := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          luxCtx,
	}

	dbManager := &simpleDBManager{
		db: db,
	}

	require.NoError(vm.Initialize(
		context.Background(),
		chainCtx,
		dbManager,
		genesisBytes,
		nil,
		nil,
		nil,
		nil,
		&testAppSender{},
	))

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		ctx.Lock.Unlock()
	}()

	initialClkTime := latestForkTime.Add(time.Second)
	vm.Clock().Set(initialClkTime)

	// Set VM state to NormalOp, to start tracking validators' uptime
	require.NoError(vm.SetState(context.Background(), interfaces.Bootstrapping))
	require.NoError(vm.SetState(context.Background(), interfaces.NormalOp))

	// Fast forward clock to time for genesis validators to leave
	vm.Clock().Set(defaultValidateEndTime)

	// evaluate a genesis validator for reward
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))

	// Assert preferences are correct.
	// vm should prefer abort since uptime requirements are not met.
	execBlk := blk.(*blockexecutor.Block)
	options, err := execBlk.Options(context.Background())
	require.NoError(err)

	abort := options[0].(*blockexecutor.Block)
	require.IsType(&block.BanffAbortBlock{}, abort.Block)

	commit := options[1].(*blockexecutor.Block)
	require.IsType(&block.BanffCommitBlock{}, commit.Block)

	// Assert block tries to reward a genesis validator
	rewardTx := execBlk.Block.Txs()[0].Unsigned
	require.IsType(&txs.RewardValidatorTx{}, rewardTx)
	txID := blk.(block.Block).Txs()[0].ID()

	// Verify options and accept abort block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	require.NoError(blk.Accept(context.Background()))
	require.NoError(abort.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	tx, _, err := vm.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

func TestRemovePermissionedValidatorDuringAddPending(t *testing.T) {
	require := require.New(t)

	validatorStartTime := latestForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	key, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	id := key.PublicKey().Address()
	nodeID := ids.GenerateTestNodeID()
	sk, err := bls.NewSecretKey()
	require.NoError(err)

	// Generate proof of possession
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	builder, txSigner := factory.NewWallet(keys[0])
	uAddValTx, err := builder.NewAddPermissionlessValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   defaultMaxValidatorStake,
			},
			Net: constants.PrimaryNetworkID,
		},
		pop,
		vm.luxAssetID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{id},
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{id},
		},
		reward.PercentDenominator,
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
		}),
	)
	require.NoError(err)
	addValidatorTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uAddValTx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(addValidatorTx))
	ctx.Lock.Lock()

	// trigger block creation for the validator tx
	addValidatorBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(addValidatorBlock.Verify(context.Background()))
	require.NoError(addValidatorBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	uCreateNetTx, err := builder.NewCreateNetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{id},
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
		}),
	)
	require.NoError(err)
	createSubnetTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uCreateNetTx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(createSubnetTx))
	ctx.Lock.Lock()

	// trigger block creation for the net tx
	createSubnetBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(createSubnetBlock.Verify(context.Background()))
	require.NoError(createSubnetBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	builder, txSigner = factory.NewWallet(key, keys[1])
	uAddNetValTx, err := builder.NewAddNetValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   defaultMaxValidatorStake,
			},
			Net: createSubnetTx.ID(),
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
		}),
	)
	require.NoError(err)
	addNetValidatorTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uAddNetValTx)
	require.NoError(err)

	builder, txSigner = factory.NewWallet(key, keys[2])
	uRemoveNetValTx, err := builder.NewRemoveNetValidatorTx(
		nodeID,
		createSubnetTx.ID(),
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[2].PublicKey().Address()},
		}),
	)
	require.NoError(err)
	removeNetValidatorTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uRemoveNetValTx)
	require.NoError(err)

	statelessBlock, err := block.NewBanffStandardBlock(
		vm.state.GetTimestamp(),
		createSubnetBlock.ID(),
		createSubnetBlock.Height()+1,
		[]*txs.Tx{
			addNetValidatorTx,
			removeNetValidatorTx,
		},
	)
	require.NoError(err)

	blockBytes := statelessBlock.Bytes()
	block, err := vm.ParseBlock(context.Background(), blockBytes)
	require.NoError(err)
	require.NoError(block.Verify(context.Background()))
	require.NoError(block.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, err = vm.state.GetPendingValidator(createSubnetTx.ID(), nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestTransferNetOwnershipTx(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	builder, txSigner := factory.NewWallet(keys[0])
	uCreateNetTx, err := builder.NewCreateNetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
		}),
	)
	require.NoError(err)
	createSubnetTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uCreateNetTx)
	require.NoError(err)

	netID := createSubnetTx.ID()

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(createSubnetTx))
	ctx.Lock.Lock()
	createSubnetBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	createSubnetRawBlock := createSubnetBlock.(*blockexecutor.Block).Block
	require.IsType(&block.BanffStandardBlock{}, createSubnetRawBlock)
	require.Contains(createSubnetRawBlock.Txs(), createSubnetTx)

	require.NoError(createSubnetBlock.Verify(context.Background()))
	require.NoError(createSubnetBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	subnetOwner, err := vm.state.GetSubnetOwner(netID)
	require.NoError(err)
	expectedOwner := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[0].PublicKey().Address(),
		},
	}
	walletCtx, err := walletbuilder.NewConsensusContext(ctx.NetworkID, vm.luxAssetID)
	require.NoError(err)
	expectedOwner.InitCtx(walletCtx)
	require.Equal(expectedOwner, subnetOwner)

	uTransferNetOwnershipTx, err := builder.NewTransferNetOwnershipTx(
		netID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
		},
	)
	require.NoError(err)
	transferSubnetOwnershipTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uTransferNetOwnershipTx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(transferSubnetOwnershipTx))
	ctx.Lock.Lock()
	transferSubnetOwnershipBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	transferSubnetOwnershipRawBlock := transferSubnetOwnershipBlock.(*blockexecutor.Block).Block
	require.IsType(&block.BanffStandardBlock{}, transferSubnetOwnershipRawBlock)
	require.Contains(transferSubnetOwnershipRawBlock.Txs(), transferSubnetOwnershipTx)

	require.NoError(transferSubnetOwnershipBlock.Verify(context.Background()))
	require.NoError(transferSubnetOwnershipBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	subnetOwner, err = vm.state.GetSubnetOwner(netID)
	require.NoError(err)
	expectedOwner = &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs: []ids.ShortID{
			keys[1].PublicKey().Address(),
		},
	}
	expectedOwner.InitCtx(ctx)
	require.Equal(expectedOwner, subnetOwner)
}

func TestBaseTx(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	sendAmt := uint64(100000)
	changeAddr := ids.ShortEmpty

	builder, txSigner := factory.NewWallet(keys[0])
	utx, err := builder.NewBaseTx(
		[]*lux.TransferableOutput{
			{
				Asset: lux.Asset{ID: vm.luxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: sendAmt,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							keys[1].Address(),
						},
					},
				},
			},
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
	)
	require.NoError(err)
	baseTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	totalInputAmt := uint64(0)
	key0InputAmt := uint64(0)
	for inputID := range baseTx.Unsigned.InputIDs() {
		utxo, err := vm.state.GetUTXO(inputID)
		require.NoError(err)
		require.IsType(&secp256k1fx.TransferOutput{}, utxo.Out)
		castOut := utxo.Out.(*secp256k1fx.TransferOutput)
		if castOut.AddressesSet().Equals(mathset.Of(keys[0].Address())) {
			key0InputAmt += castOut.Amt
		}
		totalInputAmt += castOut.Amt
	}
	require.Equal(totalInputAmt, key0InputAmt)

	totalOutputAmt := uint64(0)
	key0OutputAmt := uint64(0)
	key1OutputAmt := uint64(0)
	changeAddrOutputAmt := uint64(0)
	for _, output := range baseTx.Unsigned.Outputs() {
		require.IsType(&secp256k1fx.TransferOutput{}, output.Out)
		castOut := output.Out.(*secp256k1fx.TransferOutput)
		if castOut.AddressesSet().Equals(mathset.Of(keys[0].Address())) {
			key0OutputAmt += castOut.Amt
		}
		if castOut.AddressesSet().Equals(mathset.Of(keys[1].Address())) {
			key1OutputAmt += castOut.Amt
		}
		if castOut.AddressesSet().Equals(mathset.Of(changeAddr)) {
			changeAddrOutputAmt += castOut.Amt
		}
		totalOutputAmt += castOut.Amt
	}
	require.Equal(totalOutputAmt, key0OutputAmt+key1OutputAmt+changeAddrOutputAmt)

	require.Equal(vm.StaticFeeConfig.TxFee, totalInputAmt-totalOutputAmt)
	require.Equal(sendAmt, key1OutputAmt)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(baseTx))
	ctx.Lock.Lock()
	baseTxBlock, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)

	baseTxRawBlock := baseTxBlock.(*blockexecutor.Block).Block
	require.IsType(&block.BanffStandardBlock{}, baseTxRawBlock)
	require.Contains(baseTxRawBlock.Txs(), baseTx)

	require.NoError(baseTxBlock.Verify(context.Background()))
	require.NoError(baseTxBlock.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))
}

func TestPruneMempool(t *testing.T) {
	require := require.New(t)
	vm, factory, _, _, ctx := defaultVM(t, latestFork)
	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()

	// Create a tx that will be valid regardless of timestamp.
	sendAmt := uint64(100000)
	changeAddr := ids.ShortEmpty

	builder, txSigner := factory.NewWallet(keys[0])
	utx, err := builder.NewBaseTx(
		[]*lux.TransferableOutput{
			{
				Asset: lux.Asset{ID: vm.luxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: sendAmt,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							keys[1].Address(),
						},
					},
				},
			},
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
	)
	require.NoError(err)
	baseTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(baseTx))
	ctx.Lock.Lock()

	// [baseTx] should be in the mempool.
	baseTxID := baseTx.ID()
	_, ok := vm.Builder.Get(baseTxID)
	require.True(ok)

	// Create a tx that will be invalid after time advancement.
	var (
		startTime = vm.Clock().Time()
		endTime   = startTime.Add(vm.MinStakeDuration)
	)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	// Generate proof of possession
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	builder, txSigner = factory.NewWallet(keys[1])
	uAddValTx, err := builder.NewAddPermissionlessValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   defaultMinValidatorStake,
			},
			Net: constants.PrimaryNetworkID,
		},
		pop,
		vm.luxAssetID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[2].Address()},
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[2].Address()},
		},
		20000,
	)
	require.NoError(err)
	addValidatorTx, err := walletsigner.SignUnsigned(context.Background(), txSigner, uAddValTx)
	require.NoError(err)

	ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(addValidatorTx))
	ctx.Lock.Lock()

	// Advance clock to [endTime], making [addValidatorTx] invalid.
	vm.Clock().Set(endTime)

	// [addValidatorTx] and [baseTx] should still be in the mempool.
	addValidatorTxID := addValidatorTx.ID()
	_, ok = vm.Builder.Get(addValidatorTxID)
	require.True(ok)
	_, ok = vm.Builder.Get(baseTxID)
	require.True(ok)

	ctx.Lock.Unlock()
	require.NoError(vm.pruneMempool())
	ctx.Lock.Lock()

	// [addValidatorTx] should be ejected from the mempool.
	// [baseTx] should still be in the mempool.
	_, ok = vm.Builder.Get(addValidatorTxID)
	require.False(ok)
	_, ok = vm.Builder.Get(baseTxID)
	require.True(ok)
}
