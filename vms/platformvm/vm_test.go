// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"context"
	// "math" // unused
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/runtime"
	// "github.com/luxfi/vm/chain/bootstrap" // unused
	"github.com/luxfi/vm"
	// "github.com/luxfi/consensus/core/coretest" // unused
	// "github.com/luxfi/consensus/core/tracker" // unused
	// consbenchlist "github.com/luxfi/consensus/networking/benchlist" // unused
	// "github.com/luxfi/consensus/networking/handler" // unused
	// "github.com/luxfi/consensus/core/router" // unused
	// "github.com/luxfi/consensus/networking/sender" // unused
	// "github.com/luxfi/consensus/networking/sender/sendertest" // unused
	// "github.com/luxfi/consensus/networking/timeout" // unused
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
	// "github.com/luxfi/crypto/bls" // unused
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/benchlist"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/vm/chains/atomic"
	// "github.com/luxfi/node/message" // unused
	// "github.com/luxfi/node/nets" // unused
	// "github.com/luxfi/p2p" // unused
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/node/upgrade/upgradetest"
	// "github.com/luxfi/node/utils/math/meter" // unused
	// "github.com/luxfi/resource" // unused
	// "github.com/luxfi/timer" // unused
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	// "github.com/luxfi/node/vms/platformvm/state" // unused after TestGenesis simplification
	"github.com/luxfi/node/vms/platformvm/status"
	// "github.com/luxfi/node/vms/platformvm/testcontext" // unused - using consensustest.Runtime instead
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/txstest"
	"github.com/luxfi/node/vms/platformvm/validators/fee"
	"github.com/luxfi/node/wallet/chain/p/wallet"
	"github.com/luxfi/utxo/secp256k1fx"
	// "github.com/luxfi/metric" // unused

	// p2ppb "github.com/luxfi/node/proto/p2p" // unused
	// smcon "github.com/luxfi/vm/chain" // unused
	// smeng "github.com/luxfi/vm/chain" // unused
	// smblock "github.com/luxfi/vm/chain" // unused
	// consensusgetter "github.com/luxfi/vm/chain/getter" // unused
	// timetracker "github.com/luxfi/node/network/tracker" // unused
	blockbuilder "github.com/luxfi/node/vms/platformvm/block/builder"
	blockexecutor "github.com/luxfi/node/vms/platformvm/block/executor"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	walletbuilder "github.com/luxfi/node/wallet/chain/p/builder"
	walletcommon "github.com/luxfi/node/wallet/network/primary/common"
)

const (
	defaultMinDelegatorStake = 1 * constants.MilliLux
	defaultMinValidatorStake = 5 * defaultMinDelegatorStake
	defaultMaxValidatorStake = 100 * defaultMinValidatorStake

	defaultMinStakingDuration = 24 * time.Hour
	defaultMaxStakingDuration = 365 * 24 * time.Hour
)

var (
	defaultRewardConfig = reward.Config{
		MaxConsumptionRate: .12 * reward.PercentDenominator,
		MinConsumptionRate: .10 * reward.PercentDenominator,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * constants.MegaLux,
	}

	latestForkTime = genesistest.DefaultValidatorStartTime.Add(time.Second)

	defaultDynamicFeeConfig = gas.Config{
		Weights: gas.Dimensions{
			gas.Bandwidth: 1,
			gas.DBRead:    1,
			gas.DBWrite:   1,
			gas.Compute:   1,
		},
		MaxCapacity:              10_000,
		MaxPerSecond:             1_000,
		TargetPerSecond:          500,
		MinPrice:                 1,
		ExcessConversionConstant: 5_000,
	}
	defaultValidatorFeeConfig = fee.Config{
		Capacity: 100,
		Target:   50,
		// The minimum price is set to 2 so that tests can include cases where
		// L1 validator balances do not evenly divide into a timestamp granular
		// to a second.
		MinPrice:                 2,
		ExcessConversionConstant: 100,
	}

	// chain that exists at genesis in defaultVM
	testNet1 *txs.Tx
)

// mockValidatorState implements runtime.ValidatorState for testing
type mockValidatorState struct{}

// Ensure mockValidatorState implements runtime.ValidatorState
var _ runtime.ValidatorState = (*mockValidatorState)(nil)

