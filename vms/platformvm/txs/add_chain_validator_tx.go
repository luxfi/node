// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// File deprecation notice: this file defines AddChainValidatorTx, the
// legacy per-chain validator registration tx. Under LP-018
// (sovereign-L1), validators join a network — never a chain — via
// AddValidatorTx. Chains live on networks (created via CreateChainTx);
// the canonical model has validators validate networks, not chains. A
// sovereign L1 IS a primary network at its own networkID; the Lux
// primary networks live at 1/2/3/1337, and any downstream consumer
// running its own primary picks any other uint32. AddValidatorTx is
// the universal add-validator-to-a-network entry point for all of
// them. New code must use AddValidatorTx. The wire codec entries,
// Visitor method, and wallet IssueAddChainValidatorTx helper are kept
// for one release cycle so existing pre-LP-018 P-chain history
// continues to decode and replay.

package txs

import (
	"context"
	"errors"

	"github.com/luxfi/runtime"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
)

var (
	_ StakerTx        = (*AddChainValidatorTx)(nil)
	_ ScheduledStaker = (*AddChainValidatorTx)(nil)

	errAddPrimaryNetworkValidator = errors.New("can't add primary network validator with AddChainValidatorTx")
)

// AddChainValidatorTx is the legacy per-chain (pre-LP-018: per-L1)
// validator registration tx.
//
// Deprecated: Use AddValidatorTx. Under LP-018 sovereign-L1, validators
// join a network (Lux primary 1/2/3/1337 or any sovereign L1's own
// primary at its EVM chainID). Chains live on networks; validators no
// longer register per-chain. This type is retained for one release
// cycle for wire/codec compat with pre-LP-018 binaries. The Visitor
// method, codec slots, and wallet IssueAddChainValidatorTx helper are
// preserved so older P-chain history still decodes and replays.
type AddChainValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	ChainValidator `serialize:"true" json:"validator"`
	// Auth that will be allowing this validator into the network
	ChainAuth verify.Verifiable `serialize:"true" json:"chainAuthorization"`
}

func (tx *AddChainValidatorTx) NodeID() ids.NodeID {
	return tx.ChainValidator.NodeID
}

func (*AddChainValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	return nil, false, nil
}

func (*AddChainValidatorTx) PendingPriority() Priority {
	return ChainPermissionedValidatorPendingPriority
}

func (*AddChainValidatorTx) CurrentPriority() Priority {
	return ChainPermissionedValidatorCurrentPriority
}

// SyntacticVerify returns nil iff [tx] is valid
func (tx *AddChainValidatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.Chain == constants.PrimaryNetworkID:
		return errAddPrimaryNetworkValidator
	}

	if err := tx.BaseTx.SyntacticVerify(rt); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.ChainAuth); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *AddChainValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddChainValidatorTx(tx)
}

// InitializeWithRuntime initializes the transaction with Runtime
func (tx *AddChainValidatorTx) Initialize(ctx context.Context) error {
	// Initialize any context-dependent fields here
	return nil
}
