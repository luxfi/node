// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"golang.org/x/exp/maps"

	consContext "github.com/luxfi/consensus/context"
	linearblock "github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/interfaces"
	"github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/consensus/uptime"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/api"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/vms/platformvm/txs/txstest"
	"github.com/luxfi/node/vms/platformvm/upgrade"
	"github.com/luxfi/node/vms/secp256k1fx"

	blockexecutor "github.com/luxfi/node/vms/platformvm/block/executor"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	walletsigner "github.com/luxfi/node/wallet/chain/p/signer"
	walletcommon "github.com/luxfi/node/wallet/net/primary/common"
)

const (
	startPrimaryWithBLS uint8 = iota
	startNetValidator

	failedValidatorSnapshotString = "could not take validators snapshot: "
	failedBuildingEventSeqString  = "failed building events sequence: "
)

var errEmptyEventsList = errors.New("empty events list")

// for a given (permissioned) subnet, the test stakes and restakes multiple
// times a node as a primary and net validator. The BLS key of the node is
// changed across staking periods, and it can even be nil. We test that
// GetValidatorSet returns the correct primary and net validators data, with
// the right BLS key version at all relevant heights.
func TestGetValidatorsSetProperty(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1685887576153675816)
	// properties := gopter.NewProperties(parameters)

	properties.Property("check GetValidatorSet", prop.ForAll(
		func(events []uint8) string {
			vm, netID, err := buildVM(t)
			if err != nil {
				return "failed building vm: " + err.Error()
			}
			vm.lock.Lock()
			defer func() {
				_ = vm.Shutdown(context.Background())
				vm.lock.Unlock()
			}()
			nodeID := ids.GenerateTestNodeID()

			currentTime := defaultGenesisTime
			vm.nodeClock.Set(currentTime)
			vm.state.SetTimestamp(currentTime)

			// build a valid sequence of validators start/end times, given the
			// random events sequence received as test input
			validatorsTimes, err := buildTimestampsList(events, currentTime, nodeID)
			if err != nil {
				return "failed building events sequence: " + err.Error()
			}

			validatorSetByHeightAndSubnet := make(map[uint64]map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
			if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
				return failedValidatorSnapshotString + err.Error()
			}

			// insert validator sequence
			var (
				currentPrimaryValidator = (*state.Staker)(nil)
				currentNetValidator     = (*state.Staker)(nil)
			)
			for _, ev := range validatorsTimes {
				// at each step we remove at least a net validator
				if currentNetValidator != nil {
					err := terminateNetValidator(vm, currentNetValidator)
					if err != nil {
						return "could not terminate current net validator: " + err.Error()
					}
					currentNetValidator = nil

					if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
						return failedValidatorSnapshotString + err.Error()
					}
				}

				switch ev.eventType {
				case startNetValidator:
					currentNetValidator, err = addNetValidator(vm, ev, netID)
					if err != nil {
						return "could not add net validator: " + err.Error()
					}
					if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
						return failedValidatorSnapshotString + err.Error()
					}

				case startPrimaryWithBLS:
					// when adding a primary validator, also remove the current
					// primary one
					if currentPrimaryValidator != nil {
						err := terminatePrimaryValidator(vm, currentPrimaryValidator)
						if err != nil {
							return "could not terminate current primary validator: " + err.Error()
						}
						// no need to nil current primary validator, we'll
						// reassign immediately

						if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
							return failedValidatorSnapshotString + err.Error()
						}
					}
					currentPrimaryValidator, err = addPrimaryValidatorWithBLSKey(vm, ev)
					if err != nil {
						return "could not add primary validator with BLS key: " + err.Error()
					}
					if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
						return failedValidatorSnapshotString + err.Error()
					}

				default:
					return fmt.Sprintf("unexpected staker type: %v", ev.eventType)
				}
			}

			// Checks: let's look back at validator sets at previous heights and
			// make sure they match the snapshots already taken
			snapshotHeights := maps.Keys(validatorSetByHeightAndSubnet)
			sort.Slice(snapshotHeights, func(i, j int) bool { return snapshotHeights[i] < snapshotHeights[j] })
			for idx, snapShotHeight := range snapshotHeights {
				lastAcceptedHeight, err := vm.GetCurrentHeight(context.Background())
				if err != nil {
					return err.Error()
				}

				nextSnapShotHeight := lastAcceptedHeight + 1
				if idx != len(snapshotHeights)-1 {
					nextSnapShotHeight = snapshotHeights[idx+1]
				}

				// within [snapShotHeight] and [nextSnapShotHeight], the validator set
				// does not change and must be equal to snapshot at [snapShotHeight]
				for height := snapShotHeight; height < nextSnapShotHeight; height++ {
					for netID, validatorsSet := range validatorSetByHeightAndSubnet[snapShotHeight] {
						res, err := vm.GetValidatorSet(context.Background(), height, netID)
						if err != nil {
							return fmt.Sprintf("failed GetValidatorSet at height %v: %v", height, err)
						}
						if !reflect.DeepEqual(validatorsSet, res) {
							return "failed validators set comparison"
						}
					}
				}
			}

			return ""
		},
		gen.SliceOfN(
			10,
			gen.OneConstOf(
				startPrimaryWithBLS,
				startNetValidator,
			),
		).SuchThat(func(v interface{}) bool {
			list := v.([]uint8)
			return len(list) > 0 && list[0] == startPrimaryWithBLS
		}),
	))

	properties.TestingRun(t)
}