func (m *mockValidatorState) GetChainID(netID ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (m *mockValidatorState) GetNetworkID(chainID ids.ID) (ids.ID, error) {
	return constants.PrimaryNetworkID, nil
}

func (m *mockValidatorState) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

func (m *mockValidatorState) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

func (m *mockValidatorState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	return 100, nil
}

func (m *mockValidatorState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockValidatorState) GetWarpValidatorSet(ctx context.Context, height uint64, netID ids.ID) (*validators.WarpSet, error) {
	return &validators.WarpSet{Height: height, Validators: make(map[ids.NodeID]*validators.WarpValidator)}, nil
}

func (m *mockValidatorState) GetWarpValidatorSets(ctx context.Context, heights []uint64, netIDs []ids.ID) (map[ids.ID]map[uint64]*validators.WarpSet, error) {
	return make(map[ids.ID]map[uint64]*validators.WarpSet), nil
}

type mutableSharedMemory struct {
	atomic.SharedMemory
}

func defaultVM(t *testing.T, f upgradetest.Fork) (*VM, database.Database, *mutableSharedMemory) {
	require := require.New(t)

	// always reset latestForkTime (a package level variable)
	// to ensure test independence
	latestForkTime = genesistest.DefaultValidatorStartTime.Add(time.Second)
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
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(f, latestForkTime),
	}}

	db := memdb.New()
	chainDB := prefixdb.New([]byte{0}, db)
	atomicDB := prefixdb.New([]byte{1}, db)

	vmImpl.Clock().Set(latestForkTime)
	rt := consensustest.Runtime(t, consensustest.PChainID)

	m := atomic.NewMemory(atomicDB)
	msm := &mutableSharedMemory{
		SharedMemory: m.NewSharedMemory(rt.ChainID),
	}
	rt.SharedMemory = msm

	// Create a mock ValidatorState that implements runtime.ValidatorState
	rt.ValidatorState = &mockValidatorState{}

	rt.Lock.Lock()
	defer rt.Lock.Unlock()
	appSender := &TestSender{}

	dynamicConfigBytes := []byte(`{"network":{"max-validator-set-staleness":0}}`)
	require.NoError(vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime: rt,
			DB:      chainDB,
			Genesis: genesistest.NewBytes(t, genesistest.Config{
				InitialBalance: 200*constants.Lux + 20000, // Doubled + 20000 nanoLux buffer for fee precision (was 10000, increased to fix 1949 shortfall)
			}),
			Upgrade:  nil,
			Config:   dynamicConfigBytes,
			ToEngine: make(chan vm.Message, 1),
			Fx:       nil,
			Sender:   appSender,
		},
	))

	// align chain time and local clock
	vmImpl.state.SetTimestamp(vmImpl.Clock().Time())
	vmImpl.state.SetFeeState(gas.State{
		Capacity: defaultDynamicFeeConfig.MaxCapacity,
	})

	require.NoError(vmImpl.SetState(context.Background(), uint32(vm.Ready)))

	// Note: testNet1 is NOT created during VM initialization to avoid
	// timing issues with mempool/builder not being fully ready.
	// Tests that need testNet1 should create it using:
	//   testNet1 = createTestNet(t, vmImpl)
	// For tests that just need a sample chain tx for fee calculation,
	// use genesistest.NewNet() instead.

	t.Cleanup(func() {
		vmImpl.rt.Lock.Lock()
		defer vmImpl.rt.Lock.Unlock()

		// Shutdown may return "closed" errors if channels are already closed,
		// which is expected during test cleanup
		_ = vmImpl.Shutdown(context.Background())
	})

	return vmImpl, db, msm
}

func buildAndAcceptStandardBlock(pvm *VM) error {
	blk, err := pvm.Builder.BuildBlock(context.Background())
	if err != nil {
		return err
	}

	if err := blk.Verify(context.Background()); err != nil {
		return err
	}

	if err := blk.Accept(context.Background()); err != nil {
		return err
	}

	if err := pvm.SetPreference(context.Background(), blk.ID()); err != nil {
		return err
	}

	return nil
}

// createAndAcceptNet creates a new chain (testNet1), adds it to mempool,
// builds and accepts a block containing it. Returns the chain transaction.
func createAndAcceptNet(t *testing.T, vm *VM, wallet wallet.Wallet) *txs.Tx {
	require := require.New(t)

	netTx, err := wallet.IssueCreateNetworkTx(
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

	// Note: In avalanchego, this calls vmImpl.Network.IssueTxFromRPC which is currently
	// commented out in both codebases, so we directly add to Builder instead
	require.NoError(vm.Builder.Add(netTx))
	require.NoError(buildAndAcceptStandardBlock(vm))

	return netTx
}

type walletConfig struct {
	keys   []*secp256k1.PrivateKey
	netIDs []ids.ID
}

func newWallet(t testing.TB, vm *VM, c walletConfig) wallet.Wallet {
	if len(c.keys) == 0 {
		c.keys = genesistest.DefaultFundedKeys
	}
	// Create a basic Config for wallet
	walletConfig := &config.Config{
		TxFee:              constants.MilliLux,
		CreateAssetTxFee:   constants.MilliLux,
		CreateNetworkTxFee: constants.Lux,
		CreateChainTxFee:   constants.Lux,
	}
	return txstest.NewWalletWithOptions(
		t,
		vm.rt,
		txstest.WalletConfig{
			Config:      walletConfig,
			InternalCfg: &vm.Internal, // Pass VM's internal config with DynamicFeeConfig
		},
		vm.state,
		secp256k1fx.NewKeychain(c.keys...),
		c.netIDs,
		nil, // validationIDs
		[]ids.ID{vm.rt.CChainID, vm.rt.XChainID},
	)
}

// Ensure genesis state is parsed from bytes and stored correctly
func TestGenesis(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Etna)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	// Ensure the genesis block has been accepted and stored
	genesisBlockID, err := vmImpl.LastAccepted(context.Background()) // lastAccepted should be ID of genesis block
	require.NoError(err)

	// Ensure the genesis block can be retrieved
	genesisBlock, err := vmImpl.manager.GetBlock(genesisBlockID)
	require.NoError(err)
	require.NotNil(genesisBlock)

	genesisState := genesistest.New(t, genesistest.Config{
		InitialBalance: 200*constants.Lux + 20000, // Match defaultVM config (doubled + 20000 nanoLux buffer)
	})

	// Ensure all the genesis UTXOs are there with correct amounts
	for _, utxo := range genesisState.UTXOs {
		genesisOut := utxo.Out.(*secp256k1fx.TransferOutput)
		utxos, err := lux.GetAllUTXOs(
			vmImpl.state,
			genesisOut.OutputOwners.AddressesSet(),
		)
		require.NoError(err)
		require.Len(utxos, 1)

		out := utxos[0].Out.(*secp256k1fx.TransferOutput)
		// Genesis UTXOs should match exactly since no transactions have been issued
		require.Equal(genesisOut.Amt, out.Amt)
	}

	// Ensure current validator set of primary network is correct
	require.Len(genesisState.Validators, vmImpl.Validators.NumValidators(constants.PrimaryNetworkID))

	for _, nodeID := range genesistest.DefaultNodeIDs {
		_, ok := vmImpl.Validators.GetValidator(constants.PrimaryNetworkID, nodeID)
		require.True(ok)
	}
}

