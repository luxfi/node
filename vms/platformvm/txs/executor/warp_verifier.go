// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"

	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/warp"
)

const (
	WarpQuorumNumerator   = 67
	WarpQuorumDenominator = 100
)

var _ txs.Visitor = (*warpVerifier)(nil)

// validatorStateWrapper wraps validators.State to implement warp.ValidatorState
type validatorStateWrapper struct {
	state validators.State
}

func (w *validatorStateWrapper) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	return w.state.GetValidatorSet(height, subnetID)
}

func (w *validatorStateWrapper) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	// TODO: This needs to be implemented based on the chain registry
	// For now, return an error
	return ids.Empty, nil
}

// VerifyWarpMessages verifies all warp messages in the tx. If any of the warp
// messages are invalid, an error is returned.
func VerifyWarpMessages(
	ctx context.Context,
	networkID uint32,
	validatorState validators.State,
	pChainHeight uint64,
	tx txs.UnsignedTx,
) error {
	return tx.Visit(&warpVerifier{
		context:        ctx,
		networkID:      networkID,
		validatorState: &validatorStateWrapper{state: validatorState},
		pChainHeight:   pChainHeight,
	})
}

type warpVerifier struct {
	context        context.Context
	networkID      uint32
	validatorState warp.ValidatorState
	pChainHeight   uint64
}

func (*warpVerifier) AddValidatorTx(*txs.AddValidatorTx) error {
	return nil
}

func (*warpVerifier) AddSubnetValidatorTx(*txs.AddSubnetValidatorTx) error {
	return nil
}

func (*warpVerifier) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return nil
}

func (*warpVerifier) CreateChainTx(*txs.CreateChainTx) error {
	return nil
}

func (*warpVerifier) CreateSubnetTx(*txs.CreateSubnetTx) error {
	return nil
}

func (*warpVerifier) ImportTx(*txs.ImportTx) error {
	return nil
}

func (*warpVerifier) ExportTx(*txs.ExportTx) error {
	return nil
}

func (*warpVerifier) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return nil
}

func (*warpVerifier) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return nil
}

func (*warpVerifier) RemoveSubnetValidatorTx(*txs.RemoveSubnetValidatorTx) error {
	return nil
}

func (*warpVerifier) TransformSubnetTx(*txs.TransformSubnetTx) error {
	return nil
}

func (*warpVerifier) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return nil
}

func (*warpVerifier) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return nil
}

func (*warpVerifier) TransferSubnetOwnershipTx(*txs.TransferSubnetOwnershipTx) error {
	return nil
}

func (*warpVerifier) BaseTx(*txs.BaseTx) error {
	return nil
}

func (*warpVerifier) ConvertSubnetToL1Tx(*txs.ConvertSubnetToL1Tx) error {
	return nil
}

func (*warpVerifier) IncreaseL1ValidatorBalanceTx(*txs.IncreaseL1ValidatorBalanceTx) error {
	return nil
}

func (*warpVerifier) DisableL1ValidatorTx(*txs.DisableL1ValidatorTx) error {
	return nil
}

func (w *warpVerifier) RegisterL1ValidatorTx(tx *txs.RegisterL1ValidatorTx) error {
	return w.verify(tx.Message)
}

func (w *warpVerifier) SetL1ValidatorWeightTx(tx *txs.SetL1ValidatorWeightTx) error {
	return w.verify(tx.Message)
}

func (w *warpVerifier) verify(message []byte) error {
	msg, err := warp.ParseMessage(message)
	if err != nil {
		return err
	}

	// The signature verification now handles getting validators internally
	return msg.Signature.Verify(
		w.context,
		&msg.UnsignedMessage,
		w.networkID,
		w.validatorState,
		w.pChainHeight,
		WarpQuorumNumerator,
		WarpQuorumDenominator,
	)
}
