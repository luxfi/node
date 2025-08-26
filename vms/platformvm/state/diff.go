// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/iterator"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var (
	_ Diff     = (*diff)(nil)
	_ Versions = stateGetter{}

	ErrMissingParentState = errors.New("missing parent state")
)

type Diff interface {
	Chain

	Apply(Chain) error
}

type diff struct {
	parentID      ids.ID
	stateVersions Versions

	timestamp time.Time

	// Net ID --> supply of native asset of the subnet
	currentSupply map[ids.ID]uint64

	currentStakerDiffs diffStakers
	// map of netID -> nodeID -> total accrued delegatee rewards
	modifiedDelegateeRewards map[ids.ID]map[ids.NodeID]uint64
	pendingStakerDiffs       diffStakers

	addedNetIDs []ids.ID
	// Net ID --> Owner of the subnet
	subnetOwners map[ids.ID]fx.Owner
	// Net ID --> Tx that transforms the subnet
	transformedSubnets map[ids.ID]*txs.Tx

	addedChains map[ids.ID][]*txs.Tx

	addedRewardUTXOs map[ids.ID][]*lux.UTXO

	addedTxs map[ids.ID]*txAndStatus

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*lux.UTXO
}

func NewDiff(
	parentID ids.ID,
	stateVersions Versions,
) (Diff, error) {
	parentState, ok := stateVersions.GetState(parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, parentID)
	}
	return &diff{
		parentID:      parentID,
		stateVersions: stateVersions,
		timestamp:     parentState.GetTimestamp(),
		subnetOwners:  make(map[ids.ID]fx.Owner),
	}, nil
}

type stateGetter struct {
	state Chain
}

func (s stateGetter) GetState(ids.ID) (Chain, bool) {
	return s.state, true
}

func NewDiffOn(parentState Chain) (Diff, error) {
	return NewDiff(ids.Empty, stateGetter{
		state: parentState,
	})
}

func (d *diff) GetTimestamp() time.Time {
	return d.timestamp
}

func (d *diff) SetTimestamp(timestamp time.Time) {
	d.timestamp = timestamp
}

func (d *diff) GetCurrentSupply(netID ids.ID) (uint64, error) {
	supply, ok := d.currentSupply[netID]
	if ok {
		return supply, nil
	}

	// If the net supply wasn't modified in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetCurrentSupply(netID)
}

func (d *diff) SetCurrentSupply(netID ids.ID, currentSupply uint64) {
	if d.currentSupply == nil {
		d.currentSupply = map[ids.ID]uint64{
			netID: currentSupply,
		}
	} else {
		d.currentSupply[netID] = currentSupply
	}
}

func (d *diff) GetCurrentValidator(netID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	// If the validator was modified in this diff, return the modified
	// validator.
	newValidator, status := d.currentStakerDiffs.GetValidator(netID, nodeID)
	switch status {
	case added:
		return newValidator, nil
	case deleted:
		return nil, database.ErrNotFound
	default:
		// If the validator wasn't modified in this diff, ask the parent state.
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetCurrentValidator(netID, nodeID)
	}
}

func (d *diff) SetDelegateeReward(netID ids.ID, nodeID ids.NodeID, amount uint64) error {
	if d.modifiedDelegateeRewards == nil {
		d.modifiedDelegateeRewards = make(map[ids.ID]map[ids.NodeID]uint64)
	}
	nodes, ok := d.modifiedDelegateeRewards[netID]
	if !ok {
		nodes = make(map[ids.NodeID]uint64)
		d.modifiedDelegateeRewards[netID] = nodes
	}
	nodes[nodeID] = amount
	return nil
}

func (d *diff) GetDelegateeReward(netID ids.ID, nodeID ids.NodeID) (uint64, error) {
	amount, modified := d.modifiedDelegateeRewards[netID][nodeID]
	if modified {
		return amount, nil
	}
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetDelegateeReward(netID, nodeID)
}

func (d *diff) PutCurrentValidator(staker *Staker) {
	d.currentStakerDiffs.PutValidator(staker)
}

func (d *diff) DeleteCurrentValidator(staker *Staker) {
	d.currentStakerDiffs.DeleteValidator(staker)
}

func (d *diff) GetCurrentDelegatorIterator(netID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetCurrentDelegatorIterator(netID, nodeID)
	if err != nil {
		return nil, err
	}

	return d.currentStakerDiffs.GetDelegatorIterator(parentIterator, netID, nodeID), nil
}

func (d *diff) PutCurrentDelegator(staker *Staker) {
	d.currentStakerDiffs.PutDelegator(staker)
}