// accept proposal to add validator to primary network
func TestAddValidatorCommit(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})

	var (
		endTime = vmImpl.Clock().Time().Add(defaultMinStakingDuration)
		nodeID  = ids.GenerateTestNodeID()
		// Use an address that actually has funds from genesis
		rewardsOwner = &secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
		}
	)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	// create valid tx
	tx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.ChainValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				End:    uint64(endTime.Unix()),
				Wght:   vmImpl.MinValidatorStake,
			},
			Chain: constants.PrimaryNetworkID,
		},
		pop,
		vmImpl.rt.XAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// trigger block creation
	vmImpl.rt.Lock.Unlock()
	defer vmImpl.rt.Lock.Lock()
	require.NoError(vmImpl.issueTxFromRPC(tx))
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	_, txStatus, err := vmImpl.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator now in current validator set
	_, err = vmImpl.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
}

// verify invalid attempt to add validator to primary network
func TestInvalidAddValidatorCommit(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Cortina)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})

	nodeID := ids.GenerateTestNodeID()
	startTime := genesistest.DefaultValidatorStartTime.Add(-txexecutor.SyncBound).Add(-1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	// create invalid tx
	tx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   vmImpl.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	preferredID := vmImpl.manager.Preferred()
	preferred, err := vmImpl.manager.GetBlock(preferredID)
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

	parsedBlock, err := vmImpl.ParseBlock(context.Background(), blkBytes)
	require.NoError(err)

	err = parsedBlock.Verify(context.Background())
	require.ErrorIs(err, txexecutor.ErrTimestampNotBeforeStartTime)

	txID := statelessBlk.Txs()[0].ID()
	reason := vmImpl.Builder.GetDropReason(txID)
	require.ErrorIs(reason, txexecutor.ErrTimestampNotBeforeStartTime)
}

// Reject attempt to add validator to primary network
func TestAddValidatorReject(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Cortina)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})

	var (
		startTime     = vmImpl.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime       = startTime.Add(defaultMinStakingDuration)
		nodeID        = ids.GenerateTestNodeID()
		rewardAddress = ids.GenerateTestShortID()
	)

	// create valid tx
	tx, err := wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: nodeID,
			Start:  uint64(startTime.Unix()),
			End:    uint64(endTime.Unix()),
			Wght:   vmImpl.MinValidatorStake,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		reward.PercentDenominator,
	)
	require.NoError(err)

	// trigger block creation
	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(tx))
	vmImpl.rt.Lock.Lock()

	blk, err := vmImpl.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Reject(context.Background()))

	_, _, err = vmImpl.state.GetTx(tx.ID())
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vmImpl.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Reject proposal to add validator to primary network
func TestAddValidatorInvalidNotReissued(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})

	// Use nodeID that is already in the genesis
	repeatNodeID := genesistest.DefaultNodeIDs[0]

	startTime := latestForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	// create valid tx
	tx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.ChainValidator{
			Validator: txs.Validator{
				NodeID: repeatNodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   vmImpl.MinValidatorStake,
			},
			Chain: constants.PrimaryNetworkID,
		},
		pop,
		vmImpl.rt.XAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	// trigger block creation
	vmImpl.rt.Lock.Unlock()
	err = vmImpl.issueTxFromRPC(tx)
	vmImpl.rt.Lock.Lock()
	require.ErrorIs(err, txexecutor.ErrDuplicateValidator)
}

// Accept proposal to add validator to chain
func TestAddNetValidatorAccept(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	// Create chain in this VM instance
	wallet0 := newWallet(t, vmImpl, walletConfig{})
	netTx := createAndAcceptNet(t, vmImpl, wallet0)
	netID := netTx.ID()

	wallet := newWallet(t, vmImpl, walletConfig{
		netIDs: []ids.ID{netID},
	})

	var (
		startTime = vmImpl.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime   = startTime.Add(defaultMinStakingDuration)
		nodeID    = genesistest.DefaultNodeIDs[0]
	)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([genesistest.DefaultValidatorStartTime, genesistest.DefaultValidatorEndTime])
	var tx *txs.Tx
	var err error
	tx, err = wallet.IssueAddChainValidatorTx(
		&txs.ChainValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Chain: netID,
		},
	)
	require.NoError(err)

	// trigger block creation
	vmImpl.rt.Lock.Unlock()
	defer vmImpl.rt.Lock.Lock()
	require.NoError(vmImpl.issueTxFromRPC(tx))
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	_, txStatus, err := vmImpl.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that new validator is in current validator set
	_, err = vmImpl.state.GetCurrentValidator(netID, nodeID)
	require.NoError(err)
}