func takeValidatorsSnapshotAtCurrentHeight(vm *VM, validatorsSetByHeightAndNet map[uint64]map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput) error {
	if validatorsSetByHeightAndNet == nil {
		validatorsSetByHeightAndNet = make(map[uint64]map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
	}

	lastBlkID := vm.state.GetLastAccepted()
	lastBlk, err := vm.state.GetStatelessBlock(lastBlkID)
	if err != nil {
		return err
	}
	height := lastBlk.Height()
	validatorsSetBySubnet, ok := validatorsSetByHeightAndNet[height]
	if !ok {
		validatorsSetByHeightAndNet[height] = make(map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
		validatorsSetBySubnet = validatorsSetByHeightAndNet[height]
	}

	stakerIt, err := vm.state.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	defer stakerIt.Release()
	for stakerIt.Next() {
		v := stakerIt.Value()
		validatorsSet, ok := validatorsSetBySubnet[v.NetID]
		if !ok {
			validatorsSetBySubnet[v.NetID] = make(map[ids.NodeID]*validators.GetValidatorOutput)
			validatorsSet = validatorsSetBySubnet[v.NetID]
		}

		blsKey := v.PublicKey
		if v.NetID != constants.PrimaryNetworkID {
			// pick bls key from primary validator
			s, err := vm.state.GetCurrentValidator(constants.PlatformChainID, v.NodeID)
			if err != nil {
				return err
			}
			blsKey = s.PublicKey
		}

		pkBytes := bls.PublicKeyToUncompressedBytes(blsKey)
		validatorsSet[v.NodeID] = &validators.GetValidatorOutput{
			NodeID:    v.NodeID,
			PublicKey: pkBytes,
			Weight:    v.Weight,
		}
	}
	return nil
}

func addNetValidator(vm *VM, data *validatorInputData, netID ids.ID) (*state.Staker, error) {
	// Create a nil shared memory for testing
	factory := txstest.NewWalletFactoryWithAssets(context.Background(), nil, &vm.Config, vm.state, vm.luxAssetID)
	builder, signer := factory.NewWallet(keys[0], keys[1])
	utx, err := builder.NewAddNetValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: data.nodeID,
				Start:  uint64(data.startTime.Unix()),
				End:    uint64(data.endTime.Unix()),
				Wght:   vm.Config.MinValidatorStake,
			},
			Net: netID,
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("could not build AddNetValidatorTx: %w", err)
	}
	tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
	if err != nil {
		return nil, fmt.Errorf("could not sign AddNetValidatorTx: %w", err)
	}
	return internalAddValidator(vm, tx)
}

func addPrimaryValidatorWithBLSKey(vm *VM, data *validatorInputData) (*state.Staker, error) {
	addr := keys[0].PublicKey().Address()

	sk, err := bls.NewSecretKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate BLS key: %w", err)
	}

	// Create a nil shared memory for testing
	factory := txstest.NewWalletFactoryWithAssets(context.Background(), nil, &vm.Config, vm.state, vm.luxAssetID)
	builder, txSigner := factory.NewWallet(keys[0], keys[1])

	// Generate proof of possession
	pop, err := signer.NewProofOfPossession(sk)
	if err != nil {
		return nil, fmt.Errorf("failed to create proof of possession: %w", err)
	}

	utx, err := builder.NewAddPermissionlessValidatorTx(
		&txs.NetValidator{
			Validator: txs.Validator{
				NodeID: data.nodeID,
				Start:  uint64(data.startTime.Unix()),
				End:    uint64(data.endTime.Unix()),
				Wght:   vm.Config.MinValidatorStake,
			},
			Net: constants.PrimaryNetworkID,
		},
		pop,
		vm.luxAssetID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		},
		reward.PercentDenominator,
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{addr},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("could not build AddPermissionlessValidatorTx: %w", err)
	}
	tx, err := walletsigner.SignUnsigned(context.Background(), txSigner, utx)
	if err != nil {
		return nil, fmt.Errorf("could not sign AddPermissionlessValidatorTx: %w", err)
	}
	return internalAddValidator(vm, tx)
}