func (d *diff) DeleteCurrentDelegator(staker *Staker) {
	d.currentStakerDiffs.DeleteDelegator(staker)
}

func (d *diff) GetCurrentStakerIterator() (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetCurrentStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.currentStakerDiffs.GetStakerIterator(parentIterator), nil
}

func (d *diff) GetPendingValidator(netID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	// If the validator was modified in this diff, return the modified
	// validator.
	newValidator, status := d.pendingStakerDiffs.GetValidator(netID, nodeID)
	switch status {
	case added:
		return newValidator, nil
	case deleted:
		return nil, database.ErrNotFound
	default:
		// If the validator wasn't modified in this diff, ask the parent state.
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetPendingValidator(netID, nodeID)
	}
}

func (d *diff) PutPendingValidator(staker *Staker) {
	d.pendingStakerDiffs.PutValidator(staker)
}

func (d *diff) DeletePendingValidator(staker *Staker) {
	d.pendingStakerDiffs.DeleteValidator(staker)
}

func (d *diff) GetPendingDelegatorIterator(netID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetPendingDelegatorIterator(netID, nodeID)
	if err != nil {
		return nil, err
	}

	return d.pendingStakerDiffs.GetDelegatorIterator(parentIterator, netID, nodeID), nil
}

func (d *diff) PutPendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.PutDelegator(staker)
}

func (d *diff) DeletePendingDelegator(staker *Staker) {
	d.pendingStakerDiffs.DeleteDelegator(staker)
}

func (d *diff) GetPendingStakerIterator() (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetPendingStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.pendingStakerDiffs.GetStakerIterator(parentIterator), nil
}

func (d *diff) AddNet(netID ids.ID) {
	d.addedNetIDs = append(d.addedNetIDs, netID)
}

func (d *diff) GetSubnetOwner(netID ids.ID) (fx.Owner, error) {
	owner, exists := d.subnetOwners[netID]
	if exists {
		return owner, nil
	}

	// If the net owner was not assigned in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, ErrMissingParentState
	}
	return parentState.GetSubnetOwner(netID)
}

func (d *diff) SetSubnetOwner(netID ids.ID, owner fx.Owner) {
	d.subnetOwners[netID] = owner
}

func (d *diff) GetSubnetTransformation(netID ids.ID) (*txs.Tx, error) {
	tx, exists := d.transformedSubnets[netID]
	if exists {
		return tx, nil
	}

	// If the net wasn't transformed in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, ErrMissingParentState
	}
	return parentState.GetSubnetTransformation(netID)
}

func (d *diff) AddNetTransformation(transformSubnetTxIntf *txs.Tx) {
	transformSubnetTx := transformSubnetTxIntf.Unsigned.(*txs.TransformNetTx)
	if d.transformedSubnets == nil {
		d.transformedSubnets = map[ids.ID]*txs.Tx{
			transformSubnetTx.Subnet: transformSubnetTxIntf,
		}
	} else {
		d.transformedSubnets[transformSubnetTx.Subnet] = transformSubnetTxIntf
	}
}

func (d *diff) AddChain(createChainTx *txs.Tx) {
	tx := createChainTx.Unsigned.(*txs.CreateChainTx)
	if d.addedChains == nil {
		d.addedChains = map[ids.ID][]*txs.Tx{
			tx.NetID: {createChainTx},
		}
	} else {
		d.addedChains[tx.NetID] = append(d.addedChains[tx.NetID], createChainTx)
	}
}

func (d *diff) GetTx(txID ids.ID) (*txs.Tx, status.Status, error) {
	if tx, exists := d.addedTxs[txID]; exists {
		return tx.tx, tx.status, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, status.Unknown, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetTx(txID)
}

func (d *diff) AddTx(tx *txs.Tx, status status.Status) {
	txID := tx.ID()
	txStatus := &txAndStatus{
		tx:     tx,
		status: status,
	}
	if d.addedTxs == nil {
		d.addedTxs = map[ids.ID]*txAndStatus{
			txID: txStatus,
		}
	} else {
		d.addedTxs[txID] = txStatus
	}
}

func (d *diff) AddRewardUTXO(txID ids.ID, utxo *lux.UTXO) {
	if d.addedRewardUTXOs == nil {
		d.addedRewardUTXOs = make(map[ids.ID][]*lux.UTXO)
	}
	d.addedRewardUTXOs[txID] = append(d.addedRewardUTXOs[txID], utxo)
}

func (d *diff) GetUTXO(utxoID ids.ID) (*lux.UTXO, error) {
	utxo, modified := d.modifiedUTXOs[utxoID]
	if !modified {
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		return parentState.GetUTXO(utxoID)
	}
	if utxo == nil {
		return nil, database.ErrNotFound
	}
	return utxo, nil
}

func (d *diff) AddUTXO(utxo *lux.UTXO) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*lux.UTXO{
			utxo.InputID(): utxo,
		}
	} else {
		d.modifiedUTXOs[utxo.InputID()] = utxo
	}
}