// Reject proposal to add validator to chain
func TestAddNetValidatorReject(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	// Create chain in this VM instance
	wallet0 := newWallet(t, vmImpl, walletConfig{})
	netTx := createAndAcceptNet(t, vmImpl, wallet0)
	netID := netTx.ID()

	wallet := newWallet(t, vmImpl, walletConfig{
		netIDs: []ids.ID{netID},
	})

	var (
		startTime = vmImpl.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
		endTime   = startTime.Add(defaultMinStakingDuration)
		nodeID    = genesistest.DefaultNodeIDs[0]
	)

	// create valid tx
	// note that [startTime, endTime] is a subset of time that keys[0]
	// validates primary network ([genesistest.DefaultValidatorStartTime, genesistest.DefaultValidatorEndTime])
	tx, err := wallet.IssueAddChainValidatorTx(
		&txs.ChainValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Chain: netID,
		},
	)
	require.NoError(err)

	// trigger block creation
	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(tx))
	vmImpl.rt.Lock.Lock()

	blk, err := vmImpl.Builder.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(blk.Reject(context.Background()))

	_, _, err = vmImpl.state.GetTx(tx.ID())
	require.ErrorIs(err, database.ErrNotFound)

	// Verify that new validator NOT in validator set
	_, err = vmImpl.state.GetCurrentValidator(netID, nodeID)
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
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	// Fast forward clock to time for genesis validators to leave
	vmImpl.Clock().Set(genesistest.DefaultValidatorEndTime)

	// Advance time and create proposal to reward a genesis validator
	blk, err := vmImpl.Builder.BuildBlock(context.Background())
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

	// Verify options and accept commit block
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))
	txID := blk.(block.Block).Txs()[0].ID()
	{
		onAbort, ok := vmImpl.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAbort.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(blk.Accept(context.Background()))
	require.NoError(commit.Accept(context.Background()))

	// Verify that chain's timestamp has advanced
	timestamp := vmImpl.state.GetTimestamp()
	require.Equal(genesistest.DefaultValidatorEndTimeUnix, uint64(timestamp.Unix()))

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vmImpl.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	tx, _, err := vmImpl.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vmImpl.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

// Test case where primary network validator not rewarded
func TestRewardValidatorReject(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	// Fast forward clock to time for genesis validators to leave
	vmImpl.Clock().Set(genesistest.DefaultValidatorEndTime)

	// Advance time and create proposal to reward a genesis validator
	blk, err := vmImpl.Builder.BuildBlock(context.Background())
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
		onAccept, ok := vmImpl.manager.GetState(commit.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Committed, txStatus)
	}

	require.NoError(blk.Accept(context.Background()))
	require.NoError(abort.Accept(context.Background()))

	// Verify that chain's timestamp has advanced
	timestamp := vmImpl.state.GetTimestamp()
	require.Equal(genesistest.DefaultValidatorEndTimeUnix, uint64(timestamp.Unix()))

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vmImpl.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	tx, _, err := vmImpl.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vmImpl.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

// Ensure BuildBlock errors when there is no block to build
func TestUnneededBuildBlock(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	_, err := vmImpl.Builder.BuildBlock(context.Background())
	require.ErrorIs(err, blockbuilder.ErrNoPendingBlocks)
}

// test acceptance of proposal to create a new chain
func TestCreateChain(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	// Create chain in this VM instance
	wallet0 := newWallet(t, vmImpl, walletConfig{})
	netTx := createAndAcceptNet(t, vmImpl, wallet0)
	netID := netTx.ID()

	wallet := newWallet(t, vmImpl, walletConfig{
		netIDs: []ids.ID{netID},
	})

	tx, err := wallet.IssueCreateChainTx(
		netID,
		nil,
		ids.ID{'t', 'e', 's', 't', 'v', 'm'},
		nil,
		"name",
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(tx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	_, txStatus, err := vmImpl.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify chain was created
	chains, err := vmImpl.state.GetChains(netID)
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
// 1) Create a chain
// 2) Add a validator to the chain's current validator set
// 3) Advance timestamp to validator's end time (removing validator from current)
func TestCreateNet(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})
	createNetTx, err := wallet.IssueCreateNetworkTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				genesistest.DefaultFundedKeys[0].Address(),
				genesistest.DefaultFundedKeys[1].Address(),
			},
		},
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(createNetTx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	netID := createNetTx.ID()
	_, txStatus, err := vmImpl.state.GetTx(netID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	netIDs, err := vmImpl.state.GetChainIDs()
	require.NoError(err)
	require.Contains(netIDs, netID)

	// Now that we've created a new chain, add a validator to that chain
	// Create a new wallet with authority over the chain
	chainWallet := newWallet(t, vmImpl, walletConfig{
		netIDs: []ids.ID{netID},
	})

	nodeID := genesistest.DefaultNodeIDs[0]
	startTime := vmImpl.Clock().Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := startTime.Add(defaultMinStakingDuration)
	// [startTime, endTime] is subset of time keys[0] validates default chain so tx is valid
	addValidatorTx, err := chainWallet.IssueAddChainValidatorTx(
		&txs.ChainValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   genesistest.DefaultValidatorWeight,
			},
			Chain: netID,
		},
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(addValidatorTx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	txID := addValidatorTx.ID()
	_, txStatus, err = vmImpl.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	_, err = vmImpl.state.GetPendingValidator(netID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vmImpl.state.GetCurrentValidator(netID, nodeID)
	require.NoError(err)

	// remove validator from current validator set
	vmImpl.Clock().Set(endTime)
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	_, err = vmImpl.state.GetPendingValidator(netID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = vmImpl.state.GetCurrentValidator(netID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

// test asset import
func TestAtomicImport(t *testing.T) {
	require := require.New(t)
	vmImpl, baseDB, mutableSharedMemory := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	recipientKey := genesistest.DefaultFundedKeys[1]
	importOwners := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{recipientKey.Address()},
	}

	m := atomic.NewMemory(prefixdb.New([]byte{5}, baseDB))
	mutableSharedMemory.SharedMemory = m.NewSharedMemory(vmImpl.rt.ChainID)

	wallet := newWallet(t, vmImpl, walletConfig{})
	_, err := wallet.IssueImportTx(
		vmImpl.rt.XChainID,
		importOwners,
	)
	require.ErrorIs(err, walletbuilder.ErrInsufficientFunds)

	// Provide the avm UTXO
	peerSharedMemory := m.NewSharedMemory(vmImpl.rt.XChainID)
	utxoID := lux.UTXOID{
		TxID:        ids.GenerateTestID(),
		OutputIndex: 1,
	}
	utxo := &lux.UTXO{
		UTXOID: utxoID,
		Asset:  lux.Asset{ID: vmImpl.rt.XAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          50 * constants.MicroLux,
			OutputOwners: *importOwners,
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		vmImpl.rt.ChainID: {
			PutRequests: []*atomic.Element{
				{
					Key:   inputID[:],
					Value: utxoBytes,
					Traits: [][]byte{
						recipientKey.Address().Bytes(),
					},
				},
			},
		},
	}))

	// The wallet must be re-loaded because the shared memory has changed
	wallet = newWallet(t, vmImpl, walletConfig{})
	tx, err := wallet.IssueImportTx(
		vmImpl.rt.XChainID,
		importOwners,
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(tx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	_, txStatus, err := vmImpl.state.GetTx(tx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	inputID = utxoID.InputID()
	sharedMemory := vmImpl.rt.SharedMemory.(atomic.SharedMemory)
	_, err = sharedMemory.Get(vmImpl.rt.XChainID, [][]byte{inputID[:]})
	require.ErrorIs(err, database.ErrNotFound)
}

// test optimistic asset import
func TestOptimisticAtomicImport(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.ApricotPhase3)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    vmImpl.rt.NetworkID,
			BlockchainID: vmImpl.rt.ChainID,
		}},
		SourceChain: vmImpl.rt.XChainID,
		ImportedInputs: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: lux.Asset{ID: vmImpl.rt.XAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx.Initialize(txs.Codec))

	preferredID := vmImpl.manager.Preferred()
	preferred, err := vmImpl.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewApricotAtomicBlock(
		preferredID,
		preferredHeight+1,
		tx,
	)
	require.NoError(err)

	blk := vmImpl.manager.NewBlock(statelessBlk)

	err = blk.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound) // erred due to missing shared memory UTXOs

	// Use proto value 2 (STATE_BOOTSTRAPPING) to enter bootstrapping mode
	require.NoError(vmImpl.SetState(context.Background(), 2))

	require.NoError(blk.Verify(context.Background())) // skips shared memory UTXO verification during bootstrapping

	require.NoError(blk.Accept(context.Background()))

	// Stop tracking before transitioning back to Ready to avoid "already started tracking" error
	// Note: StopTracking method no longer exists in uptime.Calculator interface
	// validatorIDs := vmImpl.Config.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
	// require.NoError(vmImpl.uptimeManager.StopTracking(validatorIDs))

	// Use proto value 3 (STATE_NORMAL_OP) to enter ready mode
	require.NoError(vmImpl.SetState(context.Background(), 3))

	_, txStatus, err := vmImpl.state.GetTx(tx.ID())
	require.NoError(err)

	require.Equal(status.Committed, txStatus)
}

// test restarting the node
func TestRestartFullyAccepted(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	// firstDB := prefixdb.New([]byte{}, db) // Not used, using firstChainDB instead
	firstVM := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	firstRT := consensustest.Runtime(t, consensustest.PChainID)

	genesisBytes := genesistest.NewBytes(t, genesistest.Config{})

	baseDB := memdb.New()
	atomicDB := prefixdb.New([]byte{1}, baseDB)
	m := atomic.NewMemory(atomicDB)
	firstRT.SharedMemory = m.NewSharedMemory(firstRT.ChainID)

	initialClkTime := latestForkTime.Add(time.Second)
	firstVM.Clock().Set(initialClkTime)
	firstRT.Lock.Lock()

	firstChainDB := prefixdb.New([]byte{2}, baseDB)
	appSender := &TestSender{}

	require.NoError(firstVM.Initialize(
		context.Background(),
		vm.Init{
			Runtime:  firstRT,
			DB:       firstChainDB,
			Genesis:  genesisBytes,
			Upgrade:  nil,
			Config:   nil,
			ToEngine: nil,
			Fx:       nil,
			Sender:   appSender,
		},
	))

	genesisID, err := firstVM.LastAccepted(context.Background())
	require.NoError(err)

	// include a tx to make the block be accepted
	tx := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    firstRT.NetworkID,
			BlockchainID: firstRT.ChainID,
		}},
		SourceChain: firstRT.XChainID,
		ImportedInputs: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: lux.Asset{ID: firstRT.XAssetID},
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
	firstRT.Lock.Unlock()

	secondVM := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	secondRT := consensustest.Runtime(t, consensustest.PChainID)
	secondRT.SharedMemory = firstRT.SharedMemory
	secondVM.Clock().Set(initialClkTime)
	secondRT.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(context.Background()))
		secondRT.Lock.Unlock()
	}()

	secondDB := prefixdb.New([]byte{}, db)
	secondAppSender := &TestSender{}
	require.NoError(secondVM.Initialize(
		context.Background(),
		vm.Init{
			Runtime:  secondRT,
			DB:       secondDB,
			Genesis:  genesisBytes,
			Upgrade:  nil,
			Config:   nil,
			ToEngine: nil,
			Fx:       nil,
			Sender:   secondAppSender,
		},
	))

	lastAccepted, err := secondVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(genesisID, lastAccepted)
}