func internalAddValidator(vm *VM, signedTx *txs.Tx) (*state.Staker, error) {
	vm.lock.Unlock()
	err := vm.issueTxFromRPC(signedTx)
	vm.lock.Lock()

	if err != nil {
		return nil, fmt.Errorf("could not add tx to mempool: %w", err)
	}

	blk, err := vm.Builder.BuildBlock(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed building block: %w", err)
	}
	if err := blk.Verify(context.Background()); err != nil {
		return nil, fmt.Errorf("failed verifying block: %w", err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		return nil, fmt.Errorf("failed accepting block: %w", err)
	}
	if err := vm.SetPreference(context.Background(), vm.manager.LastAccepted()); err != nil {
		return nil, fmt.Errorf("failed setting preference: %w", err)
	}

	stakerTx := signedTx.Unsigned.(txs.Staker)
	return vm.state.GetCurrentValidator(stakerTx.NetID(), stakerTx.NodeID())
}

func terminateNetValidator(vm *VM, validator *state.Staker) error {
	currentTime := validator.EndTime
	vm.nodeClock.Set(currentTime)
	vm.state.SetTimestamp(currentTime)

	blk, err := vm.Builder.BuildBlock(context.Background())
	if err != nil {
		return fmt.Errorf("failed building block: %w", err)
	}
	if err := blk.Verify(context.Background()); err != nil {
		return fmt.Errorf("failed verifying block: %w", err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		return fmt.Errorf("failed accepting block: %w", err)
	}
	if err := vm.SetPreference(context.Background(), vm.manager.LastAccepted()); err != nil {
		return fmt.Errorf("failed setting preference: %w", err)
	}

	return nil
}

func terminatePrimaryValidator(vm *VM, validator *state.Staker) error {
	currentTime := validator.EndTime
	vm.nodeClock.Set(currentTime)
	vm.state.SetTimestamp(currentTime)

	blk, err := vm.Builder.BuildBlock(context.Background())
	if err != nil {
		return fmt.Errorf("failed building block: %w", err)
	}
	if err := blk.Verify(context.Background()); err != nil {
		return fmt.Errorf("failed verifying block: %w", err)
	}

	// For now, skip oracle block processing
	options := []chain.Block{}
	if len(options) == 0 {
		// No oracle options to process
		_ = options // Mark as intentionally unused
	}

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*block.BanffCommitBlock)
	if !ok {
		return fmt.Errorf("failed retrieving commit option: %w", err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		return fmt.Errorf("failed accepting block: %w", err)
	}

	if err := commit.Verify(context.Background()); err != nil {
		return fmt.Errorf("failed verifying commit block: %w", err)
	}
	if err := commit.Accept(context.Background()); err != nil {
		return fmt.Errorf("failed accepting commit block: %w", err)
	}

	if err := vm.SetPreference(context.Background(), vm.manager.LastAccepted()); err != nil {
		return fmt.Errorf("failed setting preference: %w", err)
	}

	return nil
}

type validatorInputData struct {
	eventType uint8
	startTime time.Time
	endTime   time.Time
	nodeID    ids.NodeID
	publicKey *bls.PublicKey
}

// buildTimestampsList creates validators start and end time, given the event list.
// output is returned as a list of validatorInputData
func buildTimestampsList(events []uint8, currentTime time.Time, nodeID ids.NodeID) ([]*validatorInputData, error) {
	res := make([]*validatorInputData, 0, len(events))

	currentTime = currentTime.Add(txexecutor.SyncBound)
	switch endTime := currentTime.Add(defaultMinStakingDuration); events[0] {
	case startPrimaryWithBLS:
		sk, err := bls.NewSecretKey()
		if err != nil {
			return nil, fmt.Errorf("could not make private key: %w", err)
		}

		res = append(res, &validatorInputData{
			eventType: startPrimaryWithBLS,
			startTime: currentTime,
			endTime:   endTime,
			nodeID:    nodeID,
			publicKey: bls.PublicFromSecretKey(sk),
		})
	default:
		return nil, fmt.Errorf("unexpected initial event %d", events[0])
	}

	// track current primary validator to make sure its staking period
	// covers all of its net validators
	currentPrimaryVal := res[0]
	for i := 1; i < len(events); i++ {
		currentTime = currentTime.Add(txexecutor.SyncBound)

		switch currentEvent := events[i]; currentEvent {
		case startNetValidator:
			endTime := currentTime.Add(defaultMinStakingDuration)
			res = append(res, &validatorInputData{
				eventType: startNetValidator,
				startTime: currentTime,
				endTime:   endTime,
				nodeID:    nodeID,
				publicKey: nil,
			})

			currentPrimaryVal.endTime = endTime.Add(time.Second)
			currentTime = endTime.Add(time.Second)

		case startPrimaryWithBLS:
			currentTime = currentPrimaryVal.endTime.Add(txexecutor.SyncBound)
			sk, err := bls.NewSecretKey()
			if err != nil {
				return nil, fmt.Errorf("could not make private key: %w", err)
			}

			endTime := currentTime.Add(defaultMinStakingDuration)
			val := &validatorInputData{
				eventType: startPrimaryWithBLS,
				startTime: currentTime,
				endTime:   endTime,
				nodeID:    nodeID,
				publicKey: bls.PublicFromSecretKey(sk),
			}
			res = append(res, val)
			currentPrimaryVal = val
		}
	}
	return res, nil
}

func TestTimestampListGenerator(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("primary validators are returned in sequence", prop.ForAll(
		func(events []uint8) string {
			currentTime := time.Now()
			nodeID := ids.GenerateTestNodeID()
			validatorsTimes, err := buildTimestampsList(events, currentTime, nodeID)
			if err != nil {
				return failedBuildingEventSeqString + err.Error()
			}

			if len(validatorsTimes) == 0 {
				return errEmptyEventsList.Error()
			}

			// nil out non net validators
			subnetIndexes := make([]int, 0)
			for idx, ev := range validatorsTimes {
				if ev.eventType == startNetValidator {
					subnetIndexes = append(subnetIndexes, idx)
				}
			}
			for _, idx := range subnetIndexes {
				validatorsTimes[idx] = nil
			}

			currentEventTime := currentTime
			for i, ev := range validatorsTimes {
				if ev == nil {
					continue // a net validator
				}
				if currentEventTime.After(ev.startTime) {
					return fmt.Sprintf("validator %d start time larger than current event time", i)
				}

				if ev.startTime.After(ev.endTime) {
					return fmt.Sprintf("validator %d start time larger than its end time", i)
				}

				currentEventTime = ev.endTime
			}

			return ""
		},
		gen.SliceOf(gen.OneConstOf(
			startPrimaryWithBLS,
			startNetValidator,
		)).SuchThat(func(v interface{}) bool {
			list := v.([]uint8)
			return len(list) > 0 && list[0] == startPrimaryWithBLS
		}),
	))

	properties.Property("subnet validators are returned in sequence", prop.ForAll(
		func(events []uint8) string {
			currentTime := time.Now()
			nodeID := ids.GenerateTestNodeID()
			validatorsTimes, err := buildTimestampsList(events, currentTime, nodeID)
			if err != nil {
				return failedBuildingEventSeqString + err.Error()
			}

			if len(validatorsTimes) == 0 {
				return errEmptyEventsList.Error()
			}

			// nil out non net validators
			nonSubnetIndexes := make([]int, 0)
			for idx, ev := range validatorsTimes {
				if ev.eventType != startNetValidator {
					nonSubnetIndexes = append(nonSubnetIndexes, idx)
				}
			}
			for _, idx := range nonSubnetIndexes {
				validatorsTimes[idx] = nil
			}

			currentEventTime := currentTime
			for i, ev := range validatorsTimes {
				if ev == nil {
					continue // a non-subnet validator
				}
				if currentEventTime.After(ev.startTime) {
					return fmt.Sprintf("validator %d start time larger than current event time", i)
				}

				if ev.startTime.After(ev.endTime) {
					return fmt.Sprintf("validator %d start time larger than its end time", i)
				}

				currentEventTime = ev.endTime
			}

			return ""
		},
		gen.SliceOf(gen.OneConstOf(
			startPrimaryWithBLS,
			startNetValidator,
		)).SuchThat(func(v interface{}) bool {
			list := v.([]uint8)
			return len(list) > 0 && list[0] == startPrimaryWithBLS
		}),
	))

	properties.Property("subnet validators' times are bound by a primary validator's times", prop.ForAll(
		func(events []uint8) string {
			currentTime := time.Now()
			nodeID := ids.GenerateTestNodeID()
			validatorsTimes, err := buildTimestampsList(events, currentTime, nodeID)
			if err != nil {
				return failedBuildingEventSeqString + err.Error()
			}

			if len(validatorsTimes) == 0 {
				return errEmptyEventsList.Error()
			}

			currentPrimaryValidator := validatorsTimes[0]
			for i := 1; i < len(validatorsTimes); i++ {
				if validatorsTimes[i].eventType != startNetValidator {
					currentPrimaryValidator = validatorsTimes[i]
					continue
				}

				subnetVal := validatorsTimes[i]
				if currentPrimaryValidator.startTime.After(subnetVal.startTime) ||
					subnetVal.endTime.After(currentPrimaryValidator.endTime) {
					return "subnet validator not bounded by primary network ones"
				}
			}
			return ""
		},
		gen.SliceOf(gen.OneConstOf(
			startPrimaryWithBLS,
			startNetValidator,
		)).SuchThat(func(v interface{}) bool {
			list := v.([]uint8)
			return len(list) > 0 && list[0] == startPrimaryWithBLS
		}),
	))

	properties.TestingRun(t)
}

// add a single validator at the end of times,
// to make sure it won't pollute our tests
func buildVM(t *testing.T) (*VM, ids.ID, error) {
	forkTime := defaultGenesisTime
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
			ApricotPhase3Time: forkTime,
			ApricotPhase5Time: forkTime,
			BanffTime:         forkTime,
			CortinaTime:       forkTime,
			EUpgradeTime:      mockable.MaxTime,
		},
	}}
	vm.nodeClock.Set(forkTime.Add(time.Second))

	baseDB := memdb.New()
	chainDB := prefixdb.New([]byte{0}, baseDB)
	// atomicDB := prefixdb.New([]byte{1}, baseDB) // Currently unused due to context issues

	// Use a fixed asset ID for testing
	luxAssetID := ids.GenerateTestID()
	vm.luxAssetID = luxAssetID

	// Create lux context for ChainContext
	luxCtx := &consContext.Context{
		QuantumID:  constants.UnitTestID,
		NetID:      constants.PrimaryNetworkID,
		ChainID:    constants.PlatformChainID,
		NodeID:     ids.GenerateTestNodeID(),
		PublicKey:  nil,
		XChainID:   ids.GenerateTestID(),
		CChainID:   ids.GenerateTestID(),
		LUXAssetID: luxAssetID, // Use the same asset ID
		StartTime:  time.Now(),
	}

	// Create ChainContext
	chainCtx := &linearblock.ChainContext{
		ConsensusContext: &linearblock.ConsensusContext{},
		Context:          luxCtx,
	}
	genesisBytes, err := buildCustomGenesis(luxAssetID)
	if err != nil {
		return nil, ids.Empty, err
	}

	// Create a mock DBManager
	dbManager := &mockDBManager{db: chainDB}
	// Create a message channel
	toEngine := make(chan linearblock.Message, 1)
	// Create a mock AppSender
	appSender := &mockAppSender{} // Use a mock instead of SenderTest

	err = vm.Initialize(
		context.Background(),
		chainCtx, // Pass proper ChainContext
		dbManager,
		genesisBytes,
		nil, // upgradeBytes
		nil, // configBytes
		toEngine,
		nil, // fxs
		appSender,
	)
	if err != nil {
		return nil, ids.Empty, err
	}

	err = vm.SetState(context.Background(), interfaces.NormalOp)
	if err != nil {
		return nil, ids.Empty, err
	}

	// Create a net and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
	// Create a context with the LUX asset ID
	factory := txstest.NewWalletFactoryWithAssets(context.Background(), nil, &vm.Config, vm.state, vm.luxAssetID)
	builder, signer := factory.NewWallet(keys[len(keys)-1])
	utx, err := builder.NewCreateNetTx(
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
		},
		walletcommon.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
		}),
	)
	if err != nil {
		return nil, ids.Empty, err
	}
	testSubnet1, err = walletsigner.SignUnsigned(context.Background(), signer, utx)
	if err != nil {
		return nil, ids.Empty, err
	}
	vm.lock.Unlock()
	err = vm.issueTxFromRPC(testSubnet1)
	vm.lock.Lock()
	if err != nil {
		return nil, ids.Empty, err
	}

	blk, err := vm.Builder.BuildBlock(context.Background())
	if err != nil {
		return nil, ids.Empty, err
	}
	if err := blk.Verify(context.Background()); err != nil {
		return nil, ids.Empty, err
	}
	if err := blk.Accept(context.Background()); err != nil {
		return nil, ids.Empty, err
	}
	if err := vm.SetPreference(context.Background(), vm.manager.LastAccepted()); err != nil {
		return nil, ids.Empty, err
	}

	return vm, testSubnet1.ID(), nil
}

