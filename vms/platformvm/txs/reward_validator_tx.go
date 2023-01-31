// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ UnsignedTx = (*RewardValidatorTx)(nil)

// RewardValidatorTx is a transaction that represents a proposal to
// remove a validator that is currently validating from the validator set.
//
// If this transaction is accepted and the next block accepted is a Commit
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX as well as a validating reward.
//
// If this transaction is accepted and the next block accepted is an Abort
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX but no reward.
type RewardValidatorTx struct {
	// ID of the tx that created the delegator/validator being removed/rewarded
	TxID ids.ID `serialize:"true" json:"txID"`

	// Marks if this validator should be rewarded according to this node.
	ShouldPreferCommit bool `json:"-"`

	unsignedBytes []byte // Unsigned byte representation of this data
}

func (tx *RewardValidatorTx) SetBytes(unsignedBytes []byte) {
	tx.unsignedBytes = unsignedBytes
}
<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))

func (*RewardValidatorTx) InitCtx(*snow.Context) {}

func (tx *RewardValidatorTx) Bytes() []byte {
	return tx.unsignedBytes
}

<<<<<<< HEAD
<<<<<<< HEAD
func (*RewardValidatorTx) InputIDs() set.Set[ids.ID] {
=======
func (*RewardValidatorTx) InputIDs() ids.Set {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
func (*RewardValidatorTx) InputIDs() set.Set[ids.ID] {
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
	return nil
}

func (*RewardValidatorTx) Outputs() []*avax.TransferableOutput {
	return nil
}

func (*RewardValidatorTx) SyntacticVerify(*snow.Context) error {
	return nil
}
<<<<<<< HEAD
=======
func (*RewardValidatorTx) InitCtx(*snow.Context)               {}
func (tx *RewardValidatorTx) Bytes() []byte                    { return tx.unsignedBytes }
func (*RewardValidatorTx) InputIDs() ids.Set                   { return nil }
func (*RewardValidatorTx) Outputs() []*avax.TransferableOutput { return nil }
func (*RewardValidatorTx) SyntacticVerify(*snow.Context) error { return nil }
>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))
=======
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))

func (tx *RewardValidatorTx) Visit(visitor Visitor) error {
	return visitor.RewardValidatorTx(tx)
}