func (d *diff) DeleteUTXO(utxoID ids.ID) {
	if d.modifiedUTXOs == nil {
		d.modifiedUTXOs = map[ids.ID]*lux.UTXO{
			utxoID: nil,
		}
	} else {
		d.modifiedUTXOs[utxoID] = nil
	}
}

// L1 Validator support methods
func (d *diff) GetL1Validator(validationID ids.ID) (L1Validator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return L1Validator{}, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetL1Validator(validationID)
}

func (d *diff) HasL1Validator(netID ids.ID, nodeID ids.NodeID) (bool, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.HasL1Validator(netID, nodeID)
}

func (d *diff) WeightOfL1Validators(netID ids.ID) (uint64, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.WeightOfL1Validators(netID)
}

func (d *diff) PutL1Validator(validator L1Validator) error {
	// For now, just return nil
	return nil
}


func (d *diff) Apply(baseState Chain) error {
	baseState.SetTimestamp(d.timestamp)
	for netID, supply := range d.currentSupply {
		baseState.SetCurrentSupply(netID, supply)
	}
	for _, subnetValidatorDiffs := range d.currentStakerDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			switch validatorDiff.validatorStatus {
			case added:
				baseState.PutCurrentValidator(validatorDiff.validator)
			case deleted:
				baseState.DeleteCurrentValidator(validatorDiff.validator)
			}

			addedDelegatorIterator := NewTreeIterator(validatorDiff.addedDelegators)
			for addedDelegatorIterator.Next() {
				baseState.PutCurrentDelegator(addedDelegatorIterator.Value())
			}
			addedDelegatorIterator.Release()

			for _, delegator := range validatorDiff.deletedDelegators {
				baseState.DeleteCurrentDelegator(delegator)
			}
		}
	}
	for netID, nodes := range d.modifiedDelegateeRewards {
		for nodeID, amount := range nodes {
			if err := baseState.SetDelegateeReward(netID, nodeID, amount); err != nil {
				return err
			}
		}
	}
	for _, subnetValidatorDiffs := range d.pendingStakerDiffs.validatorDiffs {
		for _, validatorDiff := range subnetValidatorDiffs {
			switch validatorDiff.validatorStatus {
			case added:
				baseState.PutPendingValidator(validatorDiff.validator)
			case deleted:
				baseState.DeletePendingValidator(validatorDiff.validator)
			}

			addedDelegatorIterator := NewTreeIterator(validatorDiff.addedDelegators)
			for addedDelegatorIterator.Next() {
				baseState.PutPendingDelegator(addedDelegatorIterator.Value())
			}
			addedDelegatorIterator.Release()

			for _, delegator := range validatorDiff.deletedDelegators {
				baseState.DeletePendingDelegator(delegator)
			}
		}
	}
	for _, netID := range d.addedNetIDs {
		baseState.AddNet(netID)
	}
	for _, tx := range d.transformedSubnets {
		baseState.AddNetTransformation(tx)
	}
	for _, chains := range d.addedChains {
		for _, chain := range chains {
			baseState.AddChain(chain)
		}
	}
	for _, tx := range d.addedTxs {
		baseState.AddTx(tx.tx, tx.status)
	}
	for txID, utxos := range d.addedRewardUTXOs {
		for _, utxo := range utxos {
			baseState.AddRewardUTXO(txID, utxo)
		}
	}
	for utxoID, utxo := range d.modifiedUTXOs {
		if utxo != nil {
			baseState.AddUTXO(utxo)
		} else {
			baseState.DeleteUTXO(utxoID)
		}
	}
	for netID, owner := range d.subnetOwners {
		baseState.SetSubnetOwner(netID, owner)
	}
	return nil
}
// GetActiveL1ValidatorsIterator implements L1Validators interface
func (d *diff) GetActiveL1ValidatorsIterator() (iterator.Iterator[L1Validator], error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetActiveL1ValidatorsIterator()
}

// NumActiveL1Validators implements L1Validators interface
func (d *diff) NumActiveL1Validators() int {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0
	}
	return parentState.NumActiveL1Validators()
}