func buildCustomGenesis(luxAssetID ids.ID) ([]byte, error) {
	genesisUTXOs := make([]api.UTXO, len(keys))
	for i, key := range keys {
		id := key.PublicKey().Address()
		addr, err := address.FormatBech32(constants.UnitTestHRP, id.Bytes())
		if err != nil {
			return nil, err
		}
		genesisUTXOs[i] = api.UTXO{
			Amount:  json.Uint64(defaultBalance),
			Address: addr,
		}
	}

	// we need at least a validator, otherwise BuildBlock would fail, since it
	// won't find next staker to promote/evict from stakers set. Contrary to
	// what happens with production code we push such validator at the end of
	// times, so to avoid interference with our tests
	nodeID := genesisNodeIDs[len(genesisNodeIDs)-1]
	// Use the corresponding key address, not nodeID bytes
	keyIndex := len(genesisNodeIDs) - 1
	keyAddr := keys[keyIndex].PublicKey().Address()
	addr, err := address.FormatBech32(constants.UnitTestHRP, keyAddr.Bytes())
	if err != nil {
		return nil, err
	}

	starTime := mockable.MaxTime.Add(-1 * defaultMinStakingDuration)
	endTime := mockable.MaxTime
	genesisValidator := api.GenesisPermissionlessValidator{
		GenesisValidator: api.GenesisValidator{
			StartTime: json.Uint64(starTime.Unix()),
			EndTime:   json.Uint64(endTime.Unix()),
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

	buildGenesisArgs := api.BuildGenesisArgs{
		Encoding:      formatting.Hex,
		NetworkID:     json.Uint32(constants.UnitTestID),
		LuxAssetID:    luxAssetID,
		UTXOs:         genesisUTXOs,
		Validators:    []api.GenesisPermissionlessValidator{genesisValidator},
		Chains:        nil,
		Time:          json.Uint64(defaultGenesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaLux),
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	if err := platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse); err != nil {
		return nil, err
	}

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	if err != nil {
		return nil, err
	}

	return genesisBytes, nil
}

// mockAppSender is a stub for app sender
type mockAppSender struct{}

func (m *mockAppSender) SendAppGossip(ctx context.Context, nodeIDs []ids.NodeID, msg []byte) error {
	return nil
}

func (m *mockAppSender) SendAppGossipSpecific(context.Context, set.Set[ids.NodeID], []byte, int) error {
	return nil
}

func (m *mockAppSender) SendCrossChainAppRequest(context.Context, ids.ID, uint32, []byte) error {
	return nil
}

func (m *mockAppSender) SendCrossChainAppError(context.Context, ids.ID, uint32, int32, string) error {
	return nil
}

func (m *mockAppSender) SendCrossChainAppResponse(context.Context, ids.ID, uint32, []byte) error {
	return nil
}

func (m *mockAppSender) SendAppError(context.Context, ids.NodeID, uint32, int32, string) error {
	return nil
}

func (m *mockAppSender) SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, request []byte) error {
	return nil
}

func (m *mockAppSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	return nil
}

// mockDBManager implements linearblock.DBManager
type mockDBManager struct {
	db database.Database
}

func (m *mockDBManager) Current() database.Database {
	return m.db
}

func (m *mockDBManager) Database(ids.ID) database.Database {
	return m.db
}

func (m *mockDBManager) Get(version uint64) (database.Database, error) {
	return m.db, nil
}

func (m *mockDBManager) Close() error {
	return nil
}
