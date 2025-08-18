// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
)

var (
	_ txs.Visitor = (*StandardTxExecutor)(nil)

	errEmptyNodeID                = errors.New("validator nodeID cannot be empty")
	errMaxStakeDurationTooLarge   = errors.New("max stake duration must be less than or equal to the global max stake duration")
	errMissingStartTimePreDurango = errors.New("staker transactions must have a StartTime pre-Durango")
)

type StandardTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	State state.Diff // state is expected to be modified
	Tx    *txs.Tx

	// outputs of visitor execution
	OnAccept       func() // may be nil
	Inputs         set.Set[ids.ID]
	AtomicRequests map[ids.ID]*atomic.Requests // may be nil
}

func (*StandardTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (*StandardTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (e *StandardTxExecutor) CreateChainTx(tx *txs.CreateChainTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := lux.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(e.Backend, e.State, e.Tx, tx.SubnetID, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	feeCalculator := fee.NewStaticCalculator(e.Backend.Config.StaticFeeConfig, e.Backend.Config.UpgradeConfig)
	fee := feeCalculator.CalculateFee(tx, currentTimestamp)

	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			e.LUXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	lux.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	lux.Produce(e.State, txID, tx.Outs)
	// Add the new chain to the database
	e.State.AddChain(e.Tx)

	// If this proposal is committed and this node is a member of the subnet
	// that validates the blockchain, create the blockchain
	e.OnAccept = func() {
		e.Config.CreateChain(txID, tx)
	}
	return nil
}

func (e *StandardTxExecutor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	// Make sure this transaction is well formed.
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := lux.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	// Verify the flowcheck
	feeCalculator := fee.NewStaticCalculator(e.Backend.Config.StaticFeeConfig, e.Backend.Config.UpgradeConfig)
	fee := feeCalculator.CalculateFee(tx, currentTimestamp)

	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.LUXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	lux.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	lux.Produce(e.State, txID, tx.Outs)
	// Add the new subnet to the database
	e.State.AddSubnet(txID)
	e.State.SetSubnetOwner(txID, tx.Owner)
	return nil
}

func (e *StandardTxExecutor) ImportTx(tx *txs.ImportTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := lux.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	e.Inputs = set.NewSet[ids.ID](len(tx.ImportedInputs))
	utxoIDs := make([][]byte, len(tx.ImportedInputs))
	for i, in := range tx.ImportedInputs {
		utxoID := in.UTXOID.InputID()

		e.Inputs.Add(utxoID)
		utxoIDs[i] = utxoID[:]
	}

	// Skip verification of the shared memory inputs if the other primary
	// network chains are not guaranteed to be up-to-date.
	if e.Bootstrapped.Get() && !e.Config.PartialSyncPrimaryNetwork {
		if err := verify.SameSubnet(context.TODO(), e.Ctx, tx.SourceChain); err != nil {
			return err
		}

		allUTXOBytes, err := e.SharedMemory.Get(tx.SourceChain, utxoIDs)
		if err != nil {
			return fmt.Errorf("failed to get shared memory: %w", err)
		}

		utxos := make([]*lux.UTXO, len(tx.Ins)+len(tx.ImportedInputs))
		for index, input := range tx.Ins {
			utxo, err := e.State.GetUTXO(input.InputID())
			if err != nil {
				return fmt.Errorf("failed to get UTXO %s: %w", &input.UTXOID, err)
			}
			utxos[index] = utxo
		}
		for i, utxoBytes := range allUTXOBytes {
			utxo := &lux.UTXO{}
			if _, err := txs.Codec.Unmarshal(utxoBytes, utxo); err != nil {
				return fmt.Errorf("failed to unmarshal UTXO: %w", err)
			}
			utxos[i+len(tx.Ins)] = utxo
		}

		ins := make([]*lux.TransferableInput, len(tx.Ins)+len(tx.ImportedInputs))
		copy(ins, tx.Ins)
		copy(ins[len(tx.Ins):], tx.ImportedInputs)

		// Verify the flowcheck
		feeCalculator := fee.NewStaticCalculator(e.Backend.Config.StaticFeeConfig, e.Backend.Config.UpgradeConfig)
		fee := feeCalculator.CalculateFee(tx, currentTimestamp)

		if err := e.FlowChecker.VerifySpendUTXOs(
			tx,
			utxos,
			ins,
			tx.Outs,
			e.Tx.Creds,
			map[ids.ID]uint64{
				e.LUXAssetID: fee,
			},
		); err != nil {
			return err
		}
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	lux.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	lux.Produce(e.State, txID, tx.Outs)

	// Note: We apply atomic requests even if we are not verifying atomic
	// requests to ensure the shared state will be correct if we later start
	// verifying the requests.
	e.AtomicRequests = map[ids.ID]*atomic.Requests{
		tx.SourceChain: {
			RemoveRequests: utxoIDs,
		},
	}
	return nil
}

func (e *StandardTxExecutor) ExportTx(tx *txs.ExportTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := lux.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	outs := make([]*lux.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	if e.Bootstrapped.Get() {
		if err := verify.SameSubnet(context.TODO(), e.Ctx, tx.DestinationChain); err != nil {
			return err
		}
	}

	// Verify the flowcheck
	feeCalculator := fee.NewStaticCalculator(e.Backend.Config.StaticFeeConfig, e.Backend.Config.UpgradeConfig)
	fee := feeCalculator.CalculateFee(tx, currentTimestamp)

	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.LUXAssetID: fee,
		},
	); err != nil {
		return fmt.Errorf("failed verifySpend: %w", err)
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	lux.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	lux.Produce(e.State, txID, tx.Outs)

	// Note: We apply atomic requests even if we are not verifying atomic
	// requests to ensure the shared state will be correct if we later start
	// verifying the requests.
	elems := make([]*atomic.Element, len(tx.ExportedOutputs))
	for i, out := range tx.ExportedOutputs {
		utxo := &lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(tx.Outs) + i),
			},
			Asset: lux.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
		if err != nil {
			return fmt.Errorf("failed to marshal UTXO: %w", err)
		}
		utxoID := utxo.InputID()
		elem := &atomic.Element{
			Key:   utxoID[:],
			Value: utxoBytes,
		}
		if out, ok := utxo.Out.(lux.Addressable); ok {
			elem.Traits = out.Addresses()
		}

		elems[i] = elem
	}
	e.AtomicRequests = map[ids.ID]*atomic.Requests{
		tx.DestinationChain: {
			PutRequests: elems,
		},
	}
	return nil
}

func (e *StandardTxExecutor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	if tx.Validator.NodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}

	if _, err := verifyAddValidatorTx(
		e.Backend,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	lux.Consume(e.State, tx.Ins)
	lux.Produce(e.State, txID, tx.Outs)

	if e.Config.PartialSyncPrimaryNetwork && tx.Validator.NodeID == e.NodeID {
		if e.Log != nil {
			e.Log.Warn("verified transaction that would cause this node to become unhealthy")
		}
	}
	return nil
}

func (e *StandardTxExecutor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	if err := verifyAddSubnetValidatorTx(
		e.Backend,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	lux.Consume(e.State, tx.Ins)
	lux.Produce(e.State, txID, tx.Outs)
	return nil
}

func (e *StandardTxExecutor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	if _, err := verifyAddDelegatorTx(
		e.Backend,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	lux.Consume(e.State, tx.Ins)
	lux.Produce(e.State, txID, tx.Outs)
	return nil
}

// Verifies a [*txs.RemoveSubnetValidatorTx] and, if it passes, executes it on
// [e.State]. For verification rules, see [verifyRemoveSubnetValidatorTx]. This
// transaction will result in [tx.NodeID] being removed as a validator of
// [tx.SubnetID].
// Note: [tx.NodeID] may be either a current or pending validator.
func (e *StandardTxExecutor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	staker, isCurrentValidator, err := verifyRemoveSubnetValidatorTx(
		e.Backend,
		e.State,
		e.Tx,
		tx,
	)
	if err != nil {
		return err
	}

	if isCurrentValidator {
		e.State.DeleteCurrentValidator(staker)
	} else {
		e.State.DeletePendingValidator(staker)
	}

	// Invariant: There are no permissioned subnet delegators to remove.

	txID := e.Tx.ID()
	lux.Consume(e.State, tx.Ins)
	lux.Produce(e.State, txID, tx.Outs)

	return nil
}

func (e *StandardTxExecutor) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = e.State.GetTimestamp()
		isDurangoActive  = e.Config.UpgradeConfig.IsDurangoActivated(currentTimestamp)
	)
	if err := lux.VerifyMemoFieldLength(tx.Memo, isDurangoActive); err != nil {
		return err
	}

	// Note: math.MaxInt32 * time.Second < math.MaxInt64 - so this can never
	// overflow.
	if time.Duration(tx.MaxStakeDuration)*time.Second > e.Backend.Config.MaxStakeDuration {
		return errMaxStakeDurationTooLarge
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(e.Backend, e.State, e.Tx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	feeCalculator := fee.NewStaticCalculator(e.Backend.Config.StaticFeeConfig, e.Backend.Config.UpgradeConfig)
	fee := feeCalculator.CalculateFee(tx, currentTimestamp)

	totalRewardAmount := tx.MaximumSupply - tx.InitialSupply
	if err := e.Backend.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		// Invariant: [tx.AssetID != e.LUXAssetID]. This prevents the first
		//            entry in this map literal from being overwritten by the
		//            second entry.
		map[ids.ID]uint64{
			e.LUXAssetID: fee,
			tx.AssetID:   totalRewardAmount,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()

	// Consume the UTXOS
	lux.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	lux.Produce(e.State, txID, tx.Outs)
	// Transform the new subnet in the database
	e.State.AddSubnetTransformation(e.Tx)
	e.State.SetCurrentSupply(tx.Subnet, tx.InitialSupply)
	return nil
}

func (e *StandardTxExecutor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	if err := verifyAddPermissionlessValidatorTx(
		e.Backend,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	lux.Consume(e.State, tx.Ins)
	lux.Produce(e.State, txID, tx.Outs)

	if e.Config.PartialSyncPrimaryNetwork &&
		tx.Subnet == constants.PrimaryNetworkID &&
		tx.Validator.NodeID == e.NodeID {
		if e.Log != nil {
			e.Log.Warn("verified transaction that would cause this node to become unhealthy")
		}
	}

	return nil
}

func (e *StandardTxExecutor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	if err := verifyAddPermissionlessDelegatorTx(
		e.Backend,
		e.State,
		e.Tx,
		tx,
	); err != nil {
		return err
	}

	if err := e.putStaker(tx); err != nil {
		return err
	}

	txID := e.Tx.ID()
	lux.Consume(e.State, tx.Ins)
	lux.Produce(e.State, txID, tx.Outs)
	return nil
}

// Verifies a [*txs.TransferSubnetOwnershipTx] and, if it passes, executes it on
// [e.State]. For verification rules, see [verifyTransferSubnetOwnershipTx].
// This transaction will result in the ownership of [tx.Subnet] being transferred
// to [tx.Owner].
func (e *StandardTxExecutor) TransferSubnetOwnershipTx(tx *txs.TransferSubnetOwnershipTx) error {
	err := verifyTransferSubnetOwnershipTx(
		e.Backend,
		e.State,
		e.Tx,
		tx,
	)
	if err != nil {
		return err
	}

	e.State.SetSubnetOwner(tx.Subnet, tx.Owner)

	txID := e.Tx.ID()
	lux.Consume(e.State, tx.Ins)
	lux.Produce(e.State, txID, tx.Outs)
	return nil
}

func (e *StandardTxExecutor) BaseTx(tx *txs.BaseTx) error {
	if !e.Backend.Config.UpgradeConfig.IsDurangoActivated(e.State.GetTimestamp()) {
		return ErrDurangoUpgradeNotActive
	}

	// Verify the tx is well-formed
	if err := e.Tx.SyntacticVerify(e.Ctx); err != nil {
		return err
	}

	if err := lux.VerifyMemoFieldLength(tx.Memo, true /*=isDurangoActive*/); err != nil {
		return err
	}

	// Verify the flowcheck
	currentTimestamp := e.State.GetTimestamp()
	feeCalculator := fee.NewStaticCalculator(e.Backend.Config.StaticFeeConfig, e.Backend.Config.UpgradeConfig)
	fee := feeCalculator.CalculateFee(tx, currentTimestamp)

	if err := e.FlowChecker.VerifySpend(
		tx,
		e.State,
		tx.Ins,
		tx.Outs,
		e.Tx.Creds,
		map[ids.ID]uint64{
			e.LUXAssetID: fee,
		},
	); err != nil {
		return err
	}

	txID := e.Tx.ID()
	// Consume the UTXOS
	lux.Consume(e.State, tx.Ins)
	// Produce the UTXOS
	lux.Produce(e.State, txID, tx.Outs)
	return nil
}

// Creates the staker as defined in [stakerTx] and adds it to [e.State].
func (e *StandardTxExecutor) putStaker(stakerTx txs.Staker) error {
	var (
		chainTime = e.State.GetTimestamp()
		txID      = e.Tx.ID()
		staker    *state.Staker
		err       error
	)

	if !e.Config.UpgradeConfig.IsDurangoActivated(chainTime) {
		// Pre-Durango, stakers set a future [StartTime] and are added to the
		// pending staker set. They are promoted to the current staker set once
		// the chain time reaches [StartTime].
		scheduledStakerTx, ok := stakerTx.(txs.ScheduledStaker)
		if !ok {
			return fmt.Errorf("%w: %T", errMissingStartTimePreDurango, stakerTx)
		}
		staker, err = state.NewPendingStaker(txID, scheduledStakerTx)
	} else {
		// Only calculate the potentialReward for permissionless stakers.
		// Recall that we only need to check if this is a permissioned
		// validator as there are no permissioned delegators
		var potentialReward uint64
		if !stakerTx.CurrentPriority().IsPermissionedValidator() {
			subnetID := stakerTx.SubnetID()
			currentSupply, err := e.State.GetCurrentSupply(subnetID)
			if err != nil {
				return err
			}

			rewards, err := GetRewardsCalculator(e.Backend, e.State, subnetID)
			if err != nil {
				return err
			}

			// Post-Durango, stakers are immediately added to the current staker
			// set. Their [StartTime] is the current chain time.
			stakeDuration := stakerTx.EndTime().Sub(chainTime)
			potentialReward = rewards.Calculate(
				stakeDuration,
				stakerTx.Weight(),
				currentSupply,
			)

			e.State.SetCurrentSupply(subnetID, currentSupply+potentialReward)
		}

		staker, err = state.NewCurrentStaker(txID, stakerTx, chainTime, potentialReward)
	}
	if err != nil {
		return err
	}

	switch priority := staker.Priority; {
	case priority.IsCurrentValidator():
		e.State.PutCurrentValidator(staker)
	case priority.IsCurrentDelegator():
		e.State.PutCurrentDelegator(staker)
	case priority.IsPendingValidator():
		e.State.PutPendingValidator(staker)
	case priority.IsPendingDelegator():
		e.State.PutPendingDelegator(staker)
	default:
		return fmt.Errorf("staker %s, unexpected priority %d", staker.TxID, priority)
	}
	return nil
}

// DisableL1ValidatorTx handles L1 validator disabling
func (e *StandardTxExecutor) DisableL1ValidatorTx(tx *txs.DisableL1ValidatorTx) error {
	return nil
}

// IncreaseL1ValidatorBalanceTx handles L1 validator balance increase
func (e *StandardTxExecutor) IncreaseL1ValidatorBalanceTx(tx *txs.IncreaseL1ValidatorBalanceTx) error {
	return nil
}

// RegisterL1ValidatorTx handles L1 validator registration
func (e *StandardTxExecutor) RegisterL1ValidatorTx(tx *txs.RegisterL1ValidatorTx) error {
	return nil
}

// SetL1ValidatorWeightTx handles L1 validator weight setting
func (e *StandardTxExecutor) SetL1ValidatorWeightTx(tx *txs.SetL1ValidatorWeightTx) error {
	return nil
}

// ConvertSubnetToL1Tx handles converting a subnet to L1
func (e *StandardTxExecutor) ConvertSubnetToL1Tx(tx *txs.ConvertSubnetToL1Tx) error {
	return nil
}
