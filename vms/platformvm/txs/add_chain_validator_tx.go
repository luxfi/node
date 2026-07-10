// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// File deprecation notice: this file defines AddChainValidatorTx, the
// legacy per-chain validator registration tx. Under LP-018
// (sovereign-L1), validators join a network — never a chain — via
// AddValidatorTx. Chains live on networks (created via CreateChainTx);
// the canonical model has validators validate networks, not chains. A
// sovereign L1 IS a primary network at its own networkID. AddValidatorTx
// is the universal add-validator-to-a-network entry point. New code must
// use AddValidatorTx. This type is retained for one release cycle so
// existing pre-LP-018 P-chain history continues to decode and replay.

package txs

import (
	"context"
	"errors"
	"time"

	"github.com/luxfi/constants"
	bls "github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

var (
	_ UnsignedTx      = (*AddChainValidatorTx)(nil)
	_ StakerTx        = (*AddChainValidatorTx)(nil)
	_ ScheduledStaker = (*AddChainValidatorTx)(nil)

	errAddPrimaryNetworkValidator = errors.New("can't add primary network validator with AddChainValidatorTx")
)

// AddChainValidatorTx is the legacy per-chain validator registration tx.
// The struct IS the wire: it holds the zap buffer and reads its fields by
// offset. No codec, no marshal.
//
// Wire: zap header + object{ envelope@0..76, Validator@77, Chain@121,
// ChainAuth@153 }.
//
// Deprecated: Use AddValidatorTx. Retained for one release cycle for
// wire/codec compat with pre-LP-018 binaries.
type AddChainValidatorTx struct {
	spendingTx
}

const (
	offACVValidator       = spendSize                       // 77: inline Validator (44B)
	offACVChain           = offACVValidator + validatorSize // 121: chain id (32B)
	offACVChainAuth       = offACVChain + 32                // 153: chain-auth list ptr (8B)
	addChainValidatorSize = offACVChainAuth + 8             // 161
)

// NewAddChainValidatorTx builds the tx into a fresh zap buffer — the one
// place a field becomes bytes (construction, not serialization).
func NewAddChainValidatorTx(base *lux.BaseTx, validator Validator, chain ids.ID, chainAuth verify.Verifiable) (*AddChainValidatorTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 1024 + addChainValidatorSize)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	authOff, authCount, err := writeAuth(b, chainAuth)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(addChainValidatorSize)
	setEnvelope(ob, kindAddChainValidator, base, p)
	setValidator(ob, offACVValidator, validator)
	setID(ob, offACVChain, chain)
	ob.SetList(offACVChainAuth, authOff, authCount)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &AddChainValidatorTx{spendingTx{msg}}, nil
}

// Validator is the inline validator descriptor (offset read).
func (tx *AddChainValidatorTx) Validator() Validator {
	return readValidator(tx.root(), offACVValidator)
}

// Chain is the network id this validator registers under (offset read).
func (tx *AddChainValidatorTx) Chain() ids.ID { return readID(tx.root(), offACVChain) }

// ChainAuth proves the issuer may add this validator to the network.
func (tx *AddChainValidatorTx) ChainAuth() verify.Verifiable {
	return readAuth(tx.root(), offACVChainAuth)
}

// ---- Staker / ScheduledStaker interface ----

// ChainID is the network id this validator registers under (legacy
// ChainValidator.ChainID, sourced from the pure Chain() accessor).
func (tx *AddChainValidatorTx) ChainID() ids.ID {
	return tx.Chain()
}

func (tx *AddChainValidatorTx) NodeID() ids.NodeID {
	return tx.Validator().NodeID
}

func (*AddChainValidatorTx) PublicKey() (*bls.PublicKey, bool, error) {
	return nil, false, nil
}

func (tx *AddChainValidatorTx) StartTime() time.Time {
	return time.Unix(int64(tx.Validator().Start), 0)
}

func (tx *AddChainValidatorTx) EndTime() time.Time {
	return time.Unix(int64(tx.Validator().End), 0)
}

func (tx *AddChainValidatorTx) Weight() uint64 {
	return tx.Validator().Wght
}

func (*AddChainValidatorTx) PendingPriority() Priority {
	return ChainPermissionedValidatorPendingPriority
}

func (*AddChainValidatorTx) CurrentPriority() Priority {
	return ChainPermissionedValidatorCurrentPriority
}

// SyntacticVerify returns nil iff [tx] is valid.
func (tx *AddChainValidatorTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	if tx.Chain() == constants.PrimaryNetworkID {
		return errAddPrimaryNetworkValidator
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	v := tx.Validator()
	if err := verify.All(&v, tx.ChainAuth()); err != nil {
		return err
	}
	return nil
}

func (tx *AddChainValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddChainValidatorTx(tx)
}

// Initialize is a no-op; Runtime is passed explicitly to InitRuntime.
func (tx *AddChainValidatorTx) Initialize(ctx context.Context) error {
	return nil
}