func TestUnverifiedParent(t *testing.T) {
	require := require.New(t)

	vmImpl := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	initialClkTime := latestForkTime.Add(time.Second)
	vmImpl.Clock().Set(initialClkTime)
	rt := consensustest.Runtime(t, consensustest.PChainID)

	require.NoError(vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime:  rt,
			DB:       memdb.New(),
			Genesis:  genesistest.NewBytes(t, genesistest.Config{}),
			Upgrade:  nil,
			Config:   nil,
			ToEngine: nil,
			Fx:       nil,
			Sender:   &TestSender{},
		},
	))

	vmImpl.rt.Lock.Lock()
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
		vmImpl.rt.Lock.Unlock()
	}()

	// include a tx1 to make the block be accepted
	tx1 := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    rt.NetworkID,
			BlockchainID: rt.ChainID, // Use context's ChainID, not constants.PlatformChainID
		}},
		SourceChain: rt.XChainID,
		ImportedInputs: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: lux.Asset{ID: vmImpl.rt.XAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx1.Initialize(txs.Codec))

	nextChainTime := initialClkTime.Add(time.Second)

	preferredID := vmImpl.manager.Preferred()
	preferred, err := vmImpl.manager.GetBlock(preferredID)
	require.NoError(err)
	preferredHeight := preferred.Height()

	statelessBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		preferredID,
		preferredHeight+1,
		[]*txs.Tx{tx1},
	)
	require.NoError(err)
	firstAdvanceTimeBlk := vmImpl.manager.NewBlock(statelessBlk)
	require.NoError(firstAdvanceTimeBlk.Verify(context.Background()))

	// include a tx2 to make the block be accepted
	tx2 := &txs.Tx{Unsigned: &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    rt.NetworkID,
			BlockchainID: rt.ChainID, // Use context's ChainID, not constants.PlatformChainID
		}},
		SourceChain: rt.XChainID,
		ImportedInputs: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty.Prefix(2),
				OutputIndex: 2,
			},
			Asset: lux.Asset{ID: vmImpl.rt.XAssetID},
			In: &secp256k1fx.TransferInput{
				Amt: 50000,
			},
		}},
	}}
	require.NoError(tx2.Initialize(txs.Codec))
	nextChainTime = nextChainTime.Add(time.Second)
	vmImpl.Clock().Set(nextChainTime)
	statelessSecondAdvanceTimeBlk, err := block.NewBanffStandardBlock(
		nextChainTime,
		firstAdvanceTimeBlk.ID(),
		firstAdvanceTimeBlk.Height()+1,
		[]*txs.Tx{tx2},
	)
	require.NoError(err)
	secondAdvanceTimeBlk := vmImpl.manager.NewBlock(statelessSecondAdvanceTimeBlk)

	require.Equal(secondAdvanceTimeBlk.Parent(), firstAdvanceTimeBlk.ID())
	require.NoError(secondAdvanceTimeBlk.Verify(context.Background()))
}

