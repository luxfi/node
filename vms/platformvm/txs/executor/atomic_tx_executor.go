// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var _ txs.Visitor = (*AtomicTxExecutor)(nil)

// atomicTxExecutor is used to execute atomic transactions pre-AP5. After AP5
// the execution was moved to be performed inside of the standardTxExecutor.
type AtomicTxExecutor struct {
	// inputs, to be filled before visitor methods are called
	*Backend
	ParentID      ids.ID
	StateVersions state.Versions
	Tx            *txs.Tx

	// outputs of visitor execution
	OnAccept       state.Diff
	Inputs         set.Set[ids.ID]
	AtomicRequests map[ids.ID]*atomic.Requests
}

func (*AtomicTxExecutor) AddValidatorTx(*txs.AddValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) CreateChainTx(*txs.CreateChainTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return ErrWrongTxType
}

func (*AtomicTxExecutor) BaseTx(*txs.BaseTx) error {
	return ErrWrongTxType
}

func (e *AtomicTxExecutor) ImportTx(tx *txs.ImportTx) error {
	return e.atomicTx(tx)
}

func (e *AtomicTxExecutor) ExportTx(tx *txs.ExportTx) error {
	return e.atomicTx(tx)
}

func (e *AtomicTxExecutor) atomicTx(tx txs.UnsignedTx) error {
	onAccept, err := state.NewDiff(
		e.ParentID,
		e.StateVersions,
	)
	if err != nil {
		return err
	}
	e.OnAccept = onAccept

	executor := StandardTxExecutor{
		Backend: e.Backend,
		State:   e.OnAccept,
		Tx:      e.Tx,
	}
	err = tx.Visit(&executor)
	e.Inputs = executor.Inputs
	e.AtomicRequests = executor.AtomicRequests
	return err
}

// DisableL1ValidatorTx handles L1 validator disabling
func (e *AtomicTxExecutor) DisableL1ValidatorTx(tx *txs.DisableL1ValidatorTx) error {
	return ErrWrongTxType
}

// IncreaseL1ValidatorBalanceTx handles L1 validator balance increase
func (e *AtomicTxExecutor) IncreaseL1ValidatorBalanceTx(tx *txs.IncreaseL1ValidatorBalanceTx) error {
	return ErrWrongTxType
}

// RegisterL1ValidatorTx handles L1 validator registration
func (e *AtomicTxExecutor) RegisterL1ValidatorTx(tx *txs.RegisterL1ValidatorTx) error {
	return ErrWrongTxType
}

// SetL1ValidatorWeightTx handles L1 validator weight setting
func (e *AtomicTxExecutor) SetL1ValidatorWeightTx(tx *txs.SetL1ValidatorWeightTx) error {
	return ErrWrongTxType
}
