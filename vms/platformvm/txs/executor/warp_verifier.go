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

func (w *validatorStateWrapper) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return w.state.GetValidatorSet(ctx, height, netID)
}

func (w *validatorStateWrapper) GetNetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
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

func (*warpVerifier) AddNetValidatorTx(*txs.AddNetValidatorTx) error {
	return nil
}

func (*warpVerifier) AddDelegatorTx(*txs.AddDelegatorTx) error {
	return nil
}

func (*warpVerifier) CreateChainTx(*txs.CreateChainTx) error {
	return nil
}

func (*warpVerifier) CreateNetTx(*txs.CreateNetTx) error {
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

func (*warpVerifier) RemoveNetValidatorTx(*txs.RemoveNetValidatorTx) error {
	return nil
}

func (*warpVerifier) TransformNetTx(*txs.TransformNetTx) error {
	return nil
}

func (*warpVerifier) AddPermissionlessValidatorTx(*txs.AddPermissionlessValidatorTx) error {
	return nil
}

func (*warpVerifier) AddPermissionlessDelegatorTx(*txs.AddPermissionlessDelegatorTx) error {
	return nil
}

func (*warpVerifier) TransferNetOwnershipTx(*txs.TransferNetOwnershipTx) error {
	return nil
}

func (*warpVerifier) BaseTx(*txs.BaseTx) error {
	return nil
}

func (*warpVerifier) ConvertNetToL1Tx(*txs.ConvertNetToL1Tx) error {
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