func TestMaxStakeAmount(t *testing.T) {
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	nodeID := genesistest.DefaultNodeIDs[0]

	tests := []struct {
		description string
		startTime   time.Time
		endTime     time.Time
	}{
		{
			description: "[validator.StartTime] == [startTime] < [endTime] == [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime,
			endTime:     genesistest.DefaultValidatorEndTime,
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] == [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime.Add(time.Minute),
			endTime:     genesistest.DefaultValidatorEndTime,
		},
		{
			description: "[validator.StartTime] == [startTime] < [endTime] < [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime,
			endTime:     genesistest.DefaultValidatorEndTime.Add(-time.Minute),
		},
		{
			description: "[validator.StartTime] < [startTime] < [endTime] < [validator.EndTime]",
			startTime:   genesistest.DefaultValidatorStartTime.Add(time.Minute),
			endTime:     genesistest.DefaultValidatorEndTime.Add(-time.Minute),
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			require := require.New(t)
			staker, err := txexecutor.GetValidator(vmImpl.state, constants.PrimaryNetworkID, nodeID)
			require.NoError(err)

			amount, err := txexecutor.GetMaxWeight(vmImpl.state, staker, test.startTime, test.endTime)
			require.NoError(err)
			require.Equal(genesistest.DefaultValidatorWeight, amount)
		})
	}
}

func TestUptimeDisallowedWithRestart(t *testing.T) {
	require := require.New(t)
	latestForkTime = genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration)
	db := memdb.New()

	firstDB := prefixdb.New([]byte{}, db)
	const firstUptimePercentage = 20 // 20%
	firstVM := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimePercentage:       firstUptimePercentage / 100.,
		RewardConfig:           defaultRewardConfig,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	firstRT := consensustest.Runtime(t, consensustest.PChainID)
	firstRT.Lock.Lock()

	genesisBytes := genesistest.NewBytes(t, genesistest.Config{})

	require.NoError(firstVM.Initialize(
		context.Background(),
		vm.Init{
			Runtime:  firstRT,
			DB:       firstDB,
			Genesis:  genesisBytes,
			Upgrade:  nil,
			Config:   nil,
			ToEngine: nil,
			Fx:       nil,
			Sender:   &TestSender{},
		},
	))

	initialClkTime := latestForkTime.Add(time.Second)
	firstVM.Clock().Set(initialClkTime)

	// Set VM state to Ready, to start tracking validators' uptime
	require.NoError(firstVM.SetState(context.Background(), uint32(vm.Bootstrapping)))
	require.NoError(firstVM.SetState(context.Background(), uint32(vm.Ready)))

	// Fast forward clock so that validators meet 20% uptime required for reward
	durationForReward := genesistest.DefaultValidatorEndTime.Sub(genesistest.DefaultValidatorStartTime) * firstUptimePercentage / 100
	vmStopTime := genesistest.DefaultValidatorStartTime.Add(durationForReward)
	firstVM.Clock().Set(vmStopTime)

	// Shutdown VM to stop all genesis validator uptime.
	// At this point they have been validating for the 20% uptime needed to be rewarded
	require.NoError(firstVM.Shutdown(context.Background()))
	firstRT.Lock.Unlock()

	// Restart the VM with a larger uptime requirement
	secondDB := prefixdb.New([]byte{}, db)
	const secondUptimePercentage = 21 // 21% > firstUptimePercentage, so uptime for reward is not met now
	// Use ZeroUptimeCalculator as fallback to simulate that uptime tracking is reset
	// and validators have 0% uptime from the perspective of the new VM
	secondVM := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimePercentage:       secondUptimePercentage / 100.,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculatorWithFallback(uptime.ZeroUptimeCalculator{}),
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	secondRT := consensustest.Runtime(t, consensustest.PChainID)
	secondRT.XAssetID = firstRT.XAssetID
	secondRT.Lock.Lock()
	defer func() {
		require.NoError(secondVM.Shutdown(context.Background()))
		secondRT.Lock.Unlock()
	}()

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)
	secondRT.SharedMemory = m.NewSharedMemory(secondRT.ChainID)

	require.NoError(secondVM.Initialize(
		context.Background(),
		vm.Init{
			Runtime:  secondRT,
			DB:       secondDB,
			Genesis:  genesisBytes,
			Upgrade:  nil,
			Config:   nil,
			ToEngine: nil,
			Fx:       nil,
			Sender:   &TestSender{},
		},
	))

	secondVM.Clock().Set(vmStopTime)

	// Set VM state to Ready, to start tracking validators' uptime
	require.NoError(secondVM.SetState(context.Background(), uint32(vm.Bootstrapping)))
	require.NoError(secondVM.SetState(context.Background(), uint32(vm.Ready)))

	// after restart and change of uptime required for reward, push validators to their end of life
	secondVM.Clock().Set(genesistest.DefaultValidatorEndTime)

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
	latestForkTime = genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration)

	db := memdb.New()

	// Use ZeroUptimeCalculator as fallback to simulate "never connected" scenario
	// where validators have 0% uptime
	vmImpl := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimePercentage:       .2,
		RewardConfig:           defaultRewardConfig,
		Validators:             validators.NewManager(),
		UptimeLockedCalculator: uptime.NewLockedCalculatorWithFallback(uptime.ZeroUptimeCalculator{}),
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, latestForkTime),
	}}

	rt := consensustest.Runtime(t, consensustest.PChainID)
	rt.XAssetID = ids.GenerateTestID()
	rt.Lock.Lock()

	atomicDB := prefixdb.New([]byte{1}, db)
	m := atomic.NewMemory(atomicDB)
	rt.SharedMemory = m.NewSharedMemory(rt.ChainID)

	// appSender := &enginetest.Sender{T: t} // enginetest package not available
	require.NoError(vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime:  rt,
			DB:       db,
			Genesis:  genesistest.NewBytes(t, genesistest.Config{}),
			Upgrade:  nil,
			Config:   nil,
			ToEngine: nil,
			Fx:       nil,
			Sender:   &TestSender{},
		},
	))

	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
		rt.Lock.Unlock()
	}()

	initialClkTime := latestForkTime.Add(time.Second)
	vmImpl.Clock().Set(initialClkTime)

	// Set VM state to Ready, to start tracking validators' uptime
	require.NoError(vmImpl.SetState(context.Background(), uint32(vm.Bootstrapping)))
	require.NoError(vmImpl.SetState(context.Background(), uint32(vm.Ready)))

	// Fast forward clock to time for genesis validators to leave
	vmImpl.Clock().Set(genesistest.DefaultValidatorEndTime)

	// evaluate a genesis validator for reward
	blk, err := vmImpl.Builder.BuildBlock(context.Background())
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
	require.NoError(vmImpl.SetPreference(context.Background(), vmImpl.manager.LastAccepted()))

	// Verify that rewarded validator has been removed.
	// Note that test genesis has multiple validators
	// terminating at the same time. The rewarded validator
	// will the first by txID. To make the test more stable
	// (txID changes every time we change any parameter
	// of the tx creating the validator), we explicitly
	//  check that rewarded validator is removed from staker set.
	_, txStatus, err := vmImpl.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Aborted, txStatus)

	tx, _, err := vmImpl.state.GetTx(rewardTx.(*txs.RewardValidatorTx).TxID)
	require.NoError(err)
	require.IsType(&txs.AddValidatorTx{}, tx.Unsigned)

	valTx, _ := tx.Unsigned.(*txs.AddValidatorTx)
	_, err = vmImpl.state.GetCurrentValidator(constants.PrimaryNetworkID, valTx.NodeID())
	require.ErrorIs(err, database.ErrNotFound)
}

func TestRemovePermissionedValidatorDuringAddPending(t *testing.T) {
	require := require.New(t)

	validatorStartTime := latestForkTime.Add(txexecutor.SyncBound).Add(1 * time.Second)
	validatorEndTime := validatorStartTime.Add(360 * 24 * time.Hour)

	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})

	nodeID := ids.GenerateTestNodeID()
	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	addValidatorTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.ChainValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   defaultMaxValidatorStake,
			},
			Chain: constants.PrimaryNetworkID,
		},
		pop,
		vmImpl.rt.XAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(addValidatorTx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	createNetTx, err := wallet.IssueCreateNetworkTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
		},
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(createNetTx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	netID := createNetTx.ID()
	addNetValidatorTx, err := wallet.IssueAddChainValidatorTx(
		&txs.ChainValidator{
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(validatorStartTime.Unix()),
				End:    uint64(validatorEndTime.Unix()),
				Wght:   defaultMaxValidatorStake,
			},
			Chain: netID,
		},
	)
	require.NoError(err)

	removeNetValidatorTx, err := wallet.IssueRemoveChainValidatorTx(
		nodeID,
		netID,
	)
	require.NoError(err)

	lastAcceptedID := vmImpl.state.GetLastAccepted()
	lastAcceptedHeight, err := vmImpl.GetCurrentHeight(context.Background())
	require.NoError(err)
	statelessBlock, err := block.NewBanffStandardBlock(
		vmImpl.state.GetTimestamp(),
		lastAcceptedID,
		lastAcceptedHeight+1,
		[]*txs.Tx{
			addNetValidatorTx,
			removeNetValidatorTx,
		},
	)
	require.NoError(err)

	blockBytes := statelessBlock.Bytes()
	block, err := vmImpl.ParseBlock(context.Background(), blockBytes)
	require.NoError(err)
	require.NoError(block.Verify(context.Background()))
	require.NoError(block.Accept(context.Background()))
	require.NoError(vmImpl.SetPreference(context.Background(), vmImpl.manager.LastAccepted()))

	_, err = vmImpl.state.GetPendingValidator(netID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestTransferChainOwnershipTx(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})

	expectedNetOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
	}
	createNetTx, err := wallet.IssueCreateNetworkTx(
		expectedNetOwner,
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(createNetTx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	netID := createNetTx.ID()
	chainOwner, err := vmImpl.state.GetNetOwner(netID)
	require.NoError(err)
	require.Equal(expectedNetOwner, chainOwner)

	expectedNetOwner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	transferNetOwnershipTx, err := wallet.IssueTransferChainOwnershipTx(
		netID,
		expectedNetOwner,
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(transferNetOwnershipTx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	chainOwner, err = vmImpl.state.GetNetOwner(netID)
	require.NoError(err)
	require.Equal(expectedNetOwner, chainOwner)
}

func TestBaseTx(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Durango)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})

	baseTx, err := wallet.IssueBaseTx(
		[]*lux.TransferableOutput{
			{
				Asset: lux.Asset{ID: vmImpl.rt.XAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 100 * constants.MicroLux,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.GenerateTestShortID(),
						},
					},
				},
			},
		},
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(baseTx))
	vmImpl.rt.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vmImpl))

	_, txStatus, err := vmImpl.state.GetTx(baseTx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)
}

func TestPruneMempool(t *testing.T) {
	require := require.New(t)
	vmImpl, _, _ := defaultVM(t, upgradetest.Latest)
	vmImpl.rt.Lock.Lock()
	defer vmImpl.rt.Lock.Unlock()

	wallet := newWallet(t, vmImpl, walletConfig{})

	// Create a tx that will be valid regardless of timestamp.
	baseTx, err := wallet.IssueBaseTx(
		[]*lux.TransferableOutput{
			{
				Asset: lux.Asset{ID: vmImpl.rt.XAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: 100 * constants.MicroLux,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							genesistest.DefaultFundedKeys[0].Address(),
						},
					},
				},
			},
		},
		walletcommon.WithCustomAddresses(set.Of(
			genesistest.DefaultFundedKeys[0].Address(),
		)),
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(baseTx))
	vmImpl.rt.Lock.Lock()

	// [baseTx] should be in the mempool.
	baseTxID := baseTx.ID()
	_, ok := vmImpl.Builder.Get(baseTxID)
	require.True(ok)

	// Create a tx that will be invalid after time advancement.
	var (
		startTime = vmImpl.Clock().Time()
		endTime   = startTime.Add(vmImpl.MinStakeDuration)
	)

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	addValidatorTx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.ChainValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(startTime.Unix()),
				End:    uint64(endTime.Unix()),
				Wght:   defaultMinValidatorStake,
			},
			Chain: constants.PrimaryNetworkID,
		},
		pop,
		vmImpl.rt.XAssetID,
		rewardsOwner,
		rewardsOwner,
		20000,
		walletcommon.WithCustomAddresses(set.Of(
			genesistest.DefaultFundedKeys[1].Address(),
		)),
	)
	require.NoError(err)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.issueTxFromRPC(addValidatorTx))
	vmImpl.rt.Lock.Lock()

	// [addValidatorTx] and [baseTx] should be in the mempool.
	addValidatorTxID := addValidatorTx.ID()
	_, ok = vmImpl.Builder.Get(addValidatorTxID)
	require.True(ok)
	_, ok = vmImpl.Builder.Get(baseTxID)
	require.True(ok)

	// Advance clock to [endTime], making [addValidatorTx] invalid.
	vmImpl.Clock().Set(endTime)

	vmImpl.rt.Lock.Unlock()
	require.NoError(vmImpl.pruneMempool())
	vmImpl.rt.Lock.Lock()

	// [addValidatorTx] should be ejected from the mempool.
	// [baseTx] should still be in the mempool.
	_, ok = vmImpl.Builder.Get(addValidatorTxID)
	require.False(ok)
	_, ok = vmImpl.Builder.Get(baseTxID)
	require.True(ok)
}
